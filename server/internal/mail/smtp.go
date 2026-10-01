package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// SMTPSender 经认证中继投递（QQ 邮箱等个人邮箱的 SMTP 服务即中继）。
// ImplicitTLS=true 走 smtps（465 隐式 TLS）；否则 smtps 587 模式：
// 明文连接后 STARTTLS 升级（升级失败即失败，绝不明文发凭证）。
type SMTPSender struct {
	Host        string
	Port        string
	Username    string
	Password    string
	From        string
	ImplicitTLS bool

	// dialTimeout 是建连/握手整体预算（含 TLS 握手），默认 15s。
	dialTimeout time.Duration
	// insecureTLS 仅供包内测试注入（自签证书的假服务器）；恒为 false。
	insecureTLS bool
}

// Send 同步投递一封邮件。错误原样上抛（调用方记日志，不重试——
// 重置链接/邀请码均有失效时间，投递失败由用户重新触发）。
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	timeout := s.dialTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addr := net.JoinHostPort(s.Host, s.Port)
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", addr, err)
	}
	deadline, _ := dialCtx.Deadline()
	_ = conn.SetDeadline(deadline)
	if s.ImplicitTLS {
		conn = tls.Client(conn, s.tlsConfig())
	}
	cli, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail: greeting %s: %w", addr, err)
	}
	defer cli.Close()

	if !s.ImplicitTLS {
		if ok, _ := cli.Extension("STARTTLS"); !ok {
			return fmt.Errorf("mail: %s does not offer STARTTLS; use smtps:// (port 465) instead", addr)
		}
		if err := cli.StartTLS(s.tlsConfig()); err != nil {
			return fmt.Errorf("mail: STARTTLS %s: %w", addr, err)
		}
	}
	if s.Password != "" {
		if err := cli.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: auth %s: %w", addr, err)
		}
	}
	if err := cli.Mail(s.From); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := cli.Rcpt(msg.To); err != nil {
		return fmt.Errorf("mail: RCPT TO %s: %w", msg.To, err)
	}
	w, err := cli.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(buildMessage(s.From, msg)); err != nil {
		_ = w.Close()
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: close body: %w", err)
	}
	return cli.Quit()
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         s.Host,
		InsecureSkipVerify: s.insecureTLS, //nolint:gosec // 仅测试注入
	}
}
