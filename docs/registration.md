# 账号注册与邀请码设计

> 裁决来源：2026-09-13 用户裁决——注册形式采用**一次性邀请码注册**，web 与 CLI
> 均可注册；链接注册 / 邮件邀请链接为衍生分发形式。决策记录见
> [ADR-0008](adr/0008-invite-registration.md)，TODO.md §2 A5。
> P1（server）已于第 29 轮落地（2026-09-13）；本文与实现的出入已就地校准，
> 已实现契约的唯一事实来源是 api/openapi.yaml。

## 1. 目标与非目标

**目标**

1. 服务器从「单管理员」走向多 human 协作：管理员之外的第二、第三个 human
   有明确的进入通道（round 23 整理时确认的唯一硬设计缺口）。
2. 进入通道可控：每个账号的产生都对应一张可审计的一次性邀请，没有开放注册。
3. 复用既有模型：邀请绑定 workspace 与角色，兑换即入伙；不新增服务器级
   管理员角色、不新增授权面。

**非目标（MVP 明确不做，见 §8）**

- 邮箱验证（email 在 MVP 里只是登录名）；
- 未绑定 workspace 的服务器级邀请；
- 已有账号者凭邀请码自助加入第二个 workspace；
- 邀请直接授予 owner 角色。

## 2. 模型

### 2.1 Invitation 实体（migration `00013_workspace_invitations`）

| 字段 | 说明 |
|------|------|
| `id` | `inv_<uuidv7>`（新 ID 前缀 `inv`，登记于 openapi Id schema 说明与 TODO.md §9） |
| `workspace_id` | 绑定的 workspace（邀请即「加入该 workspace」的凭证） |
| `role` | `viewer` / `contributor` / `maintainer`（**不含 owner**，见 §6.3） |
| `code_hash` | 邀请码的 sha256（唯一索引）；**明文码不落库** |
| `created_by` | 签发人 actor_id（human） |
| `created_at` / `expires_at` | TTL：默认 7d，创建时可指定，上限 30d |
| `status` | `invited` → `redeemed` \| `revoked`（`expired` 是派生态：`invited` 且 now > `expires_at`） |
| `redeemed_by` / `redeemed_at` | 兑换者与时间（兑换即注册成功） |

### 2.2 邀请码形状

- Crockford base32、20 字符（100 bit 熵），展示分组 `XXXXX-XXXXX-XXXXX-XXXXX`；
- 与 device flow 的 `user_code`（人短时手输，生命周期 10min）不同：邀请码要在
  邮箱/聊天里存活数天，防御对象是离线爆破，因此熵高两个量级；
- 存储 hash（与 tag confirm_code、credential secret 同一模式）；明文只在
  创建响应里出现**一次**，之后任何接口/日志/审计不得再现。

### 2.3 链接形态

```
{WebBaseURL}/register?code=<code>
```

绝对 URL 拼装沿用 round 28 为 device `verification_uri` 建立的回退链：
`ASTRAL_WEB_BASE_URL` → `ASTRAL_PUBLIC_URL` → 请求 Host（与 device create
同一 helper，不再另造拼装点）。

## 3. 端点契约（草案，正式形状以实施轮 openapi 为准）

| 端点 | 授权 | 语义 |
|------|------|------|
| `POST /workspaces/{id}/invitations` | human session + `workspace:manage_members` | 签发。body `{role, expires_in?}`（秒，缺省 7d 上限 30d）→ 201 `{id, code, invite_url, workspace_id, role, status, created_at, expires_at}`，`code` 明文与拼好的 `invite_url` 仅本次返回 |
| `GET /workspaces/{id}/invitations?status=` | human session + `workspace:manage_members` | 列表（`{items,next_cursor}` 分页信封），**不含 code**（库里只有 hash） |
| `POST /invitations/{id}/revoke` | human session + `workspace:manage_members` | `invited`→`revoked`；对已关闭（redeemed/revoked）的撤销**幂等 204**；不存在 404 |
| `POST /auth/register`（扩展） | 匿名（凭邀请码） | body 增 `invite_code`：兑换 + 建号 + 入伙一个事务；**注册成功即建立 web 会话**（响应 = 现有 `MeResponse` + `session`，与 login 同形状） |

**错误码**（新增，实施时登记）：

- `INVITE_INVALID`（400）——不存在 / 已兑换 / 已撤销 / 已过期**统一此码同文案**，
  不给区分（防探测；持有者对码状态无合法需求）；
- `EMAIL_TAKEN`（409）——邮箱已被注册。仅在邀请码验证通过后才会暴露
  （要求持有效码才可探测，攻击者已有合法入伙通道，泄露面可接受）；
- 非法 role / 缺字段 → 既有 `VALIDATION_FAILED`。

**授权要点**：签发/列表/撤销必须是 **human session**——agent credential 一律
403（与 device 审批同语义：程序不能替人决定谁能进来）。兑换（注册）是匿名端点，
唯一凭证就是邀请码本身。

## 4. 兑换流程（注册事务）

复用 tag 两步确认验证过的模式（条件更新抢状态 + 事务内唯一约束兜底）：

