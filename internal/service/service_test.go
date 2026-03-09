package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/pkg/snowflake"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestEnv 初始化 SQLite 内存数据库 + 全局配置，用于 service 层单元测试
func setupTestEnv(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

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

	// 设置 repository 全局 DB
	repository.DB = db

	// 初始化 Snowflake（service 层 Register 依赖 snowflake.GenID）
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("failed to init snowflake: %v", err)
	}

	// 初始化 miniredis（service 层 JoinGroup 依赖 redisRepo.DelGroupMembers）
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(func() { mr.Close() })
	repository.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// 设置全局配置
	config.GlobalConfig = &config.Config{
		JWT: config.JWTConfig{
			Secret:            "test-secret-for-service",
			AccessExpireHours: 24,
		},
	}
}

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

// --- UserService 测试 ---

func TestUserService_Register(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	err := svc.Register(ctx, "testuser", "password123", "Test User")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}

func TestUserService_Register_DuplicateUsername(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	_ = svc.Register(ctx, "duplicate", "pw", "Dup")
	err := svc.Register(ctx, "duplicate", "pw2", "Dup2")
	if err == nil {
		t.Fatal("expected error for duplicate username, got nil")
	}
	// 应包含 ErrUserExist (20001)
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 20001 {
		t.Errorf("expected business error code 20001, got code=%d isBiz=%v err=%v", code, isBiz, err)
	}
}

func TestUserService_Login_Success(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	_ = svc.Register(ctx, "loginuser", "mypassword", "Login")

	token, err := svc.Login(ctx, "loginuser", "mypassword")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if token == "" {
		t.Fatal("Login returned empty token")
	}
}

