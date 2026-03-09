package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/pkg/snowflake"
	"github.com/yjydist/go-im/internal/repository"
	"github.com/yjydist/go-im/internal/ws"
	"go.uber.org/zap"
	"time"
)

// --- Mock Repositories ---

// mockMessageRepo 消息仓库 mock
type mockMessageRepo struct {
	created  []*model.Message
	offlines []*model.OfflineMessage
	mu       sync.Mutex
}

var _ repository.MessageRepository = (*mockMessageRepo)(nil)

func (r *mockMessageRepo) Create(_ context.Context, msg *model.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, msg)
	return nil
}
func (r *mockMessageRepo) GetHistory(_ context.Context, _, _ int64, _ int8, _ int64, _ int) ([]model.Message, error) {
	return nil, nil
}
func (r *mockMessageRepo) CreateOffline(_ context.Context, off *model.OfflineMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offlines = append(r.offlines, off)
	return nil
}
func (r *mockMessageRepo) ListOffline(_ context.Context, _ int64) ([]model.Message, int64, error) {
	return nil, 0, nil
}
func (r *mockMessageRepo) DeleteOffline(_ context.Context, _ int64, _ int64) error {
	return nil
}

// mockGroupRepo 群组仓库 mock
type mockGroupRepo struct {
	members map[int64][]int64 // groupID -> memberIDs
}

var _ repository.GroupRepository = (*mockGroupRepo)(nil)

func (r *mockGroupRepo) CreateWithOwner(_ context.Context, _ *model.Group) error { return nil }
func (r *mockGroupRepo) GetByID(_ context.Context, _ int64) (*model.Group, error) {
	return nil, nil
}
func (r *mockGroupRepo) AddMember(_ context.Context, _ *model.GroupMember) error { return nil }
func (r *mockGroupRepo) GetMember(_ context.Context, _, _ int64) (*model.GroupMember, error) {
	return nil, nil
}
func (r *mockGroupRepo) ListMembers(_ context.Context, _ int64) ([]model.GroupMember, error) {
	return nil, nil
}
func (r *mockGroupRepo) ListMemberIDs(_ context.Context, groupID int64) ([]int64, error) {
	return r.members[groupID], nil
}
func (r *mockGroupRepo) ListMyGroups(_ context.Context, _ int64) ([]model.Group, error) {
	return nil, nil
}

// mockRedisRepo Redis 仓库 mock
type mockRedisRepo struct {
	online       map[int64]string // userID -> wsAddr
	groupMembers map[int64][]int64
	mu           sync.Mutex
}

var _ repository.RedisRepository = (*mockRedisRepo)(nil)

func newMockRedisRepo() *mockRedisRepo {
	return &mockRedisRepo{
		online:       make(map[int64]string),
		groupMembers: make(map[int64][]int64),
	}
}

func (r *mockRedisRepo) SetOnline(_ context.Context, userID int64, wsAddr string, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.online[userID] = wsAddr
	return nil
}
func (r *mockRedisRepo) GetOnline(_ context.Context, userID int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.online[userID], nil
}
func (r *mockRedisRepo) DelOnline(_ context.Context, _ int64, _ string) error { return nil }
func (r *mockRedisRepo) SetMsgDedup(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}
func (r *mockRedisRepo) DelMsgDedup(_ context.Context, _ string) error { return nil }
func (r *mockRedisRepo) SetGroupMembers(_ context.Context, groupID int64, memberIDs []int64, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groupMembers[groupID] = memberIDs
	return nil
}
func (r *mockRedisRepo) GetGroupMembers(_ context.Context, groupID int64) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.groupMembers[groupID], nil
}
func (r *mockRedisRepo) IsGroupMember(_ context.Context, _, _ int64) (bool, bool, error) {
	return false, false, nil
}
func (r *mockRedisRepo) DelGroupMembers(_ context.Context, _ int64) error { return nil }

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

func init() {
	// push 包的 HandleMessage 调用 snowflake.GenID()
	_ = snowflake.Init(1)
}

