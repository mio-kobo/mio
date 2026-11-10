package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanLocaleEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"MIO_LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "CI", "COLORTERM", "COLORFGBG", "MIO_THEME", "COLUMNS"} {
		t.Setenv(name, "")
	}
}

func TestGlobalHelpFollowsLanguageBeforeOrAfterCommand(t *testing.T) {
	cleanLocaleEnvironment(t)
	for _, args := range [][]string{
		{"--lang", "ja-JP", "help"},
		{"help", "--lang=ja-JP"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("args=%v exit=%d stderr=%q", args, code, stderr.String())
		}
		for _, want := range []string{"使用方法", "コマンド", "オプション", "例"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("args=%v missing %q:\n%s", args, want, stdout.String())
			}
		}
		if strings.ContainsRune(stdout.String(), '\x1b') {
			t.Fatalf("non-TTY help contains ANSI: %q", stdout.String())
		}
	}
}

func TestSubcommandHelpIsDistinctAndLocalized(t *testing.T) {
	cleanLocaleEnvironment(t)
	var build, test bytes.Buffer
	if code := runCLI([]string{"--lang", "zh-CN", "help", "build"}, &build, &bytes.Buffer{}); code != 0 {
		t.Fatalf("build help exit=%d", code)
	}
	if code := runCLI([]string{"--lang", "zh-CN", "help", "test"}, &test, &bytes.Buffer{}); code != 0 {
		t.Fatalf("test help exit=%d", code)
	}
	if build.String() == test.String() {
		t.Fatal("build and test help pages are identical")
	}
	if !strings.Contains(build.String(), "mio build <source>") || !strings.Contains(build.String(), "从 Mio 源码构建原生可执行文件") {
		t.Fatalf("build help = %q", build.String())
	}
	if !strings.Contains(test.String(), "mio test <directory") || !strings.Contains(test.String(), "--run=<regexp>") {
		t.Fatalf("test help = %q", test.String())
	}
}

func TestUnknownHelpCommand(t *testing.T) {
	cleanLocaleEnvironment(t)
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"--lang", "fr-FR", "help", "nope"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr.String(), "E_COMMAND") || !strings.Contains(stderr.String(), "commande inconnue") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if strings.ContainsRune(stderr.String(), '\x1b') {
		t.Fatalf("stderr contains ANSI: %q", stderr.String())
	}
}

func TestHelpThemeAndColorPrecedence(t *testing.T) {
	cleanLocaleEnvironment(t)
	t.Setenv("COLORTERM", "truecolor")
	var dark, light bytes.Buffer
	if code := runCLI([]string{"--color=always", "--theme=dark", "help"}, &dark, &bytes.Buffer{}); code != 0 {
		t.Fatalf("dark exit=%d", code)
	}
	if code := runCLI([]string{"--color=always", "--theme=light", "help"}, &light, &bytes.Buffer{}); code != 0 {
		t.Fatalf("light exit=%d", code)
	}
	if !strings.Contains(dark.String(), "\x1b[") || !strings.Contains(light.String(), "\x1b[") {
		t.Fatal("explicit themes did not color help")
	}
	if dark.String() == light.String() {
		t.Fatal("dark and light palettes produced identical output")
	}
	t.Setenv("NO_COLOR", "1")
	var forced bytes.Buffer
	if code := runCLI([]string{"--color=always", "help"}, &forced, &bytes.Buffer{}); code != 0 {
		t.Fatalf("forced exit=%d", code)
	}
	if !strings.Contains(forced.String(), "\x1b[") {
		t.Fatal("--color=always did not override NO_COLOR")
	}
	var plain bytes.Buffer
	if code := runCLI([]string{"--color=never", "help"}, &plain, &bytes.Buffer{}); code != 0 {
		t.Fatalf("never exit=%d", code)
	}
	if strings.ContainsRune(plain.String(), '\x1b') {
		t.Fatal("--color=never emitted ANSI")
	}
}

func TestHelpNarrowTerminalWraps(t *testing.T) {
	cleanLocaleEnvironment(t)
	t.Setenv("COLUMNS", "40")
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"--lang", "fr-FR", "help", "build"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "N'") && helpWidth(line) > 44 {
			t.Fatalf("wrapped description line is too wide (%d): %q", helpWidth(line), line)
		}
	}
	if !strings.Contains(stdout.String(), "\n    ") {
		t.Fatalf("narrow help did not use stacked layout:\n%s", stdout.String())
	}
}

