# Architecture

> 本文件是入口指针。架构事实来源：[docs/architecture.md](docs/architecture.md)；
> 协议事实来源：[api/openapi.yaml](api/openapi.yaml)（与 docs/protocol.md 冲突时以 openapi 为准）；
> 当前进度与契约裁决：[TODO.md](TODO.md)。

## 一图流

```text
Human ──> astral CLI (astral-cli 仓库, C++20) ──┐
Human ──> Web GUI (web/, Vue 3) ────────────────┼──HTTPS──> Go 控制面 (server/) ──> PostgreSQL
Agent ──> CLI / 公开 API ───────────────────────┘              │
                                                               └─ SSE 事件流（transactional outbox）
```

## 仓库职责边界

| 归属 | 内容 |
|---|---|
| 本仓库（astral-modulator） | Go 服务端、Vue Web、OpenAPI/JSON Schema、PostgreSQL migrations、协议发布 |
| astral-cli 仓库 | C++20 CLI、OS Credential Store、`.astral/config.json` 绑定、三平台发行、协议快照 contract test |

两仓库只通过 `api/` 的版本化协议耦合，不共享源码 ABI。

## 关键不可破坏约定

1. 所有非 2xx 响应必须走 `server/internal/httpx` 的错误 envelope（稳定 `error.code`）；
2. 所有 `/api/v1` 路径必须与 `api/openapi.yaml` 一字不差；
3. schema 变更先写 goose migration（`server/migrations/`），禁止 GORM AutoMigrate 管生产 schema；
4. 业务写操作 = 领域表变更 + audit + outbox 同事务（模块化单体 + transactional outbox）；
5. ID 一律由 `server/internal/ids` 生成（前缀 + UUIDv7），客户端不得解析。
