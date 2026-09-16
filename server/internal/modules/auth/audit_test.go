package auth

// S4-3/S4-4 验收：认证失败各路径的 denied 审计行 + Me 响应 session.expires_at。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// auditRows 取指定 action 的审计行（按 id 升序 = 写入序，uuidv7 时间序）。
func auditRows(t *testing.T, s *Service, action string) []model.AuditEntry {
	t.Helper()
	var rows []model.AuditEntry
	if err := s.DB.Where("action = ?", action).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load audit rows (%s): %v", action, err)
	}
	return rows
}

func TestLoginDeniedAudit(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, err := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// 口令错：邮箱命中 → actor 可辨。
	if _, _, err := s.Login(ctx, "human@example.com", "wrong-pass1", "ip", "ua"); err == nil {
		t.Fatal("bad password should fail")
	}
	rows := auditRows(t, s, "auth.login")
	if len(rows) != 1 || rows[0].Outcome != "denied" || rows[0].ActorID == nil || *rows[0].ActorID != actor.ID {
		t.Fatalf("bad-password audit: %+v", rows)
	}

	// 未知邮箱：actor 不可辨 → NULL。
	if _, _, err := s.Login(ctx, "nobody@example.com", "wrong-pass1", "ip", "ua"); err == nil {
		t.Fatal("unknown email should fail")
	}
	rows = auditRows(t, s, "auth.login")
	if len(rows) != 2 || rows[1].Outcome != "denied" || rows[1].ActorID != nil {
		t.Fatalf("unknown-email audit: %+v", rows)
	}

	// 停用账号：actor 可辨，reason=account disabled。
	if err := s.DB.Model(&model.Actor{}).Where("id = ?", actor.ID).Update("disabled_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua"); err == nil {
		t.Fatal("disabled account should fail")
	}
	rows = auditRows(t, s, "auth.login")
	if len(rows) != 3 || !strings.Contains(string(rows[2].Details), "account disabled") {
		t.Fatalf("disabled audit: %+v", rows)
	}

	// R7：成功路径不写 audit（sessions 表自身即登录事实）。
	if err := s.DB.Model(&model.Actor{}).Where("id = ?", actor.ID).Update("disabled_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if rows = auditRows(t, s, "auth.login"); len(rows) != 3 {
		t.Fatalf("success must not audit (R7): %+v", rows)
	}
}

func TestRefreshReplayAudit(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"}, "ip", "ua")
	refresh, _, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := s.Refresh(ctx, refresh, "ip", "ua"); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// 旧 refresh 重放 → 整族撤销 + audit（actor = family 属主）。
	if _, err := s.Refresh(ctx, refresh, "ip", "ua"); err == nil {
		t.Fatal("replay should fail")
	}
	rows := auditRows(t, s, "auth.refresh")
	if len(rows) != 1 || rows[0].Outcome != "denied" || rows[0].ActorID == nil || *rows[0].ActorID != actor.ID {
		t.Fatalf("replay audit: %+v", rows)
	}
	if !strings.Contains(string(rows[0].Details), "refresh_token_replay") {
		t.Fatalf("replay audit details: %s", rows[0].Details)
	}

	// 非重放的普通 refresh 失败（unknown token）不在 S4-3 口径内，不写。
	if _, err := s.Refresh(ctx, "atr_unknown-token-value", "ip", "ua"); err == nil {
		t.Fatal("unknown refresh should fail")
	}
	if rows = auditRows(t, s, "auth.refresh"); len(rows) != 1 {
		t.Fatalf("unknown-token refresh must not audit: %+v", rows)
	}
}

