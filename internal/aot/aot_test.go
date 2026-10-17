package aot

import (
	"bytes"
	"debug/macho"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func TestEmptyMochiAccepted(t *testing.T) {
	image, err := CompileDarwinARM64(&compiler.Bytecode{})
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != 0x4000 {
		t.Fatalf("native image length=%d, want 16384", len(image))
	}
}

func TestM1IntegerCalculationAccepted(t *testing.T) {
	program := &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(40), value.NewInt(2)},
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0,
			byte(code.OpConstant), 0, 1,
			byte(code.OpAdd),
			byte(code.OpChant),
		},
	}
	image, err := CompileDarwinARM64(program)
	if err != nil {
		t.Fatal(err)
	}
	f, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("invalid Mach-O: %v", err)
	}
	defer f.Close()
	section := f.Section("__text")
	if section == nil || section.Size <= 12 {
		t.Fatalf("expected native arithmetic and print code, got section=%v", section)
	}
}

func TestInvalidMochiRejected(t *testing.T) {
	_, err := CompileDarwinARM64(nil)
	if err == nil || !strings.Contains(err.Error(), "invalid Mochi bytecode") {
		t.Fatalf("expected Mochi validation error; got %v", err)
	}
}

func TestUnsupportedOpcodeRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Instructions: code.Instructions{byte(code.OpTrue)},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported opcode OpTrue") {
		t.Fatalf("expected unsupported opcode, got %v", err)
	}
}

func TestUnsupportedConstantRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Constants: []value.Value{value.NewString("hello")},
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpChant),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "only int64") {
		t.Fatalf("expected unsupported constant, got %v", err)
	}
}

func TestUnsupportedGlobalRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{GlobalsCount: 1})
	if err == nil || !strings.Contains(err.Error(), "does not support globals") {
		t.Fatalf("expected unsupported globals, got %v", err)
	}
}

func TestArithmeticStackUnderflowRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Instructions: code.Instructions{byte(code.OpAdd)},
	})
	if err == nil || !strings.Contains(err.Error(), "stack underflow") {
		t.Fatalf("expected stack underflow, got %v", err)
	}
}

func TestOperandStackLimitRejected(t *testing.T) {
	instructions := make(code.Instructions, 0, 8*3)
	for i := 0; i < 8; i++ {
		instructions = append(instructions, byte(code.OpConstant), 0, 0)
	}
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(7)},
		Instructions: instructions,
	})
	if err == nil || !strings.Contains(err.Error(), "operand stack limit") {
		t.Fatalf("expected stack limit, got %v", err)
	}
}

func TestUnsupportedDivisionRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Instructions: code.Instructions{byte(code.OpDiv)},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported opcode OpDiv") {
		t.Fatalf("expected unsupported division, got %v", err)
	}
}
