package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/pkg/snowflake"
	"github.com/yjydist/go-im/internal/repository"
	"github.com/yjydist/go-im/internal/ws"
	"go.uber.org/zap"
)

const (
	// 默认值
	defaultPushTimeout          = 3 * time.Second
	defaultGroupMemberCacheTTL  = 1 * time.Hour
	defaultGroupPushConcurrency = 20
)

// PusherConfig 推送服务可配置参数
type PusherConfig struct {
	PushTimeout          time.Duration
	GroupMemberCacheTTL  time.Duration
	GroupPushConcurrency int
}

func (c PusherConfig) pushTimeout() time.Duration {
	if c.PushTimeout > 0 {
		return c.PushTimeout
	}
	return defaultPushTimeout
}

func (c PusherConfig) groupMemberCacheTTL() time.Duration {
	if c.GroupMemberCacheTTL > 0 {
		return c.GroupMemberCacheTTL
	}
	return defaultGroupMemberCacheTTL
}

func (c PusherConfig) groupPushConcurrency() int {
	if c.GroupPushConcurrency > 0 {
		return c.GroupPushConcurrency
	}
	return defaultGroupPushConcurrency
}

// Pusher 消息路由与推送
type Pusher struct {
	messageRepo    repository.MessageRepository
	groupRepo      repository.GroupRepository
	redisRepo      repository.RedisRepository
	httpClient     *http.Client
	internalAPIKey string
	cfg            PusherConfig
	logger         *zap.Logger
}

// NewPusher 创建 Pusher
func NewPusher(
	messageRepo repository.MessageRepository,
	groupRepo repository.GroupRepository,
	redisRepo repository.RedisRepository,
	internalAPIKey string,
	cfg PusherConfig,
	logger *zap.Logger,
) *Pusher {
	return &Pusher{
		messageRepo:    messageRepo,
		groupRepo:      groupRepo,
		redisRepo:      redisRepo,
		internalAPIKey: internalAPIKey,
		cfg:            cfg,
		httpClient: &http.Client{
			Timeout: cfg.pushTimeout(),
		},
		logger: logger,
	}
}

// HandleMessage 处理从 Kafka 消费到的消息
func (p *Pusher) HandleMessage(ctx context.Context, chatMsg *ws.KafkaChatMsg) error {
	// 1. 入库持久化
	msgModel := &model.Message{
		ID:          snowflake.GenID(),
		MsgID:       chatMsg.MsgID,
		FromID:      chatMsg.FromID,
		ToID:        chatMsg.ToID,
		ChatType:    int8(chatMsg.ChatType),
		ContentType: int8(chatMsg.ContentType),
		Content:     chatMsg.Content,
	}

	if err := p.messageRepo.Create(ctx, msgModel); err != nil {
		return fmt.Errorf("persist message failed: %w", err)
	}

	p.logger.Info("message persisted",
		zap.String("msg_id", chatMsg.MsgID),
		zap.Int64("db_id", msgModel.ID),
	)

	// 2. 路由推送
	switch chatMsg.ChatType {
	case 1: // 单聊
		return p.pushToUser(ctx, chatMsg.ToID, chatMsg.FromID, msgModel)
	case 2: // 群聊
		return p.pushToGroup(ctx, chatMsg.ToID, chatMsg.FromID, msgModel)
	default:
		p.logger.Warn("unknown chat type",
			zap.String("msg_id", chatMsg.MsgID),
			zap.Int("chat_type", chatMsg.ChatType),
		)
		return nil
	}
}

// pushToUser 单聊推送：检查在线状态，在线则推送，离线则写离线表
func (p *Pusher) pushToUser(ctx context.Context, toID, fromID int64, msg *model.Message) error {
	// 查询目标用户在线状态
	wsAddr, err := p.redisRepo.GetOnline(ctx, toID)
	if err != nil {
		return fmt.Errorf("get online status failed: %w", err)
	}

	if wsAddr != "" {
		// 用户在线，向 WS 网关发起内部 HTTP 推送
		if err := p.sendPush(ctx, wsAddr, toID, msg); err != nil {
			p.logger.Warn("push to online user failed, saving as offline",
				zap.Int64("user_id", toID),
				zap.Error(err),
			)
			// 推送失败，降级为离线存储
			return p.saveOffline(ctx, toID, msg.ID)
		}
		p.logger.Debug("pushed to online user",
			zap.Int64("user_id", toID),
			zap.String("ws_addr", wsAddr),
		)
		return nil
	}

	// 用户离线，写入离线消息表
	return p.saveOffline(ctx, toID, msg.ID)
}

// pushToGroup 群聊推送：获取群成员，逐个推送
func (p *Pusher) pushToGroup(ctx context.Context, groupID, fromID int64, msg *model.Message) error {
	// 先尝试从 Redis 获取群成员
	memberIDs, err := p.redisRepo.GetGroupMembers(ctx, groupID)
	if err != nil {
		p.logger.Error("get group members from redis failed",
			zap.Int64("group_id", groupID),
			zap.Error(err),
		)
	}

	// Redis 缓存未命中，从 MySQL 查询并回填
	if len(memberIDs) == 0 {
		memberIDs, err = p.groupRepo.ListMemberIDs(ctx, groupID)
		if err != nil {
			return fmt.Errorf("get group member ids failed: %w", err)
		}

		// 回填 Redis 缓存
		if len(memberIDs) > 0 {
			if err := p.redisRepo.SetGroupMembers(ctx, groupID, memberIDs, p.cfg.groupMemberCacheTTL()); err != nil {
				p.logger.Warn("set group members cache failed",
					zap.Int64("group_id", groupID),
					zap.Error(err),
				)
			}
		}
	}

	// 并发推送给群成员（排除发送者），限制并发度
	sem := make(chan struct{}, p.cfg.groupPushConcurrency())
	var wg sync.WaitGroup
	for _, memberID := range memberIDs {
		if memberID == fromID {
			continue // 跳过发送者自己
		}

		wg.Add(1)
		sem <- struct{}{} // 获取信号量
		go func(mid int64) {
			defer wg.Done()
			defer func() { <-sem }() // 释放信号量

			if err := p.pushToUser(ctx, mid, fromID, msg); err != nil {
				p.logger.Error("push to group member failed",
					zap.Int64("group_id", groupID),
					zap.Int64("member_id", mid),
					zap.String("msg_id", msg.MsgID),
					zap.Error(err),
				)
			}
		}(memberID)
	}
	wg.Wait()

	return nil
}

// sendPush 向 WS 网关发起内部 HTTP 推送
func (p *Pusher) sendPush(ctx context.Context, wsAddr string, userID int64, msg *model.Message) error {
	pushMsg := ws.PushMsg{
		UserID: userID,
		Data:   msg,
	}

	body, err := json.Marshal(pushMsg)
	if err != nil {
		return fmt.Errorf("marshal push msg failed: %w", err)
	}

	url := fmt.Sprintf("http://%s/internal/push", wsAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create push request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.internalAPIKey != "" {
		req.Header.Set("X-API-Key", p.internalAPIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send push request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push request returned status %d", resp.StatusCode)
	}

	return nil
}

// saveOffline 保存离线消息
func (p *Pusher) saveOffline(ctx context.Context, userID, messageID int64) error {
	offline := &model.OfflineMessage{
		UserID:    userID,
		MessageID: messageID,
	}

	if err := p.messageRepo.CreateOffline(ctx, offline); err != nil {
		return fmt.Errorf("save offline message failed: %w", err)
	}

	p.logger.Debug("saved offline message",
		zap.Int64("user_id", userID),
		zap.Int64("message_id", messageID),
	)

	return nil
}
