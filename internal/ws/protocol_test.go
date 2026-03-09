package ws

import (
	"encoding/json"
	"testing"
)

func TestClientMsg_Decode(t *testing.T) {
	raw := `{"type":"chat","data":{"msg_id":"abc","to_id":2,"chat_type":1,"content_type":1,"content":"hello"}}`
	var msg ClientMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg.Type != "chat" {
		t.Errorf("expected type=chat, got %s", msg.Type)
	}

	var chat ChatData
	if err := json.Unmarshal(msg.Data, &chat); err != nil {
		t.Fatalf("unmarshal chat data failed: %v", err)
	}
	if chat.MsgID != "abc" {
		t.Errorf("expected msg_id=abc, got %s", chat.MsgID)
	}
	if chat.ToID != 2 {
		t.Errorf("expected to_id=2, got %d", chat.ToID)
	}
	if chat.ChatType != 1 {
		t.Errorf("expected chat_type=1, got %d", chat.ChatType)
	}
	if chat.Content != "hello" {
		t.Errorf("expected content=hello, got %s", chat.Content)
	}
}

func TestClientMsg_PingDecode(t *testing.T) {
	raw := `{"type":"ping","data":null}`
	var msg ClientMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg.Type != "ping" {
		t.Errorf("expected type=ping, got %s", msg.Type)
	}
}

func TestServerMsg_AckRoundTrip(t *testing.T) {
	orig := ServerMsg{
		Type: "ack",
		Data: AckData{MsgID: "msg-123"},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	var typ string
	_ = json.Unmarshal(decoded["type"], &typ)
	if typ != "ack" {
		t.Errorf("expected type=ack, got %s", typ)
	}

	var ack AckData
	if err := json.Unmarshal(decoded["data"], &ack); err != nil {
		t.Fatalf("unmarshal ack data failed: %v", err)
	}
	if ack.MsgID != "msg-123" {
		t.Errorf("expected msg_id=msg-123, got %s", ack.MsgID)
	}
}

func TestServerMsg_ErrorRoundTrip(t *testing.T) {
	orig := ServerMsg{
		Type: "error",
		Data: ErrorData{Code: 400, Msg: "invalid"},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	var errData ErrorData
	if err := json.Unmarshal(decoded["data"], &errData); err != nil {
		t.Fatalf("unmarshal error data failed: %v", err)
	}
	if errData.Code != 400 {
		t.Errorf("expected code=400, got %d", errData.Code)
	}
	if errData.Msg != "invalid" {
		t.Errorf("expected msg=invalid, got %s", errData.Msg)
	}
}

func TestKafkaChatMsg_RoundTrip(t *testing.T) {
	orig := KafkaChatMsg{
		MsgID:       "k-001",
		FromID:      10,
		ToID:        20,
		ChatType:    2,
		ContentType: 1,
		Content:     "group message",
		Timestamp:   1700000000000,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded KafkaChatMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.MsgID != orig.MsgID {
		t.Errorf("msg_id mismatch: %s != %s", decoded.MsgID, orig.MsgID)
	}
	if decoded.FromID != orig.FromID {
		t.Errorf("from_id mismatch: %d != %d", decoded.FromID, orig.FromID)
	}
	if decoded.ToID != orig.ToID {
		t.Errorf("to_id mismatch: %d != %d", decoded.ToID, orig.ToID)
	}
	if decoded.ChatType != orig.ChatType {
		t.Errorf("chat_type mismatch: %d != %d", decoded.ChatType, orig.ChatType)
	}
	if decoded.ContentType != orig.ContentType {
		t.Errorf("content_type mismatch: %d != %d", decoded.ContentType, orig.ContentType)
	}
	if decoded.Content != orig.Content {
		t.Errorf("content mismatch: %s != %s", decoded.Content, orig.Content)
	}
	if decoded.Timestamp != orig.Timestamp {
		t.Errorf("timestamp mismatch: %d != %d", decoded.Timestamp, orig.Timestamp)
	}
}

func TestPushMsg_Encode(t *testing.T) {
	msg := PushMsg{
		UserID: 42,
		Data:   map[string]string{"key": "value"},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	var uid int64
	_ = json.Unmarshal(decoded["user_id"], &uid)
	if uid != 42 {
		t.Errorf("expected user_id=42, got %d", uid)
	}
}

func TestChatData_AllFields(t *testing.T) {
	orig := ChatData{
		MsgID:       "chat-001",
		ToID:        100,
		ChatType:    2,
		ContentType: 3,
		Content:     "file.pdf",
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ChatData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded != orig {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, orig)
	}
}
