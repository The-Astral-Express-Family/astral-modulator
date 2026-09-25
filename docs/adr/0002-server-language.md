# ADR-0002: 服务端首选 Go 模块化单体

- Status: Accepted
- Date: 2026-09-06

## Context

服务端需要处理身份、PostgreSQL、SSE/WebSocket、任务租约、消息、审计和长期运行。服务端不要求与 CLI 使用同一语言。

## Decision

v0.1 首选 Go 实现模块化单体。

CLI 与服务端只通过 OpenAPI/Event Schema 共享契约，不共享 ABI。

## Consequences

### Positive

- 单二进制部署；
- 网络并发与长连接适配自然；
- PostgreSQL、OAuth、可观测性生态成熟；
- 适合小团队自托管。

### Negative

- C++ + Go 双语言；
- schema 需要生成/校验；
- 团队若无 Go 经验需要学习。

## Alternatives

### Rust

强类型、内存安全、性能优秀。若团队 Rust 熟练，可替代 Go；代价是学习与编译成本。

### TypeScript

产品开发速度快，Web 团队友好；Runtime/dependency footprint 更重。

### C++

语言统一，但业务服务开发和运维成本通常更高。除非团队有明确优势，不建议为了统一语言而选择。

## Revisit Trigger

在第一个 server Spike 结束后，如果 Go 显著阻碍团队效率，可在 API schema 尚未冻结前切换。
