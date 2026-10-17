package aot

import (
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
)

func TestEmptyMochiAccepted(t *testing.T) {
	image, err := CompileDarwinARM64(&compiler.Bytecode{})
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != 0x4000 {
		t.Fatalf("native image length: %d", len(image))
	}
}

func TestNonemptyMochiRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{Instructions: code.Instructions{byte(code.OpTrue)}})
	if err == nil || !strings.Contains(err.Error(), "only an empty Mochi program") {
		t.Fatalf("expected explicit unsupported-program error; got %v", err)
	}
}

func TestInvalidMochiRejected(t *testing.T) {
	_, err := CompileDarwinARM64(nil)
	if err == nil || !strings.Contains(err.Error(), "invalid Mochi bytecode") {
		t.Fatalf("expected Mochi validation error; got %v", err)
	}
}
