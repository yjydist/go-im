package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
)

const (
	// 写入超时时间
	writeWait = 10 * time.Second
	// 读取 Pong 超时时间
	pongWait = 60 * time.Second
	// Ping 间隔（必须小于 pongWait）
	pingPeriod = 54 * time.Second
	// 最大消息大小
	maxMessageSize = 4096
	// 发送通道缓冲大小
	sendChanSize = 256
	// 在线状态 TTL
	onlineTTL = 120 * time.Second
	// 心跳刷新在线状态间隔
	heartbeatInterval = 60 * time.Second
)

// Client 单个长连接对象
type Client struct {
	UserID int64
	conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
	logger *zap.Logger

	closeCh   chan struct{}
	closeOnce sync.Once

	// kafkaWriter 用于写入 Kafka
	kafkaWriter KafkaWriter
	// redisRepo 用于 Redis 操作
	redisRepo repository.RedisRepository
	// wsRPCAddr WS 网关的内部 RPC 地址
	wsRPCAddr string
}

// KafkaWriter Kafka 写入接口
type KafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...KafkaMessage) error
}

// KafkaMessage Kafka 消息
type KafkaMessage struct {
	Key   []byte
	Value []byte
}

// NewClient 创建客户端连接
func NewClient(userID int64, conn *websocket.Conn, hub *Hub, kafkaWriter KafkaWriter, redisRepo repository.RedisRepository, wsRPCAddr string, logger *zap.Logger) *Client {
	return &Client{
		UserID:      userID,
		conn:        conn,
		send:        make(chan []byte, sendChanSize),
		hub:         hub,
		logger:      logger,
		closeCh:     make(chan struct{}),
		kafkaWriter: kafkaWriter,
		redisRepo:   redisRepo,
		wsRPCAddr:   wsRPCAddr,
	}
}

// Start 启动客户端的读写 Goroutine
func (c *Client) Start() {
	go c.readPump()
	go c.writePump()

	// 注册在线状态
	ctx := context.Background()
	if err := c.redisRepo.SetOnline(ctx, c.UserID, c.wsRPCAddr, onlineTTL); err != nil {
		c.logger.Error("set online status failed",
			zap.Int64("user_id", c.UserID),
			zap.Error(err),
		)
	}
}

// Close 关闭连接
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		c.conn.Close()
	})
}

// Send 向客户端发送消息
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		c.logger.Warn("client send channel full, closing",
			zap.Int64("user_id", c.UserID),
		)
		c.hub.Unregister(c)
		c.Close()
	}
}

// readPump 循环读取客户端消息
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.Close()
		// 清除在线状态
		ctx := context.Background()
		if err := c.redisRepo.DelOnline(ctx, c.UserID); err != nil {
			c.logger.Error("delete online status failed",
				zap.Int64("user_id", c.UserID),
				zap.Error(err),
			)
		}
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Error("unexpected close",
					zap.Int64("user_id", c.UserID),
					zap.Error(err),
				)
			}
			return
		}

		c.handleMessage(message)
	}
}

// writePump 从发送通道读取消息发给客户端
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	heartbeat := time.NewTicker(heartbeatInterval)
	defer func() {
		ticker.Stop()
		heartbeat.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				c.logger.Error("write message failed",
					zap.Int64("user_id", c.UserID),
					zap.Error(err),
				)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-heartbeat.C:
			// 定期刷新在线状态
			ctx := context.Background()
			if err := c.redisRepo.SetOnline(ctx, c.UserID, c.wsRPCAddr, onlineTTL); err != nil {
				c.logger.Error("refresh online status failed",
					zap.Int64("user_id", c.UserID),
					zap.Error(err),
				)
			}

		case <-c.closeCh:
			return
		}
	}
}

// handleMessage 处理客户端上行消息
func (c *Client) handleMessage(data []byte) {
	var msg ClientMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendError(400, "invalid message format")
		return
	}

	switch msg.Type {
	case "chat":
		c.handleChat(msg.Data)
	case "ping":
		c.sendPong()
	default:
		c.sendError(400, "unknown message type")
	}
}

// handleChat 处理聊天消息
func (c *Client) handleChat(data json.RawMessage) {
	var chatData ChatData
	if err := json.Unmarshal(data, &chatData); err != nil {
		c.sendError(400, "invalid chat data")
		return
	}

	ctx := context.Background()

	// 幂等校验：SETNX msg_dedup:{msg_id}
	isNew, err := c.redisRepo.SetMsgDedup(ctx, chatData.MsgID, 5*time.Minute)
	if err != nil {
		c.logger.Error("msg dedup check failed",
			zap.String("msg_id", chatData.MsgID),
			zap.Error(err),
		)
		c.sendError(500, "server error")
		return
	}
	if !isNew {
		// 重复消息，直接回 ACK
		c.sendAck(chatData.MsgID)
		return
	}

	// 封装 Kafka 消息
	kafkaMsg := KafkaChatMsg{
		MsgID:       chatData.MsgID,
		FromID:      c.UserID,
		ToID:        chatData.ToID,
		ChatType:    chatData.ChatType,
		ContentType: chatData.ContentType,
		Content:     chatData.Content,
		Timestamp:   time.Now().UnixMilli(),
	}

	msgBytes, err := json.Marshal(kafkaMsg)
	if err != nil {
		c.logger.Error("marshal kafka msg failed", zap.Error(err))
		c.sendError(500, "server error")
		return
	}

	// 写入 Kafka
	if err := c.kafkaWriter.WriteMessages(ctx, KafkaMessage{
		Key:   []byte(fmt.Sprintf("%d", c.UserID)),
		Value: msgBytes,
	}); err != nil {
		c.logger.Error("write kafka failed",
			zap.String("msg_id", chatData.MsgID),
			zap.Int64("user_id", c.UserID),
			zap.Error(err),
		)
		c.sendError(500, "message send failed")
		return
	}

	c.logger.Info("message sent to kafka",
		zap.String("msg_id", chatData.MsgID),
		zap.Int64("from_id", c.UserID),
		zap.Int64("to_id", chatData.ToID),
		zap.Int("chat_type", chatData.ChatType),
	)

	// 发送 ACK
	c.sendAck(chatData.MsgID)
}

// sendAck 发送 ACK 确认
func (c *Client) sendAck(msgID string) {
	msg := ServerMsg{
		Type: "ack",
		Data: AckData{MsgID: msgID},
	}
	data, _ := json.Marshal(msg)
	c.Send(data)
}

// sendPong 发送 Pong 响应
func (c *Client) sendPong() {
	msg := ServerMsg{
		Type: "pong",
		Data: nil,
	}
	data, _ := json.Marshal(msg)
	c.Send(data)
}

// sendError 发送错误消息
func (c *Client) sendError(code int, errMsg string) {
	msg := ServerMsg{
		Type: "error",
		Data: ErrorData{Code: code, Msg: errMsg},
	}
	data, _ := json.Marshal(msg)
	c.Send(data)
}
