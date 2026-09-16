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
	"strconv"
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
	// 服务可启动、healthz 可用，但依赖存储的端点不可用（鉴权依赖缺失，
	// 受保护端点 401），readyz 返回 503。
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
	// RateLimit 进程内限流桶配置（TODO.md S5 / docs/protocol.md §6）。
	// Load 填充生产默认值；直接构造 Config 零值 = 四桶全禁用（集成测试
	// 装配零值即整体免限流，避免打敏感端点被 10/min 桶限死）。
	RateLimit RateLimitConfig
	// TrustedProxy 为 true 时机限流 key 采信 X-Forwarded-For 首跳
	//（ASTRAL_TRUSTED_PROXY，默认 false = 直连 RemoteAddr）。仅作用于
	// 限流 key 解析；见 docs/deployment.md §1。
	TrustedProxy bool
}

// RateLimitConfig 是四类限流桶的每分钟令牌数（internal/ratelimit：
// 容量 = 每分钟令牌数，匀速回填；某项 <=0 表示禁用对应桶——显式设
// ASTRAL_RATELIMIT_*=0 或零值构造）。
type RateLimitConfig struct {
	// SensitivePerMin 敏感桶/min/IP：login、register、token/refresh、
	// device 授权面（含 GET authorizations 的 user_code 防枚举与 approve/deny）。
	SensitivePerMin int
	// PollPerMin 轮询桶/min/IP：device token 交换（CLI interval=3s 轮询必须容纳）。
	PollPerMin int
	// APIPerMin 通用桶/min/actor：其余 /api/v1。
	APIPerMin int
	// SSEPerMin SSE 桶/min/actor：events 连接建立（独立，重连风暴不占通用桶）。
	SSEPerMin int
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
		RateLimit: RateLimitConfig{
			SensitivePerMin: envInt("ASTRAL_RATELIMIT_SENSITIVE_PER_MIN", 10),
			PollPerMin:      envInt("ASTRAL_RATELIMIT_POLL_PER_MIN", 60),
			APIPerMin:       envInt("ASTRAL_RATELIMIT_API_PER_MIN", 300),
			SSEPerMin:       envInt("ASTRAL_RATELIMIT_SSE_PER_MIN", 30),
		},
		TrustedProxy: envBool("ASTRAL_TRUSTED_PROXY", false),
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
	return fmt.Sprintf("addr=%s public_url=%s web_base_url=%s db=%s server_id=%s auto_migrate=%v cors_origins=%v ratelimit=sensitive:%d/poll:%d/api:%d/sse:%d trusted_proxy=%v",
		c.HTTPAddr, c.PublicURL, c.WebBaseURL, db, c.ServerID, c.AutoMigrate, c.DevCORSOrigins,
		c.RateLimit.SensitivePerMin, c.RateLimit.PollPerMin, c.RateLimit.APIPerMin, c.RateLimit.SSEPerMin, c.TrustedProxy)
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

// envInt 读整数 env；缺失或非法值回退默认（限流桶额度等数值开关）。
// 显式设 "0" 生效（= 禁用对应桶），与 env 字符串非空判断一致。
func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
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
