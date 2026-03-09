package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/pkg/errcode"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MessageService 消息服务接口
type MessageService interface {
	GetOfflineMessages(ctx context.Context, userID int64) ([]model.Message, error)
	GetHistory(ctx context.Context, userID, targetID int64, chatType int8, cursorMsgID int64, limit int) ([]model.Message, error)
}

type messageService struct {
	msgRepo   repository.MessageRepository
	groupRepo repository.GroupRepository
	logger    *zap.Logger
}

// NewMessageService 创建消息服务
func NewMessageService(logger *zap.Logger) MessageService {
	return &messageService{
		msgRepo:   repository.NewMessageRepository(),
		groupRepo: repository.NewGroupRepository(),
		logger:    logger,
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

	// 群聊需要验证请求者是否为群成员，防止非成员拉取群历史消息
	if chatType == 2 {
		_, err := s.groupRepo.GetMember(ctx, targetID, userID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("%w: %d", ErrBusiness, errcode.ErrGroupNotMember)
			}
			return nil, fmt.Errorf("check group membership failed: %w", err)
		}
	}

	return s.msgRepo.GetHistory(ctx, userID, targetID, chatType, cursorMsgID, limit)
}
