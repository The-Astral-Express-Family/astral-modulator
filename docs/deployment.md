# Astral Modulator 服务端部署

> 状态：Draft（随服务端实现演进）
>
> CLI 的构建、签名与三端分发属 `astral-cli` 仓库职责
> （见其 docs/ARCHITECTURE.md 发行章节与 release workflow），本文不再覆盖。

## 1. 部署形态

### 本地开发

仓库根 `docker-compose.yml` 提供 PostgreSQL 17（`localhost:5433`，astral/astral/astral），
服务端以 `go run` 直启，启动时自动执行 goose migration：

```bash
docker compose up -d
cd server
ASTRAL_DATABASE_DSN='postgres://astral:astral@localhost:5433/astral?sslmode=disable' \
ASTRAL_SERVER_ID='srv_dev_local' \
go run ./cmd/astral-server
```

无数据库也能启动（桩模式）：发现/能力/健康/SSE 可用，受保护端点返回 401；
需要存储的端点在鉴权后返回 503 INTERNAL_ERROR；未实装端点返回 501
（document/audit，phase-5/6）。memory 无独立路由（M1 裁决：复用 documents
端点与 `memory/` 路径前缀，见 architecture §6.5）。
环境变量全集见 `server/internal/config/config.go`。

### 小型自托管（推荐基线）

```text
reverse proxy / TLS
  -> astral-server (单二进制/容器)
  -> PostgreSQL
```

不要求 Kubernetes。Web 控制台与 API 同源部署（server 托管 `web/dist`，phase-6），
反向代理只需转发一个服务。

### 二进制部署

`astral-server` 为单个静态二进制，goose migration 已 embed（随二进制同版本）。
关键配置（环境变量）：

```text
ASTRAL_DATABASE_DSN=postgres://...
ASTRAL_PUBLIC_URL=https://astral.example.com   # canonical URL，写入 /.well-known/astral
ASTRAL_SERVER_ID=srv_...                        # 首启后固化进 server_meta，库中值优先
ASTRAL_HTTP_ADDR=:8080
ASTRAL_LOG_LEVEL=info                           # 可选
```

（非全集；完整说明以 `server/internal/config/config.go` 为准。）

Secrets 只走环境变量/secret manager，不提交配置库。

### Scale-out（达到需要时）

- stateless API replicas；
- managed PostgreSQL；
- NATS JetStream（多实例事件广播成为产品需求后，见 architecture §19）；
- object storage、worker、集中式 logs/metrics/traces。

## 2. 数据库升级

- migration 为 goose 版本化 SQL（`server/migrations/`），单调递增、幂等；
- 开发环境 `ASTRAL_AUTO_MIGRATE=true`（默认）随启动执行；
  **生产多实例部署必须关闭**，由部署流程显式执行 `goose up`，
  避免并发实例无协调地跑 migration；
- deploy 前备份；forward-only 优先；destructive migration 分两个 release 完成（expand/contract）；
- CI 中的 postgres job 从空库跑全部 migration 作为最低验证；
- server 启动读取 `server_meta.server_id`，schema 与身份均向前兼容演进。

## 3. 健康检查

- `GET /healthz` — 进程存活（无依赖检查）；
- `GET /readyz` — 数据库可达才返回 200，供负载均衡摘除节点；
- `GET /.well-known/astral` — 协议发现，登录前可匿名访问。
