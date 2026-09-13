// Package outbox 是事务性 outbox 的**写侧**：领域事件类型词表 + EmitTx
// （与业务变更同事务写入 outbox，architecture §19）。
//
// 独立成叶包的原因：auth（事件生产方）与 modules/event（SSE 读侧）互相
// 依赖——event/sse.go 的订阅鉴权需要 auth，auth → event 会成环。生产方
// 一律 import 本包；读侧（hub/SSE/dispatcher/保留窗口）在 modules/event。
// 词表三方同步门：TestEventTypesSync（types ↔ api/schemas/event.json ↔
// web/src/api/sse.ts）。
package outbox

// 事件类型目录（api/schemas/event.json EventType enum）。
// TypeDocumentUpdated/TypeDocumentConflict/TypeHumanOverride 属契约预留：
// document sync（phase-5）与 human override（phase-6）实装前无 emit 方。
const (
	TypeWorkspaceMemberChanged    = "workspace.member.changed"
	TypeActorPresenceChanged      = "actor.presence.changed"
	TypeTaskCreated               = "task.created"
	TypeTaskClaimed               = "task.claimed"
	TypeTaskLeaseExpired          = "task.lease.expired"
	TypeTaskUpdated               = "task.updated"
	TypeTaskReleased              = "task.released"
	TypeDocumentUpdated           = "document.updated"
	TypeDocumentConflict          = "document.conflict"
	TypeMessageCreated            = "message.created"
	TypeSecurityCredentialCreated = "security.credential.created"
	TypeSecurityCredentialRevoked = "security.credential.revoked"
	TypeSecurityInviteCreated     = "security.invite.created"
	TypeSecurityInviteRevoked     = "security.invite.revoked"
	TypeSecurityInviteRedeemed    = "security.invite.redeemed"
	TypeTagCreated                = "tag.created"
	TypeTagRenamed                = "tag.renamed"
	TypeTagDeleted                = "tag.deleted"
	TypeHumanOverride             = "human.override"

	// TypeSnapshotRequired 是控制事件（不属于领域事实）：resume 游标超出
	// outbox 保留窗口、无法安全补发时下发，客户端必须重新拉取快照
	// （docs/protocol.md §5；保留窗口 S1 裁决 = 24h）。
	TypeSnapshotRequired = "snapshot.required"
)
