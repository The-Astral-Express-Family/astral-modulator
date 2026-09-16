# 脚手架 TODO 登记簿

> 本文件是脚手架阶段的**任务与契约登记中心**，只保留「现在需要看」的内容：
> 裁决、各 Phase 剩余项、契约台账、待办。历史（轮次详录、已完成实施清单、
> CLI 联调清单）在 [TODO-archive.md](TODO-archive.md)，只增不改。
> 代码内 `TODO(phase-x)` 注释负责局部上下文，本文件负责全局视图
> （实施 Phase 划分见 docs/architecture.md §27；roadmap.md 是里程碑视图）。
>
> 维护规则：
> - 完成一项就划掉并注明轮次，不要静默删除（保留裁决痕迹）；
> - 轮次详录写进 [TODO-archive.md](TODO-archive.md) §A 卷末，本文件 §0 只加一行索引；
> - §11 条目编号是稳定引用 ID（代码注释与 docs 会以「§11 第 N 项」引用）：
>   完成后压缩为单行保留编号，不删除、不重排；
> - 新增契约（路径/错误码/事件/ID 前缀/env 变量）必须同步登记到「契约变更登记」（§9）；
> - `grep -rn "TODO(phase" server web` 可找到全部代码内待办。

## 0. 已完成（轮次索引）

> 详录（实施明细、撞车裁决、commit 对照）见 [TODO-archive.md](TODO-archive.md) §A；
> 归档不删史，只移不改。

- 第 1 轮（脚手架，2026-09-07 上午）
- 第 2 轮（2026-09-07 下午）：Phase 1 Auth + Phase 2 Workspace + Phase 3 spike 核心 全量实装
- 第 3 轮（2026-09-07 晚）：astral-cli 对接轮
- 第 4 轮（2026-09-07 深夜）：代码/逻辑/文档 卫生清理
- 第 5 轮（2026-09-07 深夜）：Phase 3 收尾 —— 搜索 / Tags / 事件统一
- 第 6 轮（2026-09-09）：SSE 断线重放 + 幂等 + 一致性检查
- 第 7 轮（2026-09-09）：代码 / 逻辑 / 文档 卫生轮（双仓库）
- 第 8 轮（2026-09-09）：T-ws-6 approvals 状态机（promote_owner）
- 第 9 轮（2026-09-09）：设计复审落地 —— D9/D10/D11 + 撤销断流
- 第 10 轮（2026-09-09）：CI 修复 + 代码/逻辑卫生轮（双仓库）
- 第 11 轮（2026-09-10）：astral-cli auth 链路实装（消费协议快照 v1）
- 第 12 轮（2026-09-10）：Web approval 裁决视图 + device 审批页联调收尾
- 第 13 轮（2026-09-10）：Web 任务树视图（§11 第 4 项）
- 第 14 轮（2026-09-10）：astral-cli todo 命令族实装（§11 第 8 项）
- 第 15 轮（2026-09-10）：v2 server 轮 —— 容器化任务树（D15，破坏性）
- 第 16 轮（2026-09-10）：v2 CLI 轮 —— 快照 v2 + todo 适配（astral-cli）
- 第 17 轮（2026-09-10）：v2 web 轮 —— 任务树逐容器懒加载
- 第 18 轮（2026-09-10）：API 冗余清理（外部 agent 实施，本端验收）
- 第 19 轮（2026-09-10）：CLI tags/msg 命令实装（astral-cli，基于 v2）
- 第 20 轮（2026-09-12）：server task thread messages + actor DTO 修正
- 第 21 轮（2026-09-12）：双仓库卫生轮 —— 冗余清理 + 补丁化收敛 + 文档重写
- 第 22 轮（2026-09-12）：SSE workspace 级授权（安全）+ CLI event listen 端到端落地
- 第 23 轮（2026-09-13）：双线并行开发整合（merge origin/main）+ 全栈 E2E 回归
- 第 24 轮（2026-09-13）：三仓逻辑拉直轮 —— 单一抽象收编、深嵌套拆平
- 第 25 轮（2026-09-13）：任务树视图 UI 重设计（双栏，round13 分支移植）
- 第 26 轮（2026-09-13）：任务树 UX 打磨 —— 骨架屏 + 过渡动画 + 事件闪烁
- 第 27 轮（2026-09-13）：设备审批页独立布局 + Device Flow 全链路真机验收
- 第 28 轮（2026-09-13）：设备登录 VS Code 式直达 —— 验证链接绝对化 + 审批页单步确认
- 第 29 轮 B 线（2026-09-13，modenicheng）：CLI 同步轮 —— actor profile 消费 + 协议快照 v2.1 刷新
- 第 30 轮（2026-09-13，Lidozs55）：邀请注册 P2（web）—— /register 页 + 邀请管理卡
- 第 31 轮（2026-09-13，Lidozs55）：server 样板收敛 —— store.First 单一出口
- 第 32 轮（2026-09-13，modenicheng）：侧栏上下文升级 + 演示身份守卫修复
- 第 32 轮 B 线（2026-09-13，Lidozs55）：卫生轮 —— 死代码清除、重复实现收拢、文档校准
- 第 33 轮（2026-09-13，modenicheng）：平台角色基建 + 全局凭证收口
- 第 34 轮（2026-09-13，modenicheng）：admin 用户管理（server + web）
- 第 33 轮 B 线（2026-09-14，Lidozs55）：全仓审查调优轮 —— 代码/逻辑/契约/架构/端点
- 第 36 轮（2026-09-16，Lidozs55）：登记簿瘦身归档 —— 轮次详录/已完成清单/CLI 联调清单移入 TODO-archive.md

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
| D16 | 平台全局角色模型（admin/user/agent 三角色，round 33/34） | **固定平台角色 + 全局 scope 词表**：`actors.platform_role`（human ∈ admin/user；agent/service 固化 kind，CHECK 钉死）；`GlobalScopesFor` 代码内 bundle 与 workspace 轴 `RoleToScopes` 同构，授权一律 `RequireGlobal` 按最终 scope 判定（403 INSUFFICIENT_SCOPE，无 404 分支）。**不建权限表**（psql 式 GRANT/REVOKE 无第三方自定义角色需求，且与 workspace 层两套体系并存）；**不做裸 `role=="admin"` 硬判**（与 scope 哲学割裂，加第二特权角色要逐点改）。首 human 即 admin（bootstrap 同管线）；停用=disabled_at 准入闸门 + 会话撤销加速 | 平台层此前零角色：任意 human 可签发服务器级 credential（代码自标 TODO phase-2）是真缺口。复刻 workspace 轴既有模式成本最低——加新特权=加 scope 常量授予 admin，零 schema 变更；opaque token 每请求查库使角色/停用变更即时生效（对比 JWT claim 需等 access TTL） |

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

