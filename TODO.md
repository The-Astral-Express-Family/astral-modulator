# Astral Modulator 实施总纲（TODO v2）

> **本文件当前的角色**（round 38 收尾更新）：S1-S8 已于 round 38 全部落地
> 验收（501 桩清零、NOT_IMPLEMENTED 错误码移除、四道 repo-hygiene 卫生门通过），
> 本文件回到人工维护模式；后续工作按 §3 触发条件重启。轮次详录见
> [TODO-archive.md](TODO-archive.md)。
>
> **执行模式**：按 §2 阶段顺序一次性完整实现；每阶段收尾跑 §0.2 验证命令
> + §0.4 卫生门，全绿才进入下一阶段；所有设计问题已有结论（§1），
> **不再讨论、不改裁决**；
> 遇本文件与代码/openapi/docs 冲突时以 `api/openapi.yaml` 为准，并把冲突
> 记到 §9 表末备注行。
>
> 实现完成后回到人工维护：勾选 §2 各项，轮次详录写入
> [TODO-archive.md](TODO-archive.md) §A 卷末。历史卷（轮次详录、已完成实施
> 清单、CLI 联调清单、旧登记簿全文）都在 TODO-archive.md，**只增不改**。

## 0. 硬约束与验证命令

### 0.1 不可破坏的约定（违反任何一条即返工）

1. **契约唯一事实来源是 `api/openapi.yaml`**：路径、错误码、响应形状与 openapi
   一字不差；改契约必须两侧同步并由契约门验证。
2. **三个自动契约门**（`server/internal/app/openapi_contract_test.go`）：
   路由↔openapi 双向一致；openapi ErrorCode enum ↔ `internal/httpx/errors.go`
   常量一致；事件类型 `outbox/types.go` ↔ `api/schemas/event.json` ↔
   `web/src/api/sse.ts` 三方一致。**go test 缓存不感知 openapi.yaml 变化**，
   验证必须 `go test ./internal/app/ -count=1`。
3. 所有非 2xx 走 `internal/httpx` 错误 envelope（稳定 `error.code`）。新增错误码
   = 同步改 httpx/errors.go + openapi ErrorCode enum + `api/schemas/error.json`。
4. 业务写操作 = 领域表变更 + audit + outbox **同事务**（三件套）；领域事件一律
   经 `outbox.EmitTx`。
5. schema 变更先写 goose migration（`server/migrations/`）。sqlite 单测不走
   goose：新表/新列必须**同时**加进 `internal/model/model.go` 与
   `internal/testsupport/db.go` 的 AutoMigrate 清单。
6. 生产 schema 禁止 GORM AutoMigrate；migration 必须在 PostgreSQL 方言下可执行。
7. secrets 只来自环境变量 / gitignored `.env`，绝不入库、不进日志（redaction
   matcher 见 docs/architecture.md §23）。
8. `docs/看我看我.md` 人工维护，只读，不修改不删减。
9. 不引入新第三方依赖（所有任务标准库 + 既有依赖可完成）；不引入 UI 组件库（§1.2 U1）。
10. 代码风格与既有模块一致：handler 只做编解码与编排；单行查询走 `store.First`
    单一出口；DTO 与 model 分离（wire 一律 snake_case）。
11. ID 一律经 `internal/ids` 生成；新增前缀须登记 §9。
12. 长度校验口径 = **UTF-8 字节数**（§1.2 L1）；openapi `maxLength` 仅参考展示。

### 0.2 验证命令（每阶段收尾必须全绿）

```bash
cd server && go vet ./... && go test ./... -count=1   # 含三个契约门
make openapi-lint                                      # redocly lint api/openapi.yaml
cd web && npm run build                                # S6-2 起必须
```

gofmt：提交前 `gofmt -w .`；本 Windows 工作区 `gofmt -l` 有 CRLF 假阳性，
以 `git diff` 是否引入格式变更为准。

### 0.3 仓库地图（实现时反复参照）

```text
api/openapi.yaml                    公网契约唯一事实来源（+ schemas/）
server/cmd/astral-server            入口：配置、装配、生命周期
server/internal/httpx               错误 envelope / 公共头 / 分页（ParseLimit/NewPage）
server/internal/modules/<域>/       auth workspace task tag message presence event
                                    audit admin document memory（后三者本轮实装）
server/internal/outbox              领域事件词表 + EmitTx（写侧）
server/internal/model + migrations  GORM 模型 + goose SQL（两者手工同步）
server/tests/                       app 级 HTTP 集成测试（新端点的端到端测试放这里）
web/src/{views,api,stores,...}      Vue3 SPA（axios 统一请求层 + 全局错误 toast）
docs/                               architecture/protocol/sync-semantics/registration/...
```

