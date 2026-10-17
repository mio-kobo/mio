package aot

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// deepPushes 构造需要大量同时存活的整数的栈表达式，避免常量折叠。
func deepPushes(depth int) code.Instructions {
	ins := make(code.Instructions, 0, depth*4)
	for i := 0; i < depth; i++ {
		ins = append(ins, byte(code.OpConstant), 0, 0)
	}
	for i := 1; i < depth; i++ {
		ins = append(ins, byte(code.OpAdd))
	}
	return ins
}

func TestM5MainDeepStackMachO(t *testing.T) {
	for _, depth := range []int{8, 32, 128} {
		ins := append(deepPushes(depth), byte(code.OpChant))
		p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: ins}
		image, err := CompileDarwinARM64(p)
		if err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		m, err := macho.NewFile(bytes.NewReader(image))
		if err != nil {
			t.Fatal(err)
		}
		if m.Section("__text") == nil {
			t.Fatal("missing native __text")
		}
		m.Close()
		// M5 在入口保存 x29/x30，然后分配与最大栈深度对应的帧。
		if binary.LittleEndian.Uint32(image[0x300:0x304]) != 0xA9BF7BFD {
			t.Fatalf("depth %d: missing FP/LR frame prologue", depth)
		}
	}
}

func TestM5RecursiveCallWithDeepCallerStack(t *testing.T) {
	// f(n) = 8*n + f(n-1), f(1)=1。
	// 递归调用发生前，当前函数已保存 8 个 n，调用返回后再依次相加。
	main := newM3TestAsm()
	main.emit(code.OpConstant, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 0)
	fn.emit(code.OpLessEqual)
	fn.jump(code.OpJumpIfFalse, "recursive")
	fn.emit(code.OpConstant, 0)
	fn.emit(code.OpReturnValue)
	fn.label("recursive")
	for i := 0; i < 8; i++ {
		fn.emit(code.OpGetLocal, 0)
	}
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 0)
	fn.emit(code.OpSub)
	fn.emit(code.OpCall, 0)
	for i := 0; i < 8; i++ {
		fn.emit(code.OpAdd)
	}
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(1), value.NewInt(5)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{{ID: 0, Name: "recursive", Instructions: fn.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true}}}
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(image[0x300:0x304]) != 0xA9BF7BFD {
		t.Fatal("recursive deep stack must use frame storage")
	}
}

func TestM5DeepOperandFrameWithManyLocals(t *testing.T) {
	// 256 个局部槽 + 300 个操作数，覆盖大于 4095 字节的 FP 负偏移。
	// 此时地址必须使用 MOVZ + SUB register，而不是截断 SUB immediate。
	fn := newM3TestAsm()
	for i := 0; i < 300; i++ {
		fn.emit(code.OpConstant, 0)
	}
	for i := 0; i < 300; i++ {
		fn.emit(code.OpPop)
	}
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpReturnValue)
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(1), value.NewInt(7)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{{ID: 0, Name: "wide", Instructions: fn.build(), LocalCount: 256, ReturnsValue: true}}}
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	// SUB x8,x29,x8 必须确实出现。
	if !bytes.Contains(image, []byte{0xA8, 0x03, 0x08, 0xCB}) {
		t.Fatal("missing large-offset address encoder SUB x8,x29,x8")
	}
}

func TestM5ShallowProgramsPreserveM4Binary(t *testing.T) {
	p := newM4Factorial()
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	// 浅栈程序仍沿用 M4 的初始 MOVZ x20,0，不在 main 生成 M5 帧。
	if binary.LittleEndian.Uint32(image[0x300:0x304]) != 0xD2800014 {
		t.Fatal("shallow factorial no longer follows the verified M4 path")
	}
}

func TestM5RejectUnbalancedDeepStack(t *testing.T) {
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: deepPushes(8)[:24]}
	_, err := CompileDarwinARM64(p)
	if err == nil || !strings.Contains(err.Error(), "exited with") {
		t.Fatalf("expected unbalanced stack rejection, got %v", err)
	}
}

func TestM5StaticOperandLimit(t *testing.T) {
	ins := make(code.Instructions, 0, (darwinarm64.MaxM5OperandSlots+1)*3)
	for i := 0; i <= darwinarm64.MaxM5OperandSlots; i++ {
		ins = append(ins, byte(code.OpConstant), 0, 0)
	}
	_, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: ins})
	if err == nil || !strings.Contains(err.Error(), "operand stack limit") {
		t.Fatalf("expected operand stack limit rejection, got %v", err)
	}
}

func TestM5ConditionalDeepStackAndPop(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpFalse)
	a.jump(code.OpJumpIfFalse, "skip")
	for i := 0; i < 12; i++ {
		a.emit(code.OpConstant, 0)
	}
	for i := 1; i < 12; i++ {
		a.emit(code.OpAdd)
	}
	a.emit(code.OpPop)
	a.label("skip")
	for i := 0; i < 12; i++ {
		a.emit(code.OpConstant, 0)
	}
	for i := 1; i < 12; i++ {
		a.emit(code.OpAdd)
	}
	a.emit(code.OpPop)
	_, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: a.build()})
	if err != nil {
		t.Fatal(err)
	}
}
