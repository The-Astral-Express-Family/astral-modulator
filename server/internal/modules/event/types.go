// Package event 是领域事件的**读侧**（SSE）：hub、补发端点、outbox
// dispatcher 与保留窗口（architecture §6.9/§19）。
// 事件 envelope 契约事实来源是 api/schemas/event.json；事件类型词表与
// 事务内写侧在 internal/outbox（拆包原因见该包文档），
// 由 TestEventTypesSync 与 web/src/api/sse.ts 三方强制同步。
package event

import "time"

// Envelope 是 SSE/未来 WebSocket 共用的事件包装（docs/protocol.md §5；契约 api/schemas/event.json）。
// 字段为公网契约，禁止重命名；新增字段必须评估旧客户端兼容。
type Envelope struct {
	ID               string `json:"id"`   // evt_...（= SSE id 行）
	Type             string `json:"type"` // 词表见 internal/outbox/types.go
	WorkspaceID      string `json:"workspace_id,omitempty"`
	ActorID          string `json:"actor_id,omitempty"`
	OccurredAt       string `json:"occurred_at"`    // RFC3339
	SchemaVersion    int    `json:"schema_version"` // 当前 1
	ResourceRevision int64  `json:"resource_revision,omitempty"`
	Data             any    `json:"data"`
}

// SSE 传输常量（outbox 保留窗口 RetentionWindow 见 retention.go）。
const (
	// KeepaliveInterval 服务器无事件时的注释心跳间隔，防止中间层断开空闲连接。
	KeepaliveInterval = 15 * time.Second
)
