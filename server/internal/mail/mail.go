// Package mail 是出站邮件的最小投递封装。消费方：忘记密码重置链接
// （auth 模块）、工作区邀请投递（workspace 模块）。
//
// 配置面只有一个可选环境变量 ASTRAL_SMTP_URL（连接串，见 SenderFromURL）；
// 未配置 = log transport（邮件全文写服务器日志，自托管小部署的零成本兜底，
// 管理员从 podman/journald 日志捞一次性链接）。纯出站连接，部署无需开任何
// 入站端口（docs/deployment.md §邮件）。
//
// 不引第三方依赖：net/smtp + crypto/tls 足够（无附件、无模板引擎需求）。
package mail

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"strings"
)

// Message 是一封出站邮件。Text/HTML 同内容双格式（multipart/alternative，
// 客户端自选）；Subject 允许中文（组装时 RFC 2047 编码）。
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender 是投递抽象（同步尽力：失败返回 error，由调用方决定记日志还是上抛；
// 重置请求恒 204、邀请签发恒 201，均不因投递失败回滚业务事实）。测试注入
// fake 实现即可，不触网络。
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender 把邮件全文打进日志（ASTRAL_SMTP_URL 未配置时的默认 transport）。
// 注意：正文包含一次性链接，log transport 下日志即凭据——生产部署应配置
// SMTP（deployment.md 的安全注记）。
type LogSender struct {
	Log *slog.Logger
}

func (s *LogSender) Send(_ context.Context, msg Message) error {
	s.Log.Info("outbound mail (log transport; configure ASTRAL_SMTP_URL to send for real)",
		"to", msg.To, "subject", msg.Subject, "body", msg.Text)
	return nil
}

// OrLog 是装配期的 nil 兜底：s 为 nil（测试/桩模式未注入）时退回 LogSender。
func OrLog(s Sender, log *slog.Logger) Sender {
	if s != nil {
		return s
	}
	return &LogSender{Log: log}
}

// SenderFromURL 按连接串构造 sender，返回脱敏描述（启动日志用，不含凭据）。
// raw 为空 → LogSender。格式：
//
//	smtps://user:pass@smtp.qq.com:465    隐式 TLS（端口 465；缺省端口 465）
//	smtp://user:pass@smtp.example.com    STARTTLS（缺省端口 587；必须能升级 TLS）
//	?from=sender@example.com             可选：覆盖发件人（缺省 = user）
//
// userinfo 密码含特殊字符需 URL 百分号编码（QQ 邮箱授权码为 16 位小写
// 字母数字，无需编码）。无 userinfo 的匿名投递（MX 直发）不支持——
// 自托管场景恒走认证中继，匿名直发只会进垃圾箱。
func SenderFromURL(raw string, log *slog.Logger) (Sender, string, error) {
	if strings.TrimSpace(raw) == "" {
		return &LogSender{Log: log}, "log", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse ASTRAL_SMTP_URL: %w", err)
	}
	var implicitTLS bool
	switch strings.ToLower(u.Scheme) {
	case "smtps":
		implicitTLS = true
	case "smtp":
		implicitTLS = false
	default:
		return nil, "", fmt.Errorf("ASTRAL_SMTP_URL: scheme must be smtp or smtps, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, "", fmt.Errorf("ASTRAL_SMTP_URL: missing host")
	}
	port := u.Port()
	if port == "" {
		if implicitTLS {
			port = "465"
		} else {
			port = "587"
		}
	}
	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	if username == "" {
		return nil, "", fmt.Errorf("ASTRAL_SMTP_URL: userinfo (username) is required")
	}
	from := u.Query().Get("from")
	if from == "" {
		from = username
	}
	s := &SMTPSender{
		Host:        u.Hostname(),
		Port:        port,
		Username:    username,
		Password:    password,
		From:        from,
		ImplicitTLS: implicitTLS,
	}
	return s, "smtp:" + u.Hostname() + ":" + port + " (from " + from + ")", nil
}

// boundary 是 multipart 分隔符。刻意含 '-'：标准 base64 字母表不含 '-'，
// 正文以 base64 编码，分隔符不可能在正文中伪现。
const boundary = "=_astral-mail-1"

// buildMessage 组装 MIME 邮件（含 \r\n 行尾——net/smtp 的 Data writer 只做
// 点填充，不做行尾翻译）。Subject 用 B 编码承载 UTF-8（中文主题）。
func buildMessage(from string, msg Message) []byte {
	var b strings.Builder
	writeLine := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\r\n", args...)
	}
	writeLine("From: %s", from)
	writeLine("To: %s", msg.To)
	writeLine("Subject: %s", mime.BEncoding.Encode("utf-8", msg.Subject))
	writeLine("MIME-Version: 1.0")
	writeLine("Content-Type: multipart/alternative; boundary=\"%s\"", boundary)
	writeLine("")
	writeLine("--%s", boundary)
	writeLine("Content-Type: text/plain; charset=utf-8")
	writeLine("Content-Transfer-Encoding: base64")
	writeLine("")
	writeLine("%s", wrapBase64(msg.Text))
	if msg.HTML != "" {
		writeLine("--%s", boundary)
		writeLine("Content-Type: text/html; charset=utf-8")
		writeLine("Content-Transfer-Encoding: base64")
		writeLine("")
		writeLine("%s", wrapBase64(msg.HTML))
	}
	// 结束分隔符带尾随 "--"（RFC 2046：delimiter 后的 "--" 关闭整个 multipart）。
	writeLine("--%s--", boundary)
	return []byte(b.String())
}

// wrapBase64 编码并按 76 字符/行折行（RFC 2045）。
func wrapBase64(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var out strings.Builder
	for len(enc) > 76 {
		out.WriteString(enc[:76])
		out.WriteString("\r\n")
		enc = enc[76:]
	}
	out.WriteString(enc)
	return out.String()
}
