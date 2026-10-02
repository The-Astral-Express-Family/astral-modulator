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
//   password reset      prt_<32B base64url>   （00023，30 分钟一次性）
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

// NewPasswordResetToken 生成忘记密码的一次性重置 token（00023）。
// 与 access/refresh 同族同强度：邮件明文传递、库中 sha256、30 分钟过期。
func NewPasswordResetToken() (string, error) { return newOpaque("prt_") }

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

// humanCodeAlphabet 去掉易混淆字符（0/O/1/I/L）。
// 供 user_code 与 tag confirm_code 共用（后者的生成器在 tag 包）。
const humanCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// randomFromAlphabet 是随机码生成的单一实现：n 字符取样自 alphabet。
// 新增随机码场景在此复用并声明自己的字母表，不再抄循环（invite 字母表在
// invites.go，恰 32 字符故 byte%len 无偏；user_code 的 31 字符表有 ±1/256
// 微偏，为既有接受设计——其定位见 docs/architecture.md §「user_code 随机强度」）。
func randomFromAlphabet(alphabet string, n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random code: %w", err)
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

// NewRandomCode 生成 n 位去混淆字符的随机码（人短时手输场景）。
func NewRandomCode(n int) (string, error) { return randomFromAlphabet(humanCodeAlphabet, n) }

// NewUserCode 生成 XXXX-XXXX 形式的人类比对码（security.md：强度要求低于
// device secret；枚举防护由 HTTP 层敏感桶承担——GET /auth/device/authorizations
// 10/min/IP，internal/ratelimit，TODO.md S5）。
func NewUserCode() (string, error) {
	half, err := NewRandomCode(4)
	if err != nil {
		return "", err
	}
	tail, err := NewRandomCode(4)
	if err != nil {
		return "", err
	}
	return half + "-" + tail, nil
}

// IsCredentialToken 判断 Bearer 值是否为 agent/service credential（区别于 human access token）。
func IsCredentialToken(bearer string) bool {
	return strings.HasPrefix(bearer, CredentialPrefix)
}