```
1. normalize code → sha256 → 按 code_hash 查 invitation
2. status ≠ invited 或已过期 → INVITE_INVALID（与查无同码同文案）
3. 校验 email 形状 / password 策略（复用 auth/password.go 既有规则）
4. 事务开：
   a. INSERT actor(human) + human_auth（email 唯一索引兜底 → EMAIL_TAKEN）
   b. UPDATE workspace_invitations
      SET status='redeemed', redeemed_by=<actor>
      WHERE id=? AND status='invited'          ← 条件更新，抢不到即并发已用
   c. INSERT workspace_members(workspace, actor, role)（账号为本事务新建，不存在既有成员行）
   d. audit：invite.redeem + auth.register 两条（同事务三件套惯例）
   e. outbox：security.invite.redeemed 事件
5. 建会话（access+refresh，与 login 同管线），setSessionCookie
```

并发同码：条件更新只允许一个事务成功，输家整体回滚（actor 不残留）。
email 撞车：唯一约束在 `a` 步先挡，邀请不消耗。

## 5. 三个入口的流程

### 5.1 Web 注册

- 路由 `/register`（公开，匿名专属；已登录访问弹回总览，与 /login 守卫对称；
  布局与 round 27 独立化后的 /device 同构——顶层路由、全屏居中，不带应用
  外壳，方便从邮件/聊天链接直达）；
- 从 `?code=` 预填邀请码（也支持手动输入）；
- 提交 → 注册 + 自动登录 → 按 `from` 参数或默认跳转；
- 「避免盲输码」裁决为**不设 preview 端点**：preview 是一次免 bcrypt、免建号的
  轻量探测 oracle，对防爆破只有坏处；签发响应已带 `invite_url`（含 workspace
  语境的链接由分发者转达），注册成功后的会话可自行拉 workspace 列表回显。

### 5.2 CLI 注册

- `astral register <server_url> [--invite-code CODE] [--email ... --password ...]`
  （缺省项交互提示；`--json` 机器可读）；
- **CLI 注册只建号不入会话**：CLI 的会话载体是 device flow 产出的凭证存储
  （D12），与 web cookie 是不同通道。「注册即登录」仅指 web 会话——CLI 注册
  成功后提示 `astral login <server_url>` 走设备流；
- web 与 CLI 共用同一个 `POST /auth/register`，CLI 不新增服务端面。

### 5.3 与 bootstrap 的关系

- bootstrap（首个 human，无邀请码）保留，是冷启动的「零号邀请」内置语义；
- `/auth/register` 两分支：带 `invite_code` → 邀请兑换；不带 → 既有
  bootstrap-only 守卫（已有 human 即 403）；
- 「注册即登录」对两分支统一：bootstrap 注册响应也补 `session`（一次性运维
  动作，契约附加无兼容负担）。

## 6. 安全设计

1. **码即凭证**：100 bit 随机 + hash 存储 + 单次使用 + TTL + 可撤销，四层
   兜底；链接泄露的处置就是撤销。
2. **防枚举**：无效码四种原因一个错误码；注册与邀请签发纳入 §11 的
   rate limit 项（auth/device 端点同批）。
3. **owner 不经邀请**：角色晋升必须走 `membership.promote_owner` approval
   （显式生命周期 + TTL + 审计，D8）；邀请最高到 maintainer，堵住绕过审批
   直接造 owner 的口子。
4. **审计与事件**：签发/撤销/兑换三条 audit（`invite.create/revoke/redeem`）；
   `security.invite.created/revoked/redeemed` 进 workspace 事件流（沿用
   `security.*` 先例），管理员在总览页可见「谁进来了」。
5. **邮箱不验证（MVP）**：email 只是登录名；不做验证意味着可能冒用他人邮箱
   注册——由于账号本身零权限（无 membership 则一切 404），危害上限是
   「占住一个邮箱串」；真实验证随 P4 邮件能力一并评估。

## 7. 分期

| 期 | 内容 | 交付 |
|----|------|------|
| P1 server | migration 00013、邀请三端点、register 扩展、audit/事件/错误码、openapi + 单测 + 契约测试；protocol_version 不变（纯增量） | 邀请闭环可用（curl/CLI 可走通） |
| P2 web | `/register` 路由与页面；workspace 邀请管理卡（签发/列表/复制链接/撤销）；守卫对称性 | 非技术成员可被邀请进场 |
| P3 CLI | `astral register` 命令；协议快照刷新并入 §11 既有 v2.1 快照项 | 全 CLI 流程 |
| P4 衍生 | SMTP 邮件邀请（.env 增 `SMTP_*`）、邮件模板含链接 | 邀请分发不必人工传码 |

## 8. 明确不做与后续项

| 项 | 状态 | 理由 / 重启条件 |
|----|------|----------------|
| 服务器级（不绑 workspace）邀请 | 不做 | 需要引入「服务器管理员」概念；账号零权限所以绑定式邀请已覆盖真实需求。出现「先建号后找组织」的真实场景再裁决 |
| 已有账号凭码自助加入 | 不做 | 与 addMember（按 actor_id 直加）职责重叠；作为邀请链接的 v2 体验项 |
| 邀请授予 owner | 永不 | 见 §6.3 |
| 邮箱验证 | P4 后评估 | MVP 邮箱仅登录名，危害上限低（§6.5） |
| 开放注册 | 永不（默认） | 与自托管信任模型冲突；如需要应做成显式配置项并单独裁决 |
