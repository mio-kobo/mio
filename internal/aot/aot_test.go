package aot

import (
	"bytes"
	"debug/macho"
	"strings"
	"testing"

	"Mio/internal/aot/darwinarm64"
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
		Instructions: code.Instructions{byte(code.OpMap)},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported opcode OpMap") {
		t.Fatalf("expected unsupported opcode, got %v", err)
	}
}

func TestM7StringConstantAccepted(t *testing.T) {
	image, err := CompileDarwinARM64(&compiler.Bytecode{
		Constants: []value.Value{value.NewString("hello")},
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpChant),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.Section("__const") == nil {
		t.Fatal("expected M7 __TEXT,__const section")
	}
}

func TestM2GlobalsProduceWritableDataSegment(t *testing.T) {
	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(40), value.NewInt(2)},
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpSetGlobal), 0, 0,
			byte(code.OpConstant), 0, 1, byte(code.OpSetGlobal), 0, 1,
			byte(code.OpGetGlobal), 0, 0,
			byte(code.OpGetGlobal), 0, 1,
			byte(code.OpAdd), byte(code.OpChant),
		},
	}
	image, err := CompileDarwinARM64(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != 2*0x4000 {
		t.Fatalf("M2 image length=%d, want 32768", len(image))
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	data := m.Segment("__DATA")
	if data == nil || data.Prot != 3 || data.Maxprot != 3 || data.Filesz != 0x4000 {
		t.Fatalf("missing or invalid writable __DATA: %+v", data)
	}
	section := m.Section("__data")
	if section == nil || section.Size != 16 || section.Offset != 0x4000 {
		t.Fatalf("invalid global slot section: %+v", section)
	}
}

func TestM2UninitializedGlobalRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		GlobalsCount: 1,
		Instructions: code.Instructions{byte(code.OpGetGlobal), 0, 0, byte(code.OpChant)},
	})
	if err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Fatalf("expected uninitialized global error, got %v", err)
	}
}

func TestM2GlobalLimitRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{GlobalsCount: 2049})
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected globals limit error, got %v", err)
	}
}

func TestM4MalformedFunctionRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Functions: []compiler.CompiledFunction{{ID: 0, Name: "f"}},
	})
	if err == nil {
		t.Fatal("expected malformed function rejection")
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
	instructions := make(code.Instructions, 0, (darwinarm64.MaxM5OperandSlots+1)*3)
	for i := 0; i <= darwinarm64.MaxM5OperandSlots; i++ {
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
