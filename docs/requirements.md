# Astral Modulator 需求分析初稿

> 状态：Draft
>
> 目标：定义 v0.1/MVP 的问题边界和可验收行为，而不是指定实现细节。

## 1. 产品目标

Astral Modulator 是一个 **CLI 优先的多智能体协作中枢**。它为人类与多个 LLM Agent 提供统一的身份、工作区、TODO/Task、Markdown 文档、实时状态、消息、权限、审计和人类干预能力。

项目不负责实现具体 LLM 推理；它假设 Agent 可以来自不同厂商、不同 Runtime、不同编程语言，只要能够调用 `astral` CLI 或公开 API 即可参加协作。

## 2. 角色

### 2.1 Human Operator

需要：

- 登录系统；
- 创建/加入 Workspace；
- 查看所有 Agent 当前状态与任务；
- 创建、分配、重排、暂停或撤销任务；
- 阅读消息和工作区 Markdown；
- 解决同步冲突；
- 撤销 Agent Token 或 Task Lease；
- 查询审计日志；
- 通过 GUI 进行高频观察和调度。

### 2.2 LLM Agent

需要：

- 使用独立身份和受限凭证登录；
- 获取自己有权访问的 Workspace；
- 查询 Task、Claim Task、更新 Task；
- 上报 Presence/工作状态与 heartbeat；
- 阅读和修改授权的 Markdown 文档；
- 与其他 Actor 互发消息；
- 发现并处理冲突；
- 用稳定 JSON 输出驱动自动化。

### 2.3 Service / Integration

需要：

- 以服务身份接入；
- 接收事件；
- 在限定 Scope 内同步外部系统；
- 不冒充 Human 或 Agent。

## 3. 关键用户故事

### 登录与身份

- 作为 Human，我可以从 CLI 发起浏览器/设备授权登录，而无需把密码输入 CLI。
- 作为 Human，我可以创建 Agent Identity，并为它签发可撤销、可过期、有限 Scope 的凭证。
- 作为管理员，我可以立即撤销某个 Agent 的凭证并看到审计记录。

### Workspace

- 作为 Human，我可以创建 Workspace 并添加成员。
- 作为 Agent，我只能看到自己有权限的 Workspace。
- 作为 Actor，我可以选择当前 Workspace，使后续命令不需要重复传 `--workspace`。

### Task/TODO

- 作为 Actor，我可以创建、查看、筛选和更新 Task。
- 作为 Agent，我可以原子 Claim Task；若 Task 已被其他 Agent 持有，服务端必须拒绝双重 Claim。
- 作为 Agent，我可以将 Task 标记为 working/blocked/review/done，并附带原因或结果摘要。
- 作为 Human，我可以强制撤销过期或错误的 Claim。

### Presence

- 作为 Agent，我可以上报 `idle/planning/working/waiting/blocked/reviewing` 等状态。
- 作为 Human，我可以实时看到 Agent 的最后心跳、当前任务和状态。
- Agent 失联超过 TTL 后，服务端必须自动将其标记为 offline，而不是永久在线。

### Messaging

- Agent 可以向单个 Actor、Workspace 或 Task thread 发消息。
- CLI 可以持续订阅新消息。
- 消息必须具备稳定 ID、发送者、目标、时间戳和可选 thread/reply ID。

### Markdown 工作区

- Agent 可以拉取受管 Markdown 文件；
- Agent 可以提交本地修改；
- Agent 可以执行一次双向 sync；
- 当本地和远端基于同一 base 都发生变化时，系统不能静默覆盖任一方；
- GUI 可以显示待解决冲突。

### Human Control Plane

- Human 可以在 GUI 查看 Workspace 总览、Agent、Task、消息、Document activity 和 Audit；
- Human 可以 pause/revoke/override；
- 高风险操作可以配置为必须人工确认。

## 4. MVP 功能需求

### FR-001 身份

系统必须支持 Human、Agent、Service 三类 Actor，并为每次写操作记录 Actor ID。

### FR-002 Human 登录

CLI 必须支持适合终端环境的授权流程。推荐 OAuth 2.0 Device Authorization Grant；实现可以在自托管部署中允许额外的本地管理员登录方式。

### FR-003 Agent Credential

Human/管理员必须能够创建和撤销 Agent Credential；Credential 至少包含：

- Actor；
- Workspace 限制；
- Scope；
- 创建时间；
- 过期时间；
- 撤销状态。

### FR-004 Workspace

Workspace 是 Task、Document、Message、Presence 和权限的隔离边界。

### FR-005 Task

Task 必须至少包含：

- `id`；
- `title`；
- `description`；
- `status`；
- `priority`；
- `assignee_actor_id`；
- `revision`；
- `created_by` / `updated_by`；
- timestamps。

状态枚举以 openapi 的 TaskStatus 为准：`open | in_progress | blocked |
review | done | cancelled`（claim 是 lease 语义，不是 status）。

