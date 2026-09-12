package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEnvFileCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	bak, err := WriteEnvFile(path, EnvValues{
		HTTPAddr:    ":8080",
		DatabaseDSN: "postgres://astral:astral@localhost:5433/astral?sslmode=disable",
		ServerID:    "srv_test",
		AutoMigrate: true,
		LogLevel:    "debug",
	})
	if err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	if bak {
		t.Fatal("全新文件不应生成备份")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ASTRAL_HTTP_ADDR=:8080",
		"ASTRAL_DATABASE_DSN=postgres://astral:astral@localhost:5433/astral?sslmode=disable",
		"ASTRAL_SERVER_ID=srv_test",
		"ASTRAL_AUTO_MIGRATE=true",
		"ASTRAL_LOG_LEVEL=debug",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("缺少 %q；实际内容：\n%s", want, got)
		}
	}
}

func TestWriteEnvFileBacksUpAndPreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	orig := "ASTRAL_HTTP_ADDR=:9090\nCUSTOM_KEY=hello\nASTRAL_LOG_LEVEL=warn\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	bak, err := WriteEnvFile(path, EnvValues{HTTPAddr: ":8080", AutoMigrate: false})
	if err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	if !bak {
		t.Fatal("已有文件应生成备份")
	}
	bakData, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bakData) != orig {
		t.Errorf("备份应与原文件逐字节一致；got %q", bakData)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "ASTRAL_HTTP_ADDR=:8080") {
		t.Errorf("已知键应更新为向导值；实际：\n%s", s)
	}
	if !strings.Contains(s, "CUSTOM_KEY=hello") {
		t.Errorf("未知键应原样保留；实际：\n%s", s)
	}
	if strings.Contains(s, "ASTRAL_LOG_LEVEL=warn") && strings.Contains(s, "ASTRAL_LOG_LEVEL=") &&
		!strings.Contains(s, "#ASTRAL_LOG_LEVEL=") {
		// 已知键旧值不应以未注释形式残留（renderEnv 固定写出注释态 info）。
		t.Errorf("已知键旧值应被向导值覆盖；实际：\n%s", s)
	}
	if !strings.Contains(s, "ASTRAL_AUTO_MIGRATE=false") {
		t.Errorf("布尔值应写出 false；实际：\n%s", s)
	}
}

func TestRenderEnvCommentsEmptyOptionals(t *testing.T) {
	s := renderEnv(EnvValues{}, map[string]string{})
	for _, want := range []string{
		"#ASTRAL_PUBLIC_URL=",
		"#ASTRAL_DATABASE_DSN=",
		"#ASTRAL_SERVER_ID=",
		"#ASTRAL_DEV_CORS_ORIGINS=",
		"#ASTRAL_LOG_LEVEL=",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("空可选项应注释写出 %q；实际：\n%s", want, s)
		}
	}
}

func TestQuoteEnvValue(t *testing.T) {
	cases := map[string]string{
		"plain":      "plain",
		"a b":        `"a b"`,
		"with#hash":  `"with#hash"`,
		`with"quote`: `"with\"quote"`,
	}
	for in, want := range cases {
		if got := quoteEnvValue(in); got != want {
			t.Errorf("quoteEnvValue(%q) = %q, want %q", in, got, want)
		}
	}
}
