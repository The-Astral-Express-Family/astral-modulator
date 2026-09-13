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

### 第 18 轮（2026-09-10）：API 冗余清理（外部 agent 实施，本端验收）

- **server**：新增 filters.go——children 集合与 task-search 的结构化过滤
  （status/tag/assignee）收敛为 applyTaskFilters 单一实现（status 校验单点化；
  列名 `tasks.` 前缀约定统一限定，children 的 workspace_id/parent_id 条件
  一并对齐）；loadTaskTags/childCount 改为批量版 tagsForTasks/childCounts 的
  退化调用（tag 行组装与排序只剩一处）；task-search 行组装复用 enrichTasks；
- **openapi**：新增 TaskStatusFilter/TaskTagFilter/TaskAssigneeFilter/
  TaskRegexFilter/TaskFuzzyFilter 组件参数，三个任务端点的内联参数块改 ref
  （过滤定义单点化，契约语义零变化）；
- **web**：apiPath 查询串构造上提 client.ts，core/workspace/task 三个模块
  收敛（原三份手写 URLSearchParams 循环删除）；core.ts 过时 phase-3 TODO 改写；
- 门禁：go build/vet/test、redocly lint、vue-tsc + vite build 全绿；
  零行为变化。

### 第 19 轮（2026-09-10）：CLI tags/msg 命令实装（astral-cli，基于 v2）

- **`astral tags`**（替换桩命令）：`list` 词典；`create <name>` propose →
  人读输出携带完整可复制的确认命令行（`--proposal <id> --confirm <code>`，
  无本地状态，与服务端 code↔actor/workspace/action/name 绑定语义一一对应）；
  `rename <name-or-id> <new>` / `delete <name-or-id>` 先经 tag 词典按名解析
  `target_tag_id`（`tag_` 前缀参数直接作 id；delete 的 confirm name 用服务端
  canonical 名）；
- **`astral msg`**：`send <target> <body> [--thread <id>]`——目标语法
  `workspace`（广播）| `actor:<id>` | `task:<id>`（非法 → USAGE exit 2）；
  发送携带确定性 Idempotency-Key（内容 FNV-1a，重跑同命令 24h 内服务端重放
  首次 2xx 不双发）；`list [--task <id>] [--thread <id>] [--limit] [--all]`
  ——`--task` 走任务线程集合端点，其余走 workspace messages；
- **auth/api 共用件**：`openWorkspace`（目标解析 + 单次 discovery +
  workspace 解析合一；LOCAL_WORKSPACE_ERROR/WORKSPACE_NOT_FOUND 附带 D14
  default/<user>/todo 提示）与 `fetchPageItems`（分页取尽），todo_cmd 同步
  去重；CLI 文档（README/ARCHITECTURE §11）同步；`event listen`（SSE 流式
  消费）列为下一轮；
- 测试：runApp 级脚手架抽至 tests/unit/support/api_fixture.hpp（EnvGuard/
  CwdGuard/FakeApi/ApiFixture 共享，test_todo_cmd 改用），新增
  test_tags_msg_cmd（两步确认流程、target 解析、线程选择、幂等键、D14 提示
  等用例），全仓 85/85 绿；clang-format 过；
- CLI 侧对应提交：astral-cli@0e757da。

### 第 20 轮（2026-09-12）：server task thread messages + actor DTO 修正

- **`GET /tasks/{task_id}/messages` 实装**（phase-4 遗留 501 桩）：task 归属
  校验 + workspace 级 message:read scope；线程按时间正序（阅读序）；
- **actor DTO 契约修正**（E2E 发现）：直接序列化 model.Actor 会漏出大写
  字段名，auth 包统一经 actorDTO（id/kind/display_name）输出；
- CLI 侧对应提交：astral-cli@c1150b9（init 解析 flat Workspace 响应）。

### 第 21 轮（2026-09-12）：双仓库卫生轮 —— 冗余清理 + 补丁化收敛 + 文档重写

> 外部勘察（server/web/cli/docs 四路）+ 本端逐条验收实施。门禁：go
> build/vet/test、gofmt、vue-tsc + vite build、cmake + ctest、clang-format
> 全绿。行为变化均为「对齐已登记裁决」的修正，逐条见 §9 与下文。

- **server：task 模块收敛（补丁化主战场）**
  - task「加载 + 404 + scope 校验」四份实现（requireTask/Claim/Attach/
    Detach）统一为 `LoadForWorkspace`（ctx 介质，可跨模块复用）；handler 侧
    `requireTask` 退化为薄包装；
  - 乐观并发「条件更新 + RowsAffected==0 → 重读 → REVISION_CONFLICT」五份
    拷贝统一为 `bumpRevisionTx`（updates 不含 revision，由其统一 +1）；
    claim 的 409 details.current_revision 由快照值改为事务内重读（并发窗口
    内更准确，正常路径无差异，§9 登记）；
  - 「释放任务归属」字段集 sweep/release 两份拷贝 → `releaseOwnershipFields`；
    parent 存在性 + 同 workspace 校验两份 → `validateParent`（createTask 与
    update 共用）；
  - **Claim 授权内聚服务层**（对齐 AttachTag/DetachTag 同规矩）：handler 去掉
    双重加载，Claim 经 LoadForWorkspace 自带 task:claim 校验——修复「服务层
    可被复用绕过授权」的分层隐患；
  - task get 的 lease 读取不再吞 DB 错误（原先故障呈现为「无租约」200）；
    renewLease 换 `httpx.DecodeJSON`（空 body 仍合法，非法 JSON 如实 400，
    对齐全仓解码纪律）。
