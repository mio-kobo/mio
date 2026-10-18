package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"bytes"
	"debug/macho"
	"strings"
	"testing"
)

// M11a 仅在显式开启时链接 GC Runtime；默认产物仍与 M7 保持一致。
func TestM11RuntimeExplicitOptIn(t *testing.T) {
	p := &compiler.Bytecode{Constants: []value.Value{value.NewString("M11a runtime\n")}, Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpChant)}}
	normal, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	withGC, err := CompileDarwinARM64WithM11Runtime(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(normal, withGC) {
		t.Fatal("M11a native runtime was not linked")
	}
	f, err := macho.NewFile(bytes.NewReader(withGC))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Section("__text") == nil || f.Section("__const") == nil {
		t.Fatal("expected executable code and read-only Root Map")
	}
	if len(withGC) < len(normal) {
		t.Fatalf("M11a binary unexpectedly shrank: %d vs %d", len(withGC), len(normal))
	}
}

func TestM11RuntimeRejectsUnsupportedTypes(t *testing.T) {
	p := &compiler.Bytecode{Constants: []value.Value{value.NewString("ok")}, Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpChant), byte(code.OpMap)}}
	_, err := CompileDarwinARM64WithM11Runtime(p)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported instruction must fail: %v", err)
	}
}
