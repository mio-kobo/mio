package aot

import (
	"bytes"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"Mio/internal/vm"
)

func TestM13aNilAndZeroDifferential(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpNil)
	a.emit(code.OpChant)
	a.emit(code.OpNil)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	for index := range 2 {
		a.emit(code.OpConstant, index)
		a.emit(code.OpIsNil)
		a.emit(code.OpChant)
	}
	a.emit(code.OpFalse)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	for index := 2; index < 4; index++ {
		a.emit(code.OpConstant, index)
		a.emit(code.OpIsNil)
		a.emit(code.OpChant)
	}
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(0), value.NewUint8(0), value.NewFloat(0), value.NewString(""),
		},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
	if got := runM12VM(t, program); !bytes.Equal(got, []byte("mu\nhai\niie\niie\niie\niie\niie\niie\n")) {
		t.Fatalf("VM output=%q", got)
	}
}

func TestM13aNilAndNullableTopLevelResults(t *testing.T) {
	if output, err := runM12Native(t, &compiler.Bytecode{
		Instructions: code.Instructions{byte(code.OpNil), byte(code.OpPop)},
	}); err != nil || !bytes.Equal(output, []byte("mu\n")) {
		t.Fatalf("nil result: err=%v output=%q", err, output)
	}
	a := newM3TestAsm()
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpPop)
	if output, err := runM12Native(t, &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(0)}, Instructions: a.build(),
	}); err != nil || !bytes.Equal(output, []byte("0\n")) {
		t.Fatalf("nullable zero result: err=%v output=%q", err, output)
	}
}

func TestM13aNullableReferenceAndControlFlowMerge(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	// Merge nil and a valid scalar zero on the operand stack.
	a.emit(code.OpFalse)
	a.jump(code.OpJumpIfFalse, "nil")
	a.emit(code.OpConstant, 0)
	a.jump(code.OpJump, "merge")
	a.label("nil")
	a.emit(code.OpNil)
	a.label("merge")
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(0)}, Instructions: a.build(),
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13aNullableGlobalFunctionAndReturn(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpNil)
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpIsNil)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpIsNil)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpAdd)
	main.emit(code.OpChant)
	identity := newM3TestAsm()
	identity.emit(code.OpGetLocal, 0)
	identity.emit(code.OpReturnValue)
	program := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "identity", Instructions: identity.build(),
			ParameterCount: 1, LocalCount: 1, ReturnsValue: true,
		}},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13aNullableStructAndArray(t *testing.T) {
	a := newM3TestAsm()
	// A nullable scalar field keeps valid zero distinct from nil.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpStruct, 1)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	a.emit(code.OpNil)
	a.emit(code.OpStruct, 1)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpStruct, 1)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	// [0, nil] is explicitly []int64?.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpNil)
	a.emit(code.OpArrayTyped, 2, int(value.KindInt)|0x80)
	a.emit(code.OpSetGlobal, 0)
	for _, index := range []int{0, 1} {
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, index)
		a.emit(code.OpIndex)
		a.emit(code.OpIsNil)
		a.emit(code.OpChant)
	}
	// Clear the live reference then restore a valid zero value.
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpNil)
	a.emit(code.OpSetNullableIndex)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetNullableIndex)
	for _, index := range []int{0, 1} {
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, index)
		a.emit(code.OpIndex)
		a.emit(code.OpIsNil)
		a.emit(code.OpChant)
	}
	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1)},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13aNullableInvalidBytecode(t *testing.T) {
	for _, test := range []struct {
		name    string
		program *compiler.Bytecode
		want    string
	}{
		{"isnil-underflow", &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpIsNil)}}, "operand stack underflow"},
		{"nullable-set-underflow", &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpSetNullableIndex)}}, "operand stack underflow"},
		{"unresolved-nil-operand", &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpNil), byte(code.OpNeg), byte(code.OpPop)}}, "unsupported arithmetic type nil"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileDarwinARM64(test.program)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}
}

func TestM13aInvalidNilUseHasCheckedNativeError(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpAdd)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(1)}, Instructions: a.build(),
	}
	if output, err := runM12Native(t, program); err == nil {
		t.Fatal("native invalid nil use unexpectedly succeeded")
	} else if !bytes.Contains(output, []byte("M13 nullable value is nil")) {
		t.Fatalf("native error=%v output=%q", err, output)
	}
	machineOutput := &bytes.Buffer{}
	// The VM must reject the same malformed flow; its diagnostic wording is
	// intentionally independent from the native checked-error message.
	if err := runM13VMError(program, machineOutput); err == nil {
		t.Fatal("VM invalid nil use unexpectedly succeeded")
	}
}

func runM13VMError(program *compiler.Bytecode, output *bytes.Buffer) error {
	return vm.NewWithOutput(program, output).Run()
}

func TestM13aNullableBoxGCReclaimsOldValues(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 1)
	a.label("loop")
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 1) // allocates a fresh nullable scalar box
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 1) // old box becomes unreachable
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "done")
	a.jump(code.OpJump, "loop")
	a.label("done")
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(30000)},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM13aClosureCellNullableScalar(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpNil)
	main.emit(code.OpCallValue, 1)
	main.emit(code.OpIsNil)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCallValue, 1)
	main.emit(code.OpIsNil)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpNil)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)

	worker := newM3TestAsm()
	worker.emit(code.OpGetLocal, 0)
	worker.emit(code.OpSetUpvalue, 0)
	worker.emit(code.OpGetUpvalue, 0)
	worker.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 1, Constants: []value.Value{value.NewInt(0)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "set", Instructions: worker.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13aClosureCellNilReleasesReference(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpCallValue, 0)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpConstant, 1)
	maker.emit(code.OpArrayMake, int(value.KindInt))
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)

	clearAndAllocate := newM3TestAsm()
	clearAndAllocate.emit(code.OpNil)
	clearAndAllocate.emit(code.OpSetUpvalue, 0)
	clearAndAllocate.emit(code.OpConstant, 0)
	clearAndAllocate.emit(code.OpConstant, 1)
	clearAndAllocate.emit(code.OpArrayMake, int(value.KindInt))
	clearAndAllocate.emit(code.OpLen)
	clearAndAllocate.emit(code.OpChant)
	clearAndAllocate.emit(code.OpReturn)

	program := &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(100000), value.NewInt(0)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "make", Instructions: maker.build(), LocalCount: 1, ReturnsValue: true},
			{ID: 1, Name: "clear", Instructions: clearAndAllocate.build(), Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
}

func TestM13aNullableBoxSurvivesStressSafepoint(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	requireM12StressDifferential(t, &compiler.Bytecode{
		GlobalsCount: 2, Constants: []value.Value{value.NewInt(0)}, Instructions: a.build(),
	})
}
