# 脚手架 TODO 登记簿

> 本文件是脚手架阶段的**任务与契约登记中心**。代码内 `TODO(phase-x)` 注释负责
> 局部上下文，本文件负责全局视图：哪些端点/契约/决策还没落地、依据哪份文档、
> 归属哪个 Phase（Phase 划分见 docs/roadmap.md）。
>
> 维护规则：
> - 完成一项就划掉并注明 PR/commit，不要静默删除（保留裁决痕迹）；
> - 新增契约（路径/错误码/事件/ID 前缀/env 变量）必须同步登记到「契约变更登记」；
> - `grep -rn "TODO(phase" server web` 可找到全部代码内待办。

## 0. 已完成

### 第 1 轮（脚手架，2026-09-07 上午）

- Go 模块化单体骨架（chi + GORM + goose + slog），`go build/vet/test` 全绿；
- `api/openapi.yaml` 全量契约 + `api/schemas/{error,event}.json`（Redocly 校验通过）；
- 8 个 goose migration（init/auth/task/tags/documents/outbox/audit/presence）；
- 真实实现：`/.well-known/astral`、`/api/v1/meta/capabilities`、`/healthz`、`/readyz`、
  SSE 事件流端点（keepalive + hub）、错误 envelope、request-id/protocol-version 中间件、
  前缀 ID 生成、公共响应头、CORS 开关；
- Web 脚手架（Vue3+TS+Vite+Pinia+Router），api client/SSE 封装/device 审批页路由，
  `vue-tsc + vite build` 通过；
- CI（server/web/openapi 三 job）、docker-compose 开发库、Makefile。

### 第 2 轮（2026-09-07 下午）：Phase 1 Auth + Phase 2 Workspace + Phase 3 spike 核心 全量实装

- **Auth 全链路**：bootstrap 注册（D6 本地账号）→ web 登录（HttpOnly Cookie）→
  Device Flow（create → web 审批 → 轮询兑换，A1 RFC 8628 语义）→ opaque
  access/refresh（15min/30d，轮换 + 重放检测整族撤销）→ logout；
- **Agent credential**：签发（`astral_` 明文一次性，A4）/ 吊销 / workspace 绑定 /
  scope 强制（403 INSUFFICIENT_SCOPE），`ASTRAL_TOKEN` Bearer 校验；
- **授权**：Authenticate 中间件（Bearer access | Bearer credential | Cookie session
  三来源）+ workspace 级 scope 解析（human 按 role bundle，agent 按 credential scopes）；
- **Workspace 模块**：CRUD、`?name=` 精确解析（init 依赖）、成员管理（owner 提升显式拒绝，
  等 approval 流）、agent identity 管理；
- **Task 模块**：CRUD + 应用层 revision 乐观并发（D5）+ parent 校验/循环检测 +
  **原子 claim**（事务内条件更新，roadmap spike 验收项）+ lease renew/release +
  过期清扫器（发 task.lease.expired）；search/tags 仍为 501 桩（T-task-6/7）；
- **Presence / Message**：heartbeat（TTL 钳制 + offline 派生）+ 发送/列表
  （actor/workspace/task 三 target）；
- **事件**：transactional outbox dispatcher（500ms 轮询 → hub → SSE）；
  关键写路径同事务写 audit + outbox；
- **server_id 固化**：首启写 server_meta，此后以库中值为准（architecture §7）；
- **Web**：登录页、Device 审批页（查询/批准/拒绝）、session store（Cookie 续期 +
  内存 access token + Bearer provider 注入）；
- **测试**：auth 单测（device flow/refresh 重放/credential/cookie）、task 单测
  （claim 竞争唯一成功/租约过期接管/revision 冲突/清扫器）、app HTTP 集成测试
  （spike 全链路 E2E + scope 强制 + 401 边界）、postgres migration 测试
  （CI 注入 DSN，本地自动跳过）；
- **CI**：server job 加 postgres service 跑 migration 测试。

### 第 3 轮（2026-09-07 晚）：astral-cli 对接轮

