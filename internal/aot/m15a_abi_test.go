package aot

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func m15SumProgram(params int) *compiler.Bytecode {
	constants := make([]value.Value, 0, params+1)
	main := newM3TestAsm()
	for i := 0; i < params; i++ {
		constants = append(constants, value.NewInt(int64(i+1)))
		main.emit(code.OpConstant, i)
	}
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)

	fn := newM3TestAsm()
	if params == 0 {
		constants = append(constants, value.NewInt(42))
		fn.emit(code.OpConstant, 0)
	} else {
		fn.emit(code.OpGetLocal, 0)
		for i := 1; i < params; i++ {
			fn.emit(code.OpGetLocal, i)
			fn.emit(code.OpAdd)
		}
	}
	fn.emit(code.OpReturnValue)
	return &compiler.Bytecode{
		Constants: constants, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: fmt.Sprintf("sum%d", params), Instructions: fn.build(),
			ParameterCount: params, LocalCount: params, ReturnsValue: true,
		}},
	}
}

func TestM15aParameterCountsDifferential(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	for _, params := range []int{0, 1, 7, 8, 9, 16} {
		t.Run(fmt.Sprintf("params-%d", params), func(t *testing.T) {
			requireM12Differential(t, m15SumProgram(params))
		})
	}
}

func TestM15aResourceLimitDiagnostic(t *testing.T) {
	program := &compiler.Bytecode{Functions: []compiler.CompiledFunction{{
		ID: 0, Name: "too_many", ParameterCount: darwinarm64.MaxM15Parameters + 1,
		LocalCount: darwinarm64.MaxM15Parameters + 1,
	}}}
	_, err := CompileDarwinARM64(program)
	if err == nil || !strings.Contains(err.Error(), "exceeds native frame limits") {
		t.Fatalf("expected explicit M15a resource diagnostic, got %v", err)
	}
	program.Constants = []value.Value{value.NewString("force-m6")}
	program.Instructions, _ = code.Make(code.OpConstant, 0)
	program.Instructions = append(program.Instructions, byte(code.OpChant))
	_, err = CompileDarwinARM64(program)
	if err == nil || !strings.Contains(err.Error(), "parameter count 257 exceeds native ABI limit 256") {
		t.Fatalf("expected explicit M15a M6 resource diagnostic, got %v", err)
	}
}

func TestM15aMixedValueSlotsDifferential(t *testing.T) {
	main := newM3TestAsm()
	// int, float64, bool, string, array, struct, float32, array(x7),
	// string(stack #1), struct(stack #2), then another mixed six values.
	main.emit(code.OpConstant, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpTrue)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpConstant, 3)
	main.emit(code.OpArray, 1)
	main.emit(code.OpConstant, 4)
	main.emit(code.OpStruct, 1)
	main.emit(code.OpConstant, 5)
	main.emit(code.OpConstant, 6)
	main.emit(code.OpArray, 1)
	main.emit(code.OpConstant, 7)
	main.emit(code.OpConstant, 8)
	main.emit(code.OpStruct, 1)
	main.emit(code.OpConstant, 9)
	main.emit(code.OpConstant, 10)
	main.emit(code.OpFalse)
	main.emit(code.OpConstant, 11)
	main.emit(code.OpArray, 1)
	main.emit(code.OpConstant, 12)
	main.emit(code.OpConstant, 13)
	main.emit(code.OpCall, 0)

	fn := newM3TestAsm()
	for i := 0; i < 16; i++ {
		fn.emit(code.OpGetLocal, i)
		switch i {
		case 4, 7, 13:
			fn.emit(code.OpConstant, 14)
			fn.emit(code.OpIndex)
		case 5, 9:
			fn.emit(code.OpGetField, 0)
		}
		fn.emit(code.OpChant)
	}
	fn.emit(code.OpReturn)

	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(11), value.NewFloat(1.5), value.NewString("alpha"),
			value.NewInt(44), value.NewInt(55), value.NewFloat32(2.25),
			value.NewInt(77), value.NewString("omega"), value.NewInt(99),
			value.NewInt(101), value.NewFloat(-3.25), value.NewInt(131),
			value.NewFloat32(-4.5), value.NewString("tail"), value.NewInt(0),
		},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "mixed16", Instructions: fn.build(),
			ParameterCount: 16, LocalCount: 16,
		}},
	}
	requireM12Differential(t, program)
}

