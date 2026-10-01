// POST /invitations/redeem（ADR-0009）：已登录 human 凭工作区码入伙。
// 五组语义：成功+幂等重试、已是成员 409、失效统一 INVITE_INVALID（含
// 注册码类型混淆）、agent 403、空码 400。
package workspace

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/outbox"
)

// redeem 直调兑换 handler。
func (f *inviteFixture) redeem(t *testing.T, p *auth.Principal, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/invitations/redeem", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ctx := auth.WithPrincipal(req.Context(), p)
	f.m.redeemInvitation(rec, req.WithContext(ctx))
	return rec
}

// newInvitee 造一个非成员 human（兑换者视角）。
func (f *inviteFixture) newInvitee(t *testing.T, id string) *auth.Principal {
	t.Helper()
	if err := f.db.Create(&model.Actor{ID: id, Kind: "human", DisplayName: id}).Error; err != nil {
		t.Fatal(err)
	}
	return &auth.Principal{ActorID: id, Kind: "human"}
}

// regCode 造一张注册邀请（类型混淆诱饵：注册码出现在 redeem 必须 INVITE_INVALID）。
func (f *inviteFixture) regCode(t *testing.T) string {
	t.Helper()
	code, err := auth.NewInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&model.RegistrationInvitation{
		ID: "reg_decoy", CodeHash: auth.HashToken(auth.NormalizeInviteCode(code)),
		CreatedBy: "usr_owner", Status: "invited", ExpiresAt: time.Now().Add(24 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	return code
}

func TestRedeemJoinAndIdempotentRetry(t *testing.T) {
	f := inviteSetup(t)
	invitee := f.newInvitee(t, "usr_invitee")
	created := f.issue(t, `{"role":"contributor"}`)
	code, _ := created["code"].(string)

	rec := f.redeem(t, invitee, `{"code":"`+code+`"}`)
	if rec.Code != 200 {
		t.Fatalf("redeem status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Workspace struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"workspace"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Workspace.ID != f.wsID || out.Role != "contributor" {
		t.Fatalf("redeem result = %+v", out)
	}

	// 成员行 + 邀请已兑 + 审计/事件齐备。
	var mem model.WorkspaceMember
	if err := f.db.First(&mem, "workspace_id = ? AND actor_id = ?", f.wsID, invitee.ActorID).Error; err != nil {
		t.Fatalf("membership missing: %v", err)
	}
	if mem.Role != "contributor" {
		t.Fatalf("membership role = %q", mem.Role)
	}
	var inv model.Invitation
	if err := f.db.First(&inv, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "redeemed" || inv.RedeemedBy == nil || *inv.RedeemedBy != invitee.ActorID {
		t.Fatalf("invitation not redeemed properly: %+v", inv)
	}
	var audits []model.AuditEntry
	if err := f.db.Where("action = ?", "invite.redeem").Find(&audits).Error; err != nil || len(audits) != 1 {
		t.Fatalf("invite.redeem audits = %d (err=%v)", len(audits), err)
	}
	var events []model.OutboxEvent
	if err := f.db.Where("type IN ?", []string{
		outbox.TypeSecurityInviteRedeemed, outbox.TypeWorkspaceMemberChanged,
	}).Find(&events).Error; err != nil || len(events) != 2 {
		t.Fatalf("redeem events = %d (err=%v)", len(events), err)
	}

	// 幂等重试：同码同人再兑 → 200（网络重试安全），不再产生新事件。
	rec = f.redeem(t, invitee, `{"code":"`+code+`"}`)
	if rec.Code != 200 {
		t.Fatalf("idempotent retry status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := f.db.Where("type IN ?", []string{
		outbox.TypeSecurityInviteRedeemed, outbox.TypeWorkspaceMemberChanged,
	}).Find(&events).Error; err != nil || len(events) != 2 {
		t.Fatalf("idempotent retry must not emit events, got %d", len(events))
	}

	// 他人复用已兑码 → INVITE_INVALID。
	other := f.newInvitee(t, "usr_other")
	rec = f.redeem(t, other, `{"code":"`+code+`"}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), httpx.CodeInviteInvalid) {
		t.Fatalf("reuse by other = %d %s", rec.Code, rec.Body.String())
	}
}

func TestRedeemAlreadyMemberDifferentCode(t *testing.T) {
	f := inviteSetup(t)
	member := f.newInvitee(t, "usr_member")
	if err := f.db.Create(&model.WorkspaceMember{WorkspaceID: f.wsID, ActorID: member.ActorID, Role: "viewer"}).Error; err != nil {
		t.Fatal(err)
	}
	created := f.issue(t, `{"role":"contributor"}`)
	code, _ := created["code"].(string)

	rec := f.redeem(t, member, `{"code":"`+code+`"}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), httpx.CodeAlreadyMember) {
		t.Fatalf("already member = %d %s", rec.Code, rec.Body.String())
	}
	// 码不消耗。
	var inv model.Invitation
	if err := f.db.First(&inv, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("code must stay invited, got %q", inv.Status)
	}
}

// 防探测：查无/撤销/过期/注册码四种失效同码同文案。
func TestRedeemInvalidUnified(t *testing.T) {
	f := inviteSetup(t)
	invitee := f.newInvitee(t, "usr_probe")

	revoked := f.issue(t, `{"role":"viewer"}`)
	if err := f.db.Model(&model.Invitation{}).
		Where("id = ?", revoked["id"]).Update("status", "revoked").Error; err != nil {
		t.Fatal(err)
	}
	expired := f.issue(t, `{"role":"viewer"}`)
	if err := f.db.Model(&model.Invitation{}).
		Where("id = ?", expired["id"]).
		Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}

	codes := map[string]string{
		"unknown":  "AAAAA-AAAAA-AAAAA-AAAAA",
		"revoked":  revoked["code"].(string),
		"expired":  expired["code"].(string),
		"reg_code": f.regCode(t),
	}
	wantMsg := ""
	for name, code := range codes {
		rec := f.redeem(t, invitee, `{"code":"`+code+`"}`)
		if rec.Code != 400 {
			t.Fatalf("%s: status = %d", name, rec.Code)
		}
		var env struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if env.Error.Code != httpx.CodeInviteInvalid {
			t.Fatalf("%s: code = %s", name, env.Error.Code)
		}
		if wantMsg == "" {
			wantMsg = env.Error.Message
			continue
		}
		if env.Error.Message != wantMsg {
			t.Fatalf("%s: message %q != %q (anti-enumeration)", name, env.Error.Message, wantMsg)
		}
	}
}

func TestRedeemAgentForbiddenAndEmptyCode(t *testing.T) {
	f := inviteSetup(t)
	created := f.issue(t, `{"role":"viewer"}`)
	code, _ := created["code"].(string)

	// agent credential：入伙是人的行为。
	if rec := f.redeem(t, f.agent, `{"code":"`+code+`"}`); rec.Code != 403 {
		t.Fatalf("agent redeem = %d", rec.Code)
	}
	// 空码：400 VALIDATION_FAILED。
	if rec := f.redeem(t, f.owner, `{}`); rec.Code != 400 {
		t.Fatalf("empty code = %d", rec.Code)
	}
	// 码未被上述两次失败消耗。
	var inv model.Invitation
	if err := f.db.First(&inv, "id = ?", created["id"]).Error; err != nil {
		t.Fatal(err)
	}
	if inv.Status != "invited" {
		t.Fatalf("code must stay invited, got %q", inv.Status)
	}
}
