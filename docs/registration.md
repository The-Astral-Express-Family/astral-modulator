# 账号注册与邀请码设计（双轨分离）

> 裁决来源：2026-09-13 用户裁决（ADR-0008）确立一次性邀请码注册；
> 2026-09-29 用户裁决（[ADR-0009](adr/0009-invitation-split.md)）确立
> **注册邀请与工作区邀请双轨分离**——两者结构同构但必须是两个东西：
> 注册仍与注册邀请码强绑定（防海量注册），工作区邀请是权限授予。
> 已实现契约的唯一事实来源是 api/openapi.yaml；本文与实现的出入就地校准。

## 1. 两条不变量（验收标准）

1. **注册闸门**：任何账号创建都消耗且仅消耗一张**注册邀请码**
   （单次有效、带 TTL、只存 hash、平台管理员签发）。攻击者拿不到
   注册码就造不出账号——海量注册被结构性挡住。每个账号 ↔ 恰好一张
   已兑换注册码，一一对账可审计。
2. **工作区邀请 = 权限授予**：一张码绑定「某 workspace + 某角色」，
   **任何已存在的 human actor** 都能兑换，兑换即建 membership。
   它不产生任何注册资格；工作区管理员不能（直接或间接）铸注册资格。

推论：注册端点只认注册码（工作区码出现 = 与查无同文案的
`INVITE_INVALID`）；入伙端点只认工作区码（注册码出现同样
`INVITE_INVALID`）。两轨错误面镜像对称。

**非目标（MVP 明确不做，见 §8）**：邮箱验证；配对签发（创建工作区
邀请时自动配发注册码——已裁决否决，见 ADR-0009）；邀请授予 owner。

## 2. 模型

### 2.1 RegistrationInvitation（`registration_invitations`，00018 落地为 platform_invitations，00022 更名）

| 字段 | 说明 |
|------|------|
| `id` | `reg_<uuidv7>`（00022 起新前缀 `reg`；更名前存量行残留 `inv_`，前缀只服务可读性） |
| `code_hash` | 码的 sha256（唯一索引）；**明文码不落库** |
| `created_by` | 签发人 actor_id（platform admin） |
| `created_at` / `expires_at` | TTL 默认 7d，签发可指定，上限 30d |
| `status` | `invited` → `redeemed` \| `revoked`（`expired` 派生：`invited` 且 now > `expires_at`） |
| `redeemed_by` / `redeemed_at` | 兑换者与时间（兑换即建号成功） |

无 workspace/role 维度：兑换只建号（普通 user），不入任何 workspace。

### 2.2 Invitation（`workspace_invitations`，00013，结构不变）

| 字段 | 说明 |
|------|------|
| `id` | `inv_<uuidv7>` |
| `workspace_id` | 绑定的 workspace（码即「加入该 workspace」的凭证） |
| `role` | `viewer` / `contributor` / `maintainer`（**不含 owner**，见 §6.3） |
| `code_hash` / `created_by` / TTL / `status` / `redeemed_*` | 与注册邀请同构 |

两表共用同一码形状与哈希空间，但**互不通用**：查表是端点语义决定的，
不存在跨表回退（00018 时期 register 的双表回退已于 ADR-0009 删除）。

### 2.3 邀请码形状（两轨共用）

- Crockford base32、20 字符（100 bit 熵），展示分组 `XXXXX-XXXXX-XXXXX-XXXXX`；
- 与 device flow 的 `user_code`（人短时手输，10min 生命周期）不同：邀请码
  要在邮箱/聊天里存活数天，防御对象是离线爆破，熵高两个量级；
- 存储 hash（与 tag confirm_code、credential secret 同一模式）；明文只在
  创建响应里出现**一次**，之后任何接口/日志/审计不得再现。

### 2.4 链接形态

```
注册码：{WebBaseURL}/register?code=<code>     # 面向未注册者
工作区码：{WebBaseURL}/join?ws=<code>          # 面向已注册者（ADR-0009 起）
```

绝对 URL 拼装沿用 device `verification_uri` 的回退链：
`ASTRAL_WEB_BASE_URL` → `ASTRAL_PUBLIC_URL` → 请求 Host（同一 helper）。

## 3. 端点契约（正式形状以 api/openapi.yaml 为准）

### 3.1 注册轨

