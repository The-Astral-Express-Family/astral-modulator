# Markdown 工作区同步语义

> 状态：Accepted（MVP 口径，round 37 裁决落档：无服务端自动合并 / tombstone
> 删除；「不建历史版本表」已于 round 41 修订——落地 document_versions
> 历史链（00017），见 §8；修订点在 §8/§9/§13/§14 就地标注）
>
> 目标：确保多个 Agent/人类并发修改文本时不会静默丢数据。
> REST 契约（端点与响应形状）以 `api/openapi.yaml` documents 段为准。

## 1. 范围

MVP 只同步明确纳入 Astral 管理的 UTF-8 Markdown/text 文件。

不承诺：

- 大型二进制资源；
- Git object database；
- 实时多人光标；
- CRDT；
- 任意目录透明镜像。

## 2. Source of Truth

- 结构化 Task 的事实来源是服务端 Task 数据模型；
- Document 内容的服务端版本是远端事实来源；
- 本地文件是一个可离线编辑副本；
- Git 可以存在，但 Git commit 不自动等于 Astral Document revision。

## 3. 受管路径

Workspace config 定义 include/exclude：

```json
{
  "documents": {
    "include": ["docs/**/*.md", "notes/**/*.md", "AGENTS.md", "TODO.md"],
    "exclude": ["**/.env*", "**/secrets/**", ".git/**", ".astral/**"]
  }
}
```

服务端必须再次校验路径；不能信任客户端 glob。

> 归属（round 38 R3 裁决，TODO.md §3）：Workspace config 的 include/exclude 是
> **客户端约定**（astral-cli 仓持有），服务端不存储、不解析该配置；服务端侧
> 的路径防线 = 固定保留前缀黑名单（`.git/`、`.astral/`、`secrets/`、`.env*`
> 等，实现见 server `document/paths.go`）+ push 时的大小写冲突检测（R1）。

## 4. 路径安全

拒绝：

- 绝对路径；
- `..` traversal；
- NUL；
- platform device path；
- 解析后越出 workspace root 的 symlink；
- 大小写规范化导致的重复 path。

需要定义跨平台 canonical path：建议协议层统一 `/`，并在 Windows/macOS 大小写问题上使用 workspace policy 检测冲突。

## 5. 本地状态

本地文件布局（`.astral/` 下的 state/conflict artifact 形状）**归属
astral-cli 仓库**（CLI UX 与本地文件的本仓约定，见其 docs/ARCHITECTURE.md）；
本文只锁 wire 语义（revision/hash/conflict artifact 的协议形状）。phase-5
动工时以 CLI 仓库的定义为准，避免两仓各说一套。

原则不变：本地状态文件不保存 token。

## 6. 三方模型

对每个 Document 比较：

- `B`：last synced base；
- `L`：当前 local；
- `R`：当前 remote。

### 情况 1：L == B && R == B

无变化。

### 情况 2：L != B && R == B

仅本地变化：push。

### 情况 3：L == B && R != B

仅远端变化：pull。

### 情况 4：L != B && R != B

双边变化：尝试三方 merge；若无法证明安全，创建 conflict。

## 7. Hash

协议指定内容 hash，例如：

```text
sha256:<hex>
```

Hash 基于 canonical bytes。MVP 必须明确 newline 处理：建议 **不自动重写文件内容**，直接对实际 UTF-8 bytes hash；平台换行由项目格式化策略解决。

## 8. Revision

每次服务端成功写 Document：

- revision 单调增加；
- 生成 `document.updated` event；
- 保存 writer；
- 保留至少足够用于三方 merge/审计的历史版本或 base 内容。

是否保存全部历史可配置；MVP 至少保存有限 revision window。

> **round 37 裁决**：MVP 不建 revision 历史表——冲突行（document_conflicts）
> 已保存双方全文，audit 记录每次 push 的 hash；历史窗口需求出现再扩展。
>
> **round 41 修订**：窗口需求已出现——CLI 缺省 push 是「GET 当前行 → 以最新
> revision 为 base」的读改写，盲推会静默覆盖他人内容且不产生冲突行（被覆盖
> 内容无处留存）。落地 document_versions（00017）：内容被取代（push 快进 /
> resolve 落地 / 复活 / tombstone）前同事务归档全量快照；保留窗口 =
> TTL（默认 30 天）+ 每文档上限（默认 50 版），先到即剪。读端点
> `/document-versions`（列表/详情，path 必填 query）；恢复不设写端点——取回
> 旧内容后走 push + pinned base，恢复操作本身也过 CAS。历史从迁移上线起算。
> **round 41 补记（CLI 侧）**：astral-cli 已配防盲推门——缺省 push 覆盖非空
> 且内容不同的远端时本地拦截（指向 --force / pinned base / 先 get），显式
> base、同内容、空远端不拦；`document history` 与 `get --revision` 消费
> /document-versions。预防（门）与恢复（历史链）两侧齐备。

## 9. Push

请求：