func TestGlobalOptionsRespectDoubleDash(t *testing.T) {
	result := parseGlobalOptions([]string{"run", "main.mio", "--", "--lang", "ja-JP", "--theme=dark"})
	if result.language != "" || result.theme != "" || result.errKey != "" {
		t.Fatalf("options after -- were consumed: %+v", result)
	}
	want := []string{"run", "main.mio", "--", "--lang", "ja-JP", "--theme=dark"}
	if strings.Join(result.args, "|") != strings.Join(want, "|") {
		t.Fatalf("args=%v want %v", result.args, want)
	}
}

func TestInvalidGlobalOptions(t *testing.T) {
	cleanLocaleEnvironment(t)
	for _, args := range [][]string{
		{"--lang=", "help"},
		{"--theme=purple", "help"},
		{"--color=sometimes", "help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 2 {
			t.Fatalf("args=%v exit=%d stderr=%q", args, code, stderr.String())
		}
		if !strings.HasPrefix(stderr.String(), "E_OPTIONS ") {
			t.Fatalf("args=%v stderr=%q", args, stderr.String())
		}
	}
}

func TestJSONOutputNeverContainsLocalizedTextOrANSI(t *testing.T) {
	cleanLocaleEnvironment(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "bad.mio")
	if err := os.WriteFile(source, []byte("chant(missing_symbol);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--lang", "ja-JP", "--color=always", "check", source, "--format=json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("invalid source was accepted")
	}
	if strings.ContainsRune(stderr.String(), '\x1b') {
		t.Fatalf("JSON diagnostics contain ANSI: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "\"code\":") || !strings.Contains(stderr.String(), "\"severity\":\"error\"") {
		t.Fatalf("stderr is not a JSON diagnostic: %q", stderr.String())
	}
}

func TestLocalizedDevelopmentCommands(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"help", "fmt"}, "Formater les fichiers source Mio"},
		{[]string{"fmt", "--help"}, "Formater les fichiers source Mio"},
		{[]string{"help", "lint"}, "Rechercher les problèmes courants"},
		{[]string{"lint", "--list-rules"}, "unused-import"},
		{[]string{"lsp", "--help"}, "Utilisation: mio lsp"},
	} {
		var stdout, stderr bytes.Buffer
		if status := runCLI(append([]string{"--lang=fr-FR"}, test.args...), &stdout, &stderr); status != 0 ||
			!strings.Contains(stdout.String(), test.want) || stderr.Len() != 0 {
			t.Errorf("%v: status %d, stdout %q, stderr %q", test.args, status, stdout.String(), stderr.String())
		}
	}
}

func TestLocalizedLintPreservesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.mio")
	if err := os.WriteFile(path, []byte("waza example() -> unit { kaku unused = 1; }"), 0600); err != nil {
		t.Fatal(err)
	}
	var baseline string
	for _, language := range []string{"en-US", "fr-FR"} {
		var stdout, stderr bytes.Buffer
		args := []string{"--lang=" + language, "lint", "--deny-warnings", path}
		if status := runCLI(append(args, "--format=json"), &stdout, &stderr); status != 1 || stderr.Len() != 0 {
			t.Fatalf("%s JSON: status %d, stdout %q, stderr %q", language, status, stdout.String(), stderr.String())
		}
		if baseline == "" {
			baseline = stdout.String()
		} else if baseline != stdout.String() {
			t.Fatalf("lint JSON changed with locale: %q / %q", baseline, stdout.String())
		}
		stdout.Reset()
		if status := runCLI(args, &stdout, &stderr); status != 1 || stderr.Len() != 0 {
			t.Fatalf("%s text: status %d, stdout %q, stderr %q", language, status, stdout.String(), stderr.String())
		}
		if language == "fr-FR" && !strings.Contains(stdout.String(), "avertissement[unused-variable]: la variable \"unused\"") {
			t.Fatalf("unexpected French finding: %s", stdout.String())
		}
	}
}
