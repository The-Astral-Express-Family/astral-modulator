# Astral Modulator 部署与发行初稿

> 状态：Draft
>
> 重点：CLI 三端分发必须是一级产品能力。

## 1. 发布目标

用户应能在 Windows、macOS、Linux 上快速得到一个可信的 `astral` CLI，并明确知道如何升级和卸载。

优先顺序：

1. 官方 Release Asset；
2. 主流包管理器；
3. 原生安装包；
4. 自动升级辅助。

## 2. CLI 构建基线

推荐：

- C++20；
- CMake；
- `CMakePresets.json`；
- vcpkg manifest mode；
- native OS CI runner；
- Release/RelWithDebInfo 两类产物；
- SHA-256 checksums；
- stable tag 签名；
- SBOM；
- `astral version --json` build metadata。

vcpkg manifest mode 使用项目级 `vcpkg.json` 声明依赖，官方文档仍将其推荐给大多数项目，适合 CI 与依赖版本管理。

## 3. Release Matrix

| OS | Arch | Asset | Package manager |
|---|---|---|---|
| Windows | x86_64 | zip | Scoop, WinGet |
| Windows | arm64 | zip | WinGet/Scoop（需求验证后） |
| macOS | arm64 | tar.gz | Homebrew |
| macOS | x86_64 | tar.gz | Homebrew |
| Linux | x86_64 glibc | tar.gz | Homebrew/deb/rpm |
| Linux | arm64 glibc | tar.gz | deb/rpm |
| Linux | x86_64 musl | tar.gz | direct |
| Linux | arm64 musl | tar.gz | direct |

MVP 可以先缩小到 Windows x86_64、macOS arm64/x86_64、Linux x86_64，再根据真实用户加入其余矩阵，但构建系统不要写死架构。

## 4. Windows

### 直接下载

```text
astral-vX.Y.Z-windows-x86_64.zip
  astral.exe
  LICENSE
  NOTICE/THIRD_PARTY_LICENSES (if needed)
```

### Package Manager

优先：

1. Scoop；
2. WinGet；
3. 后续 MSI/MSIX。

### Runtime

需要早期决定 MSVC CRT 策略：

- `/MT`：减少 VC Runtime 安装依赖，但增大体积并影响某些依赖构建方式；
- `/MD`：通常更常规，但目标机器需要匹配 Runtime。

推荐做两端干净 VM 测试，而不是仅凭理论选择。

### Signing

Stable release 使用 Authenticode code signing。CI secret/签名证书应隔离，PR 构建不接触生产签名凭证。

## 5. macOS

### Assets

分别发布：

```text
astral-vX.Y.Z-macos-arm64.tar.gz
astral-vX.Y.Z-macos-x86_64.tar.gz
```

验证稳定后可额外 universal2，但双架构产物更易定位问题。

### Homebrew

建议维护独立 tap，例如组织级 `homebrew-tap`。Homebrew 的 tap 机制允许通过 Git 仓库分发第三方 formula。

### Signing & Notarization

Stable release：

- Developer ID signing；
- notarization；
- stapling/验证；
- CI 使用受限 signing credential。

CLI 虽不是 `.app`，仍应验证 Gatekeeper 实际体验。

## 6. Linux

Linux 二进制兼容需要重点设计。

### 方案 A：glibc baseline

在足够旧的受控发行版/容器构建，使二进制可运行在较新系统。

优点：与系统生态兼容自然。

缺点：需要维护 baseline 与依赖兼容。

### 方案 B：musl static

额外发布 musl 静态版本。

优点：可移植性强。

缺点：DNS、TLS、locale、某些第三方库行为需要充分测试。

推荐同时评估 A+B，不要盲目声称“静态链接 = 所有 Linux 都兼容”。

### Package Formats

顺序：

1. tar.gz；
2. deb；
3. rpm；
4. Linuxbrew；
5. 其他生态按用户需求。

Snap/Flatpak 不作为 CLI 首选：严格 sandbox 与任意 workspace 文件访问存在天然摩擦。AppImage 可以作为便携备选，但对单个 CLI 的价值低于 tarball + package manager。

## 7. CPack

CPack 可帮助生成：

- ZIP/TGZ；
- DEB；
- RPM；
- 某些 installer formats。

但不要让 CPack 承担全部发行职责。Homebrew/Scoop/WinGet metadata、平台签名、notarization、GitHub Release 都应由 release workflow 独立编排。

## 8. 推荐 GitHub Actions 结构

```text
.github/workflows/
  ci.yml
  release.yml
  nightly.yml          # optional
  security.yml
```

### `ci.yml`

PR/push：

