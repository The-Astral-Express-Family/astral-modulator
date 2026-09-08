package task

import (
	"context"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
)

func seedSearchTasks(t *testing.T, f *fixture) {
	t.Helper()
	rows := []model.Task{
		{ID: "tsk_s1", WorkspaceID: f.wsID, Title: "Implement LaTeX export", Description: "pdf pipeline", Status: "open", Priority: "normal", Revision: 1, CreatedBy: f.human.ID},
		{ID: "tsk_s2", WorkspaceID: f.wsID, Title: "Fix login flow", Description: "device flow polling", Status: "in_progress", Priority: "high", Revision: 1, CreatedBy: f.human.ID},
		{ID: "tsk_s3", WorkspaceID: f.wsID, Title: "Write docs", Description: "architecture overview", Status: "open", Priority: "low", Revision: 1, CreatedBy: f.human.ID},
	}
	for i := range rows {
		if err := f.db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchRegexFilter(t *testing.T) {
	f := setup(t)
	seedSearchTasks(t, f)

	results, _, apiErr := f.m.Search(context.Background(), f.wsID, SearchParams{Regex: "(?i)latex|login"})
	if apiErr != nil {
		t.Fatalf("search: %v", apiErr)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}

	// 非法 regex → 400 VALIDATION_FAILED。
	if _, _, apiErr := f.m.Search(context.Background(), f.wsID, SearchParams{Regex: "(["}); apiErr == nil || apiErr.Code != "VALIDATION_FAILED" {
		t.Fatalf("invalid regex: %v", apiErr)
	}

	// 无 regex/fuzzy → handler 层拒绝；Search 层允许（结构化过滤直查）。
	results, _, apiErr = f.m.Search(context.Background(), f.wsID, SearchParams{Status: "open"})
	if apiErr != nil {
		t.Fatalf("structured-only search: %v", apiErr)
	}
	if len(results) != 2 {
		t.Fatalf("status filter: want 2, got %d", len(results))
	}
}

func TestSearchFuzzyRanking(t *testing.T) {
	f := setup(t)
	seedSearchTasks(t, f)

	// regex 先筛（docs），fuzzy 后排。
	results, _, apiErr := f.m.Search(context.Background(), f.wsID,
		SearchParams{Fuzzy: "login"})
	if apiErr != nil {
		t.Fatalf("fuzzy search: %v", apiErr)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Task.Title != "Fix login flow" {
		t.Fatalf("top hit = %q, want login task", results[0].Task.Title)
	}
	if results[0].Score == nil || *results[0].Score <= 0 {
		t.Fatalf("score missing on fuzzy search: %+v", results[0].Score)
	}

	// regex + fuzzy 组合：先筛后排。
	results, _, apiErr = f.m.Search(context.Background(), f.wsID,
		SearchParams{Regex: "flow", Fuzzy: "login"})
	if apiErr != nil {
		t.Fatalf("combined search: %v", apiErr)
	}
	if len(results) != 1 || results[0].Task.Title != "Fix login flow" {
		t.Fatalf("combined results: %+v", results)
	}
}

func TestSearchTagFilter(t *testing.T) {
	f := setup(t)
	seedSearchTasks(t, f)

	// 给 tsk_s1 挂 tag "latex"。
	norm := tag.NormalizeName("LaTeX")
	tagRow := model.Tag{ID: "tag_t1", WorkspaceID: f.wsID, Name: "LaTeX", NormalizedName: norm, CreatedBy: f.human.ID}
	if err := f.db.Create(&tagRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec("INSERT INTO task_tags (task_id, tag_id, added_by) VALUES (?, ?, ?)", "tsk_s1", "tag_t1", f.human.ID).Error; err != nil {
		t.Fatal(err)
	}

	results, _, apiErr := f.m.Search(context.Background(), f.wsID, SearchParams{Tag: "latex"})
	if apiErr != nil {
		t.Fatalf("tag search: %v", apiErr)
	}
	if len(results) != 1 || results[0].Task.ID != "tsk_s1" {
		t.Fatalf("tag filter results: %+v", results)
	}
}

func TestSearchPaginationCursor(t *testing.T) {
	f := setup(t)
	seedSearchTasks(t, f)

	page1, next, apiErr := f.m.Search(context.Background(), f.wsID,
		SearchParams{Regex: ".", Limit: 2})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(page1) != 2 || next == "" {
		t.Fatalf("page1: %d items, next=%q", len(page1), next)
	}
	page2, next2, apiErr := f.m.Search(context.Background(), f.wsID,
		SearchParams{Regex: ".", Limit: 2, Cursor: next})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if len(page2) != 1 || next2 != "" {
		t.Fatalf("page2: %d items, next=%q", len(page2), next2)
	}
	if page1[0].Task.ID == page2[0].Task.ID {
		t.Fatal("pages overlap")
	}
}