### 0.4 卫生门（repo-hygiene skill，定期执行）

实现全程按固定节奏调用 **`repo-hygiene`** skill：

- **完整档**：每个 S 阶段收尾时，在 §0.2 验证全绿后追加（skill 的 A+B+C+D
  全部；B 段必须派发只读 subagent 独立复核；P1/P2 当场修复后重跑 A）。
  相邻小阶段可合并过门（S2+S3 一道、S4+S5 一道）；
- **快速档**：阶段内每完成 3-4 个任务、或单个大任务落地后（如 S1-3、
  S1-5、S5-2、S7 每两个视图）；
- **首次完整档（S1 收尾）先做 skill §0 校准**：生成并提交仓库根
  `.hygiene.config.json`（行数阈值 + allowlist）与 `.hygiene-baseline.json`
  （TODO/FIXME/HACK 计数基线）；此后每道门计数不得上升；
- **skill 不可达时降级**（如执行环境未装该 skill）：§0.2 全量命令 +
  `git ls-files | xargs wc -l | sort -n` 行数抽查 + 本次新增文件按 skill
  B 段七条自查 + §4 表内锚点引用 grep 可解析，并在提交信息注明降级；
- 修复纪律遵循 skill §E：发现即修、重构优先于叠加、卫生重构单独成 commit。

## 1. 决策速查（全部已裁决，实现时不再讨论）

### 1.1 历史裁决（D/A/S 系列，一行结论；全文与理由见 TODO-archive.md §D）

| # | 一行结论 |
|---|---|
| D1/D2/D3 | device flow 走 `/auth/device/authorizations` 两端点；身份自检 `/auth/me`；CLI 拼写 `--confirm` |
| D4/D5 | 乐观并发 = body 内 `expected_revision`（409 REVISION_CONFLICT）；revision 应用层自增 |
| D6 | human 浏览器登录 = 本地账号（bcrypt）+ HttpOnly Cookie session；bootstrap 开放首账号 |
| D7 | task 搜索 = Go 应用层 RE2 + trigram，候选集封顶 2000；pg_trgm 仅规模触发后回迁 |
| D8 | approval MVP 允许 owner 自批（单 owner 死锁规避） |
| D9 | agent 归属 = membership 行 ∪ credential 绑定，无第三条路径 |
| D10 | session 绝对上限 90d（创建起算）+ RefreshTTL 30d 滑动轮换 |
| D11 | tag attach/detach 是任务修改：条件 revision bump + task.updated |
| D14 | 全局根 TODO = 默认工作区约定 `default/<user>/todo` |
| D15 | 任务树读取 v2 容器化：`<container>/children` 端点；平面查询归 task-search |
| D16 | 平台角色 admin/user + 全局 scope bundle（GlobalScopesFor），无权限表 |
| A1-A5 | device pending 按 RFC 8628；access token 每请求查库；审批三端点；ASTRAL_TOKEN=`astral_<43字符>`；邀请码注册 |
| S1 | SSE outbox 保留窗口 24h；超窗 `snapshot.required` 断流 |

### 1.2 本轮裁决（round 37，实现按此执行）

