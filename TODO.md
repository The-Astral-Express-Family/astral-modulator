# 脚手架 TODO 登记簿

> 本文件是脚手架阶段的**任务与契约登记中心**。代码内 `TODO(phase-x)` 注释负责
> 局部上下文，本文件负责全局视图：哪些端点/契约/决策还没落地、依据哪份文档、
> 归属哪个 Phase（实施 Phase 划分见 docs/architecture.md §27；roadmap.md 是里程碑视图）。
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

### 第 7 轮（2026-09-09）：代码 / 逻辑 / 文档 卫生轮（双仓库）

- **正确性修复（server）**：
  - message list 私信可见性补 `workspace_id` 收口（跨 workspace 私信泄露）；
    send 对 actor 目标补「本 workspace 可达」校验（成员 或 绑定 credential）；
  - lease 清扫器改条件删除（`expires_at < now`），快照后已续租的行不再被误删；
  - task.release 条件更新检查 RowsAffected，0 行回滚并返回 REVISION_CONFLICT；
  - task.update 业务写与 audit/outbox 合并单事务（原先分离提交，失败窗口会卡死客户端重试）；
  - SSE 补发与实时流重叠期重复投递修复（replay 返回去重前沿）；
  - logout 补桩模式 nil-DB 防护；Recover 中间件改为契约 JSON envelope（带日志）；
  - /api/v1 未知路由 404 改用新码 `NOT_FOUND`（原先返回 retryable=true 的
    INTERNAL_ERROR）；各次级资源 404（成员/凭证/tag/proposal/actor）统一 NOT_FOUND；
  - Claim/Login 区分「查无此行」与「DB 故障」（后者 500 + 日志，不再伪装成 404/401）。
- **去重与死代码**：
  - audit+outbox 序列三种写法并存 → createAgent 改事务内 RecordInTx、workspace.update 补审计；
  - `httpx.NewPage` 统一分页 envelope（删除 task 内 nextCursorPtr/手写 map）；
  - `revisionConflict` / `httpx.NotFound` / `Invalid` / `Conflict` 构造器消除 ~30 处字面量；
  - 新增 `internal/background.RunEvery`，4 份 ticker 循环归一；
  - task 列表 cursor 简化为 id（UUIDv7 时间序，单列比较 sqlite/PG 行为一致，
    替换原 RFC3339Nano 与 sqlite 存储格式不匹配导致的分页序错乱）；
  - EmitTx 双重序列化死代码、`httpx.Page` 死类型、`ids.Validate`、
    模块 Hub/Audit 死字段（task/message/presence/workspace/tag）、presence 手动 TTL 常量
    从 auth.Service 归位各模块；
  - `auth.Refresh` 改条件轮换（并发双刷新不再互相踩踏/误撤族）；
    ExchangeDeviceToken 状态机补 default；
  - presence 主键改 (actor_id, workspace_id)（migration 00010；同 actor 多 workspace
    presence 不再互相覆盖）+ list N+1 修复；
  - credential last_used 更新按分钟节流；RequireWorkspaceScopes 记录底层错误。
- **过时 TODO 注释清理**：config/server_meta、hub/SSE resume、outbox/LISTEN、
  errors.go contract test、types.go codegen、task 包头（均已完成，注释删除或改写）。
- **web**：SSE 重连携带 lastSeenId（原先重连丢事件）；access token 到期前静默续期
  （与注释承诺一致）；types.ts 补齐 4 个漂移错误码并接入 AstralApiError；
  删除死类型（Task/Tag/Lease/Me/register 等）；formatApiError 收敛 4 处复制粘贴；
  WorkspaceOverview 路由参数响应式。
- **astral-cli**：cacheDir 尊重 ASTRAL_HOME；删除死代码 versionString；
  login/logout/whoami 桩骨架合一；randomSuffix+原子写提取 platform/atomic_file；
  parseTargetSpec 接入 normalizeServerUrl（原先只测不用）；sse.cpp 死条件删除；
  openInBrowser 改 fork/exec 消除 shell 拼接面；MANIFEST.json 三处过时描述对齐 revisions。
- **文档**：roadmap.md 里程碑与 architecture §27 的 Phase 编号冲突消除；
  architecture §5/§13 pg_trgm/POSIX 表述对齐 D7；§25 仓库结构对齐实际；
  本文件（TODO.md）勾选实况、修正 Phase 指向与 501 桩清单。
