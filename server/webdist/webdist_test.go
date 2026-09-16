package webdist

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// populatedFS 模拟 make web-dist 拷入后的 dist（index.html + 哈希资产）。
func populatedFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>astral</title>")},
		"assets/app-1a2b.js": {Data: []byte("console.log('app')")},
		"favicon.ico":        {Data: []byte("\x00\x00\x01\x00")},
	}
}

func TestAvailableFS(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
		want bool
	}{
		{"index 存在", populatedFS(), true},
		{"仅 .gitkeep 占位（仓库内默认态）", fstest.MapFS{".gitkeep": {}}, false},
		{"只有资产无壳", fstest.MapFS{"assets/app.js": {}}, false},
		{"空目录", fstest.MapFS{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AvailableFS(tt.fsys); got != tt.want {
				t.Fatalf("AvailableFS() = %v, want %v", got, tt.want)
			}
		})
	}
	if AvailableFS(nil) {
		t.Fatal("AvailableFS(nil) = true, want false")
	}
	// 嵌入态与 dist 实际内容一致即可（web-dist 执行前后取值不同，
	// 不对 repo 当前状态做断言，只保证查询不 panic）。
	_ = Available()
}

func TestHandlerFS(t *testing.T) {
	tests := []struct {
		name       string
		fsys       fstest.MapFS
		path       string
		wantStatus int
		wantBody   string // 非空时做包含断言
	}{
		{"根路径回退壳", populatedFS(), "/", http.StatusOK, "<title>astral</title>"},
		{"index 直接命中", populatedFS(), "/index.html", http.StatusOK, "<title>astral</title>"},
		{"哈希资产命中", populatedFS(), "/assets/app-1a2b.js", http.StatusOK, "console.log('app')"},
		{"SPA 深链回退壳", populatedFS(), "/workspaces/ws_1/tasks", http.StatusOK, "<title>astral</title>"},
		{"尾斜杠深链回退壳", populatedFS(), "/login/", http.StatusOK, "<title>astral</title>"},
		{"目录路径回退壳", populatedFS(), "/assets", http.StatusOK, "<title>astral</title>"},
		{"越界路径不逃逸回退壳", populatedFS(), "/../../etc/passwd", http.StatusOK, "<title>astral</title>"},
		{"资产缺失回退壳", populatedFS(), "/assets/missing-9z.js", http.StatusOK, "<title>astral</title>"},
		{"无壳时根路径 404", fstest.MapFS{".gitkeep": {}}, "/", http.StatusNotFound, ""},
		{"无壳时深路径 404", fstest.MapFS{".gitkeep": {}}, "/login", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			HandlerFS(tt.fsys).ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body %q does not contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestHandlerFSContentType(t *testing.T) {
	fsys := populatedFS()

	rec := httptest.NewRecorder()
	HandlerFS(fsys).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("fallback Content-Type = %q, want text/html; charset=utf-8", ct)
	}

	rec = httptest.NewRecorder()
	HandlerFS(fsys).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app-1a2b.js", nil))
	// .js 的精确值随平台 mime 来源漂移（Windows 注册表 application/javascript，
	// 内建表 text/javascript），只断言脚本类型。
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset Content-Type = %q, want *javascript*", ct)
	}
}
