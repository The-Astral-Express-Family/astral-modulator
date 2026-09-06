# Astral Modulator 文档包清单

将本目录内容复制到仓库根目录即可。

## Core docs

- `docs/README.md` — 文档索引与术语。
- `docs/requirements.md` — MVP 需求、用户故事、功能/非功能要求。
- `docs/architecture.md` — 总体架构、模块边界、技术路线与 Repo 结构。
- `docs/protocol.md` — REST/SSE/WebSocket、错误、事件、Task/Document API 草案。
- `docs/cli-ux.md` — CLI 命令、JSON 模式、退出码、重试与 Agent 契约。
- `docs/sync-semantics.md` — Markdown 三方同步、revision/hash、冲突与删除语义。
- `docs/security.md` — Actor/Credential、Scope、审批、Audit、威胁模型。
- `docs/deployment.md` — Windows/macOS/Linux 构建、签名、包管理器与服务端部署。
- `docs/roadmap.md` — Phase 0–6、首批 Issue、MVP Definition of Done。

## ADR

- `docs/adr/0001-cpp20-cli.md`
- `docs/adr/0002-server-language.md`
- `docs/adr/0003-rest-sse-public-protocol.md`
- `docs/adr/0004-structured-task-source-of-truth.md`
- `docs/adr/0005-postgresql-outbox-before-message-bus.md`
- `docs/adr/0006-web-first-gui.md`
- `docs/adr/0007-cli-distribution.md`

## Skills source

- `skills/astral-install/`
- `skills/astral-cli/`
- `skills/astral-collaboration/`

每个 Skill 均包含 `SKILL.md`、`agents/openai.yaml` 与必要 reference。
