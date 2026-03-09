package model

import "time"

// Message 消息主表
type Message struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	MsgID       string    `gorm:"size:64;not null;uniqueIndex" json:"msg_id"` // 客户端生成的 UUID，防重幂等
	FromID      int64     `gorm:"not null;index:idx_chat_history" json:"from_id"`
	ToID        int64     `gorm:"not null;index:idx_chat_history;index:idx_to_time" json:"to_id"` // 接收人ID 或 群组ID
	ChatType    int8      `gorm:"not null;index:idx_chat_history" json:"chat_type"`               // 1:单聊 2:群聊
	ContentType int8      `gorm:"not null;default:1" json:"content_type"`                         // 1:文本 2:图片 3:文件
	Content     string    `gorm:"type:text;not null" json:"content"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index:idx_to_time" json:"created_at"`
}

func (Message) TableName() string {
	return "messages"
}

// OfflineMessage 离线消息表
type OfflineMessage struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    int64     `gorm:"not null;index:idx_user_id" json:"user_id"` // 接收方ID
	MessageID int64     `gorm:"not null" json:"message_id"`                // 关联 messages 表的 ID
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (OfflineMessage) TableName() string {
	return "offline_messages"
}
