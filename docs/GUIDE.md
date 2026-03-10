# go-im 学习总指南（GUIDE）

## 当前学习阶段

- **阶段定位**：`full` 模式第 1 轮（已完成项目地图 + 初步主链路拆解）
- **本轮目标**：先建立“能导航”的整体认知，再进入模块深讲、简历提炼、面试训练。
- **用户背景（已确认）**：当前代码主要由 AI 生成，你的目标是把它转化为“可解释、可维护、可面试”的真实能力。

---

## 学习目标（分层）

### Must Know（必须掌握）

- 项目目标：高性能 IM 后端，三服务解耦。
- 架构分层：Handler / Service / Repository + WS / Push 专用链路。
- 主链路：WebSocket 上行 -> Kafka -> Push -> 在线推送/离线存储 -> API 拉取。
- 关键规则位置：消息幂等、群成员校验、离线消息清理、JWT 鉴权。

### Should Know（应该掌握）

- ACK 语义边界、Kafka 手动提交 offset 策略。
- 群聊缓存回填与并发限制策略。
- 连接管理与在线状态刷新策略。

### Good to Know（加分项）

- 压测脚本与 e2e 脚本使用方式。
- 日志与配置安全校验策略。

---

## 推荐学习路线（可复习）

1. **系统视角**：`README.md` + `docs/PROJECT_MAP.md`
2. **入口视角**：`cmd/api|ws|push/main.go`
3. **实时链路**：`internal/ws/*`
4. **异步投递链路**：`internal/push/*`
5. **业务 API 链路**：`handler -> service -> repository`
6. **数据与约束**：`internal/model/*` + `deploy/mysql/init.sql`
7. **验证视角**：`examples/e2e/main.go`、`examples/bench/main.go`

---

## 主链路速记（30 秒复述版）

1. WS 鉴权建连，写入 Redis 在线状态。
2. 客户端发 chat，WS 做参数校验 + msg_id 去重。
3. WS 异步写 Kafka，并返回 ACK。
4. Push 消费后先落 MySQL。
5. 在线则内部 HTTP 推送到目标 WS；离线或推送失败则写离线表。
6. 用户通过 API 拉离线消息，拉取后按 `maxOfflineID` 安全清理。

---

## 本轮结论沉淀

- 已确认事实：见 `docs/PROJECT_MAP.md` 与 `docs/MODULES.md`。
- 待确认问题：见 `docs/QA.md`。

---

## 下一轮学习计划（建议）

1. 先做一次“主链路口述演练”（你用自己的话讲，我来纠偏）。
2. 进入模块深讲：优先 `internal/ws/client.go` + `internal/push/pusher.go`。
3. 补齐你的真实参与记录，再进入简历版落地。