| # | 问题 | 结论 |
|---|------|------|
| M1 | Memory 公网 API 形状 | **不设独立 API**。项目记忆 = documents 保留前缀 `memory/**`，经既有 `/workspaces/{id}/documents/*` 端点读写，前缀内改用 memory:read/write scope；组织记忆 = 保留 workspace `org-memory`（bootstrap 注册惰性种子 + owner membership，见 S1-4），此后经邀请/加成员进入；memory agent 编排是客户端行为（architecture §15），服务端不建整理/审阅接口 |
| T1 | task 删除/取消 | MVP **不提供 DELETE task**；取消走 status=cancelled（既有）；cancelled 非硬终态，允许显式改回（乐观并发保护乱序） |
| P1 | push 双改处理 | **不做服务端自动三方合并**（diff3 选型关闭）：base 不匹配一律落 document_conflicts + 409 DOCUMENT_CONFLICT；客户端本地合并后经 resolve(merged/manual) 提交最终内容 |
| P2 | 文档删除 | 版本化 tombstone：`DELETE ?base_revision=N`；远端已变更 → conflict artifact（delete-vs-edit）；对已删行且 base 匹配 → 幂等 204；对 tombstone push 且 base_revision=0 → 复活（revision 续增） |
| P3 | 文档历史版本 | ~~MVP 不建 revision 历史表~~ **第 41 轮解冻落地**（00017）：内容被取代前同事务归档全量快照到 document_versions（kind=push/resolve/revive/delete）；保留窗口 = TTL 30d + 每文档 50 版（ASTRAL_DOC_HISTORY_*，先到即剪）；读端点 /document-versions 列表+详情；恢复走 push + pinned base 不设写端点。触发条件：CLI 缺省 push 读改写语义可静默覆盖他人内容且不产生冲突行 |
| L1 | 长度校验单位 | 口径统一 = UTF-8 字节数。**既有端点上限数值与口径不动**（避免破坏合法输入），新端点一律字节口径并在协议文档标注 |
| E1 | 错误码配对 | 400 VALIDATION_FAILED=形状/约束；403 INSUFFICIENT_SCOPE=权限；409+专用码=状态/唯一性冲突；一次性码错误维持现状（tag confirm 403 / 过期 409）。新端点一律按此表 |
| A1' | message.send 审计 | **豁免**：messages 表自身即完整事实（作者/目标/时间）；audit 只记治理与安全动作（与 architecture §6.10 一致） |
| F1 | fuzzy 0 分行 | fuzzy-only 查询剔除 0 分行（无共享 trigram = 无语义价值）；带 regex/结构化过滤时不过滤 |
| G1 | 分页信封 | 全列表端点统一 `{items,next_cursor}`（契约 round 37 已铺）；presence 恒 null（成员规模有界） |
| U1 | UI 框架 | 维持 Vue3 + 手写组件，不引入组件库（现有 10 视图已成型，引入=重写无收益） |
| X1 | 范围裁剪 | SMTP 邮件邀请、CLI `astral register`（属 astral-cli 仓）、config TOML、pg_trgm 回迁、平台角色可选项全部移出本轮（§3） |

### 1.3 Phase 5 关键既有语义（实装前必读）

- documents/document_conflicts 表结构见 migration 00005；契约形状见 openapi
  documents 段（manifest / get / put / delete / conflicts / resolve 全部已定稿）。
- `content_hash` 统一 `sha256:<hex>`（小写十六进制），基于**原始 UTF-8 bytes**
  重算（不重写换行）；CLI 本地 hash 必须与此一致——双端对接最易错点。
- scope 判定入口 `auth.RequireWorkspace(r, svc, wsID, scope)`；memory/ 前缀 =
  同一端点内按 path 前缀换 scope（单点实现见 S1-4）。
- 事件 `document.updated` / `document.conflict` 已在词表预留（outbox/types.go），
  实装时启用；**不新增事件类型**（resolve 落内容时发 document.updated）。
- org workspace 种子不能放 migration（workspaces.created_by NOT NULL，届时无
  actor），放 bootstrap 注册事务内（S1-4）。
- httpx 已有：`ParseLimit`（缺省 50 上限 200）、`NewPage(items, nextCursor)`、
  `NotFound/Invalid/Conflict` 错误构造（round 38 起 501 桩与 NOT_IMPLEMENTED 已全部移除）。

## 2. 实施计划（S1-S8 按序执行）

> 契约与 501 桩已先行铺好（round 37）。实装 = 替换 501 处理器为真实实现，
> 并**删除 openapi 中对应操作的 "501" 响应行**（契约门强制两侧同步）。
> 每个任务的「验收」是完成的定义；端到端测试放 `server/tests/`（照既有
> 集成测试模式），单元测试放各模块包内。
> 阶段收尾 = §0.2 验证 + repo-hygiene 完整档（§0.4）；阶段内按 §0.4 跑快速档。

### S1 Phase 5 · document 模块实装（最大阶段）

- [x] **S1-0 模型与 migration**：model.go 增 `Document`/`DocumentConflict`
  （列对应 00005 + 新增 deleted_at）；testsupport AutoMigrate 清单补两模型；
  新 migration `00015_documents_tombstone.sql`（documents 加
  `deleted_at TIMESTAMPTZ NULL`）。
  验收：build/test 绿；本地 `docker compose up -d` + `make serve-db` goose up 通过。
- [x] **S1-1 路径 canonicalize**（新文件 `document/paths.go`，单点 helper）：
  URL 解码后按 `/` 切分；拒绝空路径、前导 `/`（绝对路径）、`..` 与 `.` 段、
  反斜杠、NUL/控制字符、空段（连续斜杠）、任一段 >255 字节、总长 >512 字节、
  保留前缀 `.git/`、`.astral/`、`secrets/`、`.env` 开头文件名；非法一律
  400 VALIDATION_FAILED（details.field=path）。表驱动单测覆盖
  sync-semantics §19 的 path 项（traversal / 绝对路径 / 空段 / 保留前缀）。
