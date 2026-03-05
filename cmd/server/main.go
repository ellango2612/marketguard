package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/yourusername/marketguard/internal/auth"
	"github.com/yourusername/marketguard/internal/cache"
	"github.com/yourusername/marketguard/internal/db"
	"github.com/yourusername/marketguard/internal/engine"
	"github.com/yourusername/marketguard/internal/kafka"
	"github.com/yourusername/marketguard/internal/models"
)

func main() {
	// ── Logger ──────────────────────────────────────────────────────────────────
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// ── Config ───────────────────────────────────────────────────────────────────
	_ = godotenv.Load()
	cfg := loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Dependencies ─────────────────────────────────────────────────────────────
	redisCache, err := cache.New(cfg.RedisAddr, cfg.RedisPassword, 0, logger)
	if err != nil {
		logger.Fatal("failed to connect to redis", zap.Error(err))
	}
	defer redisCache.Close()

	database, err := db.New(ctx, cfg.PostgresDSN, logger)
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer database.Close()

	// ── Kafka consumer ───────────────────────────────────────────────────────────
	consumer := kafka.NewConsumer(cfg.KafkaBrokers, cfg.KafkaTopicTx, "marketguard-group", logger)
	defer consumer.Close()

	producer := kafka.NewProducer(cfg.KafkaBrokers, cfg.KafkaTopicAlerts, logger)
	defer producer.Close()

	// ── Risk Engine ──────────────────────────────────────────────────────────────
	riskEngine := engine.New(cfg.Workers, logger)
	riskEngine.Start(ctx)

	// Pipe Kafka → engine
	go func() {
		for tx := range consumer.Read(ctx) {
			riskEngine.Submit(tx)
		}
	}()

	// Pipe engine alerts → Kafka + Redis + PostgreSQL
	go func() {
		for alert := range riskEngine.Alerts() {
			if err := redisCache.PushAlert(ctx, alert); err != nil {
				logger.Warn("redis push failed", zap.Error(err))
			}
			if err := database.InsertAlert(ctx, alert); err != nil {
				logger.Warn("db insert failed", zap.Error(err))
			}
			if err := producer.PublishAlert(ctx, alert); err != nil {
				logger.Warn("kafka publish failed", zap.Error(err))
			}
		}
	}()

	// ── HTTP Server (Gin + Swagger) ───────────────────────────────────────────────
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	// Public routes
	r.GET("/health", healthHandler)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	r.POST("/auth/login", loginHandler(database, cfg.JWTSecret))

	// Protected routes
	api := r.Group("/api/v1", auth.Middleware(cfg.JWTSecret))
	{
		api.GET("/alerts", listAlertsHandler(database, redisCache))
		api.PATCH("/alerts/:id/status", updateAlertHandler(database))
		api.GET("/metrics/system", systemMetricsHandler(riskEngine, redisCache))
		api.GET("/metrics/severity", severityMetricsHandler(database))
	}

	// Admin-only
	admin := api.Group("/admin", auth.RequireRole("ADMIN"))
	{
		admin.GET("/users", func(c *gin.Context) { c.JSON(200, gin.H{"message": "user management"}) })
	}

	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: r}

	go func() {
		logger.Info("HTTP server started", zap.String("port", cfg.HTTPPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down...")
	cancel()
	riskEngine.Stop()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	logger.Info("shutdown complete")
}

// ── Handlers ───────────────────────────────────────────────────────────────────

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().UTC()})
}

func loginHandler(database *db.DB, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		user, err := database.GetUserByUsername(c.Request.Context(), req.Username)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		// In production: bcrypt.CompareHashAndPassword
		_ = user
		token, err := auth.GenerateToken(user.ID, user.Username, user.Role, secret, 24*time.Hour)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token, "role": user.Role})
	}
}

func listAlertsHandler(database *db.DB, redisCache *cache.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Try cache first (hot path)
		if alerts, ok := redisCache.GetRecentAlerts(c.Request.Context(), 100); ok {
			c.JSON(http.StatusOK, gin.H{"alerts": alerts, "source": "cache"})
			return
		}
		// Fall through to DB
		filter := db.AlertFilter{
			Symbol:   c.Query("symbol"),
			Severity: c.Query("severity"),
			Status:   c.Query("status"),
			Limit:    100,
		}
		alerts, err := database.ListAlerts(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"alerts": alerts, "source": "db"})
	}
}

func updateAlertHandler(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct{ Status string `json:"status" binding:"required"` }
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := database.UpdateAlertStatus(c.Request.Context(), c.Param("id"), req.Status); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"updated": true})
	}
}

func systemMetricsHandler(e *engine.RiskEngine, redisCache *cache.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		if m, ok := redisCache.GetMetrics(c.Request.Context()); ok {
			c.JSON(http.StatusOK, m)
			return
		}
		processed, flagged, avgLat := e.Metrics()
		m := models.SystemMetrics{
			TPS:           float64(processed) / 60,
			AvgLatencyMs:  avgLat,
			CacheHitRate:  redisCache.HitRate(),
			ActiveWorkers: 48 - e.QueueDepth()/100,
			QueueDepth:    e.QueueDepth(),
			Uptime:        99.9,
			SnapshotAt:    time.Now().UTC(),
			EventsPerHour: flagged * 60,
		}
		_ = redisCache.SetMetrics(c.Request.Context(), m)
		c.JSON(http.StatusOK, m)
	}
}

func severityMetricsHandler(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		counts, err := database.CountBySeverity(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, counts)
	}
}

// ── Config ─────────────────────────────────────────────────────────────────────

type config struct {
	HTTPPort         string
	PostgresDSN      string
	RedisAddr        string
	RedisPassword    string
	KafkaBrokers     []string
	KafkaTopicTx     string
	KafkaTopicAlerts string
	JWTSecret        string
	Workers          int
}

func loadConfig() config {
	workers, _ := strconv.Atoi(getEnv("WORKERS", "48"))
	return config{
		HTTPPort:         getEnv("HTTP_PORT", "8080"),
		PostgresDSN:      getEnv("POSTGRES_DSN", "postgres://marketguard:password@localhost:5432/marketguard?sslmode=disable"),
		RedisAddr:        getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:    getEnv("REDIS_PASSWORD", ""),
		KafkaBrokers:     []string{getEnv("KAFKA_BROKER", "localhost:9092")},
		KafkaTopicTx:     getEnv("KAFKA_TOPIC_TX", "transactions"),
		KafkaTopicAlerts: getEnv("KAFKA_TOPIC_ALERTS", "alerts"),
		JWTSecret:        getEnv("JWT_SECRET", "change-me-in-production"),
		Workers:          workers,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
