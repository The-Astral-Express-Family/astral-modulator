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

// humanCodeAlphabet 去掉易混淆字符（0/O/1/I/L）。
// 供 user_code 与 tag confirm_code 共用（后者的生成器在 tag 包）。
const humanCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// randomFromAlphabet 是随机码生成的单一实现：n 字符取样自 alphabet。
// 新增随机码场景在此复用并声明自己的字母表，不再抄循环（invite 字母表恰
// 32 字符故 byte%len 无偏；user_code 的 31 字符表有 ±1/256 微偏，为既有
// 接受设计——其定位见 docs/architecture.md §「user_code 随机强度」）。
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
// device secret，但需限流防枚举——限流在 HTTP 层做，TODO(phase-6)）。
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

// inviteCodeAlphabet 是 Crockford base32（去 I/L/O/U）。恰好 32 字符，
// byte%32 无取样偏差；20 字符 = 100 bit 熵。
const inviteCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewInviteCode 生成一次性邀请码，分组展示形 XXXXX-XXXXX-XXXXX-XXXXX。
// 与 user_code（人短时手输）不同：邀请码要在邮箱/聊天里存活数天，防御对象
// 是离线爆破，因此熵高两个量级（docs/registration.md §2.2）。
func NewInviteCode() (string, error) {
	body, err := randomFromAlphabet(inviteCodeAlphabet, 20)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{body[:5], body[5:10], body[10:15], body[15:]}, "-"), nil
}

// NormalizeInviteCode 是兑换时的码归一化（比对前唯一入口）：去分隔符、大写。
// 库中 code_hash 一律基于归一化形式计算。
func NormalizeInviteCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(code, "-", ""))
}
