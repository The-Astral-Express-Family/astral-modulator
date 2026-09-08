package auth

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// 本地口令（TODO.md D6）。bcrypt cost 默认 10；口令上限 72 字节（bcrypt 输入限制）。
const (
	minPasswordLen  = 8
	maxPasswordByte = 72
	bcryptCost      = 10
)

func HashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func validatePassword(password string) error {
	if n := len(password); n < minPasswordLen {
		return fmt.Errorf("password too short (min %d chars)", minPasswordLen)
	}
	if len(password) > maxPasswordByte {
		return fmt.Errorf("password too long (max %d bytes)", maxPasswordByte)
	}
	if strings.TrimSpace(password) == "" || strings.EqualFold(password, strings.Repeat(string(password[0]), len(password))) {
		return fmt.Errorf("password too weak")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("password must contain letters and digits")
	}
	return nil
}
