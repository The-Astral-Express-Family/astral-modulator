package document

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

// maxContentBytes 是单篇文档内容上限（1MiB，UTF-8 字节数；sync-semantics §16）。
// 全局 body 兜底 8MiB 保留（httpx），本上限在 handler 校验（TODO.md §4 映射）。
const maxContentBytes = 1 << 20

// 审计动作名（audit_log.action 稳定值）。
const (
	actionDocumentPush   = "document.push"
	actionDocumentDelete = "document.delete"
)

type pushInput struct {
	BaseRevision int64
	BaseHash     string
	Content      string
	ContentHash  string // 可选；空 = 请求未携带
}

// conflictOutcome 承载「事务内已落冲突工件、事务提交后转 409」的业务结果：
// 409 是正常业务分支而非回滚条件，工件/audit/事件必须随事务一起提交
// （在事务回调里返回错误会让 gorm ROLLBACK，工件就消失了）。
type conflictOutcome struct {
	row    *model.DocumentConflict
	theirs conflictSide
}

func (c *conflictOutcome) apiError() *httpx.APIError {
	return &httpx.APIError{
		Status:  http.StatusConflict,
		Code:    httpx.CodeDocumentConflict,
		Message: "document changed remotely; conflict artifact recorded",
		Details: map[string]any{
			"conflict_id":      c.row.ID,
			"current_revision": c.theirs.Revision,
			"current_hash":     c.theirs.ContentHash,
		},
	}
}