- **协议变更**：见 §9 登记表 2026-09-09 各条（NOT_FOUND、次级资源 404 码、
  task 列表排序/cursor、presence 语义、私信可达性校验）。

### 第 8 轮（2026-09-09）：T-ws-6 approvals 状态机（promote_owner）

- migration 00011_approvals（apv_ 前缀；requested -> approved|rejected|expired -> executed）；
- 端点：POST/GET `/workspaces/{id}/approvals`、POST `/approvals/{id}/approve|deny`
  （openapi 契约 + approval DTO/ApprovalPage；新增错误码 `APPROVAL_EXPIRED`，三方已同步）；
- 语义：MVP 仅开放 `membership.promote_owner`；发起需 workspace:manage_members，
  目标须为非 owner 成员；同 (action,target) 只允许一条 pending；
  裁决仅限 workspace **owner**（maintainer 持 manage_members 亦不可）；TTL 72h 惰性过期；
  **approve 与 promote 同事务**（成员 role 变更 + status=executed + audit + member.changed 事件，
  任一失败整体回滚）；裁决单次使用（条件更新防并发双裁决）；
  D8 裁决：MVP 允许发起人自批（单 owner workspace 的唯一出路；双人裁决列为后续收紧项）；
- addMember/updateMember 的 owner 分支改为 400 引导走 approvals 端点；
- 测试：approve 原子提升 / maintainer 无权裁决 / 裁决单次使用 / 过期 409 /
  owner 目标与非成员目标拒绝。go build/vet/test、redocly lint、路由与错误码契约门全绿。

### 第 9 轮（2026-09-09）：设计复审落地 —— D9/D10/D11 + 撤销断流

> 本轮先做「对计划本身的设计审查」再实施：发现并裁决 4 个规划缺陷（D9/D10/D11
> 及 tag 列表 DTO 缺 workspace_id 的契约漂移），全部闭环后才动代码。

- **D11 tag 关联链路补全**：`PUT/DELETE /tasks/{id}/tags/{tag_id}`（幂等语义见
  openapi 注释）；关联 = 任务修改（条件 revision bump + `task.updated` 事件
  data.tag_change=attach|detach + audit）；Task DTO 增补 `tags`（get/update/claim/
  attach 响应填充，list/search 省略）；tag 包导出统一 `TagDTO`（补 workspace_id，
  修掉与 openapi Tag schema 的漂移）。task_tags 表、search?tag= 过滤自此真实可用。
- **D9 T-ws-7**：不建新绑定模型（推翻原计划前提）；listAgents = membership 行 ∪
  有效 credential 绑定（覆盖存量只发过 credential 的 agent）；createAgent 同事务
  补 role='agent' 成员行——「谁在 workspace」自此只有 membership 一个事实来源。
- **D10 session 绝对寿命**：MaxSessionLife 30d→90d（原与 RefreshTTL 相同，
  上限永不生效）；Refresh 在轮换后若已越界明确拒绝（修掉「签发即过期 session」的
  边界洞）；补 TODO 3.2 拖欠的滑动 vs 创建起算边界测试（89d 过 / 90d+1s 拒）。
- **凭证/会话撤销断流（security.md）**：Hub 订阅携带 actor 身份，`DisconnectActor`
  关闭该 actor 全部 SSE 流；装配层把 auth.OnRevoke 接到 hub（session family 撤销、
  refresh 重放撤族、credential 吊销、logout 四条路径触发）。
- 协议行为变更登记见 §9；openapi 新增 2 个操作（attachTaskTag/detachTaskTag）。

### 第 10 轮（2026-09-09）：CI 修复 + 代码/逻辑卫生轮（双仓库）

- **CI 修复（先导）**：
  - store.Migrate 的 goose 目录参数与 embed FS 根不匹配（`*.sql` 直接嵌在根，
    却找 `migrations/` 子目录），postgres migration test 自引入 embed 起从未真正
    跑过；改指 FS 根并新增无库回归测试（CollectMigrations 锁定 (FS, dir) 组合）；
  - 修复暴露出的 00003 迁移解析失败：plpgsql 函数体加 goose
    StatementBegin/End（分号切分器不识别 $$ 引用）。
- **去重（server）**：4 个模块逐字复制的 `requireWorkspace` 上收为
  `auth.RequireWorkspace`（workspace 模块的三返回值变体语义不同，保留）；
  唯一约束冲突判断 3 份实现（含 "constraint failed" 文案分叉）归一为
  `store.IsUniqueViolation`；`v := x; &x` 取址样板 8 处归一为 `ptr.Of`；
  user_code 与 tag confirm_code 的去混淆字母表+生成循环归一为
  `auth.NewRandomCode`。