astral-cli 已落地 v0.1 骨架（login/init/doctor/version 命令面、HTTP/SSE client、
credential store、workspace binding、protocol snapshot 机制；login/init 业务逻辑
仍是桩，等 auth client 接线）。本轮完成：

- **发布 v1 协议快照**到 astral-cli `protocol/snapshots/v1/`
  （openapi.yaml + error/event schema + MANIFEST.json，冻结于 modulator
  commit 75269c4）。CLI 后续 device flow / init 实现即以此为准。
- **openapi↔路由防漂移契约测试**（`server/internal/app/openapi_contract_test.go`）：
  双向比对 openapi 全部 47 个操作与 chi 实际注册路由，任一侧漂移 CI 即失败；
  附错误码枚举 ↔ httpx 常量一致性检查。
- **跨仓库冒烟**：真实二进制上按 CLI contract test 的断言逐字段校验 well-known
  快照形状 + capabilities，全部通过。
- CLI 侧现状核对（无冲突）：kProtocolVersion=1；well-known 字段一致；CLI 仅发
  Authorization 头（其余公共头可选，服务端不强制）。


### 第 4 轮（2026-09-07 深夜）：代码/逻辑/文档 卫生清理

- **去重**：事件发布 4 处重复 envelope 构造统一为 `event.Hub.PublishDomain`
  （workspace/task/presence/message；nil hub 安全）；workspace 级授权前置
  8 处重复模式统一为 `auth.Service.RequireWorkspaceScopes`
  （非成员 404 / scope 不足 403 语义单点定义）。
- **死代码移除**：`audit.Recorder` 死接口、`deviceAuthRow` 别名、
  `ids.MustValidate`（无生产调用）、web `listTasks`（无调用方）。
- **文档清理**：`docs/protocol.md` 瘦身为"OpenAPI 之外的传输/语义约定"
  （删除与 openapi 冲突的 §8-13 端点草案、§6 并发双方案、过时 ID 前缀表）；
  删除 `docs/cli-ux.md`（职责已归 astral-cli 仓库）；
  `docs/deployment.md` 由 17 章 CLI 发行手册瘦身为纯服务端部署
  （CLI 分发归 astral-cli）；MANIFEST/docs 索引/roadmap 同步刷新。

### 第 5 轮（2026-09-07 深夜）：Phase 3 收尾 —— 搜索 / Tags / 事件统一

- **task 搜索实装**（原 501 桩）：语义固定为 权限 → 结构化（parent/tag/status）→
  regex 过滤 → fuzzy 排序 → 分页；实现路径见裁决 D7（Go RE2 + trigram，
  候选集封顶 2000）。`GET /workspaces/{id}/tasks/search` 需要 regex 或 fuzzy 至少其一。
- **tags 两步确认实装**（原 501 桩）：
  - 规范化：trim + NFKC + 小写（`tag.NormalizeName`，唯一性基于规范化名）；
  - propose：确定性重名预检（精确，非模糊相似度）、confirm_code 只存 hash、
    TTL 120s、绑定 actor/workspace/action/name、响应带全量 existing_tags；
  - confirm：单次使用原子置位、code 常数时间比对、同事务复查唯一约束（TOCTOU 兜底）、
    create/rename/delete 一体落地，audit + outbox 同事务；
  - 新增 `TaskTag` GORM 模型（此前只有 SQL 表）。
- **事件路径统一**：全部领域事件经 EmitTx 写 outbox（同事务），dispatcher 投递 hub；
  删除 PublishDomain 直发路径。代价：SSE 事件可见延迟 ≤500ms 轮询间隔（已登记）。
- **capabilities.features** 开放 `task_lease`。
- 事件枚举三处同步（types.go / event.json / web sse.ts）——自动一致性检查列入下轮。

### 第 6 轮（2026-09-09）：SSE 断线重放 + 幂等 + 一致性检查

