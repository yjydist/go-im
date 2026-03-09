package repository

import (
	"context"
	"fmt"

	"github.com/yjydist/go-im/internal/model"
	"gorm.io/gorm"
)

// MessageRepository 消息数据访问接口
type MessageRepository interface {
	Create(ctx context.Context, msg *model.Message) error
	GetHistory(ctx context.Context, userID, targetID int64, chatType int8, cursorMsgID int64, limit int) ([]model.Message, error)
	CreateOffline(ctx context.Context, offline *model.OfflineMessage) error
	ListOffline(ctx context.Context, userID int64) ([]model.Message, error)
	DeleteOffline(ctx context.Context, userID int64) error
}

type messageRepository struct {
	db *gorm.DB
}

// NewMessageRepository 创建消息 Repository
func NewMessageRepository() MessageRepository {
	return &messageRepository{db: DB}
}

func (r *messageRepository) Create(ctx context.Context, msg *model.Message) error {
	if err := r.db.WithContext(ctx).Create(msg).Error; err != nil {
		return fmt.Errorf("create message failed: %w", err)
	}
	return nil
}

func (r *messageRepository) GetHistory(ctx context.Context, userID, targetID int64, chatType int8, cursorMsgID int64, limit int) ([]model.Message, error) {
	var messages []model.Message
	query := r.db.WithContext(ctx).Where("chat_type = ?", chatType)

	if chatType == 1 {
		// 单聊：查找两个用户之间的消息
		query = query.Where(
			"(from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)",
			userID, targetID, targetID, userID,
		)
	} else {
		// 群聊：查找发送到该群组的消息
		query = query.Where("to_id = ?", targetID)
	}

	if cursorMsgID > 0 {
		query = query.Where("id < ?", cursorMsgID)
	}

	err := query.Order("id DESC").Limit(limit).Find(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("get message history failed: %w", err)
	}
	return messages, nil
}

func (r *messageRepository) CreateOffline(ctx context.Context, offline *model.OfflineMessage) error {
	if err := r.db.WithContext(ctx).Create(offline).Error; err != nil {
		return fmt.Errorf("create offline message failed: %w", err)
	}
	return nil
}

func (r *messageRepository) ListOffline(ctx context.Context, userID int64) ([]model.Message, error) {
	var messages []model.Message
	err := r.db.WithContext(ctx).
		Table("messages").
		Joins("JOIN offline_messages ON offline_messages.message_id = messages.id").
		Where("offline_messages.user_id = ?", userID).
		Order("messages.id ASC").
		Find(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("list offline messages failed: %w", err)
	}
	return messages, nil
}

func (r *messageRepository) DeleteOffline(ctx context.Context, userID int64) error {
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.OfflineMessage{}).Error
	if err != nil {
		return fmt.Errorf("delete offline messages failed: %w", err)
	}
	return nil
}