- **死代码移除**：workspace.Module.Audit 字段（装配与测试零有效引用）；
  web `ApiErrorEnvelope` 类型、`AstralApiError`/`currentAccessToken` 的多余导出；
  config 的 `getEnv` 纯别名。
- **行为对齐**：RevokeCredential 404 由 `VALIDATION_FAILED` 修正为 `NOT_FOUND`
  （对齐第 7 轮已登记的次级资源 404 裁决，openapi 该端点未文档化错误码，无契约冲突）；
  audit 录入在调用方未显式传值时从请求上下文补 `request_id`
  （列此前恒空）；message.send 的 task 目标查询区分「查无此行 404」与「DB 故障 500」
  （此前一律 404）。
- **构造器收敛**：新增 `httpx.Internal`/`httpx.ConflictWith`，全库 ~40 处
  APIError 字面量改为构造器（带 Retryable 的扩展字面量保留）。
- **web**：session boot/login 的会话建立序列去重（establishSession）。
- **过时 TODO 注释清理**：service.go 撤销断流（第 9 轮已实装）、middleware/router
  的孤儿 phase-2 标签（改指本文件 §3.2）、module.go 的 me() phase 标签。
- **本文件对账**：§3.2/§4.1/§6 中「已完成未勾选」的撤销断流、T-ws-7、
  refresh 窗口测试补勾并注明轮次；§10 桩清单与实际一致。
- **astral-cli**：见该仓库同轮提交（commitlint 放行 protocol、macos-13 摘除、
  严格构建修复、死代码与过时表述清理、MANIFEST 修订链修复、文档对齐）。

### 第 11 轮（2026-09-10）：astral-cli auth 链路实装（消费协议快照 v1）

> 按 §11 第 3 项的设计预审（D12/D13）照图施工，未新增契约、未改 openapi。

- **D12 会话分槽落地**：`credentials.json` 升 v2——human 会话（device flow 产出、
  按 canonical server URL 键）与 agent credential（按 server_id 键）分槽互不混淆；
- **device flow**（A1/RFC 8628）：well-known 协议门 → 创建授权 → 拉起浏览器
  （失败降级手动 URL+user_code，输出走 stderr 保住 --json 单对象契约）→
  轮询兑换（AUTHORIZATION_PENDING/SLOW_DOWN 退避 +5s、401 拒绝、expires_in 超窗 TIMEOUT）；
  传输/等待均为注入 seam，轮询状态机纯逻辑单测覆盖；
- **D13 惰性刷新**：whoami/init 的已认证调用统一经 withLazyRefresh——
  401 时单次轮换并重放，refresh 再 401 即判整族撤销并清除本地会话；
- **login/whoami/logout/init 实装**：init 走 §9.3 状态机（解析 → 发现 →
  ?name= 解析 → --create 可选创建 → GET /workspaces/{id} 可见性校验 →
  绑定写入，已绑定同目标幂等、异目标须 --rebind）；logout 服务端登出尽力而为 +
  本地必清；server 解析统一为 positional > --server > ASTRAL_SERVER；
- **doctor** 增加服务端连通性探测（仅在 env/绑定给出目标时，离线仍是合法状态）；
- 测试 55 项全绿（新增 device flow 状态机 6 项、会话槽往返 2 项）；CI 4 平台全绿。
- CLI 侧对应提交：astral-cli@4cd6a74。

### 第 12 轮（2026-09-10）：Web approval 裁决视图 + device 审批页联调收尾

- **裁决队列视图**（§11 第 5 项）：`/workspaces/{id}/approvals`——待裁决列表
  （GET ?status=requested）+ 批准/拒绝（confirm 后 POST，owner 专用），
  15s 自动刷新、裁决后立即刷新；新增 api/modules/workspace.ts（approvals 三个调用）
  与 Approval/ApprovalPage 类型（手工对齐 openapi）；
- **device 审批页联调收尾**（§11 第 6 项）：pending 状态每 5s 轮询，
  请求在别处被批准/拒绝/过期时页面自动跟进；裁决或终态后停止轮询；
- workspace 总览页增加裁决队列入口。

### 第 13 轮（2026-09-10）：Web 任务树视图（§11 第 4 项）