- **server：message 模块**
  - `listTaskThread` 复用 `task.LoadForWorkspace`：消除手写 task 加载，
    404 由通用 NOT_FOUND 修正为 **TASK_NOT_FOUND**（task 是端点主语，对齐
    errors.go 既有规则；§9 登记）；
  - message.send 补 thread parent 同 workspace 校验（原先可用他 workspace
    的 thread_id 建立跨 ws 关联；§9 登记）；
- **server：workspace 模块**
  - **T-ws-7/D9 断链闭合**：`listAgents` 此前仍返回服务器全局 agent 列表
    （round 9 已登记完成但代码未实施），现按 D9 实装 membership(role=agent)
    ∪ 有效 credential 绑定的并集去重；
  - addMember/createCredential/revokeCredential 三处「查无此行 vs DB 故障」
    区分补齐（DB 故障不再伪装 404，对齐 round 7 裁决）；
  - listMembers 逐行 First 的 N+1 与静默吞错 → 一次 IN 查询 + 错误上抛
    （对齐 presence）；
- **server：单一来源与构造器**
  - `auth.ActorDTO/ToActorDTO` 导出为全仓单一来源，workspace 模块删除平行
    actorDTO（round 20 修正的巩固，下次契约修正只改一处）；
  - `httpx.Forbidden` / `httpx.Unavailable` 构造器新增，8 处 403/503 字面量
    收敛（readyz、dbOrError、requireHuman、租约/审批/提议非属主等）；
  - tag 列表查询 + DTO 组装两份 → `workspaceTags`；task 加载常量/DTL 常量
    降导出（leaseDefault 等无外部消费）；`boolPtr` → `ptr.Of`；
    presence list 死条件（Find 永不返回 ErrRecordNotFound）删除。
- **server：装配**：message.Module 增 `Tasks` 依赖并在 main/测试装配接线；
  tag 桩模式补 `Auth` 字段（一致性）；router_test 顺手修正 gofmt 对齐。
- **server：过时注释清理**：model.go TaskTag「无 GORM 读写路径」（已有）、
  ids.go 前缀表（protocol.md 引用 + 不存在的 obx）、审计 TODO 四处重复
  （集中登记到 audit/module.go）、hub dropped TODO 双登记（phase 号统一）、
  config.go 桩模式行为描述。
- **web**：
  - `useWorkspaceEvents` composable 抽取（WorkspaceOverview/TaskTree 的 SSE
    生命周期各删 ~15 行）；`lib/format.ts` 收敛 fmtTime 两份拷贝；
  - main.css 新增 `.error-text`/`.notice-text`，7 处内联色值收敛；
  - auth.ts 查询串换 `apiPath`（round 18 收尾）、session.ts bootError 换
    `formatApiError`（对齐「UI 一律经 formatApiError」纪律）；
  - TaskTreeView 删除 tag.* 事件的冗余第二次详情 GET（reloadVisible 内部
    已刷新）、hits 声明上移到使用点之前（round 17 残留）、历史战况注释精简；
  - WorkspaceOverview 头注释对齐实况（presence/消息视图未实装）。
- **astral-cli**（astral-cli@9a912f0）：
  - `output/render.{hpp,cpp}` 新增：scalarOr（3 份拷贝）、truncateUtf8
    （2 份）、printPageJson（4 份 --json 列表 envelope）、printMoreHint
    （分页尾注）收敛；`auth::getJson/sendJson` 消灭 6 处手写 HTTP 请求样板
    （postJson 封装自此有消费方）；
  - **token_provider 模块删除**（自述「do not delete」的策略已被 round 11
    ApiSession 完整接管，全仓零生产调用方）；init/whoami 的手写
    withLazyRefresh 闭包收敛为 `sessionGet/sessionPost`（会话专用身份策略
    与 ApiSession 的 ASTRAL_TOKEN 优先有意分离，注释言明）；
  - **行为修正**：init 的 `?name=` 补 urlEncode（特殊字符 workspace 名
    崩坏）；logout body 改 nlohmann 序列化（不再手拼 JSON）；错误 envelope
    组装统一到 `errorEnvelope`（app.cpp printFailure 复用，可选
    request_id/retryable 字段）；
  - 死代码删除：core::logger()、Painter::enabled()；过时注释修正：
    credential_store 格式（v1 单槽 → v2 双槽实况）、device_flow「snapshot
    v1」、target.hpp 两阶段 discovery（已不存在）、doctor 页脚（探测已
    实装）、registry stub 提示（login/init 已落地）；
  - 测试：test_cli 迁移到 api_fixture（删 ~35 行同构脚手架）、死常量
    kWellKnown 删除、D14 提示用例从 test_tags_msg_cmd 归位 test_todo_cmd；
    85/85 绿、clang-format 过。
- **文档卫生（本仓库）**：roadmap 进度块重写；architecture §7 示例升 v2、
  §20 错误码手抄清单删除改链接（曾连漏 6 码）、§25 仓库树补 ptr/TODO 等、
  §26 兼容声明对齐 D15、§27 pg_trgm 措辞 + approval 归属标注、§14 confirm
  body 补 name、§6.3 补 v2 语境；protocol §2 示例升 v2、§3 分页例外写明；
  requirements FR-005 去 claimed；security §4 scope 手抄表改链接；
  sync-semantics §5/§12 的 .astral/ 布局移交 astral-cli（单一来源）；
  deployment/README 桩模式 501 清单修正 + env 表补 ASTRAL_LOG_LEVEL；
  MANIFEST 补 redocly/LICENSE/.github；docs 索引收录看我看我.md。
