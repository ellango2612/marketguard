package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/yourusername/marketguard/internal/models"
)

const (
	alertTTL   = 24 * time.Hour
	metricsTTL = 5 * time.Second
	traderTTL  = 10 * time.Minute

	keyAlerts      = "marketguard:alerts"
	keyMetrics     = "marketguard:metrics"
	keyTraderPrefix = "marketguard:trader:"
	keySymbolPrefix = "marketguard:symbol:"
)

// Cache wraps Redis with domain-specific helpers.
type Cache struct {
	rdb    *redis.Client
	logger *zap.Logger
	hits   int64
	misses int64
}

// New connects to Redis and returns a Cache.
func New(addr, password string, db int, logger *zap.Logger) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     20,
		MinIdleConns: 5,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	logger.Info("redis connected", zap.String("addr", addr))
	return &Cache{rdb: rdb, logger: logger}, nil
}

// ── Alert caching ──────────────────────────────────────────────────────────────

// PushAlert prepends alert to the recent alerts list and trims to 500 entries.
func (c *Cache) PushAlert(ctx context.Context, alert models.Alert) error {
	data, err := json.Marshal(alert)
	if err != nil {
		return err
	}
	pipe := c.rdb.Pipeline()
	pipe.LPush(ctx, keyAlerts, data)
	pipe.LTrim(ctx, keyAlerts, 0, 499)
	pipe.Expire(ctx, keyAlerts, alertTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// GetRecentAlerts returns the most recent n alerts from cache.
// Returns (nil, false) on cache miss.
func (c *Cache) GetRecentAlerts(ctx context.Context, n int) ([]models.Alert, bool) {
	vals, err := c.rdb.LRange(ctx, keyAlerts, 0, int64(n-1)).Result()
	if err != nil || len(vals) == 0 {
		c.misses++
		return nil, false
	}
	c.hits++
	alerts := make([]models.Alert, 0, len(vals))
	for _, v := range vals {
		var a models.Alert
		if json.Unmarshal([]byte(v), &a) == nil {
			alerts = append(alerts, a)
		}
	}
	return alerts, true
}

// ── Metrics caching ────────────────────────────────────────────────────────────

// SetMetrics caches a system metrics snapshot with a short TTL.
func (c *Cache) SetMetrics(ctx context.Context, m models.SystemMetrics) error {
	data, _ := json.Marshal(m)
	return c.rdb.Set(ctx, keyMetrics, data, metricsTTL).Err()
}

// GetMetrics returns cached system metrics.
func (c *Cache) GetMetrics(ctx context.Context) (*models.SystemMetrics, bool) {
	val, err := c.rdb.Get(ctx, keyMetrics).Bytes()
	if err != nil {
		c.misses++
		return nil, false
	}
	c.hits++
	var m models.SystemMetrics
	if err := json.Unmarshal(val, &m); err != nil {
		return nil, false
	}
	return &m, true
}

// ── Trader profile caching (hot-path dedup) ────────────────────────────────────

// IncrTraderVolume increments a trader's rolling volume counter.
func (c *Cache) IncrTraderVolume(ctx context.Context, traderID string, vol int64) error {
	key := keyTraderPrefix + traderID
	pipe := c.rdb.Pipeline()
	pipe.IncrBy(ctx, key, vol)
	pipe.Expire(ctx, key, traderTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// GetTraderVolume returns the cached rolling volume for a trader.
func (c *Cache) GetTraderVolume(ctx context.Context, traderID string) (int64, bool) {
	val, err := c.rdb.Get(ctx, keyTraderPrefix+traderID).Int64()
	if err != nil {
		c.misses++
		return 0, false
	}
	c.hits++
	return val, true
}

// ── Symbol reference price ─────────────────────────────────────────────────────

// SetSymbolPrice caches the latest reference price for cross-venue detection.
func (c *Cache) SetSymbolPrice(ctx context.Context, symbol string, price float64) error {
	return c.rdb.Set(ctx, keySymbolPrefix+symbol, price, 30*time.Second).Err()
}

// GetSymbolPrice retrieves the cached reference price for a symbol.
func (c *Cache) GetSymbolPrice(ctx context.Context, symbol string) (float64, bool) {
	val, err := c.rdb.Get(ctx, keySymbolPrefix+symbol).Float64()
	if err != nil {
		c.misses++
		return 0, false
	}
	c.hits++
	return val, true
}

// ── Stats ──────────────────────────────────────────────────────────────────────

// HitRate returns the cache hit rate as a percentage.
func (c *Cache) HitRate() float64 {
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total) * 100
}

// Close closes the Redis connection.
func (c *Cache) Close() error { return c.rdb.Close() }
