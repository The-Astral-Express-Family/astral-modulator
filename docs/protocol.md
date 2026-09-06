# Astral Modulator 公网协议初稿

> 状态：Draft
>
> 目标：定义 CLI、GUI、Agent SDK 与服务端之间的稳定边界。服务端实现语言不得影响本协议。

## 1. 协议选择

MVP 推荐：

- HTTPS + REST/JSON：查询与命令；
- Server-Sent Events (SSE)：CLI 实时事件；
- WebSocket：GUI 可选；
- OpenAPI：REST API 事实来源；
- JSON Schema：事件 `data` 事实来源；
- UTF-8；
- `/api/v1` 作为首个公网版本前缀。

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

服务端返回：

```http
X-Astral-Request-Id: <same-or-generated-id>
X-Astral-Protocol-Version: 1
```

## 3. 资源标识

ID 应：

- 全局唯一；
- 不泄露数据库自增规模；
- 可安全出现在 URL/日志中。

可以选 UUIDv7 或 ULID。具体格式通过 ADR 固化；客户端不得依赖 ID 内部结构。

建议前缀仅用于可读性，而非授权：

```text
usr_...
agt_...
svc_...
ws_...
tsk_...
msg_...
evt_...
```

## 4. 错误 Envelope

所有非 2xx JSON 错误使用：

```json
{
  "error": {
    "code": "TASK_ALREADY_CLAIMED",
    "message": "Task is already claimed by another actor",
    "retryable": false,
    "details": {
      "task_id": "tsk_...",
      "holder_actor_id": "agt_..."
    },
    "request_id": "req_..."
  }
}
```

`message` 面向人类；Agent 应基于稳定的 `code` 处理。

### 建议错误码

- `AUTH_REQUIRED`
- `TOKEN_EXPIRED`
- `TOKEN_REVOKED`
- `INSUFFICIENT_SCOPE`
- `WORKSPACE_NOT_FOUND`
- `TASK_NOT_FOUND`
- `TASK_ALREADY_CLAIMED`
- `TASK_LEASE_EXPIRED`
- `REVISION_CONFLICT`
- `DOCUMENT_CONFLICT`
- `RATE_LIMITED`
- `CLIENT_VERSION_UNSUPPORTED`
- `VALIDATION_FAILED`
- `INTERNAL_ERROR`

## 5. 分页

使用 cursor，不公开数据库 offset 语义：

```json
{
  "items": [],
  "next_cursor": "opaque-or-null"
}
```

Query：

```text
?limit=100&cursor=...
```

服务端设置最大 limit。

## 6. 乐观并发

支持两种等价实现，最终通过 OpenAPI 固化一种：

### 方案 A：Body revision

```json
{
  "expected_revision": 7,
  "status": "done"
}
```

### 方案 B：HTTP ETag

```http
If-Match: "rev-7"
```

冲突返回 `409 Conflict` 或 `412 Precondition Failed`，并带当前 revision。MVP 推荐统一使用显式 `expected_revision`，便于 CLI JSON 与事件保持一致。

## 7. Idempotency

对网络重试后可能造成重复副作用的写操作支持 `Idempotency-Key`：

- 创建 Task；
- 发送 Message；
- 创建 Agent Credential；
- Document push；
- 某些 integration 操作。

同一 Actor + endpoint + idempotency key 在有效窗口内应返回同一结果或明确冲突。

## 8. Auth API 草案

### Human Device Flow

```text
POST /api/v1/auth/device
POST /api/v1/auth/device/token
POST /api/v1/auth/logout
GET  /api/v1/auth/session
```

服务端如果对接外部 IdP，可把 Device Flow 代理到 IdP；自托管实现也可以提供自己的授权页面。

### Agent Credential

```text
POST   /api/v1/workspaces/{workspace_id}/agents
GET    /api/v1/workspaces/{workspace_id}/agents
POST   /api/v1/agents/{agent_id}/credentials
DELETE /api/v1/agents/{agent_id}/credentials/{credential_id}
```

创建凭证时明文 secret 只返回一次；服务端只保留不可逆 verifier/hash 或安全的 token metadata。

## 9. Workspace API 草案

```text
POST /api/v1/workspaces
GET  /api/v1/workspaces
GET  /api/v1/workspaces/{workspace_id}
PATCH /api/v1/workspaces/{workspace_id}

GET  /api/v1/workspaces/{workspace_id}/members
POST /api/v1/workspaces/{workspace_id}/members
PATCH /api/v1/workspaces/{workspace_id}/members/{actor_id}
DELETE /api/v1/workspaces/{workspace_id}/members/{actor_id}
```

## 10. Task API 草案

```text
POST /api/v1/workspaces/{workspace_id}/tasks
GET  /api/v1/workspaces/{workspace_id}/tasks
GET  /api/v1/tasks/{task_id}
PATCH /api/v1/tasks/{task_id}

POST /api/v1/tasks/{task_id}/claim
POST /api/v1/tasks/{task_id}/lease/renew
DELETE /api/v1/tasks/{task_id}/lease
```

