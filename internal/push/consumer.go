package push

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"
	"github.com/yjydist/go-im/internal/ws"
	"go.uber.org/zap"
)

// Consumer Kafka 消费者
type Consumer struct {
	reader *kafka.Reader
	pusher *Pusher
	logger *zap.Logger
}

// NewConsumer 创建 Kafka 消费者
func NewConsumer(brokers []string, topic, groupID string, pusher *Pusher, logger *zap.Logger) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		Topic:    topic,
		GroupID:  groupID,
		MinBytes: 1,
		MaxBytes: 10e6, // 10MB
	})

	return &Consumer{
		reader: reader,
		pusher: pusher,
		logger: logger,
	}
}

// Start 启动消费循环
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("kafka consumer started")

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("kafka consumer stopping")
			return
		default:
		}

		// 读取消息
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled
			}
			c.logger.Error("read kafka message failed", zap.Error(err))
			continue
		}

		// 反序列化消息
		var chatMsg ws.KafkaChatMsg
		if err := json.Unmarshal(msg.Value, &chatMsg); err != nil {
			c.logger.Error("unmarshal kafka message failed",
				zap.Error(err),
				zap.ByteString("value", msg.Value),
			)
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
			c.logger.Error("handle message failed",
				zap.String("msg_id", chatMsg.MsgID),
				zap.Error(err),
			)
		}
	}
}

// Close 关闭消费者
func (c *Consumer) Close() error {
	return c.reader.Close()
}
