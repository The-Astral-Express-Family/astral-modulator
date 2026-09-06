# Astral Modulator 开发路线图初稿

> 状态：Draft
>
> 原则：先验证协作语义和分发链路，再扩功能。

## Phase 0 — Design Baseline

目标：在写业务代码前把最容易导致返工的决策做成可讨论的规格。

产出：

- `docs/requirements.md`
- `docs/architecture.md`
- `docs/protocol.md`
- `docs/cli-ux.md`
- `docs/sync-semantics.md`
- `docs/security.md`
- `docs/deployment.md`
- ADR 0001–0007
- 三个初始 Skills

Exit criteria：

- 团队对 Actor/Workspace/Task/Lease/Presence/Document/Message 定义没有根本歧义；
- 确认首个 server language；
- 确认 public protocol；
- 确认 CLI build/dependency baseline。

## Phase 1 — End-to-End Spike

只做一条链路：

```text
CLI login
 -> workspace
 -> create task
 -> claim task
 -> set presence
 -> send message
 -> SSE receive
 -> minimal web console sees event
```

### CLI

- CMake/vcpkg bootstrap；
- CLI11 command skeleton；
- libcurl HTTP；
- JSON output/error；
- auth token store abstraction；
- `doctor` 最小版。

### Server

- health/capabilities；
- Actor/Workspace/Task/Lease tables；
- auth stub/device flow integration；
- REST endpoints；
- PostgreSQL；
- outbox；
- SSE。

### Web

只要能看：

- actors；
- task state；
- event stream。

### CI

从 Spike 开始就跑 Windows/macOS/Linux CLI build。

Exit criteria：两个 CLI 实例在竞争同一 Task 时只有一个 Claim 成功，并且 GUI/SSE 可实时观察。

## Phase 2 — MVP Collaboration

增加：

- real Agent Credential lifecycle；
- scope/RBAC；
- Task full state machine；
- lease renew/expiry；
- presence heartbeat；
- message inbox/thread；
- audit；
- revoke connection；
- stable JSON contract。

Exit criteria：可以让两个真实 LLM Agent 完成一个有依赖的协作任务，不需要人工直接改数据库。

## Phase 3 — Markdown Sync

增加：

- managed path config；
- local manifest；
- remote manifest；
- pull/push/sync；
- optimistic revision；
- diff3 merge；
- conflict artifact；
- GUI conflict view。

Exit criteria：并发修改不会静默丢失，断网/重试后状态仍一致。

## Phase 4 — Distribution MVP

### Windows

- x86_64 zip；
- Scoop；
- signed stable executable。

### macOS

- arm64/x86_64 tar.gz；
- Homebrew tap；
- codesign/notarization。

### Linux

- x86_64 tar.gz；
- glibc compatibility test；
- musl spike；
- deb 或 rpm 至少一个。

共通：

- checksums；
- SBOM；
- release workflow；
- packaged-binary smoke test。

Exit criteria：新机器无需开发工具链即可安装并登录。

## Phase 5 — Human Control Plane

Web GUI：

- task board；
- agent presence；
- messages；
- document activity；
- conflicts；
- credential/membership；
- audit；
- force release/pause/revoke；
- approval requests。

Exit criteria：Human 可以只通过 GUI 监控协作并完成常见干预。

## Phase 6 — Integration & Scale

按真实需求选择，不预先承诺：

- GitHub App；
- GitLab；
- Jira/Linear；
- NATS JetStream；
- object storage；
- Tauri desktop shell；
- runtime adapter / actual process pause；
- plugin/event consumer SDK；
- organization/tenant；
- OIDC federation。

## 第一批 Issues 建议

### Docs / ADR

1. `docs: define MVP personas and non-goals`
2. `adr: choose CLI executable name`
3. `adr: choose C++ dependency manager`
4. `adr: choose server implementation language`
5. `adr: choose public API and realtime transport`
6. `spec: define task lease state machine`
7. `spec: define document conflict semantics`
8. `security: define agent credential lifecycle`

### CLI

9. `cli: bootstrap CMake and presets`
10. `cli: add vcpkg manifest`
11. `cli: add command skeleton`
12. `cli: add HTTP client and error envelope`
13. `cli: add JSON renderer and exit codes`
14. `cli: add credential store abstraction`
15. `cli: add event SSE prototype`

### Server

16. `server: bootstrap modular monolith`
17. `server: add PostgreSQL migrations`
18. `server: add capability endpoint`
19. `server: implement workspace and actor models`
20. `server: implement task claim transaction`
21. `server: implement outbox and SSE`

### Web

22. `web: bootstrap monitoring console`
23. `web: show live task and presence events`

### CI/Release

24. `ci: add three-OS CLI matrix`
25. `release: create preview archives and checksums`
26. `release: test clean-machine install`

### Skills

27. `skill: validate astral-install workflow`
28. `skill: validate astral-cli machine mode`
29. `skill: validate astral-collaboration workflow`

## MVP Definition of Done

- Human Device Flow login works；
- Agent uses independent scoped credential；
- Task Claim is atomic；
- stale Agent presence expires；
- stale Task Lease expires/revokes；
- Agent messaging works and is realtime；
- Document sync catches dual edits；
- Human can revoke token/lease；
- Audit can reconstruct actions；
- CLI JSON output is documented and tested；
- Windows/macOS/Linux downloadable assets pass smoke test；
- at least Homebrew + Scoop are functional；
- three Skills guide a fresh Agent through install → login → claim → work → sync → handoff。

## 暂缓项

除非用户验证强烈要求，不要在 MVP 前引入：

- CRDT；
- microservices；
- Kubernetes-only deployment；
- mandatory NATS/Redis；
- desktop-only GUI；
- complex plugin ABI；
- automatic Git commit/push；
- dozens of provider integrations。
