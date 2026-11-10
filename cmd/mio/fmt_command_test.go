package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runFmtCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := runFmt(args, strings.NewReader(stdin), &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func writeSource(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFmtRewritesInPlaceAndIsStable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my project 日本語")
	file := filepath.Join(dir, "main.mio")
	writeSource(t, file, "kaku x=1;\n")

	status, stdout, stderr := runFmtCLI(t, "", dir)
	if status != 0 || stderr != "" {
		t.Fatalf("status=%d stderr=%q", status, stderr)
	}
	if !strings.Contains(stdout, "main.mio") {
		t.Fatalf("rewritten file not listed: %q", stdout)
	}
	got, _ := os.ReadFile(file)
	if string(got) != "kaku x = 1;\n" {
		t.Fatalf("content = %q", got)
	}
	// Second run is a no-op.
	status, stdout, _ = runFmtCLI(t, "", dir)
	if status != 0 || stdout != "" {
		t.Fatalf("second run status=%d stdout=%q", status, stdout)
	}
}

func TestFmtCheckDoesNotWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.mio")
	writeSource(t, file, "kaku x=1;\n")
	status, stdout, _ := runFmtCLI(t, "", "--check", file)
	if status != 1 || !strings.Contains(stdout, "a.mio") {
		t.Fatalf("status=%d stdout=%q", status, stdout)
	}
	if got, _ := os.ReadFile(file); string(got) != "kaku x=1;\n" {
		t.Fatalf("--check modified the file: %q", got)
	}
	writeSource(t, file, "kaku x = 1;\n")
	if status, _, _ := runFmtCLI(t, "", "--check", file); status != 0 {
		t.Fatalf("formatted file must pass --check, status=%d", status)
	}
}

func TestFmtInvalidFileIsReportedAndUntouched(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.mio")
	good := filepath.Join(dir, "good.mio")
	writeSource(t, bad, "kaku x = ;\n")
	writeSource(t, good, "kaku y=2;\n")
	status, _, stderr := runFmtCLI(t, "", dir)
	if status != 2 || !strings.Contains(stderr, "bad.mio") {
		t.Fatalf("status=%d stderr=%q", status, stderr)
	}
	if got, _ := os.ReadFile(bad); string(got) != "kaku x = ;\n" {
		t.Fatalf("invalid file was modified: %q", got)
	}
	if got, _ := os.ReadFile(good); string(got) != "kaku y = 2;\n" {
		t.Fatalf("other files must still be formatted: %q", got)
	}
}

func TestFmtStdin(t *testing.T) {
	status, stdout, _ := runFmtCLI(t, "kaku x=1;", "--stdin")
	if status != 0 || stdout != "kaku x = 1;\n" {
		t.Fatalf("status=%d stdout=%q", status, stdout)
	}
	if status, stdout, _ := runFmtCLI(t, "kaku x=1;", "--stdin", "--check"); status != 1 || stdout != "" {
		t.Fatalf("--stdin --check status=%d stdout=%q", status, stdout)
	}
	if status, _, stderr := runFmtCLI(t, "kaku x = ;", "--stdin"); status != 2 || stderr == "" {
		t.Fatalf("invalid stdin status=%d stderr=%q", status, stderr)
	}
}

func TestFmtSkipsHiddenAndBuildDirectoriesAndUsageErrors(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, filepath.Join(dir, ".hidden", "a.mio"), "kaku x=1;\n")
	writeSource(t, filepath.Join(dir, "node_modules", "b.mio"), "kaku x=1;\n")
	if status, stdout, _ := runFmtCLI(t, "", "--check", dir); status != 0 || stdout != "" {
		t.Fatalf("skipped dirs must be ignored: status=%d stdout=%q", status, stdout)
	}
	for _, args := range [][]string{{}, {"--bogus", "x.mio"}, {"--stdin", "x.mio"}} {
		if status, _, stderr := runFmtCLI(t, "", args...); status != 2 || !strings.Contains(stderr, "Usage") {
			t.Fatalf("args %v: status=%d stderr=%q", args, status, stderr)
		}
	}
	if status, stdout, _ := runFmtCLI(t, "", "--help"); status != 0 || !strings.Contains(stdout, "Usage: mio fmt") {
		t.Fatalf("help status=%d", status)
	}
	if status, _, stderr := runFmtCLI(t, "", filepath.Join(dir, "missing.mio")); status != 2 || stderr == "" {
		t.Fatalf("missing path status=%d", status)
	}
}

func TestFmtDispatchedByMainCLI(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := runCLI([]string{"fmt", "--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), "mio fmt") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}
