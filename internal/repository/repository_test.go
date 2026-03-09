package repository

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/model"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB 初始化 SQLite 内存数据库，用于 repository 层单元测试
func setupTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	// 自动迁移所有表
	err = db.AutoMigrate(
		&model.User{},
		&model.Friendship{},
		&model.Group{},
		&model.GroupMember{},
		&model.Message{},
		&model.OfflineMessage{},
	)
	if err != nil {
		t.Fatalf("failed to auto migrate: %v", err)
	}

	// 设置包级变量，供 NewXxxRepository() 使用
	DB = db
}

// --- InitRedis 测试 ---

func TestInitRedis_Success(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(func() { mr.Close() })

	// 需要 GlobalConfig 以防 InitRedis 或被测路径读取（实际 InitRedis 不读 GlobalConfig）
	cfg := &config.RedisConfig{
		Addr: mr.Addr(),
	}
	l, _ := zap.NewDevelopment()
	if err := InitRedis(cfg, l); err != nil {
		t.Fatalf("InitRedis failed: %v", err)
	}
	if RDB == nil {
		t.Fatal("expected RDB to be set after InitRedis, got nil")
	}

	// 验证连通性
	ctx := context.Background()
	if err := RDB.Ping(ctx).Err(); err != nil {
		t.Fatalf("RDB.Ping failed: %v", err)
	}
}

func TestInitRedis_WithPoolConfig(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(func() { mr.Close() })

	cfg := &config.RedisConfig{
		Addr:           mr.Addr(),
		PoolSize:       50,
		MinIdleConns:   5,
		DialTimeoutMs:  2000,
		ReadTimeoutMs:  1000,
		WriteTimeoutMs: 1000,
	}
	l, _ := zap.NewDevelopment()
	if err := InitRedis(cfg, l); err != nil {
		t.Fatalf("InitRedis with pool config failed: %v", err)
	}
	if RDB == nil {
		t.Fatal("expected RDB to be set")
	}

	// 验证连接池参数生效
	opts := RDB.Options()
	if opts.PoolSize != 50 {
		t.Errorf("expected PoolSize=50, got %d", opts.PoolSize)
	}
	if opts.MinIdleConns != 5 {
		t.Errorf("expected MinIdleConns=5, got %d", opts.MinIdleConns)
	}
}

func TestInitRedis_BadAddr(t *testing.T) {
	cfg := &config.RedisConfig{
		Addr:          "localhost:1", // 不可连接的地址
		DialTimeoutMs: 500,           // 缩短超时加快测试
	}
	l, _ := zap.NewDevelopment()
	err := InitRedis(cfg, l)
	if err == nil {
		t.Fatal("expected error for unreachable redis addr, got nil")
	}
}

// --- UserRepository 测试 ---

func TestUserRepo_CreateAndGetByID(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()
	ctx := context.Background()

	user := &model.User{
		ID:       1001,
		Username: "testuser",
		Password: "hashedpw",
		Nickname: "Test",
		Status:   1,
	}

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, 1001)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Username != "testuser" {
		t.Errorf("expected username=testuser, got %s", got.Username)
	}
	if got.Nickname != "Test" {
		t.Errorf("expected nickname=Test, got %s", got.Nickname)
	}
}

func TestUserRepo_GetByUsername(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()
	ctx := context.Background()

	user := &model.User{ID: 1002, Username: "alice", Password: "pw", Nickname: "Alice", Status: 1}
	_ = repo.Create(ctx, user)

	got, err := repo.GetByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetByUsername failed: %v", err)
	}
	if got.ID != 1002 {
		t.Errorf("expected id=1002, got %d", got.ID)
	}
}