- **openapi 卫生**：悬空 `#/components/responses/{NotFound,Conflict}` 引用
  补齐组件（此前 CI lint 未见报，疑 redocly 配置放行——组件现已真实存在）；
  死 `schemas.NotFound`（响应形状误放 schemas）删除；`schemas.Conflict`
  正名 `DocumentConflict`（消除与 responses.Conflict 的同名异物混淆）；
  info/schemas 描述里的 protocol.md 陈旧节号改指 TODO.md 裁决；servers.
  description 写明「路径前缀 ≠ 协议版本」；**Task schema required 补齐
  parent_id/description/priority/assignee_actor_id**（服务端恒序列化、
  web 类型一致，openapi 此前落后于实现；契约文档修正）；TokenPair.
  refresh_token 补「cookie 模式省略」口径。
- **已知遗留（登记为后续项，本轮不做）**：见 §11。

### 第 22 轮（2026-09-12）：SSE workspace 级授权（安全）+ CLI event listen 端到端落地

> §11 第 10 项（安全提级）+ 第 8 项（CLI event listen）同轮闭环；第 21 轮
> 卫生提交验收（补一处 test_cli.cpp 缺 namespace 闭合的编译损坏，
> astral-cli@8b51332）。全程本地双仓库端到端联测（PG 18 临时实例 + 真服务器
> + 真 CLI 流式）。

- **server：`GET /workspaces/{id}/events` 订阅授权**（此前仅要求已认证，
  任何主体可订阅任意 workspace 事件流）：
  - SSEHandler 增 `Auth *auth.Service` 依赖；stream 入口
    `auth.RequireWorkspace(..., workspace:read)`——非成员 404
    WORKSPACE_NOT_FOUND（不泄露存在性）/ scope 不足 403，与其余 workspace
    端点同语义；Auth 未接线 fail closed（503）；main.go 两个分支（有库/
    桩模式）均接线；
  - event 模块测试重构：带授权的流式测试夹具（actor+workspace+member 行 +
    WithPrincipal 注入），新增非成员 404 与 fail-closed 用例；openapi
    events 端点补 404/403 响应；protocol.md §5 补订阅授权语义。
- **CLI：`astral event listen` 实装**（astral-cli c055744 + 两枚 E2E 修复）：
  - `HttpClient::sendStreaming`：chunk 级 sink、无总超时、60s 停滞探测器
    （keepalive 15s 兜底）、非 200 body 缓存供错误 envelope 解析、sink 返
    false 干净中止；
  - `events/listen` 重连循环（FrameParser 之上）：指数退避（1s 起步、30s
    封顶，投递成功即重置）、Last-Event-ID 断线续传、snapshot.required 后
    丢弃过期游标、429 遵循 Retry-After；401 经 withLazyRefresh 每连接一次
    懒刷新重放，403/404 等终态走 throwApiError（协议 envelope 透传 +
    标准退出码）；`--max-events N` 消费满干净退出（控制事件不计入）；
    --json 输出原始 envelope JSON Lines，人读模式输出「时间 类型 ID」；
  - 测试：test_event_cmd 7 用例（脚本化流式 fake：JSON Lines/max-events、
    关流续传游标、跨 chunk 帧完整性、snapshot.required 清游标、传输错误
    退避、5xx 重试 vs 404 终止、人读格式），全仓 92/92 绿。
- **E2E 揪出并修复两枚真 bug（同类：lambda 按引用捕获已亡局部）**：
  - astral-cli@f2cf2f3：makeAttempt 返回的 attempt 链捕获 helper 局部
    （HttpClient/HttpFn），返回即悬垂，首个真实流上崩 INTERNAL
    "string too long"；
  - astral-cli@6f5bf20：LoginSession 仍声明在 else 分支块内被按引用捕获，
    块结束即亡 → 空 Bearer 401 → 空 session 拼出无 scheme 的刷新 URL。
    两枚都是单测 fake 覆盖不到的接线层生命周期错误。
- **端到端联测结论（本地栈，三场景全过）**：
  - A 成员流式：listener --json --max-events 2，`todo add` + `msg send`
    触发 task.created/message.created，JSON Lines 按序各一行，干净退出；
  - B 断线续传：listener 常驻，杀掉 astral-server 再重启，断线窗口内
    创建的 task 经 outbox 按 Last-Event-ID 补发送达，无重复交付；
  - C 越权 404：无成员关系的 workspace 绑定订阅事件流 → 服务端
    WORKSPACE_NOT_FOUND，CLI exit 4 + 协议 envelope 透传（即本轮安全修复
    的黑盒验证）。
- CLI 侧对应提交：astral-cli@c055744、f2cf2f3、8b51332（卫生轮验收修复）。

### 第 23 轮（2026-09-13）：双线并行开发整合（merge origin/main）+ 全栈 E2E 回归

> 远端 modenicheng 在本线 rounds 13-22 期间并行落地了 用户资料系统
> （actors.bio/avatar_url + PATCH /auth/me + web 个人页）、astral-bootstrap
> 交互式向导、godotenv .env 预加载、shadcn-vue reka-nova 主题重构 +
> MainLayout/路由守卫。本轮合并两条线并解决冲突，全栈回归。

