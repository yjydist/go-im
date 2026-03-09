package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/yjydist/go-im/internal/config"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	DB  *gorm.DB
	RDB *redis.Client
)

// InitMySQL 初始化 MySQL 连接
func InitMySQL(cfg *config.MySQLConfig, zapLogger *zap.Logger) error {
	var gormLogLevel logger.LogLevel
	if config.GlobalConfig.App.Env == "debug" {
		gormLogLevel = logger.Info
	} else {
		gormLogLevel = logger.Warn
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return fmt.Errorf("connect mysql failed: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB failed: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	// 连接池生命周期设置（防止使用过期的 MySQL 连接）
	connMaxLife := 300 // 默认 5 分钟
	if cfg.ConnMaxLifeSec > 0 {
		connMaxLife = cfg.ConnMaxLifeSec
	}
	sqlDB.SetConnMaxLifetime(time.Duration(connMaxLife) * time.Second)

	connMaxIdle := 180 // 默认 3 分钟
	if cfg.ConnMaxIdleSec > 0 {
		connMaxIdle = cfg.ConnMaxIdleSec
	}
	sqlDB.SetConnMaxIdleTime(time.Duration(connMaxIdle) * time.Second)

	DB = db
	zapLogger.Info("mysql connected")
	return nil
}

// InitRedis 初始化 Redis 连接并验证连通性
func InitRedis(cfg *config.RedisConfig, zapLogger *zap.Logger) error {
	opts := &redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}

	// 连接池大小（0 使用 go-redis 默认值：10 * NumCPU）
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}
	if cfg.MinIdleConns > 0 {
		opts.MinIdleConns = cfg.MinIdleConns
	}

	// 超时设置（0 或未配置使用 go-redis 默认值）
	dialTimeout := 5000 // 默认 5 秒
	if cfg.DialTimeoutMs > 0 {
		dialTimeout = cfg.DialTimeoutMs
	}
	opts.DialTimeout = time.Duration(dialTimeout) * time.Millisecond

	readTimeout := 3000 // 默认 3 秒
	if cfg.ReadTimeoutMs > 0 {
		readTimeout = cfg.ReadTimeoutMs
	}
	opts.ReadTimeout = time.Duration(readTimeout) * time.Millisecond

	writeTimeout := 3000 // 默认 3 秒
	if cfg.WriteTimeoutMs > 0 {
		writeTimeout = cfg.WriteTimeoutMs
	}
	opts.WriteTimeout = time.Duration(writeTimeout) * time.Millisecond

	RDB = redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RDB.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	zapLogger.Info("redis connected")
	return nil
}
