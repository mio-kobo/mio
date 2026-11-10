package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lintFile(t *testing.T, source string, args ...string) (int, string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "プロジェクト dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.mio")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := runLint(append(args, path), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestLintReportsWarningsAndExitCodes(t *testing.T) {
	source := "waza f() -> int64 {\n    kaku unused = 1;\n    kaesu 0;\n}\nchant(f());\n"
	status, stdout, stderr := lintFile(t, source)
	if status != 0 || stderr != "" || !strings.Contains(stdout, ":2:10: warning[unused-variable]") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if status, _, _ := lintFile(t, source, "--deny-warnings"); status != 1 {
		t.Fatalf("--deny-warnings status = %d", status)
	}
	if status, stdout, _ := lintFile(t, "chant(1);\n", "--deny-warnings"); status != 0 || stdout != "" {
		t.Fatalf("clean file status=%d stdout=%q", status, stdout)
	}
}

func TestLintJSON(t *testing.T) {
	_, stdout, _ := lintFile(t, "moshi (hai) { chant(1); }\n", "--format=json")
	var findings []map[string]any
	if err := json.Unmarshal([]byte(stdout), &findings); err != nil || len(findings) != 1 || findings[0]["rule"] != "constant-condition" {
		t.Fatalf("json = %q (%v)", stdout, err)
	}
	_, stdout, _ = lintFile(t, "chant(1);\n", "--format=json")
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("empty result must be [], got %q", stdout)
	}
}

func TestLintCompilerErrorIsNotAWarning(t *testing.T) {
	status, stdout, stderr := lintFile(t, "kaku x = ;\n")
	if status != 2 || stdout != "" || stderr == "" {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestLintUsageAndRuleList(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := runLint([]string{"--list-rules"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "unreachable-code") {
		t.Fatalf("list-rules status=%d out=%q", status, stdout.String())
	}
	for _, args := range [][]string{{}, {"--bogus", "x.mio"}, {"a.mio", "b.mio"}} {
		stderr.Reset()
		if status := runLint(args, &stdout, &stderr); status != 2 || !strings.Contains(stderr.String(), "Usage") {
			t.Fatalf("args %v status=%d", args, status)
		}
	}
	var out bytes.Buffer
	if status := runCLI([]string{"lint", "--help"}, &out, &stderr); status != 0 || !strings.Contains(out.String(), "mio lint") {
		t.Fatalf("dispatch status=%d", status)
	}
}
