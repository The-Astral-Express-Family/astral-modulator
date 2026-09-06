# ADR-0007: CLI 发行采用原生 Release Asset + 包管理器

- Status: Proposed
- Date: 2026-09-06

## Context

CLI 是 Agent 进入 Astral 的主要入口。如果安装困难，协作协议设计再好也无法降低使用门槛。三端分发必须在实现早期验证。

## Decision

所有 stable release 至少提供：

- Windows/macOS/Linux 原生 archive；
- SHA-256 checksum；
- stable 签名；
- SBOM。

包管理器按阶段：

- macOS/Linux：Homebrew；
- Windows：Scoop，然后 WinGet；
- Linux：后续 deb/rpm；
- 企业需求后再考虑 MSI/MSIX。

不把 `curl | sh` 作为唯一官方安装方式，也不默认静默自更新。

## Consequences

### Positive

- 新机器不需要开发环境；
- Agent 安装 Skill 可提供明确分支；
- 版本可复现；
- 包管理器承担升级。

### Negative

- Release pipeline 很早就要处理三端 CI、签名与 metadata；
- Linux glibc/musl 兼容需要额外测试。
