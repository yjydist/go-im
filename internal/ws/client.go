package ws

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
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

// kafkaSendMsg 异步 Kafka 写入任务
type kafkaSendMsg struct {
	chatData ChatData
	kafkaMsg KafkaChatMsg
	msgBytes []byte
}

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
	// kafkaCh 异步 Kafka 写入通道
	kafkaCh chan kafkaSendMsg
	// redisRepo 用于 Redis 操作
	redisRepo repository.RedisRepository
	// groupRepo 用于群成员 DB 查询（Redis 缓存未命中时回退）
	groupRepo repository.GroupRepository
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
func NewClient(userID int64, conn *websocket.Conn, hub *Hub, kafkaWriter KafkaWriter, redisRepo repository.RedisRepository, groupRepo repository.GroupRepository, wsRPCAddr string, logger *zap.Logger) *Client {
	return &Client{
		UserID:      userID,
		conn:        conn,
		send:        make(chan []byte, sendChanSize),
		hub:         hub,
		logger:      logger,
		closeCh:     make(chan struct{}),
		kafkaWriter: kafkaWriter,
		kafkaCh:     make(chan kafkaSendMsg, sendChanSize),
		redisRepo:   redisRepo,
		groupRepo:   groupRepo,
		wsRPCAddr:   wsRPCAddr,
	}
}

// Start 启动客户端的读写 Goroutine
func (c *Client) Start() {
	go c.readPump()
	go c.writePump()
	go c.kafkaWorker()

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
		_ = c.conn.Close()
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
		// 清除在线状态（仅当值匹配本连接地址时才删除）
		ctx := context.Background()
		if err := c.redisRepo.DelOnline(ctx, c.UserID, c.wsRPCAddr); err != nil {
			c.logger.Error("delete online status failed",
				zap.Int64("user_id", c.UserID),
				zap.Error(err),
			)
		}
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
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
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
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
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
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

	// 输入校验
	if chatData.MsgID == "" {
		c.sendError(400, "msg_id is required")
		return
	}
	if chatData.ChatType != 1 && chatData.ChatType != 2 {
		c.sendError(400, "invalid chat_type, must be 1 or 2")
		return
	}
	if chatData.ToID <= 0 {
		c.sendError(400, "invalid to_id")
		return
	}
	if chatData.ContentType < 1 || chatData.ContentType > 3 {
		c.sendError(400, "invalid content_type, must be 1-3")
		return
	}
	if chatData.Content == "" {
		c.sendError(400, "content is required")
		return
	}
	if len(chatData.Content) > 2000 {
		c.sendError(400, "content too long, max 2000 characters")
		return
	}

	ctx := context.Background()

	// 群聊需要验证发送者是否为群成员
	if chatData.ChatType == 2 {
		isMember, cacheMiss, err := c.redisRepo.IsGroupMember(ctx, chatData.ToID, c.UserID)
		if err != nil {
			c.logger.Error("check group member failed",
				zap.Int64("user_id", c.UserID),
				zap.Int64("group_id", chatData.ToID),
				zap.Error(err),
			)
			c.sendError(500, "server error")
			return
		}
		if cacheMiss {
			// Redis 缓存未命中，回退到 DB 查询
			isMember, err = c.checkGroupMemberFromDB(ctx, chatData.ToID, c.UserID)
			if err != nil {
				c.logger.Error("check group member from DB failed",
					zap.Int64("user_id", c.UserID),
					zap.Int64("group_id", chatData.ToID),
					zap.Error(err),
				)
				c.sendError(500, "server error")
				return
			}
		}
		if !isMember {
			c.logger.Warn("non-member tried to send group message",
				zap.Int64("user_id", c.UserID),
				zap.Int64("group_id", chatData.ToID),
			)
			c.sendError(403, "not a group member")
			return
		}
	}

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

	// 异步发送到 Kafka（不阻塞 readPump）
	select {
	case c.kafkaCh <- kafkaSendMsg{chatData: chatData, kafkaMsg: kafkaMsg, msgBytes: msgBytes}:
		// 立即发送 ACK（at-most-once 语义，Kafka 失败时 dedup key 会回滚让客户端重试）
		c.sendAck(chatData.MsgID)
	default:
		c.logger.Warn("kafka send channel full",
			zap.String("msg_id", chatData.MsgID),
			zap.Int64("user_id", c.UserID),
		)
		// 回滚 dedup key
		if delErr := c.redisRepo.DelMsgDedup(ctx, chatData.MsgID); delErr != nil {
			c.logger.Error("rollback dedup key failed", zap.String("msg_id", chatData.MsgID), zap.Error(delErr))
		}
		c.sendError(500, "server busy")
	}

	c.logger.Debug("message queued for kafka",
		zap.String("msg_id", chatData.MsgID),
		zap.Int64("from_id", c.UserID),
		zap.Int64("to_id", chatData.ToID),
		zap.Int("chat_type", chatData.ChatType),
	)
}

