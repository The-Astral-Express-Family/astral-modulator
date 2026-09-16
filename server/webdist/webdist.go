// Package webdist 嵌入 Web 前端构建产物并提供同源静态托管（TODO.md S6-1）。
//
// dist 目录在仓库内只有 .gitkeep 占位（go:embed 不支持空目录，点前缀文件
// 需 all: 前缀才会嵌入）；`make web-dist`（server/Makefile）把 web/dist 构建
// 产物拷入后 Available() 为 true，router 把根路径 GET 交给 Handler()——
// 生产同源部署由此消除 CORS 依赖（开发期 Vite dev server 跨域直连仍走
// ASTRAL_DEV_CORS_ORIGINS 白名单，见 internal/httpx CORS 中间件注释）。
package webdist

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// distFS 是剥离 dist/ 前缀后的构建产物文件系统（index.html 位于根）。
// embed 模式含 .gitkeep，fs.Sub 不会失败；防御性允许错误（distFS 为 nil
// 时 Available 恒 false，router 不挂载静态托管）。
var distFS, _ = fs.Sub(embedded, "dist")

// Available 报告嵌入的构建产物是否含 index.html（即 web-dist 已执行且
// 产物已随编译嵌入）。
func Available() bool { return AvailableFS(distFS) }

// AvailableFS 是 fs 参数版（单测用小 fs 抽象；fsys 为 nil 时恒 false）。
func AvailableFS(fsys fs.FS) bool {
	if fsys == nil {
		return false
	}
	_, err := fs.Stat(fsys, "index.html")
	return err == nil
}

// Handler 返回嵌入前端的静态托管：命中的文件直接返回，未命中路径回退
// index.html（SPA 前端路由刷新与深链）。仅在 Available() 为 true 时挂载。
func Handler() http.Handler { return HandlerFS(distFS) }

// HandlerFS 是 fs 参数版（单测用 fstest.MapFS 驱动）。fsys 缺 index.html
// 时一切路径按 404 处理（没有壳可回退，绝不落到 panic）。
func HandlerFS(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// index.html 一律走 serveIndex：http.FileServer 对 */index.html 形态
		// 会 301 重定向到 ./，SPA 场景直接 200 输出壳更符合预期。
		if name := cleanName(r.URL.Path); name != "" && name != "index.html" {
			if stat, err := fs.Stat(fsys, name); err == nil && !stat.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, r, fsys)
	})
}

// serveIndex 输出 index.html（SPA fallback）。壳缺失时按 404 兜底。
func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	f, err := fsys.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// cleanName 把 URL 路径归一为 fs 相对名：去前导斜杠、压掉 . / .. 与重复段。
// 根路径与含越界 .. 的路径归一后若为空或仍以 .. 开头则返回空串（交给
// SPA fallback——fs.Stat 本就拒绝此类名字，双保险防目录逃逸）。
func cleanName(urlPath string) string {
	p := path.Clean(strings.TrimPrefix(urlPath, "/"))
	if p == "." || p == "/" || strings.HasPrefix(p, "../") || p == ".." {
		return ""
	}
	return p
}
