# ADR-0004: Task 使用结构化模型作为事实来源

- Status: Accepted
- Date: 2026-09-06

## Context

项目需要多个 Agent 原子 Claim TODO、设置状态、表达依赖、审计和权限。如果只用 Markdown checkbox 作为事实来源，很难可靠处理并发与租约。

## Decision

Task 在服务端使用结构化记录作为 source of truth。

Markdown `TODO.md` 在 MVP 中只是普通 Document；未来可以增加带稳定 Task ID 的 projection/import/export。

## Consequences

### Positive

- Claim transaction 清晰；
- revision/权限/审计可实现；
- GUI 看板与查询高效；
- Task 不受 Markdown 格式变化影响。

### Negative

- 用户可能需要同时理解 Task 与 Markdown；
- 若未来做 `TODO.md` projection，需要定义映射规则。

## Alternatives

- Markdown-only：简单但并发/权限/审计风险高；
- GitHub Issues-only：绑定外部平台，不适合自托管核心协议。