- **`/workspaces/{id}/tasks` 视图**：
  - 树模式（默认）：list 端点全量分页拉取（limit=200 循环到 next_cursor=null）→
    前端按 parent_id 组树；兄弟按 id（UUIDv7 字典序=时间序）排序；折叠状态按
    任务 id 记忆；孤儿任务（parent 不在集合内，防御性）标「孤儿」徽标；
  - 搜索模式：search 端点（regex 过滤 / fuzzy 排序，可叠加 tag/status；
    客户端强制 regex/fuzzy 至少其一，对齐服务端 400 语义），flat 结果表 +
    score 列 + 「加载更多」（cursor 续页）；
  - 状态过滤（树模式为前端树形过滤，保留命中节点到根的路径并忽略折叠；
    搜索模式下透传给服务端）；
  - 详情侧栏：`GET /tasks/{id}`（tags/lease 仅详情响应填充，D11）——
    tag 徽标、租约 holder/到期、revision、description；
  - **SSE 实时刷新**：task.* 事件 300ms 防抖重载当前模式；snapshot.required
    立即全量重拉（游标超窗语义）；tag.* 刷新打开中的详情；
  - tag 搜索输入带 datalist 联想（GET /workspaces/{id}/tags）；成员列表
    （GET /members）做 actor id → 显示名映射，失败降级显示原始 id；
- api/modules/task.ts 新增（list/search/getDetail）；workspace.ts 补 listTags/
  listMembers；types.ts 补 Task/Lease/Tag/Member/TaskSearchHit（手工对齐
  openapi，注意 list/search 省略 tags、lease 恒 null 的 DTO 差异）；
- workspace 总览页加任务树入口（phase-3 的 TODO 注释兑现删除）；
- 无契约变更（纯消费既有端点）；vue-tsc + vite build 全绿。

### 第 14 轮（2026-09-10）：astral-cli todo 命令族实装（§11 第 8 项）

> CLI 侧按 docs/ARCHITECTURE.md §11 命令面施工，消费协议快照 v1，无契约变更。

- **`astral todo` 六命令**（替换 StubbedNounCommand）：
  - `list`：结构化过滤（--status/--assignee/--parent）+ cursor 分页
    （默认单页，`--all` 跟随 next_cursor 取尽）；
  - `add`：POST create（--parent/--priority/--description/--tag 重复）；
  - `show`：GET /tasks/{id} 详情（tags/lease 展示，人读输出为键值面板）；
  - `claim`：POST /claim {expected_revision, lease_seconds}——不传 --revision 时
    先 GET 当前 revision 再提交（读改写窗口由服务端 409 兜底）；--revision 跳过读取；
  - `done`：PATCH {expected_revision, status:"done"}，同上 revision 语义；
  - `search`：--regex/--fuzzy（至少其一，缺失为 exit 2 USAGE）+ --tag/--status，
    regex/fuzzy 查询串百分号编码（client::urlEncode，libcurl escape）；
- **auth/api 模块（新）**：业务命令公共底座——resolveLocalTarget（flag >
  binding > env）→ ApiSession（well-known 发现 + 鉴权策略：ASTRAL_TOKEN 优先，
  否则 human 会话槽 + 单次惰性刷新 D13；一个命令跑只做一次 discovery）→
  resolveWorkspace（仅 name 时 ?name= 精确解析，空 items = WORKSPACE_NOT_FOUND）；
  throwApiError 按状态映射退出码（401/403→3、404→4、409→5、5xx→6、其余
  4xx→9）；
- **协议错误透传**（兑现 CLI ARCHITECTURE.md §12 拖欠）：AstralError 增加
  protocolCode/requestId/retryable 附加；--json 失败 envelope 对服务端失败输出
  冻结契约 `{"error":{code,message,request_id,retryable}}`（code 如
  TASK_ALREADY_CLAIMED），CLI 本地失败保持本地码；新增 Errc
  NotFound/Conflict/InsufficientScope/Usage；
- 测试 seam：auth::commandHttp() + setCommandTransportForTests（进程级注入，
  供 runApp 级单测脚本化传输）；test_todo_cmd 16 项（分页合并、revision 读取/
  钳定、协议码透传、惰性刷新重放与换新对持久化、撤族清会话、URL 编码、
  无目标 LOCAL_WORKSPACE_ERROR），全仓 71/71 绿；clang-format 通过；
