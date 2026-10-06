package auth

import "strings"

// NormalizeEmail 是邮箱输入的单一规范化+形态闸：lower + trim，非空、含 @、
// ≤254 字节（human_auth.email 列宽——超长原会撞列 500，统一前置 400）。
// 消费方：Register/Login/password-reset/workspace 邀请/bootstrap 向导
// （bootstrap.ValidateEmail 经此同步，不再手工镜像规则）。
func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || !strings.Contains(email, "@") || len(email) > 254 {
		return "", false
	}
	return email, true
}