| 端点 | 授权 | 语义 |
|------|------|------|
| `POST /auth/register` | 匿名（凭注册码） | body `{email, password, display_name?, registration_code?}`：兑换注册码 + 建号（普通 user，不入任何 workspace）+ **建 web 会话**一个事务；`registration_code` 为空保持 bootstrap-only 守卫（已有 human 即 403） |
| `POST /admin/registration-invitations` | `platform:users:manage` | 签发注册码；code 明文 + `invite_url`（/register?code=）仅本次返回；TTL 默认 7d 上限 30d |
| `GET /admin/registration-invitations` | `platform:users:manage` | 列表（id 降序游标分页）；不回传 code |
| `POST /admin/registration-invitations/{id}/revoke` | `platform:users:manage` | 撤销；重复撤销幂等 204 |

### 3.2 工作区轨

| 端点 | 授权 | 语义 |
|------|------|------|
| `POST /workspaces/{id}/invitations` | human session + `workspace:manage_members` | 签发；body `{role, expires_in?}` → 201 `{..., code, invite_url}`（/join?ws=），明文仅本次返回 |
| `GET /workspaces/{id}/invitations?status=` | human session + `workspace:manage_members` | 列表；**不含 code** |
| `POST /invitations/{id}/revoke` | human session + `workspace:manage_members` | `invited`→`revoked`；幂等 204；不存在 404 |
| `POST /invitations/redeem` | human session（agent 403——入伙是人的行为） | body `{code}`：兑换入伙，见 §4.2 |

**错误码**：

- `INVITE_INVALID`（400）——查无 / 已兑换 / 已撤销 / 已过期 / **类型不符**
  （工作区码进注册端点、注册码进 redeem）**统一此码同文案**，不给区分
  （防探测；持有者对码状态无合法需求）；
- `ALREADY_MEMBER`（409，ADR-0009 新增）——redeem 时已是该 workspace
  成员（持另一张有效码）；码不消耗。只陈述本人身份事实，不泄露码状态；
- `EMAIL_TAKEN`（409）——邮箱已被注册。仅在注册码验证通过后暴露
  （要求持有效码才可探测，泄露面可接受）；
- 非法 role / 缺字段 → 既有 `VALIDATION_FAILED`。

## 4. 兑换流程（两轨各自的事务）

### 4.1 注册事务（`registerWithRegistrationCode`，只查 registration_invitations）

```
1. normalize code → sha256 → 按 code_hash 查 registration_invitations
   （查无 → INVITE_INVALID；工作区码在此必然查无，同文案被拒）
2. status ≠ invited 或已过期 → INVITE_INVALID
3. 校验 password 策略
4. 事务开：
   a. INSERT actor(human, platform_role=user) + human_auth（email 唯一
      索引兜底 → EMAIL_TAKEN，此时码不消耗、可换邮箱重试）
   b. UPDATE registration_invitations
      SET status='redeemed', redeemed_by=<actor>
      WHERE id=? AND status='invited'          ← 条件更新抢状态
   c. audit：invite.redeem（服务器级，scope=registration）+ auth.register
   d. outbox：security.invite.redeemed（workspace_id 空，scope=registration）
5. 建会话（与 login 同管线），setSessionCookie
```

并发同码：条件更新只允许一个事务赢，输家整体回滚（actor 不残留）。

### 4.2 入伙事务（`redeemInvitation`，只查 workspace_invitations）

```
0. RequireHuman（agent credential 403）；code 空 → VALIDATION_FAILED
1. normalize → hash → 查 workspace_invitations（查无/注册码 → INVITE_INVALID）
2. 幂等窗口（先于失效判定）：码已 redeemed 且 redeemed_by=本人，
   且成员行仍在 → 200 直接返回入伙结果（网络重试安全，不产生新事件）
3. status ≠ invited 或已过期 → INVITE_INVALID
4. 已是该 workspace 成员（另一张码）→ 409 ALREADY_MEMBER，码不消耗
5. 事务开：
   a. 条件更新抢状态（同 4.1b）
   b. INSERT workspace_members(ws, actor, role)
      （并发窗口被 addMember 抢先 → 唯一约束 → 整体回滚 → ALREADY_MEMBER）
   c. audit：invite.redeem（via=redeem）
   d. outbox：security.invite.redeemed（via=redeem）
      + workspace.member.changed（change=joined，与 addMember 同事件面）
6. 200 {workspace, role}
```

## 5. 各入口的流程

### 5.1 Web

- `/register`（公开，匿名专属）：`?code=` 预填**注册码**；注册只建号；
  已登录访问被守卫转 `/join`（邻接动作是入伙，不是注册）。
- `/join?ws=`（auth required）：工作区邀请链接落点；匿名先跳登录、
  成功后原路返回自动兑换；带码自动兑一次，失败降级手动输入。
- 侧栏「加入工作区」对话框：登录态手动兑码入口（与新建工作区并排）。
- 管理面：平台 admin 在用户管理页签发注册码；workspace admin 在总览
  邀请卡签发工作区码。