- [x] **S1-2 manifest + get**：manifest 按 path 升序游标（游标=末行 path），
  limit 缺省 200 上限 1000（不用共享 Limit 参数，契约即如此）；默认排除
  tombstone，`include_deleted=true` 含之（deleted=true）；get 返回 Document
  DTO，不存在 404 NOT_FOUND，tombstone 也返回（deleted=true，同步端侦测远端
  删除的依据）。scope 按 S1-4 前缀规则。
  验收：分页/过滤/scope 前缀两轴/404 单测。
- [x] **S1-3 push + delete**（事务与冲突核心）：
  push（PUT，支持 Idempotency-Key）：服务端重算 sha256，与请求 content_hash
  （若带）不符 → 400；路径不存在且 base_revision=0 → 创建 revision=1
  （audit `document.push` details.created=true）；存在且 revision==base_revision
  且 content_hash==base_hash → 写入 revision+1 + `document.updated`；base 与
  该 revision 的 hash 不一致 → 400（调用方本地状态错乱）；revision 不匹配 →
  插入 document_conflicts（ours_json=push 内容+hash+actor，theirs_json=当前
  内容+hash）+ 409 DOCUMENT_CONFLICT（details 含 conflict_id / current_revision
  / current_hash）+ `document.conflict` 事件 + audit（action=document.push，
  details.conflict_id）；对 tombstone：base_revision=0 → 复活（deleted_at=NULL，
  revision+1），base 不匹配 → 同冲突路径。
  delete（`DELETE ?base_revision=N`，参数缺失 400）：当前未删且 revision 匹配 →
  deleted_at 落地 + revision+1 + `document.updated`(data.deleted=true) + 204；
  已删且 base 匹配 → 幂等 204；不匹配 → 冲突路径（delete-vs-edit）。
  验收：单测覆盖 sync-semantics §19 前八项（local-only / remote-only / dual /
  edit-vs-delete / delete-vs-edit / 重复删 / 复活 / hash 不符）。
- [x] **S1-4 scope 前缀单点 + org workspace 种子**：helper
  `requireDocScope(r, svc, wsID, path, read bool)`——`memory/` 前缀 →
  ScopeMemoryRead/Write，否则 ScopeDocumentRead/Write；S1-2/S1-3 端点统一走它。
  bootstrap 注册分支（无 invite_code）同事务：若 slug=`org-memory` 的
  workspace 不存在则创建（name "Organization Memory"，created_by=新 actor）+
  该 actor owner membership（已有 human 即 403 的守卫保证无并发竞争）。
  验收：前缀 scope 单测（viewer 可读 memory/ 路径但无 document:read 的
  agent 不可读其他路径等，按 RoleToScopes 实际 bundle 设计断言）；bootstrap
  单测断言 org workspace + owner membership 落地。
- [x] **S1-5 conflicts 列表/详情/resolve**：列表 status=open|resolved|all
  （open = resolved_at IS NULL），created_at DESC + id 游标，共享 Limit；
  详情 404 NOT_FOUND，返回 DocumentConflictDetail（列表形状 + 双方全文）；
  resolve：非 open → 409 VALIDATION_FAILED（details.reason=already_resolved）；
  ours → 以 ours_json 落新 revision；theirs → 仅关闭冲突返回当前 Document
  （不 bump revision 不发事件）；merged/manual → content 必填（缺失 400），
  落新 revision；全部同事务写 resolution/resolved_by/resolved_at +
  audit `document.resolve` + `document.updated`（theirs 分支除外）。
  验收：四种 resolution 各一测 + 已解决 409 + 详情 404。
- [x] **S1-6 契约收口**：删除 openapi documents 段全部 "501" 响应行；
  契约门 + redocly 绿；`grep -rn "TODO(phase-5" server` 仅剩已处置项。
- [x] **S1 收尾**：完整档卫生门（§0.4；首次含 skill §0 校准，落
  `.hygiene.config.json` + `.hygiene-baseline.json` 并提交）。

### S2 Phase 5 收口：capabilities

- [x] `GET /api/v1/meta/capabilities` 的 features 开放 `document_sync`
  （task_lease 既有先例）；memory 不开 feature（复用 documents，无独立能力面）。
  验收：capabilities 测试断言 features 含 task_lease + document_sync。

### S3 行为小项

- [x] **S3-1 fuzzy 0 分行剔除**（task/search.go）：fuzzy 非空且无 regex 时过滤
  score==0 行；有 regex 不过滤。验收：单测两分支。
