package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stressTestEnabled 判断是否启用专项压力测试
func stressTestEnabled() bool {
	return os.Getenv("MIO_STRESS_TEST") == "1"
}

// skipInShortMode 跳过日常测试中的高负载用例
func skipInShortMode(t *testing.T, reason string) {
	t.Helper()
	if testing.Short() && !stressTestEnabled() {
		t.Skip(reason)
	}
}

// 测试 Mio 命令行工具
func TestCLI(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		source   string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{
			name:     "静态检查通过",
			command:  "check",
			source:   "kaku age = 18; age = 20;",
			wantCode: 0,
			wantOut:  "check passed",
		},
		{
			name:     "静态类型检查失败",
			command:  "check",
			source:   `kaku age = 18; age = "Mio";`,
			wantCode: 1,
			wantErr:  "cannot assign string",
		},
		{
			name:     "执行整数表达式",
			command:  "run",
			source:   "1 + 2 * 3;",
			wantCode: 0,
			wantOut:  "7\n",
		},
		{
			name:    "执行全局变量程序",
			command: "run",
			source: `
kaku age = 18;
age = age + 1;
age;
`,
			wantCode: 0,
			wantOut:  "19\n",
		},
		{
			name:     "执行布尔表达式",
			command:  "run",
			source:   "!hai;",
			wantCode: 0,
			wantOut:  "iie\n",
		},
		{
			name:     "执行 unit 用户函数",
			command:  "run",
			source:   `f(); waza f() -> unit { kaesu; }`,
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 为每个测试创建临时 Mio 文件
			path := filepath.Join(
				t.TempDir(),
				"test.mio",
			)

			if err := os.WriteFile(
				path,
				[]byte(tt.source),
				0644,
			); err != nil {
				t.Fatal(err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := runCLI(
				[]string{tt.command, path},
				&stdout,
				&stderr,
			)

			if code != tt.wantCode {
				t.Fatalf(
					"exit code: got %d, want %d, stderr=%q",
					code,
					tt.wantCode,
					stderr.String(),
				)
			}

			if tt.wantOut != "" &&
				!strings.Contains(stdout.String(), tt.wantOut) {
				t.Fatalf(
					"stdout: got %q, want %q",
					stdout.String(),
					tt.wantOut,
				)
			}

			if tt.wantErr != "" &&
				!strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf(
					"stderr: got %q, expected %q",
					stderr.String(),
					tt.wantErr,
				)
			}
		})
	}
}

// 测试版本命令
func TestCLIVersion(t *testing.T) {
	const want = "Mio 1.0.0-beta.1\n"
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runCLI(
		[]string{"version"},
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf("unexpected exit code %d", code)
	}

	if stdout.String() != want {
		t.Fatalf(
			"expected version output %q, got %q",
			want,
			stdout.String(),
		)
	}
}

// 测试帮助信息显示当前版本和全部命令
func TestCLIHelpShowsCurrentVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runCLI([]string{"help"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{
		"Mio " + version + "\n",
		"mio check <file.mio>",
		"mio run <file.mio>",
		"mio build [-o <file.mochi>] <file.mio>",
		"mio exec <file.mochi>",
		"mio disasm <file.mochi>",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help output missing %q:\n%s", want, stdout.String())
		}
	}
}

// 测试缺少命令参数
func TestCLIMissingArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runCLI(
		nil,
		&stdout,
		&stderr,
	)

	if code != 2 {
		t.Fatalf("got exit code %d, want 2", code)
	}

	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatal("expected usage information")
	}
}
