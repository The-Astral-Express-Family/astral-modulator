package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	msg := Message{
		To:      "u@example.com",
		Subject: "重置你的密码",
		Text:    "链接：https://x/reset?token=prt_abc",
		HTML:    "<p>链接：<a href=\"https://x/reset?token=prt_abc\">重置</a></p>",
	}
	raw := string(buildMessage("s@example.com", msg))

	for _, want := range []string{
		"From: s@example.com\r\n",
		"To: u@example.com\r\n",
		"Subject: =?utf-8?b?",
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("message missing %q:\n%s", want, raw)
		}
	}
	// 正文 base64 往返：文本段可解回原文。
	enc := base64.StdEncoding.EncodeToString([]byte(msg.Text))
	if !strings.Contains(raw, enc) {
		t.Fatalf("message missing base64 text body:\n%s", raw)
	}
	// multipart 结构：boundary 出现 3 次（文本段、HTML 段、结束符）。
	if n := strings.Count(raw, "--"+boundary); n != 3 {
		t.Fatalf("boundary appears %d times, want 3:\n%s", n, raw)
	}
	if !strings.HasSuffix(raw, "--"+boundary+"--\r\n") {
		t.Fatalf("message must end with closing boundary:\n%q", raw[len(raw)-40:])
	}
	// 行尾一律 CRLF（net/smtp 不做行尾翻译）。
	if strings.Contains(strings.ReplaceAll(raw, "\r\n", ""), "\n") {
		t.Fatal("bare LF found in message")
	}
}

func TestBuildMessageTextOnly(t *testing.T) {
	raw := string(buildMessage("s@example.com", Message{To: "u@example.com", Subject: "hi", Text: "body"}))
	if strings.Contains(raw, "text/html") {
		t.Fatalf("text-only message must not contain html part:\n%s", raw)
	}
}

func TestSenderFromURL(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s, desc, err := SenderFromURL("", log)
	_, isLog := s.(*LogSender)
	if err != nil || !isLog || desc != "log" {
		t.Fatalf("empty url must yield log sender, got %T %q err=%v", s, desc, err)
	}

	s, desc, err = SenderFromURL("smtps://bot%40qq.com:authcode@smtp.qq.com", log)
	if err != nil {
		t.Fatalf("smtps url: %v", err)
	}
	smtpS, ok := s.(*SMTPSender)
	if !ok {
		t.Fatalf("want *SMTPSender, got %T", s)
	}
	if smtpS.Host != "smtp.qq.com" || smtpS.Port != "465" || !smtpS.ImplicitTLS {
		t.Fatalf("smtps defaults wrong: %+v", smtpS)
	}
	// userinfo 百分号解码：bot%40qq.com → bot@qq.com（既是账号也是缺省发件人）。
	if smtpS.Username != "bot@qq.com" || smtpS.Password != "authcode" || smtpS.From != "bot@qq.com" {
		t.Fatalf("userinfo parse wrong: %+v", smtpS)
	}
	if !strings.Contains(desc, "smtp.qq.com:465") || strings.Contains(desc, "authcode") {
		t.Fatalf("description must be sanitized: %q", desc)
	}

	s, _, err = SenderFromURL("smtp://u@mx.example.com:2525?from=no-reply@example.com", log)
	if err != nil {
		t.Fatalf("smtp url: %v", err)
	}
	smtpS = s.(*SMTPSender)
	if smtpS.Port != "2525" || smtpS.ImplicitTLS || smtpS.From != "no-reply@example.com" {
		t.Fatalf("smtp explicit port/from wrong: %+v", smtpS)
	}

	for _, bad := range []string{
		"http://u@x", "smtp://", "smtp://host", "://x", "smtps://u:p@host:bad",
	} {
		if _, _, err := SenderFromURL(bad, log); err == nil {
			t.Fatalf("bad url %q must error", bad)
		}
	}
}

// fakeSMTP 起一个最小 TLS SMTP 服务器（自签证书），记录收到的会话。
type fakeSMTP struct {
	ln       net.Listener
	mailFrom string
	rcptTo   string
	auth     string
	data     string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeSMTP) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	write("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "EHLO"):
			write("250-fake")
			write("250 AUTH PLAIN")
		case strings.HasPrefix(line, "AUTH PLAIN "):
			if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN ")); err == nil {
				f.auth = string(b)
			}
			write("235 ok")
		case strings.HasPrefix(line, "MAIL FROM:"):
			f.mailFrom = strings.TrimSuffix(strings.TrimPrefix(line, "MAIL FROM:"), ">")
			write("250 ok")
		case strings.HasPrefix(line, "RCPT TO:"):
			f.rcptTo = strings.TrimSuffix(strings.TrimPrefix(line, "RCPT TO:"), ">")
			write("250 ok")
		case line == "DATA":
			write("354 go")
			var sb strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" {
					break
				}
				sb.WriteString(strings.TrimSuffix(dl, "\r\n") + "\n")
			}
			f.data = sb.String()
			write("250 queued")
		case line == "QUIT":
			write("221 bye")
			return
		default:
			write("250 ok")
		}
	}
}

func TestSMTPSenderSend(t *testing.T) {
	f := newFakeSMTP(t)
	addr := f.ln.Addr().String()
	host, port, _ := strings.Cut(addr, ":")

	s := &SMTPSender{
		Host: host, Port: port,
		Username: "bot@example.com", Password: "secret", From: "bot@example.com",
		ImplicitTLS: true, insecureTLS: true, dialTimeout: 5 * time.Second,
	}
	msg := Message{To: "user@example.com", Subject: "重置", Text: "hello"}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	// AUTH PLAIN 解码 = "\x00user\x00pass"。
	if want := "\x00bot@example.com\x00secret"; f.auth != want {
		t.Fatalf("auth = %q, want %q", f.auth, want)
	}
	if f.mailFrom != "<bot@example.com" || f.rcptTo != "<user@example.com" {
		t.Fatalf("envelope wrong: from=%q to=%q", f.mailFrom, f.rcptTo)
	}
	if !strings.Contains(f.data, "Subject: =?utf-8?b?") || !strings.Contains(f.data, "To: user@example.com") {
		t.Fatalf("data payload wrong:\n%s", f.data)
	}
}