```json
{
  "path": "docs/design.md",
  "base_revision": 12,
  "base_hash": "sha256:base",
  "content_hash": "sha256:new",
  "content": "..."
}
```

服务端：

1. 校验权限与路径；
2. 校验 hash；
3. 查询当前 revision；
4. 若当前 revision == base revision，则写入；
5. 否则返回 `REVISION_CONFLICT`，提供当前 metadata；
6. 客户端进入 merge/conflict 流程。

> **round 37 裁决**：第 5 步固定为 `409 DOCUMENT_CONFLICT` 并落 conflict
> artifact（返回体带 conflict_id / current_revision / current_hash）；
> 服务端不尝试自动合并（见 §13）。对已 tombstone 的路径 push 且
> base_revision=0 视为复活（revision 续增）。

## 10. Pull

CLI 获取 remote manifest，只有 hash/revision 不同的文件才下载正文。

大量文件时 manifest 支持 cursor/ETag，避免每次下载完整内容。

## 11. Sync

建议流程：

```text
scan local
  -> fetch remote manifest
  -> classify each path
  -> perform safe pulls
  -> perform safe pushes
  -> merge/conflict dual changes
  -> update local state atomically
  -> print summary
```

如果其中一个文件冲突，不必回滚所有独立文件的成功同步；但 CLI 必须给出明确的 partial-success 结果。

## 12. Conflict

冲突记录至少包含：

```text
conflict_id
workspace_id
path
base_revision/base_hash
local_hash
remote_revision/remote_hash
created_by/current_actor
created_at
status
```

### 本地 conflict artifact

本地布局（目录/文件名）由 astral-cli 定义；协议层只要求 conflict artifact
携带 base/local/metadata 三类信息（形状见 openapi 的 ConflictResolve /
DocumentConflict）。

原始工作文件是否替换为 conflict marker 需要谨慎。推荐默认 **不破坏 local 当前版本**，而是生成 conflict artifact + 明确状态。

## 13. 三方 merge

MVP 可使用行级 diff3 思路：

- 不相交变更自动合并；
- 同一区域冲突则失败；
- 合并成功后仍以新的 `base_revision` push；
- 合并过程中 remote 再变化，需要重新检查 revision。

不要把“merge 工具返回成功”当作语义正确；高风险文档可配置禁止自动 merge。

> **round 37 裁决**：diff3 **不在服务端做**——服务端一律「落冲突工件 +
> 409」，把合并决策留给客户端/人；客户端本地合并的结果经
> `resolve(resolution=merged|manual, content=…)` 提交。选型项就此关闭。

## 14. 删除

删除也必须版本化，使用 tombstone 或明确 delete operation。

> **round 37 裁决（契约已定稿）**：`DELETE /workspaces/{id}/documents/{path}
> ?base_revision=N` 落 tombstone（行保留、deleted=true、revision+1、
> `document.updated` 事件 data.deleted=true）；manifest 默认排除 tombstone，
> `include_deleted=true` 供同步端侦测远端删除；重复删除（base 匹配已删行）
> 幂等 204；delete-vs-edit 与 edit-vs-delete 双边变化一律落冲突工件。

情况：

- local delete + remote unchanged：可删除远端；
- remote delete + local unchanged：删除/移入本地回收位置；
- local edit + remote delete：冲突；
- local delete + remote edit：冲突。

MVP 不应无提示永久删除用户本地修改。

## 15. Rename

MVP 可以把 rename 视为 delete + create，简单可靠。

后续如果需要保留文档身份/历史，可引入 stable document ID 和 rename detection。

## 16. File Size

配置最大文本文件大小，超限返回明确错误。大文件/二进制后续迁移到对象存储，不要让 PostgreSQL/SSE 被误用作文件分发系统。

## 17. 与 Git 的关系

Astral sync 与 Git 是正交系统：

- Astral revision 解决协作中枢里的并发；
- Git commit 解决源码版本控制；
- Agent 可以在完成 Task 时附 commit/branch/PR metadata；
- 不自动执行 `git add/commit/push`，除非未来明确的 Integration/Policy 允许。

## 18. TODO.md 特殊规则

如果仓库同时存在结构化 Task 与 `TODO.md`：

推荐 MVP 把 `TODO.md` 当普通文档，不自动双向映射 checkbox。

后续可增加 projection：

```markdown
- [ ] [tsk_123] Implement device auth
```

但必须定义：

- ID 缺失如何处理；
- 删除 checkbox 是否删除 Task；
- status 如何映射；
- 并发编辑如何处理；
- projection 是否可手改。

在这些语义没有固化前，禁止把 Markdown checkbox 当结构化 Task 的隐式写接口。

## 19. 验收测试

至少覆盖：

- local-only change；
- remote-only change；
- dual non-overlap merge；
- dual overlap conflict；
- edit vs delete；
- delete vs edit；
- remote changes during merge；
- path traversal；
- symlink escape；
- case collision；
- interrupted sync 后 state 文件一致；
- network retry 不产生重复 revision。
