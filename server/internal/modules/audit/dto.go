// dto.go 是审计行的 wire 层（openapi components/schemas/AuditEntry，
// snake_case；DTO 与 model 分离，硬约束 §1-10）。workspace 查询端点
// （本模块）与平台查询端点（admin 模块）共用同一形状。
package audit

import (
	"encoding/json"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// EntryDTO 是审计行的公网形状。可空 ID 字段（workspace_id/actor_id/
// target_id）nil 即省略（契约 IdOrNull）；request_id 可空同理；
// details 恒为 JSON object（落库的 null 归一为 {}，契约不允许 null）。
type EntryDTO struct {
	ID          string          `json:"id"`
	WorkspaceID *string         `json:"workspace_id,omitempty"`
	ActorID     *string         `json:"actor_id,omitempty"`
	Action      string          `json:"action"`
	Outcome     string          `json:"outcome"`
	TargetType  string          `json:"target_type,omitempty"`
	TargetID    *string         `json:"target_id,omitempty"`
	Details     json.RawMessage `json:"details"`
	RequestID   *string         `json:"request_id,omitempty"`
	CreatedAt   string          `json:"created_at"`
}

// ToEntryDTO 转换模型行；时间统一 UTC RFC3339（与各模块 DTO 惯例一致）。
func ToEntryDTO(row model.AuditEntry) EntryDTO {
	details := json.RawMessage(row.Details)
	if len(details) == 0 || string(details) == "null" {
		details = json.RawMessage("{}")
	}
	targetType := ""
	if row.TargetType != nil {
		targetType = *row.TargetType
	}
	return EntryDTO{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		ActorID:     row.ActorID,
		Action:      row.Action,
		Outcome:     row.Outcome,
		TargetType:  targetType,
		TargetID:    row.TargetID,
		Details:     details,
		RequestID:   row.RequestID,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
	}
}
