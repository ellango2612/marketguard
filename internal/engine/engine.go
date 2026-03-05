package engine

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/ellango2612/marketguard/internal/models"
)

const (
	defaultWorkers   = 48
	defaultQueueSize = 10000

	spoofingVolumeThreshold  = 500000
	washTradeWindowSecs      = 60
	layeringLevelThreshold   = 5
	crossVenuePriceDeviation = 0.005 // 0.5%
)

// RiskEngine processes transactions concurrently and emits alerts.
type RiskEngine struct {
	workers    int
	queue      chan models.Transaction
	alerts     chan models.Alert
	wg         sync.WaitGroup
	logger     *zap.Logger
	processed  atomic.Int64
	flagged    atomic.Int64
	totalLatNs atomic.Int64 // for avg latency calculation
}

// New creates a RiskEngine with a worker pool of the given size.
func New(workers int, logger *zap.Logger) *RiskEngine {
	if workers <= 0 {
		workers = defaultWorkers
	}
	return &RiskEngine{
		workers: workers,
		queue:   make(chan models.Transaction, defaultQueueSize),
		alerts:  make(chan models.Alert, 1000),
		logger:  logger,
	}
}

// Start launches worker goroutines and returns immediately.
func (e *RiskEngine) Start(ctx context.Context) {
	e.logger.Info("starting risk engine", zap.Int("workers", e.workers))
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go e.worker(ctx, i)
	}
}

// Stop drains the queue and shuts down workers gracefully.
func (e *RiskEngine) Stop() {
	close(e.queue)
	e.wg.Wait()
	close(e.alerts)
	e.logger.Info("risk engine stopped",
		zap.Int64("processed", e.processed.Load()),
		zap.Int64("flagged", e.flagged.Load()),
	)
}

// Submit enqueues a transaction for async processing (non-blocking).
func (e *RiskEngine) Submit(tx models.Transaction) bool {
	select {
	case e.queue <- tx:
		return true
	default:
		e.logger.Warn("queue full, dropping transaction", zap.String("id", tx.ID))
		return false
	}
}

// Alerts returns the read-only channel of emitted alerts.
func (e *RiskEngine) Alerts() <-chan models.Alert {
	return e.alerts
}

// QueueDepth returns the current number of pending transactions.
func (e *RiskEngine) QueueDepth() int { return len(e.queue) }

// Metrics returns a snapshot of engine statistics.
func (e *RiskEngine) Metrics() (processed, flagged int64, avgLatMs float64) {
	processed = e.processed.Load()
	flagged = e.flagged.Load()
	total := e.totalLatNs.Load()
	if processed > 0 {
		avgLatMs = float64(total) / float64(processed) / 1e6
	}
	return
}

// ── Worker ─────────────────────────────────────────────────────────────────────

func (e *RiskEngine) worker(ctx context.Context, id int) {
	defer e.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case tx, ok := <-e.queue:
			if !ok {
				return
			}
			start := time.Now()
			alert := e.analyze(tx)
			latNs := time.Since(start).Nanoseconds()

			e.processed.Add(1)
			e.totalLatNs.Add(latNs)

			if alert != nil {
				alert.LatencyMs = latNs / 1e6
				e.flagged.Add(1)
				select {
				case e.alerts <- *alert:
				default:
					e.logger.Warn("alert channel full, dropping alert", zap.String("id", alert.ID))
				}
			}
		}
	}
}

// ── Detection rules ────────────────────────────────────────────────────────────

func (e *RiskEngine) analyze(tx models.Transaction) *models.Alert {
	checks := []func(models.Transaction) *models.Alert{
		e.detectSpoofing,
		e.detectWashTrade,
		e.detectLayering,
		e.detectCrossVenueManipulation,
		e.detectMomentumIgnition,
	}
	for _, check := range checks {
		if alert := check(tx); alert != nil {
			return alert
		}
	}
	return nil
}

// detectSpoofing flags large orders that deviate significantly from recent avg volume.
func (e *RiskEngine) detectSpoofing(tx models.Transaction) *models.Alert {
	if tx.Volume < spoofingVolumeThreshold {
		return nil
	}
	score := math.Min(100, 60+float64(tx.Volume)/float64(spoofingVolumeThreshold)*20)
	return e.newAlert(tx, models.RiskSpoofing, severityFromScore(score), score)
}

// detectWashTrade identifies rapid buy/sell cycles on the same symbol.
func (e *RiskEngine) detectWashTrade(tx models.Transaction) *models.Alert {
	// In production: query Redis for recent trades by same trader_id+symbol in window.
	// Simplified: probabilistic flag based on volume pattern.
	if tx.Volume < 100000 {
		return nil
	}
	score := 55.0
	if tx.Volume > 2000000 {
		score = 78.0
	}
	if score < 55 {
		return nil
	}
	return e.newAlert(tx, models.RiskWashTrade, severityFromScore(score), score)
}

// detectLayering checks for multiple pending orders creating artificial depth.
func (e *RiskEngine) detectLayering(tx models.Transaction) *models.Alert {
	// In production: query order book state from Redis.
	// Simplified heuristic: suspicious if volume is a round multiple of threshold.
	if tx.Volume%100000 != 0 || tx.Volume < 300000 {
		return nil
	}
	levels := int(tx.Volume / 100000)
	if levels < layeringLevelThreshold {
		return nil
	}
	score := math.Min(95, 45+float64(levels)*5)
	return e.newAlert(tx, models.RiskLayering, severityFromScore(score), score)
}

// detectCrossVenueManipulation flags price divergence across venues.
func (e *RiskEngine) detectCrossVenueManipulation(tx models.Transaction) *models.Alert {
	// In production: fetch reference price from Redis, compare.
	// Simplified: flag if price ends in a suspicious pattern (demo heuristic).
	deviation := math.Abs(tx.Price-math.Round(tx.Price)) / tx.Price
	if deviation < crossVenuePriceDeviation {
		return nil
	}
	score := math.Min(100, deviation/crossVenuePriceDeviation*30)
	if score < 40 {
		return nil
	}
	return e.newAlert(tx, models.RiskCrossVenue, severityFromScore(score), score)
}

// detectMomentumIgnition identifies sequences designed to trigger stop-losses.
func (e *RiskEngine) detectMomentumIgnition(tx models.Transaction) *models.Alert {
	// In production: sliding window of price velocity from Redis time series.
	if tx.Volume < 750000 || tx.Price <= 0 {
		return nil
	}
	score := 62.0
	return e.newAlert(tx, models.RiskMomentumIgn, severityFromScore(score), score)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func (e *RiskEngine) newAlert(tx models.Transaction, rt models.RiskType, sev models.Severity, score float64) *models.Alert {
	return &models.Alert{
		ID:            newID(),
		TransactionID: tx.ID,
		Symbol:        tx.Symbol,
		TraderID:      tx.TraderID,
		RiskType:      rt,
		Severity:      sev,
		Score:         score,
		DetectedAt:    time.Now().UTC(),
		Status:        "FLAGGED",
	}
}

func severityFromScore(score float64) models.Severity {
	switch {
	case score >= 90:
		return models.SeverityCritical
	case score >= 70:
		return models.SeverityHigh
	case score >= 40:
		return models.SeverityMedium
	default:
		return models.SeverityLow
	}
}

func newID() string {
	return time.Now().Format("20060102150405.000000000")
}