- Linux build + tests；
- Windows build + tests；
- macOS build + tests；
- clang-format/lint；
- server tests；
- web typecheck/tests；
- OpenAPI validation；
- dependency/license scan。

### `release.yml`

Tag `vX.Y.Z`：

```text
validate version
 -> build matrix
 -> test packaged binary
 -> assemble archive
 -> SBOM
 -> checksums
 -> sign
 -> publish GitHub Release
 -> update package repositories
```

Package repository 更新最好通过自动 PR，而不是 release job 直接无审查推主分支。

## 9. Artifact Naming

统一：

```text
astral-v0.1.0-windows-x86_64.zip
astral-v0.1.0-macos-arm64.tar.gz
astral-v0.1.0-linux-x86_64-gnu.tar.gz
astral-v0.1.0-linux-x86_64-musl.tar.gz
checksums.txt
checksums.txt.sig
sbom.spdx.json
```

不要随平台任意改变架构命名。

## 10. 安装方式

### macOS/Linux Homebrew

```bash
brew install <org>/tap/astral
```

### Windows Scoop

```powershell
scoop bucket add astral <bucket-url>
scoop install astral
```

### Windows WinGet

```powershell
winget install <PackageId>
```

### Direct

文档必须提供：

- 如何选择 OS/arch；
- checksum 验证；
- signature 验证；
- 放入 PATH；
- `astral doctor`；
- 卸载路径。

不建议把 `curl | sh` 作为唯一官方入口。若提供 convenience installer，脚本必须校验下载内容，并可以先下载后检查脚本源码。

## 11. 升级

### Package manager install

由 Homebrew/Scoop/WinGet/apt/dnf 升级。

### Direct install

MVP：

```text
astral update check
```

后续可以：

```text
astral update
```

但默认不静默自动升级。Agent 自动化需要版本可复现。

## 12. 版本兼容

Server capability endpoint 返回：

- protocol version；
- minimum CLI version；
- feature flags。

CLI 发现不兼容时：

- 明确错误；
- 给出当前/最低版本；
- 不继续执行可能破坏状态的写操作。

## 13. 服务端部署

### Local Dev

Docker Compose：

```text
astral-server
postgres
nats (profile/optional)
```

### Small Self-hosted

推荐：

```text
reverse proxy / TLS
  -> astral-server
  -> PostgreSQL
```

不要求 Kubernetes。

### Binary Deployment

若服务端用 Go，可发布 `astral-server` 单二进制，配置：

```text
/etc/astral/server.toml
DATABASE_URL=...
ASTRAL_PUBLIC_URL=...
```

Secrets 使用环境变量/secret manager，不提交配置库。

### Scale-out

达到需要时：

- stateless API replicas；
- managed PostgreSQL；
- NATS JetStream；
- object storage；
- worker；
- centralized logs/metrics/traces。

## 14. 数据库升级

- migration 具有单调版本；
- deploy 前备份；
- forward-only 优先；
- destructive migration 分两个 release 完成；
- server 启动检查 schema compatibility；
- 不让每个并发实例无协调自动跑 destructive migration。

## 15. Release Channels

- `stable`
- `rc`/`beta`
- `nightly`（可选）

Workspace policy 可限制 Agent 允许的最低版本，避免老 CLI 与新协议不一致。

## 16. 分发方案对比

| 方案 | 安装简单 | 三端 | 签名/信任 | 维护成本 | 推荐 |
|---|---:|---:|---:|---:|---|
| GitHub Release archive | 高 | 高 | 需自建 | 低 | MVP 必须 |
| Homebrew | 高 | macOS/Linux | 较好 | 中 | 推荐 |
| Scoop | 高 | Windows | checksum | 低/中 | 推荐 |
| WinGet | 高 | Windows | 生态好 | 中 | 稳定版推荐 |
| DEB/RPM repo | 高 | Linux | 好 | 中/高 | 第二阶段 |
| MSI/MSIX | 高 | Windows | 好 | 高 | 企业需求后 |
| AppImage | 中 | Linux | 一般 | 中 | 可选 |
| Snap/Flatpak | 中 | Linux | sandbox | 中 | CLI 非首选 |

## 17. 参考资料

- CMake CPack: https://cmake.org/cmake/help/latest/manual/cpack.1.html
- vcpkg manifest mode: https://learn.microsoft.com/vcpkg/consume/manifest-mode
- Homebrew taps: https://docs.brew.sh/Taps
- WinGet manifests: https://learn.microsoft.com/windows/package-manager/package/manifest
- Scoop manifests: https://github.com/ScoopInstaller/Scoop/wiki/App-Manifests