- **SSE resume 实装**（protocol.md §5 承诺兑现）：
  `Last-Event-ID` 头 / `last_event_id` query 双通道；先订阅缓冲 → outbox 按
  id 升序批量补发（500/批）→ 按游标去重接入实时流；游标超窗（24h）下发
  `snapshot.required` 后断流。超窗判定依赖 UUIDv7 字符串可比性；
  同毫秒乱序的极小概率误去重已注释记录（客户端幂等消费兜底）。
- **outbox 保留窗口清扫**（S1 = 24h，每小时执行）与 **幂等键清理**同批启动。
- **Idempotency-Key 中间件**（T-ws-5）：`internal/idempotency`，actor+endpoint
  (路由模板)+key 主键；仅激活于携带头请求；只缓存 2xx（≤64KB）；并发同键
  依赖主键冲突后回读重放；挂载于鉴权后（公共端点不受影响）。
- **事件枚举一致性检查**（TestEventTypesSync）：types.go ↔ event.json ↔
  web sse.ts 三方互比，CI 防漂移（与路由/错误码检查同属契约门）。
- snapshot.required 纳入 event.json 契约 + web 订阅清单。

## 1. 文档分歧裁决（脚手架已统一，实现时不要再摇摆）

两份文档对同一端点写了不同路径。**api/openapi.yaml 是唯一事实来源**，
以下为裁决结果与理由：

| # | 分歧点 | 裁决 | 理由 |
|---|--------|------|------|
| D1 | device flow 路径 | `POST /api/v1/auth/device/authorizations`、`POST /api/v1/auth/device/authorizations/{device_code}/token` | architecture.md §8.2 与 astral-cli 文档一致（2:1）；protocol.md §8 的 `/auth/device` 废弃 |
| D2 | 身份自检端点 | `GET /api/v1/auth/me` | 同上；protocol.md §8 的 `/auth/session` 废弃 |
| D3 | Tag 确认参数拼写 | `--confirm` | 文档 typo `--comfirm` 已在 architecture §14 明确不沿用 |
| D4 | 乐观并发 | body 内 `expected_revision`（方案 A） | protocol.md §6 MVP 推荐方案 A，CLI JSON/事件流一致性好处理 |
| D5 | revision 自增位置 | **应用层**（UPDATE 里显式 `revision = revision + 1`），不靠 PG 触发器 | 可移植 + 可测（sqlite 测试与 PG 生产行为一致）；migration 00003 的 bump 触发器已删除，同 workspace 父子触发器保留作纵深防御 |
| D7 | task 搜索实现路径 | **Go 侧过滤排序**（RE2 regex + 字符 trigram 相似度），候选集结构化过滤后封顶 2000 行 | RE2 线性时间天然免疫 ReDoS（架构文档担心的 POSIX regex 拖库问题不存在）；MVP 规模下内存排序足够且 sqlite/PG 可移植可测。回迁 pg_trgm/POSIX SQL 的触发条件：单 workspace 任务量到万级或出现搜索延迟 SLO（实现隔离在 task/search.go，语义不变） |
| D6 | Human 浏览器登录 MVP | **本地账号**（email+password, bcrypt）+ HttpOnly Cookie session | Device Flow 需要 Human 在浏览器完成登录才能闭环；OIDC/federation 是后续项（security.md 暂缓清单）。首个 human 账号通过 bootstrap 注册创建（仅当服务器无 human 时开放） |

## 2. 待裁决契约

> 2026-09-07 第 2 轮：A1/A2/A3 已裁决并实现（见下表“状态”）。

