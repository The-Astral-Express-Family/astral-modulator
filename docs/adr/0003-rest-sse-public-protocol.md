# ADR-0003: 公网协议采用 REST/JSON + SSE

- Status: Accepted
- Date: 2026-09-06

## Context

CLI、浏览器 GUI、未来 SDK 和自托管部署都需要易调试、易代理、跨语言的协议。实时状态主要是服务端向客户端推送。

## Decision

- REST/JSON：查询和命令；
- SSE：CLI 事件流；
- WebSocket：GUI 有需要时补充；
- OpenAPI + JSON Schema：契约；
- `/api/v1`：首个版本。

GUI 即使使用 WebSocket，业务写操作仍优先 REST。

## Consequences

### Positive

- curl/浏览器/反向代理友好；
- C++ libcurl 即可覆盖大多数功能；
- SSE 自动重连模型简单；
- 服务端实现语言可替换。

### Negative

- JSON 比二进制协议体积大；
- SSE 是单向；
- 需要额外设计 resume cursor 与 event retention。

## Alternatives

- gRPC：强 schema，但浏览器、代理和 CLI 调试摩擦更高；
- WebSocket everywhere：双向灵活，但会复制 request/response 语义与错误处理；
- polling：实现最简单，但实时性和负载差。
