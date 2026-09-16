package document

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

// TestCanonicalizePath 表驱动覆盖 sync-semantics §19 的 path 相关项
// （traversal / 绝对路径 / 空段 / 保留前缀 / 控制字符）与长度边界
// （硬约束 §1-12：按 UTF-8 字节数计）。
func TestCanonicalizePath(t *testing.T) {
	seg255 := strings.Repeat("a", 255)
	seg256 := strings.Repeat("a", 256)
	seg258CJK := strings.Repeat("中", 86) // 86 * 3 = 258 字节 > 255（按字节而非 rune 计）
	// 255 + 1 + 100 + 1 + 155 = 512 字节，各段均 ≤255。
	path512 := seg255 + "/" + strings.Repeat("b", 100) + "/" + strings.Repeat("c", 155)
	path513 := seg255 + "/" + strings.Repeat("b", 100) + "/" + strings.Repeat("c", 156)

	tests := []struct {
		name       string
		raw        string
		wantOK     bool
		wantReason string // 拒绝时断言 details.reason
	}{
		// 合法路径：原样通过（不做大小写变换）。
		{name: "合法多段", raw: "notes/2026/todo.md", wantOK: true},
		{name: "合法memory前缀", raw: "memory/org/plan.md", wantOK: true},
		{name: "合法单段", raw: "AGENTS.md", wantOK: true},
		{name: "合法中文路径", raw: "中文/笔记/待办.md", wantOK: true},
		{name: "合法段长恰255", raw: seg255, wantOK: true},
		{name: "合法总长恰512", raw: path512, wantOK: true},

		// traversal（sync-semantics §4）。
		{name: "traversal中段", raw: "a/../b", wantReason: "path_traversal"},
		{name: "traversal前导", raw: "../x", wantReason: "path_traversal"},
		{name: "traversal当前目录段", raw: "a/./b", wantReason: "path_traversal"},
		{name: "traversal尾段", raw: "a/..", wantReason: "path_traversal"},

		// 绝对路径。
		{name: "绝对路径", raw: "/a", wantReason: "path_absolute"},
		{name: "仅根斜杠", raw: "/", wantReason: "path_absolute"},

		// 空路径与空段。
		{name: "空路径", raw: "", wantReason: "path_empty"},
		{name: "连续斜杠", raw: "a//b", wantReason: "path_empty_segment"},
		{name: "尾随斜杠", raw: "a/", wantReason: "path_empty_segment"},
		{name: "目录间连续斜杠", raw: "notes//2026/x.md", wantReason: "path_empty_segment"},

		// 反斜杠（含 Windows 风格 traversal）。
		{name: "反斜杠", raw: "a\\b", wantReason: "path_backslash"},
		{name: "Windows风格traversal", raw: "..\\..\\etc", wantReason: "path_backslash"},

		// NUL / 控制字符（<0x20 及 0x7F）。
		{name: "NUL", raw: "a\x00b.md", wantReason: "path_control_char"},
		{name: "换行", raw: "a\nb.md", wantReason: "path_control_char"},
		{name: "DEL(0x7F)", raw: "a\x7fb.md", wantReason: "path_control_char"},

		// 保留前缀与保留文件名（sync-semantics §3 exclude 黑名单）。
		{name: "git目录", raw: ".git/config", wantReason: "reserved_prefix"},
		{name: "astral状态目录", raw: ".astral/x", wantReason: "reserved_prefix"},
		{name: "secrets目录", raw: "secrets/k", wantReason: "reserved_prefix"},
		{name: "env文件", raw: ".env", wantReason: "reserved_file"},
		{name: "env变体", raw: ".env.production", wantReason: "reserved_file"},
		{name: "嵌套env文件", raw: "config/.env.local", wantReason: "reserved_file"},

		// 长度边界（UTF-8 字节数）。
		{name: "段长256", raw: seg256, wantReason: "segment_too_long"},
		{name: "中文段258字节", raw: seg258CJK, wantReason: "segment_too_long"},
		{name: "总长513", raw: path513, wantReason: "path_too_long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalizePath(tt.raw)
			if !tt.wantOK {
				if err == nil {
					t.Fatalf("CanonicalizePath(%q) = %q, want error (reason=%s)", tt.raw, got, tt.wantReason)
				}
				var apiErr *httpx.APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("error 类型 %T 不是 *httpx.APIError: %v", err, err)
				}
				if apiErr.Status != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", apiErr.Status)
				}
				if apiErr.Code != httpx.CodeValidationFailed {
					t.Errorf("code = %q, want %q", apiErr.Code, httpx.CodeValidationFailed)
				}
				if field, _ := apiErr.Details["field"].(string); field != "path" {
					t.Errorf("details.field = %v, want path", apiErr.Details["field"])
				}
				if reason, _ := apiErr.Details["reason"].(string); reason != tt.wantReason {
					t.Errorf("details.reason = %q, want %q", reason, tt.wantReason)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalizePath(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.raw {
				t.Errorf("CanonicalizePath(%q) = %q, want 原样返回", tt.raw, got)
			}
		})
	}
}

// TestHasCaseCollision 覆盖 R1 裁决（仅大小写不同才算 collision）正反例。
func TestHasCaseCollision(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		candidate string
		want      bool
	}{
		{"目录段大小写不同", "Docs/a.md", "docs/a.md", true},
		{"完全相同不算冲突", "docs/a.md", "docs/a.md", false},
		{"文件段大小写不同", "a/B.md", "a/b.md", true},
		{"多段同时大小写不同", "DOCS/Readme.MD", "docs/readme.md", true},
		{"单段大小写不同", "A.md", "a.md", true},
		{"中文段后仅大小写差异", "中文/Plan.md", "中文/plan.md", true},
		{"不同层级", "a/b.md", "a/b/c.md", false},
		{"同层级不同目录", "a/b.md", "x/b.md", false},
		{"差异不只是大小写", "Docs/a.md", "docs/b.md", false},
		{"中文不同路径", "中文/a.md", "论文/a.md", false},
		{"空路径完全相同", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasCaseCollision(tt.existing, tt.candidate); got != tt.want {
				t.Errorf("HasCaseCollision(%q, %q) = %v, want %v", tt.existing, tt.candidate, got, tt.want)
			}
			// 对称性：交换参数结论不变。
			if got := HasCaseCollision(tt.candidate, tt.existing); got != tt.want {
				t.Errorf("HasCaseCollision(%q, %q) = %v, want %v（对称性）", tt.candidate, tt.existing, got, tt.want)
			}
		})
	}
}
