# 重构队列：邀请系统双轨分离（纯双码分离）

> 工作状态文件（repo-refactor 工作流），完工后删除。裁决记录见 ADR-0009（阶段 G 落盘）。
> 不变量 1：账号创建 = 恰好一张注册邀请码，仅平台管理员签发。
> 不变量 2：工作区邀请 = 权限授予（ws+role），任何已存在 human 可兑，不产生注册资格。

## 基线（2026-09-29，merge origin/main 后）

- server `go test ./...` 绿
- web `npm run build` 绿（先 `npm ci` 同步 merge 带入的 lockfile；旧 vite dev server 锁 node_modules 已杀）
- redocly lint 绿
- gen:api 零 drift

## 队列

- [x] 阶段 0：merge origin/main（冲突 .github/workflows/ci.yml 触发策略取 `**`+tags 侧，覆盖 dev push 意图）
- [ ] A1 fix(web): main.ts 导入 vue-sonner/style.css
- [ ] A2 fix(web): adoptMeAndRenew 容忍静默刷新失败
- [ ] A3 fix(server): token/refresh 移出敏感桶
- [ ] B: 00019 rename platform_invitations→registration_invitations + admin 模块/路由/DTO/openapi/web 改名 + reg 前缀
- [ ] C: register 收窄 registration_code（删 workspace 回退）+ 测试重写（契约门：ws 码注册=INVITE_INVALID）
- [ ] D: POST /invitations/redeem + ALREADY_MEMBER + workspace invite_url→/join?ws=
- [ ] E: /join 路由 + 侧栏兑码对话框 + RegisterView/InvitationsCard/AdminUsersView 文案
- [ ] G: registration.md 重写 + ADR-0009 + security.md/architecture.md + TODO §3/§9 登记
- [ ] F: astral-cli（D:\Code\astral-cli）字段/flag 改名 + docs + protocol/snapshots/v2 刷新
- [ ] 终验：go test + web build + redocly + E2E 三旅程 + 行为 delta log

## 行为 delta log（≥L3）

（执行中逐条补）
