# 脚手架 TODO 登记簿

> 本文件是脚手架阶段的**任务与契约登记中心**。代码内 `TODO(phase-x)` 注释负责
> 局部上下文，本文件负责全局视图：哪些端点/契约/决策还没落地、依据哪份文档、
> 归属哪个 Phase（Phase 划分见 docs/roadmap.md）。
>
> 维护规则：
> - 完成一项就划掉并注明 PR/commit，不要静默删除（保留裁决痕迹）；
> - 新增契约（路径/错误码/事件/ID 前缀/env 变量）必须同步登记到「契约变更登记」；
> - `grep -rn "TODO(phase" server web` 可找到全部代码内待办。

## 0. 已完成（脚手架，2026-09-07）

- Go 模块化单体骨架（chi + GORM + goose + slog），`go build/vet/test` 全绿；
- `api/openapi.yaml` 全量契约 + `api/schemas/{error,event}.json`（Redocly 校验通过）；
- 8 个 goose migration（init/auth/task/tags/documents/outbox/audit/presence）；
- 真实实现：`/.well-known/astral`、`/api/v1/meta/capabilities`、`/healthz`、`/readyz`、
  SSE 事件流端点（keepalive + hub）、错误 envelope、request-id/protocol-version 中间件、
  前缀 ID 生成、公共响应头、CORS 开关；
- Web 脚手架（Vue3+TS+Vite+Pinia+Router），api client/SSE 封装/device 审批页路由，
  `vue-tsc + vite build` 通过；
- CI（server/web/openapi 三 job）、docker-compose 开发库、Makefile。

## 1. 文档分歧裁决（脚手架已统一，实现时不要再摇摆）

两份文档对同一端点写了不同路径。**api/openapi.yaml 是唯一事实来源**，
以下为裁决结果与理由：

| # | 分歧点 | 裁决 | 理由 |
|---|--------|------|------|
| D1 | device flow 路径 | `POST /api/v1/auth/device/authorizations`、`POST /api/v1/auth/device/authorizations/{device_code}/token` | architecture.md §8.2 与 astral-cli 文档一致（2:1）；protocol.md §8 的 `/auth/device` 废弃 |
| D2 | 身份自检端点 | `GET /api/v1/auth/me` | 同上；protocol.md §8 的 `/auth/session` 废弃 |
| D3 | Tag 确认参数拼写 | `--confirm` | 文档 typo `--comfirm` 已在 architecture §14 明确不沿用 |
| D4 | 乐观并发 | body 内 `expected_revision`（方案 A） | protocol.md §6 MVP 推荐方案 A，CLI JSON/事件流一致性好处理 |

## 2. 待裁决契约（实现前必须先定，避免返工）

| # | 问题 | 涉及 | 建议 | 状态 |
|---|------|------|------|------|
| M1 | Memory 工作区的公网 API 形状未在任何文档定稿 | memory 模块、astral-cli | 复用 documents 表 + 保留路径前缀（`organization/`、`projects/<ws>/`），不发明第二套协议 | open（phase-5 前裁决） |
| A1 | Device token 轮询的 pending 语义（HTTP 状态码/错误码/慢轮询惩罚） | auth、CLI | 参考 OAuth Device Flow 惯例：400 + `authorization_pending` 语义码，需新增错误码（走契约变更登记） | open（phase-1 前裁决） |
| A2 | access token 服务端校验路径：每请求查库 vs 短 TTL 缓存 | auth 性能 | MVP 先查库（单体内延迟可接受），预留缓存接口 | open（phase-1 前裁决） |
| A3 | Web device 审批页所需 API 未在 openapi 定义 | web、auth | 草案见 `web/src/views/DeviceApproveView.vue` 头注释（GET by user_code + approve/deny） | open（phase-1 前裁决） |
| T1 | task 删除/取消语义：不级联时子任务如何呈现 | task、CLI UX | 取消父任务时子任务独立存活；显式 cascade 选项后续再加 | open（phase-3 前裁决） |
| S1 | snapshot.required 事件形状与 outbox 保留窗口时长 | events、CLI | 建议 24h 起步，按内存/磁盘预算调整 | open（phase-4 前裁决） |

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
- [ ] 裁决并登记 A1/A2/A3

## 4. Phase 2 — Workspace（roadmap Phase 2）

- [ ] workspace CRUD + `?name=` 精确解析（astral init 依赖；404 与无权限必须可区分）
- [ ] membership 管理；promote_owner 走 approval 状态机（migration 尚无 approvals 表，
      建表时走契约变更登记）
- [ ] agent actor + credential 签发/撤销（明文只返回一次）
- [ ] Idempotency-Key 中间件（幂等窗口存储选型：库表 vs 内存 + 库持久化）
- [ ] GORM 模型补全 + repository 层（当前只映射核心表）

## 5. Phase 3 — TODO 树 / 搜索 / Tags（roadmap Phase 3）

- [ ] task CRUD + revision 乐观并发 + parent 循环检测（recursive CTE）
- [ ] **原子 claim**（Phase 1 spike 验收项）：条件 UPDATE + 事务内 revision 校验 +
      audit + outbox；并发竞争只有一个成功
- [ ] lease renew/release/过期清扫器（发 `task.lease.expired`）
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

## 10. 对接 astral-cli 的联调清单（避免踩坑）

CLI 仓库开工时按此清单对表，顺序即依赖顺序：

1. `GET /.well-known/astral` — server_id/api_base/protocol_version（已实装 ✅）
2. `GET /api/v1/meta/capabilities` — features 门控（已实装 ✅，features 暂为空）
3. 错误 envelope 解析 — 所有非 2xx（已实装 ✅，含 `NOT_IMPLEMENTED` 桩）
4. 公共响应头回显 — `X-Astral-Request-Id`/`X-Astral-Protocol-Version`（已实装 ✅）
5. ID 形状 `^[a-z]{2,3}_<uuidv7>` — CLI 只做透传与展示（已实装 ✅）
6. SSE 流 + keepalive — `/api/v1/workspaces/{id}/events`（已实装 ✅，
   但**无重放**：断线期间事件缺失，CLI 需接受快照+增量模型直至 phase-4）
7. device flow / token / 各业务端点 — 501 桩（按 Phase 落地）
8. `content_hash` 统一 `sha256:<hex>`（小写十六进制）— 文档同步 phase-5 联调时最易错
