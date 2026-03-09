// @title           Go-IM API
// @version         1.0
// @description     高性能即时通讯系统 REST API
// @host            localhost:8080
// @BasePath        /api/v1
// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description 输入 "Bearer {token}"（注意空格）
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/handler"
	"github.com/yjydist/go-im/internal/middleware"
	"github.com/yjydist/go-im/internal/pkg/logger"
	"github.com/yjydist/go-im/internal/pkg/snowflake"
	"github.com/yjydist/go-im/internal/repository"

	_ "github.com/yjydist/go-im/docs" // swagger docs
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
	r.Use(middleware.CORS(cfg.APIServer.AllowedOrigins...))
	r.Use(middleware.Logger(logger.L))

	// 注册路由
	handler.RegisterRoutes(r, logger.L)

	// 挂载 Swagger 文档（仅非 release 环境）
	if cfg.App.Env != "release" {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// 启动服务（支持 Graceful Shutdown）
	addr := fmt.Sprintf(":%d", cfg.APIServer.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// 监听系统信号
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.L.Sugar().Infof("API server starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("start api server failed: %v", err)
		}
	}()

	// 阻塞等待信号
	<-ctx.Done()
	logger.L.Info("API server shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.L.Sugar().Errorf("API server forced shutdown: %v", err)
	}
	logger.L.Info("API server stopped")
}