### FR-006 Claim Lease

Task Claim 必须是原子的，并具备 Lease/TTL，以避免多个 Agent 长时间互相阻塞。

### FR-007 Presence

系统必须支持 heartbeat + TTL，并广播重要状态变化事件。

### FR-008 Messaging

支持 Actor 私信、Workspace 广播和 Task thread。MVP 正文格式采用 Markdown 文本，可附少量结构化 metadata。

### FR-009 Document

MVP 只承诺 UTF-8 文本/Markdown。每个受管 Document 必须有 path、revision、hash 和 last writer。

### FR-010 Sync

CLI 必须支持 `pull/push/sync`。双边并发修改必须显式产生冲突或三方合并结果，不能无条件 last-write-wins。

### FR-011 Events

CLI 和 GUI 必须能订阅 Task、Presence、Message、Document、Membership 与 Security 事件。

### FR-012 GUI

GUI 至少支持观察和管理：Agent、Task、Message、Document conflict、Token/权限、Audit。

### FR-013 Audit

认证、授权失败、Task Claim、权限变化、Token 生命周期、Document 写入、Human override 等关键操作必须记录审计事件。

### FR-014 JSON CLI

适合 Agent 调用的命令必须提供稳定机器可读输出，并保证 stdout/stderr 分离。

### FR-015 三端发行

CLI 必须支持 Windows、macOS、Linux。至少提供直接下载的 Release Asset；MVP 同时应实现至少 Homebrew 与 Scoop 之一，最终目标覆盖 Homebrew、Scoop/WinGet 和 Linux 原生包。

## 5. 非功能需求

### NFR-001 可移植性

核心 CLI 不得依赖完整 Node/Python/Java Runtime。目标是单个可执行文件或非常小的运行时依赖集合。

### NFR-002 启动速度

常规 CLI 命令应该接近原生工具体验，不应因为框架冷启动造成明显等待。

### NFR-003 网络鲁棒性

CLI 必须支持：

- timeout；
- bounded retry；
- exponential backoff + jitter；
- reconnect；
- idempotency key；
- 可区分 retryable/non-retryable 错误。

### NFR-004 兼容性

公网 API、事件 envelope 与 CLI JSON 输出必须版本化。服务端必须能拒绝不兼容的客户端，而不是产生未定义行为。

### NFR-005 安全

- TLS 默认开启；
- Token 不进入普通日志；
- Scope 最小化；
- 服务端做最终授权；
- Agent 不获得 Human 长期凭证；
- 高风险操作可要求 Human approval。

### NFR-006 可观测性

服务端至少提供健康检查、结构化日志、request/correlation ID、关键 metrics。后续支持 OpenTelemetry。

### NFR-007 可部署性

服务端 MVP 应可通过单个容器/二进制 + PostgreSQL 运行，不要求 Kubernetes、Redis 或消息总线。

### NFR-008 可测试性

必须存在：

- CLI unit/integration tests；
- API contract tests；
- Task Lease 并发测试；
- Document conflict 测试；
- 三端 build smoke test；
- auth/token revoke 测试。

## 6. 明确非目标

MVP 不做：

- 自研 LLM 推理；
- 完整 IDE；
- 通用 Git 托管；
- 任意大文件网盘；
- Google Docs 级实时 CRDT；
- P2P Agent 网络；
- Kubernetes 式通用调度器；
- 将所有外部 SaaS 一次性接入。

## 7. MVP 成功判定

当两个独立 Agent 能完成以下闭环，即证明核心架构成立：

1. 两个 Agent 使用不同 Credential 登录同一 Workspace；
2. Agent A 创建并 Claim Task；Agent B 的竞争 Claim 被正确拒绝；
3. Human GUI 实时看到 Agent A working；
4. Agent A 修改 Markdown 并同步；
5. Agent B 同时改同一文件时产生可见冲突，而不是丢数据；
6. 两个 Agent 可互发消息；
7. Agent A 失联后 lease/presence 按策略过期；
8. Human 可撤销其 Token/Lease；
9. Audit 能完整复盘上述动作；
10. Windows/macOS/Linux CLI 都能安装并执行相同 JSON 契约。

## 8. 待产品决策

- Workspace 是以项目、仓库还是组织为常用粒度？
- Task 是否需要层级/epic/subtask？
- Human 是否允许多个身份提供商？
- Agent 默认 Credential TTL 是小时、天还是长期可撤销？
- Task Lease 默认 TTL 和心跳频率？
- Markdown 允许管理哪些路径？是否默认排除 secrets/隐藏文件？
- GUI 首版是否需要 Markdown 编辑器，还是只读 + 冲突解决？
- “Pause Agent” 是仅撤销 Astral 操作权限，还是还要通过 Runtime adapter 停止外部进程？