- 「避免盲输码」仍**不设 preview 端点**：preview 是免 bcrypt、免建号的
  轻量探测 oracle，对防爆破只有坏处。

### 5.2 CLI

- `astral register <server_url> (--registration-code CODE | --bootstrap) ...`
  （ADR-0009 起 flag/字段随契约更名，见 astral-cli docs/ARCHITECTURE §6.4）；
  201 后自动衔接 device-flow login（web cookie 会话 CLI 消费不了）；
- CLI 侧 `astral join <code>` 命令**不做**（记 TODO 移交，CLI 场景入组
  需求薄）；协议快照随本批契约变更刷新。

### 5.3 与 bootstrap 的关系

- bootstrap（首个 human，无码）保留，是冷启动的「零号邀请」内置语义，
  仍种 org-memory workspace（见 architecture §6.5）；
- `/auth/register` 两分支：带 `registration_code` → 注册码兑换（**不种**
  org-memory——那是冷启动专属）；不带 → bootstrap-only 守卫。

新用户进某 workspace 的完整旅程（双码）：平台 admin 发注册码 → 注册建号
登录 → workspace admin 发工作区码 → `/join` 链接或侧栏兑码入伙。

## 6. 安全设计

1. **码即凭证**：100 bit 随机 + hash 存储 + 单次使用 + TTL + 可撤销，
   四层兜底；链接泄露的处置就是撤销。
2. **防枚举**：无效码五种原因（含类型不符）一个错误码、同文案；注册
   端点纳入敏感限流桶（login/register，10/min/IP）；redeem 是认证端点
   （通用桶，按 actor 记账）且 100 bit 熵使在线爆破不可行。
3. **owner 不经邀请**：晋升必须走 `membership.promote_owner` approval
   （D8）；两轨码最高到 maintainer。
4. **审计与事件**：两轨各自 `invite.create/revoke/redeem` 三条 audit
   （注册轨服务器级 scope=registration，工作区轨 workspace 级）；
   `security.invite.created/revoked/redeemed` 一族事件（注册轨
   workspace_id 空，广播到所有 workspace 流；工作区轨定向）。redeem
   额外发 `workspace.member.changed(change=joined)` 对齐 addMember。
5. **注册资格稀缺且可审计**：只有 platform admin 能签发注册码；工作区
   管理员的签发能力到「入伙」为止，无法铸造注册资格（ADR-0009 裁决
   配对签发为否决项，正是为了守住这一点）。
6. **邮箱不验证（MVP）**：email 只是登录名；冒用的危害上限是「占住一个
   邮箱串」（零权限账号），真实验证随邮件能力一并评估。

## 7. 落地历史

| 批次 | 内容 |
|------|------|
| P1 server（round 29） | 00013 工作区邀请 + 三端点 + register `invite_code` 分支（注册即入伙） |
| P2 web（round 30） | /register 页 + 邀请卡 |
| P3 CLI（astral-cli 3822b29） | `astral register` |
| 平台邀请（00018） | `platform_invitations` + `/admin/invitations` + register 双表回退 |
| **双轨分离（00022 / ADR-0009，2026-09-29）** | 更名 registration_invitations；register 收窄为 `registration_code`（删回退）；新增 `POST /invitations/redeem`（ALREADY_MEMBER 幂等语义）；`/admin/registration-invitations` 改名；web `/join` + 侧栏兑码；CLI 字段/flag 更名 |

## 8. 明确不做与后续项

| 项 | 状态 | 理由 / 重启条件 |
|----|------|----------------|
| 配对签发（工作区邀请自动配发注册码） | **否决（ADR-0009）** | 破坏「注册资格仅平台 admin 可铸」的稀缺性；纯双码分离语义最纯，两码协调成本已被接受 |
| 已有账号凭码自助加入 | ✅ 2026-09-29 落地 | `POST /invitations/redeem`（ADR-0009；原「不做」裁决被双轨分离推翻——入伙成了工作区码的唯一兑换路径） |
| 邀请授予 owner | 永不 | 见 §6.3 |
| 邮箱验证 | 后评估 | MVP 邮箱仅登录名，危害上限低（§6.6） |
| 开放注册 | 永不（默认） | 与自托管信任模型冲突；如需要应做成显式配置项并单独裁决 |
| 站内通知系统（入伙通知） | 不做（本批） | `security.invite.redeemed` SSE 事件已覆盖工作区维度；无 per-actor 事件通道，真实需求出现再评估 |
| CLI `astral join` | TODO 移交 | astral-cli 仓；CLI 入组需求薄 |