// TestHandleMessage_SingleChat_Online 单聊推送：用户在线，HTTP 推送成功
func TestHandleMessage_SingleChat_Online(t *testing.T) {
	// 创建 mock WS RPC 服务
	pushReceived := make(chan struct{}, 1)
	rpcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/push" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		pushReceived <- struct{}{}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer rpcServer.Close()

	// 提取 RPC 地址（去掉 http:// 前缀）
	rpcAddr := strings.TrimPrefix(rpcServer.URL, "http://")

	msgRepo := &mockMessageRepo{}
	redisRepo := newMockRedisRepo()
	// 标记用户 2 在线，地址为 mock RPC 服务
	redisRepo.online[2] = rpcAddr

	pusher := NewPusher(msgRepo, &mockGroupRepo{}, redisRepo, "test-key", PusherConfig{}, testLogger())

	chatMsg := &ws.KafkaChatMsg{
		MsgID:       "msg-001",
		FromID:      1,
		ToID:        2,
		ChatType:    1,
		ContentType: 1,
		Content:     "hello",
		Timestamp:   1700000000000,
	}

	err := pusher.HandleMessage(context.Background(), chatMsg)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// 验证消息已入库
	if len(msgRepo.created) != 1 {
		t.Fatalf("expected 1 created message, got %d", len(msgRepo.created))
	}
	if msgRepo.created[0].MsgID != "msg-001" {
		t.Errorf("expected msg_id=msg-001, got %s", msgRepo.created[0].MsgID)
	}

	// 验证 HTTP push 已发送
	select {
	case <-pushReceived:
		// ok
	default:
		t.Error("expected HTTP push to be sent")
	}

	// 验证没有离线消息
	if len(msgRepo.offlines) != 0 {
		t.Errorf("expected 0 offline messages, got %d", len(msgRepo.offlines))
	}
}

// TestHandleMessage_SingleChat_Offline 单聊推送：用户离线，写离线表
func TestHandleMessage_SingleChat_Offline(t *testing.T) {
	msgRepo := &mockMessageRepo{}
	redisRepo := newMockRedisRepo()
	// 用户 2 不在线（online map 为空）

	pusher := NewPusher(msgRepo, &mockGroupRepo{}, redisRepo, "test-key", PusherConfig{}, testLogger())

	chatMsg := &ws.KafkaChatMsg{
		MsgID:       "msg-002",
		FromID:      1,
		ToID:        2,
		ChatType:    1,
		ContentType: 1,
		Content:     "offline message",
		Timestamp:   1700000000000,
	}

	err := pusher.HandleMessage(context.Background(), chatMsg)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// 验证消息已入库
	if len(msgRepo.created) != 1 {
		t.Fatalf("expected 1 created message, got %d", len(msgRepo.created))
	}

	// 验证离线消息已保存
	if len(msgRepo.offlines) != 1 {
		t.Fatalf("expected 1 offline message, got %d", len(msgRepo.offlines))
	}
	if msgRepo.offlines[0].UserID != 2 {
		t.Errorf("expected offline user_id=2, got %d", msgRepo.offlines[0].UserID)
	}
}

// TestHandleMessage_GroupChat 群聊推送：3 个成员（1 是发送者，2 在线，3 离线）
func TestHandleMessage_GroupChat(t *testing.T) {
	pushCount := 0
	var pushMu sync.Mutex
	rpcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushMu.Lock()
		pushCount++
		pushMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer rpcServer.Close()

	rpcAddr := strings.TrimPrefix(rpcServer.URL, "http://")

	msgRepo := &mockMessageRepo{}
	groupRepo := &mockGroupRepo{
		members: map[int64][]int64{
			100: {1, 2, 3}, // group 100 有 3 个成员
		},
	}
	redisRepo := newMockRedisRepo()
	// 群成员缓存为空 → 从 DB 加载
	// 用户 2 在线，用户 3 离线
	redisRepo.online[2] = rpcAddr

	pusher := NewPusher(msgRepo, groupRepo, redisRepo, "", PusherConfig{}, testLogger())

	chatMsg := &ws.KafkaChatMsg{
		MsgID:       "gmsg-001",
		FromID:      1, // 发送者
		ToID:        100,
		ChatType:    2,
		ContentType: 1,
		Content:     "group hello",
		Timestamp:   1700000000000,
	}

	err := pusher.HandleMessage(context.Background(), chatMsg)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// 验证消息已入库
	if len(msgRepo.created) != 1 {
		t.Fatalf("expected 1 created message, got %d", len(msgRepo.created))
	}

	// 用户 2（在线）应收到 HTTP push
	pushMu.Lock()
	pc := pushCount
	pushMu.Unlock()
	if pc != 1 {
		t.Errorf("expected 1 HTTP push (to user 2), got %d", pc)
	}

	// 用户 3（离线）应有离线消息
	msgRepo.mu.Lock()
	offCount := len(msgRepo.offlines)
	msgRepo.mu.Unlock()
	if offCount != 1 {
		t.Errorf("expected 1 offline message (user 3), got %d", offCount)
	}
}

