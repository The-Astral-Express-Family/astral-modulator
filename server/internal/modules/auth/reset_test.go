// 忘记密码（00023）服务层测试：恒 204 防枚举、单活跃 token、一次性兑换、
// 成功后吊销全部会话、弱口令不烧 token。
package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/mail"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// fakeMailer 捕获投递（不触网络）；fail=true 时投递一律失败。
type fakeMailer struct {
	sent []mail.Message
	fail bool
}

func (f *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	if f.fail {
		return errors.New("smtp down")
	}
	f.sent = append(f.sent, msg)
	return nil
}

// registerHuman 直接走 bootstrap 注册一个账号并登录一次（产生会话）。
func registerHuman(t *testing.T, s *Service, email, password string) (*model.Actor, string) {
	t.Helper()
	actor, refresh, err := s.Register(context.Background(), RegisterInput{
		Email: email, Password: password, DisplayName: "Tester",
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return actor, refresh
}

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %v", err)
	}
	return apiErr.Code
}

func TestRequestPasswordResetIssuesSingleActiveToken(t *testing.T) {
	s := newSvc(t)
	fm := &fakeMailer{}
	s.Mailer = fm
	ctx := context.Background()
	registerHuman(t, s, "u@example.com", "hunter2safe")

	if err := s.RequestPasswordReset(ctx, "u@example.com", "ip", "https://web"); err != nil {
		t.Fatalf("request: %v", err)
	}
	if len(fm.sent) != 1 {
		t.Fatalf("want 1 mail, got %d", len(fm.sent))
	}
	m := fm.sent[0]
	if m.To != "u@example.com" || !strings.Contains(m.Subject, "重置") {
		t.Fatalf("mail envelope wrong: %+v", m)
	}
	if !strings.Contains(m.Text, "https://web/reset-password?token=prt_") {
		t.Fatalf("mail body missing reset link:\n%s", m.Text)
	}

	// 第二次请求：单活跃——旧 token 作废、新邮件可兑。
	if err := s.RequestPasswordReset(ctx, "U@Example.com ", "ip", "https://web"); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	if len(fm.sent) != 2 {
		t.Fatalf("want 2 mails, got %d", len(fm.sent))
	}
	var rows []model.PasswordResetToken
	if err := s.DB.Order("created_at").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 token rows, got %d", len(rows))
	}
	if rows[0].UsedAt == nil {
		t.Fatal("first token must be superseded (used_at set)")
	}
	if rows[1].UsedAt != nil {
		t.Fatal("second token must be active")
	}
}