func TestM15aNestedCallDifferential(t *testing.T) {
	main := newM3TestAsm()
	constants := make([]value.Value, 16)
	for i := range constants {
		constants[i] = value.NewInt(int64(i + 1))
		main.emit(code.OpConstant, i)
	}
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)

	outer := newM3TestAsm()
	for i := 7; i < 16; i++ {
		outer.emit(code.OpGetLocal, i)
	}
	outer.emit(code.OpCall, 1)
	outer.emit(code.OpReturnValue)

	inner := newM3TestAsm()
	inner.emit(code.OpGetLocal, 0)
	for i := 1; i < 9; i++ {
		inner.emit(code.OpGetLocal, i)
		inner.emit(code.OpAdd)
	}
	inner.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants: constants, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "outer16", Instructions: outer.build(), ParameterCount: 16, LocalCount: 16, ReturnsValue: true},
			{ID: 1, Name: "inner9", Instructions: inner.build(), ParameterCount: 9, LocalCount: 9, ReturnsValue: true},
		},
	}
	requireM12Differential(t, program)
}

func m15MutualFunction(target int) code.Instructions {
	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 0)
	fn.emit(code.OpGreater)
	fn.jump(code.OpJumpIfFalse, "base")
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpSub)
	for i := 1; i < 9; i++ {
		fn.emit(code.OpGetLocal, i)
	}
	fn.emit(code.OpCall, target)
	fn.emit(code.OpReturnValue)
	fn.label("base")
	fn.emit(code.OpGetLocal, 8)
	fn.emit(code.OpReturnValue)
	return fn.build()
}

func TestM15aMutualRecursionDifferential(t *testing.T) {
	main := newM3TestAsm()
	constants := []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(24)}
	main.emit(code.OpConstant, 2)
	for i := 1; i < 9; i++ {
		constants = append(constants, value.NewInt(int64(i*101)))
		main.emit(code.OpConstant, 2+i)
	}
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: constants, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "mutual_a", Instructions: m15MutualFunction(1), ParameterCount: 9, LocalCount: 9, ReturnsValue: true},
			{ID: 1, Name: "mutual_b", Instructions: m15MutualFunction(0), ParameterCount: 9, LocalCount: 9, ReturnsValue: true},
		},
	}
	requireM12Differential(t, program)
}

func TestM15aSelfRecursionDifferential(t *testing.T) {
	main := newM3TestAsm()
	constants := []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(18)}
	main.emit(code.OpConstant, 2)
	for i := 1; i < 9; i++ {
		constants = append(constants, value.NewInt(int64(i*17)))
		main.emit(code.OpConstant, 2+i)
	}
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: constants, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "recursive9", Instructions: m15MutualFunction(0),
			ParameterCount: 9, LocalCount: 9, ReturnsValue: true,
		}},
	}
	requireM12Differential(t, program)
}

func TestM15aGCStressRootsAcrossArgumentTransfer(t *testing.T) {
	main := newM3TestAsm()
	constants := []value.Value{value.NewInt(999), value.NewInt(0), value.NewInt(123), value.NewInt(808)}
	main.emit(code.OpConstant, 2)
	main.emit(code.OpArray, 1)
	main.emit(code.OpSetGlobal, 0)
	// A caller-owned reference remains below all arguments across the call.
	main.emit(code.OpConstant, 0)
	main.emit(code.OpArray, 1)
	for i := 0; i < 8; i++ {
		main.emit(code.OpGetGlobal, 0)
	}
	// Allocation of argument #9 forces GC while the caller-owned reference and
	// all eight already evaluated register arguments are live on the stack.
	main.emit(code.OpConstant, 3)
	main.emit(code.OpArray, 1)
	for i := 9; i < 16; i++ {
		main.emit(code.OpGetGlobal, 0)
	}
	main.emit(code.OpCall, 0)
	// Both the caller-owned reference and returned reference must survive this GC.
	main.emit(code.OpArrayTyped, 0, int(value.KindInt))
	main.emit(code.OpSetGlobal, 1)
	main.emit(code.OpSetGlobal, 2)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpIndex)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 2)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpIndex)
	main.emit(code.OpChant)

	fn := newM3TestAsm()
	// Force collection after x7 and all stack arguments have entered local slots.
	fn.emit(code.OpArrayTyped, 0, int(value.KindInt))
	fn.emit(code.OpSetLocal, 16)
	for i := 0; i < 15; i++ {
		fn.emit(code.OpGetLocal, i)
		fn.emit(code.OpLen)
		fn.emit(code.OpPop)
	}
	fn.emit(code.OpGetLocal, 15)
	fn.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 3, Constants: constants, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "gc16", Instructions: fn.build(),
			ParameterCount: 16, LocalCount: 17, ReturnsValue: true,
		}},
	}
	requireM12StressDifferential(t, program)
}