| # | 问题 | 裁决 | 状态 |
|---|------|------|------|
| M1 | Memory 工作区公网 API 形状未定稿 | （未裁决）复用 documents 表 + 保留路径前缀，phase-5 前定 | **open** |
| A1 | Device 轮询 pending 语义 | **按 RFC 8628**：token 端点对 pending 返回 `400 AUTHORIZATION_PENDING`、轮询过快返回 `400 SLOW_DOWN`（客户端应退避）；denied→`401 TOKEN_REVOKED` 语义不复用，用 `VALIDATION_FAILED`+details 或专用码见 openapi 注释 | ✅ 已实现 |
| A2 | access token 校验路径 | **每请求查库**（比对 sha256 hash）；MVP 单体延迟可接受；缓存接口后续再加 | ✅ 已实现 |
| A3 | Web 审批页 API | `GET /api/v1/auth/device/authorizations?user_code=`（需 human session）+ `POST .../{id}/approve`、`POST .../{id}/deny` | ✅ 已实现 |
| A4 | ASTRAL_TOKEN 格式 | Agent credential secret 为 `astral_<43字符base64url>` 随机串；服务端按 sha256 hash 查 credentials 表校验；请求头仍为 `Authorization: Bearer astral_...` | ✅ 已实现 |
| T1 | task 删除/取消语义 | （未裁决，phase-3）MVP 暂不提供 DELETE，仅 cancelled 状态 | **open** |
| S1 | snapshot.required 事件与 outbox 保留窗口 | **已裁决并实装**：保留窗口 24h（`event.RetentionWindow`，清扫器每小时清理）；游标超窗下发 `snapshot.required`（reason=cursor_expired）后断流；已纳入 event.json 契约 | ✅ 已实现 |

## 3. Phase 1 — Auth（roadmap Phase 1）

- [ ] device flow 全链路：create → 浏览器审批（web /device 页）→ 轮询兑换
      （`server/internal/modules/auth/module.go`、migration 00002）
- [ ] opaque access/refresh：签发、轮换、重放检测撤族、logout/revoke
- [ ] `server_meta` 固化 server_id（启动时读库覆盖 env；`cmd/astral-server/main.go` 已留 TODO）
- [ ] `Authenticate` 中间件实装并接入 app.router 各写操作组（当前桩阶段放行）
- [ ] `RequireScopes` 实装；scope 常量与 role bundle 已就绪（`auth/scopes.go`）
- [ ] Web HttpOnly Cookie session；`/auth/me` 返回 actor
- [ ] ASTRAL_TOKEN（Agent Bearer）校验路径
- [ ] 审计：登录/授权失败/撤销全记录（`audit.Recorder` 接口已定）
- [x] 裁决并登记 A1/A2/A3（2026-09-07，另新增 A4/D5/D6）

### 3.1 第 2 轮实施清单（2026-09-07，✅ 全部完成）

- [x] T-auth-1 migration：00002 sessions 增 access_token_hash/access_expires_at；
      scopes 从 TEXT[] 改 JSON text；00001 增 human_auth 表
- [x] T-auth-2 tokens.go：opaque token 生成、sha256、常数时间比较
- [x] T-auth-3 password.go：bcrypt 包装 + 强度校验
- [x] T-auth-4 AuthService：register(bootstrap-only)/login/logout/refresh(轮换+重放撤族)/
      device create-approve-deny-exchange(A1 语义)/me/credential 校验(A4)
- [x] T-auth-5 Authenticate 中间件（Bearer access | ASTRAL_TOKEN credential | Cookie session）
- [x] T-auth-6 audit.GormRecorder + redaction（security.md 敏感字段 matcher）
- [x] T-auth-7 server_id 由 server_meta 固化（库中值优先于 env）
- [x] T-auth-8 测试：device flow 全链路、refresh 重放撤族、credential 校验、scope 中间件
- [x] T-auth-9 web：登录页 + /device 审批页实装（A3 API）+ session store 接 /auth/me
- [x] T-auth-10 openapi 增补：/auth/register、/auth/login、approve/deny、新错误码
      AUTHORIZATION_PENDING、SLOW_DOWN；已登记契约变更

### 3.2 Phase 1 剩余（下轮）

- [ ] user_code 防枚举限流（HTTP 层，phase-6 一并做 rate limit）
- [ ] credential/session 撤销后主动断开 SSE 连接（依赖 hub 订阅者携带身份）
- [ ] 认证失败写 audit（Authenticate 中间件当前只拒不记）
- [ ] refresh 家族生命周期上限窗口测试加固（轮换滑动 vs 创建起算的边界用例）

