package aot

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func m15bCounterProgram() *compiler.Bytecode {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)

	counter := newM3TestAsm()
	counter.emit(code.OpGetUpvalue, 0)
	counter.emit(code.OpConstant, 1)
	counter.emit(code.OpAdd)
	counter.emit(code.OpSetUpvalue, 0)
	counter.emit(code.OpGetUpvalue, 0)
	counter.emit(code.OpReturnValue)

	return &compiler.Bytecode{
		GlobalsCount: 1,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_counter", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "counter", Instructions: counter.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
}

func TestM15bMutableCaptureDifferential(t *testing.T) {
	requireM12Differential(t, m15bCounterProgram())
}

func TestM15bMutableCaptureGCStress(t *testing.T) {
	requireM12StressDifferential(t, m15bCounterProgram())
}

func TestM15bUncapturedFunctionGCStress(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpFunction, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpFunction, 0)
	main.emit(code.OpSetGlobal, 0)
	fn := newM3TestAsm()
	fn.emit(code.OpReturn)
	requireM12StressDifferential(t, &compiler.Bytecode{
		GlobalsCount: 1, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{ID: 0, Name: "noop", Instructions: fn.build()}},
	})
}

func TestM15bUncapturedFunctionValueDifferential(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpFunction, 0)
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCallValue, 1)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpMul)
	fn.emit(code.OpReturnValue)
	requireM12Differential(t, &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(21), value.NewInt(2)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{ID: 0, Name: "double", Instructions: fn.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true}},
	})
}

