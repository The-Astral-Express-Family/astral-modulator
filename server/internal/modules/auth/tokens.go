package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// opaque token 家族与格式（TODO.md A4）：
//   human access token  ata_<32B base64url>
//   human refresh token atr_<32B base64url>
//   device code         adc_<32B base64url>   （≥128-bit 随机）
//   agent credential    astral_<32B base64url>
// 库中一律只存 sha256 hex。比较用常数时间。

const tokenRandomBytes = 32

func newOpaque(prefix string) (string, error) {
	buf := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func NewAccessToken() (string, error)  { return newOpaque("ata_") }
func NewRefreshToken() (string, error) { return newOpaque("atr_") }
func NewDeviceCode() (string, error)   { return newOpaque("adc_") }

// CredentialPrefix 是 agent/service credential secret 的固定前缀。
// 服务端通过前缀区分 access token 与 credential（两者同为 Bearer）。
const CredentialPrefix = "astral_"

func NewCredentialSecret() (string, error) { return newOpaque(CredentialPrefix) }

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// HashEqual 常数时间比较明文与其 hash。
func HashEqual(token, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashToken(token)), []byte(hash)) == 1
}

// userCodeAlphabet 去掉易混淆字符（0/O/1/I/L）。
const userCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewUserCode 生成 XXXX-XXXX 形式的人类比对码（security.md：强度要求低于
// device secret，但需限流防枚举——限流在 HTTP 层做，TODO(phase-6)）。
func NewUserCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 9)
	for i := 0; i < 9; i++ {
		if i == 4 {
			out[i] = '-'
			continue
		}
		idx := i
		if idx > 4 {
			idx--
		}
		out[i] = userCodeAlphabet[int(buf[idx])%len(userCodeAlphabet)]
	}
	return string(out), nil
}

// IsCredentialToken 判断 Bearer 值是否为 agent/service credential（区别于 human access token）。
func IsCredentialToken(bearer string) bool {
	return strings.HasPrefix(bearer, CredentialPrefix)
}
