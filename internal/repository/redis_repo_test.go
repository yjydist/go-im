package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// setupTestRedis 初始化 miniredis，用于 Redis repository 层单元测试
func setupTestRedis(t *testing.T) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(func() { mr.Close() })
	RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// --- 在线状态测试 ---

func TestRedisRepo_SetAndGetOnline(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	err := repo.SetOnline(ctx, 1001, "127.0.0.1:9091", 5*time.Minute)
	if err != nil {
		t.Fatalf("SetOnline failed: %v", err)
	}

	addr, err := repo.GetOnline(ctx, 1001)
	if err != nil {
		t.Fatalf("GetOnline failed: %v", err)
	}
	if addr != "127.0.0.1:9091" {
		t.Errorf("expected addr=127.0.0.1:9091, got %s", addr)
	}
}

func TestRedisRepo_GetOnline_NotFound(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	addr, err := repo.GetOnline(ctx, 99999)
	if err != nil {
		t.Fatalf("GetOnline unexpected error: %v", err)
	}
	if addr != "" {
		t.Errorf("expected empty addr for offline user, got %s", addr)
	}
}

func TestRedisRepo_DelOnline(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	_ = repo.SetOnline(ctx, 2001, "10.0.0.1:9091", 5*time.Minute)

	err := repo.DelOnline(ctx, 2001)
	if err != nil {
		t.Fatalf("DelOnline failed: %v", err)
	}

	addr, _ := repo.GetOnline(ctx, 2001)
	if addr != "" {
		t.Errorf("expected empty addr after DelOnline, got %s", addr)
	}
}

// --- 消息防重测试 ---

func TestRedisRepo_SetMsgDedup_FirstTime(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	ok, err := repo.SetMsgDedup(ctx, "msg-001", 5*time.Minute)
	if err != nil {
		t.Fatalf("SetMsgDedup failed: %v", err)
	}
	if !ok {
		t.Error("expected true (first time), got false")
	}
}

func TestRedisRepo_SetMsgDedup_Duplicate(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	_, _ = repo.SetMsgDedup(ctx, "msg-dup", 5*time.Minute)

	ok, err := repo.SetMsgDedup(ctx, "msg-dup", 5*time.Minute)
	if err != nil {
		t.Fatalf("SetMsgDedup failed: %v", err)
	}
	if ok {
		t.Error("expected false (duplicate), got true")
	}
}

// --- 群成员缓存测试 ---

func TestRedisRepo_SetAndGetGroupMembers(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	memberIDs := []int64{1, 2, 3, 4, 5}
	err := repo.SetGroupMembers(ctx, 100, memberIDs, 10*time.Minute)
	if err != nil {
		t.Fatalf("SetGroupMembers failed: %v", err)
	}

	got, err := repo.GetGroupMembers(ctx, 100)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}
	if len(got) != len(memberIDs) {
		t.Errorf("expected %d members, got %d", len(memberIDs), len(got))
	}

	// 验证所有成员都在返回结果中
	gotMap := make(map[int64]bool)
	for _, id := range got {
		gotMap[id] = true
	}
	for _, id := range memberIDs {
		if !gotMap[id] {
			t.Errorf("member %d not found in result", id)
		}
	}
}

func TestRedisRepo_GetGroupMembers_NotCached(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	got, err := repo.GetGroupMembers(ctx, 99999)
	if err != nil {
		t.Fatalf("GetGroupMembers unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for uncached group, got %v", got)
	}
}

func TestRedisRepo_DelGroupMembers(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	_ = repo.SetGroupMembers(ctx, 200, []int64{10, 20}, 10*time.Minute)

	err := repo.DelGroupMembers(ctx, 200)
	if err != nil {
		t.Fatalf("DelGroupMembers failed: %v", err)
	}

	got, _ := repo.GetGroupMembers(ctx, 200)
	if got != nil {
		t.Errorf("expected nil after DelGroupMembers, got %v", got)
	}
}

func TestRedisRepo_SetGroupMembers_Empty(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	// 空成员列表不应报错
	err := repo.SetGroupMembers(ctx, 300, []int64{}, 10*time.Minute)
	if err != nil {
		t.Fatalf("SetGroupMembers with empty list failed: %v", err)
	}

	// 空集合应返回 nil（缓存未命中）
	got, err := repo.GetGroupMembers(ctx, 300)
	if err != nil {
		t.Fatalf("GetGroupMembers unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty group, got %v", got)
	}
}

func TestRedisRepo_SetGroupMembers_Overwrite(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	// 先设置 3 个成员
	_ = repo.SetGroupMembers(ctx, 400, []int64{1, 2, 3}, 10*time.Minute)

	// 覆盖为 2 个成员
	_ = repo.SetGroupMembers(ctx, 400, []int64{10, 20}, 10*time.Minute)

	got, err := repo.GetGroupMembers(ctx, 400)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 members after overwrite, got %d", len(got))
	}
}

// --- IsGroupMember 测试 ---

func TestRedisRepo_IsGroupMember_CacheHit_IsMember(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	_ = repo.SetGroupMembers(ctx, 500, []int64{1, 2, 3}, 10*time.Minute)

	isMember, cacheMiss, err := repo.IsGroupMember(ctx, 500, 2)
	if err != nil {
		t.Fatalf("IsGroupMember failed: %v", err)
	}
	if cacheMiss {
		t.Error("expected cacheMiss=false, got true")
	}
	if !isMember {
		t.Error("expected isMember=true, got false")
	}
}

func TestRedisRepo_IsGroupMember_CacheHit_NotMember(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	_ = repo.SetGroupMembers(ctx, 600, []int64{1, 2, 3}, 10*time.Minute)

	isMember, cacheMiss, err := repo.IsGroupMember(ctx, 600, 999)
	if err != nil {
		t.Fatalf("IsGroupMember failed: %v", err)
	}
	if cacheMiss {
		t.Error("expected cacheMiss=false, got true")
	}
	if isMember {
		t.Error("expected isMember=false, got true")
	}
}

func TestRedisRepo_IsGroupMember_CacheMiss(t *testing.T) {
	setupTestRedis(t)
	repo := NewRedisRepository()
	ctx := context.Background()

	// 不设置任何缓存，直接查询
	isMember, cacheMiss, err := repo.IsGroupMember(ctx, 99999, 1)
	if err != nil {
		t.Fatalf("IsGroupMember failed: %v", err)
	}
	if !cacheMiss {
		t.Error("expected cacheMiss=true, got false")
	}
	if isMember {
		t.Error("expected isMember=false on cache miss, got true")
	}
}