## 4. Phase 2 — Workspace（roadmap Phase 2）

### 4.1 第 2 轮实施清单（✅ 大部分完成）

- [x] T-ws-1 workspace CRUD + `?name=` 精确解析 + 成员管理（创建者自动 owner）
- [x] T-ws-2 agent actor 创建 + credential 签发（明文一次性返回）/撤销
- [x] T-ws-3 workspace/member/credential 变更的审计与事件（credential 事件经 outbox 的
      仅 task 路径；workspace 侧事件当前直发 hub，见 T-task-4 统一计划）
- [x] T-ws-4 测试：CRUD、claim 竞争、scope 强制（app 集成测试覆盖）
- [x] T-ws-5 Idempotency-Key 中间件（`internal/idempotency`：库表存储、24h 窗口、
      仅缓存 2xx、actor+endpoint+key 主键、并发同键回读重放；挂载于鉴权后，
      携带头即激活）
- [ ] T-ws-6 promote_owner approval 状态机（当前显式拒绝，需建 approvals 表）
- [ ] T-ws-7 agents 列表按 workspace 过滤（需 agent-workspace 绑定模型；
      见 workspace/module.go listAgents TODO，审查轮 2026-09-07 登记）

## 5. Phase 3 — TODO 树 / 搜索 / Tags（roadmap Phase 3）

### 5.1 第 2 轮实施清单

- [x] T-task-1 task create/get/list/update（应用层 revision + expected_revision 乐观并发，
      parent 同 workspace 校验 + 循环检测）
- [x] T-task-2 **原子 claim**（roadmap spike 验收项）：事务内条件 UPDATE 抢租约，
      只有一个成功；renew/release
- [x] T-task-3 lease 过期清扫器（发 task.lease.expired）
- [x] T-task-4 outbox → hub dispatcher（业务事务同事务写 outbox，后台轮询投递 SSE；
      task 关键路径已走 outbox，workspace/presence/message 事件仍直发 hub，待统一）
- [x] T-task-5 测试：并发 claim 唯一成功、revision 冲突、lease 过期语义、清扫器事件
- [x] T-task-6 search：regex(RE2)→fuzzy(trigram) 管线 + 游标分页（D7；实现 task/search.go）
- [x] T-task-7 tags proposal/confirm（normalize NFKC+case-fold、confirm_code hash/TTL 120s/
      单次使用、事务内唯一约束复查；新增 tag.created/renamed/deleted 事件）
- [ ] 搜索：pg_trgm 索引 + regex→fuzzy 语义 + statement_timeout 防护（migration 00003 已留位）
- [ ] tag proposal/confirm 两步流（confirm_code hash、TTL、单次使用）
- [ ] capabilities.features 开放首个特性 `task_lease`
- [ ] 裁决并登记 T1

## 6. Phase 4 — Presence / Message / Events（roadmap Phase 4）

- [ ] presence heartbeat + TTL 钳制 + offline 派生
- [ ] messaging（target 三类 + thread + 私信可见性）
- [ ] **outbox dispatcher**：业务事务写 outbox → 轮询/LISTEN → hub.Publish → sent_at
- [ ] SSE resume：Last-Event-ID 重放 + snapshot.required（hub.go/sse.go 已留 TODO）
- [ ] 凭证 revoke 后主动断流（hub 订阅者需携带 actor/credential 标识）
- [ ] 裁决并登记 S1

## 7. Phase 5 — Memory / Document Sync（roadmap Phase 5）

- [ ] manifest/get/push 三方同步 + conflict artifact（禁止 last-write-wins）
- [ ] diff3 合并选型（sync-semantics.md）
- [ ] path canonicalize + 越界/secrets 路径防护
- [ ] memory agent 整理流程 + 人工 review policy（裁决 M1 后开工）

## 8. Phase 6 — Web GUI / Hardening（roadmap Phase 6）