- CLI 侧对应提交：astral-cli@881c919（README/ARCHITECTURE §11/§12 已同步）。

### 第 15 轮（2026-09-10）：v2 server 轮 —— 容器化任务树（D15，破坏性）

- **openapi 2.0.0-scaffold**：移除 `GET/POST /workspaces/{id}/tasks` 与
  `GET /workspaces/{id}/tasks/search`；新增 `GET/POST /workspaces/{id}/children`、
  `GET/POST /tasks/{id}/children`、`GET /workspaces/{id}/task-search`；
  TaskCreate 移除 parent_id；Task schema 增 `children_count`（required），
  `tags` 转 required（恒填充，修订 D11）；redocly lint 过；
- **task 模块**：新 children.go——容器集合统一核心（workspace 容器=根层
  parent_id IS NULL，task 容器=直接子层；参数 status/tag/assignee/limit/cursor；
  id cursor 分页沿用 v1 语义）；创建核心 createTask（容器寻址 + tags-on-create：
  规范化名解析、未知名字 404 整体不创建、关联与 audit/事件同事务）；
  批量填充 enrichTasks（每页各一次 tags/children_count 查询，杜绝 N+1）；
  task-search 守卫放宽为「任一过滤条件」（regex/fuzzy/tag/status/assignee），
  search.go 补 assignee 结构化过滤 + 结果批量填充；
- **顺带修复两个 v1 隐性缺陷**：① tag 模块生产装配从未接 DB（全部 tag HTTP
  端点上线至今 500）——main.go 与测试装配补接；② search 结果行误包在
  `"Task"` 键下（ScoredTask 具名字段无 json tag），与契约内联语义漂移——
  改匿名嵌入内联；
- **protocol_version 1→2**（httpx 常量 + well-known min_cli）；go build/vet/test
  全绿（新增 app 级 TestTaskTreeContainersV2 全链路契约测试）。

### 第 16 轮（2026-09-10）：v2 CLI 轮 —— 快照 v2 + todo 适配（astral-cli）

- **协议快照 v2 发布**至 `protocol/snapshots/v2/`（openapi + schemas +
  well-known protocol_version=2 + MANIFEST：breaking_changes 清单与
  key_semantics 速查；冻结 modulator@0330768）；
- **kProtocolVersion 2**；契约测试指向 v2 快照；
- **todo 命令 v2 语义**：`list` 默认列 workspace 根层集合，`--parent <id>`
  切到该任务 children 集合（URL 寻址取代 parent_id 查询参数），新增 `--tag`；
  `add --parent <id>` 投递进 task 容器（body 不再带 parent_id）；`search` 走
  `/task-search`，守卫放宽为「任一过滤条件」，新增 `--assignee`；
  表格新增 KIDS 列（children_count）；
- 测试同步（URL/协议 pin/search 守卫/容器切换新增用例），74/74 全绿；
  clang-format 过；astral-cli@d7899b8。

### 第 17 轮（2026-09-10）：v2 web 轮 —— 任务树逐容器懒加载

- **TaskTreeView 重写**：根层 = workspace children 集合；展开节点懒拉取该
  任务 children 集合并缓存；重载范围 = 可见集合（根层 + 已展开容器），
  与树规模解耦（第 13 轮全量平铺拉取的 O(N) 问题了结）；
  行内展示 tags 徽标与 children_count 展开列（v2 集合行内恒带）；
- 过滤（status/tag/assignee）服务端生效；搜索模式走 task-search
  （regex/fuzzy/tag/status/assignee 至少其一）；