func TestM15bMultipleCompositeCapturesGCStress(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpConstant, 1)
	maker.emit(code.OpSetLocal, 1)
	maker.emit(code.OpConstant, 2)
	maker.emit(code.OpSetLocal, 2)
	maker.emit(code.OpConstant, 3)
	maker.emit(code.OpArray, 1)
	maker.emit(code.OpSetLocal, 3)
	maker.emit(code.OpConstant, 4)
	maker.emit(code.OpStruct, 1)
	maker.emit(code.OpSetLocal, 4)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)

	closure := newM3TestAsm()
	closure.emit(code.OpGetUpvalue, 0)
	closure.emit(code.OpGetUpvalue, 1)
	closure.emit(code.OpAdd)
	closure.emit(code.OpChant)
	closure.emit(code.OpGetUpvalue, 2)
	closure.emit(code.OpChant)
	closure.emit(code.OpGetUpvalue, 3)
	closure.emit(code.OpConstant, 5)
	closure.emit(code.OpIndex)
	closure.emit(code.OpChant)
	closure.emit(code.OpGetUpvalue, 4)
	closure.emit(code.OpGetField, 0)
	closure.emit(code.OpChant)
	closure.emit(code.OpGetUpvalue, 0)
	closure.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(10), value.NewInt(20), value.NewString("capture"),
			value.NewInt(30), value.NewInt(40), value.NewInt(0),
		},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_composite", Instructions: maker.build(), LocalCount: 5, ReturnsValue: true},
			{ID: 1, Name: "composite_closure", Instructions: closure.build(), ReturnsValue: true, Captures: []compiler.Capture{
				{Local: true, Index: 0}, {Local: true, Index: 1}, {Local: true, Index: 2},
				{Local: true, Index: 3}, {Local: true, Index: 4},
			}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bSharedCellAndNestedForwarding(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	for _, index := range []int{0, 1, 0, 1} {
		main.emit(code.OpGetGlobal, 0)
		main.emit(code.OpConstant, index)
		main.emit(code.OpIndex)
		main.emit(code.OpCallValue, 0)
		main.emit(code.OpChant)
	}

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpClosure, 2)
	maker.emit(code.OpArray, 2)
	maker.emit(code.OpReturnValue)

	inc := newM3TestAsm()
	inc.emit(code.OpGetUpvalue, 0)
	inc.emit(code.OpConstant, 1)
	inc.emit(code.OpAdd)
	inc.emit(code.OpSetUpvalue, 0)
	inc.emit(code.OpGetUpvalue, 0)
	inc.emit(code.OpReturnValue)

	read := newM3TestAsm()
	read.emit(code.OpGetUpvalue, 0)
	read.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(0), value.NewInt(1)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_pair", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "increment", Instructions: inc.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
			{ID: 2, Name: "read", Instructions: read.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bClosureCallsSixteenArgumentFunction(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)

	closure := newM3TestAsm()
	closure.emit(code.OpGetUpvalue, 0)
	for i := 1; i < 16; i++ {
		closure.emit(code.OpConstant, i)
	}
	closure.emit(code.OpCall, 2)
	closure.emit(code.OpReturnValue)

	sum := newM3TestAsm()
	sum.emit(code.OpGetLocal, 0)
	for i := 1; i < 16; i++ {
		sum.emit(code.OpGetLocal, i)
		sum.emit(code.OpAdd)
	}
	sum.emit(code.OpReturnValue)
	constants := make([]value.Value, 16)
	for i := range constants {
		constants[i] = value.NewInt(int64(i + 1))
	}
	program := &compiler.Bytecode{
		Constants: constants, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_wide", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "wide_closure", Instructions: closure.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
			{ID: 2, Name: "sum16", Instructions: sum.build(), ParameterCount: 16, LocalCount: 16, ReturnsValue: true},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bNestedCaptureReturnsClosure(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)

	outer := newM3TestAsm()
	outer.emit(code.OpConstant, 0)
	outer.emit(code.OpSetLocal, 0)
	outer.emit(code.OpClosure, 1)
	outer.emit(code.OpCallValue, 0)
	outer.emit(code.OpReturnValue)

	middle := newM3TestAsm()
	middle.emit(code.OpClosure, 2)
	middle.emit(code.OpReturnValue)

	inner := newM3TestAsm()
	inner.emit(code.OpGetUpvalue, 0)
	inner.emit(code.OpConstant, 1)
	inner.emit(code.OpAdd)
	inner.emit(code.OpSetUpvalue, 0)
	inner.emit(code.OpGetUpvalue, 0)
	inner.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(40), value.NewInt(1)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "outer", Instructions: outer.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "middle", Instructions: middle.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
			{ID: 2, Name: "inner", Instructions: inner.build(), ReturnsValue: true, Captures: []compiler.Capture{{Index: 0}}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bClosureAsFunctionParameter(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpFunction, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)

	apply := newM3TestAsm()
	apply.emit(code.OpGetLocal, 1)
	apply.emit(code.OpGetLocal, 0)
	apply.emit(code.OpCallValue, 1)
	apply.emit(code.OpReturnValue)

	double := newM3TestAsm()
	double.emit(code.OpGetLocal, 0)
	double.emit(code.OpConstant, 1)
	double.emit(code.OpMul)
	double.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(21), value.NewInt(2)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "apply", Instructions: apply.build(), ParameterCount: 2, LocalCount: 2, ReturnsValue: true},
			{ID: 1, Name: "double", Instructions: double.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bRecursiveClosureDeepShadowFrames(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpCallValue, 1)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpNil)
	maker.emit(code.OpDefineLocal, 1)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpSetLocal, 1)
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpReturnValue)

	recursive := newM3TestAsm()
	recursive.emit(code.OpGetLocal, 0)
	recursive.emit(code.OpConstant, 2)
	recursive.emit(code.OpLessEqual)
	recursive.jump(code.OpJumpIfFalse, "recurse")
	recursive.emit(code.OpGetUpvalue, 0)
	recursive.emit(code.OpReturnValue)
	recursive.label("recurse")
	recursive.emit(code.OpGetUpvalue, 1)
	recursive.emit(code.OpGetLocal, 0)
	recursive.emit(code.OpConstant, 3)
	recursive.emit(code.OpSub)
	recursive.emit(code.OpCallValue, 1)
	recursive.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(77), value.NewInt(64), value.NewInt(1), value.NewInt(1)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_recursive", Instructions: maker.build(), ParameterCount: 1, LocalCount: 2, ReturnsValue: true},
			{ID: 1, Name: "recursive", Instructions: recursive.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Captures: []compiler.Capture{
				{Local: true, Index: 0}, {Local: true, Index: 1},
			}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bUnreachableClosuresAreCollected(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpSetGlobal, 0)
	main.label("loop")
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 1)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpAdd)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpLess)
	main.jump(code.OpJumpIfFalse, "done")
	main.jump(code.OpJump, "loop")
	main.label("done")
	main.emit(code.OpGetGlobal, 1)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 3)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)
	closure := newM3TestAsm()
	closure.emit(code.OpGetUpvalue, 0)
	closure.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(12000), value.NewInt(42)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_garbage", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "garbage_closure", Instructions: closure.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
}

