package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/aot/bootstrapaudit"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

func TestRunReportsAndStrictMode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "compiler-A.mochi")
	output := filepath.Join(dir, "audit.json")
	f, e := os.Create(input)
	if e != nil {
		t.Fatal(e)
	}
	program := &compiler.Bytecode{Constants: []value.Value{value.NewInt(42)}, Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpChant), byte(code.OpNil)}}
	if e = mochi.Encode(f, program); e != nil {
		f.Close()
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	var stdout, stderr bytes.Buffer
	status := run([]string{"-in", input, "-json", output, "-top", "3"}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpNil") || !strings.Contains(stdout.String(), "OpConstant") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	content, e := os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	var report bootstrapaudit.Report
	if e = json.Unmarshal(content, &report); e != nil {
		t.Fatal(e)
	}
	if report.TotalInstructions != 3 || report.MissingInstructions != 1 || report.SourceSHA256 == "" {
		t.Fatalf("report: %+v", report)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-in", input, "-json", "", "-strict"}, &stdout, &stderr); code != 3 {
		t.Fatalf("strict exit=%d", code)
	}
}

func TestRunRejectsUnavailableFile(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"-in", filepath.Join(t.TempDir(), "absent.mochi"), "-json", ""}, &out, &err); code != 1 {
		t.Fatalf("missing file returned %d", code)
	}
	if !strings.Contains(err.String(), "读取失败") {
		t.Fatalf("unexpected stderr: %s", err.String())
	}
}
