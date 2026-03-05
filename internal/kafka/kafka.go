package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/ellango2612/marketguard/internal/models"
)

const (
	defaultBatchSize    = 100
	defaultBatchTimeout = 50 * time.Millisecond
	defaultMaxBytes     = 10 << 20 // 10 MB
)

// Consumer reads transactions from a Kafka topic.
type Consumer struct {
	reader *kafka.Reader
	logger *zap.Logger
}

// NewConsumer creates a Kafka consumer for the given broker and topic.
func NewConsumer(brokers []string, topic, groupID string, logger *zap.Logger) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:         brokers,
		Topic:           topic,
		GroupID:         groupID,
		MinBytes:        1,
		MaxBytes:        defaultMaxBytes,
		MaxWait:         100 * time.Millisecond,
		ReadLagInterval: 30 * time.Second,
		Logger:          kafka.LoggerFunc(func(msg string, args ...interface{}) { logger.Sugar().Debugf(msg, args...) }),
	})
	return &Consumer{reader: r, logger: logger}
}

// Read returns a channel that streams decoded transactions.
// The channel is closed when ctx is cancelled.
func (c *Consumer) Read(ctx context.Context) <-chan models.Transaction {
	ch := make(chan models.Transaction, 1000)
	go func() {
		defer close(ch)
		for {
			m, err := c.reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				c.logger.Error("kafka read error", zap.Error(err))
				time.Sleep(500 * time.Millisecond)
				continue
			}
			var tx models.Transaction
			if err := json.Unmarshal(m.Value, &tx); err != nil {
				c.logger.Warn("failed to unmarshal transaction", zap.Error(err), zap.ByteString("raw", m.Value))
				continue
			}
			select {
			case ch <- tx:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// Close shuts down the consumer.
func (c *Consumer) Close() error { return c.reader.Close() }

// Lag returns the consumer's current lag (unread messages).
func (c *Consumer) Lag() int64 { return c.reader.Stats().Lag }

// ── Producer ───────────────────────────────────────────────────────────────────

// Producer writes alerts back to a Kafka topic for downstream consumers.
type Producer struct {
	writer *kafka.Writer
	logger *zap.Logger
}

// NewProducer creates a Kafka producer for the given broker and topic.
func NewProducer(brokers []string, topic string, logger *zap.Logger) *Producer {
	w := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    defaultBatchSize,
		BatchTimeout: defaultBatchTimeout,
		Async:        true, // fire-and-forget for throughput
	}
	return &Producer{writer: w, logger: logger}
}

// PublishAlert serialises and sends an alert to Kafka.
func (p *Producer) PublishAlert(ctx context.Context, alert models.Alert) error {
	payload, err := json.Marshal(alert)
	if err != nil {
		return err
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(alert.Symbol),
		Value: payload,
	})
}

// Close flushes and closes the producer.
func (p *Producer) Close() error { return p.writer.Close() }