- [ ] 生产模式 server 托管 `web/dist`（同源，去 CORS）
- [ ] GUI 各视图：任务树/Tag 管理/presence/消息/冲突/成员/凭证/审计
- [ ] UI 框架选型（用户标记「待定」；候选评估后写 ADR）
- [ ] rate limit（auth/device 端点优先）+ SSE 重连独立限流
- [ ] config TOML 支持（deployment.md 提到 server.toml）
- [ ] 跨版本 contract test、协议 artifact 发布流程
- [ ] `types.ts` 改为 openapi-typescript 生成；错误码/事件枚举一致性 CI 检查

## 9. 契约变更登记（新增/修改协议时追加）

| 日期 | 变更 | 类型 | 影响端 |
|------|------|------|--------|
| 2026-09-07 | 初始契约基线（openapi 1.0.0-scaffold） | 初版 | CLI/Web |
| 2026-09-07 | 新增 ID 前缀：srv/req/dev/ses/cred/tgp/tag/doc/cfl/prs/aud/apv（protocol.md §3 仅列 7 个） | 补充 | CLI（不得解析前缀语义，无破坏） |
| 2026-09-07 | 新增端点 `GET /workspaces/{id}/tags`（proposal 比对数据源，architecture §14 隐含但未列） | 补充 | CLI/Web |
| 2026-09-07 | 新增端点 `GET /workspaces/{id}/audit`（audit:read scope 与 GUI 需求隐含） | 补充 | Web |
| 2026-09-07 | 新增开发期错误码 `NOT_IMPLEMENTED`（HTTP 501 桩专用，各 Phase 移除） | 临时 | CLI 不得依赖 |
| 2026-09-07 | 新增 env：ASTRAL_HTTP_ADDR/ASTRAL_PUBLIC_URL/ASTRAL_DATABASE_DSN(或 DATABASE_URL)/ASTRAL_SERVER_ID/ASTRAL_AUTO_MIGRATE/ASTRAL_DEV_CORS_ORIGINS/ASTRAL_LOG_LEVEL | 补充 | 部署 |
| 2026-09-07 | 第 2 轮：新增端点 `POST /auth/register`（bootstrap-only）、`POST /auth/login`（web 表单→Cookie session）、`GET /auth/device/authorizations?user_code=`、`POST /auth/device/authorizations/{id}/approve|deny`（A3） | 补充 | Web/CLI |
| 2026-09-07 | 第 2 轮：新增错误码 `AUTHORIZATION_PENDING`、`SLOW_DOWN`（A1，RFC 8628 语义） | 补充 | CLI |
| 2026-09-07 | 第 2 轮：定义 ASTRAL_TOKEN 格式 `astral_<base64url>`（A4）；Bearer 可为 human access token 或 agent credential | 补充 | CLI |
| 2026-09-07 | 第 2 轮：sessions 表 scopes 列由 TEXT[] 改 JSON text（可移植）；tasks revision 自增改应用层（D5） | 内部 | 无（schema 未发布） |
| 2026-09-07 | 第 3 轮：v1 协议快照发布至 astral-cli protocol/snapshots/v1（冻结 modulator@75269c4，含 MANIFEST）；无语义变更 | 发布 | CLI |
| 2026-09-07 | 第 5 轮：新增事件类型 `tag.created`/`tag.renamed`/`tag.deleted`（architecture §14 要求 tag 变更写 outbox；types.go/event.json/web 三处已同步） | 补充 | CLI/Web |
| 2026-09-07 | 第 5 轮：capabilities.features 开放 `task_lease`（原子 claim 稳定并有并发测试覆盖） | 补充 | CLI |
| 2026-09-07 | 第 5 轮：事件路径统一——全部领域事件经 EmitTx outbox（PublishDomain 直发已移除），SSE 事件延迟 ≤ dispatcher 轮询间隔（500ms） | 行为 | CLI/Web |
| 2026-09-09 | 第 6 轮：新增控制事件 `snapshot.required`（SSE resume 超窗，reason=cursor_expired；不在领域事件语义内） | 补充 | CLI/Web |
| 2026-09-09 | 第 6 轮：SSE resume 正式可用（保留窗口 24h，S1 裁决落定）；Idempotency-Key 语义实装（同 actor+endpoint+key 24h 内重放首次 2xx） | 行为 | CLI/Web |

