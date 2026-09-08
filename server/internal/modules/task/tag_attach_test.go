package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

func httpStatus(err error) int {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// tagAttachSetup 在 task fixture 之上补 tag 场景。
func tagAttachSetup(t *testing.T) (*fixture, model.Tag) {
	t.Helper()
	f := setup(t)
	tagRow := model.Tag{
		ID: "tag_t1", WorkspaceID: f.wsID, Name: "Backend",
		NormalizedName: "backend", CreatedBy: f.human.ID, CreatedAt: time.Now(),
	}
	if err := f.db.Create(&tagRow).Error; err != nil {
		t.Fatal(err)
	}
	return f, tagRow
}

func TestAttachTagBumpsRevisionAndSearchFindsIt(t *testing.T) {
	f, tagRow := tagAttachSetup(t)
	ctx := context.Background()
	tk := createTask(t, f, "tsk_tag1", "tagged task")
	pa := principal(t, f, f.credA)

	fresh, tags, err := f.m.AttachTag(ctx, pa, tk.ID, tagRow.ID, &tk.Revision)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if fresh.Revision != tk.Revision+1 {
		t.Fatalf("revision = %d, want %d（关联是任务修改）", fresh.Revision, tk.Revision+1)
	}
	if len(tags) != 1 || tags[0].ID != tagRow.ID {
		t.Fatalf("tags = %+v, want [%s]", tags, tagRow.ID)
	}

	// 幂等：重复挂载不 bump。
	again, _, err := f.m.AttachTag(ctx, pa, tk.ID, tagRow.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Revision != fresh.Revision {
		t.Fatalf("re-attach bumped revision %d -> %d, want idempotent", fresh.Revision, again.Revision)
	}

	// search ?tag= 命中。
	results, _, apiErr := f.m.Search(ctx, f.wsID, SearchParams{Tag: "backend", Limit: 10})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(results) != 1 || results[0].Task.ID != tk.ID {
		t.Fatalf("search by tag = %+v, want [tsk_tag1]", results)
	}

	// detach：revision 再 bump，关联清空，search 不再命中。
	if err := f.m.DetachTag(ctx, pa, tk.ID, tagRow.ID); err != nil {
		t.Fatalf("detach: %v", err)
	}
	results, _, _ = f.m.Search(ctx, f.wsID, SearchParams{Tag: "backend", Limit: 10})
	if len(results) != 0 {
		t.Fatalf("search after detach = %+v, want empty", results)
	}
	var links []model.TaskTag
	if err := f.db.Find(&links, "task_id = ?", tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("task_tags rows = %+v, want empty", links)
	}
}

func TestAttachTagValidatesWorkspaceAndRevision(t *testing.T) {
	f, tagRow := tagAttachSetup(t)
	ctx := context.Background()
	tk := createTask(t, f, "tsk_tag2", "other ws task")
	pa := principal(t, f, f.credA)

	// 其他 workspace 的 tag：404。
	other := model.Tag{ID: "tag_other", WorkspaceID: "ws_elsewhere", Name: "X", NormalizedName: "x"}
	if err := f.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.m.AttachTag(ctx, pa, tk.ID, other.ID, nil); err == nil || httpStatus(err) != 404 {
		t.Fatalf("cross-ws attach err = %v, want 404", err)
	}

	// 预期 revision 不符：409。
	wrong := tk.Revision + 5
	if _, _, err := f.m.AttachTag(ctx, pa, tk.ID, tagRow.ID, &wrong); err == nil || httpStatus(err) != 409 {
		t.Fatalf("stale expected_revision err = %v, want 409", err)
	}
	// 失败的挂载不得留下关联行或 bump revision。
	var n int64
	_ = f.db.Model(&model.TaskTag{}).Where("task_id = ?", tk.ID).Count(&n).Error
	if n != 0 {
		t.Fatalf("failed attaches must not leave rows, got %d", n)
	}
	var current model.Task
	if err := f.db.First(&current, "id = ?", tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Revision != 1 {
		t.Fatalf("failed attach bumped revision to %d, want 1", current.Revision)
	}
}

func TestDetachTagIsIdempotent(t *testing.T) {
	f, tagRow := tagAttachSetup(t)
	ctx := context.Background()
	tk := createTask(t, f, "tsk_tag3", "task")
	pa := principal(t, f, f.credA)

	if err := f.m.DetachTag(ctx, pa, tk.ID, tagRow.ID); err != nil {
		t.Fatalf("detach unlinked: %v", err)
	}
	var t2 model.Task
	if err := f.db.First(&t2, "id = ?", tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if t2.Revision != 1 {
		t.Fatalf("no-op detach bumped revision to %d, want 1", t2.Revision)
	}
}
