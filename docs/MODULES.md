# go-im 模块深讲（MODULES）

> 本文采用统一结构：角色 -> 输入输出 -> 关键步骤 -> 依赖关系 -> 风险边界 -> 面试说法。

---

## 模块 A：`internal/ws`（WebSocket 网关）

### 1) 在系统中的角色

- 实时消息入口层：承接客户端长连接、上行消息、心跳、ACK。
- 不负责最终投递成功，负责“接入 + 基础校验 + 入队 Kafka”。

### 2) 输入 / 输出 / 副作用

- 输入：`/ws?token=...`，以及 WS 文本消息（`chat/ping`）。
- 输出：下行 `ack/pong/error/chat`。
- 副作用：
  - 写 Redis 在线状态与 msg_id 去重键。
  - 写 Kafka topic。

### 3) 关键逻辑步骤

1. `server.HandleWS` 解析 token 并做 JWT 鉴权。
2. 建立 `Client`，注册 `Hub`，启动 `readPump/writePump/kafkaWorker`。
3. `handleChat` 做字段校验（chat_type/content_type/content 长度等）。
4. 群聊先验成员资格（Redis 命中优先，未命中回退 DB，并异步回填缓存）。
5. `SETNX msg_dedup:{msg_id}` 防重。
6. 投递任务放入 `kafkaCh`，并立即 ACK（通道满则回滚 dedup key）。
7. `kafkaWorker` 真正写 Kafka；失败时回滚 dedup key 允许客户端重试。

### 4) 依赖关系

- 依赖：`repository.RedisRepository`、`repository.GroupRepository`、Kafka writer、JWT。
- 被调用：`cmd/ws/main.go` 创建并挂载路由，客户端直接连入。

### 5) 边界与风险

- ACK 早于 Kafka 成功写入，语义是“网关已受理”而非“消息已送达”。
- 连接断开时通过 Lua 条件删除在线状态，避免旧连接误删新连接状态。
- 若 `internal_api_key` 未配置，`/internal/push` 会拒绝所有请求（安全优先）。

### 6) 面试解释（精简版）

“WS 网关专注轻量接入：做鉴权、协议校验、幂等和快速 ACK，把重活交给 Kafka + Push。这样可以让连接层保持高并发吞吐，同时通过回滚 dedup key 让客户端可重试。”

---

## 模块 B：`internal/push`（异步消费与投递）

### 1) 在系统中的角色

- 消息路由核心：消费 Kafka 后决定在线推送还是离线存储。

### 2) 输入 / 输出 / 副作用

- 输入：Kafka `chat_messages` 消息。
- 输出：调用 WS 内部接口 `/internal/push` 或写离线表。
- 副作用：持久化 `messages`、写 `offline_messages`、更新群成员缓存。

### 3) 关键逻辑步骤

1. Consumer `FetchMessage` 拉取消息（手动提交 offset）。
2. JSON 反序列化为 `ws.KafkaChatMsg`。
3. `pusher.HandleMessage` 先落 `messages` 表。
4. `chat_type=1`：查 `online:{user_id}`，在线就 HTTP 推送，失败降级离线。
5. `chat_type=2`：查群成员（Redis -> DB 回填），并发扇出推送（带并发上限）。
6. 处理成功后提交 offset；失败不提交，等待重试。

### 4) 依赖关系

- 依赖：`MessageRepository`、`GroupRepository`、`RedisRepository`、HTTP client。
- 被调用：`cmd/push/main.go` 初始化后持续运行。

### 5) 边界与风险

- `pushToGroup` 对每个成员推送失败仅记录日志，不中断整批。
- 消费成功与 offset 提交间存在窗口，可能导致重复消费（需依赖业务幂等）。
- `sendPush` 依赖 WS 节点地址正确性与内部网络可达性。

### 6) 面试解释（精简版）

“Push 服务把实时接入和重逻辑解耦：Kafka 消费后先落库，再按在线状态分流。在线走内部 HTTP 推送，离线走离线表，保证最终用户可拉取消息。”

---

## 模块 C：`handler -> service -> repository`（HTTP 业务链）

### 1) 在系统中的角色

- 对外 REST 能力层：用户/好友/群组/历史与离线消息拉取。

### 2) 输入输出

- 输入：HTTP 请求 + JWT（中间件解析后写入 `gin.Context`）。
- 输出：统一响应 `{code,msg,data}`。

### 3) 关键业务规则示例

- 用户登录：bcrypt 校验后签发 JWT。
- 好友同意：事务内“更新申请 + 创建反向关系”。
- 群历史拉取：先校验“请求者是否群成员”。
- 离线拉取清理：按 `maxOfflineID` 删除，规避并发竞态误删新消息。

### 4) 风险点

- `ParseBusinessError` 通过字符串末尾解析错误码，可维护性一般，后续可改结构化错误类型。

### 5) 面试解释

“API 层维持典型分层：Handler 做参数与协议，Service 放业务规则，Repository 隔离数据访问。好处是规则集中、可测试、可替换存储实现。”

---

## 快速检查题（自测）

1. 为什么 `handleChat` 要先做群成员校验再做 dedup？
2. Kafka 写失败后为什么要回滚 dedup key？
3. 离线消息为什么不是“拉完全部就删全部”，而是按 `maxOfflineID` 删？
4. `/internal/push` 如果不做 API Key 会有什么风险？
