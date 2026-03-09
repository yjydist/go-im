package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// 在线状态键前缀
const (
	keyOnline       = "online:%d"        // online:{user_id} -> ws_rpc_addr
	keyMsgDedup     = "msg_dedup:%s"     // msg_dedup:{msg_id} -> "1"
	keyGroupMembers = "group:members:%d" // group:members:{group_id} -> SET of user_ids
)

// RedisRepository Redis 数据访问接口
type RedisRepository interface {
	// 在线状态
	SetOnline(ctx context.Context, userID int64, wsAddr string, ttl time.Duration) error
	GetOnline(ctx context.Context, userID int64) (string, error)
	DelOnline(ctx context.Context, userID int64) error

	// 消息防重
	SetMsgDedup(ctx context.Context, msgID string, ttl time.Duration) (bool, error)

	// 群成员缓存
	SetGroupMembers(ctx context.Context, groupID int64, memberIDs []int64, ttl time.Duration) error
	GetGroupMembers(ctx context.Context, groupID int64) ([]int64, error)
	DelGroupMembers(ctx context.Context, groupID int64) error
}

type redisRepository struct {
	rdb *redis.Client
}

// NewRedisRepository 创建 Redis Repository
func NewRedisRepository() RedisRepository {
	return &redisRepository{rdb: RDB}
}

func (r *redisRepository) SetOnline(ctx context.Context, userID int64, wsAddr string, ttl time.Duration) error {
	key := fmt.Sprintf(keyOnline, userID)
	return r.rdb.Set(ctx, key, wsAddr, ttl).Err()
}

func (r *redisRepository) GetOnline(ctx context.Context, userID int64) (string, error) {
	key := fmt.Sprintf(keyOnline, userID)
	val, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (r *redisRepository) DelOnline(ctx context.Context, userID int64) error {
	key := fmt.Sprintf(keyOnline, userID)
	return r.rdb.Del(ctx, key).Err()
}

func (r *redisRepository) SetMsgDedup(ctx context.Context, msgID string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf(keyMsgDedup, msgID)
	// SETNX：如果 key 不存在则设置成功返回 true，已存在返回 false
	return r.rdb.SetNX(ctx, key, "1", ttl).Result()
}

func (r *redisRepository) SetGroupMembers(ctx context.Context, groupID int64, memberIDs []int64, ttl time.Duration) error {
	key := fmt.Sprintf(keyGroupMembers, groupID)
	pipe := r.rdb.Pipeline()
	// 先删除旧缓存
	pipe.Del(ctx, key)
	// 批量添加成员
	members := make([]interface{}, len(memberIDs))
	for i, id := range memberIDs {
		members[i] = id
	}
	if len(members) > 0 {
		pipe.SAdd(ctx, key, members...)
		pipe.Expire(ctx, key, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (r *redisRepository) GetGroupMembers(ctx context.Context, groupID int64) ([]int64, error) {
	key := fmt.Sprintf(keyGroupMembers, groupID)
	vals, err := r.rdb.SMembers(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, nil // 缓存未命中
	}
	ids := make([]int64, 0, len(vals))
	for _, v := range vals {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *redisRepository) DelGroupMembers(ctx context.Context, groupID int64) error {
	key := fmt.Sprintf(keyGroupMembers, groupID)
	return r.rdb.Del(ctx, key).Err()
}
