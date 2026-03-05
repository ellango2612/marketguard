package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/ellango2612/marketguard/internal/models"
)

// DB wraps a pgxpool connection pool with domain-specific query methods.
type DB struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// New opens a PostgreSQL connection pool and runs migrations.
func New(ctx context.Context, dsn string, logger *zap.Logger) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db config error: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db connect error: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("db ping error: %w", err)
	}
	d := &DB{pool: pool, logger: logger}
	if err := d.migrate(ctx); err != nil {
		return nil, fmt.Errorf("migration error: %w", err)
	}
	logger.Info("postgres connected")
	return d, nil
}

// ── Migrations ─────────────────────────────────────────────────────────────────

func (d *DB) migrate(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS alerts (
			id              TEXT PRIMARY KEY,
			transaction_id  TEXT NOT NULL,
			symbol          TEXT NOT NULL,
			trader_id       TEXT NOT NULL,
			risk_type       TEXT NOT NULL,
			severity        TEXT NOT NULL,
			score           NUMERIC(6,2) NOT NULL,
			latency_ms      BIGINT NOT NULL DEFAULT 0,
			detected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			status          TEXT NOT NULL DEFAULT 'FLAGGED',
			metadata        JSONB
		);

		CREATE INDEX IF NOT EXISTS idx_alerts_symbol     ON alerts(symbol);
		CREATE INDEX IF NOT EXISTS idx_alerts_severity   ON alerts(severity);
		CREATE INDEX IF NOT EXISTS idx_alerts_detected_at ON alerts(detected_at DESC);
		CREATE INDEX IF NOT EXISTS idx_alerts_trader     ON alerts(trader_id);

		CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			username      TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			role          TEXT NOT NULL DEFAULT 'ANALYST',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

// ── Alerts ─────────────────────────────────────────────────────────────────────

// InsertAlert persists a new alert. Skips on conflict (idempotent).
func (d *DB) InsertAlert(ctx context.Context, a models.Alert) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO alerts (id, transaction_id, symbol, trader_id, risk_type, severity, score, latency_ms, detected_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING`,
		a.ID, a.TransactionID, a.Symbol, a.TraderID,
		string(a.RiskType), string(a.Severity), a.Score,
		a.LatencyMs, a.DetectedAt, a.Status,
	)
	return err
}

// AlertFilter defines query parameters for listing alerts.
type AlertFilter struct {
	Symbol   string
	Severity string
	Status   string
	From     time.Time
	To       time.Time
	Limit    int
	Offset   int
}

// ListAlerts fetches alerts matching the filter — optimised with indexed columns.
func (d *DB) ListAlerts(ctx context.Context, f AlertFilter) ([]models.Alert, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := d.pool.Query(ctx, `
		SELECT id, transaction_id, symbol, trader_id, risk_type, severity, score, latency_ms, detected_at, status
		FROM alerts
		WHERE ($1 = '' OR symbol = $1)
		  AND ($2 = '' OR severity = $2)
		  AND ($3 = '' OR status = $3)
		  AND ($4::timestamptz IS NULL OR detected_at >= $4)
		  AND ($5::timestamptz IS NULL OR detected_at <= $5)
		ORDER BY detected_at DESC
		LIMIT $6 OFFSET $7`,
		f.Symbol, f.Severity, f.Status,
		nilTime(f.From), nilTime(f.To),
		f.Limit, f.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []models.Alert
	for rows.Next() {
		var a models.Alert
		if err := rows.Scan(&a.ID, &a.TransactionID, &a.Symbol, &a.TraderID,
			&a.RiskType, &a.Severity, &a.Score, &a.LatencyMs, &a.DetectedAt, &a.Status); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// UpdateAlertStatus changes a single alert's status (e.g. REVIEWED, DISMISSED).
func (d *DB) UpdateAlertStatus(ctx context.Context, id, status string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE alerts SET status = $1 WHERE id = $2`, status, id)
	return err
}

// CountBySeverity returns alert counts grouped by severity — used for dashboard KPIs.
func (d *DB) CountBySeverity(ctx context.Context) (map[string]int64, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT severity, COUNT(*) FROM alerts GROUP BY severity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var sev string
		var cnt int64
		if err := rows.Scan(&sev, &cnt); err != nil {
			return nil, err
		}
		result[sev] = cnt
	}
	return result, rows.Err()
}

// ── Users ──────────────────────────────────────────────────────────────────────

// GetUserByUsername loads a user for login.
func (d *DB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var u models.User
	err := d.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, role, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Close releases the connection pool.
func (d *DB) Close() { d.pool.Close() }

// nilTime returns nil if t is zero, used for optional timestamp filter params.
func nilTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
