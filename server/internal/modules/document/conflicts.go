package document

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// actionDocumentResolve 是 resolve 的审计动作名。
const actionDocumentResolve = "document.resolve"

// listConflicts：status=open|resolved|all（open = resolved_at IS NULL），
// 共享 Limit/Cursor，created_at 降序。游标用 id（cfl_ 前缀 uuidv7，字典序 =
// 创建序，与 message 列表同惯例），排序 id DESC。
func (m *Module) listConflicts(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	if _, apiErr := requireAnyDocScope(r, m.Auth, wsID, true); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "resolved" && status != "all" {
		httpx.WriteError(w, r, invalidField("status", "status must be open|resolved|all"))
		return
	}
	limit := httpx.ParseLimit(q.Get("limit"), 50, 200)
	query := m.DB.WithContext(r.Context()).Model(&model.DocumentConflict{}).
		Where("workspace_id = ?", wsID)
	switch status {
	case "open":
		query = query.Where("resolved_at IS NULL")
	case "resolved":
		query = query.Where("resolved_at IS NOT NULL")
	}
	if cursor := q.Get("cursor"); cursor != "" {
		query = query.Where("id < ?", cursor)
	}
	var rows []model.DocumentConflict
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].ID
	}
	items := make([]conflictDTO, 0, len(rows))
	for _, row := range rows {
		ours, theirs := decodeSides(row)
		items = append(items, toConflictDTO(row, ours, theirs))
	}
	httpx.WriteOK(w, r, http.StatusOK, httpx.NewPage(items, next))
}