### Claim request

```json
{
  "expected_revision": 4,
  "lease_seconds": 300
}
```

### Claim response

```json
{
  "task": {
    "id": "tsk_123",
    "status": "claimed",
    "assignee_actor_id": "agt_7",
    "revision": 5
  },
  "lease": {
    "expires_at": "2026-09-06T15:00:00Z"
  }
}
```

Lease secret 如果需要客户端证明，应单独处理且不得进入日志；也可以将 Lease 完全绑定 Actor session，避免再引入客户端 lease token。

## 11. Presence API 草案

```text
PUT /api/v1/workspaces/{workspace_id}/presence/me
GET /api/v1/workspaces/{workspace_id}/presence
```

Heartbeat：

```json
{
  "state": "working",
  "current_task_id": "tsk_123",
  "note": "Implementing CLI auth flow",
  "ttl_seconds": 90
}
```

服务端可以限制 TTL 范围，例如防止客户端声明一周不心跳仍在线。

## 12. Message API 草案

```text
POST /api/v1/workspaces/{workspace_id}/messages
GET  /api/v1/workspaces/{workspace_id}/messages
GET  /api/v1/tasks/{task_id}/messages
```

发送：

```json
{
  "target": {
    "type": "actor",
    "id": "agt_8"
  },
  "thread_id": null,
  "body": "I updated the protocol draft. Please review section 7.",
  "metadata": {}
}
```

## 13. Document API 草案

```text
GET  /api/v1/workspaces/{workspace_id}/documents/manifest
GET  /api/v1/workspaces/{workspace_id}/documents/{path}
PUT  /api/v1/workspaces/{workspace_id}/documents/{path}
GET  /api/v1/workspaces/{workspace_id}/conflicts
POST /api/v1/workspaces/{workspace_id}/conflicts/{conflict_id}/resolve
```

URL 中的 path 必须严格编码并在服务端 canonicalize，禁止 `..`、绝对路径和 symlink escape 导致越界访问。

### Manifest item

```json
{
  "path": "docs/design.md",
  "revision": 12,
  "content_hash": "sha256:...",
  "size": 4932,
  "updated_at": "2026-09-06T12:00:00Z"
}
```

### Push

```json
{
  "base_revision": 12,
  "base_hash": "sha256:...",
  "content": "# ...",
  "content_hash": "sha256:..."
}
```

## 14. SSE Event Stream

Endpoint：

```text
GET /api/v1/workspaces/{workspace_id}/events
```

Request：

```http
Accept: text/event-stream
Last-Event-ID: evt_...
```

Event：

```text
id: evt_01...
event: task.updated
data: {"id":"evt_01...","type":"task.updated",...}

```

### Event envelope

```json
{
  "id": "evt_...",
  "type": "task.updated",
  "workspace_id": "ws_...",
  "actor_id": "agt_...",
  "occurred_at": "2026-09-06T12:34:56Z",
  "schema_version": 1,
  "resource_revision": 8,
  "data": {}
}
```

### 基础事件类型

```text
workspace.member.changed
actor.presence.changed
task.created
task.claimed
task.lease.expired
task.updated
task.released
document.updated
document.conflict
message.created
security.credential.created
security.credential.revoked
human.override
```

### Resume 语义

- 客户端保存最近处理完成的 event ID；
- reconnect 时使用 `Last-Event-ID`；
- 服务端在 retention 窗口内重放；
- 如果 cursor 已过期，返回明确错误/事件要求客户端重新获取 snapshot；
- 客户端必须把事件消费设计为幂等。

## 15. WebSocket

GUI 若使用 WebSocket，不定义另一套领域协议；同一 event envelope 直接复用。

MVP 可只让 WebSocket 承载 server -> client event。GUI 的业务写操作仍通过 REST，降低双协议复杂性。

## 16. Rate Limit

服务端应按 Actor/Credential/IP 的组合限制异常流量，至少返回：

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 2
```

JSON `error.retryable = true`。

Heartbeat 与 SSE reconnect 应有单独限制，避免正常 Agent 被通用限流误伤。

## 17. Compatibility

### CLI

`astral version --json`：

```json
{
  "cli_version": "0.1.0",
  "protocol_versions": [1],
  "build": {
    "os": "linux",
    "arch": "x86_64"
  }
}
```

### Server capability

```text
GET /api/v1/meta/capabilities
```

示例：

```json
{
  "protocol_version": 1,
  "minimum_cli_version": "0.1.0",
  "features": [
    "task_lease",
    "document_sync_v1",
    "sse_resume"
  ]
}
```

客户端根据 capability 判断功能，不根据 server implementation/version 猜测。

## 18. OpenAPI 文件拆分建议

```text
api/
  openapi.yaml
  paths/
    auth.yaml
    workspaces.yaml
    tasks.yaml
    documents.yaml
    messages.yaml
    events.yaml
  schemas/
    actor.json
    task.json
    event.json
    error.json
```

初期也可以保持单 `openapi.yaml`，直到文件过大再拆。
