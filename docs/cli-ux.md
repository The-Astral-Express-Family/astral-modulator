# `astral` CLI UX 与 Agent 契约初稿

> 状态：Draft
>
> 暂定可执行文件名：`astral`。最终名称由 ADR 决定。

## 1. 目标

CLI 同时面向两类调用者：

1. 人类终端用户；
2. LLM Agent / 自动化脚本。

两者需要共享同一功能面，但输出契约不同：人类需要简洁可读，Agent 需要稳定、无歧义、机器可解析。

## 2. 总体原则

- 默认 human-readable；
- 所有机器调用核心命令支持 `--json`；
- JSON 模式 stdout 只输出 JSON；
- diagnostics/progress/logs 一律 stderr；
- 非 TTY 不进行隐式交互；
- 破坏性操作必须显式参数或确认；
- `--yes` 只能跳过已经定义清楚的确认，不能绕过服务端权限；
- 错误码稳定；
- 命令可安全重试时使用 idempotency key；
- 每个网络命令支持 timeout；
- 能由环境变量/配置注入 workspace/server/token，但优先级必须明确。

## 3. 顶层命令草案

```text
astral auth ...
astral workspace ...
astral todo ...
astral status ...
astral msg ...
astral agent ...
astral document ...
astral event ...
astral skill ...
astral config ...
astral completion ...
astral doctor
astral version
```

`todo` 是用户友好的命令名，服务端领域模型使用 `Task`。如后续发现混淆，也可改为 `task`；不要同时长期维护两套完全不同语义。

## 4. Auth

```text
astral auth login
astral auth login --server https://astral.example.com
astral auth status
astral auth logout
```

`auth login`：

- Human TTY：打印 verification URI + user code，并尝试打开浏览器；
- headless/Agent：不得假装完成人类交互；使用预配置 Agent Credential；
- `auth status --json` 不输出 token secret。

## 5. Workspace

```text
astral workspace list
astral workspace create <name>
astral workspace use <workspace>
astral workspace status
astral workspace init [path]
astral workspace pull
astral workspace push
astral workspace sync
```

`workspace use` 将默认 workspace 写入本地 `.astral/config.json` 或用户级 config，但不写 secrets。

## 6. TODO / Task

```text
astral todo list
astral todo list --status open --label backend
astral todo add "Implement device auth"
astral todo show <id>
astral todo claim <id>
astral todo start <id>
astral todo block <id> --reason "Waiting for API schema"
astral todo review <id>
astral todo done <id> --summary "..."
astral todo release <id>
```

### Agent 推荐模式

```bash
astral todo list --status open --json
astral todo claim tsk_123 --json
astral status set working --task tsk_123 --note "Implementing auth" --json
```

Claim 失败不能自动挑别的 Task，除非调用者显式要求策略型命令；基础 CLI 必须保持可预测。

## 7. Presence

```text
astral status show
astral status set idle
astral status set planning --note "Reading architecture"
astral status set working --task <id> --note "..."
astral status set blocked --task <id> --note "..."
astral status watch
```

后台 heartbeat 有两种实现路线：

### 方案 A：每次 Agent command 都刷新 heartbeat

简单但长任务可能误判 offline。

### 方案 B：`astral session`/daemon 保持 heartbeat

更可靠但部署复杂。

MVP 建议 Agent Runtime 每隔固定周期执行轻量 heartbeat 命令或保持 `event watch` 长连接时同步 heartbeat。后续再决定是否提供本地 daemon。

## 8. Message

```text
astral msg send @agent-name "Please review tsk_123"
astral msg send --workspace "Build is blocked on API decision"
astral msg send --task tsk_123 "Implementation ready for review"
astral msg inbox
astral msg tail
```

机器模式禁止通过纯 display name 唯一定位 Actor，除非服务端明确返回唯一匹配。推荐 `--actor-id` 或由 CLI 先 resolve。

## 9. Document

```text
astral document list
astral document get docs/design.md
astral document put docs/design.md
astral document diff docs/design.md
astral document conflicts
astral document resolve <conflict-id>
```

普通用户主要使用 `workspace sync`；`document ...` 提供调试和精确控制。

