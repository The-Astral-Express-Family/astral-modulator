# Astral Modulator 仓库地图

## 根目录

- `README.md` — 项目简介与快速开始。
- `ARCHITECTURE.md` — 架构入口指针（事实来源在 docs/ 与 api/）。
- `TODO.md` — 进度登记簿：已完成工作、契约裁决、待办清单、CLI 联调清单。
- `docker-compose.yml` — 开发用 PostgreSQL。
- `Makefile` — 常用开发命令。

## 契约（与 astral-cli 的唯一耦合点）

- `api/openapi.yaml` — REST API 事实来源。
- `api/schemas/` — 错误与事件 envelope 的 JSON Schema。
- `api/README.md` — 协议发布流程。

## 服务端与 Web

- `server/` — Go 模块化单体（chi + GORM + goose；模块布局见 server/internal）。
- `web/` — Vue 3 控制台。

## 文档

- `docs/README.md` — 文档索引与术语。
- `docs/requirements.md` — MVP 需求、用户故事、功能/非功能要求。
- `docs/architecture.md` — 总体架构、模块边界、技术路线与仓库结构。
- `docs/protocol.md` — OpenAPI 之外的传输与语义约定。
- `docs/sync-semantics.md` — Markdown 三方同步、revision/hash、冲突与删除语义。
- `docs/security.md` — Actor/Credential、Scope、审批、Audit、威胁模型。
- `docs/deployment.md` — 服务端部署与数据库升级。
- `docs/看我看我.md` — 人工维护的原始构想笔记（AI 协作者只读，不修改不删减）。
- `docs/roadmap.md` — 里程碑视图与 MVP Definition of Done（实施 Phase 划分见 architecture.md §27）。

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
