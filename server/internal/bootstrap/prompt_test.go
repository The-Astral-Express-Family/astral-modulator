package bootstrap

import (
	"errors"
	"strings"
	"testing"
)

// 用 strings.Reader 驱动 UI（非 TTY 路径），验证提示原语的行为契约。
func TestAskStringDefaultAndInput(t *testing.T) {
	var out strings.Builder
	ui := NewUI(strings.NewReader("\noverride\n"), &out)

	got, err := ui.AskString("问题", "默认值")
	if err != nil || got != "默认值" {
		t.Errorf("空输入应取默认：got %q, err %v", got, err)
	}
	got, err = ui.AskString("问题", "默认值")
	if err != nil || got != "override" {
		t.Errorf("非空输入应原样返回：got %q, err %v", got, err)
	}
}

func TestAskStringEOFAborts(t *testing.T) {
	var out strings.Builder
	ui := NewUI(strings.NewReader(""), &out)
	if _, err := ui.AskString("问题", "d"); !errors.Is(err, errAborted) {
		t.Errorf("EOF 应返回 errAborted，got %v", err)
	}
}

func TestAskBool(t *testing.T) {
	var out strings.Builder
	ui := NewUI(strings.NewReader("\nY\nno\nbad\nn\n"), &out)

	if v, _ := ui.AskBool("q", true); !v {
		t.Error("空输入应取默认 true")
	}
	if v, _ := ui.AskBool("q", false); !v {
		t.Error("Y 应返回 true")
	}
	if v, _ := ui.AskBool("q", true); v {
		t.Error("no 应返回 false")
	}
	// bad → 重问 → n
	if v, _ := ui.AskBool("q", true); v {
		t.Error("无效输入后重问，n 应返回 false")
	}
}

func TestAskSelect(t *testing.T) {
	var out strings.Builder
	ui := NewUI(strings.NewReader("\n3\nx\n2\n"), &out)
	opts := []string{"debug", "info", "warn", "error"}

	if v, _ := ui.AskSelect("级别", opts, 1); v != "info" {
		t.Errorf("空输入应取默认 info，got %q", v)
	}
	if v, _ := ui.AskSelect("级别", opts, 1); v != "warn" {
		t.Errorf("输入 3 应选 warn，got %q", v)
	}
	if v, _ := ui.AskSelect("级别", opts, 1); v != "info" {
		t.Errorf("无效输入 x 后重问，2 应选 info，got %q", v)
	}
}

func TestAskValidLoopsUntilValid(t *testing.T) {
	var out strings.Builder
	ui := NewUI(strings.NewReader("bad\nstill-bad\nok@x.com\n"), &out)

	v, err := ui.AskValid("邮箱", "", ValidateEmail)
	if err != nil || v != "ok@x.com" {
		t.Errorf("应循环到合法输入：got %q, err %v", v, err)
	}
	if !strings.Contains(out.String(), "✗") {
		t.Error("非法输入应有错误提示输出")
	}
}

func TestAskSecretNonTTYPlaintext(t *testing.T) {
	var out strings.Builder
	ui := NewUI(strings.NewReader("hunter2hunter\n"), &out)

	v, err := ui.AskSecret("密码")
	if err != nil || v != "hunter2hunter" {
		t.Errorf("非 TTY 应回退明文读取：got %q, err %v", v, err)
	}
	if !strings.Contains(out.String(), "明文") {
		t.Error("非 TTY 回退应有明文警告")
	}
}
