package bootstrap

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// 本文件的校验规则与既有实现保持一致，避免向导拒绝服务端会接受的输入：
//   - ValidateEmail / ValidatePassword 镜像 internal/modules/auth 的
//     Register/validatePassword 规则（改规则需两处同步）；
//   - ValidateDSN 只做形态检查，连通性一律以 store.Open 实测为准。

// ValidateDSN 校验 PostgreSQL 连接串形态（postgres:// 或 postgresql:// 且带 host）。
func ValidateDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("DSN 不能为空（无数据库请输入 none）")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return fmt.Errorf("DSN 需形如 postgres://user:pass@host:5433/db?sslmode=disable")
	}
	return nil
}

// ValidateEmail 与 auth.Service.Register 的最小校验一致（非空且含 @）。
func ValidateEmail(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("邮箱需形如 name@example.com")
	}
	return nil
}

// ValidatePassword 镜像 auth.validatePassword：8~72 字节、非纯空白/单一字符、
// 需同时含字母与数字（bcrypt 输入上限 72 字节）。
func ValidatePassword(pw string) error {
	if len(pw) < 8 {
		return fmt.Errorf("密码至少 8 位")
	}
	if len(pw) > 72 {
		return fmt.Errorf("密码过长（上限 72 字节）")
	}
	if strings.TrimSpace(pw) == "" || strings.EqualFold(pw, strings.Repeat(string(pw[0]), len(pw))) {
		return fmt.Errorf("密码过于简单（不能为空白或单一重复字符）")
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("密码需同时包含字母与数字")
	}
	return nil
}

// MaskDSN 脱敏展示 DSN：去掉 userinfo 与 query（两者都可能含口令），保留
// scheme/host/path。空串（桩模式）返回占位说明。
func MaskDSN(dsn string) string {
	if dsn == "" {
		return "（桩模式，未配置数据库）"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "（无法解析的 DSN）"
	}
	u.User = nil
	u.RawQuery = ""
	return u.String()
}
