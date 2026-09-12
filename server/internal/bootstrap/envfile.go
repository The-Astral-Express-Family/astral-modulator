package bootstrap

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// envFile 是向导读写的 .env 文件名（相对 CWD，与 godotenv.Load / 服务端
// 启动语义一致：向导应在 server/ 目录下运行）。
const envFile = ".env"

// envKnownKeys 是向导管理的键集合；原 .env 中不在此列的键视为用户自定义，
// 写回时原样保留，避免覆盖手写配置。
var envKnownKeys = map[string]bool{
	"ASTRAL_HTTP_ADDR":        true,
	"ASTRAL_PUBLIC_URL":       true,
	"ASTRAL_DATABASE_DSN":     true,
	"ASTRAL_SERVER_ID":        true,
	"ASTRAL_AUTO_MIGRATE":     true,
	"ASTRAL_DEV_CORS_ORIGINS": true,
	"ASTRAL_LOG_LEVEL":        true,
}

// EnvValues 承载向导收集的配置，即写回 .env 的目标值。
type EnvValues struct {
	HTTPAddr       string
	PublicURL      string
	DatabaseDSN    string // 空 = 桩模式（写出时注释掉）
	ServerID       string
	AutoMigrate    bool
	DevCORSOrigins string // 已合并为逗号分隔串
	LogLevel       string // info 时注释掉（与 config 默认一致）
}

// WriteEnvFile 写 CWD 下 .env：已有文件先整字节备份为 .env.bak，再合并写回 ——
// 已知键取向导值，未知已有键追加保留。返回是否生成了备份。
func WriteEnvFile(path string, v EnvValues) (backedUp bool, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, readErr
	}
	prev := map[string]string{}
	if readErr == nil {
		prev, _ = godotenv.Parse(bytes.NewReader(data))
		if err := os.WriteFile(path+".bak", data, 0o600); err != nil {
			return false, err
		}
		backedUp = true
	}
	return backedUp, os.WriteFile(path, []byte(renderEnv(v, prev)), 0o600)
}

// renderEnv 生成带注释的规范 .env 内容；空的可选项注释掉以传达默认语义。
func renderEnv(v EnvValues, prev map[string]string) string {
	var b strings.Builder
	b.WriteString("# astral-server 配置（由 cmd/astral-bootstrap 生成）。\n")
	b.WriteString("# 变量说明见 server/internal/config/config.go 与 README「主要环境变量」；\n")
	b.WriteString("# 本文件可能含凭证，已被 .gitignore 忽略，严禁提交。\n\n")

	writeKV(&b, "ASTRAL_HTTP_ADDR", v.HTTPAddr, "HTTP 监听地址", false)
	writeKV(&b, "ASTRAL_PUBLIC_URL", v.PublicURL, "canonical URL，写入 /.well-known/astral；留空回退请求 Host", true)
	if v.DatabaseDSN != "" {
		writeKV(&b, "ASTRAL_DATABASE_DSN", v.DatabaseDSN, "PostgreSQL 连接串", false)
	} else {
		b.WriteString("# PostgreSQL 连接串（当前为桩模式：受保护端点 401 / readyz 503）\n")
		b.WriteString("#ASTRAL_DATABASE_DSN=\n\n")
	}
	writeKV(&b, "ASTRAL_SERVER_ID", v.ServerID, "稳定服务器身份；仅首启生效，之后以 server_meta 表中值为准", true)
	writeKV(&b, "ASTRAL_AUTO_MIGRATE", strconv.FormatBool(v.AutoMigrate), "启动时自动 goose up（生产多实例部署必须关闭）", false)
	writeKV(&b, "ASTRAL_DEV_CORS_ORIGINS", v.DevCORSOrigins, "开发期浏览器跨域白名单（逗号分隔）；同源部署留空", true)
	if v.LogLevel == "" || v.LogLevel == "info" {
		writeKV(&b, "ASTRAL_LOG_LEVEL", "", "日志级别：debug / info（默认）/ warn / error", true)
	} else {
		writeKV(&b, "ASTRAL_LOG_LEVEL", v.LogLevel, "日志级别：debug / info（默认）/ warn / error", false)
	}

	var extras []string
	for k := range prev {
		if !envKnownKeys[k] {
			extras = append(extras, k)
		}
	}
	if len(extras) > 0 {
		sort.Strings(extras)
		b.WriteString("# 以下键非向导管理，自原文件原样保留\n")
		for _, k := range extras {
			b.WriteString(k + "=" + quoteEnvValue(prev[k]) + "\n")
		}
	}
	return b.String()
}

func writeKV(b *strings.Builder, key, value, comment string, commentWhenEmpty bool) {
	fmt.Fprintf(b, "# %s\n", comment)
	if value == "" && commentWhenEmpty {
		fmt.Fprintf(b, "#%s=\n\n", key)
		return
	}
	fmt.Fprintf(b, "%s=%s\n\n", key, quoteEnvValue(value))
}

// quoteEnvValue 在值含空白/#/引号时加双引号转义，避免 godotenv 解析歧义。
func quoteEnvValue(v string) string {
	if strings.ContainsAny(v, " \t\r\n#'\"") {
		return strconv.Quote(v)
	}
	return v
}