func TestDeviceDeniedAndExpiredAudit(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"}, "ip", "ua")

	// denied：人工拒绝后兑换。
	created, err := s.CreateDeviceAuthorization(ctx, "cli", "https://astral.example.com")
	if err != nil {
		t.Fatalf("create device auth: %v", err)
	}
	view, err := s.FindByUserCode(ctx, created.UserCode)
	if err != nil {
		t.Fatalf("find by user_code: %v", err)
	}
	if err := s.Deny(ctx, view.ID, actor.ID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if _, err := s.ExchangeDeviceToken(ctx, created.DeviceCode, "ip", "ua"); err == nil {
		t.Fatal("denied exchange must fail")
	}
	rows := auditRows(t, s, "auth.device")
	if len(rows) != 1 || rows[0].Outcome != "denied" || rows[0].ActorID != nil ||
		rows[0].TargetID == nil || *rows[0].TargetID != view.ID {
		t.Fatalf("device denied audit: %+v", rows)
	}

	// expired：ExpiresAt 拨过 → 兑换时惰性置 expired → 401 + audit。
	created2, err := s.CreateDeviceAuthorization(ctx, "cli", "https://astral.example.com")
	if err != nil {
		t.Fatalf("create device auth 2: %v", err)
	}
	if err := s.DB.Model(&model.DeviceAuthorization{}).
		Where("user_code = ?", created2.UserCode).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExchangeDeviceToken(ctx, created2.DeviceCode, "ip", "ua"); err == nil {
		t.Fatal("expired exchange must fail")
	}
	rows = auditRows(t, s, "auth.device")
	if len(rows) != 2 || rows[1].Outcome != "denied" || !strings.Contains(string(rows[1].Details), "expired") {
		t.Fatalf("device expired audit: %+v", rows)
	}
}

func TestBearerDeniedAudit(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"}, "ip", "ua")
	refresh, _, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	pair, err := s.Refresh(ctx, refresh, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// 缺凭证（无 Bearer 无 Cookie）。
	if _, apiErr := s.ResolvePrincipal(ctx, "", ""); apiErr == nil || apiErr.Code != httpx.CodeAuthRequired {
		t.Fatalf("no credentials: %+v", apiErr)
	}
	// 无效 access token（查无 session）。
	if _, apiErr := s.ResolvePrincipal(ctx, "atr_not-a-real-token", ""); apiErr == nil || apiErr.Code != httpx.CodeAuthRequired {
		t.Fatalf("invalid token: %+v", apiErr)
	}
	// access 过期：actor 可辨（session 属主）。
	p, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, "")
	if apiErr != nil || p.SessionID == "" {
		t.Fatalf("principal: %+v %v", p, apiErr)
	}
	if err := s.DB.Model(&model.Session{}).Where("id = ?", p.SessionID).
		Update("access_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, ""); apiErr == nil || apiErr.Code != httpx.CodeTokenExpired {
		t.Fatalf("expired access: %+v", apiErr)
	}

	rows := auditRows(t, s, "auth.bearer")
	if len(rows) != 3 {
		t.Fatalf("bearer audit rows = %d: %+v", len(rows), rows)
	}
	for i, r := range rows {
		if r.Outcome != "denied" || r.WorkspaceID != nil {
			t.Fatalf("row %d not server-level denied: %+v", i, r)
		}
	}
	if rows[0].ActorID != nil || rows[1].ActorID != nil {
		t.Fatalf("unidentifiable rows must have NULL actor: %+v", rows)
	}
	if rows[2].ActorID == nil || *rows[2].ActorID != actor.ID {
		t.Fatalf("expired-access row actor: %+v", rows[2])
	}
}

// TestMeResponseSessionExpiry 是 S4-4 验收：/auth/me 与 login 响应都带
// session.expires_at（RFC3339），且值指向未来。
func TestMeResponseSessionExpiry(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	_, refresh, err := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe"}, "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	pair, err := s.Refresh(ctx, refresh, "ip", "ua")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	p, apiErr := s.ResolvePrincipal(ctx, pair.AccessToken, "")
	if apiErr != nil || p.SessionID == "" {
		t.Fatalf("principal: %+v %v", p, apiErr)
	}

	m := &Module{Svc: s}
	assertSession := func(t *testing.T, rec *httptest.ResponseRecorder) SessionInfo {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Session *SessionInfo `json:"session"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Session == nil || resp.Session.ClientType != "web" || resp.Session.ExpiresAt == "" {
			t.Fatalf("session block: %+v", resp.Session)
		}
		exp, err := time.Parse(time.RFC3339, resp.Session.ExpiresAt)
		if err != nil || !exp.After(time.Now()) {
			t.Fatalf("expires_at not a future RFC3339: %q err=%v", resp.Session.ExpiresAt, err)
		}
		return *resp.Session
	}

	// GET /auth/me：Principal 带 SessionID → 查 session 行。
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req = req.WithContext(WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	m.me(rec, req)
	assertSession(t, rec)

	// POST /auth/login：同构响应，session 块来自刚签发的 refresh。
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login",
		strings.NewReader(`{"email":"human@example.com","password":"hunter2safe"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	m.login(rec2, loginReq)
	assertSession(t, rec2)
}
