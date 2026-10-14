package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/mochi"
)

// 测试 uint8 数组通过 run、build、exec 与 disasm 保持一致
func TestCLIUint8ArrayMochi(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "bytes.mio")
	mochiPath := filepath.Join(directory, "bytes.mochi")
	source := `kaku values: []uint8 = [0, 255]; values[0] = values[1]; chant(len(values)); chant(values[0]);`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	var runOut, runErr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &runOut, &runErr); exit != 0 {
		t.Fatalf("run exit=%d stderr=%q", exit, runErr.String())
	}
	var buildOut, buildErr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &buildOut, &buildErr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, buildErr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV11 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV11)
	}

	var execOut, execErr bytes.Buffer
	if exit := runCLI([]string{"exec", mochiPath}, &execOut, &execErr); exit != 0 {
		t.Fatalf("exec exit=%d stderr=%q", exit, execErr.String())
	}
	if execOut.String() != runOut.String() || execOut.String() != "2\n255\n" {
		t.Fatalf("run=%q exec=%q", runOut.String(), execOut.String())
	}

	var disasmOut, disasmErr bytes.Buffer
	if exit := runCLI([]string{"disasm", mochiPath}, &disasmOut, &disasmErr); exit != 0 {
		t.Fatalf("disasm exit=%d stderr=%q", exit, disasmErr.String())
	}
	for _, want := range []string{"uint8 255", "OpArray 2", "OpSetIndex", "OpIndex"} {
		if !strings.Contains(disasmOut.String(), want) {
			t.Fatalf("disassembly missing %q: %s", want, disasmOut.String())
		}
	}
}