// push 是三方同步的写入口（PUT，支持 Idempotency-Key——中间件已在路由级挂载，
// 同键重放 2xx）。服务端对原始 UTF-8 bytes 重算 sha256 并以重算值为准。
func (m *Module) push(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	path, ok := decodePath(w, r)
	if !ok {
		return
	}
	p, apiErr := requireDocScope(r, m.Auth, wsID, path, false)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	var in struct {
		BaseRevision *int64  `json:"base_revision"`
		BaseHash     *string `json:"base_hash"`
		Content      *string `json:"content"`
		ContentHash  *string `json:"content_hash"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.BaseRevision == nil || *in.BaseRevision < 0 {
		httpx.WriteError(w, r, invalidField("base_revision", "base_revision must be a non-negative integer"))
		return
	}
	if in.BaseHash == nil || !hashPattern.MatchString(*in.BaseHash) {
		httpx.WriteError(w, r, invalidField("base_hash", "base_hash must match sha256:<64 lowercase hex>"))
		return
	}
	if in.Content == nil {
		httpx.WriteError(w, r, invalidField("content", "content is required"))
		return
	}
	if in.ContentHash != nil && !hashPattern.MatchString(*in.ContentHash) {
		httpx.WriteError(w, r, invalidField("content_hash", "content_hash must match sha256:<64 lowercase hex>"))
		return
	}
	if len(*in.Content) > maxContentBytes {
		httpx.WriteError(w, r, invalidField("content", "content exceeds 1MiB limit"))
		return
	}
	hash := contentHash(*in.Content)
	if in.ContentHash != nil && *in.ContentHash != hash {
		httpx.WriteError(w, r, &httpx.APIError{
			Status:  http.StatusBadRequest,
			Code:    httpx.CodeValidationFailed,
			Message: "content_hash does not match server-side recomputation",
			Details: map[string]any{"field": "content_hash", "reason": "content_hash_mismatch"},
		})
		return
	}

	row, apiErr := m.pushDoc(r.Context(), p, wsID, path, pushInput{
		BaseRevision: *in.BaseRevision,
		BaseHash:     *in.BaseHash,
		Content:      *in.Content,
		ContentHash:  derefOrEmpty(in.ContentHash),
	})
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toDocumentDTO(*row))
}

// pushDoc 是 push 的事务核心：单事务内完成 R1 大小写检查 → 行分类 →
// 创建/快进/复活/落冲突 → audit + outbox 三件套。冲突分支经 conflictOutcome
// 在事务提交后转 409（工件必须落地）；400/500 分支无写入，回滚无害。
func (m *Module) pushDoc(ctx context.Context, p *auth.Principal, wsID, path string, in pushInput) (*model.Document, *httpx.APIError) {
	hash := contentHash(in.Content)
	ours := conflictSide{Content: in.Content, ContentHash: hash, ActorID: p.ActorID}
	var out *model.Document
	var pending *conflictOutcome
	recordConflict := func(tx *gorm.DB, doc *model.Document) error {
		c, err := recordConflictTx(tx, p, doc, actionDocumentPush, ours, in.BaseRevision, in.BaseHash)
		if err != nil {
			return err
		}
		pending = &conflictOutcome{row: c, theirs: sideFromDocument(*doc)}
		return nil // 事务照常提交（409 是业务结果）
	}
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// R1：push 一律先查同 workspace 仅大小写不同的已存路径（含目标行本身
		// 之外的命中即拒；与自身完全同 path 的重推不受影响）。
		collided, err := hasCaseCollisionTx(tx, wsID, path)
		if err != nil {
			return err
		}
		if collided {
			return &httpx.APIError{
				Status:  http.StatusBadRequest,
				Code:    httpx.CodeValidationFailed,
				Message: "path differs only by case from an existing document",
				Details: map[string]any{"field": "path", "reason": "path_case_collision"},
			}
		}
		row, exists, err := loadDocumentTx(tx, wsID, path)
		if err != nil {
			return err
		}
		switch {
		case !exists && in.BaseRevision != 0:
			// 行从未存在（tombstone 也是行）而调用方带着 base 指针 → 本地状态错乱。
			return invalidField("base_revision", "base_revision refers to a document that does not exist")
		case !exists:
			// 创建 revision=1（local-only 创建路径，sync-semantics §6 情况 2）。
			now := time.Now()
			created := &model.Document{
				ID: ids.New(ids.Document), WorkspaceID: wsID, Path: path,
				Revision: 1, ContentHash: hash, Content: in.Content,
				UpdatedBy: p.ActorID, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(created).Error; err != nil {
				if store.IsUniqueViolation(err) {
					// 并发同路径创建 = 双改：重读行，按失配落冲突工件。
					fresh, ok, e := loadDocumentTx(tx, wsID, path)
					if e != nil || !ok {
						return err
					}
					return recordConflict(tx, fresh)
				}
				return err
			}
			if err := auditPushTx(tx, wsID, p, created, map[string]any{"created": true}); err != nil {
				return err
			}
			out = created
			return emitUpdatedTx(tx, wsID, p.ActorID, created, false)
		case row.DeletedAt != nil && in.BaseRevision == 0:
			// 复活（P2）：tombstone + base_revision=0 → 写入新内容、revision 续增。
			fresh, err := applyContentTx(tx, row, in.Content, hash, p.ActorID)
			if err != nil {
				return err
			}
			if err := auditPushTx(tx, wsID, p, fresh, map[string]any{"revived": true}); err != nil {
				return err
			}
			out = fresh
			return emitUpdatedTx(tx, wsID, p.ActorID, fresh, false)
		case row.DeletedAt != nil:
			// tombstone 且 base 不匹配：同冲突路径，theirs 标记远端已删除。
			return recordConflict(tx, row)
		case row.Revision == in.BaseRevision:
			if row.ContentHash != in.BaseHash {
				// revision 匹配但 base_hash 与该行当前 hash 不一致：调用方本地状态
				// 错乱（TODO §2 S1-3 明确 400，不落工件）。
				return &httpx.APIError{
					Status:  http.StatusBadRequest,
					Code:    httpx.CodeValidationFailed,
					Message: "base_hash does not match the hash at base_revision",
					Details: map[string]any{
						"field": "base_hash", "reason": "base_hash_mismatch",
						"current_hash": row.ContentHash,
					},
				}
			}
			// 快进：条件更新（id+revision+hash+未删）防并发覆盖；失配即并发推进，
			// 重读后按失配落冲突工件（sync-semantics §13「合并过程中 remote 再变化」）。
			now := time.Now()
			res := tx.Model(&model.Document{}).
				Where("id = ? AND revision = ? AND content_hash = ? AND deleted_at IS NULL",
					row.ID, row.Revision, row.ContentHash).
				Updates(map[string]any{
					"content": in.Content, "content_hash": hash,
					"revision": row.Revision + 1, "updated_by": p.ActorID, "updated_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				fresh, ok, e := loadDocumentTx(tx, wsID, path)
				if e != nil || !ok {
					return e
				}
				return recordConflict(tx, fresh)
			}
			row.Content, row.ContentHash, row.UpdatedBy, row.UpdatedAt = in.Content, hash, p.ActorID, now
			row.Revision++
			if err := auditPushTx(tx, wsID, p, row, nil); err != nil {
				return err
			}
			out = row
			return emitUpdatedTx(tx, wsID, p.ActorID, row, false)
		default:
			// revision 不匹配：双改（dual overlap）→ 落工件 + 409（P1）。
			return recordConflict(tx, row)
		}
	})
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, httpx.Internal("push failed")
	}
	if pending != nil {
		return nil, pending.apiError()
	}
	return out, nil
}

// delete 是版本化删除（DELETE ?base_revision=，禁止盲删）：
//   - 未删且 revision 匹配 → tombstone 落地（revision+1）+ document.updated
//     (data.deleted=true) + 204；
//   - 已删且 base 匹配当前 revision → 幂等 204（无事件无审计——无状态变更）；
//   - 失配 → delete-vs-edit 冲突工件（ours 标记 delete 意图）+ 409。
func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	wsID := chiWorkspaceID(r)
	path, ok := decodePath(w, r)
	if !ok {
		return
	}
	p, apiErr := requireDocScope(r, m.Auth, wsID, path, false)
	if apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	raw := r.URL.Query().Get("base_revision")
	if raw == "" {
		httpx.WriteError(w, r, invalidField("base_revision", "base_revision query parameter is required"))
		return
	}
	base, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || base < 1 {
		httpx.WriteError(w, r, invalidField("base_revision", "base_revision must be a positive integer"))
		return
	}
	if apiErr := m.deleteDoc(r.Context(), p, wsID, path, base); apiErr != nil {
		httpx.WriteError(w, r, apiErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) deleteDoc(ctx context.Context, p *auth.Principal, wsID, path string, base int64) *httpx.APIError {
	var pending *conflictOutcome
	err := m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, exists, err := loadDocumentTx(tx, wsID, path)
		if err != nil {
			return err
		}
		if !exists {
			return httpx.NotFound("document not found")
		}
		if row.DeletedAt == nil && row.Revision == base {
			// 条件更新防并发：id + revision + 未删。失配 → 重读分流
			//（并发已删且 base 匹配 → 幂等；否则落工件）。
			now := time.Now()
			res := tx.Model(&model.Document{}).
				Where("id = ? AND revision = ? AND deleted_at IS NULL", row.ID, row.Revision).
				Updates(map[string]any{
					"deleted_at": now, "revision": row.Revision + 1,
					"updated_by": p.ActorID, "updated_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				fresh, ok, e := loadDocumentTx(tx, wsID, path)
				if e != nil || !ok {
					return e
				}
				if fresh.DeletedAt != nil && fresh.Revision == base {
					return nil // 并发重复删 → 幂等
				}
				return recordDeleteConflict(tx, p, fresh, base, &pending)
			}
			row.DeletedAt, row.UpdatedBy, row.UpdatedAt = &now, p.ActorID, now
			row.Revision++
			if err := audit.RecordInTx(tx, audit.Entry{
				WorkspaceID: wsID, ActorID: p.ActorID,
				Action: actionDocumentDelete, Outcome: "allowed",
				TargetType: "document", TargetID: row.ID,
				Details: map[string]any{"path": path, "revision": row.Revision, "deleted": true},
			}); err != nil {
				return err
			}
			return emitUpdatedTx(tx, wsID, p.ActorID, row, true)
		}
		if row.DeletedAt != nil && row.Revision == base {
			return nil // 幂等 204：调用方所见即当前已删状态
		}
		// 失配（delete-vs-edit，含对已被并发变更的 tombstone）→ 工件 + 409。
		return recordDeleteConflict(tx, p, row, base, &pending)
	})
	if err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return httpx.Internal("delete failed")
	}
	if pending != nil {
		return pending.apiError()
	}
	return nil
}

// recordDeleteConflict 是 delete 失配分支的工件落地小件（ours = delete 意图；
// base_hash 置空——DELETE 契约只带 base_revision，调用方 base 指针无 hash 成分）。
func recordDeleteConflict(tx *gorm.DB, p *auth.Principal, doc *model.Document, base int64, pending **conflictOutcome) error {
	ours := conflictSide{Delete: true, BaseRevision: base, ActorID: p.ActorID}
	c, err := recordConflictTx(tx, p, doc, actionDocumentDelete, ours, base, "")
	if err != nil {
		return err
	}
	*pending = &conflictOutcome{row: c, theirs: sideFromDocument(*doc)}
	return nil
}

// ---- 事务内共享小件 ----

// recordConflictTx 落一条冲突工件并同事务完成 audit + document.conflict 事件。
// 只写不判——409 响应由调用方在事务提交后经 conflictOutcome 构造。
func recordConflictTx(tx *gorm.DB, p *auth.Principal, doc *model.Document, action string, ours conflictSide, baseRevision int64, baseHash string) (*model.DocumentConflict, error) {
	theirs := sideFromDocument(*doc)
	oursRaw, err := json.Marshal(ours)
	if err != nil {
		return nil, err
	}
	theirsRaw, err := json.Marshal(theirs)
	if err != nil {
		return nil, err
	}
	row := &model.DocumentConflict{
		ID: ids.New(ids.Conflict), WorkspaceID: doc.WorkspaceID, Path: doc.Path,
		BaseRevision: baseRevision, BaseHash: baseHash,
		OursJSON: oursRaw, TheirsJSON: theirsRaw,
		CreatedAt: time.Now(),
	}
	if err := tx.Create(row).Error; err != nil {
		return nil, err
	}
	if err := audit.RecordInTx(tx, audit.Entry{
		WorkspaceID: doc.WorkspaceID, ActorID: p.ActorID,
		Action: action, Outcome: "allowed",
		TargetType: "document", TargetID: doc.ID,
		Details: map[string]any{"path": doc.Path, "conflict_id": row.ID},
	}); err != nil {
		return nil, err
	}
	if err := outbox.EmitTx(tx, outbox.TypeDocumentConflict, doc.WorkspaceID, p.ActorID, theirs.Revision, map[string]any{
		"conflict_id": row.ID, "path": doc.Path,
		"current_revision": theirs.Revision, "current_hash": theirs.ContentHash,
	}); err != nil {
		return nil, err
	}
	return row, nil
}

// auditPushTx 是 push 各成功分支的审计小件（details 合并 path/revision）。
func auditPushTx(tx *gorm.DB, wsID string, p *auth.Principal, doc *model.Document, extra map[string]any) error {
	details := map[string]any{"path": doc.Path, "revision": doc.Revision}
	for k, v := range extra {
		details[k] = v
	}
	return audit.RecordInTx(tx, audit.Entry{
		WorkspaceID: wsID, ActorID: p.ActorID,
		Action: actionDocumentPush, Outcome: "allowed",
		TargetType: "document", TargetID: doc.ID,
		Details: details,
	})
}

// emitUpdatedTx 发 document.updated（resolve 的内容分支复用）。
// data payload 字段是 CLI 消费契约（api/schemas/event.json data 为开放对象，
// 字段名以本处为事实来源）：path / revision / content_hash / deleted。
func emitUpdatedTx(tx *gorm.DB, wsID, actorID string, doc *model.Document, deleted bool) error {
	return outbox.EmitTx(tx, outbox.TypeDocumentUpdated, wsID, actorID, doc.Revision, map[string]any{
		"path": doc.Path, "revision": doc.Revision,
		"content_hash": doc.ContentHash, "deleted": deleted,
	})
}

// applyContentTx 落一份新内容：revision+1；行是 tombstone 时顺带复活
// （deleted_at=NULL）。用于 push 复活与 resolve 的内容落地。重读行后写入，
// 以 resolve 的仲裁语义落到最新 revision 之上（resolve 稀有且为显式人工决策，
// 与 push 快进的严格条件更新互补）。
func applyContentTx(tx *gorm.DB, doc *model.Document, content, hash, actorID string) (*model.Document, error) {
	fresh := *doc
	if err := tx.First(&fresh, "id = ?", doc.ID).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	updates := map[string]any{
		"content": content, "content_hash": hash,
		"revision": fresh.Revision + 1, "updated_by": actorID, "updated_at": now,
	}
	if fresh.DeletedAt != nil {
		updates["deleted_at"] = nil
	}
	if err := tx.Model(&model.Document{}).Where("id = ?", fresh.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	fresh.Content, fresh.ContentHash, fresh.UpdatedBy, fresh.UpdatedAt = content, hash, actorID, now
	fresh.Revision++
	fresh.DeletedAt = nil
	return &fresh, nil
}

// applyTombstoneTx 落 tombstone（resolve 的 ours=delete 意图分支）；
// 行已是 tombstone 时幂等无操作。
func applyTombstoneTx(tx *gorm.DB, doc *model.Document, actorID string) (*model.Document, error) {
	fresh := *doc
	if err := tx.First(&fresh, "id = ?", doc.ID).Error; err != nil {
		return nil, err
	}
	if fresh.DeletedAt != nil {
		return &fresh, nil
	}
	now := time.Now()
	if err := tx.Model(&model.Document{}).Where("id = ?", fresh.ID).Updates(map[string]any{
		"deleted_at": now, "revision": fresh.Revision + 1,
		"updated_by": actorID, "updated_at": now,
	}).Error; err != nil {
		return nil, err
	}
	fresh.DeletedAt, fresh.UpdatedBy, fresh.UpdatedAt = &now, actorID, now
	fresh.Revision++
	return &fresh, nil
}

// invalidField 构造 400 VALIDATION_FAILED（details.field 定位参数）。
func invalidField(field, message string) *httpx.APIError {
	return &httpx.APIError{
		Status:  http.StatusBadRequest,
		Code:    httpx.CodeValidationFailed,
		Message: message,
		Details: map[string]any{"field": field},
	}
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