- [x] **S3-2 长度口径登记**：docs/protocol.md §3 补一句「长度上限按 UTF-8
  字节数计，openapi maxLength 仅参考」；巡检新端点（S1）校验均为字节口径。
  既有端点不动（L1）。

### S4 audit 闭环

- [x] **S4-1 workspace 审计实装**：`GET /workspaces/{id}/audit`（audit:read）：
  actor_id/action/outcome 过滤 + 共享 Limit/Cursor，created_at DESC + id 游标；
  AuditEntry DTO 含 request_id；删 openapi 501 行。
- [x] **S4-2 平台审计实装**：`GET /admin/audit`（RequireGlobal +
  ScopePlatformAuditRead，常量与 admin bundle round 37 已加）：workspace_id/
  actor_id/action/outcome 过滤，**含 workspace_id IS NULL 的服务器级记录**。
- [x] **S4-3 认证失败写 audit**（原 §3.2，集中登记处注释在 audit/module.go）：
  auth.Service 注入 audit recorder（构造器加参，app 装配处传入，参照 message
  模块注入模式）；覆盖 login 失败、refresh 重放、device token denied/expired、
  Authenticate 中间件 401（actor 可辨时填 actor_id，否则 NULL；action 用
  auth.login / auth.refresh / auth.device / auth.bearer，outcome=denied）。
  中间件路径无事务用 Record（非 RecordInTx）；量由 S5 限流兜住。完成后移除
  audit/module.go 与 app/router.go 对应 TODO 注释。
  验收：各失败路径单测断言 audit 行 + outcome=denied。
- [x] **S4-4 MeResponse 增 session.expires_at**：Principal 已带 SessionID，
  补过期时间输出；openapi Me schema 同步（auth/module.go 的 TODO(phase-6) 就此关闭）。

### S5 rate limit（§1.2 X1 之外全量实装；protocol.md §6 形状已定）

- [x] **S5-1 限流器**（新包 `internal/ratelimit`，仅标准库）：进程内 token
  bucket，map[key]*bucket + mutex + 惰性清理（最后访问超 10 分钟删除）；
  key：未认证 = 客户端 IP（`X-Forwarded-For` 仅当 `ASTRAL_TRUSTED_PROXY=true`
  采信首跳，默认 false 直连），认证后 = actor_id。
- [x] **S5-2 桶配置与挂载**（env 可覆盖，config.go 增配置 + 部署文档登记）：
  敏感桶 10/min/IP：`/auth/login`、`/auth/register`、
  `POST /auth/device/authorizations`、`GET /auth/device/authorizations`（user_code
  防枚举）、approve/deny、`/auth/token/refresh`；
  轮询桶 60/min/IP：`POST /auth/device/authorizations/{device_code}/token`
  （CLI interval=3s 轮询必须容纳）；
  通用桶 300/min/actor：其余 `/api/v1`；
  SSE 桶 30/min/actor（独立，重连风暴不占通用桶）：events 连接建立。
  超限 429 RATE_LIMITED（httpx 已有码）+ `Retry-After` 秒头。
  env：`ASTRAL_RATELIMIT_SENSITIVE_PER_MIN` / `_POLL_PER_MIN` / `_API_PER_MIN`
  / `_SSE_PER_MIN` / `ASTRAL_TRUSTED_PROXY`。
  验收：单测（桶满 429+Retry-After、窗口恢复、桶间隔离、XFF 开关）；auth/
  tokens.go 与 auth/module.go 的限流 TODO 注释随之关闭。

### S6 工程化与部署

- [x] **S6-1 生产托管 web/dist**（同源去 CORS）：新包 `server/webdist`，
  `//go:embed all:dist`（仓库内 `webdist/dist/.gitkeep` 占位保证 embed 可编译）；
  router：dist/index.html 存在时，非 `/api`、`/.well-known`、`/healthz|readyz`
  的 GET 走静态 + SPA fallback（任何未命中路径回 index.html）；Makefile 增
  `web-dist`（npm run build → 拷贝 web/dist → server/webdist/dist）；
  httpx/middleware.go 与 web/vite.config.ts 的 CORS TODO 注释随之更新。
  验收：make web-dist 后启动，`curl /` 返回 index.html；API 路由不受影响。
