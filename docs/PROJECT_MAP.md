# go-im 项目地图（PROJECT MAP）

## 1) 项目类型

- **已确认事实**
  - 这是一个 Go 后端 IM（即时通讯）系统，采用三服务拆分：`API Server`、`WS Gateway`、`Push Server`。
  - 核心协议是 HTTP + WebSocket，异步链路使用 Kafka，状态与幂等使用 Redis，持久化使用 MySQL。

- **合理推断**
  - 当前仓库主要聚焦“后端消息投递链路 + 基础社交关系（用户/好友/群组）”，前端不是主角。

- **待确认问题**
  - 实际业务场景是内部系统、教育项目，还是生产业务？

---

## 2) 技术栈概览

- 语言/框架：Go 1.25、Gin、gorilla/websocket
- 存储：MySQL（GORM）
- 缓存与状态：Redis（go-redis）
- 消息队列：Kafka（kafka-go）
- 认证：JWT
- 配置：Viper（支持 `GOIM_*` 环境变量覆盖）
- 日志：Zap + Lumberjack
- ID：Snowflake
- 部署：Docker Compose（`deploy/docker-compose.yaml`）

---

## 3) 目录结构解读（职责视角）

```text
cmd/
  api/main.go      API 服务启动入口
  ws/main.go       WebSocket 网关启动入口
  push/main.go     Kafka 消费 + 推送服务入口

internal/
  config/          配置加载与安全校验
  handler/         Gin 路由与 HTTP Handler
  middleware/      JWT、CORS、日志中间件
  service/         业务逻辑层（用户/好友/群组/消息）
  repository/      MySQL/Redis 数据访问
  ws/              WebSocket 网关核心（Hub、Client、协议）
  push/            Kafka Consumer + Push 路由逻辑
  model/           数据模型（users/messages/offline 等）
  pkg/             公共组件（jwt/errcode/response/logger/snowflake）

deploy/
  docker-compose.yaml   本地一键环境
  mysql/init.sql        建表脚本

examples/
  e2e/                  端到端链路脚本
  bench/                压测脚本
```

---

## 4) 入口点（Start Points）

- API 入口：`cmd/api/main.go`
  - 加载配置 -> 校验 -> 初始化日志/Snowflake/MySQL/Redis -> 注册路由 -> 启动 HTTP 服务。
- WS 入口：`cmd/ws/main.go`
  - 初始化 Redis/MySQL -> 启动 Hub -> 注册 `/ws` 与 `/internal/push` -> 启动双端口服务。
- Push 入口：`cmd/push/main.go`
  - 初始化依赖 -> 创建 `Pusher` -> 创建 Kafka `Consumer` -> 消费并投递。

---

## 5) 核心模块与职责

1. `internal/ws`（实时入口）
   - 处理 WS 鉴权、心跳、上行消息校验、去重、写 Kafka、ACK 下发。
2. `internal/push`（异步中枢）
   - 消费 Kafka -> 持久化 -> 判断在线状态 -> 在线推送或离线存储。
3. `internal/service`（业务规则层）
   - 账号、好友、群组、离线拉取、历史消息权限校验。
4. `internal/repository`（数据边界）
   - MySQL 与 Redis 的统一访问封装。

---

## 6) 一条主链路概览（单聊）

1. 客户端通过 `ws://host:8081/ws?token=...` 建连。
2. WS 网关校验 JWT，创建 `Client`，注册在线状态 `online:{user_id}`。
3. 客户端上行 `chat`：校验字段 + Redis `SETNX msg_dedup:{msg_id}` 防重。
4. WS 把消息异步写入 Kafka，并给发送方 ACK。
5. Push 服务消费 Kafka，先持久化到 `messages`。
6. Push 查 Redis 在线状态：
   - 在线 -> 调用目标 WS 的 `/internal/push` 推送。
   - 离线/推送失败 -> 写 `offline_messages`。
7. 用户上线后通过 API `GET /api/v1/message/offline` 拉取离线消息并清理已拉取记录。

---

## 7) 推荐学习顺序

1. `README.md`（先建立系统图）
2. `cmd/*/main.go`（理解三服务启动差异）
3. `internal/ws/*`（消息入口、幂等、ACK、在线状态）
4. `internal/push/*`（消费、持久化、在线/离线分流）
5. `internal/service + handler + repository`（HTTP 业务面）
6. `deploy/mysql/init.sql`（数据模型约束）
7. `examples/e2e/main.go`（把抽象流程映射为可执行脚本）

---

## 8) 当前不确定点

- 真实线上部署拓扑（是否多 WS 节点、多 Kafka 分区、多机房）。
- 是否有消息投递 SLA、读写峰值、容量指标。
- 是否存在消息已读回执、撤回、多端同步等高级 IM 能力。
- 用户本人在该项目中的真实参与边界（后续简历提炼必须补）。

---

## 9) 面试高频区域

1. 为什么要拆 API / WS / Push 三服务？
2. `ACK` 与真正“送达”语义差异（当前 ACK 更接近网关接收确认）。
3. Redis 去重与 Kafka 失败回滚策略。
4. 离线消息删除的竞态规避（`maxOfflineID` 截断删除）。
5. 群聊成员校验（Redis 缓存 + DB 回退 + 回填）。
6. `/internal/push` 的鉴权与边界（`X-API-Key`）。
