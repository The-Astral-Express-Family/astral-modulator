// tag_attach.go：task↔tag 关联（TODO.md D11）。
// 关联是任务修改：条件 revision bump + task.updated 事件，走既有乐观并发
// 与事件流，不新造事件类型。PUT 幂等（已挂则原样返回 200，不 bump）；
// DELETE 幂等（未挂返回 204）。
package task

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/store"
)

var errAlreadyLinked = errors.New("task tag already linked")

// loadTags 是 handler 侧便捷入口。
func (m *Module) loadTags(r *http.Request, taskID string) []tag.TagDTO {
	return m.loadTaskTags(r.Context(), taskID)
}

// loadTaskTags 取任务的 tag 列表（service 层与 handler 共用）。单任务场景是
// 批量版 tagsForTasks 的退化调用——tag 行组装与排序只有 tagsForTasks 一处实现。
func (m *Module) loadTaskTags(ctx context.Context, taskID string) []tag.TagDTO {
	return m.tagsForTasks(ctx, []string{taskID})[taskID]
}

// AttachTag 关联核心（HTTP handler 与测试共用，语义同 Claim/Confirm）：
// 权限（task:write）→ tag 同 workspace → 幂等/revision 预检 → 单事务
// 条件 bump + 关联行 + audit + 事件。
func (m *Module) AttachTag(ctx context.Context, p *auth.Principal, taskID, tagID string, expectedRevision *int64) (*model.Task, []tag.TagDTO, error) {
	t, apiErr := m.LoadForWorkspace(ctx, p, taskID, auth.ScopeTaskWrite)
	if apiErr != nil {
		return nil, nil, apiErr
	}
	var tagRow model.Tag
	err := m.DB.WithContext(ctx).First(&tagRow, "id = ? AND workspace_id = ?", tagID, t.WorkspaceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, httpx.NotFound("tag not found in this workspace")
	}
	if err != nil {
		return nil, nil, err
	}
	if expectedRevision != nil && *expectedRevision != t.Revision {
		return nil, nil, revisionConflict(t.Revision)
	}

	// PUT 幂等：已挂直接返回当前状态（不 bump、不发事件）。
	var linked int64
	if err := m.DB.WithContext(ctx).Model(&model.TaskTag{}).
		Where("task_id = ? AND tag_id = ?", taskID, tagID).Count(&linked).Error; err != nil {
		return nil, nil, err
	}
	if linked > 0 {
		return t, m.loadTaskTags(ctx, taskID), nil
	}

	var fresh model.Task
	err = m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpRevisionTx(tx, taskID, t.Revision, map[string]any{"updated_by": p.ActorID}); err != nil {
			return err
		}
		if err := tx.Create(&model.TaskTag{TaskID: taskID, TagID: tagID, AddedBy: p.ActorID}).Error; err != nil {
			// 并发重复挂载：整体回滚（含 revision bump），等价目标状态已达成。
			if store.IsUniqueViolation(err) {
				return errAlreadyLinked
			}
			return err
		}
		if err := tx.First(&fresh, "id = ?", taskID).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: t.WorkspaceID, ActorID: p.ActorID,
			Action: "task.tag.attach", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
			Details: map[string]any{"tag_id": tagID},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskUpdated, t.WorkspaceID, p.ActorID, fresh.Revision,
			map[string]any{"task_id": taskID, "tag_change": "attach", "tag_id": tagID})
	})
	if errors.Is(err, errAlreadyLinked) {
		if e := m.DB.WithContext(ctx).First(&fresh, "id = ?", taskID).Error; e != nil {
			return nil, nil, e
		}
		return &fresh, m.loadTaskTags(ctx, taskID), nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &fresh, m.loadTaskTags(ctx, taskID), nil
}

// DetachTag 摘除关联。未挂载（或 tag 已删连带清理）→ no-op，不动 revision。
func (m *Module) DetachTag(ctx context.Context, p *auth.Principal, taskID, tagID string) error {
	t, apiErr := m.LoadForWorkspace(ctx, p, taskID, auth.ScopeTaskWrite)
	if apiErr != nil {
		return apiErr
	}

	var linked int64
	if err := m.DB.WithContext(ctx).Model(&model.TaskTag{}).
		Where("task_id = ? AND tag_id = ?", taskID, tagID).Count(&linked).Error; err != nil {
		return err
	}
	if linked == 0 {
		return nil
	}

	return m.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpRevisionTx(tx, taskID, t.Revision, map[string]any{"updated_by": p.ActorID}); err != nil {
			return err
		}
		if err := tx.Where("task_id = ? AND tag_id = ?", taskID, tagID).Delete(&model.TaskTag{}).Error; err != nil {
			return err
		}
		if err := audit.RecordInTx(tx, audit.Entry{
			WorkspaceID: t.WorkspaceID, ActorID: p.ActorID,
			Action: "task.tag.detach", Outcome: "allowed",
			TargetType: "task", TargetID: taskID,
			Details: map[string]any{"tag_id": tagID},
		}); err != nil {
			return err
		}
		return event.EmitTx(tx, event.TypeTaskUpdated, t.WorkspaceID, p.ActorID, t.Revision+1,
			map[string]any{"task_id": taskID, "tag_change": "detach", "tag_id": tagID})
	})
}

// ---- HTTP handlers ----

func (m *Module) attachTag(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	p := auth.PrincipalFrom(r.Context())
	fresh, tags, err := m.AttachTag(r.Context(), p,
		chi.URLParam(r, "task_id"), chi.URLParam(r, "tag_id"), in.ExpectedRevision)
	if err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, http.StatusOK, toTaskDTO(*fresh, nil, tags, m.childCount(r.Context(), fresh.ID)))
}

func (m *Module) detachTag(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if err := m.DetachTag(r.Context(), p, chi.URLParam(r, "task_id"), chi.URLParam(r, "tag_id")); err != nil {
		httpx.RespondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