- [x] **S6-2 类型生成与契约 CI**：web devDependency 加 `openapi-typescript`；
  package.json script `gen:api` = `npx openapi-typescript ../api/openapi.yaml -o
  src/api/schema.d.ts`；S7 各视图迁移时改用生成类型（types.ts 保留导出别名，
  手工重复类型逐步删除）；CI 增 web job：npm ci → gen:api →
  `git diff --exit-code src/api/schema.d.ts` → npm run build；
  client.ts 客户端版本改 vite define 从 package.json 注入（消除双写）。
  验收：CI 绿；schema.d.ts 与 openapi 一致（diff 门）。

### S7 Web GUI 视图补全（复用 axios 请求层 + 全局错误 toast；不引入组件库）

每个视图的通用验收：`npm run build` 绿；新类型走 schema.d.ts；写清一段
手动验收步骤（登录 → 操作 → 断言）作为任务产出。

- [x] **S7-1 任务树翻页**：TaskTreeView 搜索/树模式消费 next_cursor
  （当前超页静默丢弃，原 §11 #11 尾巴）。
- [x] **S7-2 Tag 管理**：列表 / proposal+confirm / rename / delete / 任务挂载入口。
- [x] **S7-3 Presence 总览**：WorkspaceOverviewView 的 TODO(phase-4) 位。
- [x] **S7-4 消息视图**：workspace 广播 + task thread 聚合展示（原 §6 树形聚合
  项），目标过滤 + 发消息。
- [x] **S7-5 文档与记忆浏览**：manifest 列表 + 内容查看；`memory/` 前缀标注
  （含 org-memory workspace 入口）。
- [x] **S7-6 冲突解决**：conflicts 列表 / 详情双栏对比（ours|theirs）/
  resolve 四选一（merged/manual 需填内容）。
- [x] **S7-7 成员与凭证管理**：成员列表/角色变更/邀请管理（已有）之外补
  agent 凭证签发（明文仅一次展示）/ 撤销卡。
- [x] **S7-8 审计时间线**：workspace audit + admin audit（platform:audit:read
  可见）两入口，actor/action/outcome 过滤。

### S8 收尾与登记

- [x] 逐项勾选本文件 §2；轮次详录写 TODO-archive.md §A 卷末（格式照既有轮次）。
- [x] NOT_IMPLEMENTED 此时应无任何端点返回：从 httpx/errors.go、openapi
  ErrorCode enum、api/schemas/error.json 三处移除（契约门强制同步）。
- [x] 最后一道完整档卫生门（§0.4，含 D 段文档卫生）+ §0.2 全量验证命令绿；
  `.hygiene-baseline.json` 刷新提交；`git status` 干净。

## 3. 明确不做 / 延期清单（不要顺手实现）

| 项 | 状态 | 理由 / 重启条件 |
|---|---|---|
| SMTP 邮件邀请（原 §11 #19 / P4） | 裁剪 | invite_url 人工分发已闭环；出现真实邮件需求再启 |
| CLI `astral register`（#18 / P3） | 移交 astral-cli 仓 | CLI 侧工作；协议快照刷新随其节奏 |
| 服务端自动三方合并（diff3） | 关闭 | §1.2 P1；客户端本地合并后 resolve(merged) 提交 |
| 文档 revision 历史表 | ✅ 第 41 轮落地 | §1.2 P3（触发条件成立后解冻，00017） |
| config TOML（server.toml） | 裁剪 | 环境变量唯一通道，避免双配置源（deployment.md 本就只写了环境变量） |
| pg_trgm / SQL 搜索回迁 | 触发式 | D7：单 workspace 万级任务或搜索延迟 SLO |
| 平台角色可选项（#22：注册开关/全 ws 可见/平台 agent/web 全局 403 出口/CLI me 消费） | 触发式 | D16 延伸清单，出现需求再做 |
| presence 真分页 | 不做 | 成员规模有界，信封统一即可 |
| metrics/tracing、慢订阅者断开、多实例 migration 门 | 触发式 | hub.go / store.go 内 TODO 注释即触发条件 |
| workspace 默认策略/repo metadata 字段 | 延期 | model.go TODO(phase-2)，无消费方 |
| presence upsert ON CONFLICT 化 | 延期 | 现实现正确，无性能触发 |
| managed path config（workspace include/exclude） | 移交 | round 38 R3：客户端约定，astral-cli 仓持有；服务端只保留固定黑名单（sync-semantics §3 已标注归属） |
| 审批面扩展（workspace.delete / credential.create_privileged 等 §22 候选） | 触发式 | round 38 R5：architecture §22 本标"候选"；出现真实高风险操作再启 |
| CLI/发行需求（FR-010/014/015、NFR-001/002/003/008、Distribution MVP） | 移交 | round 38 R6：整体归属 astral-cli 仓，本仓不登记其实现 |
| admin/users 分页 | 契约明示不做 | round 38 R9：契约即注明"平台用户量小，暂不分页"；规模触发再改契约 |
| 开放注册 / 邮箱验证 / 服务器级邀请 / 邀请授 owner | 永不（MVP） | docs/registration.md §8 |