- **合并与冲突解决**（10 文件冲突，双侧功能均保留）：
  - **auth DTO 双轨合一**：远端为资料功能引入的私有 `actorDTO` 并入 round 20
    的全仓单一来源 `ActorDTO`（增补 bio/avatar_url；AvatarURL nil→空串），
    workspace 模块成员/agent 响应随之带上资料字段（与 openapi Actor schema
    required 一致，wire 无破坏）；`newActorDTO` 删除，`meBody`（human 邮箱
    查询）保留为 Me 响应组装单点；
  - router：取远端 MainLayout + meta.auth 子路由结构，`workspace-tasks`
    路由补回（auth: required）；5 个冲突视图取远端主题版，总览页补回
    「任务树」入口；error/notice 文本色工具类移植进新 `index.css`
    （TaskTreeView 等仍依赖）；
  - openapi/TODO.md 双侧条目均保留，登记整合记录。
- **验证**：server `go build/vet/test` 全绿（openapi↔路由契约测试含新
  PATCH /auth/me）；web `vue-tsc` + `vite build` 全绿；openapi redocly
  valid；astral-cli 92/92。
- **端到端联测（本地栈：PG 18 临时实例 :5439 + 真服务器 + 真 CLI + 浏览器）**：
  - CLI 全命令面：register/login（设备流 Web API 审批）/whoami/init/
    todo add·list·search(fuzzy)/tags 两步确认/msg send/list 全通；
  - `event listen --max-events 2`：todo add + msg send 触发双事件，
    JSON Lines 按序、干净退出（round 22 场景 A 回归）；
  - SSE 订阅授权回归：匿名 401 / 非成员 404（不泄露存在性）；
  - 整合新面：PATCH/GET /auth/me（bio/avatar_url）；workspace members
    响应含统一 ActorDTO 资料字段；astral-bootstrap 向导全流程（迁移校验、
    server_id 沿用库中值、已有账号跳过、写 .env）；
  - 浏览器黑盒：路由守卫匿名拦截 → 登录（新主题）→ 侧边栏用户菜单 →
    总览 workspace 列表 → 任务树（SSE open、容器懒加载展开出子任务）→
    个人资料页（API 写入的 bio 正确回显）。
- **遗留观察（下轮可处理）**：astral-bootstrap 对 Public URL 缺 URL 形状
  校验（任意字符串可写入 .env）；web 侧 SSE 封装双轨（远端 `useEventStream`
  vs 本线 `useWorkspaceEvents`）可择一收敛；自动化无障碍点击在 reka-ui
  Button 上超时（真用户点击正常，测试基建观察项，非应用 bug）。

### 第 24 轮（2026-09-13）：三仓逻辑拉直轮 —— 单一抽象收编、深嵌套拆平

> 三路并行审查（server Go / web Vue / CLI C++）产出 30 项缠绕点，本轮落地
> 其中影响×安全度最高的一批；每仓库独立提交，全量测试兜底。原则：同一条
> 业务规则/同一段管线只允许一个实现点，分支「决定语义」、尾部统一「执行」。

- **server**（6850bbb）：
  - `requireWorkspace` 删 scopes 死返回值（13 个调用点全部丢弃，`_` 白扛）；
  - idempotency：两处逐字复制的重放块收编为 `replay()`；
  - auth：`authenticateAccessToken`/`authenticateCookieSession` 双胞胎
    （查行→404/500→撤销→过期→Principal）公共管线抽 `liveSession()`，
    差异（列名/过期列/文案）留在各自入口显式可见；
  - `model.ActorsByIDs`：「收集 ID→IN 查询→按 ID 建索引」三份手写
    （workspace members/presence/listAgents）收单点；listAgents 顺带删掉
    多余的收集期去重闭包（IN 按主键天然去重）；
  - `tag.Confirm`：100 行事务闭包、全仓最深 6 层嵌套，拆为
    `loadProposalTx/checkProposal/claimProposalTx/applyTagAction{Create,
    Rename,Delete}`，闭包退化为 5 步直线（加载→校验→单次置位→动作→审计+事件）。
- **web**（bd43640）：
  - SSE 封装双轨合一（第 23 轮遗留项闭合）：`useEventStream` 增
    `onEvent/maxEvents` 选项，删除 `useWorkspaceEvents`（TaskTreeView 迁移，
    订阅时机从 load 末尾提前到 setup，重载本就有 300ms 防抖）；`SseState`
    类型单点化到 `api/sse.ts`；
  - `useApiAction` 删 notice 死代码路径（零调用方；成功提示归 vue-sonner），
    run 收敛 busy/error 两态；
  - `DeviceApproveView`：终态三个写入点收敛为 `enterTerminal` 单点 +
    status→文案映射表；
  - `TaskTreeView` 接入共享件：StatusBadge（map 补 task 六状态）/
    Badge outline/ErrorAlert，删 15 行手写徽章 CSS 与死的 `session.boot()`
    （路由守卫已保证）；`fmtTime` 去重（ApprovalsTable → lib/format）；
    `loginLocation()` 统一 401 出口与路由守卫的登录跳转构造。