// getConflict：详情 = 列表形状 + 双方完整内容。内容级授权走冲突行 path 的
// 前缀轴（memory 冲突要 memory:read）——与 get 文档同口径。
func (m *Module) getConflict(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	conflictID := chi.URLParam(r, "conflict_id")
	var row model.DocumentConflict
	if apiErr := store.First(m.DB.WithContext(r.Context()), &row,
		httpx.NotFound("conflict not found"), "id = ? AND workspace_id = ?", conflictID, wsID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	if _, apiErr := requireDocScope(r, m.Auth, wsID, row.Path, true); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	ours, theirs := decodeSides(row)
	httpx.WriteOK(w, r, http.StatusOK, conflictDetailDTO{
		conflictDTO:   toConflictDTO(row, ours, theirs),
		OursContent:   ours.Content,
		TheirsContent: theirs.Content,
	})
}

// resolveConflict：resolution=ours|theirs|merged|manual（merged/manual 需 content）。
// 非 open → 409 VALIDATION_FAILED(details.reason=already_resolved)。写路径的
// 授权按冲突行 path 前缀轴（写内容 = 写该路径文档）。
func (m *Module) resolveConflict(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	conflictID := chi.URLParam(r, "conflict_id")
	var in struct {
		Resolution *string `json:"resolution"`
		Content    *string `json:"content"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.Resolution == nil {
		httpx.WriteError(w, r, invalidField("resolution", "resolution is required"))
		return
	}
	switch *in.Resolution {
	case "ours", "theirs", "merged", "manual":
	default:
		httpx.WriteError(w, r, invalidField("resolution", "resolution must be ours|theirs|merged|manual"))
		return
	}
	if (*in.Resolution == "merged" || *in.Resolution == "manual") && in.Content == nil {
		httpx.WriteError(w, r, invalidField("content", "content is required for resolution=merged|manual"))
		return
	}
	if in.Content != nil && len(*in.Content) > maxContentBytes {
		httpx.WriteError(w, r, invalidField("content", "content exceeds 1MiB limit"))
		return
	}
	// 先加载冲突行供路径前缀授权（404 优先于 403，与详情端点同序）；
	// open 状态在事务核心里以最新行重判，杜绝并发双 resolve。
	var row model.DocumentConflict
	if apiErr := store.First(m.DB.WithContext(r.Context()), &row,
		httpx.NotFound("conflict not found"), "id = ? AND workspace_id = ?", conflictID, wsID); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	p, apiErr := requireDocScope(r, m.Auth, wsID, row.Path, false)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	doc, apiErr := m.resolveCore(r.Context(), p, wsID, conflictID, *in.Resolution, in.Content)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toDocumentDTO(*doc))
}

// resolveCore 是 resolve 的事务核心。语义（TODO §2 S1-5）：
//   - ours：以 ours_json 落新 revision——push 工件写内容；delete 意图工件
//     （delete-vs-edit 的 ours 侧）落 tombstone，行已删则无操作；
//   - theirs：仅关闭冲突，返回当前 Document——不 bump revision、不发事件；
//   - merged|manual：以请求 content（服务端重算 hash）落新 revision；
//   - 全部分支同事务写 resolution/resolved_by/resolved_at + audit
//     document.resolve(details.resolution)；document.updated 事件 theirs 除外。
//
// 裁决 switch 下沉 applyResolutionTx，关闭工件 + audit 下沉 closeConflictTx。
func (m *Module) resolveCore(ctx context.Context, p *auth.Principal, wsID, conflictID, resolution string, content *string) (*model.Document, *httpx.APIError) {
	var out *model.Document
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 以事务内最新行为准（handler 读到的快照可能已被并发 resolve）。
		row, err := loadConflictTx(tx, conflictID, wsID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return httpx.NotFound("conflict not found")
			}
			return err
		}
		if row.ResolvedAt != nil {
			return &httpx.APIError{
				Status:  http.StatusConflict,
				Code:    httpx.CodeValidationFailed,
				Message: "conflict is already resolved",
				Details: map[string]any{"reason": "already_resolved"},
			}
		}
		// 写路径对 ours 侧严格判错：ours_json 一旦损坏即 500（数据损坏语义），
		// 不得以零值空侧把空内容写进 document 行（读端点仍宽容展示空侧）。
		ours, err := decodeSide(row.OursJSON)
		if err != nil {
			return httpx.Internal("conflict ours_json is corrupted")
		}
		doc, exists, err := loadDocumentTx(tx, wsID, row.Path)
		if err != nil {
			return err
		}
		if !exists {
			// 工件只能由既有行产生（行不硬删），缺失即数据损坏，如实 500。
			return httpx.Internal("document row missing for conflict")
		}

		fresh, emit, err := applyResolutionTx(tx, p, doc, ours, resolution, content)
		if err != nil {
			return err
		}
		if err := closeConflictTx(tx, p, wsID, row, doc, resolution); err != nil {
			return err
		}
		if emit {
			if err := emitUpdatedTx(tx, wsID, p.ActorID, fresh, fresh.DeletedAt != nil); err != nil {
				return err
			}
		}
		out = fresh
		return nil
	})
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, httpx.Internal("resolve failed")
	}
	return out, nil
}

// applyResolutionTx 是 resolve 的裁决 switch：按 resolution 把文档行落到裁决
// 结果，返回最新行与是否发 document.updated（theirs 不动行不发事件）。
func applyResolutionTx(tx *gorm.DB, p *auth.Principal, doc *model.Document, ours conflictSide, resolution string, content *string) (*model.Document, bool, error) {
	var fresh *model.Document
	var err error
	emit := false
	switch resolution {
	case "ours":
		if ours.Delete {
			fresh, err = applyTombstoneTx(tx, doc, p.ActorID)
			if err != nil {
				return nil, false, err
			}
			// 行原本已是 tombstone 时为无操作（不 bump 不发事件）。
			if doc.DeletedAt == nil {
				emit = true
			}
		} else {
			fresh, err = applyContentTx(tx, doc, ours.Content, ours.ContentHash, p.ActorID)
			if err != nil {
				return nil, false, err
			}
			emit = true
		}
	case "theirs":
		fresh = doc // 远端版本即裁决：不动行、不发事件
	default: // merged | manual
		hash := contentHash(*content)
		fresh, err = applyContentTx(tx, doc, *content, hash, p.ActorID)
		if err != nil {
			return nil, false, err
		}
		emit = true
	}
	return fresh, emit, nil
}

// closeConflictTx 关闭冲突工件（条件更新防并发双 resolve）+ audit
// document.resolve。已被并发 resolve（RowsAffected=0）时返回 409 already_resolved。
func closeConflictTx(tx *gorm.DB, p *auth.Principal, wsID string, row *model.DocumentConflict, doc *model.Document, resolution string) error {
	now := time.Now()
	res := tx.Model(&model.DocumentConflict{}).
		Where("id = ? AND resolved_at IS NULL", row.ID).
		Updates(map[string]any{
			"resolution": resolution, "resolved_by": p.ActorID, "resolved_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &httpx.APIError{
			Status:  http.StatusConflict,
			Code:    httpx.CodeValidationFailed,
			Message: "conflict is already resolved",
			Details: map[string]any{"reason": "already_resolved"},
		}
	}
	return audit.RecordInTx(tx, audit.Entry{
		WorkspaceID: wsID, ActorID: p.ActorID,
		Action: actionDocumentResolve, Outcome: "allowed",
		TargetType: "document", TargetID: doc.ID,
		Details: map[string]any{"path": row.Path, "resolution": resolution},
	})
}
