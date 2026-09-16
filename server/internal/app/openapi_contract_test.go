// Package app 的 openapi↔路由 一致性测试（防漂移契约门）：
// api/openapi.yaml 是公网协议唯一事实来源，本测试保证
//  1. openapi 声明的每个 path+method 都在 chi 路由上真实注册；
//  2. 服务端注册的每个 /api/v1 路由（及 well-known）都出现在 openapi 里。
//
// 任何一侧改动而另一侧未同步，CI 会在此处失败。
package app

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/config"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/audit"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/auth"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/document"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/event"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/memory"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/message"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/presence"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/task"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/workspace"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

// openapiMethods 是 openapi path item 下代表 HTTP 操作的键。
var openapiMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true, "delete": true,
	"head": true, "options": true, "trace": true,
}

// loadOpenapiOperations 解析 openapi.yaml，返回 "METHOD PATH" 集合。
func loadOpenapiOperations(t *testing.T) map[string]bool {
	t.Helper()
	// 测试位于 server/internal/app，仓库根在 ../../../。
	specPath := filepath.Join("..", "..", "..", "api", "openapi.yaml")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi yaml: %v", err)
	}
	ops := map[string]bool{}
	for path, item := range doc.Paths {
		for key := range item {
			if openapiMethods[strings.ToLower(key)] {
				ops[strings.ToUpper(key)+" "+path] = true
			}
		}
	}
	if len(ops) == 0 {
		t.Fatal("openapi spec yielded zero operations; parse failure?")
	}
	return ops
}

// collectChiRoutes 收集真实路由（含 well-known），规范化 chi 通配语法。
func collectChiRoutes(t *testing.T) map[string]bool {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := testsupport.NewTestDB(t)
	svc := auth.NewService(db, log)
	hub := event.NewHub()
	mods := &Modules{
		Auth:      &auth.Module{Svc: svc, PublicURL: "https://astral.example.com"},
		Workspace: &workspace.Module{DB: db, Auth: svc},
		Task:      &task.Module{DB: db, Auth: svc},
		Tag:       &tag.Module{},
		Memory:    &memory.Module{},
		Document:  &document.Module{},
		Message:   &message.Module{DB: db, Auth: svc},
		Presence:  &presence.Module{DB: db, Auth: svc},
		Audit:     &audit.Module{},
		Events:    &event.SSEHandler{Hub: hub, DB: db, Auth: svc},
	}
	mods.Message.Tasks = mods.Task
	mux := NewRouter(config.Config{ServerID: "srv_test"}, log, db, mods).(*chi.Mux)

	routes := map[string]bool{}
	walkErr := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "" {
			return nil // 挂载点等内部节点
		}
		// 运维端点不属于公网契约。
		if route == "/healthz" || route == "/readyz" {
			return nil
		}
		// chi 的 {path:.*} 正则后缀在 openapi 中写作 {path}。
		route = strings.ReplaceAll(route, ":.*}", "}")
		// chi 的尾通配 /*（catch-all，document 多段路径的唯一可匹配形态，
		// round 38 T4 落地：{path:.*} 是单段正则节点）同样写作 {path}。
		if strings.HasSuffix(route, "/*") {
			route = strings.TrimSuffix(route, "/*") + "/{path}"
		}
		// openapi paths 相对 servers.url=/api/v1；well-known 在规范里写绝对路径。
		if strings.HasPrefix(route, "/api/v1") {
			route = strings.TrimPrefix(route, "/api/v1")
		}
		routes[method+" "+route] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk chi routes: %v", walkErr)
	}
	return routes
}

func TestRoutesMatchOpenapi(t *testing.T) {
	specOps := loadOpenapiOperations(t)
	serverRoutes := collectChiRoutes(t)

	var missingOnServer, missingInSpec []string
	for op := range specOps {
		if !serverRoutes[op] {
			missingOnServer = append(missingOnServer, op)
		}
	}
	for op := range serverRoutes {
		if !specOps[op] {
			missingInSpec = append(missingInSpec, op)
		}
	}
	sort.Strings(missingOnServer)
	sort.Strings(missingInSpec)

	if len(missingOnServer) > 0 {
		t.Errorf("openapi 声明但服务端未注册的路由 (%d):\n  %s",
			len(missingOnServer), strings.Join(missingOnServer, "\n  "))
	}
	if len(missingInSpec) > 0 {
		t.Errorf("服务端注册但 openapi 未声明的路由 (%d):\n  %s",
			len(missingInSpec), strings.Join(missingInSpec, "\n  "))
	}
	if len(missingOnServer) == 0 && len(missingInSpec) == 0 {
		t.Logf("route↔openapi in sync: %d operations", len(specOps))
	}
}

// TestOpenapiErrorCodesMatchHttpx 保证 openapi 的错误码枚举与
// internal/httpx 的常量不漂移（CLI 的错误处理直接依赖这些 code）。
func TestOpenapiErrorCodesMatchHttpx(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas struct {
				ErrorCode struct {
					Enum []string `yaml:"enum"`
				} `yaml:"ErrorCode"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	specCodes := map[string]bool{}
	for _, c := range doc.Components.Schemas.ErrorCode.Enum {
		specCodes[c] = true
	}
	httpxCodes := parseHttpxErrorCodes(t)
	if len(httpxCodes) == 0 {
		t.Fatal("failed to parse httpx error codes; parser broken?")
	}
	for _, code := range httpxCodes {
		if !specCodes[code] {
			t.Errorf("httpx 错误码 %q 缺席 openapi ErrorCode enum", code)
		}
	}
	for code := range specCodes {
		if !slices.Contains(httpxCodes, code) {
			t.Errorf("openapi 错误码 %q 在 httpx 中不存在", code)
		}
	}
	if len(doc.Components.Schemas.ErrorCode.Enum) != len(httpxCodes) {
		t.Errorf("openapi enum 有 %d 个错误码，httpx 清单 %d 个；两清单必须一致",
			len(doc.Components.Schemas.ErrorCode.Enum), len(httpxCodes))
	}
}

// parseHttpxErrorCodes 直接解析 internal/httpx/errors.go 的字符串常量，
// 避免“契约门”自身再维护一份易漂移的手工清单。
func parseHttpxErrorCodes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "httpx", "errors.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`Code\w+\s*=\s*"([A-Z_]+)"`)
	seen := map[string]bool{}
	var codes []string
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			codes = append(codes, m[1])
		}
	}
	sort.Strings(codes)
	return codes
}
