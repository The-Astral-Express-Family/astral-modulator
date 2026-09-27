# AGENTS.md — astral-modulator 仓库 Agent 指南

> 本文件面向在本仓库工作的一切 AI coding agent（以及新加入的人类协作者），
> 内容是**仓库硬约束与工作流速查**，不是完整文档；架构/协议细节见
> [docs/architecture.md](docs/architecture.md) 与 [docs/protocol.md](docs/protocol.md)。

## 仓库定位（一句话）

本仓库 = Go 服务端 + Vue Web GUI + 公网协议契约（OpenAPI/JSON Schema）+ PostgreSQL schema。
CLI（`astral`，C++20）在姊妹仓库 astral-cli，两仓库只通过本仓库发布的版本化协议耦合，
不共享源码，不使用 git submodule。

## 分支与 CI 触发模型

```text
开发分支 ──push──▶ 3 道质量门（ci.yml）
    │
    └─PR──▶ 3 道质量门 + 镜像构建验证（不推送）
                │
             merge main ──▶ 质量门 + 推滚动 dev / dev-<短sha> 镜像
                │
             push v*.*.* tag ──▶ 质量门 + semver 镜像 + GitHub Release
```

- **分支 push**：`.github/workflows/ci.yml` 跑 server / web / openapi 三道门
  （任意分支；同分支新提交自动取消旧运行）。
- **PR**：三道门 + `.github/workflows/docker.yml` 验证镜像可构建（不推送）。
- **merge 到 main**：ci.yml 三道门 + docker.yml 推 `dev`（滚动）与 `dev-<短sha>`（可回溯）。
- **push `v*.*.*` tag**：ci.yml 最终验证 + docker.yml 推 semver 标签并创建 GitHub Release。

## 镜像发布（semver tag 驱动，docker.yml）

目标仓库 `ghcr.io/the-astral-express-family/astral-modulator`（amd64）。

| 操作 | 推送标签 | Release |
|---|---|---|
| merge main | `dev`、`dev-<短sha>` | — |
| tag `v1.2.3` | `1.2.3`、`1.2`、`1`、`latest` | ✅ 自动（自动生成变更日志） |
| tag `v1.2.3-rc.1` | `1.2.3-rc.1`（仅完整版本号） | ✅ 自动（标记 prerelease） |

规则要点（agent 常踩点）：
- 预发布 tag（含 `-`）**只推完整版本号**，不动 `1.2`/`1`/`latest` 移动标签；
  正式版才更新移动标签与 `latest`。
- main 分支的 dev 镜像**永远不会打 latest**（metadata-action 的 `flavor: latest=auto`
  只作用于 semver 类型，raw/sha 类型不打）。
- tag 必须匹配 `^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-...)?$`，非法 semver
  （如 `v1.2`、`v01.2.3`）会在 validate 步骤被拦并报错。
- tag 推送在 CI 里**只是 CI 触发器**，不做 git tag 之外的任何动作；发布动作
  （Release 创建、镜像推送）全部由 workflow 完成。
- `VERSION` build-arg：tag 触发传 `v1.2.3`，main 触发传分支名 → 写入 OCI label。
- GHCR 包默认 private，首次发布后需在 package settings 手动改 visibility。
- GHCR 要求镜像名全小写，workflow 已显式写小写镜像名，勿改回大写 owner。

## 发布操作（人类执行，agent 勿自动打 tag）

```bash
git tag v1.2.3 && git push origin v1.2.3   # 全自动：镜像 + Release
```

预发布：`v1.2.3-rc.1`（或 -beta/-alpha），CI 自动识别并只推完整版本号。

## 协议变更流程（硬约束）

`api/` 是协议 owner。改协议 → 同步服务端 → 在 `TODO.md` 契约变更登记 → 通知
astral-cli 更新协议快照。详见 [api/README.md](api/README.md)。

## 常用验证命令

```bash
cd server && go vet ./... && go test ./... -count=1   # 含三个契约门
cd server && make openapi-lint                        # redocly lint api/openapi.yaml
cd web && npm run build
```

## 仓库结构速查

```text
api/            公网协议契约（openapi/JSON Schema）—— 与 astral-cli 的唯一耦合点
server/         Go 模块化单体（chi + GORM + goose）
web/            Vue 3 + TS + Vite 控制台
docs/           需求/架构/协议/安全/部署等文档与 ADR
skills/         Agent 技能（astral-install / astral-cli / astral-collaboration）
.github/        CI（ci.yml 三道门 + docker.yml 镜像构建/发布）
```

## 其他

- 许可：MIT。
- CLI 的构建、签名与三端分发属 astral-cli 仓库职责，本仓库 CI 不涉及。
