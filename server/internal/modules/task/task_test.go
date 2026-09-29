package task

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

type fixture struct {
	m      *Module
	db     *gorm.DB
	hub    *event.Hub
	svc    *auth.Service
	wsID   string
	human  *model.Actor
	agentA *model.Actor
	agentB *model.Actor
	credA  string
	credB  string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(svc.DrainBackgroundWrites) // 先于关库/TempDir 清理 drain 后台写（cleanup LIFO）
	hub := event.NewHub()
	m := &Module{DB: db, Auth: svc}

	human := &model.Actor{ID: "usr_h1", Kind: "human", DisplayName: "H"}
	agentA := &model.Actor{ID: "agt_a1", Kind: "agent", DisplayName: "A"}
	agentB := &model.Actor{ID: "agt_b1", Kind: "agent", DisplayName: "B"}
	for _, a := range []*model.Actor{human, agentA, agentB} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	wsID := "ws_t1"
	if err := db.Create(&model.Workspace{ID: wsID, Name: "demo", Slug: "demo", CreatedBy: human.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.WorkspaceMember{WorkspaceID: wsID, ActorID: human.ID, Role: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	issue := func(actorID string) string {
		issued, err := svc.IssueCredential(context.Background(), auth.CreateCredentialInput{
			ActorID: actorID, Kind: "agent",
			Scopes:    []string{auth.ScopeTaskRead, auth.ScopeTaskWrite, auth.ScopeTaskClaim},
			Workspace: &wsID, CreatedBy: human.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return issued.Secret
	}
	return &fixture{
		m: m, db: db, hub: hub, svc: svc, wsID: wsID,
		human: human, agentA: agentA, agentB: agentB,
		credA: issue(agentA.ID), credB: issue(agentB.ID),
	}
}

func createTask(t *testing.T, f *fixture, id, title string) model.Task {
	t.Helper()
	tk := model.Task{ID: id, WorkspaceID: f.wsID, Title: title, Status: "open", Priority: "normal", Revision: 1, CreatedBy: f.human.ID}
	if err := f.db.Create(&tk).Error; err != nil {
		t.Fatal(err)
	}
	return tk
}

func principal(t *testing.T, f *fixture, secret string) *auth.Principal {
	t.Helper()
	p, apiErr := f.svc.ResolvePrincipal(context.Background(), secret, "")
	if apiErr != nil {
		t.Fatalf("resolve credential: %v", apiErr)
	}
	return p
}

// TestClaimRaceIsAtomic：roadmap spike 验收语义 —— 两个 actor 竞争同一 task，
// 只有一个 claim 成功，另一个收到冲突错误。
func TestClaimRaceIsAtomic(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_race", "race")

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, secret := range []string{f.credA, f.credB} {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			p := principal(t, f, s)
			_, err := f.m.Claim(context.Background(), p, "tsk_race", nil)
			results <- err
		}(secret)
	}
	wg.Wait()
	close(results)

	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if apiErr, ok := err.(*httpx.APIError); ok && apiErr.Code == httpx.CodeTaskAlreadyClaimed {
			conflicts++
			continue
		}
		t.Fatalf("unexpected claim error: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("want exactly 1 success + 1 conflict, got %d/%d", successes, conflicts)
	}
}

// TestSequentialClaimRejected：已认领时他人 claim 失败；claimant 重复 claim 幂等。
func TestSequentialClaimRejected(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_seq", "seq")
	pa := principal(t, f, f.credA)
	pb := principal(t, f, f.credB)

	if _, err := f.m.Claim(context.Background(), pa, "tsk_seq", nil); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	_, err := f.m.Claim(context.Background(), pb, "tsk_seq", nil)
	if err == nil {
		t.Fatal("second claim should fail")
	}
	if apiErr, ok := err.(*httpx.APIError); ok && apiErr.Code != httpx.CodeTaskAlreadyClaimed {
		t.Fatalf("want TASK_ALREADY_CLAIMED, got %v", apiErr.Code)
	}
	// holder 重复 claim：幂等续占。
	if _, err := f.m.Claim(context.Background(), pa, "tsk_seq", nil); err != nil {
		t.Fatalf("holder re-claim: %v", err)
	}
}

// TestReleaseFreesTaskForReclaim：claimant 释放后任务回 open、他人可认领；
// 非 claimant 且无 task:override 的释放被拒（死 agent 的回收路径 = 人工
// task:override，无时间自动过期，TODO.md §9 2026-09-30）。
func TestReleaseFreesTaskForReclaim(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_rel", "release")
	pa := principal(t, f, f.credA)
	pb := principal(t, f, f.credB)

	if _, err := f.m.Claim(context.Background(), pa, "tsk_rel", nil); err != nil {
		t.Fatal(err)
	}
	// B 无 task:override：释放他人认领被拒。
	err := f.m.Release(context.Background(), pb, "tsk_rel")
	apiErr, ok := err.(*httpx.APIError)
	if !ok || apiErr.Status != http.StatusForbidden {
		t.Fatalf("want 403, got %v", err)
	}
	// A 主动释放：assignee 清空、in_progress 回退 open。
	if err := f.m.Release(context.Background(), pa, "tsk_rel"); err != nil {
		t.Fatal(err)
	}
	var t0 model.Task
	if err := f.db.First(&t0, "id = ?", "tsk_rel").Error; err != nil {
		t.Fatal(err)
	}
	if t0.AssigneeActorID != nil || t0.Status != "open" {
		t.Fatalf("assignee=%v status=%s", t0.AssigneeActorID, t0.Status)
	}
	// B 现在可认领。
	fresh, err := f.m.Claim(context.Background(), pb, "tsk_rel", nil)
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if fresh.AssigneeActorID == nil || *fresh.AssigneeActorID != f.agentB.ID || fresh.Status != "in_progress" {
		t.Fatalf("assignee=%v status=%s", fresh.AssigneeActorID, fresh.Status)
	}
}

// TestForceReleaseByOverride：task:override 凭证（human 干预路径）可释放
// 他人的认领——自动过期移除后的唯一回收手段。
func TestForceReleaseByOverride(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_force", "force release")
	pa := principal(t, f, f.credA)

	if _, err := f.m.Claim(context.Background(), pa, "tsk_force", nil); err != nil {
		t.Fatal(err)
	}
	issued, err := f.svc.IssueCredential(context.Background(), auth.CreateCredentialInput{
		ActorID: f.agentB.ID, Kind: "agent",
		Scopes:    []string{auth.ScopeTaskRead, auth.ScopeTaskClaim, auth.ScopeTaskOverride},
		Workspace: &f.wsID, CreatedBy: f.human.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	po := principal(t, f, issued.Secret)
	if err := f.m.Release(context.Background(), po, "tsk_force"); err != nil {
		t.Fatalf("override release: %v", err)
	}
	var t0 model.Task
	if err := f.db.First(&t0, "id = ?", "tsk_force").Error; err != nil {
		t.Fatal(err)
	}
	if t0.AssigneeActorID != nil || t0.Status != "open" {
		t.Fatalf("assignee=%v status=%s", t0.AssigneeActorID, t0.Status)
	}
}

// TestClaimRevisionCheck：expected_revision 不匹配 → REVISION_CONFLICT。
func TestClaimRevisionCheck(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_rev", "rev")
	pa := principal(t, f, f.credA)

	bad := int64(99)
	_, err := f.m.Claim(context.Background(), pa, "tsk_rev", &bad)
	apiErr, ok := err.(*httpx.APIError)
	if !ok || apiErr.Code != httpx.CodeRevisionConflict {
		t.Fatalf("want REVISION_CONFLICT, got %v", err)
	}
}