func TestUserService_Login_WrongPassword(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	_ = svc.Register(ctx, "user1", "correct", "U1")

	_, err := svc.Login(ctx, "user1", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 20003 {
		t.Errorf("expected ErrPasswordWrong (20003), got code=%d isBiz=%v", code, isBiz)
	}
}

func TestUserService_Login_UserNotFound(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	_, err := svc.Login(ctx, "nonexistent", "pw")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 20002 {
		t.Errorf("expected ErrUserNotFound (20002), got code=%d isBiz=%v", code, isBiz)
	}
}

func TestUserService_GetUserInfo(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	_ = svc.Register(ctx, "infouser", "pw", "Info User")

	// 需要先通过 repo 获取 ID（因为 snowflake 生成的 ID 不确定）
	userRepo := repository.NewUserRepository()
	u, _ := userRepo.GetByUsername(ctx, "infouser")

	got, err := svc.GetUserInfo(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserInfo failed: %v", err)
	}
	if got.Username != "infouser" {
		t.Errorf("expected username=infouser, got %s", got.Username)
	}
}

func TestUserService_GetUserInfo_NotFound(t *testing.T) {
	setupTestEnv(t)
	svc := NewUserService(testLogger())
	ctx := context.Background()

	_, err := svc.GetUserInfo(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

// --- FriendService 测试 ---

func TestFriendService_AddFriend(t *testing.T) {
	setupTestEnv(t)
	userSvc := NewUserService(testLogger())
	friendSvc := NewFriendService(testLogger())
	ctx := context.Background()

	_ = userSvc.Register(ctx, "alice", "pw", "Alice")
	_ = userSvc.Register(ctx, "bob", "pw", "Bob")

	userRepo := repository.NewUserRepository()
	alice, _ := userRepo.GetByUsername(ctx, "alice")
	bob, _ := userRepo.GetByUsername(ctx, "bob")

	err := friendSvc.AddFriend(ctx, alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("AddFriend failed: %v", err)
	}
}

func TestFriendService_AddFriend_Self(t *testing.T) {
	setupTestEnv(t)
	friendSvc := NewFriendService(testLogger())
	ctx := context.Background()

	err := friendSvc.AddFriend(ctx, 1, 1)
	if err == nil {
		t.Fatal("expected error for adding self as friend, got nil")
	}
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 30003 {
		t.Errorf("expected ErrFriendSelf (30003), got code=%d isBiz=%v", code, isBiz)
	}
}

func TestFriendService_AddFriend_UserNotFound(t *testing.T) {
	setupTestEnv(t)
	userSvc := NewUserService(testLogger())
	friendSvc := NewFriendService(testLogger())
	ctx := context.Background()

	_ = userSvc.Register(ctx, "real", "pw", "Real")
	userRepo := repository.NewUserRepository()
	real, _ := userRepo.GetByUsername(ctx, "real")

	err := friendSvc.AddFriend(ctx, real.ID, 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent friend, got nil")
	}
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 20002 {
		t.Errorf("expected ErrUserNotFound (20002), got code=%d isBiz=%v", code, isBiz)
	}
}

func TestFriendService_AcceptAndList(t *testing.T) {
	setupTestEnv(t)
	userSvc := NewUserService(testLogger())
	friendSvc := NewFriendService(testLogger())
	ctx := context.Background()

	_ = userSvc.Register(ctx, "x", "pw", "X")
	_ = userSvc.Register(ctx, "y", "pw", "Y")

	userRepo := repository.NewUserRepository()
	x, _ := userRepo.GetByUsername(ctx, "x")
	y, _ := userRepo.GetByUsername(ctx, "y")

	_ = friendSvc.AddFriend(ctx, x.ID, y.ID)

	err := friendSvc.AcceptFriend(ctx, y.ID, x.ID)
	if err != nil {
		t.Fatalf("AcceptFriend failed: %v", err)
	}

	friends, err := friendSvc.ListFriends(ctx, x.ID)
	if err != nil {
		t.Fatalf("ListFriends failed: %v", err)
	}
	found := false
	for _, f := range friends {
		if f.ID == y.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Y in X's friend list")
	}
}

// --- GroupService 测试 ---

func TestGroupService_CreateGroup(t *testing.T) {
	setupTestEnv(t)
	groupSvc := NewGroupService(testLogger())
	ctx := context.Background()

	group, err := groupSvc.CreateGroup(ctx, "Test Group", 1)
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if group.Name != "Test Group" {
		t.Errorf("expected name=Test Group, got %s", group.Name)
	}
}

func TestGroupService_JoinGroup(t *testing.T) {
	setupTestEnv(t)
	groupSvc := NewGroupService(testLogger())
	ctx := context.Background()

	group, _ := groupSvc.CreateGroup(ctx, "JoinTest", 1)

	// user 2 加入
	err := groupSvc.JoinGroup(ctx, group.ID, 2)
	if err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	// 验证成员列表
	members, err := groupSvc.ListMembers(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
}

func TestGroupService_JoinGroup_AlreadyMember(t *testing.T) {
	setupTestEnv(t)
	groupSvc := NewGroupService(testLogger())
	ctx := context.Background()

	group, _ := groupSvc.CreateGroup(ctx, "DupJoin", 1)
	_ = groupSvc.JoinGroup(ctx, group.ID, 2)

	// 重复加入
	err := groupSvc.JoinGroup(ctx, group.ID, 2)
	if err == nil {
		t.Fatal("expected error for duplicate join, got nil")
	}
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 40002 {
		t.Errorf("expected ErrGroupMember (40002), got code=%d isBiz=%v", code, isBiz)
	}
}

func TestGroupService_JoinGroup_NotFound(t *testing.T) {
	setupTestEnv(t)
	groupSvc := NewGroupService(testLogger())
	ctx := context.Background()

	err := groupSvc.JoinGroup(ctx, 99999, 1)
	if err == nil {
		t.Fatal("expected error for nonexistent group, got nil")
	}
	code, isBiz := ParseBusinessError(err)
	if !isBiz || code != 40001 {
		t.Errorf("expected ErrGroupNotFound (40001), got code=%d isBiz=%v", code, isBiz)
	}
}

func TestGroupService_ListMyGroups(t *testing.T) {
	setupTestEnv(t)
	groupSvc := NewGroupService(testLogger())
	ctx := context.Background()

	_, _ = groupSvc.CreateGroup(ctx, "G1", 10)
	g2, _ := groupSvc.CreateGroup(ctx, "G2", 20)
	_ = groupSvc.JoinGroup(ctx, g2.ID, 10)

	groups, err := groupSvc.ListMyGroups(ctx, 10)
	if err != nil {
		t.Fatalf("ListMyGroups failed: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
}

// --- MessageService 测试 ---

func TestMessageService_GetHistory(t *testing.T) {
	setupTestEnv(t)
	msgSvc := NewMessageService(testLogger())
	msgRepo := repository.NewMessageRepository()
	ctx := context.Background()

	// 直接通过 repo 插入消息
	for i := int64(1); i <= 5; i++ {
		_ = msgRepo.Create(ctx, &model.Message{
			ID: i, MsgID: string(rune('a' + i - 1)),
			FromID: 1, ToID: 2, ChatType: 1, ContentType: 1, Content: "test",
		})
	}

	msgs, err := msgSvc.GetHistory(ctx, 1, 2, 1, 0, 3)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

func TestMessageService_GetHistory_DefaultLimit(t *testing.T) {
	setupTestEnv(t)
	msgSvc := NewMessageService(testLogger())
	ctx := context.Background()

	// limit <= 0 应该默认为 20
	msgs, err := msgSvc.GetHistory(ctx, 1, 2, 1, 0, -1)
	if err != nil {
		t.Fatalf("GetHistory with negative limit failed: %v", err)
	}
	// 没有消息，只确认不报错；GORM Find returns nil slice when no records
	_ = msgs
}

func TestMessageService_GetOfflineMessages(t *testing.T) {
	setupTestEnv(t)
	msgSvc := NewMessageService(testLogger())
	msgRepo := repository.NewMessageRepository()
	ctx := context.Background()

	_ = msgRepo.Create(ctx, &model.Message{ID: 1, MsgID: "off1", FromID: 1, ToID: 2, ChatType: 1, ContentType: 1, Content: "offline"})
	_ = msgRepo.CreateOffline(ctx, &model.OfflineMessage{UserID: 2, MessageID: 1})

	msgs, err := msgSvc.GetOfflineMessages(ctx, 2)
	if err != nil {
		t.Fatalf("GetOfflineMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 offline message, got %d", len(msgs))
	}

	// 再次拉取应为空（拉取后自动删除离线记录）
	msgs, err = msgSvc.GetOfflineMessages(ctx, 2)
	if err != nil {
		t.Fatalf("GetOfflineMessages 2nd call failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 offline messages after first pull, got %d", len(msgs))
	}
}

// --- ParseBusinessError 测试 ---

func TestParseBusinessError_NilError(t *testing.T) {
	code, isBiz := ParseBusinessError(nil)
	if isBiz {
		t.Error("nil error should not be business error")
	}
	if code != 0 {
		t.Errorf("expected code 0 for nil error, got %d", code)
	}
}

func TestParseBusinessError_NonBusinessError(t *testing.T) {
	code, isBiz := ParseBusinessError(context.DeadlineExceeded)
	if isBiz {
		t.Error("non-business error should not be parsed as business error")
	}
	if code != 10005 {
		t.Errorf("expected ErrInternal (10005) for non-business error, got %d", code)
	}
}