- **astral-cli**（086de96）：
  - `HttpClient`：`send`/`sendStreaming` 约 45 行逐行重复的 curl 接线
    （URL 校验/init/header 组装/公共 setopt/清理）收编为 RAII
    `PreparedRequest`，两函数只留差异项（TIMEOUT vs LOW_SPEED 停滞探测、
    body 回调）；清理逻辑随 RAII 覆盖 throw 路径；
  - `events/listen`：三段复制「sleep+growBackoff+continue」合一为单一
    重连尾部，分支只产出 `reconnectIn+diagnose`（429/5xx 文案动词统一，
    无测试断言依赖）；
  - `commands/paging.hpp`：`appendParam/PageFlags/addPageFlags/addPageParams`
    共享件落地，msg_cmd 删手搓 append lambda 与 limit_/all_ 对，todo_cmd
    以 `TaskPageFlags` 组合共享旗标 + status；
  - `tags_cmd`：runMutate 的字符串状态机改 `enum class Action`（协议串经
    `wireName()` 单点转换），嵌套三元 outcome 改 switch。
- **验证**：server `go build/vet/test` 12 包全绿；web `vue-tsc`+`vite build`
  全绿；astral-cli 92/92。
- **已识别未落地（下轮候选，按价值排序）**：
  1. CLI `event_cmd` attempt 组装链（约 8 层 lambda 间接、认证策略与 api.cpp
     重复、`&store/&session` 引用捕获靠作用域约定兜底）收编进
     `ApiSession::sendStreaming`——本轮 listen 循环已动，此项涉及认证接线
     形态，单独成提交；
  2. server「First→NotFound/500」三行样板约 15 处（机械替换面大，收益中）；
  3. server approval 状态机动作表（当前仅 promote_owner 一项，加第二动作前做）；
  4. server `bootstrap.Run` 230 行主流程按段抽取（远端活跃开发中，避免踩线）；
  5. web TaskTreeView 五处手写 try/catch 接入 useApiAction；session.login
     三连请求（login 响应含 Me 却丢弃再 getMe）可省一次往返。

### 第 25 轮（2026-09-13）：任务树视图 UI 重设计（双栏，round13 分支移植）

- **背景**：并行会话曾在 v1 契约上实现过完整任务视图（本地分支
  `round13-task-view-alt`，视觉验收 10/10），因 D15 v2 落地而废弃；本轮把其
  UI 层按 v2 容器契约移植到主线，**数据骨架沿用 round 17 的逐容器懒加载语义
  不变**（reloadVisible/防抖/snapshot.required 原样保留）。
- **数据层**：`api/modules/task.ts` 补写操作（createRoot/createChild/updateTask/
  claim/renew/release/attachTag/detachTag——这些端点 v2 未变）；新增
  `api/taskSource.ts` 数据源缝（mock/真实同签名）+ `lib/mockMode.ts` +
  `mocks/`（v2 内存实现：children 过滤、task-search ≥1 条件守卫、claim/release/
  租约清扫语义）；dev + `VITE_TASKS_MOCK=1` 时任务视图离线可演示（不触发
  真实 API 401 全局登出）；session boot 在「后端不可达/在线未登录」时落演示
  身份（仅 mock 开启时生效，真实会话优先）。
- **视图**：TaskTreeView 从原生 HTML 表格重写为双栏（左缩进树 + 右详情面板）；
  工具栏 shadcn 组件化（fuzzy/regex/assignee/状态/标签；状态与标签变更即时
  重载可见集合）；详情面板全操作——编辑标题/描述、状态/优先级、标签增删、
  认领（时长可选）/续租/释放、新建子任务；REVISION_CONFLICT/租约过期 → 回源
  + toast，不静默覆盖；新建对话框按 v2 寻址（无 parent_id，创建位置由容器
  端点决定，支持按名附带标签），创建子任务后自动展开父容器保证新行可见；
  搜索结果模式带匹配度徽章。
- **顺带关闭** §11 第 13 项的搜索守卫缺口（assignee 纳入 canSearch，与契约
  ≥1 条件语义一致）。
- **验证**：vue-tsc/build 全绿；mock 模式浏览器实测 8 场景自审通过（主视图/
  展开子层/创建对话框/创建后自动展开/搜索/模拟认领/快照重载）；真实 API
  冒烟待有账号的环境点验（置 VITE_TASKS_MOCK=0）。
- **已知未决**：fuzzy-only 搜索出现 0 分行（服务端无阈值语义，UI 忠实呈现，
  见 §11 第 15 项）；next_cursor 消费仍在 §11 第 11 项。


### 第 26 轮（2026-09-13）：任务树 UX 打磨 —— 骨架屏 + 过渡动画 + 事件闪烁

- **骨架屏三处**：整树初始加载（既有）；容器展开时子层骨架挂在容器节点下
  （缓存命中则即时展开不出骨架）；点选任务后详情面板结构化骨架（标题/选择器/
  描述/标签/租约占位）。`expandingIds`/`detailLoading` 分路驱动。
- **过渡动画**：树行 `TransitionGroup` 进出场/重排（leave 用 absolute 让留存行
  立即上移配合 v-move）；搜索结果列表同动画；详情面板 out-in 淡入切换。
  全部带 `prefers-reduced-motion` 降级。
- **事件闪烁**：SSE/模拟事件改任务时，revision 发生变化的可见行底色闪烁 1.1s
  （revision diff 驱动 `.task-row-flash` keyframe）——「实时事件驱动」可感知。