func TestM15bDefineLocalCreatesIndependentLoopCells(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	for i := 0; i < 3; i++ {
		main.emit(code.OpGetGlobal, 0)
		main.emit(code.OpConstant, i)
		main.emit(code.OpIndex)
		main.emit(code.OpCallValue, 0)
		main.emit(code.OpChant)
	}

	maker := newM3TestAsm()
	for i := 0; i < 3; i++ {
		maker.emit(code.OpFunction, 2)
	}
	maker.emit(code.OpArray, 3)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 1)
	maker.label("loop")
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpDefineLocal, 2)
	maker.emit(code.OpGetLocal, 0)
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpSetIndex)
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpConstant, 3)
	maker.emit(code.OpAdd)
	maker.emit(code.OpSetLocal, 1)
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpConstant, 4)
	maker.emit(code.OpLess)
	maker.jump(code.OpJumpIfFalse, "done")
	maker.jump(code.OpJump, "loop")
	maker.label("done")
	maker.emit(code.OpGetLocal, 0)
	maker.emit(code.OpReturnValue)

	closure := newM3TestAsm()
	closure.emit(code.OpGetUpvalue, 0)
	closure.emit(code.OpReturnValue)
	zero := newM3TestAsm()
	zero.emit(code.OpConstant, 0)
	zero.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(2), value.NewInt(1), value.NewInt(3)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_all", Instructions: maker.build(), LocalCount: 3, ReturnsValue: true},
			{ID: 1, Name: "loop_closure", Instructions: closure.build(), ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 2}}},
			{ID: 2, Name: "zero", Instructions: zero.build(), ReturnsValue: true},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bRejectsNonFunctionCallValue(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)
	fn := newM3TestAsm()
	fn.emit(code.OpConstant, 0)
	fn.emit(code.OpReturnValue)
	_, err := CompileDarwinARM64(&compiler.Bytecode{
		Constants: []value.Value{value.NewInt(7)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{ID: 0, Name: "candidate", Instructions: fn.build(), ReturnsValue: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("error=%v", err)
	}
}

func TestM15bUnitClosureStackBalance(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCallValue, 0)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)
	closure := newM3TestAsm()
	closure.emit(code.OpGetUpvalue, 0)
	closure.emit(code.OpChant)
	closure.emit(code.OpReturn)

	program := &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(7)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_unit", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "unit_closure", Instructions: closure.build(), Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bMutuallyRecursiveClosures(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCallValue, 1)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpNil)
	maker.emit(code.OpDefineLocal, 0)
	maker.emit(code.OpNil)
	maker.emit(code.OpDefineLocal, 1)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 2)
	maker.emit(code.OpSetLocal, 1)
	maker.emit(code.OpGetLocal, 0)
	maker.emit(code.OpReturnValue)

	mutual := func() code.Instructions {
		fn := newM3TestAsm()
		fn.emit(code.OpGetLocal, 0)
		fn.emit(code.OpConstant, 1)
		fn.emit(code.OpLessEqual)
		fn.jump(code.OpJumpIfFalse, "again")
		fn.emit(code.OpConstant, 2)
		fn.emit(code.OpReturnValue)
		fn.label("again")
		fn.emit(code.OpGetUpvalue, 0)
		fn.emit(code.OpGetLocal, 0)
		fn.emit(code.OpConstant, 3)
		fn.emit(code.OpSub)
		fn.emit(code.OpCallValue, 1)
		fn.emit(code.OpReturnValue)
		return fn.build()
	}

	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(80), value.NewInt(0), value.NewInt(123), value.NewInt(1)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make_mutual", Instructions: maker.build(), LocalCount: 2, ReturnsValue: true},
			{ID: 1, Name: "mutual_a", Instructions: mutual(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 1}}},
			{ID: 2, Name: "mutual_b", Instructions: mutual(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM15bInvalidCaptureMetadata(t *testing.T) {
	program := m15bCounterProgram()
	program.Functions[1].Captures[0].Index = 99
	_, err := CompileDarwinARM64(program)
	if err == nil || !strings.Contains(err.Error(), "capture 0 index 99 out of range") {
		t.Fatalf("error=%v", err)
	}
}

func TestM15bNativeImageIsMachOAndMatchesVM(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	program := m15bCounterProgram()
	want := runM12VM(t, program)
	got, err := runM12Native(t, program)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("native=%q VM=%q", got, want)
	}
}
