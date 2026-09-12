package bootstrap

import (
	"strings"
	"testing"
)

func TestValidateDSN(t *testing.T) {
	cases := []struct {
		dsn string
		ok  bool
	}{
		{"postgres://u:p@localhost:5433/db?sslmode=disable", true},
		{"postgresql://u:p@db.example.com:5432/astral", true},
		{"", false},
		{"none", false},
		{"mysql://localhost/db", false},
		{"not a dsn", false},
		{"postgres://", false},
	}
	for _, c := range cases {
		if err := ValidateDSN(c.dsn); (err == nil) != c.ok {
			t.Errorf("ValidateDSN(%q) err=%v, want ok=%v", c.dsn, err, c.ok)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	for _, ok := range []string{"a@b.com", "Admin@Example.COM"} {
		if err := ValidateEmail(ok); err != nil {
			t.Errorf("ValidateEmail(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "no-at-sign"} {
		if err := ValidateEmail(bad); err == nil {
			t.Errorf("ValidateEmail(%q) = nil, want error", bad)
		}
	}
}

// 规则需与 internal/modules/auth 的 validatePassword 保持一致。
func TestValidatePassword(t *testing.T) {
	for _, ok := range []string{"passw0rd", "abcdefgh1", "pass word1"} {
		if err := ValidatePassword(ok); err != nil {
			t.Errorf("ValidatePassword(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"",           // 过短
		"short1",     // <8
		"onlyletter", // 无数字
		"12345678",   // 无字母
		"aaaaaaaa",   // 单一重复字符
		"        ",   // 纯空白
	} {
		if err := ValidatePassword(bad); err == nil {
			t.Errorf("ValidatePassword(%q) = nil, want error", bad)
		}
	}
}

func TestMaskDSN(t *testing.T) {
	got := MaskDSN("postgres://astral:secret@localhost:5433/astral?sslmode=disable")
	if strings.Contains(got, "secret") {
		t.Errorf("MaskDSN 泄漏凭证：%q", got)
	}
	if !strings.Contains(got, "localhost:5433") {
		t.Errorf("MaskDSN 应保留 host:port：%q", got)
	}
	if MaskDSN("") == "" {
		t.Error("空 DSN 应返回桩模式占位说明")
	}
}
