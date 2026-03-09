package service

import (
	"context"

	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
)

// MessageService 消息服务接口
type MessageService interface {
	GetOfflineMessages(ctx context.Context, userID int64) ([]model.Message, error)
	GetHistory(ctx context.Context, userID, targetID int64, chatType int8, cursorMsgID int64, limit int) ([]model.Message, error)
}

type messageService struct {
	msgRepo repository.MessageRepository
	logger  *zap.Logger
}

// NewMessageService 创建消息服务
func NewMessageService(logger *zap.Logger) MessageService {
	return &messageService{
		msgRepo: repository.NewMessageRepository(),
		logger:  logger,
	}
}

func (s *messageService) GetOfflineMessages(ctx context.Context, userID int64) ([]model.Message, error) {
	messages, maxOfflineID, err := s.msgRepo.ListOffline(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 拉取后清除已拉取的离线消息记录（只删 id <= maxOfflineID，避免竞态删除新到达消息）
	if len(messages) > 0 && maxOfflineID > 0 {
		if err := s.msgRepo.DeleteOffline(ctx, userID, maxOfflineID); err != nil {
			s.logger.Error("delete offline messages failed",
				zap.Int64("user_id", userID),
				zap.Int64("max_offline_id", maxOfflineID),
				zap.Error(err),
			)
			// 不返回错误，消息已经拉取成功
		}
	}

	s.logger.Info("offline messages pulled",
		zap.Int64("user_id", userID),
		zap.Int("count", len(messages)),
	)
	return messages, nil
}

func (s *messageService) GetHistory(ctx context.Context, userID, targetID int64, chatType int8, cursorMsgID int64, limit int) ([]model.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.msgRepo.GetHistory(ctx, userID, targetID, chatType, cursorMsgID, limit)
}