✅ 全部落地，无遗留项（原 §4.1 实施清单见归档卷 §B）。

## 5. Phase 3 — TODO 树 / 搜索 / Tags（architecture §27 Phase 3）

### 5.1 剩余

- [ ] pg_trgm 回迁路径（D7 触发条件：单 workspace 任务量到万级或搜索延迟 SLO；
      语义不变，实现替换点在 task/search.go）
- [ ] 裁决并登记 T1

## 6. Phase 4 — Presence / Message / Events（architecture §27 Phase 4）

- [ ] messaging thread 树形聚合视图（GUI/CLI 消费侧，随任务视图一并做）

## 7. Phase 5 — Memory / Document Sync（architecture §27 Phase 5）

- [ ] manifest/get/push 三方同步 + conflict artifact（禁止 last-write-wins）
- [ ] diff3 合并选型（sync-semantics.md）
- [ ] path canonicalize + 越界/secrets 路径防护
- [ ] memory agent 整理流程 + 人工 review policy（裁决 M1 后开工）
- `content_hash` 统一 `sha256:<hex>`（小写十六进制）——文档同步联调最易错点
  （原 §10 第 12 项，随第 36 轮归档迁入）

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
| 2026-09-13 | 第 33 轮：Me 响应（/auth/me、login、register 共用）增 required 字段 `platform_role`（enum admin/user/agent/service，D16）；actor 形状（ActorDTO/成员列表）不带该字段；migration 00014 `actors.platform_role` + `disabled_at`（纯增量，protocol_version 不变） | 补充 | Web/CLI（CLI 纯增量兼容，`astral me` 后续可选消费） |
| 2026-09-13 | 第 33 轮：行为收口——签发/吊销不绑定 workspace 的服务器级 credential 由「任意已注册 human」改为 `platform:credentials:manage`（admin 专属，403 INSUFFICIENT_SCOPE）；绑定 workspace 的路径不变（agent:manage） | 行为（破坏性收紧） | CLI（此前凭 human token 可发的用法被拒） |
| 2026-09-13 | 第 34 轮：新增端点 `GET /admin/users`、`POST /admin/users/{id}/disable|enable`、`POST /admin/users/{id}/role`（body `{platform_role: admin\|user}`，返回 AdminUser）；新 schema `AdminUser`（含 email/disabled_at）；新增 scope 词表 `platform:users:read`/`platform:users:manage`/`platform:credentials:manage`（admin 角色专属，D16）；停用后该 actor 全部会话/凭证 401 `TOKEN_REVOKED`（account disabled） | 补充 | Web（CLI 不消费 /admin/*） |
| 2026-09-14 | 第 33 轮 B 线：**messages 两个列表端点落实已声明的分页**——`GET /workspaces/{id}/messages`（id 游标 DESC，此前恒 `Limit(100)` 且忽略 limit/cursor，CLI `--all`/`--limit` 被静默丢弃）与 `GET /tasks/{id}/messages`（id 游标 ASC）补 `{items,next_cursor}` 真实翻页（limit 缺省 50 上限 200，经新 `httpx.ParseLimit` 单点，与 children/task-search 同语义）；openapi `listMessages` 补声明已实装的 `target_id` 过滤参数 | 行为（对齐契约） | CLI（msg list 分页立即生效） |
| 2026-09-14 | 第 33 轮 B 线：openapi Message schema 补 `target_type`/`target_id`（服务端 DTO 恒序列化、契约漏声明，required 同步补齐）；documents 四端点与 audit list 六个脚手架桩补声明 `501 NOT_IMPLEMENTED` 响应（此前契约只写 200，超卖现状） | 契约文档 | CLI（快照随 P3 `astral register` 一并刷新，归入 §11 第 14 项） |

## 10. 对接 astral-cli 的联调清单（已归档）

全部条目已实装；原文照录于 [TODO-archive.md](TODO-archive.md) §C，
其中 phase-5 相关的 `content_hash` 约定迁入 §7。

## 11. 下一轮计划

> 优先级原则：先闭合「已宣称完成但实际断链」的功能，再做新面。
> 条目编号是稳定引用 ID（代码注释与 docs 以「§11 第 N 项」引用）：
> 完成后压缩为单行保留编号，不删除、不重排；实施明细见归档卷对应轮次。

1. 【✅ 第 9 轮完成】D11 tag attach/detach + D9 T-ws-7 + D10 session 上限 + 凭证/会话撤销断流
2. 【✅ 第 10 轮完成】CI 修复（goose embed 目录、迁移 StatementBegin/End）+ 双仓库卫生轮
3. 【✅ 第 11 轮完成】CLI login/init 实装（D12/D13 预审施工；端到端真机验收见第 27 轮）
4. 【✅ 第 13 轮完成】Web 任务树视图（第 17 轮重构为逐容器懒加载）
5. 【✅ 第 12 轮完成】Web/GUI approval 裁决视图 + device 审批页联调收尾
6. 【✅ 第 16/19 轮完成】CLI todo v2 适配 + tags/msg 命令族
7. 【✅ 第 21 轮完成】双仓库卫生轮（冗余清理/补丁化收敛/文档重写）
8. 【✅ 第 22 轮完成】CLI event listen（SSE 流式消费，JSON Lines + 断线续传）
9. rate limit（auth/device 端点优先；phase-6）
10. 【✅ 第 22 轮完成】SSE workspace 级授权（安全项提级）：非成员 404 / scope 不足 403，fail closed
11. 分页统一（契约债务）：presence/documents manifest/conflicts/audit 四个
    列表端点补 `{items, next_cursor}` envelope（protocol.md §3 已注明例外）；
    web TaskTreeView 的搜索/树模式消费 next_cursor（当前超页静默丢弃）
12. message.send 审计策略裁决：send 目前只写业务行 + outbox，无 audit
    （task/tag/workspace 全为三件套）——裁决「高频消息豁免」或补齐
13. 小项打包：~~web 搜索守卫补 assignee~~（✅ 第 25 轮随 UI 移植关闭：assignee
    已纳入 canSearch）；长度校验 byte vs rune 统一（message/task 按
    字节、tag 按字符）；409/403 搭配 VALIDATION_FAILED 的配对规则裁决
    （approval pending 重复 409、tag confirm_code 403）
14. 【✅ 第 29 轮 B 线完成】协议快照 v2.1 刷新（0330768 → 48ab5f2）；
    invite P1 与 round 33B 的 openapi 增量随 P3 `astral register` 消费时再刷新
15. fuzzy-only task-search 的 0 分行展示策略：服务端无 score 阈值 → UI 出现
    大量「匹配 0%」行（第 25 轮忠实呈现现语义）；裁决「服务端加阈值」或
    「前端过滤/弱化零分行」
16. 【✅ 第 29 轮完成】邀请注册 P1（server，A5）：migration 00013 + 邀请三端点 +
    register invite_code 分支 + INVITE_INVALID/EMAIL_TAKEN + security.invite 三事件
17. 【✅ 第 30 轮完成】邀请注册 P2（web）：/register 页 + workspace 邀请管理卡
18. 邀请注册 P3（CLI）：`astral register <server> [--invite-code …]`
    （缺省交互提示，--json）；注册只建号，登录仍走设备流（CLI 凭证存储与
    web cookie 是不同通道）；快照消费并入第 14 项刷新
19. 邀请注册 P4（衍生）：SMTP 邮件邀请（.env 增 SMTP_*，邮件含
    `/register?code=` 链接）；邮箱验证策略随本项一并评估（MVP email 仅登录名）
20. 【✅ 第 33 轮完成】平台角色基建（D16）：platform_role + 全局 scope 词表 +
    RequireGlobal + 首 human=admin + 全局凭证收口（migration 00014）
21. 【✅ 第 34 轮完成】admin 用户管理：/admin/* server 端点 + web 用户管理页 +
    停用即时失效闸门
22. 平台角色后续可选项（D16 延伸，触发再做）：注册策略开关（开放 vs 仅邀请，
    server_meta + admin 切换）；全工作区可见（platform:workspaces:read，运维
    排查用）；平台级 agent 账号（动 D9 语义）；web 全局 403 统一出口（现仍走
    各 surface fail-closed）；CLI `astral me` 消费 platform_role
23. 【✅ 第 35 轮完成】web 请求层重构：api/client 换 axios 实例（拦截器管
    请求头/Bearer/请求 id/204 归一）+ 失败统一 toast（silent 尾参豁免后台路径）
    + useApiAction 瘦身；修复 login/register 401 误触全局登出