// checkGroupMemberFromDB 从 DB 查询群成员资格，并回填 Redis 缓存
func (c *Client) checkGroupMemberFromDB(ctx context.Context, groupID, userID int64) (bool, error) {
	if c.groupRepo == nil {
		// groupRepo 未配置（不应该发生），拒绝放行以保安全
		c.logger.Warn("groupRepo is nil, denying group message",
			zap.Int64("user_id", userID),
			zap.Int64("group_id", groupID),
		)
		return false, nil
	}

	_, err := c.groupRepo.GetMember(ctx, groupID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil // 非群成员
		}
		return false, err // DB 查询出错
	}

	// 成员存在，异步回填 Redis 缓存（加载该群所有成员）
	go c.backfillGroupMembersCache(groupID)

	return true, nil
}

// backfillGroupMembersCache 异步回填群成员 Redis 缓存
func (c *Client) backfillGroupMembersCache(groupID int64) {
	if c.groupRepo == nil {
		return
	}
	ctx := context.Background()
	memberIDs, err := c.groupRepo.ListMemberIDs(ctx, groupID)
	if err != nil {
		c.logger.Error("backfill group members cache: list member IDs failed",
			zap.Int64("group_id", groupID),
			zap.Error(err),
		)
		return
	}
	if len(memberIDs) == 0 {
		return
	}
	if err := c.redisRepo.SetGroupMembers(ctx, groupID, memberIDs, 30*time.Minute); err != nil {
		c.logger.Error("backfill group members cache: set redis failed",
			zap.Int64("group_id", groupID),
			zap.Error(err),
		)
	}
}

// kafkaWorker 异步消费 kafkaCh 并写入 Kafka，避免阻塞 readPump
func (c *Client) kafkaWorker() {
	for {
		select {
		case task, ok := <-c.kafkaCh:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := c.kafkaWriter.WriteMessages(ctx, KafkaMessage{
				Key:   []byte(strconv.FormatInt(c.UserID, 10)),
				Value: task.msgBytes,
			})
			cancel()

			if err != nil {
				c.logger.Error("kafka write failed, rolling back dedup key",
					zap.String("msg_id", task.chatData.MsgID),
					zap.Int64("from_id", c.UserID),
					zap.Error(err),
				)
				// 回滚 dedup key，让客户端可以重试
				if delErr := c.redisRepo.DelMsgDedup(context.Background(), task.chatData.MsgID); delErr != nil {
					c.logger.Error("rollback dedup key failed",
						zap.String("msg_id", task.chatData.MsgID),
						zap.Error(delErr),
					)
				}
				continue
			}

			c.logger.Info("message written to kafka",
				zap.String("msg_id", task.chatData.MsgID),
				zap.Int64("from_id", c.UserID),
				zap.Int64("to_id", task.chatData.ToID),
				zap.Int("chat_type", task.chatData.ChatType),
			)

		case <-c.closeCh:
			return
		}
	}
}

// sendAck 发送 ACK 确认
func (c *Client) sendAck(msgID string) {
	msg := ServerMsg{
		Type: "ack",
		Data: AckData{MsgID: msgID},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("marshal ack msg failed", zap.Error(err))
		return
	}
	c.Send(data)
}

// sendPong 发送 Pong 响应
func (c *Client) sendPong() {
	msg := ServerMsg{
		Type: "pong",
		Data: nil,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("marshal pong msg failed", zap.Error(err))
		return
	}
	c.Send(data)
}

// sendError 发送错误消息
func (c *Client) sendError(code int, errMsg string) {
	msg := ServerMsg{
		Type: "error",
		Data: ErrorData{Code: code, Msg: errMsg},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("marshal error msg failed", zap.Error(err))
		return
	}
	c.Send(data)
}
