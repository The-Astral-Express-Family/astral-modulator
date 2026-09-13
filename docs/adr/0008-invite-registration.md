# ADR-0008: Human 注册采用一次性邀请码

- Status: Accepted
- Date: 2026-09-13

## Context

服务器当前只允许 bootstrap 创建首个 human 账号，之后 `POST /auth/register`
永久关闭（`registration closed: initial human already exists`）；成员管理
只能把**已存在**的 actor 加入 workspace。因此每个部署事实上是
「1 个人 + N 个 agent」，多 human 协作没有进入通道（round 23 梳理确认的
唯一硬设计缺口）。

需要裁决的是：第二个及之后的 human 如何产生账号。连带约束：密码/邮箱管理、
审批人收紧（D8 触发条件）等设计都挂在「账号如何产生」之上，须一并定调。

## Decision

采用**一次性邀请码注册**：

- 邀请由拥有 `workspace:manage_members` 的 human 针对**特定 workspace + 角色**
  签发（viewer/contributor/maintainer，不含 owner）；码一次性、TTL、可撤销、
  只存 hash；
- 持码者在 web 或 CLI 完成注册，同一事务内建号 + 兑换邀请 + 加入该 workspace；
- 衍生形式：`/register?code=` 链接（核心）、SMTP 邮件邀请链接（后续期）；
- bootstrap 保留为冷启动的「零号邀请」；`/auth/register` 按「带码=邀请兑换、
  无码=bootstrap-only」双分支。

详细设计（实体、端点、兑换事务、安全分析、分期）见
[registration.md](../registration.md)；实施任务登记 TODO.md §11（A5）。

## 备选方案（否决理由）

- **开放注册**：与自托管信任模型冲突，spam/滥用面大；如未来需要应做成显式
  配置项单独裁决。
- **服务器级管理员签发的全局邀请**：需要先发明「服务器管理员」角色与授权面；
  而账号本身零权限，workspace 绑定式邀请已覆盖真实场景。留待「先建号后找
  组织」需求真实出现。
- **邀请直接授予 owner**：绕过 `membership.promote_owner` approval 的显式
  生命周期与审计（D8 语义），形成提权后门。
- **Magic link 免密注册**：把邮件投递变成核心路径依赖，且与 D6 本地账号
  （CLI 场景必须有密码）冲突。

## Consequences

### Positive

- 多 human 协作有受控入口；每个账号可审计到一张邀请与签发人；
- 完全复用既有 scope（`workspace:manage_members`）、角色包与「条件更新 +
  事务内唯一约束」成熟模式，无新授权概念；
- 协议纯增量，protocol_version 不变。

### Negative

- 邮箱不做验证（MVP），存在占号冒用邮箱串的低危面（账号零权限封顶）；
- 邀请管理是新的管理面（web 卡片 + CLI 命令），有 UI/命令实现成本；
- 「注册即登录」使 bootstrap 注册响应补 `session` 字段（一次性契约附加）。
