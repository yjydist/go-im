package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/yjydist/go-im/internal/model"
	"gorm.io/gorm"
)

// UserRepository 用户数据访问接口
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	GetByID(ctx context.Context, id int64) (*model.User, error)
}

type userRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户 Repository
func NewUserRepository() UserRepository {
	return &userRepository{db: DB}
}

func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("create user failed: %w", err)
	}
	return nil
}

func (r *userRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// FriendRepository 好友数据访问接口
type FriendRepository interface {
	Create(ctx context.Context, friendship *model.Friendship) error
	Accept(ctx context.Context, userID, friendID int64) error
	AcceptAndCreateReverse(ctx context.Context, userID, friendID int64) error
	GetByUserAndFriend(ctx context.Context, userID, friendID int64) (*model.Friendship, error)
	ListFriends(ctx context.Context, userID int64) ([]model.User, error)
}

type friendRepository struct {
	db *gorm.DB
}

// NewFriendRepository 创建好友 Repository
func NewFriendRepository() FriendRepository {
	return &friendRepository{db: DB}
}

func (r *friendRepository) Create(ctx context.Context, friendship *model.Friendship) error {
	if err := r.db.WithContext(ctx).Create(friendship).Error; err != nil {
		return fmt.Errorf("create friendship failed: %w", err)
	}
	return nil
}

func (r *friendRepository) Accept(ctx context.Context, userID, friendID int64) error {
	result := r.db.WithContext(ctx).
		Model(&model.Friendship{}).
		Where("user_id = ? AND friend_id = ? AND status = 0", userID, friendID).
		Updates(map[string]interface{}{
			"status":     1,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return fmt.Errorf("accept friendship failed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// AcceptAndCreateReverse 在事务中接受好友请求并创建反向关系
func (r *friendRepository) AcceptAndCreateReverse(ctx context.Context, userID, friendID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 更新原始好友请求状态
		result := tx.Model(&model.Friendship{}).
			Where("user_id = ? AND friend_id = ? AND status = 0", userID, friendID).
			Updates(map[string]interface{}{
				"status":     1,
				"updated_at": time.Now(),
			})
		if result.Error != nil {
			return fmt.Errorf("accept friendship failed: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		// 2. 创建反向好友关系（双向）
		reverse := &model.Friendship{
			UserID:   friendID,
			FriendID: userID,
			Status:   1,
		}
		if err := tx.Create(reverse).Error; err != nil {
			return fmt.Errorf("create reverse friendship failed: %w", err)
		}
		return nil
	})
}

func (r *friendRepository) GetByUserAndFriend(ctx context.Context, userID, friendID int64) (*model.Friendship, error) {
	var friendship model.Friendship
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND friend_id = ?", userID, friendID).
		First(&friendship).Error
	if err != nil {
		return nil, err
	}
	return &friendship, nil
}

func (r *friendRepository) ListFriends(ctx context.Context, userID int64) ([]model.User, error) {
	var users []model.User
	// 查找所有已接受的好友关系，获取好友用户信息
	err := r.db.WithContext(ctx).
		Table("users").
		Joins("JOIN friendships ON friendships.friend_id = users.id").
		Where("friendships.user_id = ? AND friendships.status = 1", userID).
		Find(&users).Error
	if err != nil {
		return nil, fmt.Errorf("list friends failed: %w", err)
	}
	return users, nil
}
