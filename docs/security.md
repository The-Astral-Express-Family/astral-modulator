# Astral Modulator 安全设计初稿

> 状态：Draft
>
> 本项目允许 LLM Agent 操作共享工作区，因此默认威胁模型必须假设：Agent 可能误解指令、被提示注入、持有过宽权限、运行在不受完全信任的主机上。

## 1. 安全目标

系统需要保证：

- Actor 身份可区分；
- Agent 不共享 Human 长期凭证；
- 权限可限制到 Workspace 与能力；
- Credential 可撤销、可过期、可轮换；
- 服务端进行最终授权；
- 敏感操作可要求 Human approval；
- 所有关键写操作可审计；
- Markdown/消息不能成为偷偷传递 secrets 的默认通道；
- CLI 日志、crash dump、debug 输出不泄露 token。

## 2. 威胁模型

### T1 Credential 泄露

来源：

- Agent prompt/log；
- shell history；
- CI log；
- `.astral/` 文件；
- crash report；
- message/document 内容。

缓解：

- Human 与 Agent Credential 分离；
- secret 只展示一次；
- OS Credential Store；
- headless 使用 secret injection；
- log redaction；
- short TTL；
- revoke/rotation；
- 禁止把 token 写入 Workspace 文档。

### T2 提示注入导致越权

恶意 Markdown 或消息可能诱导 Agent 执行高风险操作。

缓解：

- 工具权限由服务端 scope 控制，不依赖 prompt 约束；
- 高风险操作 Human approval；
- Agent 不能自行提升 scope；
- Integration credential 不暴露给 Agent；
- policy 可以限制 path/action。

### T3 多 Agent 竞争

两个 Agent 同时修改 Task/Document。

缓解：Task Lease + revision + Document conflict，禁止静默覆盖。

### T4 Workspace 越界

客户端利用 path traversal/symlink 访问 Workspace 外文件。

缓解：服务端 canonical path 校验；CLI 本地同样限制；禁止 `..`、绝对路径、symlink escape。

### T5 伪造 Presence

Agent 声称自己在工作但实际已离线。

缓解：Presence heartbeat TTL；Task ownership 使用独立 Lease，不信任 note/state。

### T6 Event/Message 重放

网络重试或攻击产生重复操作。

缓解：事件全局 ID、idempotency key、消费者幂等、token/session 校验。

### T7 被撤销 Token 继续工作

缓解：短 access token + server-side revocation metadata；关键写操作检查 Credential 状态；SSE/WebSocket 在 revoke 后主动断开。

## 3. Actor 与 Credential

### Human

Human 可以使用 OAuth/OIDC。CLI 推荐 Device Authorization Grant，因为终端应用不需要内嵌 WebView 或接收密码。

RFC 8628 定义的 Device Authorization Grant 适用于能够发起 HTTPS 请求并向用户展示授权 URI/代码的设备/客户端。

### Agent

Agent 使用独立身份：

```text
Human
  └─ creates Agent Identity
       └─ creates scoped Credential
```

Credential 建议记录：

```text
id
actor_id
workspace restrictions
scopes
created_by
created_at
expires_at
last_used_at
revoked_at
```

明文 secret 不落审计。

### Service

Integration/CI 使用 Service Identity，不冒充 Agent。

## 4. Scope 建议

```text
workspace:read
workspace:manage_members

task:read
task:write
task:claim
task:override

document:read
document:write

message:read
message:send

presence:write

audit:read

agent:manage
integration:use
integration:manage
```

不要一开始定义几十个过细 scope；先覆盖明确安全边界。

## 5. Role 与 Scope

Role 是预定义 scope bundle：

- `viewer`
- `contributor`
- `agent`
- `maintainer`
- `owner`

服务端授权检查以最终 scope/policy 为准；不要把客户端角色字符串当权限判断。

## 6. Human Approval Policy

可配置高风险动作：

```text
workspace.delete
membership.promote_owner
credential.create_privileged
credential.extend_long_ttl
task.force_release
integration.execute_external_write
integration.merge_pull_request
```

Approval 应是服务端状态机：

```text
requested -> approved | rejected | expired -> executed
```