- **mock 注入 250ms 延迟**：骨架/busy 态在演示模式下真实可见（约等于本地 API 往返）。
- **验证**：vue-tsc/build 全绿；浏览器页内轮询实证（展开骨架 max=2、详情骨架
  max=10、闪烁类命中）+ 定时截图自审（事件后子行无丢失/透明卡死）。


### 第 27 轮（2026-09-13）：设备审批页独立布局 + Device Flow 全链路真机验收

- **/device 独立化**：路由从 MainLayout children 提升为顶层（与 /login 同构）——
  CLI 拉起的浏览器窗口不再携带应用外壳；视图补全屏居中容器。守卫链路不变：
  未登录 `/device?code=X` → `/login?from=...` → 登录后原路返回自动查询。
- **终态展示补齐**：lookup 直接命中非 pending（expired/denied/exchanged）也走
  enterTerminal 终态 Alert（原先只显示卡片徽章，与「轮询发现」路径不一致）；
  StatusBadge 补 exchanged variant。
- **全链路真机验收**（收口 §11 第 3 项遗留）：curl 模拟 CLI + 内置浏览器走真实
  审批 UI，9 项全过——create → pending 400 → 未登录重定向 /login 回跳 → 卡片
  自动查询 → 批准终态 Alert → exchange 200（ata_/atr_）→ 重放 401 → /auth/me
  Bearer 200 → 拒绝路径 exchange 401 + denied 终态展示。
- **验收中发现的运行态问题**（留观，不在本轮代码内）：本地 8080 常驻
  astral-server.exe 是旧编译产物（register 响应仍泄漏大写字段，round 20 已修），
  已重启为当前源码，dev 库 bootstrap human 由验收账号占用；`verification_uri`
  在 ASTRAL_PUBLIC_URL 未配置时返回相对路径 `/device`，CLI 需自行拼 base
  （well-known 已有 Host 兜底，device create 尚无）——候选后续小轮。


### 第 28 轮（2026-09-13）：设备登录 VS Code 式直达 —— 验证链接绝对化 + 审批页单步确认

- **verification 链接绝对化**：新增 env `ASTRAL_WEB_BASE_URL`（§9 已登记）——
  device create 的 `verification_uri`/`verification_uri_complete` 按
  WebBaseURL → PublicURL → 请求 Host 回退链拼**绝对 URL**（对齐 openapi
  `format: uri`，round 27 验收遗留项关闭）。CLI 拿到即可直接打开/展示，
  不再二次拼接；dev 下指向 vite 5173。
- **审批页单步确认**：带 `?code=` 直达（CLI 默认路径）时隐藏手动输码框，
  页面只剩「CLI 请求登录」确认卡片 + 批准/拒绝——对齐 VS Code 设备码登录
  体验；无码入口（侧栏）保留手动输码回退。终态/轮询/守卫链路不变。
- **安全取舍**：已登录仍需一次点击批准，不做纯自动批准——防 login-CSRF
  （攻击者诱导已登录浏览器批准攻击者的 device_code，等于把本账号 CLI 凭证
  送给攻击者），与 GitHub/VS Code 设备流一致。
