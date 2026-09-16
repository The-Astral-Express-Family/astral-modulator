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
X-Astral-Client-Version: 2   # 客户端协议大版本（正整数，语义见 §7）
X-Astral-Request-Id: <uuid/ulid>
Idempotency-Key: <opaque-key>   # 对可重试写操作
```

服务端在**所有** `/api/v1` 响应上回写：

```http
X-Astral-Request-Id: <same-or-generated-id>
X-Astral-Protocol-Version: 2
```

Bearer token 三种来源共用一个头：human access token（`ata_`）、
agent/service credential（`astral_`，即 `ASTRAL_TOKEN`）、以及 Web 场景由
HttpOnly Cookie 承载的 session（浏览器自动携带）。语义差异见 openapi 的
security 定义与 auth 模块实现。

## 3. 错误与分页

- 所有非 2xx JSON 响应使用统一错误 envelope，稳定 `error.code` 枚举
  见 `api/schemas/error.json`；`message` 面向人类，客户端基于 `code` 处理；
- 分页使用不透明 cursor（`{items, next_cursor}`），不暴露 offset 语义。
  第 37 轮起全部列表端点信封统一（documents manifest / conflicts / audit
  均带真实游标；presence 成员规模有界，`next_cursor` 恒 null 不做翻页），
  契约见 openapi 各列表端点；
- 错误码配对规则（第 37 轮裁决）：400 VALIDATION_FAILED=形状/约束；
  403 INSUFFICIENT_SCOPE=权限；409+专用码=状态/唯一性冲突；
- 长度上限一律按 **UTF-8 字节数**计；openapi `maxLength` 仅参考展示，
  不作精确边界。

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
- **Resume 语义（已实装）**：客户端保存最近处理完成的事件 ID，重连时经
  `Last-Event-ID` 头（或 `last_event_id` query 参数）请求补发；服务端从 outbox
  保留窗口内按事件 ID 升序补发后接入实时流；保留窗口为 **24 小时**（S1 裁决），
  游标超窗时服务端下发 `snapshot.required` 控制事件（`data.reason=cursor_expired`）
  并断流，客户端必须重新拉取快照后再建立新游标；
- **订阅授权**：与其他 workspace 端点同语义——非成员 404
  （`WORKSPACE_NOT_FOUND`，不泄露存在性）、成员但缺 `workspace:read` scope 403；
  凭证/会话撤销后服务端主动断开该 actor 的全部事件流；
- 客户端必须把事件消费设计为幂等（重放与补发可能带来重复事件）。

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
- 版本协商下限（NFR-004）：`/api/v1` 请求携带 `X-Astral-Client-Version`
  头时，值必须是不小于 `min_cli_protocol_version` 的正整数，否则整个请求
  被拒绝为 `400 CLIENT_VERSION_UNSUPPORTED`；头缺失一律放行（契约是
  "支持协商"而非"要求携带"）。客户端应在首次连接前读取
  `/.well-known/astral` 自查版本，避免盲发被拒；
- 服务端在 `GET /api/v1/meta/capabilities` 公布 feature 开关清单；
  客户端按 feature 判断能力，不猜测 server 实现版本；
- 兼容改动不要求两仓库同时发布；breaking protocol change 必须先设计服务端
  兼容窗口（详见 architecture §26）。
