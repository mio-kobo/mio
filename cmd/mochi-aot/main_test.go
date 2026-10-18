package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

func TestRunWritesNativeExecutable(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "program.mochi")
	output := filepath.Join(dir, "program")
	f, e := os.Create(input)
	if e != nil {
		t.Fatal(e)
	}
	program := &compiler.Bytecode{Constants: []value.Value{value.NewInt(42)}, Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpChant)}}
	if e = mochi.Encode(f, program); e != nil {
		f.Close()
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	var stdout, stderr bytes.Buffer
	status := run([]string{"-o", output, input}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote "+output) {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	content, e := os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	if len(content) < 4 || !bytes.Equal(content[:4], []byte{0xcf, 0xfa, 0xed, 0xfe}) {
		t.Fatalf("output is not a Mach-O image: %x", content[:min(4, len(content))])
	}
	info, e := os.Stat(output)
	if e != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("output is not executable: info=%v err=%v", info, e)
	}
}

func TestRunRejectsUnavailableFile(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"-o", filepath.Join(t.TempDir(), "out"), filepath.Join(t.TempDir(), "absent.mochi")}, &out, &err); code != 1 {
		t.Fatalf("missing file returned %d", code)
	}
	if !strings.Contains(err.String(), "open input") {
		t.Fatalf("unexpected stderr: %s", err.String())
	}
}

func TestRunRejectsHeapOutsideHardLimit(t *testing.T) {
	var out, stderr bytes.Buffer
	if status := run([]string{"-heap-mib", "1025", "-o", filepath.Join(t.TempDir(), "out"), "unused.mochi"}, &out, &stderr); status != 1 {
		t.Fatalf("status=%d stderr=%q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "between 1 and 1024") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