而不是 CLI 弹一个 `Are you sure?` 就视为安全审批。

## 7. Token 策略

推荐：

- access token 短期；
- refresh/long-lived Agent credential 可撤销；
- secret 至少 128-bit 随机强度；
- 服务端保存 verifier/hash 或安全受保护的 credential material；
- rotation 允许短重叠窗口；
- revoke 立即失效关键写操作。

JWT vs opaque token 不在 v0.1 强制：

- opaque token + DB/cache 检查最直观；
- JWT 适合减少中心查询，但 revocation 更复杂。

MVP 推荐 opaque 或 short JWT + server session metadata，先优先安全和可撤销性。

## 8. 本地凭证存储

抽象 `CredentialStore`：

- Windows：Credential Manager / DPAPI；
- macOS：Keychain；
- Linux Desktop：Secret Service；
- Headless Linux/CI：`ASTRAL_TOKEN` 或 secret file/manager。

显式 fallback 文件必须：

- 权限限制到当前用户；
- 不进入 repo；
- CLI 提示风险；
- `doctor` 检查权限。

## 9. TLS 与服务器信任

生产默认 HTTPS。

CLI 应：

- 校验证书；
- 尊重系统 CA；
- 支持企业自定义 CA；
- 不提供全局默认 `--insecure`；
- 若开发环境提供 `--insecure`，必须清晰标记并避免写入永久默认配置。

## 10. Secrets Redaction

统一 sensitive key matcher：

```text
authorization
token
access_token
refresh_token
credential
secret
password
cookie
api_key
private_key
```

日志输出 request/response body 前经过 redactor。调试模式也不例外。

## 11. Workspace 内容安全

默认 exclude：

```text
.env
.env.*
**/secrets/**
**/*.pem
**/*.key
.git/**
.astral/**
```

但不能只依赖文件名规则。文档明确告诉 Agent：不要把 secret 放入消息、Task description 或 Markdown。

## 12. Audit

重要 event：

- login success/failure；
- Credential create/revoke/rotate；
- membership/role/scope change；
- Task claim/release/force override；
- Document write/delete/conflict resolve；
- policy change；
- external integration write；
- human approval result。

Audit payload：

```text
actor_id
action
resource
workspace_id
result
request_id
client_version
source metadata (redacted)
timestamp
```

## 13. 审计完整性

MVP 可以存 PostgreSQL append-only 表，并限制应用角色 UPDATE/DELETE。

更高等级部署以后可选：

- WORM/Object storage export；
- hash chain；
- SIEM export；
- external immutable sink。

不要在 MVP 假装提供密码学不可抵赖性。

## 14. Rate Limit / Abuse

按 Credential + Actor + IP 组合限流：

- auth polling；
- message send；
- credential create；
- login failure；
- expensive document operation。

Presence heartbeat 和 SSE reconnect 使用单独 budget。

## 15. 服务端数据库权限

生产中：

- migration role 与 runtime role 分离；
- runtime 不应有创建超级用户等权限；
- backup 加密；
- secret/config 从 secret manager/environment 注入；
- 不把生产数据库密码写进 image。

## 16. Supply Chain

Release pipeline：

- dependency lock/baseline；
- CI pinned actions；
- SBOM；
- checksum；
- release signature；
- artifact provenance（后续可加入 SLSA）；
- code signing/notarization；
- vulnerability/dependency scan。

## 17. 安全测试

至少覆盖：

- expired/revoked token；
- scope escalation attempt；
- cross-workspace access；
- path traversal；
- symlink escape；
- IDOR；
- duplicate idempotency key；
- stale revision；
- force release without permission；
- secret redaction；
- SSE connection revoked mid-stream；
- rate limit；
- malformed JSON/schema abuse。

## 18. 未来能力

- OIDC federation；
- organization/tenant；
- WebAuthn/passkeys for Human；
- workload identity；
- mTLS for service；
- policy engine (OPA/Cedar-like)；
- signed agent attestations；
- secret-scanning integration。

这些都不应成为 MVP 前置条件。

## 19. 参考资料

- OAuth 2.0 Device Authorization Grant: https://www.rfc-editor.org/rfc/rfc8628.html
