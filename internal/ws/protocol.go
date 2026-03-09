package ws

import "encoding/json"

// ClientMsg 客户端上行消息
type ClientMsg struct {
	Type string          `json:"type"` // "chat" (聊天), "ack" (已读确认), "ping" (心跳)
	Data json.RawMessage `json:"data"`
}

// ChatData 聊天消息数据
type ChatData struct {
	MsgID       string `json:"msg_id"`
	ToID        int64  `json:"to_id"`
	ChatType    int    `json:"chat_type"`    // 1:单聊 2:群聊
	ContentType int    `json:"content_type"` // 1:文本 2:图片 3:文件
	Content     string `json:"content"`
}

// ServerMsg 服务端下行消息
type ServerMsg struct {
	Type string      `json:"type"` // "chat" (新消息), "ack" (服务端确认接收), "error" (错误信息)
	Data interface{} `json:"data"`
}

// AckData ACK 数据
type AckData struct {
	MsgID string `json:"msg_id"`
}

// ErrorData 错误数据
type ErrorData struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// PushMsg 内部推送消息（Push 服务 -> WS 网关）
type PushMsg struct {
	UserID int64       `json:"user_id"`
	Data   interface{} `json:"data"`
}

// KafkaChatMsg Kafka 中传输的聊天消息
type KafkaChatMsg struct {
	MsgID       string `json:"msg_id"`
	FromID      int64  `json:"from_id"`
	ToID        int64  `json:"to_id"`
	ChatType    int    `json:"chat_type"`
	ContentType int    `json:"content_type"`
	Content     string `json:"content"`
	Timestamp   int64  `json:"timestamp"`
}
