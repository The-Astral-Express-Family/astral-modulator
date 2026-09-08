# Astral Modulator 文档索引

本目录承载服务端、公网协议与协作语义的设计文档。仓库当前状态（已实现内容、
契约裁决、待办）以根目录 [TODO.md](../TODO.md) 为准；本文档描述目标设计。

## 阅读顺序

1. [requirements.md](requirements.md) — 用户、场景、MVP 与非功能需求。
2. [architecture.md](architecture.md) — 总体架构、模块边界与技术路线。
3. [../api/openapi.yaml](../api/openapi.yaml) — 公网 REST 契约唯一事实来源
   （说明见 [../api/README.md](../api/README.md)）。
4. [protocol.md](protocol.md) — OpenAPI 之外的传输与语义约定（请求头、SSE、限流、兼容性）。
5. [sync-semantics.md](sync-semantics.md) — Markdown 工作区的版本、同步与冲突语义。
6. [security.md](security.md) — 身份、凭证、RBAC/Scope、审计和威胁模型。
7. [deployment.md](deployment.md) — 服务端部署与数据库升级（CLI 分发在 astral-cli 仓库）。
8. [roadmap.md](roadmap.md) — Phase 0–6 实施顺序和验收标准。
9. [adr/](adr/) — 关键技术选择与备选方案。

> CLI 的命令 UX、输出契约与退出码文档在 `astral-cli` 仓库（docs/ARCHITECTURE.md）；
> 原本暂存于此的 cli-ux.md 草稿已随双仓库拆分移除。

## 术语

- **Astral Modulator**：本项目，面向人类与 LLM Agent 的协作中枢。
- **`astral`**：暂定的 CLI 可执行文件名。
- **Control Plane**：中心服务端，负责身份、Workspace、Task、文档、消息、Presence、事件和审计。
- **Actor**：行为主体，分为 Human、Agent、Service。
- **Workspace**：协作与权限隔离的边界。
- **Task**：结构化 TODO/任务，是任务协作的事实来源。
- **Presence**：Actor 当前在线与工作状态。
- **Document**：受 Astral 管理的 Markdown/文本文件。
- **Skill**：教另一个 Agent 安装、使用 Astral 或遵守协作规范的可复用指令包。

## 设计原则

1. **CLI-first，但不是 CLI-only。** Agent 的核心操作必须全部可由 CLI 完成，人类再通过 GUI 获得更强的观察与干预能力。
2. **协议优先。** CLI、GUI、Agent SDK 与未来插件都依赖稳定的公开协议，而不是耦合某个服务端实现语言。
3. **结构化协作数据优先。** Task、Presence、Message、Permission 等保持结构化；Markdown 作为知识/文本工作区，不承担所有并发语义。
4. **默认可审计。** 每个重要写操作都能回答“谁、何时、对什么、做了什么、结果如何”。
5. **默认最小权限。** Agent 不共享人类长期凭证，Token 按 Workspace、Scope 与 TTL 限制。
6. **MVP 模块化单体。** 先证明协作模型，再在确有规模压力时拆服务或引入消息总线。
7. **分发是一等架构约束。** CLI 必须从第一天考虑三端构建、签名、包管理器与升级策略。
8. **冲突不可静默丢失。** Task 与 Document 都使用显式 revision/lease 语义，避免 last-write-wins 掩盖并发问题。

## Skills

仓库建议同时维护：

- `skills/astral-install/`：如何在三端安装、校验、升级、卸载 CLI；
- `skills/astral-cli/`：Agent 如何稳定调用 CLI；
- `skills/astral-collaboration/`：多 Agent 协作规范、Claim/Heartbeat/交接/冲突/秘密处理。

这三个 Skill 的职责有意分离：安装方式变化不应迫使协作规范一起发布；CLI 新增子命令也不应改变团队行为原则。
