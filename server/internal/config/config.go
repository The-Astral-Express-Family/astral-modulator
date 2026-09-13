// Package config 汇集服务端运行配置。
//
// v0.1 以环境变量为准：cmd 入口启动时用 godotenv 预加载本地 .env（缺失可容忍，
// 已有环境变量优先，见 cmd/astral-server/main.go；模板 .env.example）。
// docs/deployment.md §Binary Deployment 提到 server.toml，属 phase-6 事项，
// 见 TODO.md。secrets 只允许来自环境变量/本地 .env（gitignore）/secret
// manager，严禁写进仓库。
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	// HTTPAddr 监听地址，默认 ":8080"。
	HTTPAddr string
	// PublicURL 即 canonical URL，写入 /.well-known/astral（architecture §7）。
	// 为空时 well-known 里回退为请求 Host，并打 warning。
	PublicURL string
	// WebBaseURL 是 device flow 验证链接（verification_uri[_complete]）指向的
	// web 控制台基址。生产同源部署留空即可（回退 PublicURL → 请求 Host）；
	// 开发期 web 与 API 端口分离（vite 5173 / server 8080）时必须显式配置，
	// 否则 CLI 拉起的链接会落到 API 端口上没有页面。
	WebBaseURL string
	// DatabaseDSN PostgreSQL 连接串。为空表示“无数据库的开发模式”：
	// 服务可启动、healthz 可用，但一切依赖存储的端点返回 NOT_IMPLEMENTED/INTERNAL_ERROR，
	// readyz 返回 503。
	DatabaseDSN string
	// ServerID 稳定服务器身份。为空时首启生成新 ID；首启后固化进
	// server_meta 表，之后以库中值为准（store.EnsureServerID），
	// 环境变量只作为首启注入。
	ServerID string
	// AutoMigrate 启动时执行 goose up（默认 true，方便开发）。
	// 生产多实例部署必须关闭，由部署流程显式执行 migration（deployment.md §14）。
	AutoMigrate bool
	// DevCORSOrigins 开发期允许的浏览器来源（如 http://localhost:5173）。
	// 生产同源部署应为空。
	DevCORSOrigins []string
	// LogLevel slog 级别，默认 info。
	LogLevel slog.Level
}

// Load 从环境变量读取配置。ASTRAL_DATABASE_DSN 缺省时回退读 DATABASE_URL
// （deployment.md 使用该名）。
func Load() Config {
	cfg := Config{
		HTTPAddr:       env("ASTRAL_HTTP_ADDR", ":8080"),
		PublicURL:      strings.TrimRight(os.Getenv("ASTRAL_PUBLIC_URL"), "/"),
		WebBaseURL:     strings.TrimRight(os.Getenv("ASTRAL_WEB_BASE_URL"), "/"),
		DatabaseDSN:    getEnvDefault("ASTRAL_DATABASE_DSN", "DATABASE_URL"),
		ServerID:       os.Getenv("ASTRAL_SERVER_ID"),
		AutoMigrate:    envBool("ASTRAL_AUTO_MIGRATE", true),
		DevCORSOrigins: splitCSV(os.Getenv("ASTRAL_DEV_CORS_ORIGINS")),
	}
	switch strings.ToLower(os.Getenv("ASTRAL_LOG_LEVEL")) {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		cfg.LogLevel = slog.LevelInfo
	}
	return cfg
}

// Describe 返回脱敏摘要（不含 DSN），用于启动日志。
func (c Config) Describe() string {
	db := "disabled"
	if c.DatabaseDSN != "" {
		db = "postgres"
	}
	return fmt.Sprintf("addr=%s public_url=%s web_base_url=%s db=%s server_id=%s auto_migrate=%v cors_origins=%v",
		c.HTTPAddr, c.PublicURL, c.WebBaseURL, db, c.ServerID, c.AutoMigrate, c.DevCORSOrigins)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvDefault(primary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	return os.Getenv(fallback)
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return def
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
