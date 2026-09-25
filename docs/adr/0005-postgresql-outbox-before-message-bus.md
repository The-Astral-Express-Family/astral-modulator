# ADR-0005: MVP 使用 PostgreSQL + Transactional Outbox，不强制消息总线

- Status: Accepted
- Date: 2026-09-06

## Context

实时系统很容易过早引入 Redis、NATS、Kafka 等基础设施，但项目早期首先需要验证领域语义和自托管体验。

## Decision

MVP：

- PostgreSQL 作为主要持久化；
- 业务写入与 `outbox_events` 在同一事务；
- 进程内 dispatcher 向 SSE/WebSocket subscriber 分发；
- 暂不要求 NATS/Redis。

达到明确扩容条件后优先评估 NATS JetStream。

## Consequences

### Positive

- 本地/小团队部署简单；
- 避免 DB + message broker 双写；
- 业务一致性容易证明。

### Negative

- 单实例 event fan-out 有规模上限；
- 多实例时需要额外协调；
- durable external consumers 需要后续能力。

## Revisit Trigger

- API 多实例；
- 需要 durable event replay；
- 外部插件消费事件；
- event throughput/latency 达到 PostgreSQL outbox 的实际瓶颈。
