# Astral Modulator

人类与多个 LLM Agent 共用的协作控制面：身份、Workspace、TODO/Task 树、Tag、
Markdown 记忆与文档同步、Presence、消息、审计与人类干预。

**本仓库 = Go 服务端 + Vue Web GUI + 公网协议契约（OpenAPI/JSON Schema）+ PostgreSQL schema。**
CLI（`astral`，C++20）在姊妹仓库 [astral-cli]，两仓库只通过本仓库发布的版本化协议耦合，
不共享源码，不使用 git submodule。

[astral-cli]: https://github.com/The-Astral-Express-Family/astral-cli

## 文档

- [docs/README.md](docs/README.md) — 文档索引与术语
- [docs/architecture.md](docs/architecture.md) — 架构事实来源（技术选型/模块边界/数据模型）
- [docs/protocol.md](docs/protocol.md) — OpenAPI 之外的传输与语义约定（请求头/SSE/限流/兼容性）
- [docs/roadmap.md](docs/roadmap.md) — 里程碑视图（实施 Phase 划分见 architecture.md §27）
- [TODO.md](TODO.md) — 实施总纲：硬约束、决策速查、S1-S8 实施计划、契约变更登记
- [TODO-archive.md](TODO-archive.md) — 登记簿历史卷（轮次详录、旧登记簿，只增不改）
- 根 [ARCHITECTURE.md](ARCHITECTURE.md) — 指向 docs 的简版入口

## 仓库结构

```text
api/            公网协议契约（openapi.yaml + schemas/）—— 与 astral-cli 的唯一耦合点
server/         Go 模块化单体（chi + GORM + goose）
  cmd/astral-server/
  cmd/astral-bootstrap/
  internal/httpx/       错误 envelope / 稳定错误码 / 公共头中间件（契约层）
  internal/ids/         前缀 ID 生成（usr_/ws_/tsk_/evt_/...）
  internal/modules/     admin auth workspace task tag memory document message presence event audit
  migrations/           goose SQL（随二进制 embed）
web/            Vue 3 + TS + Vite 控制台（api client/SSE 封装已就绪）
docs/           需求/架构/协议/安全/部署等文档与 ADR
skills/         Agent 技能（astral-install / astral-cli / astral-collaboration）
.github/        CI（server / web / openapi 三道门）
```

## 快速开始（开发）

前置：Go ≥ 1.27、Node ≥ 24。数据库可选（无 DB 时以桩模式启动：发现/能力/健康可用，
受保护端点与 readyz 均返回 503 INTERNAL_ERROR；501 桩已于 round 38 全部清零，
memory 无独立路由——M1 裁决：复用 documents 端点与 `memory/` 路径前缀）。

```bash
# 1. 服务端（桩模式，监听 :8080）
cd server && go run ./cmd/astral-server

# 2. Web（Vite 5173，/api 与 /.well-known 已代理到 8080）
cd web && npm install && npm run dev
```

带数据库（推荐走交互式向导，一条命令完成配置问答 → 连库验证 → goose 迁移 →
固化 server_id → 写 `.env` → 创建首个管理员账号，全程幂等可重跑）：

```bash
docker compose up -d                          # postgres:17 @ localhost:5433（astral/astral/astral）
cd server && go run ./cmd/astral-bootstrap    # 在 server/ 目录下运行；已有 .env 的值作为问答默认
cd server && go run ./cmd/astral-server
```

向导写 `.env` 前会备份原文件为 `.env.bak`，并保留其中非向导管理的自定义键；
首个管理员创建走的就是 `POST /api/v1/auth/register` 同一条 `Register` 路径
（已有 human 账号时自动跳过）。也可以完全手动：

```bash
cp server/.env.example server/.env       # 按需编辑；.env 已被 .gitignore 忽略
cd server && go run ./cmd/astral-server  # 自动加载 .env；启动时自动 goose up（ASTRAL_AUTO_MIGRATE 默认开）
```

`.env` 在启动时由 godotenv 预加载（文件缺失可容忍，解析错误打 warning）；
**shell 里已设置的真实环境变量优先**，`.env` 不会覆盖它们，因此临时覆盖仍用内联写法：

```bash
cd server
ASTRAL_DATABASE_DSN='postgres://astral:astral@localhost:5433/astral?sslmode=disable' \
ASTRAL_SERVER_ID='srv_dev_local' \
go run ./cmd/astral-server
```

主要环境变量（全部可选，完整说明见 `server/internal/config/config.go`，模板见 `server/.env.example`）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `ASTRAL_HTTP_ADDR` | `:8080` | 监听地址 |
| `ASTRAL_PUBLIC_URL` | — | canonical URL（写入 /.well-known） |
| `ASTRAL_WEB_BASE_URL` | — | web 控制台基址（device flow 验证链接指向；生产同源留空回退 PublicURL，dev 指向 vite 端口） |
| `ASTRAL_DATABASE_DSN` / `DATABASE_URL` | — | PostgreSQL DSN；空 = 桩模式 |
| `ASTRAL_SERVER_ID` | 随机 | 稳定服务器身份；首启固化进 server_meta，此后库中值优先 |
| `ASTRAL_AUTO_MIGRATE` | `true` | 启动时 goose up；**生产多实例必须关闭** |
| `ASTRAL_DOC_HISTORY_MAX_PER_DOC` | `50` | 文档历史版本保留窗口：每文档上限（0 = 不限；与 TTL 先到即剪） |
| `ASTRAL_DOC_HISTORY_TTL_HOURS` | `720` | 文档历史版本保留窗口：最短保留时长，小时（0 = 不限） |
| `ASTRAL_DEV_CORS_ORIGINS` | — | 开发期浏览器跨域白名单（生产同源应留空） |
| `ASTRAL_LOG_LEVEL` | `info` | slog 级别（debug/info/warn/error） |

## 验证

```bash
cd server && go vet ./... && go test ./... -count=1   # 含三个契约门
cd server && make openapi-lint                        # redocly lint api/openapi.yaml
cd web && npm run build
```

与 CI 三道门（`.github/workflows/ci.yml`：server / web / openapi）对应；web 门
另含 `npm run gen:api` 后 `schema.d.ts` 逐字节一致的 drift 检查。

## 协议变更流程

`api/` 是协议 owner。改协议 → 同步服务端 → 在 `TODO.md` 契约变更登记 → 通知
astral-cli 更新协议快照。详见 [api/README.md](api/README.md)。

## 许可

MIT（见 [LICENSE](LICENSE)）。
