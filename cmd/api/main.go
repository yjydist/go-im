package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/handler"
	"github.com/yjydist/go-im/internal/middleware"
	"github.com/yjydist/go-im/internal/pkg/logger"
	"github.com/yjydist/go-im/internal/pkg/snowflake"
	"github.com/yjydist/go-im/internal/repository"
)

func main() {
	configPath := flag.String("config", "config/go-im.yaml", "config file path")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	// 初始化日志
	if err := logger.Init(cfg.Log.Level, cfg.Log.Filename); err != nil {
		log.Fatalf("init logger failed: %v", err)
	}
	defer logger.Sync()

	// 初始化 Snowflake
	if err := snowflake.Init(cfg.App.ServerID); err != nil {
		log.Fatalf("init snowflake failed: %v", err)
	}

	// 初始化 MySQL
	if err := repository.InitMySQL(&cfg.MySQL, logger.L); err != nil {
		log.Fatalf("init mysql failed: %v", err)
	}

	// 初始化 Redis
	if err := repository.InitRedis(&cfg.Redis, logger.L); err != nil {
		log.Fatalf("init redis failed: %v", err)
	}

	// 设置 Gin 模式
	if cfg.App.Env == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 创建 Gin Engine
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.Logger(logger.L))

	// 注册路由
	handler.RegisterRoutes(r, logger.L)

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.APIServer.Port)
	logger.L.Sugar().Infof("API server starting on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("start api server failed: %v", err)
	}
}