- SSE：task.* 防抖重载可见集合；snapshot.required 立即重拉；tag.* 重载
  可见集合 + 刷新详情；types.ts/task.ts 对齐 v2 契约（tags/children_count
  required）；vue-tsc + vite build 全绿。

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
| D8 | approval 裁决人约束 | MVP 允许发起人**自批**（审批人须为 owner） | 单 owner workspace 若强制双人裁决，首位新 owner 永远无法产生（死锁）。approval 的 MVP 目标是显式生命周期 + TTL + 可审计，而非双人控制；收紧为「他人裁决」的触发条件：出现多 owner 的生产 workspace 或安全事件 |
| D9 | agent 的 workspace 归属模型 | **不引入新绑定模型**：membership 行（人/agent 通用）与 credential 绑定（agent 专用）即既有两条真实路径；`createAgent` 同事务补 role='agent' 成员行；listAgents 取两路径并集 | 原计划「需 agent-workspace 绑定模型」是第三条平行路径，会让「谁在 workspace 里」出现三种事实来源；membership 本就是人 actor 的归属事实，agent 复用它即可。credential 绑定保持纯授权语义（scope 载体），不承担归属语义 |
| D10 | session 绝对上限 | `MaxSessionLife=90d`（创建起算），`RefreshTTL=30d`（滑动） | 原两者同为 30d，轮换窗口可无限续命，绝对上限永不生效，与 architecture §8.3「生命周期上限从创建时刻算」矛盾。90d 给足跨季度长任务余量，同时封顶被盗 refresh 的最长寿命 |
| D11 | tag 与 task 的关联 | `PUT/DELETE /tasks/{id}/tags/{tag_id}`；关联是任务修改：条件 revision bump + `task.updated` 事件；Task DTO 的 `tags` 仅 get/attach/detach/update 响应填充 | tags 故事在 round 5 只做了「建/删」，attach 链路缺失导致 task_tags 表、search?tag= 过滤、Task.tags 字段全部空转。revision bump 使 tag 变更纳入既有乐观并发与事件流，不新造事件类型 |
| D14 | 全局「根 TODO」的归属 | **默认工作区约定**：`default/<user>/todo`——普通 workspace + membership 权限隔离；不引入「个人工作区」类型；按需显式创建（不做首次使用自动开荒）；`<user>` = 登录用户名（非 usr_id、非可变 display_name） | 任务必须归属协作边界（授权/事件/审计的锚点）；全局任务映射为「个人默认容器」模型零改动。服务端命名空间强制（`default/<user>/*` 仅 `<user>` 可建）列为后续收紧项，触发条件：出现抢注/滥用 |
| D15 | 任务树读取模型（v2 破坏性重构） | **容器化**：凡容器（workspace/task），子任务集合统一为 `GET/POST /workspaces/{id}/children` 与 `GET/POST /tasks/{id}/children`（同参数 status/tag/assignee/limit/cursor，同响应，行内批量填充 tags + children_count）；**移除** `GET/POST /workspaces/{id}/tasks`（同路径改语义=隐性漂移，禁止）；平面查询归 `GET /workspaces/{id}/task-search`（原 search 路径废除；结构化与内容过滤平权，≥1 条件守卫保留，补 assignee）；TaskCreate 移除 parent_id；嵌套树端点**永不建**（将来真需要属纯增量，不破坏 v2） | 「默认=根层、参数=子层、flat=逃生门」让一个集合背三种语义，不优雅；客户端递归只换容器 id。开发期零兼容负担，protocol_version 1→2、快照 v2、CLI/Web 锁步适配；v2 落地前排队中的 CLI tags/msg 暂缓以免白干 |

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

## 3. Phase 1 — Auth（architecture §27 Phase 1）

- [x] device flow 全链路 / opaque access+refresh / Authenticate 中间件 / RequireScopes /
      Web Cookie session / ASTRAL_TOKEN 校验 / server_meta 固化（第 2 轮全量实装，清单见 3.1）
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
- [x] credential/session 撤销后主动断开 SSE 连接（第 9 轮实装：Hub 订阅携带
      actor 身份，auth.OnRevoke → Hub.DisconnectActor，覆盖 session 撤族/
      refresh 重放/credential 吊销/logout 四条路径）
- [ ] 认证失败写 audit（Authenticate 中间件当前只拒不记；需先向 Service
      注入 audit recorder）
- [x] refresh 家族生命周期上限窗口测试加固（第 9 轮随 D10 补齐：89d 过 /
      90d+1s 拒）

## 4. Phase 2 — Workspace（architecture §27 Phase 2）

### 4.1 第 2 轮实施清单（✅ 大部分完成）

- [x] T-ws-1 workspace CRUD + `?name=` 精确解析 + 成员管理（创建者自动 owner）
- [x] T-ws-2 agent actor 创建 + credential 签发（明文一次性返回）/撤销
- [x] T-ws-3 workspace/member/credential 变更的审计与事件（credential 事件经 outbox 的
      仅 task 路径；workspace 侧事件当前直发 hub，见 T-task-4 统一计划）
- [x] T-ws-4 测试：CRUD、claim 竞争、scope 强制（app 集成测试覆盖）
- [x] T-ws-5 Idempotency-Key 中间件（`internal/idempotency`：库表存储、24h 窗口、
      仅缓存 2xx、actor+endpoint+key 主键、并发同键回读重放；挂载于鉴权后，
      携带头即激活）