func TestRequestPasswordResetUnknownEmailIsSilent204(t *testing.T) {
	s := newSvc(t)
	fm := &fakeMailer{}
	s.Mailer = fm
	// 先注册一个账号，保证库里「有 human」但请求的是另一个邮箱。
	registerHuman(t, s, "real@example.com", "hunter2safe")

	if err := s.RequestPasswordReset(context.Background(), "ghost@example.com", "ip", "https://web"); err != nil {
		t.Fatalf("unknown email must be silent success, got %v", err)
	}
	if len(fm.sent) != 0 {
		t.Fatalf("no mail expected, got %d", len(fm.sent))
	}
	var count int64
	if err := s.DB.Model(&model.PasswordResetToken{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("no token row expected, got %d", count)
	}
	// 停用账号同样静默（不暴露区分信号）。
	var actor model.Actor
	if err := s.DB.First(&actor, "kind = ?", "human").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.DB.Model(&actor).Update("disabled_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.RequestPasswordReset(context.Background(), "real@example.com", "ip", "https://web"); err != nil {
		t.Fatalf("disabled account must be silent success, got %v", err)
	}
	if len(fm.sent) != 0 {
		t.Fatalf("disabled account must not receive mail, got %d", len(fm.sent))
	}
	// 非法邮箱形状 400 VALIDATION_FAILED。
	err := s.RequestPasswordReset(context.Background(), "not-an-email", "ip", "https://web")
	if code := apiCode(t, err); code != httpx.CodeValidationFailed {
		t.Fatalf("malformed email = %s, want VALIDATION_FAILED", code)
	}
}

func TestConfirmPasswordResetHappyPath(t *testing.T) {
	s := newSvc(t)
	fm := &fakeMailer{}
	s.Mailer = fm
	ctx := context.Background()
	actor, refresh := registerHuman(t, s, "u@example.com", "hunter2safe")

	if err := s.RequestPasswordReset(ctx, "u@example.com", "ip", "https://web"); err != nil {
		t.Fatal(err)
	}
	token := extractToken(t, fm.sent[0].Text)

	if err := s.ConfirmPasswordReset(ctx, token, "newpass1safe"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	// 新口令可登录、旧口令失效。
	if _, _, err := s.Login(ctx, "u@example.com", "newpass1safe", "ip", "ua"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if _, _, err := s.Login(ctx, "u@example.com", "hunter2safe", "ip", "ua"); err == nil {
		t.Fatal("login with old password must fail")
	}
	// 旧会话被吊销：refresh 现在必须被拒。
	if _, err := s.Refresh(ctx, refresh, "ip", "ua"); err == nil {
		t.Fatal("old session must be revoked after reset")
	}
	// token 一次性：再用同 token（哪怕口令相同）→ PASSWORD_RESET_INVALID。
	err := s.ConfirmPasswordReset(ctx, token, "another1pass")
	if code := apiCode(t, err); code != httpx.CodePasswordResetInvalid {
		t.Fatalf("reuse = %s, want PASSWORD_RESET_INVALID", code)
	}
	_ = actor
}

func TestConfirmPasswordResetInvalidTokens(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	registerHuman(t, s, "u@example.com", "hunter2safe")

	// 查无 token。
	err := s.ConfirmPasswordReset(ctx, "prt_nonexistent", "newpass1safe")
	if code := apiCode(t, err); code != httpx.CodePasswordResetInvalid {
		t.Fatalf("unknown token = %s", code)
	}

	// 过期 token：直插一条已过期的。
	var ha model.HumanAuth
	if err := s.DB.First(&ha, "email = ?", "u@example.com").Error; err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := s.DB.Create(&model.PasswordResetToken{
		ID: "prt_expired", HumanAuthID: ha.ActorID, TokenHash: HashToken("prt_expiredtok"),
		ExpiresAt: past, CreatedAt: past.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	err = s.ConfirmPasswordReset(ctx, "prt_expiredtok", "newpass1safe")
	if code := apiCode(t, err); code != httpx.CodePasswordResetInvalid {
		t.Fatalf("expired token = %s", code)
	}
}

func TestConfirmPasswordResetWeakPasswordDoesNotBurnToken(t *testing.T) {
	s := newSvc(t)
	fm := &fakeMailer{}
	s.Mailer = fm
	ctx := context.Background()
	registerHuman(t, s, "u@example.com", "hunter2safe")

	if err := s.RequestPasswordReset(ctx, "u@example.com", "ip", "https://web"); err != nil {
		t.Fatal(err)
	}
	token := extractToken(t, fm.sent[0].Text)

	// 弱口令 400 VALIDATION_FAILED，token 不烧。
	err := s.ConfirmPasswordReset(ctx, token, "short")
	if code := apiCode(t, err); code != httpx.CodeValidationFailed {
		t.Fatalf("weak password = %s, want VALIDATION_FAILED", code)
	}
	// 同一 token 立即用合规口令兑换必须成功。
	if err := s.ConfirmPasswordReset(ctx, token, "goodpass1new"); err != nil {
		t.Fatalf("confirm after weak attempt: %v", err)
	}
}

func TestRequestPasswordResetMailFailureStill204(t *testing.T) {
	s := newSvc(t)
	s.Mailer = &fakeMailer{fail: true}
	ctx := context.Background()
	registerHuman(t, s, "u@example.com", "hunter2safe")

	// 投递失败不影响受理（HTTP 层恒 204）：token 行已落库。
	if err := s.RequestPasswordReset(ctx, "u@example.com", "ip", "https://web"); err != nil {
		t.Fatalf("request with broken mailer: %v", err)
	}
	var count int64
	if err := s.DB.Model(&model.PasswordResetToken{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("token row must persist on delivery failure, got %d", count)
	}
}

// extractToken 从邮件正文捞出 prt_ 明文（log transport 下管理员同样从日志捞）。
func extractToken(t *testing.T, body string) string {
	t.Helper()
	idx := strings.Index(body, "prt_")
	if idx < 0 {
		t.Fatalf("no token in body:\n%s", body)
	}
	end := idx
	for end < len(body) && !strings.ContainsRune(" \r\n\t", rune(body[end])) {
		end++
	}
	return body[idx:end]
}
