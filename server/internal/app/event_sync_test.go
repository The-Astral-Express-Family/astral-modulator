package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEventTypesSync 保证事件类型目录三处不漂移：
//  1. server/internal/modules/event/types.go（服务端实现）；
//  2. api/schemas/event.json（契约，CLI 快照的源）；
//  3. web/src/api/sse.ts（Web 订阅清单）。
//
// 任一侧新增/删除事件而其他侧未同步，CI 在此失败。
func TestEventTypesSync(t *testing.T) {
	// 服务端常量（types.go 中 Type* = "..." 形式）。
	typesGo, err := os.ReadFile(filepath.Join("..", "..", "..", "server", "internal", "modules", "event", "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	constRe := regexp.MustCompile(`Type\w+\s*=\s*"([a-z]+\.[a-z.]+)"`)
	goTypes := map[string]bool{}
	for _, m := range constRe.FindAllStringSubmatch(string(typesGo), -1) {
		goTypes[m[1]] = true
	}

	// 契约 enum（event.json "type" 的 enum 值）。
	eventJSON, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "schemas", "event.json"))
	if err != nil {
		t.Fatal(err)
	}
	jsonTypes := map[string]bool{}
	jsonRe := regexp.MustCompile(`"(?:workspace|actor|task|document|message|security|tag|snapshot|human)\.[a-z.]+"`)
	for _, m := range jsonRe.FindAllString(string(eventJSON), -1) {
		jsonTypes[strings.Trim(m, `"`)] = true
	}

	// web 订阅清单（sse.ts 中引号字符串行）。
	sseTS, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "api", "sse.ts"))
	if err != nil {
		t.Fatal(err)
	}
	webTypes := map[string]bool{}
	webRe := regexp.MustCompile(`'(?:workspace|actor|task|document|message|security|tag|snapshot|human)\.[a-z.]+'`)
	for _, m := range webRe.FindAllString(string(sseTS), -1) {
		webTypes[strings.Trim(m, "'")] = true
	}

	if len(goTypes) == 0 || len(jsonTypes) == 0 || len(webTypes) == 0 {
		t.Fatalf("regex 提取失败：go=%d json=%d web=%d", len(goTypes), len(jsonTypes), len(webTypes))
	}

	compare := func(name string, a, b map[string]bool) {
		var missing []string
		for v := range a {
			if !b[v] {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("%s 缺失事件类型: %s", name, strings.Join(missing, ", "))
		}
	}
	compare("api/schemas/event.json", goTypes, jsonTypes)
	compare("web/src/api/sse.ts", goTypes, webTypes)
	compare("server types.go", jsonTypes, goTypes)
	compare("web/src/api/sse.ts", jsonTypes, webTypes)
}