// TestHandleMessage_UnknownChatType unknown chat_type 不报错，静默忽略
func TestHandleMessage_UnknownChatType(t *testing.T) {
	msgRepo := &mockMessageRepo{}
	redisRepo := newMockRedisRepo()

	pusher := NewPusher(msgRepo, &mockGroupRepo{}, redisRepo, "", PusherConfig{}, testLogger())

	chatMsg := &ws.KafkaChatMsg{
		MsgID:       "msg-unk",
		FromID:      1,
		ToID:        2,
		ChatType:    99, // unknown
		ContentType: 1,
		Content:     "unknown type",
		Timestamp:   1700000000000,
	}

	err := pusher.HandleMessage(context.Background(), chatMsg)
	if err != nil {
		t.Fatalf("HandleMessage should not fail for unknown chat type: %v", err)
	}

	// 消息仍应入库
	if len(msgRepo.created) != 1 {
		t.Errorf("expected 1 created message, got %d", len(msgRepo.created))
	}

	// 不应有离线消息和推送
	if len(msgRepo.offlines) != 0 {
		t.Errorf("expected 0 offline messages for unknown type, got %d", len(msgRepo.offlines))
	}
}

// TestHandleMessage_SingleChat_PushFail 推送失败时降级为离线存储
func TestHandleMessage_SingleChat_PushFail(t *testing.T) {
	// 创建一个始终返回 500 的 RPC 服务
	rpcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer rpcServer.Close()

	rpcAddr := strings.TrimPrefix(rpcServer.URL, "http://")

	msgRepo := &mockMessageRepo{}
	redisRepo := newMockRedisRepo()
	redisRepo.online[2] = rpcAddr // 用户 2 "在线"但 RPC 返回 500

	pusher := NewPusher(msgRepo, &mockGroupRepo{}, redisRepo, "", PusherConfig{}, testLogger())

	chatMsg := &ws.KafkaChatMsg{
		MsgID:       "msg-fail",
		FromID:      1,
		ToID:        2,
		ChatType:    1,
		ContentType: 1,
		Content:     "push will fail",
		Timestamp:   1700000000000,
	}

	err := pusher.HandleMessage(context.Background(), chatMsg)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// 推送失败后应降级为离线存储
	if len(msgRepo.offlines) != 1 {
		t.Errorf("expected 1 offline message (push failed fallback), got %d", len(msgRepo.offlines))
	}
}

// TestPusherConfig_Defaults 验证零值配置使用默认值
func TestPusherConfig_Defaults(t *testing.T) {
	cfg := PusherConfig{}
	if cfg.pushTimeout() != defaultPushTimeout {
		t.Errorf("expected default pushTimeout=%v, got %v", defaultPushTimeout, cfg.pushTimeout())
	}
	if cfg.groupMemberCacheTTL() != defaultGroupMemberCacheTTL {
		t.Errorf("expected default groupMemberCacheTTL=%v, got %v", defaultGroupMemberCacheTTL, cfg.groupMemberCacheTTL())
	}
	if cfg.groupPushConcurrency() != defaultGroupPushConcurrency {
		t.Errorf("expected default groupPushConcurrency=%d, got %d", defaultGroupPushConcurrency, cfg.groupPushConcurrency())
	}
}