## 10. 对接 astral-cli 的联调清单（避免踩坑）

CLI 仓库开工时按此清单对表，顺序即依赖顺序：

1. `GET /.well-known/astral` — server_id/api_base/protocol_version（已实装 ✅）
2. `GET /api/v1/meta/capabilities` — features 门控（已实装 ✅，features 暂为空）
3. 错误 envelope 解析 — 所有非 2xx（已实装 ✅；`NOT_IMPLEMENTED` 桩仅剩
   task.search 与 tags/memory/document 相关端点）
4. 公共响应头回显 — `X-Astral-Request-Id`/`X-Astral-Protocol-Version`（已实装 ✅）
5. ID 形状 `^[a-z]{2,3}_<uuidv7>` — CLI 只做透传与展示（已实装 ✅）
6. **Device Flow 全链路（已实装 ✅，第 2 轮）**：
   - `POST /auth/device/authorizations` `{"client_type":"cli"}` → 201
     `{device_code, user_code, verification_uri, verification_uri_complete, expires_in:600, interval:3}`
   - CLI 轮询 `POST /auth/device/authorizations/{device_code}/token`：
     pending → `400 AUTHORIZATION_PENDING`(retryable)；过快 → `400 SLOW_DOWN`（退避）；
     成功 → `200 {access_token, token_type:"Bearer", expires_in:900, refresh_token, actor_id}`；
     denied/expired/reused → 401
   - **刷新**：`POST /auth/token/refresh` body `{refresh_token}` → 新对（rotating）；
     旧值重放 → `401 TOKEN_REVOKED` 且整族失效 —— CLI 收到此码必须重新 login
   - 401 处理顺序（astral-cli §13）与服务端行为已对齐：先试 refresh 一次，再重放原请求
7. `POST /auth/logout` body `{refresh_token}` → 204（CLI logout 用）
8. **ASTRAL_TOKEN（A4）**：credential secret 形如 `astral_xxxxx`，直接作
   `Authorization: Bearer` 值；失效返回 401（TOKEN_REVOKED=被吊销 / TOKEN_EXPIRED=过期）
9. **workspace init 语义（已实装 ✅）**：
   - `GET /api/v1/workspaces?name=<exact-or-slug>`：空 items = 不存在或不可见
     （CLI 可提示 `--create`）；命中 → items[0]
   - `POST /api/v1/workspaces` `{name, slug?}` → 201；409 `WORKSPACE_NAME_TAKEN`
   - `GET /api/v1/workspaces/{id}` 校验最终绑定；非成员 404
10. **task claim（已实装 ✅）**：`POST /tasks/{id}/claim`
    `{expected_revision, lease_seconds}` → 200 `{task, lease}`；
    竞争失败 → 409 `TASK_ALREADY_CLAIMED`；过期后需重新 claim
    （renew 对过期租约返回 409 `TASK_LEASE_EXPIRED`）
11. **SSE（已实装 ✅，无重放）**：`GET /api/v1/workspaces/{id}/events`，
    keepalive 注释行 15s；断线期间事件缺失，CLI 需接受快照+增量模型直至 phase-4
    （Last-Event-ID 重放 + snapshot.required）
12. `content_hash` 统一 `sha256:<hex>`（小写十六进制）— phase-5 文档同步联调时最易错

## 11. 下一轮计划

1. approvals 表 + promote_owner 状态机（T-ws-6，高风险动作审批落地）
2. agents 列表按 workspace 过滤（T-ws-7，需 agent-workspace 绑定模型）
3. Web 任务树视图（消费 search API + SSE 实时刷新 + snapshot.required 处理）
4. 凭证/会话撤销后主动断开 SSE 连接（hub 订阅者携带身份；security.md 要求）
5. rate limit（auth/device 端点优先；phase-6）
6. Web device 审批页联调收尾：登录后轮询/自动刷新 pending 请求
