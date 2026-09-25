# api/ — 公网协议契约

本目录是 astral-modulator 与 astral-cli 之间**唯一**的耦合点（architecture §26）。

## 文件

- `openapi.yaml` — REST API 事实来源。路径、字段、错误码、分页、并发语义都在这里裁决。
- `schemas/event.json` — SSE/WebSocket 事件 envelope 契约。
- `schemas/error.json` — 错误 envelope 契约（与 openapi 内定义同源，供 CLI 独立校验）。

## 服务端同步义务

| 契约 | 服务端实现 | 备注 |
|---|---|---|
| 错误码 enum | `server/internal/httpx/errors.go` | openapi_contract_test 双向防漂移 |
| 事件 enum/envelope | `server/internal/modules/event/types.go` | 同上 |
| ID 前缀 | `server/internal/ids/ids.go` | 新前缀需登记 TODO.md |
| 路由 | 各模块 `RegisterRoutes` | 路径必须与 openapi 一字不差 |
| DB schema | `server/migrations/*.sql` + `internal/model` | 先 migration 后模型 |

## 协议变更流程

1. 修改 `openapi.yaml` / schemas（含兼容性评估：breaking 需服务端先开兼容窗口）；
2. 同步服务端实现与测试；
3. 在根目录 `TODO.md`「契约变更登记」追加一行；
4. 打 tag 或发布 protocol artifact 快照；
5. `astral-cli` 更新其 `protocol/` 快照并跑 contract test。

## 客户端消费方式

- CLI：以本目录快照为准生成/手写客户端与 contract test，不 import 服务端 Go 包。
- Web：`web/src/api/schema.d.ts` 由 openapi-typescript 生成（`npm run gen:api`，
  CI drift 门校验与 openapi.yaml 一致）；`web/src/api/types.ts` 为存量手写别名层，
  各视图迁移生成类型后逐步删除。

## Lint

```bash
npx @redocly/cli lint api/openapi.yaml
```

CI 中会执行；新增端点前先本地跑一遍。
