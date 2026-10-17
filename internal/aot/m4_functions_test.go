package aot

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func newM4Factorial() *compiler.Bytecode {
	// main: chant(factorial(5)); factorial 使用真正的递归 OpCall。
	main := newM3TestAsm()
	main.emit(code.OpConstant, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	f := newM3TestAsm()
	f.emit(code.OpGetLocal, 0)
	f.emit(code.OpConstant, 0)
	f.emit(code.OpLessEqual)
	f.jump(code.OpJumpIfFalse, "recursive")
	f.emit(code.OpConstant, 0)
	f.emit(code.OpReturnValue)
	f.label("recursive")
	f.emit(code.OpGetLocal, 0)
	f.emit(code.OpGetLocal, 0)
	f.emit(code.OpConstant, 0)
	f.emit(code.OpSub)
	f.emit(code.OpCall, 0)
	f.emit(code.OpMul)
	f.emit(code.OpReturnValue)
	return &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(1), value.NewInt(5)},
		Instructions: main.build(),
		Functions:    []compiler.CompiledFunction{{ID: 0, Name: "factorial", Instructions: f.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true}},
	}
}

func TestM4NativeFactorialMachineCode(t *testing.T) {
	image, err := CompileDarwinARM64(newM4Factorial())
	if err != nil {
		t.Fatal(err)
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	section := m.Section("__text")
	if section == nil {
		t.Fatal("M4 Mach-O missing executable section")
	}
	code := make([]uint32, section.Size/4)
	for i := range code {
		pos := int(section.Offset) + 4*i
		code[i] = binary.LittleEndian.Uint32(image[pos : pos+4])
	}
	var calls, returns, prologues, guards int
	for _, word := range code {
		if word&0xFC000000 == 0x94000000 {
			calls++
		}
		if word == 0xD65F03C0 {
			returns++
		}
		if word == 0xA9BF7BFD {
			prologues++
		}
		if word&0xFF000010 == 0x54000000 && word&0xf == 8 {
			guards++
		}
	}
	if calls < 3 || returns < 2 || prologues < 1 || guards < 2 {
		t.Fatalf("native call protocol incomplete: bl=%d ret=%d prologue=%d guard=%d", calls, returns, prologues, guards)
	}
}

func TestM4NativeCallPreservesOperandStack(t *testing.T) {
	// add(10,inc(20)) + 3 应保留调用者的左侧运算数。
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpAdd)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpAdd)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 2)
	fn.emit(code.OpAdd)
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(10), value.NewInt(20), value.NewInt(3)},
		Instructions: main.build(),
		Functions:    []compiler.CompiledFunction{{ID: 0, Name: "inc3", Instructions: fn.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true}},
	}
	if _, err := CompileDarwinARM64(p); err != nil {
		t.Fatal(err)
	}
}

func TestM4UnitFunctionAndLocalSlot(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpSetLocal, 1)
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpChant)
	fn.emit(code.OpReturn)
	p := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(42), value.NewInt(7)},
		Instructions: main.build(),
		Functions:    []compiler.CompiledFunction{{ID: 0, Name: "show", Instructions: fn.build(), ParameterCount: 1, LocalCount: 2, ReturnsValue: false}},
	}
	if _, err := CompileDarwinARM64(p); err != nil {
		t.Fatal(err)
	}
}

func TestM4FunctionLocalLoop(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpSetLocal, 1)
	fn.label("head")
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpLess)
	fn.jump(code.OpJumpIfFalse, "end")
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpConstant, 2)
	fn.emit(code.OpAdd)
	fn.emit(code.OpSetLocal, 1)
	fn.jump(code.OpJump, "head")
	fn.label("end")
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(0), value.NewInt(5), value.NewInt(1)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{{ID: 0, Name: "loop", Instructions: fn.build(), ParameterCount: 1, LocalCount: 2, ReturnsValue: true}}}
	if _, err := CompileDarwinARM64(p); err != nil {
		t.Fatal(err)
	}
}

func TestM4UninitializedLocalRejected(t *testing.T) {
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 0, Name: "bad", Instructions: fn.build(), ParameterCount: 1, LocalCount: 2, ReturnsValue: true}}}
	if _, err := CompileDarwinARM64(p); err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Fatalf("expected uninitialized local rejection, got %v", err)
	}
}

func TestM4BoolReturnRejected(t *testing.T) {
	fn := newM3TestAsm()
	fn.emit(code.OpTrue)
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 0, Name: "bool", Instructions: fn.build(), ReturnsValue: true}}}
	if _, err := CompileDarwinARM64(p); err == nil || !strings.Contains(err.Error(), "int64") {
		t.Fatalf("expected bool return limitation, got %v", err)
	}
}

func TestM4CapturingClosureRejected(t *testing.T) {
	fn := newM3TestAsm()
	fn.emit(code.OpReturn)
	p := &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 0, Name: "capture", Instructions: fn.build(), Captures: []compiler.Capture{{Local: true, Index: 0}}}}}
	if _, err := CompileDarwinARM64(p); err == nil {
		t.Fatalf("expected capturing closure rejection, got %v", err)
	}
}

func TestM4FunctionFrameLimit(t *testing.T) {
	p := &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 0, Name: "too_many", ParameterCount: 8, LocalCount: 8}}}
	if _, err := CompileDarwinARM64(p); err == nil || !strings.Contains(err.Error(), "frame limits") {
		t.Fatalf("expected frame limit, got %v", err)
	}
}

func TestM4LocalLoopInitializationRejected(t *testing.T) {
	f := newM3TestAsm()
	f.emit(code.OpTrue)
	f.jump(code.OpJumpIfFalse, "join")
	f.emit(code.OpConstant, 0)
	f.emit(code.OpDefineLocal, 0)
	f.label("join")
	f.emit(code.OpGetLocal, 0)
	f.emit(code.OpReturnValue)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(9)}, Functions: []compiler.CompiledFunction{{ID: 0, Name: "branch", Instructions: f.build(), LocalCount: 1, ReturnsValue: true}}}
	if _, err := CompileDarwinARM64(p); err == nil || !strings.Contains(err.Error(), "uninitialized") {
		t.Fatalf("expected definite initialization check, got %v", err)
	}
}
