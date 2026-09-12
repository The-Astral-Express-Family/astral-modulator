package bootstrap

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// UI 是向导的终端交互原语。In/Out 抽象为 io 便于单测与管道（非 TTY）驱动；
// 密码输入在 TTY 下经 x/term 关闭回显，非 TTY 降级为明文读取并警告。
type UI struct {
	In  *bufio.Reader
	Out io.Writer

	// readSecret 读一行密码（不回显）；NewUI 按 stdin 是否 TTY 注入实现，测试可替换。
	readSecret func() (string, error)
}

// errAborted 表示输入流结束（EOF），调用方以此区分“用户中止”。
var errAborted = errors.New("aborted")

func NewUI(in io.Reader, out io.Writer) *UI {
	ui := &UI{In: bufio.NewReader(in), Out: out}
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		ui.readSecret = func() (string, error) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(ui.Out) // ReadPassword 不回显换行，补齐提示行
			return string(b), err
		}
	} else {
		ui.readSecret = func() (string, error) {
			fmt.Fprintln(ui.Out, "  （当前非 TTY，密码将以明文回显）")
			return ui.readLine()
		}
	}
	return ui
}

func (u *UI) readLine() (string, error) {
	line, err := u.In.ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return "", errAborted
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// AskString 提示输入一行；空输入（直接回车）接受默认值 def。
func (u *UI) AskString(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(u.Out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(u.Out, "%s: ", label)
	}
	s, err := u.readLine()
	if err != nil {
		return "", err
	}
	if s = strings.TrimSpace(s); s == "" {
		return def, nil
	}
	return s, nil
}

// AskValid 循环提问直到 validate 通过；空输入接受默认值（默认值应已合法）。
func (u *UI) AskValid(label, def string, validate func(string) error) (string, error) {
	for {
		v, err := u.AskString(label, def)
		if err != nil {
			return "", err
		}
		if err := validate(v); err == nil {
			return v, nil
		}
		fmt.Fprintf(u.Out, "  ✗ %v\n", err)
	}
}

// AskBool 是 y/n 提示；空输入接受默认值，方括号内大写一侧即默认。
func (u *UI) AskBool(label string, def bool) (bool, error) {
	suffix := "y/N"
	if def {
		suffix = "Y/n"
	}
	for {
		fmt.Fprintf(u.Out, "%s [%s]: ", label, suffix)
		s, err := u.readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(u.Out, "  ✗ 请输入 y 或 n")
	}
}

// AskSelect 编号单选；空输入接受默认项。
func (u *UI) AskSelect(label string, options []string, defIdx int) (string, error) {
	def := options[defIdx]
	for i, o := range options {
		marker := " "
		if i == defIdx {
			marker = ">"
		}
		fmt.Fprintf(u.Out, "  %s %d) %s\n", marker, i+1, o)
	}
	for {
		fmt.Fprintf(u.Out, "%s [%s]: ", label, def)
		s, err := u.readLine()
		if err != nil {
			return "", err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(options) {
			return options[n-1], nil
		}
		fmt.Fprintln(u.Out, "  ✗ 无效选项")
	}
}

// AskSecret 读密码：TTY 下掩码，非 TTY 降级明文（由 NewUI 决定）。
func (u *UI) AskSecret(label string) (string, error) {
	fmt.Fprintf(u.Out, "%s: ", label)
	return u.readSecret()
}