- [x] T-ws-6 promote_owner approval 状态机（第 8 轮实装：approvals 表 + approve/deny +
      同事务 promote；D8 登记 MVP 允许自批，双人裁决为后续收紧项）
- [x] T-ws-7 agents 列表按 workspace 过滤（第 9 轮随 D9 实装：**推翻原
      「需绑定模型」前提**，不建第三条路径；listAgents = membership 行 ∪
      有效 credential 绑定，createAgent 同事务补 role='agent' 成员行）

## 5. Phase 3 — TODO 树 / 搜索 / Tags（architecture §27 Phase 3）

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
- [x] capabilities.features 开放首个特性 `task_lease`
- [ ] pg_trgm 回迁路径（D7 触发条件：单 workspace 任务量到万级或搜索延迟 SLO；
      语义不变，实现替换点在 task/search.go）
- [x] tag attach/detach 实装（D11；round 5 遗留的关联链路缺口，task_tags 表自此启用）
- [ ] 裁决并登记 T1

## 6. Phase 4 — Presence / Message / Events（architecture §27 Phase 4）

- [x] presence heartbeat + TTL 钳制 + offline 读路径派生（第 2 轮）
- [x] messaging：send/list（target 三类 + thread；第 2 轮；私信跨 workspace 可见性在第 7 轮收紧）
- [x] **outbox dispatcher**：业务事务写 outbox → 轮询 → hub → SSE（第 2 轮；LISTEN/NOTIFY
      待多实例需求出现，见 architecture §19 触发条件）
- [x] SSE resume：Last-Event-ID 重放 + snapshot.required（第 6 轮，S1 裁决落定）
- [x] 裁决并登记 S1
- [x] 凭证 revoke 后主动断流（第 9 轮，见 §3.2 同项）
- [ ] messaging thread 树形聚合视图（GUI/CLI 消费侧，随任务视图一并做）

## 7. Phase 5 — Memory / Document Sync（architecture §27 Phase 5）

- [ ] manifest/get/push 三方同步 + conflict artifact（禁止 last-write-wins）
- [ ] diff3 合并选型（sync-semantics.md）
- [ ] path canonicalize + 越界/secrets 路径防护
- [ ] memory agent 整理流程 + 人工 review policy（裁决 M1 后开工）

## 8. Phase 6 — Web GUI / Hardening（architecture §27 Phase 6）

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
| 2026-09-09 | 第 7 轮：新增错误码 `NOT_FOUND`（HTTP 404，非 retryable）：/api/v1 未知路由，及无专用码的次级资源不存在（成员/凭证/tag/proposal/actor/thread 等）。原先这些场景返回 `VALIDATION_FAILED`（404/409）或 `INTERNAL_ERROR`（未知路由） | 补充 | CLI/Web |
| 2026-09-09 | 第 7 轮：task 列表排序定为 `id DESC`（UUIDv7 创建序，毫秒精度），cursor 即末行 id；search cursor 语义不变 | 行为 | CLI/Web |
| 2026-09-09 | 第 7 轮：presence 语义定为 (actor, workspace) 一行：同一 actor 在多个 workspace 各自心跳，互不覆盖（migration 00010 主键变更） | 行为 | CLI/Web |
| 2026-09-09 | 第 7 轮：私信发送校验收件人「本 workspace 可达」（成员 或 绑定 credential 的 agent）；不可达返回 404 NOT_FOUND | 行为 | CLI/Web |
| 2026-09-09 | 第 7 轮：refresh 并发轮换改条件更新：输家收到 401（token 已被替换），不再误触整族撤销 | 行为 | CLI |
| 2026-09-09 | 第 8 轮：新增端点 POST/GET `/workspaces/{id}/approvals`、POST `/approvals/{id}/approve|deny`（T-ws-6，architecture §22）；新增错误码 `APPROVAL_EXPIRED`（409）；MVP 仅开放 `membership.promote_owner`，approve 同事务执行并重用 workspace.member.changed 事件（data.change=promoted） | 补充 | CLI/Web |
| 2026-09-09 | 第 9 轮：新增端点 PUT/DELETE `/tasks/{id}/tags/{tag_id}`（D11，attach/detach 幂等；attach 响应=Task 含 tags）；task.updated 事件 data 增补 `tag_change`/`tag_id`；session 绝对寿命 90d（超期 refresh 返回 TOKEN_EXPIRED）；撤销（session family/credential/logout）后该 actor 的 SSE 流主动断开 | 行为 | CLI/Web |
| 2026-09-10 | **v2 破坏性重构（D15，定案未实施）**：移除 `GET/POST /workspaces/{id}/tasks` 与 `GET /workspaces/{id}/tasks/search`；新增 `GET/POST /workspaces/{id}/children`、`GET/POST /tasks/{id}/children`、`GET /workspaces/{id}/task-search`；TaskCreate 移除 parent_id；children/task-search 响应行内增补 `tags`（当页批量填充，修订 D11）与 `children_count`；task-search 补 assignee、免 regex/fuzzy 强制（≥1 过滤条件）；protocol_version 1→2 | 破坏 | CLI/Web 锁步适配（快照 v2） |