## 4. 代码内 TODO(phase-x) 处置映射

| 位置 | 处置 |
|---|---|
| httpx/respond.go maxBodyBytes 注释 | S1-3 后更新：全局 8MiB 兜底保留，文档 1MiB 上限在 handler 校验（size>1MiB → 400） |
| httpx/middleware.go metrics/tracing | 延期（§3） |
| httpx/middleware.go CORS 生产关闭 | S6-1 随同源托管更新注释 |
| model/model.go workspace 默认策略 | 延期（§3） |
| auth/module.go session 过期时间 | S4-4 |
| auth/module.go 可信代理 | S5（XFF 采信开关） |
| auth/tokens.go user_code 限流 | S5 |
| presence/module.go ON CONFLICT | 延期（§3） |
| event/hub.go dropped 指标 | 延期（§3） |
| store/db.go 多实例 migration | 延期（§3） |
| testsupport/db.go sqlite/PG 差异 | 长期备注，不处置 |
| web/src/api/client.ts 版本双写 | S6-2 |
| web/src/api/types.ts 手工类型 | S6-2 起逐步替换 |
| WorkspaceOverviewView 三个 TODO 注释 | S7-3 / S7-4 / S7-5 完成时删 |
| document / memory / audit / admin 模块桩注释 | S1 / S4 实装时以真实实现注释替换 |

## 9. 契约变更登记（新增/修改协议时追加；历史 40 行见 TODO-archive.md §D）

> 编号沿用历史「§9」：代码与 docs 以「TODO.md §9」「契约变更登记」引用本表；
> §5-§8 编号已随 round 37 重写退役。

| 日期 | 变更 | 类型 | 影响端 |
|------|------|------|--------|
| 2026-09-16 | 第 37 轮：新增端点 `DELETE /workspaces/{id}/documents/{path}`（tombstone 删除，`?base_revision=`）与 `GET /workspaces/{id}/conflicts/{conflict_id}`（冲突详情）；均以 501 桩落地 | 补充 | CLI（phase-5 消费） |
| 2026-09-16 | 第 37 轮：新增端点 `GET /admin/audit`（服务器级审计查询）；新增全局 scope `platform:audit:read`（scopes.go 常量 + admin bundle） | 补充 | Web |
| 2026-09-16 | 第 37 轮：列表信封统一（G1）——documents manifest（limit 上限 1000 + include_deleted）/conflicts（status 过滤）/workspace audit（outcome 过滤）补 `{items,next_cursor}`；presence 补 next_cursor（恒 null）；DocumentConflict 扩展（status/base_hash/双方摘要/resolution 三件）并新增 DocumentConflictDetail；Document/ManifestItem 增 deleted；AuditEntry 增 request_id | 契约文档+补充 | CLI/Web |
| 2026-09-16 | 第 37 轮：裁决落档（无新增 wire）——M1 memory 无独立 API（`memory/` 前缀 + org-memory 保留 workspace）；T1 无 task DELETE；push 双改不做自动合并；message.send 豁免 audit；长度口径=字节（既有端点不动）；fuzzy-only 剔除 0 分行 | 行为登记 | CLI/Web |
| 2026-09-16 | 第 38 轮：documents 七端点 + workspace/admin audit 双端点实装（9 处 501 桩全部替换）；**R1 行为**：push 时同 workspace 存在仅大小写不同路径 → 400 VALIDATION_FAILED（details.reason=path_case_collision） | 实装+行为 | CLI/Web |
| 2026-09-16 | 第 38 轮：**R2 版本协商**——/api/v1 携带 `X-Astral-Client-Version` 且低于 `min_cli_protocol_version` → 400 CLIENT_VERSION_UNSUPPORTED（头缺失放行，protocol.md §2/§7）；ErrorCode enum **移除 NOT_IMPLEMENTED**（error.json 同步，501 清零） | 契约变更 | CLI/Web |
| 2026-09-16 | 第 38 轮：**R7/R8 行为登记**——login 成功不写 audit（sessions 表即事实，与 A1' 同精神）；Idempotency-Key 并发同键进程内互斥（单实例 MVP，多实例边界同 store/db.go 触发条件） | 行为登记 | CLI |
| 2026-09-16 | 第 38 轮：workspace audit 补 401/403 响应文档行；capabilities features += document_sync；S6-2 类型生成入库（web gen:api + CI drift 门）；S6-1 同源托管（webdist go:embed） | 契约文档 | CLI/Web |
| 2026-09-16 | 第 38 轮 G4 补录：`CredentialCreate` 补声明可选 `workspace_id`（授权分流字段——服务端行为本就如此，契约补齐文档；Web MembersView 传当前 ws） | 契约文档 | CLI/Web |
| 2026-09-25 | 第 41 轮：新增端点 `GET /workspaces/{id}/document-versions`（历史版本列表，path 必填 query，revision 降序游标分页）与 `GET /workspaces/{id}/document-versions/{revision}`（单版全量快照）；新增 schema DocumentVersion / DocumentVersionDetail；新增 ID 前缀 `dvh`；新增 env ASTRAL_DOC_HISTORY_MAX_PER_DOC / ASTRAL_DOC_HISTORY_TTL_HOURS；行为：每次内容被取代前同事务归档全量快照（§1.2 P3 解冻） | 补充 | CLI（history/get --revision 后续） / Web |

