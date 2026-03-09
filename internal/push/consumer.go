package push

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/yjydist/go-im/internal/ws"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Consumer Kafka 消费者
type Consumer struct {
	reader *kafka.Reader
	cfg    kafka.ReaderConfig
	pusher *Pusher
	logger *zap.Logger
}

// NewConsumer 创建 Kafka 消费者（Reader 延迟到 Start 中创建，确保 broker 就绪）
func NewConsumer(brokers []string, topic, groupID string, maxWait time.Duration, maxBytes int64, startOffset int64, pusher *Pusher, logger *zap.Logger) *Consumer {
	if maxWait <= 0 {
		maxWait = 3 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 10e6 // 10MB
	}
	// startOffset: -1 = newest (kafka.LastOffset), -2 = oldest (kafka.FirstOffset)
	if startOffset != kafka.FirstOffset && startOffset != kafka.LastOffset {
		startOffset = kafka.LastOffset
	}

	cfg := kafka.ReaderConfig{
		Brokers:     brokers,
		Topic:       topic,
		GroupID:     groupID,
		MinBytes:    1,
		MaxBytes:    int(maxBytes),
		MaxWait:     maxWait,
		StartOffset: startOffset,
		Logger:      kafka.LoggerFunc(newKafkaLogFunc(logger, zap.DebugLevel)),
		ErrorLogger: kafka.LoggerFunc(newKafkaLogFunc(logger, zap.ErrorLevel)),
	}

	return &Consumer{
		cfg:    cfg,
		pusher: pusher,
		logger: logger,
	}
}

// Start 启动消费循环
func (c *Consumer) Start(ctx context.Context) {
	// 等待 Kafka consumer group coordinator 就绪后再创建 Reader，
	// 避免 kafka-go 在 coordinator 不可用时永久阻塞在 ReadMessage 内部。
	c.waitForKafka(ctx)
	if ctx.Err() != nil {
		return
	}

	c.reader = kafka.NewReader(c.cfg)
	c.logger.Info("kafka consumer started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("kafka consumer stopping")
			return
		default:
		}

		// 使用 FetchMessage 而非 ReadMessage，避免自动提交 offset。
		// 只有消息处理成功后才手动提交，防止处理失败时丢失消息。
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled
			}
			c.logger.Error("fetch kafka message failed", zap.Error(err))
			continue
		}

		// 反序列化消息
		var chatMsg ws.KafkaChatMsg
		if err := json.Unmarshal(msg.Value, &chatMsg); err != nil {
			c.logger.Error("unmarshal kafka message failed",
				zap.Error(err),
				zap.ByteString("value", msg.Value),
			)
			// 格式错误的消息无法恢复，提交 offset 跳过
			if commitErr := c.reader.CommitMessages(ctx, msg); commitErr != nil {
				c.logger.Error("commit skipped message failed", zap.Error(commitErr))
			}
			continue
		}

		c.logger.Info("received kafka message",
			zap.String("msg_id", chatMsg.MsgID),
			zap.Int64("from_id", chatMsg.FromID),
			zap.Int64("to_id", chatMsg.ToID),
			zap.Int("chat_type", chatMsg.ChatType),
		)

		// 交给 Pusher 处理
		if err := c.pusher.HandleMessage(ctx, &chatMsg); err != nil {
			c.logger.Error("handle message failed, will retry on next consume",
				zap.String("msg_id", chatMsg.MsgID),
				zap.Error(err),
			)
			// 处理失败不提交 offset，下次消费会重试
			continue
		}

		// 处理成功，提交 offset
		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			c.logger.Error("commit message failed",
				zap.String("msg_id", chatMsg.MsgID),
				zap.Error(err),
			)
		}
	}
}

// waitForKafka 等待 Kafka consumer group coordinator 就绪。
// 通过 kafka.Dial 连接 broker 并尝试获取 topic 分区信息来判断 broker 是否完全初始化。
// 由于 __consumer_offsets topic 在首次 consumer group 请求时才创建，
// 这里额外等待一个短暂的初始化窗口以确保 coordinator 就绪。
func (c *Consumer) waitForKafka(ctx context.Context) {
	topic := c.cfg.Topic
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		for _, broker := range c.cfg.Brokers {
			conn, err := kafka.DialContext(ctx, "tcp", broker)
			if err != nil {
				continue
			}
			// 尝试读取目标 topic 的分区信息，确认 broker 完全初始化
			partitions, err := conn.ReadPartitions(topic)
			conn.Close()
			if err == nil && len(partitions) > 0 {
				c.logger.Info("kafka broker is ready",
					zap.String("broker", broker),
					zap.String("topic", topic),
					zap.Int("partitions", len(partitions)),
				)
				// 额外等待 3 秒让 __consumer_offsets 完成初始化
				// （首次 consumer group 注册会触发该 topic 创建）
				c.logger.Info("waiting 3s for consumer group coordinator initialization...")
				select {
				case <-ctx.Done():
					return
				case <-time.After(3 * time.Second):
				}
				return
			}
			if err != nil {
				c.logger.Debug("kafka topic not ready yet", zap.String("broker", broker), zap.Error(err))
			}
		}
		c.logger.Warn("waiting for kafka topic to become available, retrying in 2s...", zap.String("topic", topic))
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// Close 关闭消费者
func (c *Consumer) Close() error {
	if c.reader != nil {
		return c.reader.Close()
	}
	return nil
}

// newKafkaLogFunc 将 zap.Logger 适配为 kafka-go 的 LoggerFunc 签名。
// kafka-go 的 Logger/ErrorLogger 使用 Printf 风格: func(string, ...interface{})
func newKafkaLogFunc(logger *zap.Logger, level zapcore.Level) func(string, ...interface{}) {
	sugar := logger.WithOptions(zap.AddCallerSkip(2)).Sugar()
	switch level {
	case zap.DebugLevel:
		return sugar.Debugf
	case zap.ErrorLevel:
		return sugar.Errorf
	case zap.WarnLevel:
		return sugar.Warnf
	default:
		return sugar.Infof
	}
}
