# ADR-0009: 注册邀请与工作区邀请双轨分离

- Status: Accepted
- Date: 2026-09-29
- Supersedes: [ADR-0008](0008-invite-registration.md)（邀请码注册的核心裁决保留；
  其「邀请绑定 workspace、注册即入伙」部分被本 ADR 取代）

## Context

ADR-0008 落地后，邀请体系经历了一次增补（00018 平台级注册邀请），
`POST /auth/register` 形成**双表回退**：先查 `platform_invitations`，
未命中回退 `workspace_invitations`。两种语义不同的码共用一个注册端点、
共用同一 `/register?code=` 链接形态、共用 `inv_` 前缀——唯一判别是
服务端查表顺序。由此产生的问题：

1. **语义混淆**：「工作区管理员签发的码可以注册账号」——工作区权限授予
   与平台注册资格两条不相干的轴被捏在一个端点里；用户侧表现为
   「工作区邀请竟然绑定在注册 code 上」，产品语义不可解释。
2. **防滥用面受损**：任何 workspace 的 maintainer（持
   `workspace:manage_members`）签发一张码就能铸出一个注册资格，
   注册闸门事实上被下放到 workspace 层。
3. **审计不对称**：账号 ↔ 注册码不再一一对应（账号可能由工作区码产生，
   注册码表查无对账行）。

用户裁决（2026-09-29）：**注册邀请码和工作区邀请码应该是两个东西**。
虽然结构可能一致，但必须做出明确区分；注册仍然与注册邀请码强绑定
（不希望有人通过注册海量账号攻击服务器）；工作区邀请码更像是
权限相关的。

## Decision

**纯双码分离**——两轨结构同构、彻底解耦：

1. **注册轨**（`registration_invitations`，00018 落地、00019 自
   `platform_invitations` 更名）：只管「允许注册」，仅平台管理员
   （`platform:users:manage`）经 `/admin/registration-invitations` 签发。
   `POST /auth/register` 只查此表（字段 `invite_code` → `registration_code`，
   删除双表回退），兑换只建号（普通 user），不入任何 workspace。
2. **工作区轨**（`workspace_invitations`，00013，结构不变）：码 =
   「某 workspace + 某角色」的权限授予；新增 `POST /invitations/redeem`
   （RequireHuman），**任何已存在 human** 凭码入伙。链接形态改为
   `/join?ws=`；`/register` 面向注册码。
3. **错误面镜像对称**：错轨的码（工作区码进注册、注册码进 redeem）与
   查无/失效统一 `INVITE_INVALID` 同文案（防探测）；redeem 独有
   `ALREADY_MEMBER`（409，已是成员且码不消耗）与本人幂等重试 200。

详细设计（实体、端点、两轨事务、安全分析）见
[registration.md](../registration.md)。

## 备选方案（否决理由）

- **配对签发**（创建工作区邀请时同事务自动配发一张绑定注册码，
  链接 `/register?reg=…&ws=…` 一条走完）：UX 最好，但任何 workspace
  maintainer 都能间接铸注册资格——「注册资格稀缺且仅平台 admin 可铸」
  被打开缺口；且注册路径重新耦合 workspace 逻辑（正是本次要删的）。
  用户裁决否决，接受双码协调成本。
- **服务端原子配对兑换**（注册请求带注册码，服务端经 source 关联同事务
  自动入伙）：单请求原子，但同样是把 membership 逻辑塞回注册事务，
  注册响应契约还要表达「是否已入伙」；否决。
- **维持现状（双表回退）**：违背上述裁决；否决。
- **站内通知系统发邀请**：需要发明 per-actor 事件通道与通知中心，
  实现面大且不解决语义混淆本身；码 + 链接的分发形态已闭环。否决
  （记 registration.md §8 重启条件）。

## Consequences

### Positive

- 注册闸门收紧到平台层：账号 ↔ 已兑换注册码一一对账，海量注册被
  结构性挡住（拿不到平台 admin 签发的码就建不了号）；
- 端点语义单一：register 只建号、redeem 只入伙，auth 模块不再触碰
  workspace 表；
- 已有账号自助加入 workspace 成为正式能力（原先明确不做）；
- 命名即语义：表名/路由/DTO/audit 前缀（registration-invitations、
  `reg_`、scope=registration）全线一致。

### Negative

- 新用户体验成本：进一个 workspace 需要两段码（平台 admin 发注册码 +
  workspace admin 发工作区码）、两步操作——裁决时已知并接受；
- **破坏性契约变更**：`invite_code` 字段更名、`/admin/invitations*`
  路径更名，CLI（astral-cli register 命令与协议快照）须同批跟进；
- `ALREADY_MEMBER` 为新增公网错误码（openapi ErrorCode enum 同步）；
- 存量部署的 `platform_invitations` 表与 `inv_` 前缀行经 00019 rename
  原样保留（前缀混杂仅影响可读性）。
