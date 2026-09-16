// list.go 是审计查询的共享核心（round 38 T5）：过滤组合 + id 游标分页，
// 供本模块 workspace 端点（S4-1）与 admin 模块平台端点（S4-2）复用——
// 两个端点的分页/过滤惯例必须单一来源。
package audit

import (
	"context"
	"net/http"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// ListFilter 是审计查询端点的统一参数；零值字段 = 不过滤。
// WorkspaceID 留空（仅平台端点允许）表示不按工作区收口——返回全部行，
// 含 workspace_id IS NULL 的服务器级记录（认证失败等，round 37 契约）。
type ListFilter struct {
	WorkspaceID string
	ActorID     string
	Action      string
	Outcome     string
	Cursor      string
	Limit       int
}

// ListEntries 按 id 降序返回审计行。id 是 aud_ 前缀 uuidv7，字典序 = 创建序
// （与 message / document conflicts 列表同惯例），故 id DESC 即 created_at
// 降序且同毫秒行也有稳定全序；游标 = 上一页最末（最旧）一行 id。
// limit+1 探测还有下一页；next 为空串表示末页。列表查询（多行）不走
// store.First（单行出口约定只约束主键/唯一查询）。
func ListEntries(ctx context.Context, db *gorm.DB, f ListFilter) ([]model.AuditEntry, string, error) {
	query := db.WithContext(ctx).Model(&model.AuditEntry{})
	if f.WorkspaceID != "" {
		query = query.Where("workspace_id = ?", f.WorkspaceID)
	}
	if f.ActorID != "" {
		query = query.Where("actor_id = ?", f.ActorID)
	}
	if f.Action != "" {
		query = query.Where("action = ?", f.Action)
	}
	if f.Outcome != "" {
		query = query.Where("outcome = ?", f.Outcome)
	}
	if f.Cursor != "" {
		query = query.Where("id < ?", f.Cursor)
	}
	var rows []model.AuditEntry
	if err := query.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error; err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > f.Limit {
		rows = rows[:f.Limit]
		next = rows[len(rows)-1].ID
	}
	return rows, next, nil
}

// ParseOutcome 校验 outcome 查询参数（契约 enum：allowed|denied|error；
// 空 = 不过滤），非法值 400 VALIDATION_FAILED（details.field=outcome，
// 与 document 列表 status 参数同口径）。
func ParseOutcome(raw string) (string, *httpx.APIError) {
	switch raw {
	case "", "allowed", "denied", "error":
		return raw, nil
	}
	return "", &httpx.APIError{
		Status:  http.StatusBadRequest,
		Code:    httpx.CodeValidationFailed,
		Message: "outcome must be allowed|denied|error",
		Details: map[string]any{"field": "outcome"},
	}
}