## 10. 对接 astral-cli 的联调清单（避免踩坑）

CLI 仓库开工时按此清单对表，顺序即依赖顺序：

1. `GET /.well-known/astral` — server_id/api_base/protocol_version（已实装 ✅）
2. `GET /api/v1/meta/capabilities` — features 门控（已实装 ✅，features 暂为空）
3. 错误 envelope 解析 — 所有非 2xx（已实装 ✅；`NOT_IMPLEMENTED` 501 桩仅剩
   document 5 端点（phase-5）与 audit.list（phase-6）；memory 因 M1 未裁决
   尚未注册路由）
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
11. **SSE（已实装 ✅，含断线重放）**：`GET /api/v1/workspaces/{id}/events`，
    keepalive 注释行 15s；`Last-Event-ID` 头或 `last_event_id` query 携带游标，
    保留窗口 24h，超窗收 `snapshot.required`（reason=cursor_expired）后须重拉快照；
    事件消费必须幂等（重放/补发可能重复）
12. `content_hash` 统一 `sha256:<hex>`（小写十六进制）— phase-5 文档同步联调时最易错

## 11. 下一轮计划

> 2026-09-09 设计复审修订：T-ws-7 撤销「需绑定模型」前提（D9，用既有表实现）；
> 新增 D10（session 绝对上限）与 D11（tag 关联链路补全）两项设计修复。
> 优先级原则：先闭合「已宣称完成但实际断链」的功能（D11），再做新面。
> 2026-09-10 API 重构复审（用户主导）：任务读取模型定案容器化 v2（D14/D15，
> 破坏性）；**v2 落地前，排在后面的 CLI tags/msg 命令暂缓**，避免在 v1 上白干；
> 第 13 轮 web 全量拉取与第 14 轮 CLI todo list/search 均在 v2 轮中重构。

1. 【✅ 第 9 轮完成】D11 tag attach/detach + D9 T-ws-7 + D10 session 上限 + 凭证/会话撤销断流
2. 【✅ 第 10 轮完成】CI 修复（goose embed 目录、迁移 StatementBegin/End）+ 双仓库卫生轮
3. 【✅ 第 11 轮完成】CLI login/init 实装（按 D12/D13 预审施工，双仓库端到端闭环；
   端到端联调依赖真实服务器 + 浏览器审批，留待部署环境手动验收）
4. 【✅ 第 13 轮完成】Web 任务树视图（v2 轮中将重构为逐容器懒加载）
5. 【✅ 第 12 轮完成】Web/GUI approval 裁决视图
6. 【✅ 第 12 轮完成】Web device 审批页联调收尾：pending 自动轮询
7. 【✅ 第 14 轮完成】astral-cli todo 命令族（v2 轮中适配容器端点）
8. 【✅ 第 14 轮完成】astral-cli todo 命令族（v1；第 16 轮已适配 v2）
9. 【✅ 第 16 轮完成】v2 CLI 轮（kProtocolVersion=2、容器端点适配、快照 v2）
10. 【✅ 第 17 轮完成】v2 web 轮（任务树逐容器懒加载 + task-search）
11. CLI tags（两步确认）/ msg / event listen 命令（基于 v2）；含 D14 的 CLI
    引导——todo add 无绑定时提示 default/<user>/todo 约定（登录名经 whoami
    可得）
12. rate limit（auth/device 端点优先；phase-6）
13. astral-cli 端到端联调验收（需真实服务器 + 浏览器审批，部署环境手动执行，
    覆盖 login → web 审批 → whoami → init → todo 全链路，按 v2 契约；
    本机无 docker/PG，v2 链路尚未跑过真服务器）
