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

// Lua 脚本：条件删除在线状态（仅当值匹配时删除）
var delOnlineScript = redis.NewScript(`
local key = KEYS[1]
local expected = ARGV[1]
local val = redis.call('GET', key)
if val == expected then
    return redis.call('DEL', key)
end
return 0
`)

// Lua 脚本：原子检查群成员（EXISTS + SISMEMBER 合并）
var isGroupMemberScript = redis.NewScript(`
local key = KEYS[1]
local uid = ARGV[1]
if redis.call('EXISTS', key) == 0 then
    return -1  -- 缓存未命中
end
return redis.call('SISMEMBER', key, uid)
`)

// Lua 脚本：原子设置群成员缓存（DEL + SADD + EXPIRE）
var setGroupMembersScript = redis.NewScript(`
local key = KEYS[1]
local ttl = tonumber(ARGV[1])
local count = tonumber(ARGV[2])
redis.call('DEL', key)
if count > 0 then
    local members = {}
    for i = 3, 3 + count - 1 do
        members[#members + 1] = ARGV[i]
    end
    redis.call('SADD', key, unpack(members))
    redis.call('EXPIRE', key, ttl)
end
return 1
`)

// RedisRepository Redis 数据访问接口
type RedisRepository interface {
	// 在线状态
	SetOnline(ctx context.Context, userID int64, wsAddr string, ttl time.Duration) error
	GetOnline(ctx context.Context, userID int64) (string, error)
	DelOnline(ctx context.Context, userID int64, expectedAddr string) error

	// 消息防重
	SetMsgDedup(ctx context.Context, msgID string, ttl time.Duration) (bool, error)
	DelMsgDedup(ctx context.Context, msgID string) error

	// 群成员缓存
	SetGroupMembers(ctx context.Context, groupID int64, memberIDs []int64, ttl time.Duration) error
	GetGroupMembers(ctx context.Context, groupID int64) ([]int64, error)
	// IsGroupMember 检查用户是否为群成员。
	// 返回 (isMember, cacheMiss, err)。
	// 当 cacheMiss == true 时，调用方应回退到 DB 查询。
	IsGroupMember(ctx context.Context, groupID, userID int64) (isMember bool, cacheMiss bool, err error)
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

// DelOnline 条件删除在线状态：仅当当前值等于 expectedAddr 时才删除，
// 防止旧连接的 readPump defer 删掉新连接的在线状态。
func (r *redisRepository) DelOnline(ctx context.Context, userID int64, expectedAddr string) error {
	key := fmt.Sprintf(keyOnline, userID)
	return delOnlineScript.Run(ctx, r.rdb, []string{key}, expectedAddr).Err()
}

func (r *redisRepository) SetMsgDedup(ctx context.Context, msgID string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf(keyMsgDedup, msgID)
	// SET key value NX EX ttl：如果 key 不存在则设置成功返回 true，已存在返回 false
	ok, err := r.rdb.SetArgs(ctx, key, "1", redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	if err == redis.Nil {
		return false, nil // key 已存在
	}
	if err != nil {
		return false, err
	}
	return ok == "OK", nil
}

// DelMsgDedup 删除消息防重 key（Kafka 写入失败时回滚用）
func (r *redisRepository) DelMsgDedup(ctx context.Context, msgID string) error {
	key := fmt.Sprintf(keyMsgDedup, msgID)
	return r.rdb.Del(ctx, key).Err()
}

// SetGroupMembers 使用 Lua 脚本原子地设置群成员缓存
func (r *redisRepository) SetGroupMembers(ctx context.Context, groupID int64, memberIDs []int64, ttl time.Duration) error {
	key := fmt.Sprintf(keyGroupMembers, groupID)
	ttlSec := int(ttl.Seconds())
	args := make([]interface{}, 0, len(memberIDs)+2)
	args = append(args, ttlSec, len(memberIDs))
	for _, id := range memberIDs {
		args = append(args, id)
	}
	return setGroupMembersScript.Run(ctx, r.rdb, []string{key}, args...).Err()
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

// IsGroupMember 使用 Lua 脚本原子地检查群成员
func (r *redisRepository) IsGroupMember(ctx context.Context, groupID, userID int64) (bool, bool, error) {
	key := fmt.Sprintf(keyGroupMembers, groupID)
	result, err := isGroupMemberScript.Run(ctx, r.rdb, []string{key}, userID).Int()
	if err != nil {
		return false, false, err
	}
	if result == -1 {
		// 缓存未命中
		return false, true, nil
	}
	return result == 1, false, nil
}

func (r *redisRepository) DelGroupMembers(ctx context.Context, groupID int64) error {
	key := fmt.Sprintf(keyGroupMembers, groupID)
	return r.rdb.Del(ctx, key).Err()
}