- **验证**：go test（新增 `TestDeviceBaseURL` 回退链断言）+ vue-tsc/build
  全绿；浏览器实测带码直达（无输码框 → 批准 → 终态 → CLI 兑换 200）与
  手动入口双路径；curl 确认 `verification_uri_complete=http://localhost:5173/device?code=…`。

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
| A5 | human 注册形式（多账号进入通道） | **一次性邀请码注册**（2026-09-13 用户裁决）：workspace 绑定的一次性邀请码（human session + `workspace:manage_members` 签发；角色限 viewer/contributor/maintainer，不含 owner），持码者经 web/CLI 注册并同事务建号+入伙；链接 `/register?code=` 为核心分发形式，SMTP 邮件邀请为衍生期；bootstrap 保留为冷启动首账号路径；注册成功即建立 web 会话。设计 docs/registration.md + ADR-0008，实施分期 §11 第 16-19 项 | ✅ P1 server 已实现（第 29 轮）；P2-P4 待做 |
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
| 2026-09-12 | 第 21 轮：`GET /tasks/{id}/messages` 对不存在/不可见 task 的 404 由 NOT_FOUND 修正为 **TASK_NOT_FOUND**（对齐「端点主语用专用码」既有规则，round 20 实装时口径漂移） | 行为 | CLI（错误码分支；CLI 目前不区分） |
| 2026-09-12 | 第 21 轮：message.send 的 `thread_id` 补「parent 必须属于本 workspace」校验，跨 ws thread 引用返回 404 NOT_FOUND（此前可关联他 workspace 线程；读路径可见性子查询本就收口，写侧对齐） | 行为 | CLI |
| 2026-09-12 | 第 21 轮：`GET /workspaces/{id}/agents` 按 D9/T-ws-7 实装 workspace 过滤（membership ∪ 有效 credential 绑定）；此前恒返回服务器全局 agent 列表（round 9 已登记完成但代码未实施的断链） | 行为 | CLI/Web |
| 2026-09-12 | 第 21 轮：openapi Task schema required 补齐 parent_id/description/priority/assignee_actor_id（服务端恒序列化、web 类型一致，契约文档落后于实现的修正，无 wire 变化）；TokenPair.refresh_token 补 cookie 模式省略说明；悬空 responses 组件补齐；死 schemas.NotFound 删除、schemas.Conflict 正名 DocumentConflict | 契约文档 | CLI/Web（无 wire 变化） |
| 2026-09-12 | 第 21 轮：claim 冲突时 409 details.current_revision 改为事务内重读（原为请求开头快照；并发窗口内信息更准，正常路径无差异）；task get 的 lease 查询 DB 故障改 500（原呈现为「无租约」）；renewLease 非法 JSON 改 400（原静默视为空 body）；workspace 三处次级资源 404 补「查无此行 vs DB 故障」区分 | 行为（错误路径） | CLI/Web（正常路径无差异） |
| 2026-09-12 | 第 22 轮：`GET /workspaces/{id}/events` 补 workspace 级订阅授权（非成员 404 / scope 不足 403，与 workspace 端点同语义；此前仅要求已认证，任何主体可订阅任意 workspace 流——§11 第 10 项安全修复）；openapi 补 404/403 响应 | 行为（安全） | CLI/Web |
| 2026-09-12 | 第 10 轮（modenicheng）：actors 表增 `bio`/`avatar_url`（00012，头像仅 http(s) 外链，服务端不抓取）；新增端点 `PATCH /auth/me`（部分更新语义：display_name/bio/avatar_url）；`Me` 响应增 `email`（human 只读）；Actor schema 增 `bio`/`avatar_url`。同时修复 /auth/register、/auth/login、/auth/me 直接序列化 model.Actor 导致字段名 PascalCase 与契约 snake_case 漂移的潜伏 bug（auth 模块引入 actorDTO） | 补充+修复 | CLI/Web |
| 2026-09-13 | 整合轮（merge origin/main）：两侧并行开发的 DTO 双轨合一——第 10 轮引入的私有 `actorDTO` 并入 round 20 的全仓单一来源 `ActorDTO`（增补 bio/avatar_url，构造器仍为 `ToActorDTO`），workspace 模块复用点不变、openapi Actor schema（bio/avatar_url 必填）覆盖两端点；`newActorDTO` 删除 | 内部（重构） | 无（wire 不变，与 Actor schema 契约一致） |
| 2026-09-13 | 第 28 轮：新增 env `ASTRAL_WEB_BASE_URL`——device flow `verification_uri[_complete]` 的 web 控制台基址（回退 PublicURL → 请求 Host）；两链接由相对路径改为绝对 URL（对齐 openapi `format: uri`） | 补充 | CLI（直接打开 verification_uri_complete）/Web |
| 2026-09-13 | 第 29 轮：**邀请注册 P1 落地（A5）**——migration `00013_workspace_invitations`（码只存 sha256，CHECK 限 viewer/contributor/maintainer）；新增 ID 前缀 `inv` | 补充 | CLI/Web |
| 2026-09-13 | 第 29 轮：新增端点 POST/GET `/workspaces/{id}/invitations`、POST `/invitations/{id}/revoke`（幂等 204）；授权=human session + `workspace:manage_members`（agent credential 403）；签发响应含 `code` 明文（仅一次）与 `invite_url`（`{WebBaseURL}/register?code=`，基址回退链与 device 链接同源 `auth.ResolveWebBaseURL`） | 补充 | CLI/Web |
| 2026-09-13 | 第 29 轮：`POST /auth/register` 扩展 `invite_code` 分支（兑换=建号+条件更新抢邀请+入伙+audit invite.redeem/auth.register+outbox 同事务；email 撞车 409 且邀请不消耗）；operationId `registerBootstrap`→`register`；**两分支注册成功即建会话**（Set-Cookie + 响应=Me+session，与 login 同形状） | 行为 | CLI/Web |
| 2026-09-13 | 第 29 轮：新增错误码 `INVITE_INVALID`（400，四种失效统一防探测）、`EMAIL_TAKEN`（409） | 补充 | CLI/Web |
| 2026-09-13 | 第 29 轮：新增事件类型 `security.invite.created/revoked/redeemed`（types.go→outbox 叶子包、event.json、web sse.ts 三方同步；纯增量，protocol_version 不变）。事件类型目录与 EmitTx 写侧移至 `server/internal/outbox`（auth 需在兑换事务内发事件，而 event/sse.go 反向依赖 auth，成环；读侧 hub/SSE/dispatcher 留在 modules/event） | 补充+内部 | CLI/Web |
| 2026-09-13 | 第 29 轮：修复唯一约束冲突在非英文 locale PostgreSQL 上漏判（错误文案随服务器 locale 本地化，`store.IsUniqueViolation` 按 message 匹配失效 → EMAIL_TAKEN/WORKSPACE_NAME_TAKEN 等变 500）：`store.Open` 开 `TranslateError`，判断补 `gorm.ErrDuplicatedKey`（sqlite 单测路径保留 message 兜底）。E2E 真机 PG（中文 locale）验证 | 修复 | Web/CLI（错误码语义恢复契约） |

## 10. 对接 astral-cli 的联调清单（避免踩坑）

CLI 仓库开工时按此清单对表，顺序即依赖顺序：

1. `GET /.well-known/astral` — server_id/api_base/protocol_version（已实装 ✅）
2. `GET /api/v1/meta/capabilities` — features 门控（已实装 ✅，features 暂为空）
3. 错误 envelope 解析 — 所有非 2xx（已实装 ✅；`NOT_IMPLEMENTED` 501 桩仅剩
   document 5 端点（phase-5）与 audit.list（phase-6）；task.messages.list 已于
   第 20 轮实装；memory 因 M1 未裁决尚未注册路由）
