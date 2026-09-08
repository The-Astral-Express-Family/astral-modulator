# Astral Modulator 公网协议约定

> 状态：Accepted（语义约定）；REST 形状见下。
>
> **REST API 的唯一事实来源是 [`api/openapi.yaml`](../api/openapi.yaml)**
> （错误 envelope 与事件 envelope 的 JSON Schema 在 `api/schemas/`）。
> 本文档只保留 OpenAPI 表达不了的传输与语义约定。与本文冲突时以 openapi 为准；
> 修改协议走 `api/README.md` 的发布流程，CLI 针对冻结快照做契约测试
> （astral-cli 仓库 `protocol/snapshots/`）。

## 1. 传输基线

- HTTPS + REST/JSON：查询与命令；
- SSE：CLI/GUI 实时事件通道；
- WebSocket：GUI 确需双向高频交互时再加，复用同一事件 envelope，不另立协议；
- UTF-8；`/api/v1` 为首个公网版本前缀。

## 2. 通用请求头

客户端建议发送：

```http
Authorization: Bearer <token>
Accept: application/json
Content-Type: application/json
X-Astral-Client: cli
X-Astral-Client-Version: 0.1.0
X-Astral-Request-Id: <uuid/ulid>
Idempotency-Key: <opaque-key>   # 对可重试写操作
```

服务端在**所有** `/api/v1` 响应上回写：

```http
X-Astral-Request-Id: <same-or-generated-id>
X-Astral-Protocol-Version: 1
```

Bearer token 三种来源共用一个头：human access token（`ata_`）、
agent/service credential（`astral_`，即 `ASTRAL_TOKEN`）、以及 Web 场景由
HttpOnly Cookie 承载的 session（浏览器自动携带）。语义差异见 openapi 的
security 定义与 auth 模块实现。

## 3. 错误与分页

- 所有非 2xx JSON 响应使用统一错误 envelope，稳定 `error.code` 枚举
  见 `api/schemas/error.json`；`message` 面向人类，客户端基于 `code` 处理；
- 分页使用不透明 cursor（`{items, next_cursor}`），不暴露 offset 语义。

## 4. 乐观并发与幂等

- 乐观并发统一使用 body 内 `expected_revision`（裁决 D4）；冲突返回
  `409 REVISION_CONFLICT`，`details.current_revision` 携带当前值；
- 支持 `Idempotency-Key` 的写操作集合见 openapi 各写端点标注；
  同一 Actor + endpoint + key 在有效窗口内返回同一结果或明确冲突。

## 5. SSE 事件流

端点：

```text
GET /api/v1/workspaces/{workspace_id}/events
```

- envelope 与事件类型目录：`api/schemas/event.json`（SSE `id:` 行 = envelope 的
  `id`，即 resume 游标）；
- keepalive 为 SSE 注释行（`: keepalive`），15s 间隔，不占事件 ID 序列；
- **Resume 语义**：客户端保存最近处理完成的事件 ID，重连时经 `Last-Event-ID`
  （或等价 query 参数）请求重放；服务端在 outbox 保留窗口内补发，游标过期时
  下发 `snapshot.required` 控制事件，客户端重新拉快照（phase-4 落地，当前版本
  无重放——客户端需接受"快照 + 增量"的最终一致模型）；
- 客户端必须把事件消费设计为幂等（重放会带来重复事件）。

## 6. Rate Limit

按 Actor/Credential/IP 组合限制异常流量，至少返回：

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 2
```

JSON `error.retryable = true`。Heartbeat 与 SSE 重连应有独立限制，
避免正常 Agent 被通用限流误伤（phase-6）。

## 7. 兼容性

- 服务端在 `GET /.well-known/astral` 公布 `protocol_version` 与
  `min_cli_protocol_version`（登录前可匿名访问）；
- 服务端在 `GET /api/v1/meta/capabilities` 公布 feature 开关清单；
  客户端按 feature 判断能力，不猜测 server 实现版本；
- 兼容改动不要求两仓库同时发布；breaking protocol change 必须先设计服务端
  兼容窗口（详见 architecture §26）。
