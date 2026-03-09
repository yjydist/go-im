package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/segmentio/kafka-go"
	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/pkg/jwt"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
)

// NewUpgrader 创建 WebSocket Upgrader，根据配置设置 Origin 检查策略。
// 当 allowedOrigins 为空时，允许所有来源（开发模式）；
// 否则只允许白名单中的来源。
func NewUpgrader(allowedOrigins []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			if len(allowedOrigins) == 0 {
				return true // 开发模式：未配置白名单时允许所有来源
			}
			origin := r.Header.Get("Origin")
			for _, allowed := range allowedOrigins {
				if allowed == "*" || allowed == origin {
					return true
				}
			}
			return false
		},
	}
}

// Server WebSocket 网关服务
type Server struct {
	hub            *Hub
	kafkaWriter    *kafkaWriterAdapter
	redisRepo      repository.RedisRepository
	groupRepo      repository.GroupRepository
	upgrader       websocket.Upgrader
	wsRPCAddr      string
	jwtSecret      string
	internalAPIKey string
	logger         *zap.Logger
}

// kafkaWriterAdapter 适配 kafka-go Writer 到 KafkaWriter 接口
type kafkaWriterAdapter struct {
	writer *kafka.Writer
}

func (a *kafkaWriterAdapter) WriteMessages(ctx context.Context, msgs ...KafkaMessage) error {
	kafkaMsgs := make([]kafka.Message, len(msgs))
	for i, m := range msgs {
		kafkaMsgs[i] = kafka.Message{
			Key:   m.Key,
			Value: m.Value,
		}
	}
	return a.writer.WriteMessages(ctx, kafkaMsgs...)
}

// NewServer 创建 WS 网关服务
func NewServer(cfg *config.Config, hub *Hub, redisRepo repository.RedisRepository, groupRepo repository.GroupRepository, logger *zap.Logger) *Server {
	// 创建 Kafka Writer
	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:  cfg.Kafka.Brokers,
		Topic:    cfg.Kafka.TopicChat,
		Balancer: &kafka.LeastBytes{},
	})

	wsRPCAddr := fmt.Sprintf("localhost:%d", cfg.WSServer.RPCPort)

	return &Server{
		hub:            hub,
		kafkaWriter:    &kafkaWriterAdapter{writer: writer},
		redisRepo:      redisRepo,
		groupRepo:      groupRepo,
		upgrader:       NewUpgrader(cfg.WSServer.AllowedOrigins),
		wsRPCAddr:      wsRPCAddr,
		jwtSecret:      cfg.JWT.Secret,
		internalAPIKey: cfg.WSServer.InternalAPIKey,
		logger:         logger,
	}
}

// HandleWS 处理 WebSocket 升级请求
// 客户端通过 URL query 传递 JWT token 进行鉴权
// ws://host:port/ws?token=xxx
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	// 从 URL query 获取 token
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// 验证 JWT
	claims, err := jwt.ParseToken(token, s.jwtSecret)
	if err != nil {
		s.logger.Warn("ws auth failed",
			zap.String("remote_addr", r.RemoteAddr),
			zap.Error(err),
		)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// 升级为 WebSocket 连接
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("ws upgrade failed",
			zap.Int64("user_id", claims.UserID),
			zap.Error(err),
		)
		return
	}

	s.logger.Info("ws connection established",
		zap.Int64("user_id", claims.UserID),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// 创建 Client 并启动
	client := NewClient(claims.UserID, conn, s.hub, s.kafkaWriter, s.redisRepo, s.groupRepo, s.wsRPCAddr, s.logger)
	s.hub.Register(client)
	client.Start()
}

// HandleInternalPush 处理内部推送请求
// Push 服务通过此接口将消息推送给在线用户
// POST /internal/push
// Header: X-API-Key: <internal_api_key>
// Body: {"user_id": 123, "data": {...}}
func (s *Server) HandleInternalPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证内部 API Key
	if s.internalAPIKey != "" {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != s.internalAPIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var msg PushMsg
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 通过 Hub 查找对应用户的连接
	client, ok := s.hub.GetClient(msg.UserID)
	if !ok {
		// 用户不在本节点
		http.Error(w, "user not connected", http.StatusNotFound)
		return
	}

	// 序列化下行消息
	serverMsg := ServerMsg{
		Type: "chat",
		Data: msg.Data,
	}
	data, err := json.Marshal(serverMsg)
	if err != nil {
		s.logger.Error("marshal push msg failed",
			zap.Int64("user_id", msg.UserID),
			zap.Error(err),
		)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 推送消息到客户端 writePump
	client.Send(data)

	s.logger.Debug("pushed message to client",
		zap.Int64("user_id", msg.UserID),
	)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
}

// Close 关闭 Kafka Writer
func (s *Server) Close() error {
	return s.kafkaWriter.writer.Close()
}
