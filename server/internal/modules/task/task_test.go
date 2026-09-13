package task

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
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
			_, _, err := f.m.Claim(context.Background(), p, "tsk_race", nil, 300)
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

// TestSequentialClaimRejected：已持有租约时他人 claim 失败；holder 重复 claim 幂等。
func TestSequentialClaimRejected(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_seq", "seq")
	pa := principal(t, f, f.credA)
	pb := principal(t, f, f.credB)

	if _, _, err := f.m.Claim(context.Background(), pa, "tsk_seq", nil, 300); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	_, _, err := f.m.Claim(context.Background(), pb, "tsk_seq", nil, 300)
	if err == nil {
		t.Fatal("second claim should fail")
	}
	if apiErr, ok := err.(*httpx.APIError); ok && apiErr.Code != httpx.CodeTaskAlreadyClaimed {
		t.Fatalf("want TASK_ALREADY_CLAIMED, got %v", apiErr.Code)
	}
	// holder 重复 claim：幂等续占。
	if _, _, err := f.m.Claim(context.Background(), pa, "tsk_seq", nil, 300); err != nil {
		t.Fatalf("holder re-claim: %v", err)
	}
}

// TestLeaseExpiryAllowsReclaim：租约过期后他人可接管。
func TestLeaseExpiryAllowsReclaim(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_exp", "expiry")
	pa := principal(t, f, f.credA)
	pb := principal(t, f, f.credB)

	if _, _, err := f.m.Claim(context.Background(), pa, "tsk_exp", nil, 300); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.TaskLease{}).Where("task_id = ?", "tsk_exp").
		Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	fresh, lease, err := f.m.Claim(context.Background(), pb, "tsk_exp", nil, 300)
	if err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
	if lease.HolderActorID != f.agentB.ID || fresh.Status != "in_progress" {
		t.Fatalf("holder=%s status=%s", lease.HolderActorID, fresh.Status)
	}
}

// TestClaimRevisionCheck：expected_revision 不匹配 → REVISION_CONFLICT。
func TestClaimRevisionCheck(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_rev", "rev")
	pa := principal(t, f, f.credA)

	bad := int64(99)
	_, _, err := f.m.Claim(context.Background(), pa, "tsk_rev", &bad, 300)
	apiErr, ok := err.(*httpx.APIError)
	if !ok || apiErr.Code != httpx.CodeRevisionConflict {
		t.Fatalf("want REVISION_CONFLICT, got %v", err)
	}
}

// TestSweeperExpiresLeases：清扫器回收过期租约并发 task.lease.expired。
func TestSweeperExpiresLeases(t *testing.T) {
	f := setup(t)
	createTask(t, f, "tsk_sweep", "sweep")
	pa := principal(t, f, f.credA)

	events, unsub := f.hub.Subscribe(event.Subscription{})
	defer unsub()
	if _, _, err := f.m.Claim(context.Background(), pa, "tsk_sweep", nil, 300); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.TaskLease{}).Where("task_id = ?", "tsk_sweep").
		Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	f.m.sweepOnce(context.Background())
	// 事件走 outbox（同事务写入），手动投递一次到 hub。
	event.PollOnce(context.Background(), f.db, f.hub, slog.New(slog.NewTextHandler(io.Discard, nil)))

	deadline := time.After(2 * time.Second)
	for {
		select {
		case env := <-events:
			if env.Type == outbox.TypeTaskLeaseExpired {
				goto done // 跳过先前 claim 事件，找到目标事件
			}
		case <-deadline:
			t.Fatal("no lease.expired event emitted")
		}
	}
done:
	var count int64
	f.db.Model(&model.TaskLease{}).Where("task_id = ?", "tsk_sweep").Count(&count)
	if count != 0 {
		t.Fatal("expired lease not swept")
	}
}
