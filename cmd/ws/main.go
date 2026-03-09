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

	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/pkg/logger"
	"github.com/yjydist/go-im/internal/repository"
	"github.com/yjydist/go-im/internal/ws"
)

func main() {
	configPath := flag.String("config", "config/go-im.yaml", "config file path")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	// 校验配置安全性
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}

	// 初始化日志
	if err := logger.Init(cfg.Log.Level, cfg.Log.Filename); err != nil {
		log.Fatalf("init logger failed: %v", err)
	}
	defer logger.Sync()

	// 初始化 Redis（WS 网关需要 Redis 来管理在线状态和消息去重）
	if err := repository.InitRedis(&cfg.Redis, logger.L); err != nil {
		log.Fatalf("init redis failed: %v", err)
	}

	// 初始化 MySQL（WS 网关需要 MySQL 来回退查询群成员资格）
	if err := repository.InitMySQL(&cfg.MySQL, logger.L); err != nil {
		log.Fatalf("init mysql failed: %v", err)
	}

	// 创建 Hub 并启动
	hub := ws.NewHub(logger.L)
	go hub.Run()

	// 创建 RedisRepo
	redisRepo := repository.NewRedisRepository()

	// 创建 GroupRepo（用于群成员缓存未命中时回退 DB 查询）
	groupRepo := repository.NewGroupRepository()

	// 创建 WS Server
	server := ws.NewServer(cfg, hub, redisRepo, groupRepo, logger.L)
	defer server.Close()

	// 注册 WebSocket 路由
	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/ws", server.HandleWS)

	// 创建 WS 对外 HTTP Server
	wsAddr := fmt.Sprintf(":%d", cfg.WSServer.Port)
	wsSrv := &http.Server{
		Addr:    wsAddr,
		Handler: wsMux,
	}

	// 启动内部 Push 接口
	internalMux := http.NewServeMux()
	internalMux.HandleFunc("/internal/push", server.HandleInternalPush)

	rpcAddr := fmt.Sprintf(":%d", cfg.WSServer.RPCPort)
	rpcSrv := &http.Server{
		Addr:    rpcAddr,
		Handler: internalMux,
	}

	// 监听系统信号
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 启动 WS 对外服务
	go func() {
		logger.L.Sugar().Infof("WS server starting on %s", wsAddr)
		if err := wsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("start ws server failed: %v", err)
		}
	}()

	// 启动内部 RPC 服务
	go func() {
		logger.L.Sugar().Infof("WS internal RPC server starting on %s", rpcAddr)
		if err := rpcSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("start ws internal rpc server failed: %v", err)
		}
	}()

	// 阻塞等待信号
	<-ctx.Done()
	logger.L.Info("WS server shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 先关闭 HTTP 服务（停止接收新连接）
	if err := wsSrv.Shutdown(shutdownCtx); err != nil {
		logger.L.Sugar().Errorf("WS server forced shutdown: %v", err)
	}
	if err := rpcSrv.Shutdown(shutdownCtx); err != nil {
		logger.L.Sugar().Errorf("RPC server forced shutdown: %v", err)
	}

	// 停止 Hub（关闭所有客户端连接）
	hub.Stop()

	logger.L.Info("WS server stopped")
}
