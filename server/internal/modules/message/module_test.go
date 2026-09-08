package message

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

type fixture struct {
	m     *Module
	db    *gorm.DB
	svc   *auth.Service
	ws1   string
	ws2   string
	human *model.Actor
	// agentA 绑定 ws1；agentB 绑定 ws2；agentGlobal 未绑定（全局）。
	agentA, agentB, agentGlobal *model.Actor
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m := &Module{DB: db, Auth: svc}

	human := &model.Actor{ID: "usr_h1", Kind: "human", DisplayName: "H"}
	agentA := &model.Actor{ID: "agt_a1", Kind: "agent", DisplayName: "A"}
	agentB := &model.Actor{ID: "agt_b1", Kind: "agent", DisplayName: "B"}
	agentGlobal := &model.Actor{ID: "agt_g1", Kind: "agent", DisplayName: "G"}
	for _, a := range []*model.Actor{human, agentA, agentB, agentGlobal} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	ws1, ws2 := "ws_one", "ws_two"
	for _, ws := range []struct{ id, name string }{{ws1, "one"}, {ws2, "two"}} {
		if err := db.Create(&model.Workspace{ID: ws.id, Name: ws.name, Slug: ws.name, CreatedBy: human.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.WorkspaceMember{WorkspaceID: ws.id, ActorID: human.ID, Role: "owner"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	bind := func(actorID, ws string) {
		w := ws
		if _, err := svc.IssueCredential(context.Background(), auth.CreateCredentialInput{
			ActorID: actorID, Kind: "agent",
			Scopes:    []string{auth.ScopeMessageRead, auth.ScopeMessageSend},
			Workspace: &w, CreatedBy: human.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	bind(agentA.ID, ws1)
	bind(agentB.ID, ws2)
	if _, err := svc.IssueCredential(context.Background(), auth.CreateCredentialInput{
		ActorID: agentGlobal.ID, Kind: "agent",
		Scopes:    []string{auth.ScopeMessageRead, auth.ScopeMessageSend},
		CreatedBy: human.ID,
	}); err != nil {
		t.Fatal(err)
	}
	return &fixture{
		m: m, db: db, svc: svc, ws1: ws1, ws2: ws2, human: human,
		agentA: agentA, agentB: agentB, agentGlobal: agentGlobal,
	}
}

func seedMessage(t *testing.T, f *fixture, wsID, senderID, targetType, targetID, body string) {
	t.Helper()
	row := model.Message{
		ID: "msg_" + body, WorkspaceID: wsID,
		TargetType: targetType, TargetID: targetID,
		SenderID: senderID, Body: body,
	}
	if err := f.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

// listBodies 以 human 身份调用 list handler，返回本 workspace 可见的消息 body 列表。
func listBodies(t *testing.T, f *fixture, wsID string) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/workspaces/"+wsID+"/messages", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace_id", wsID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithPrincipal(ctx, &auth.Principal{ActorID: f.human.ID, Kind: "human"})

	rec := httptest.NewRecorder()
	f.m.list(rec, req.WithContext(ctx))
	if rec.Code != 200 {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []messageDTO `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		bodies = append(bodies, it.Body)
	}
	return bodies
}

func TestListVisibilityConfinesPrivateMessagesToWorkspace(t *testing.T) {
	f := setup(t)
	// 构造历史脏数据场景：同一收件人在两个 workspace 各有一条私信。
	seedMessage(t, f, f.ws1, f.human.ID, "actor", f.agentA.ID, "dm-ws1")
	seedMessage(t, f, f.ws2, f.human.ID, "actor", f.agentA.ID, "dm-ws2")

	bodies := listBodies(t, f, f.ws1)
	if len(bodies) != 1 || bodies[0] != "dm-ws1" {
		t.Fatalf("ws1 可见消息 = %v，应为 [dm-ws1]（不得泄露 ws2 私信）", bodies)
	}
}

func TestReachabilityRules(t *testing.T) {
	f := setup(t)
	if reachable, err := f.m.reachableInWorkspace(context.Background(), f.ws1, f.agentA.ID); err != nil || !reachable {
		t.Fatalf("agentA 应在 ws1 可达（err=%v reachable=%v）", err, reachable)
	}
	if reachable, _ := f.m.reachableInWorkspace(context.Background(), f.ws1, f.agentB.ID); reachable {
		t.Fatal("agentB 只绑定 ws2，不得在 ws1 可达")
	}
	if reachable, _ := f.m.reachableInWorkspace(context.Background(), f.ws1, f.agentGlobal.ID); !reachable {
		t.Fatal("未绑定 credential 的全局 agent 应在任意 workspace 可达")
	}
	// 广播消息不受可达性约束（target 强制为本 ws，list 对成员可见）。
	seedMessage(t, f, f.ws1, f.human.ID, "workspace", f.ws1, "broadcast-1")
	if bodies := listBodies(t, f, f.ws1); len(bodies) != 1 {
		t.Fatalf("broadcast 可见 = %v，应为 [broadcast-1]", bodies)
	}
}
