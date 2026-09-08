package workspace

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

type approvalFixture struct {
	m      *Module
	db     *gorm.DB
	hub    *event.Hub
	svc    *auth.Service
	wsID   string
	owner  *auth.Principal
	maint  *auth.Principal
	agent  *model.Actor
	agent2 *model.Actor
}

func approvalSetup(t *testing.T) *approvalFixture {
	t.Helper()
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hub := event.NewHub()
	m := &Module{DB: db, Audit: &audit.GormRecorder{DB: db}, Auth: svc}

	ownerActor := &model.Actor{ID: "usr_owner", Kind: "human", DisplayName: "Owner"}
	maintActor := &model.Actor{ID: "usr_maint", Kind: "human", DisplayName: "Maint"}
	agent := &model.Actor{ID: "agt_t1", Kind: "agent", DisplayName: "T1"}
	agent2 := &model.Actor{ID: "agt_t2", Kind: "agent", DisplayName: "T2"}
	for _, a := range []*model.Actor{ownerActor, maintActor, agent, agent2} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	wsID := "ws_apv"
	if err := db.Create(&model.Workspace{ID: wsID, Name: "demo", Slug: "demo", CreatedBy: ownerActor.ID}).Error; err != nil {
		t.Fatal(err)
	}
	members := []model.WorkspaceMember{
		{WorkspaceID: wsID, ActorID: ownerActor.ID, Role: "owner"},
		{WorkspaceID: wsID, ActorID: maintActor.ID, Role: "maintainer"},
		{WorkspaceID: wsID, ActorID: agent.ID, Role: "agent"},
		{WorkspaceID: wsID, ActorID: agent2.ID, Role: "agent"},
	}
	for _, mem := range members {
		if err := db.Create(&mem).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &approvalFixture{
		m: m, db: db, hub: hub, svc: svc, wsID: wsID,
		owner: &auth.Principal{ActorID: ownerActor.ID, Kind: "human"},
		maint: &auth.Principal{ActorID: maintActor.ID, Kind: "human"},
		agent: agent, agent2: agent2,
	}
}

func postApproval(t *testing.T, f *approvalFixture, p *auth.Principal, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	if id := pathID(path, "/approvals/"); id != "" {
		rctx.URLParams.Add("approval_id", id)
	}
	if strings.Contains(path, "/workspaces/") {
		rctx.URLParams.Add("workspace_id", f.wsID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = auth.WithPrincipal(ctx, p)
	// 依据路径前缀选择 handler（chi 在生产路由中完成等价分发）。
	switch {
	case strings.HasSuffix(path, "/approvals"):
		f.m.createApproval(rec, req.WithContext(ctx))
	case strings.HasSuffix(path, "/approve"):
		f.m.decideApproval("approved")(rec, req.WithContext(ctx))
	case strings.HasSuffix(path, "/deny"):
		f.m.decideApproval("rejected")(rec, req.WithContext(ctx))
	}
	return rec
}

func pathID(path, prefix string) string {
	i := strings.Index(path, prefix)
	if i < 0 {
		return ""
	}
	rest := path[i+len(prefix):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func createPromotion(t *testing.T, f *approvalFixture, targetID string) model.Approval {
	t.Helper()
	rec := postApproval(t, f, f.owner, "/api/v1/workspaces/"+f.wsID+"/approvals",
		`{"action":"membership.promote_owner","target_actor_id":"`+targetID+`"}`)
	if rec.Code != 201 {
		t.Fatalf("create approval status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var dto approvalDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	var row model.Approval
	if err := f.db.First(&row, "id = ?", dto.ID).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestApprovePromotesOwnerAtomically(t *testing.T) {
	f := approvalSetup(t)
	apv := createPromotion(t, f, f.agent.ID)

	rec := postApproval(t, f, f.owner, "/api/v1/approvals/"+apv.ID+"/approve", "")
	if rec.Code != 200 {
		t.Fatalf("approve status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var member model.WorkspaceMember
	if err := f.db.First(&member, "workspace_id = ? AND actor_id = ?", f.wsID, f.agent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.Role != "owner" {
		t.Fatalf("role = %q, want owner (approve 必须同事务执行)", member.Role)
	}
	var final model.Approval
	if err := f.db.First(&final, "id = ?", apv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if final.Status != "executed" {
		t.Fatalf("status = %q, want executed", final.Status)
	}
}

func TestMaintainerCannotDecide(t *testing.T) {
	f := approvalSetup(t)
	apv := createPromotion(t, f, f.agent.ID)

	rec := postApproval(t, f, f.maint, "/api/v1/approvals/"+apv.ID+"/deny", "")
	if rec.Code != 403 {
		t.Fatalf("maintainer deny status = %d, want 403", rec.Code)
	}
}

func TestDecisionIsSingleUse(t *testing.T) {
	f := approvalSetup(t)
	apv := createPromotion(t, f, f.agent.ID)

	if rec := postApproval(t, f, f.owner, "/api/v1/approvals/"+apv.ID+"/deny", ""); rec.Code != 200 {
		t.Fatalf("first deny status = %d", rec.Code)
	}
	// 第二次裁决（无论 approve/deny）必须 409，不得重复执行。
	rec := postApproval(t, f, f.owner, "/api/v1/approvals/"+apv.ID+"/approve", "")
	if rec.Code != 409 {
		t.Fatalf("second decision status = %d, want 409", rec.Code)
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Code != httpx.CodeApprovalExpired {
		t.Fatalf("error code = %q, want APPROVAL_EXPIRED", errBody.Error.Code)
	}
}

func TestExpiredApprovalRejected(t *testing.T) {
	f := approvalSetup(t)
	apv := createPromotion(t, f, f.agent.ID)
	if err := f.db.Model(&model.Approval{}).Where("id = ?", apv.ID).
		Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	rec := postApproval(t, f, f.owner, "/api/v1/approvals/"+apv.ID+"/approve", "")
	if rec.Code != 409 {
		t.Fatalf("expired approve status = %d, want 409", rec.Code)
	}
}

func TestCreateRejectsOwnerAndNonMember(t *testing.T) {
	f := approvalSetup(t)
	// agent 已是 owner？先手工提升 agent2，再对它发起 promote。
	if err := f.db.Model(&model.WorkspaceMember{}).
		Where("workspace_id = ? AND actor_id = ?", f.wsID, f.agent2.ID).
		Update("role", "owner").Error; err != nil {
		t.Fatal(err)
	}
	rec := postApproval(t, f, f.owner, "/api/v1/workspaces/"+f.wsID+"/approvals",
		`{"action":"membership.promote_owner","target_actor_id":"`+f.agent2.ID+`"}`)
	if rec.Code != 400 {
		t.Fatalf("promote-owner-on-owner status = %d, want 400", rec.Code)
	}
	// 非成员目标。
	rec = postApproval(t, f, f.owner, "/api/v1/workspaces/"+f.wsID+"/approvals",
		`{"action":"membership.promote_owner","target_actor_id":"usr_nobody"}`)
	if rec.Code != 404 {
		t.Fatalf("non-member target status = %d, want 404", rec.Code)
	}
}