## 10. Event

```text
astral event watch
astral event watch --type task.updated,message.created
astral event watch --json
```

JSON stream 推荐每行一个完整 JSON object（JSON Lines），而不是输出一个永不闭合的大数组。

示例：

```jsonl
{"id":"evt_1","type":"task.updated","data":{}}
{"id":"evt_2","type":"message.created","data":{}}
```

## 11. Agent 管理

```text
astral agent list
astral agent create <name>
astral agent credential create <agent-id> --scope task:read --scope task:write
astral agent credential revoke <credential-id>
```

创建 Credential 的 secret 只显示一次。TTY 模式应明确提醒用户保存位置；JSON 模式返回明确字段但 stderr 不重复 secret。

## 12. Skill

CLI 可以逐步提供：

```text
astral skill list
astral skill doctor
astral skill path
```

是否由 CLI 直接“安装 ChatGPT Skill”取决于未来 Skill 分发机制，不建议 v0.1 过度绑定某个平台私有安装 API。

## 13. Config

优先级由高到低：

1. 显式 CLI flag；
2. 环境变量；
3. workspace `.astral/config.json`；
4. user config；
5. compiled defaults。

环境变量建议：

```text
ASTRAL_SERVER
ASTRAL_WORKSPACE
ASTRAL_TOKEN
ASTRAL_OUTPUT
ASTRAL_TIMEOUT
```

`ASTRAL_TOKEN` 仅用于 headless/CI 场景。Desktop 优先 OS Credential Store。

## 14. 输出格式

### Human

```text
✓ Claimed task tsk_123  Implement device auth
  Lease expires in 5m
```

### JSON

```json
{
  "ok": true,
  "task": {
    "id": "tsk_123",
    "status": "claimed",
    "revision": 5
  },
  "lease": {
    "expires_at": "2026-09-06T15:00:00Z"
  }
}
```

JSON 中不要输出 ANSI color、spinner 文本或本地化字段名。

## 15. 退出码

建议保持较少、稳定的顶层退出码；细节由 JSON `error.code` 表达。

```text
0   success
1   generic failure
2   invalid CLI usage
3   authentication/authorization failure
4   not found
5   conflict / precondition failure
6   network / server unavailable
7   timeout
8   local workspace/sync error
9   unsupported client/server version
```

不要为每个服务端业务错误创造新的进程退出码。

## 16. 确认与破坏性操作

TTY：

```text
astral agent credential revoke cred_123
Revoke credential cred_123? [y/N]
```

非 TTY：若缺少 `--yes`，直接失败并返回明确 usage error，而不是等待 stdin。

以下操作不允许仅靠 `--yes` 绕过 server policy：

- workspace delete；
- privilege elevation；
- human-approval integration action。

## 17. Retry

CLI 只自动重试：

- 明确幂等 GET；
- 带 idempotency key 的可重试写操作；
- SSE reconnect；
- server 返回 retryable 的暂态错误。

不要自动重试：

- credential create（除非 idempotency key）；
- 非幂等 integration action；
- revision conflict；
- insufficient scope。

## 18. Proxy/TLS

libcurl 路线应尊重常见系统代理环境变量，并支持企业 CA/自托管 CA 配置。

CLI 必须提供安全的 debug 信息，但默认不得输出 Authorization header、token、cookie 或 credential body。

## 19. `astral doctor`

检查：

- CLI build/version；
- config 路径；
- server URL；
- DNS/TLS；
- server capability；
- auth status；
- current workspace；
- workspace directory permissions；
- credential store availability；
- proxy summary（脱敏）；
- clock skew 提示。

`doctor --json` 适合自动排障。

## 20. Shell Completion

```text
astral completion bash
astral completion zsh
astral completion fish
astral completion powershell
```

输出 completion script 到 stdout，不直接修改 shell profile。

## 21. CLI 稳定性规则

在同一 major protocol/CLI contract 内：

- 不重命名 JSON 字段；
- 新字段只能 additive；
- 不把成功变成需要交互；
- 不改变默认 workspace resolution 顺序；
- 不改变已有 error code 语义；
- deprecated 命令至少保留一个明确迁移周期。