4. 公共响应头回显 — `X-Astral-Request-Id`/`X-Astral-Protocol-Version`（已实装 ✅）
5. ID 形状 `^[a-z]{2,3}_<uuidv7>` — CLI 只做透传与展示（已实装 ✅）
6. **Device Flow 全链路（已实装 ✅，第 2 轮）**：
   - `POST /auth/device/authorizations` `{"client_type":"cli"}` → 201
     `{device_code, user_code, verification_uri, verification_uri_complete, expires_in:600, interval:3}`
   - 两个 verification 链接为**绝对 URL**（第 28 轮起，基址 = ASTRAL_WEB_BASE_URL）：
     CLI 默认直接打开/展示 `verification_uri_complete`（带码直达审批页，
     已登录用户一步确认）；手动回退才展示 `verification_uri` + `user_code`
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
> 2026-09-12 第 21 轮（卫生轮）遗留的行为类缺口集中登记于此。

1. 【✅ 第 9 轮完成】D11 tag attach/detach + D9 T-ws-7 + D10 session 上限 + 凭证/会话撤销断流
2. 【✅ 第 10 轮完成】CI 修复（goose embed 目录、迁移 StatementBegin/End）+ 双仓库卫生轮
3. 【✅ 第 11 轮完成】CLI login/init 实装（按 D12/D13 预审施工，双仓库端到端闭环；
   端到端联调已于第 27 轮在本地真机完成人工验收）
4. 【✅ 第 13 轮完成】Web 任务树视图（第 17 轮已重构为逐容器懒加载）
5. 【✅ 第 12 轮完成】Web/GUI approval 裁决视图 + device 审批页联调收尾
6. 【✅ 第 16/19 轮完成】CLI todo v2 适配 + tags/msg 命令族（原第 7/8 条合并）
7. 【✅ 第 21 轮完成】双仓库卫生轮（冗余清理/补丁化收敛/文档重写）
8. 【✅ 第 22 轮完成】CLI event listen（SSE 流式消费，JSON Lines + 断线续传）
9. rate limit（auth/device 端点优先；phase-6）
10. 【✅ 第 22 轮完成】SSE workspace 级授权（安全项提级）：非成员 404 /
    scope 不足 403，fail closed；E2E 黑盒验证过
11. 分页统一（契约债务）：presence/documents manifest/conflicts/audit 四个
    列表端点补 `{items, next_cursor}` envelope（protocol.md §3 已注明例外）；
    web TaskTreeView 的搜索/树模式消费 next_cursor（当前超页静默丢弃）
12. message.send 审计策略裁决：send 目前只写业务行 + outbox，无 audit
    （task/tag/workspace 全为三件套）——裁决「高频消息豁免」或补齐
13. 小项打包：~~web 搜索守卫补 assignee~~（✅ 第 25 轮随 UI 移植关闭：assignee
    已纳入 canSearch）；长度校验 byte vs rune 统一（message/task 按
    字节、tag 按字符）；409/403 搭配 VALIDATION_FAILED 的配对规则裁决
    （approval pending 重复 409、tag confirm_code 403）
15. fuzzy-only task-search 的 0 分行展示策略：服务端无 score 阈值 → UI 出现
    大量「匹配 0%」行（第 25 轮忠实呈现现语义）；裁决「服务端加阈值」或
    「前端过滤/弱化零分行」
14. 协议快照 v2.1 刷新（下次 CLI 消费契约变化时一并）：收拢 round 18 参数
    组件化与本轮 Task required/responses 组件对齐的形态漂移（均无语义变化，
    CLI 契约测试暂 pin 现有 v2 快照不受影响）
16. 【✅ 第 29 轮完成】邀请注册 P1（server，A5，设计 docs/registration.md）：migration 00013
    `workspace_invitations`（id 前缀 `inv`，码只存 sha256）；三端点
    POST/GET `/workspaces/{id}/invitations`、POST `/invitations/{id}/revoke`
    （human session + manage_members；agent credential 403）；register 扩展
    `invite_code` 分支（无码保持 bootstrap-only），兑换事务=建号+条件更新
    邀请+入 membership+audit（invite.redeem/auth.register）+outbox
    （security.invite.created/revoked/redeemed）；错误码 `INVITE_INVALID`
    （四种失效一码防探测）/`EMAIL_TAKEN`；注册成功即建会话（bootstrap
    响应同步补 session）；邀请链接复用 round 28 的 WebBaseURL→PublicURL→Host
    回退链；纯增量 protocol_version 不变；openapi + 单测 + 契约测试，实施时
    契约变更同步登记 §9
17. 邀请注册 P2（web）：`/register` 顶层独立路由（与 /login、round 27 后的
    /device 同构；匿名专属守卫，?code= 预填）；注册成功按 from 跳转；workspace
    侧邀请管理卡（签发/列表/复制链接/撤销，manage_members 可见）
18. 邀请注册 P3（CLI）：`astral register <server> [--invite-code …]`
    （缺省交互提示，--json）；注册只建号，登录仍走设备流（CLI 凭证存储与
    web cookie 是不同通道）；快照消费并入第 14 项 v2.1 刷新
19. 邀请注册 P4（衍生）：SMTP 邮件邀请（.env 增 SMTP_*，邮件含
    `/register?code=` 链接）；邮箱验证策略随本项一并评估（MVP email 仅登录名）

