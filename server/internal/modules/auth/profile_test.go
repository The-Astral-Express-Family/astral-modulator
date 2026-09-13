package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// authRouter 组装仅含 auth 模块的路由（含 Authenticate），供 HTTP 层测试。
func authRouter(s *Service) http.Handler {
	m := &Module{Svc: s}
	r := chi.NewRouter()
	m.RegisterPublic(r)
	r.Group(func(priv chi.Router) {
		priv.Use(s.Authenticate)
		m.RegisterPrivate(priv)
	})
	return r
}

func strPtr(v string) *string { return &v }

func TestUpdateProfile(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, _ := s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe", DisplayName: "Hime"}, "ip", "ua")

	updated, err := s.UpdateProfile(ctx, actor.ID, UpdateProfileInput{
		DisplayName: strPtr("  Hime Chen  "),
		Bio:         strPtr("星灵调制器维护者"),
		AvatarURL:   strPtr("https://cdn.example.com/a.png"),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DisplayName != "Hime Chen" { // trim 后落库
		t.Fatalf("display_name = %q", updated.DisplayName)
	}
	if updated.Bio != "星灵调制器维护者" || updated.AvatarURL == nil || *updated.AvatarURL != "https://cdn.example.com/a.png" {
		t.Fatalf("updated = %+v", updated)
	}

	// 部分更新语义：未给出的字段不动。
	updated, err = s.UpdateProfile(ctx, actor.ID, UpdateProfileInput{Bio: strPtr("新签名")})
	if err != nil {
		t.Fatalf("partial update: %v", err)
	}
	if updated.DisplayName != "Hime Chen" || updated.Bio != "新签名" || updated.AvatarURL == nil {
		t.Fatalf("partial updated = %+v", updated)
	}

	// 空串 = 清除头像（回 NULL）。
	updated, err = s.UpdateProfile(ctx, actor.ID, UpdateProfileInput{AvatarURL: strPtr("")})
	if err != nil {
		t.Fatalf("clear avatar: %v", err)
	}
	if updated.AvatarURL != nil {
		t.Fatalf("avatar_url = %v, want nil", updated.AvatarURL)
	}

	// 校验失败 → 400 VALIDATION_FAILED。
	bad := []UpdateProfileInput{
		{DisplayName: strPtr("   ")},                    // 空白名
		{DisplayName: strPtr(strings.Repeat("x", 201))}, // 超长名
		{Bio: strPtr(strings.Repeat("签", 501))},         // 超长签名（rune 计数）
		{AvatarURL: strPtr("javascript:alert(1)")},      // 非 http(s)
		{AvatarURL: strPtr("ftp://example.com/a.png")},  // 非 http(s)
		{AvatarURL: strPtr("https://")},                 // 无 host
		{AvatarURL: strPtr(strings.Repeat("h", 501))},   // 超长 URL
	}
	for i, in := range bad {
		_, err := s.UpdateProfile(ctx, actor.ID, in)
		if err == nil {
			t.Fatalf("case %d should fail: %+v", i, in)
		}
		if apiErr, ok := err.(*httpx.APIError); !ok || apiErr.Code != httpx.CodeValidationFailed {
			t.Fatalf("case %d err = %v, want VALIDATION_FAILED", i, err)
		}
	}

	// 500 rune 签名恰好通过（rune 计数而非字节，中文友好）。
	if _, err := s.UpdateProfile(ctx, actor.ID, UpdateProfileInput{Bio: strPtr(strings.Repeat("签", 500))}); err != nil {
		t.Fatalf("bio at limit: %v", err)
	}

	// 全字段缺省 = 幂等 no-op。
	if _, err := s.UpdateProfile(ctx, actor.ID, UpdateProfileInput{}); err != nil {
		t.Fatalf("noop: %v", err)
	}

	// 不存在的 actor → 404。
	_, err = s.UpdateProfile(ctx, "usr_missing", UpdateProfileInput{Bio: strPtr("x")})
	if apiErr, ok := err.(*httpx.APIError); !ok || apiErr.Code != httpx.CodeNotFound {
		t.Fatalf("missing actor err = %v, want NOT_FOUND", err)
	}
}

func TestHumanEmail(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	actor, _, _ := s.Register(ctx, RegisterInput{Email: "Human@Example.com", Password: "hunter2safe"}, "ip", "ua")
	if email, err := s.HumanEmail(ctx, actor.ID); err != nil || email != "human@example.com" {
		t.Fatalf("email = %q err = %v", email, err)
	}
	// agent 无 human_auth → 空串（Me 响应随之省略）。
	s.DB.Create(&model.Actor{ID: "agt_p1", Kind: "agent", DisplayName: "A"})
	if email, err := s.HumanEmail(ctx, "agt_p1"); err != nil || email != "" {
		t.Fatalf("agent email = %q err = %v", email, err)
	}
}

// TestMeEndpointsSerialization：HTTP 层回归——Me/Actor 必须 snake_case 且带
// email/bio/avatar_url（修复直接序列化 model.Actor 的 PascalCase 漂移），
// PATCH /auth/me 走完整 编解码→校验→更新 链路。
func TestMeEndpointsSerialization(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.Register(ctx, RegisterInput{Email: "human@example.com", Password: "hunter2safe", DisplayName: "Hime"}, "ip", "ua")
	refresh, actor, err := s.Login(ctx, "human@example.com", "hunter2safe", "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateProfile(ctx, actor.ID, UpdateProfileInput{Bio: strPtr("签名"), AvatarURL: strPtr("https://cdn.example.com/a.png")})
	pair, err := s.Refresh(ctx, refresh, "ip", "ua")
	if err != nil {
		t.Fatal(err)
	}
	router := authRouter(s)
	do := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/auth/me", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["DisplayName"] != nil || body["ID"] != nil {
		t.Fatalf("PascalCase leaked: %v", body)
	}
	me, _ := body["actor"].(map[string]any)
	for _, k := range []string{"id", "kind", "display_name", "bio", "avatar_url"} {
		if _, ok := me[k]; !ok {
			t.Fatalf("actor missing %q: %v", k, me)
		}
	}
	if me["display_name"] != "Hime" || me["bio"] != "签名" || me["avatar_url"] != "https://cdn.example.com/a.png" {
		t.Fatalf("actor = %v", me)
	}
	if body["email"] != "human@example.com" {
		t.Fatalf("email = %v", body["email"])
	}

	rec = do(http.MethodPatch, `{"display_name":"Hime Chen","avatar_url":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	me, _ = body["actor"].(map[string]any)
	if me["display_name"] != "Hime Chen" || me["avatar_url"] != "" || me["bio"] != "签名" {
		t.Fatalf("patched actor = %v", me)
	}

	// 非法 avatar_url → 400 VALIDATION_FAILED envelope。
	rec = do(http.MethodPatch, `{"avatar_url":"javascript:alert(1)"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Error.Code != httpx.CodeValidationFailed {
		t.Fatalf("error code = %q", errBody.Error.Code)
	}

	// 未认证 → 401。
	req := httptest.NewRequest(http.MethodPatch, "/auth/me", bytes.NewBufferString(`{}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", rec.Code)
	}
}
