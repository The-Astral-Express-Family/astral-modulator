// Package event 是领域事件模块（architecture §6.9）。
// 事件 envelope 与 type 目录的契约事实来源是 api/schemas/event.json；
// 本文件常量由 TestEventTypesSync 与 web/src/api/sse.ts 三方强制同步。
package event

import "time"

// Envelope 是 SSE/未来 WebSocket 共用的事件包装（docs/protocol.md §5；契约 api/schemas/event.json）。
// 字段为公网契约，禁止重命名；新增字段必须评估旧客户端兼容。
type Envelope struct {
	ID               string `json:"id"`   // evt_...（= SSE id 行）
	Type             string `json:"type"` // 见 Type* 常量
	WorkspaceID      string `json:"workspace_id,omitempty"`
	ActorID          string `json:"actor_id,omitempty"`
	OccurredAt       string `json:"occurred_at"`    // RFC3339
	SchemaVersion    int    `json:"schema_version"` // 当前 1
	ResourceRevision int64  `json:"resource_revision,omitempty"`
	Data             any    `json:"data"`
}

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
	TypeTagCreated                = "tag.created"
	TypeTagRenamed                = "tag.renamed"
	TypeTagDeleted                = "tag.deleted"
	TypeHumanOverride             = "human.override"

	// TypeSnapshotRequired 是控制事件（不属于领域事实）：resume 游标超出
	// outbox 保留窗口、无法安全补发时下发，客户端必须重新拉取快照
	// （docs/protocol.md §5；保留窗口 S1 裁决 = 24h）。
	TypeSnapshotRequired = "snapshot.required"
)

// SSE 传输常量（outbox 保留窗口 RetentionWindow 见 retention.go）。
const (
	// KeepaliveInterval 服务器无事件时的注释心跳间隔，防止中间层断开空闲连接。
	KeepaliveInterval = 15 * time.Second
)
