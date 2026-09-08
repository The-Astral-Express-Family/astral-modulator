// Tag 名规范化与确认码生成（architecture §14：服务端只做确定性规则，
// 不做模糊相似度自动拒绝——语义近似判断交给调用者）。
package tag

import (
	"crypto/rand"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxTagNameLen  = 64
	confirmCodeLen = 6
)

// NormalizeName：trim + Unicode NFKC（兼容性折叠：全角→半角等）+ 小写。
// 入库前必须经过本函数（tags.normalized_name 的唯一性基于规范化名）；
// task 搜索按 tag 名过滤时也用它对齐语义（单一规范化实现）。
func NormalizeName(name string) string {
	trimmed := strings.TrimSpace(name)
	folded := norm.NFKC.String(trimmed)
	return strings.ToLower(folded)
}

// ValidateTagName 校验原始输入：非空、长度合法、无控制字符。
// 返回规范化名。
func ValidateTagName(name string) (string, bool) {
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	if utf8.RuneCountInString(strings.TrimSpace(name)) > maxTagNameLen {
		return "", false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return NormalizeName(name), true
}

// confirmCodeAlphabet 与 auth 的 user code 同源（去易混淆字符），
// 但确认码强度需求更低（TTL 120s、单次使用、绑定 actor）。
const confirmCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// NewConfirmCode 生成 6 位确认码（architecture §14 示例形状 K7P4Q2）。
// 明文只在 proposal 响应出现一次；库中只存 sha256 hash（见 module.go）。
func NewConfirmCode() (string, error) {
	buf := make([]byte, confirmCodeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, confirmCodeLen)
	for i, b := range buf {
		out[i] = confirmCodeAlphabet[int(b)%len(confirmCodeAlphabet)]
	}
	return string(out), nil
}
