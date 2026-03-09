package ws

import (
	"context"
	"time"

	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/repository"
)

// noopRedisRepo 用于 Hub 测试的空操作 RedisRepository 实现
type noopRedisRepo struct{}

var _ repository.RedisRepository = (*noopRedisRepo)(nil)

func (r *noopRedisRepo) SetOnline(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}
func (r *noopRedisRepo) GetOnline(_ context.Context, _ int64) (string, error) { return "", nil }
func (r *noopRedisRepo) DelOnline(_ context.Context, _ int64, _ string) error { return nil }
func (r *noopRedisRepo) SetMsgDedup(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}
func (r *noopRedisRepo) DelMsgDedup(_ context.Context, _ string) error { return nil }
func (r *noopRedisRepo) SetGroupMembers(_ context.Context, _ int64, _ []int64, _ time.Duration) error {
	return nil
}
func (r *noopRedisRepo) GetGroupMembers(_ context.Context, _ int64) ([]int64, error) {
	return nil, nil
}
func (r *noopRedisRepo) IsGroupMember(_ context.Context, _, _ int64) (bool, bool, error) {
	return false, false, nil
}
func (r *noopRedisRepo) DelGroupMembers(_ context.Context, _ int64) error { return nil }

// noopGroupRepo 用于测试的空操作 GroupRepository 实现
type noopGroupRepo struct{}

var _ repository.GroupRepository = (*noopGroupRepo)(nil)

func (r *noopGroupRepo) CreateWithOwner(_ context.Context, _ *model.Group) error { return nil }
func (r *noopGroupRepo) GetByID(_ context.Context, _ int64) (*model.Group, error) {
	return nil, nil
}
func (r *noopGroupRepo) AddMember(_ context.Context, _ *model.GroupMember) error { return nil }
func (r *noopGroupRepo) GetMember(_ context.Context, _, _ int64) (*model.GroupMember, error) {
	return nil, nil
}
func (r *noopGroupRepo) ListMembers(_ context.Context, _ int64) ([]model.GroupMember, error) {
	return nil, nil
}
func (r *noopGroupRepo) ListMemberIDs(_ context.Context, _ int64) ([]int64, error) {
	return nil, nil
}
func (r *noopGroupRepo) ListMyGroups(_ context.Context, _ int64) ([]model.Group, error) {
	return nil, nil
}