## 附录 A. 稳定锚点（原 §2 裁决表 / 原 §11 编号清单，编号不变）

> 编号是稳定引用 ID（代码注释与 docs 以「TODO.md §2 A5」「§11 第 N 项」引用），
> 只压缩不删除不重排；原表全文与理由见 TODO-archive.md §D。

### A.1 原 §2 待裁决契约表（终态）

| # | 一行终态 |
|---|---|
| M1 | ✅ 第 37 轮裁决（§1.2）：Memory 无独立 API；`memory/` 前缀 + org-memory 保留 workspace |
| A1 | ✅ device pending 按 RFC 8628（AUTHORIZATION_PENDING/SLOW_DOWN） |
| A2 | ✅ access token 每请求查库（sha256 比对） |
| A3 | ✅ 审批端点 `GET ?user_code=` + approve/deny |
| A4 | ✅ ASTRAL_TOKEN = `astral_<43字符base64url>`，服务端只存 sha256 |
| A5 | ✅ 一次性邀请码注册（P1/P2 已落地；P3 移交 CLI 仓、P4 裁剪，见 §3） |
| T1 | ✅ 第 37 轮裁决（§1.2）：无 DELETE task；cancelled 可复活 |
| S1 | ✅ 保留窗口 24h + snapshot.required（cursor_expired） |

### A.2 原 §11 下一轮计划（编号保留）

1. 【✅ 第 9 轮】D11 tag attach/detach + D9 T-ws-7 + D10 session 上限 + 撤销断流
2. 【✅ 第 10 轮】CI 修复 + 双仓库卫生轮
3. 【✅ 第 11 轮】CLI login/init 实装
4. 【✅ 第 13 轮】Web 任务树视图（第 17 轮重构为逐容器懒加载）
5. 【✅ 第 12 轮】Web/GUI approval 裁决视图 + device 审批页联调收尾
6. 【✅ 第 16/19 轮】CLI todo v2 适配 + tags/msg 命令族
7. 【✅ 第 21 轮】双仓库卫生轮
8. 【✅ 第 22 轮】CLI event listen（SSE 流式消费，JSON Lines + 断线续传）
9. 【→ S5】rate limit（auth/device 端点优先）——第 37 轮已定桶配置
10. 【✅ 第 22 轮】SSE workspace 级授权（安全项）：非成员 404 / scope 不足 403，fail closed
11. 【✅ 第 37 轮契约化 → S1/S4/S7-1】分页统一：四列表端点补 `{items,next_cursor}`
12. 【✅ 第 37 轮裁决】message.send 审计豁免（§1.2 A1'）
13. 【✅ 第 37 轮裁决】长度口径=字节 + 409/403 配对规则（§1.2 L1/E1）→ S3-2
14. 【✅ 第 29 轮 B 线】协议快照 v2.1 刷新；后续增量随 CLI 仓 P3 消费时刷新
15. 【→ S3-1】fuzzy 0 分行剔除（§1.2 F1）
16. 【✅ 第 29 轮】邀请注册 P1（server）
17. 【✅ 第 30 轮】邀请注册 P2（web）
18. 【→ §3 移交 astral-cli 仓】邀请注册 P3：`astral register`
19. 【→ §3 裁剪】邀请注册 P4：SMTP 邮件邀请
20. 【✅ 第 33 轮】平台角色基建（D16）
21. 【✅ 第 34 轮】admin 用户管理
22. 【→ §3 触发式】平台角色后续可选项
23. 【✅ 第 35 轮】web 请求层重构（axios + 全局错误 toast）
