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

// m3TestAssembler 专门构造包含真实 byte offset 的 Mochi 控制流测试用例。
type m3TestAssembler struct {
	ins    code.Instructions
	labels map[string]int
	fixups []struct {
		at    int
		op    code.Opcode
		label string
	}
}

func newM3TestAsm() *m3TestAssembler { return &m3TestAssembler{labels: make(map[string]int)} }
func (a *m3TestAssembler) emit(op code.Opcode, args ...int) {
	b, err := code.Make(op, args...)
	if err != nil {
		panic(err)
	}
	a.ins = append(a.ins, b...)
}
func (a *m3TestAssembler) label(name string) { a.labels[name] = len(a.ins) }
func (a *m3TestAssembler) jump(op code.Opcode, label string) {
	a.fixups = append(a.fixups, struct {
		at    int
		op    code.Opcode
		label string
	}{len(a.ins), op, label})
	a.emit(op, 0)
}
func (a *m3TestAssembler) build() code.Instructions {
	for _, f := range a.fixups {
		target, ok := a.labels[f.label]
		if !ok {
			panic("missing label " + f.label)
		}
		b, err := code.Make(f.op, target)
		if err != nil {
			panic(err)
		}
		copy(a.ins[f.at:], b)
	}
	return a.ins
}

func TestM3BranchBackedgeAndBoolOutput(t *testing.T) {
	a := newM3TestAsm()
	// g0=0; while (g0<4) g0=g0+1; chant(g0); chant(g0==4);
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 0)
	a.label("head")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "done")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 0)
	a.jump(code.OpJump, "head")
	a.label("done")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpEqual)
	a.emit(code.OpChant)
	p := &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(0), value.NewInt(4), value.NewInt(1)}, Instructions: a.build()}
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Segment("__DATA") == nil {
		t.Fatal("branching program lost global data segment")
	}
	sec := m.Section("__text")
	if sec == nil {
		t.Fatal("missing native text")
	}
	code := make([]uint32, sec.Size/4)
	for i := range code {
		off := int(sec.Offset) + i*4
		code[i] = binary.LittleEndian.Uint32(image[off : off+4])
	}
	foundBack, foundCBZ, foundCSET := false, false, false
	for _, w := range code {
		if w&0xfc000000 == 0x14000000 && w&0x02000000 != 0 {
			foundBack = true
		}
		if w&0xff000000 == 0xb4000000 {
			foundCBZ = true
		}
		if w&0xffe00fe0 == 0x1a8007e0 {
			foundCSET = true
		}
	}
	if !foundBack || !foundCBZ || !foundCSET {
		t.Fatalf("missing ARM64 control flow instructions: B(back)=%v CBZ=%v CSET=%v", foundBack, foundCBZ, foundCSET)
	}
}

func TestM3StackDepthMismatchAtJoinRejected(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpTrue)
	a.jump(code.OpJumpIfFalse, "end")
	a.emit(code.OpConstant, 0)
	a.label("end")
	a.emit(code.OpChant)
	_, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewInt(7)}, Instructions: a.build()})
	if err == nil || !strings.Contains(err.Error(), "stack depth mismatch") {
		t.Fatalf("expected stack mismatch, got %v", err)
	}
}
func TestM3StackTypeMismatchAtJoinRejected(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpTrue)
	a.jump(code.OpJumpIfFalse, "false")
	a.emit(code.OpConstant, 0)
	a.jump(code.OpJump, "done")
	a.label("false")
	a.emit(code.OpTrue)
	a.label("done")
	a.emit(code.OpChant)
	_, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewInt(7)}, Instructions: a.build()})
	if err == nil || !strings.Contains(err.Error(), "stack type mismatch") {
		t.Fatalf("expected stack type mismatch, got %v", err)
	}
}
func TestM3PartiallyInitializedGlobalRejected(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpTrue)
	a.jump(code.OpJumpIfFalse, "join")
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 0)
	a.label("join")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpChant)
	_, err := CompileDarwinARM64(&compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(12)}, Instructions: a.build()})
	if err == nil || !strings.Contains(err.Error(), "may be uninitialized") {
		t.Fatalf("expected global may be uninitialized, got %v", err)
	}
}
func TestM3BoolGlobalRejected(t *testing.T) {
	_, err := CompileDarwinARM64(&compiler.Bytecode{GlobalsCount: 1, Instructions: code.Instructions{byte(code.OpTrue), byte(code.OpSetGlobal), 0, 0}})
	if err == nil || !strings.Contains(err.Error(), "requires int64") {
		t.Fatalf("expected unsupported bool global, got %v", err)
	}
}
func TestM3SignedComparisonUsesCMP(t *testing.T) {
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(-9223372036854775808), value.NewInt(9223372036854775807)}, Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpConstant), 0, 1, byte(code.OpLess), byte(code.OpChant)}}
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(image, []byte{0x3f, 0x01, 0x0a, 0xeb}) {
		t.Fatal("expected CMP X9,X10 in native code")
	}
}
func TestM3BadJumpTargetRejected(t *testing.T) {
	p := &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpJump), 0, 1}}
	_, err := CompileDarwinARM64(p)
	if err == nil || !strings.Contains(err.Error(), "instruction boundary") {
		t.Fatalf("expected jump boundary validation, got %v", err)
	}
}
func TestM3RejectUnsupportedUnreachableOpcode(t *testing.T) {
	p := &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpJump), 0, 4, byte(code.OpNil)}}
	_, err := CompileDarwinARM64(p)
	if err == nil || !strings.Contains(err.Error(), "unsupported opcode") {
		t.Fatalf("expected unsupported unreachable opcode, got %v", err)
	}
}
func TestM3ConditionalPopTracksExecution(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpFalse)
	a.jump(code.OpJumpIfFalse, "end")
	a.emit(code.OpConstant, 0)
	a.emit(code.OpPop)
	a.label("end")
	p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(99)}, Instructions: a.build()}
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(image, []byte{0x14, 0x00, 0x80, 0xd2}) {
		t.Fatal("expected MOVZ x20,0 initialization for conditional Pop")
	}
}

func TestM3EmptyProgramPreservesM0(t *testing.T) {
	got, err := CompileDarwinARM64(&compiler.Bytecode{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := darwinarm64.ExitProgram(0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("empty program changed M0 exit image")
	}
}
