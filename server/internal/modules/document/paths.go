package document

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// 路径限制：长度一律按 UTF-8 字节数计（round 38 硬约束 §1-12）。
const (
	maxPathBytes    = 512 // 整条路径（URL 解码后）上限
	maxSegmentBytes = 255 // 单段（相邻 '/' 之间）上限
)

// reservedDirPrefixes 是整条路径第一段的保留目录（sync-semantics §3 exclude 的
// 服务端固定子集；R3 裁决：include/exclude glob 归客户端约定，服务端只保留黑名单）。
var reservedDirPrefixes = map[string]struct{}{
	".git":    {},
	".astral": {},
	"secrets": {},
}

// reservedFilePrefix：任一文件名段以此开头即拒绝（对应 exclude glob `**/.env*`，
// 覆盖 .env / .env.local / .env.production 等）。
const reservedFilePrefix = ".env"

// CanonicalizePath 校验并规范化 URL 解码后的受管文档路径（chi {path:.*} 捕获值，
// 服务端不信任客户端 glob，sync-semantics §3）。通过校验时**原样返回**：
// 只做安全校验，不做大小写/分隔符变换——大小写冲突由 push 侧 HasCaseCollision
// + DB LOWER 查询把关（R1 裁决）。
//
// 拒绝规则（sync-semantics §3/§4、architecture §16）：
//   - 空路径、前导 '/'（绝对路径）、反斜杠 '\'；
//   - NUL/控制字符（<0x20 及 0x7F）；
//   - 空段（连续 '/' 或尾随 '/'）、'.'/'..' 段（traversal）；
//   - 任一段 >255 字节、总长 >512 字节（UTF-8 字节数）；
//   - 保留目录前缀（.git/.astral/secrets）、以 .env 开头的文件名段。
//
// 错误为 *httpx.APIError（400 VALIDATION_FAILED，details.field=path，
// details.reason 为稳定细分码），handler 可直接交给 httpx.RespondError
// 完成 envelope 转换。
func CanonicalizePath(raw string) (string, error) {
	if raw == "" {
		return "", invalidPath("path_empty", "path must not be empty")
	}
	if strings.HasPrefix(raw, "/") {
		return "", invalidPath("path_absolute", "path must be relative, not absolute")
	}
	if strings.Contains(raw, `\`) {
		return "", invalidPath("path_backslash", `path separator must be '/', not '\'`)
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c < 0x20 || c == 0x7F {
			return "", invalidPath("path_control_char", "path must not contain control characters")
		}
	}
	if len(raw) > maxPathBytes {
		return "", invalidPath("path_too_long", fmt.Sprintf("path exceeds %d bytes", maxPathBytes))
	}
	segments := strings.Split(raw, "/")
	for _, seg := range segments {
		switch {
		case seg == "":
			return "", invalidPath("path_empty_segment", "path must not contain empty segments")
		case seg == "." || seg == "..":
			return "", invalidPath("path_traversal", "path must not contain '.' or '..' segments")
		case len(seg) > maxSegmentBytes:
			return "", invalidPath("segment_too_long", fmt.Sprintf("path segment exceeds %d bytes", maxSegmentBytes))
		case strings.HasPrefix(seg, reservedFilePrefix):
			return "", invalidPath("reserved_file", "path segment must not start with '.env'")
		}
	}
	if _, reserved := reservedDirPrefixes[segments[0]]; reserved {
		return "", invalidPath("reserved_prefix", "reserved directory prefix: "+segments[0])
	}
	return raw, nil
}

// invalidPath 构造路径校验失败的 400 VALIDATION_FAILED 错误：
// details.field=path 供 CLI/GUI 定位到路径参数；reason 为稳定细分码
// （单测与后续 T4 集成测试断言用）。
func invalidPath(reason, message string) *httpx.APIError {
	return &httpx.APIError{
		Status:  http.StatusBadRequest,
		Code:    httpx.CodeValidationFailed,
		Message: message,
		Details: map[string]any{"field": "path", "reason": reason},
	}
}

// HasCaseCollision 判断 existing 与 candidate 是否「仅大小写不同」（R1 裁决的
// 纯函数部分，语义等价于 push 侧 DB 查询 `LOWER(path)=LOWER(?) AND path<>?`，
// ASCII 小写）：段数相同且逐段小写后全部相等、但原路径不完全相同 → true。
//   - 完全相同的路径不算冲突（返回 false）；
//   - 段数不同（不同层级）→ false；
//   - 任一段小写后仍不相等（差异不只是大小写，如 Docs/a.md vs docs/b.md）→ false。
//
// DB 查询侧由 document store 实现（T4），两侧必须保持同一口径。
func HasCaseCollision(existing, candidate string) bool {
	if existing == candidate {
		return false
	}
	a := strings.Split(existing, "/")
	b := strings.Split(candidate, "/")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if asciiLower(a[i]) != asciiLower(b[i]) {
			return false
		}
	}
	return true
}

// asciiLower 仅做 ASCII A–Z 小写化（R1 裁决：ASCII-only LOWER 可接受，MVP）。
// 非 ASCII 字符原样保留——中文等路径不参与折叠，也不引入 Unicode 大小写陷阱。
func asciiLower(s string) string {
	buf := []byte(s)
	for i, c := range buf {
		if c >= 'A' && c <= 'Z' {
			buf[i] = c + ('a' - 'A')
		}
	}
	return string(buf)
}
