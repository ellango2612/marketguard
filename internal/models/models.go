package models

import "time"

type Severity string

const (
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

type RiskType string

const (
	RiskSpoofing        RiskType = "SPOOFING"
	RiskWashTrade       RiskType = "WASH_TRADE"
	RiskLayering        RiskType = "LAYERING"
	RiskMomentumIgn     RiskType = "MOMENTUM_IGNITION"
	RiskCrossVenue      RiskType = "CROSS_VENUE_MANIP"
)

// Transaction represents an inbound market event from Kafka.
type Transaction struct {
	ID        string    `json:"id"`
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	Volume    int64     `json:"volume"`
	Side      string    `json:"side"` // BUY | SELL
	TraderID  string    `json:"trader_id"`
	VenueID   string    `json:"venue_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Alert is emitted by the risk engine when a violation is detected.
type Alert struct {
	ID          string    `json:"id"          db:"id"`
	TransactionID string  `json:"transaction_id" db:"transaction_id"`
	Symbol      string    `json:"symbol"      db:"symbol"`
	TraderID    string    `json:"trader_id"   db:"trader_id"`
	RiskType    RiskType  `json:"risk_type"   db:"risk_type"`
	Severity    Severity  `json:"severity"    db:"severity"`
	Score       float64   `json:"score"       db:"score"`
	LatencyMs   int64     `json:"latency_ms"  db:"latency_ms"`
	DetectedAt  time.Time `json:"detected_at" db:"detected_at"`
	Status      string    `json:"status"      db:"status"` // FLAGGED | REVIEWED | DISMISSED
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// User represents an analyst account.
type User struct {
	ID           string    `json:"id"         db:"id"`
	Username     string    `json:"username"   db:"username"`
	PasswordHash string    `json:"-"          db:"password_hash"`
	Role         string    `json:"role"       db:"role"` // ADMIN | ANALYST | VIEWER
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// SystemMetrics is a snapshot of runtime health.
type SystemMetrics struct {
	TPS           float64   `json:"tps"`
	AvgLatencyMs  float64   `json:"avg_latency_ms"`
	CacheHitRate  float64   `json:"cache_hit_rate"`
	EventsPerHour int64     `json:"events_per_hour"`
	ActiveWorkers int       `json:"active_workers"`
	QueueDepth    int       `json:"queue_depth"`
	Uptime        float64   `json:"uptime_pct"`
	SnapshotAt    time.Time `json:"snapshot_at"`
}