func TestUserRepo_GetByUsername_NotFound(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()
	ctx := context.Background()

	_, err := repo.GetByUsername(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestUserRepo_GetByID_NotFound(t *testing.T) {
	setupTestDB(t)
	repo := NewUserRepository()
	ctx := context.Background()

	_, err := repo.GetByID(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

// --- FriendRepository 测试 ---

func TestFriendRepo_CreateAndGet(t *testing.T) {
	setupTestDB(t)
	userRepo := NewUserRepository()
	friendRepo := NewFriendRepository()
	ctx := context.Background()

	// 先创建两个用户
	_ = userRepo.Create(ctx, &model.User{ID: 1, Username: "u1", Password: "pw", Status: 1})
	_ = userRepo.Create(ctx, &model.User{ID: 2, Username: "u2", Password: "pw", Status: 1})

	friendship := &model.Friendship{UserID: 1, FriendID: 2, Status: 0}
	if err := friendRepo.Create(ctx, friendship); err != nil {
		t.Fatalf("Create friendship failed: %v", err)
	}

	got, err := friendRepo.GetByUserAndFriend(ctx, 1, 2)
	if err != nil {
		t.Fatalf("GetByUserAndFriend failed: %v", err)
	}
	if got.Status != 0 {
		t.Errorf("expected status=0 (pending), got %d", got.Status)
	}
}

func TestFriendRepo_AcceptAndList(t *testing.T) {
	setupTestDB(t)
	userRepo := NewUserRepository()
	friendRepo := NewFriendRepository()
	ctx := context.Background()

	_ = userRepo.Create(ctx, &model.User{ID: 10, Username: "bob", Password: "pw", Nickname: "Bob", Status: 1})
	_ = userRepo.Create(ctx, &model.User{ID: 20, Username: "carol", Password: "pw", Nickname: "Carol", Status: 1})

	// bob -> carol 发送好友请求
	_ = friendRepo.Create(ctx, &model.Friendship{UserID: 10, FriendID: 20, Status: 0})

	// carol 接受
	if err := friendRepo.Accept(ctx, 10, 20); err != nil {
		t.Fatalf("Accept failed: %v", err)
	}

	// 创建反向关系（双向好友）
	_ = friendRepo.Create(ctx, &model.Friendship{UserID: 20, FriendID: 10, Status: 1})

	// 检查 bob 的好友列表
	friends, err := friendRepo.ListFriends(ctx, 10)
	if err != nil {
		t.Fatalf("ListFriends failed: %v", err)
	}
	// bob -> carol 的关系需要 status=1 才能被 ListFriends 查到
	// Accept 更新了 status=1，所以应该查到 carol
	found := false
	for _, f := range friends {
		if f.ID == 20 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected carol in bob's friend list, got %+v", friends)
	}
}

func TestFriendRepo_GetByUserAndFriend_NotFound(t *testing.T) {
	setupTestDB(t)
	friendRepo := NewFriendRepository()
	ctx := context.Background()

	_, err := friendRepo.GetByUserAndFriend(ctx, 999, 888)
	if err == nil {
		t.Fatal("expected error for nonexistent friendship, got nil")
	}
}

// --- GroupRepository 测试 ---

func TestGroupRepo_CreateWithOwner(t *testing.T) {
	setupTestDB(t)
	groupRepo := NewGroupRepository()
	ctx := context.Background()

	group := &model.Group{ID: 100, Name: "Test Group", OwnerID: 1}
	if err := groupRepo.CreateWithOwner(ctx, group); err != nil {
		t.Fatalf("CreateWithOwner failed: %v", err)
	}

	// 验证群组创建
	got, err := groupRepo.GetByID(ctx, 100)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Name != "Test Group" {
		t.Errorf("expected name=Test Group, got %s", got.Name)
	}

	// 验证群主已作为成员加入
	member, err := groupRepo.GetMember(ctx, 100, 1)
	if err != nil {
		t.Fatalf("GetMember for owner failed: %v", err)
	}
	if member.Role != 2 {
		t.Errorf("expected role=2 (owner), got %d", member.Role)
	}
}

func TestGroupRepo_AddMemberAndList(t *testing.T) {
	setupTestDB(t)
	groupRepo := NewGroupRepository()
	ctx := context.Background()

	_ = groupRepo.CreateWithOwner(ctx, &model.Group{ID: 200, Name: "Group2", OwnerID: 1})
	_ = groupRepo.AddMember(ctx, &model.GroupMember{GroupID: 200, UserID: 2, Role: 0})
	_ = groupRepo.AddMember(ctx, &model.GroupMember{GroupID: 200, UserID: 3, Role: 0})

	members, err := groupRepo.ListMembers(ctx, 200)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	// 群主 + 2 个成员 = 3
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}
}

func TestGroupRepo_ListMemberIDs(t *testing.T) {
	setupTestDB(t)
	groupRepo := NewGroupRepository()
	ctx := context.Background()

	_ = groupRepo.CreateWithOwner(ctx, &model.Group{ID: 300, Name: "Group3", OwnerID: 10})
	_ = groupRepo.AddMember(ctx, &model.GroupMember{GroupID: 300, UserID: 20, Role: 0})

	ids, err := groupRepo.ListMemberIDs(ctx, 300)
	if err != nil {
		t.Fatalf("ListMemberIDs failed: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 member IDs, got %d", len(ids))
	}
}

func TestGroupRepo_ListMyGroups(t *testing.T) {
	setupTestDB(t)
	groupRepo := NewGroupRepository()
	ctx := context.Background()

	_ = groupRepo.CreateWithOwner(ctx, &model.Group{ID: 400, Name: "GroupA", OwnerID: 5})
	_ = groupRepo.CreateWithOwner(ctx, &model.Group{ID: 401, Name: "GroupB", OwnerID: 6})
	_ = groupRepo.AddMember(ctx, &model.GroupMember{GroupID: 401, UserID: 5, Role: 0})

	groups, err := groupRepo.ListMyGroups(ctx, 5)
	if err != nil {
		t.Fatalf("ListMyGroups failed: %v", err)
	}
	// user 5 是 GroupA 的群主 + GroupB 的成员 = 2 个群
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
}

func TestGroupRepo_GetByID_NotFound(t *testing.T) {
	setupTestDB(t)
	groupRepo := NewGroupRepository()
	ctx := context.Background()

	_, err := groupRepo.GetByID(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
}

func TestGroupRepo_GetMember_NotFound(t *testing.T) {
	setupTestDB(t)
	groupRepo := NewGroupRepository()
	ctx := context.Background()

	_, err := groupRepo.GetMember(ctx, 99999, 88888)
	if err == nil {
		t.Fatal("expected error for nonexistent member, got nil")
	}
}

// --- MessageRepository 测试 ---

func TestMessageRepo_CreateAndGetHistory(t *testing.T) {
	setupTestDB(t)
	msgRepo := NewMessageRepository()
	ctx := context.Background()

	// 创建几条单聊消息
	for i := int64(1); i <= 5; i++ {
		_ = msgRepo.Create(ctx, &model.Message{
			ID:          i,
			MsgID:       "msg-" + string(rune('A'+i-1)),
			FromID:      1,
			ToID:        2,
			ChatType:    1,
			ContentType: 1,
			Content:     "hello " + string(rune('A'+i-1)),
		})
	}

	// 查询历史（最新 3 条）
	msgs, err := msgRepo.GetHistory(ctx, 1, 2, 1, 0, 3)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	// 应该按 id DESC 排序
	if len(msgs) > 0 && msgs[0].ID != 5 {
		t.Errorf("expected first message id=5, got %d", msgs[0].ID)
	}
}

func TestMessageRepo_GetHistory_WithCursor(t *testing.T) {
	setupTestDB(t)
	msgRepo := NewMessageRepository()
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		_ = msgRepo.Create(ctx, &model.Message{
			ID: i, MsgID: "cursor-" + string(rune('0'+i)),
			FromID: 1, ToID: 2, ChatType: 1, ContentType: 1, Content: "msg",
		})
	}

	// 以 cursorMsgID=8 为游标，获取 ID < 8 的消息
	msgs, err := msgRepo.GetHistory(ctx, 1, 2, 1, 8, 3)
	if err != nil {
		t.Fatalf("GetHistory with cursor failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	// 最新的应该是 id=7
	if len(msgs) > 0 && msgs[0].ID != 7 {
		t.Errorf("expected first message id=7, got %d", msgs[0].ID)
	}
}

func TestMessageRepo_GetHistory_GroupChat(t *testing.T) {
	setupTestDB(t)
	msgRepo := NewMessageRepository()
	ctx := context.Background()

	// 群聊消息（chat_type=2, to_id=群组ID）
	_ = msgRepo.Create(ctx, &model.Message{ID: 1, MsgID: "g1", FromID: 1, ToID: 100, ChatType: 2, ContentType: 1, Content: "group msg 1"})
	_ = msgRepo.Create(ctx, &model.Message{ID: 2, MsgID: "g2", FromID: 2, ToID: 100, ChatType: 2, ContentType: 1, Content: "group msg 2"})

	msgs, err := msgRepo.GetHistory(ctx, 1, 100, 2, 0, 10)
	if err != nil {
		t.Fatalf("GetHistory group failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 group messages, got %d", len(msgs))
	}
}

func TestMessageRepo_OfflineMessages(t *testing.T) {
	setupTestDB(t)
	msgRepo := NewMessageRepository()
	ctx := context.Background()

	// 先创建消息
	_ = msgRepo.Create(ctx, &model.Message{ID: 1, MsgID: "off1", FromID: 1, ToID: 2, ChatType: 1, ContentType: 1, Content: "offline msg"})

	// 创建离线记录
	if err := msgRepo.CreateOffline(ctx, &model.OfflineMessage{UserID: 2, MessageID: 1}); err != nil {
		t.Fatalf("CreateOffline failed: %v", err)
	}

	// 拉取离线消息
	msgs, maxOfflineID, err := msgRepo.ListOffline(ctx, 2)
	if err != nil {
		t.Fatalf("ListOffline failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 offline message, got %d", len(msgs))
	}
	if len(msgs) > 0 && msgs[0].Content != "offline msg" {
		t.Errorf("expected content=offline msg, got %s", msgs[0].Content)
	}
	if maxOfflineID == 0 {
		t.Error("expected maxOfflineID > 0")
	}

	// 删除离线消息（只删 id <= maxOfflineID）
	if err := msgRepo.DeleteOffline(ctx, 2, maxOfflineID); err != nil {
		t.Fatalf("DeleteOffline failed: %v", err)
	}

	// 确认删除后为空
	msgs, _, err = msgRepo.ListOffline(ctx, 2)
	if err != nil {
		t.Fatalf("ListOffline after delete failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 offline messages after delete, got %d", len(msgs))
	}
}
