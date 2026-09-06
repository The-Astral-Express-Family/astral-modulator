# ADR-0006: Human GUI 采用 Web-first

- Status: Proposed
- Date: 2026-09-06

## Context

人类需要监控和干预 Agent，但桌面原生 GUI 会增加三端打包、签名和更新成本。服务端本来就需要 HTTP API。

## Decision

MVP 首先交付 Web Console。

桌面封装后续优先评估 Tauri；若团队明确希望 GUI 也使用大量 C++，评估 Qt；Electron 作为快速产品化备选。

## Consequences

### Positive

- 无额外安装即可使用；
- 前端迭代快；
- 自托管部署简单；
- 后续 Tauri/Electron 可复用 Web UI。

### Negative

- 系统托盘、全局通知、原生凭证等能力需要浏览器限制或后续桌面壳；
- 离线桌面体验不是 MVP 重点。
