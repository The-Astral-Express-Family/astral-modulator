package audit

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// GormRecorder 把审计写入 audit_log（00007）。业务事务内调用时传 tx，
// 保证审计与业务变更同事务（architecture §19 同一模式）。
type GormRecorder struct {
	DB *gorm.DB
}

// sensitiveKeys 来自 docs/security.md 敏感字段 matcher。
// details 落库前把命中键的值替换为 "[REDACTED]"。
var sensitiveKeys = map[string]bool{
	"authorization": true, "token": true, "access_token": true, "refresh_token": true,
	"credential": true, "secret": true, "password": true, "cookie": true,
	"api_key": true, "private_key": true, "device_code": true,
}

func redact(m map[string]any) map[string]any {
	for k, v := range m {
		if sensitiveKeys[k] {
			m[k] = "[REDACTED]"
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			redact(sub)
		}
	}
	return m
}

func (r *GormRecorder) Record(ctx context.Context, e Entry) error {
	details, err := json.Marshal(redact(e.Details))
	if err != nil {
		details = []byte(`{}`)
	}
	row := model.AuditEntry{
		ID:      ids.New(ids.Audit),
		Action:  e.Action,
		Outcome: e.Outcome,
		Details: details,
	}
	if e.WorkspaceID != "" {
		ws := e.WorkspaceID
		row.WorkspaceID = &ws
	}
	if e.ActorID != "" {
		a := e.ActorID
		row.ActorID = &a
	}
	if e.TargetType != "" {
		t := e.TargetType
		row.TargetType = &t
	}
	if e.TargetID != "" {
		t := e.TargetID
		row.TargetID = &t
	}
	if e.RequestID != "" {
		req := e.RequestID
		row.RequestID = &req
	}
	return r.DB.WithContext(ctx).Create(&row).Error
}

// RecordInTx 在调用方事务内写审计（与业务变更同事务原子提交）。
func RecordInTx(tx *gorm.DB, e Entry) error {
	return (&GormRecorder{DB: tx}).Record(tx.Statement.Context, e)
}
