package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"bytes"
	"debug/macho"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestM6NativeFloatCodegen(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpAdd)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpEqual)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpFloatToInt)
	a.emit(code.OpChant)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewFloat(1.5), value.NewFloat(2.25), value.NewFloat(3.75)}, Instructions: a.build()}
	img, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	m, err := macho.NewFile(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Section("__text") == nil {
		t.Fatal("missing __text")
	}
	// 确认 FADD D0,D0,D1、FCMP D0,D1 以及 FCVTZS X9,D0 存在。
	for _, word := range []uint32{0x1E612800, 0x1E612000, 0x9E780009} {
		var little [4]byte
		binary.LittleEndian.PutUint32(little[:], word)
		if !bytes.Contains(img, little[:]) {
			t.Fatalf("missing AArch64 FP opcode %#x", word)
		}
	}
}
func TestM6Float32Codegen(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpMul)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpEqual)
	a.emit(code.OpChant)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewFloat32(1.5), value.NewFloat32(2.0), value.NewFloat32(3.0)}, Instructions: a.build()}
	img, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], 0x1E210800)
	if !bytes.Contains(img, word[:]) {
		t.Fatal("missing FMUL S0,S0,S1")
	}
}
func TestM6FPCompareAll(t *testing.T) {
	for _, op := range []code.Opcode{code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual} {
		a := newM3TestAsm()
		a.emit(code.OpConstant, 0)
		a.emit(code.OpConstant, 1)
		a.emit(op)
		a.emit(code.OpChant)
		if _, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewFloat(math.NaN()), value.NewFloat(1)}, Instructions: a.build()}); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	}
}
func TestM6FloatFunctionCall(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpAdd)
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewFloat(2.5), value.NewFloat(1.5)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{{ID: 0, Name: "float_add", Instructions: fn.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true}}}
	if _, err := CompileDarwinARM64(p); err != nil {
		t.Fatal(err)
	}
}
func TestM6RejectMixedPrecision(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpAdd)
	a.emit(code.OpFloatToInt)
	a.emit(code.OpChant)
	_, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewFloat32(1.5), value.NewFloat(2.0)}, Instructions: a.build()})
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected mixed precision rejection: %v", err)
	}
}
func TestM6FloatPrintingExplicitlyRejected(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpChant)
	_, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewFloat(1.5)}, Instructions: a.build()})
	if err == nil || !strings.Contains(err.Error(), "float output") {
		t.Fatalf("expected floating display diagnostic: %v", err)
	}
}
func TestM6FloatToIntHasRangeCheck(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpFloatToInt)
	a.emit(code.OpChant)
	img, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewFloat(math.Inf(1))}, Instructions: a.build()})
	if err != nil {
		t.Fatal(err)
	}
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], 0x1E602000)
	if !bytes.Contains(img, word[:]) {
		t.Fatal("missing unordered NaN check")
	}
}
func TestM6FloatBranchAndBoolGlobal(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpGreater)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.jump(code.OpJumpIfFalse, "end")
	a.emit(code.OpTrue)
	a.emit(code.OpChant)
	a.label("end")
	if _, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewFloat(3.0), value.NewFloat(2.0)}, GlobalsCount: 1, Instructions: a.build()}); err != nil {
		t.Fatal(err)
	}
}
