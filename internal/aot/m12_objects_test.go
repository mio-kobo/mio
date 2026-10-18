package aot

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"Mio/internal/vm"
)

func runM12VM(t *testing.T, program *compiler.Bytecode) []byte {
	t.Helper()
	var output bytes.Buffer
	machine := vm.NewWithOutput(program, &output)
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func runM12Native(t *testing.T, program *compiler.Bytecode) ([]byte, error) {
	t.Helper()
	return runM12NativeCompile(t, program, CompileDarwinARM64)
}

func runM12NativeStress(t *testing.T, program *compiler.Bytecode) ([]byte, error) {
	t.Helper()
	return runM12NativeCompile(t, program, CompileDarwinARM64WithGCStress)
}

func runM12NativeCompile(t *testing.T, program *compiler.Bytecode, compile func(*compiler.Bytecode) ([]byte, error)) ([]byte, error) {
	t.Helper()
	image, err := compile(program)
	if err != nil {
		return nil, err
	}
	path := t.TempDir() + "/m12-test"
	if err := os.WriteFile(path, image, 0o755); err != nil {
		return nil, err
	}
	if output, err := exec.Command("codesign", "-s", "-", path).CombinedOutput(); err != nil {
		return nil, &m12CommandError{err: err, output: output}
	}
	return exec.Command(path).CombinedOutput()
}

func requireM12StressDifferential(t *testing.T, program *compiler.Bytecode) {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	want := runM12VM(t, program)
	got, err := runM12NativeStress(t, program)
	if err != nil {
		t.Fatalf("native stress execution failed: %v, output=%q", err, got)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("native stress output=%q, VM output=%q", got, want)
	}
}

type m12CommandError struct {
	err    error
	output []byte
}

func (e *m12CommandError) Error() string { return e.err.Error() + ": " + string(e.output) }

func requireM12Differential(t *testing.T, program *compiler.Bytecode) {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	want := runM12VM(t, program)
	got, err := runM12Native(t, program)
	if err != nil {
		t.Fatalf("native execution failed: %v, output=%q", err, got)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("native output=%q, VM output=%q", got, want)
	}
}

func TestM12StructArrayDifferential(t *testing.T) {
	a := newM3TestAsm()
	// shared := [10, 20]
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArray, 2)
	a.emit(code.OpSetGlobal, 0)
	// shared[0] = 99; chant(shared[0]); chant(len(shared))
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpSetIndex)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	// struct { shared, "猫" }.field0 keeps the same array object.
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 4)
	a.emit(code.OpStruct, 2)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpConstant, 5)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	// String length is UTF-8 byte length, matching the VM.
	a.emit(code.OpConstant, 4)
	a.emit(code.OpLen)
	a.emit(code.OpChant)

	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(10), value.NewInt(20), value.NewInt(0), value.NewInt(99), value.NewString("猫"), value.NewInt(1),
		},
		GlobalsCount: 1,
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM12TypedEmptyAndUint8Differential(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayTyped, 2, int(value.KindUint8))
	a.emit(code.OpConstant, 2)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewUint8(65), value.NewUint8(255), value.NewInt(1)},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM12DynamicArrayCapacityDifferential(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindInt))
	a.emit(code.OpSetGlobal, 0)
	for constant := 1; constant <= 5; constant++ {
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, constant)
		a.emit(code.OpArrayPush)
	}
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 6)
	a.emit(code.OpArrayReserve)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 7)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(0), value.NewInt(10), value.NewInt(20), value.NewInt(30),
			value.NewInt(40), value.NewInt(50), value.NewInt(100), value.NewInt(4),
		},
		GlobalsCount: 1,
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM12ArrayMakeAndUint8Differential(t *testing.T) {
	a := newM3TestAsm()
	// make<string>(3, "猫")
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayMake, int(value.KindString))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	// uint8 使用紧凑 storage，并从零容量跨越扩容边界。
	a.emit(code.OpConstant, 3)
	a.emit(code.OpArrayWithCapacity, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 1)
	for constant := 4; constant <= 8; constant++ {
		a.emit(code.OpGetGlobal, 1)
		a.emit(code.OpConstant, constant)
		a.emit(code.OpArrayPush)
	}
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 9)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	// 非零初始容量必须精确保留，同时长度仍为零。
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindInt))
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(3), value.NewString("猫"), value.NewInt(2), value.NewInt(0),
			value.NewUint8(1), value.NewUint8(2), value.NewUint8(3), value.NewUint8(4),
			value.NewUint8(255), value.NewInt(4),
		},
		GlobalsCount: 3,
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM12NestedDynamicArrayAndStructDifferential(t *testing.T) {
	a := newM3TestAsm()
	// inner := with_capacity<int>(0); outer := with_capacity<array>(0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindInt))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindArray))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayPush)
	// struct { outer } 保存同一个 descriptor；随后通过 inner 别名扩容。
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpStruct, 1)
	a.emit(code.OpSetGlobal, 2)
	for constant := 1; constant <= 5; constant++ {
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, constant)
		a.emit(code.OpArrayPush)
	}
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpIndex)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpIndex)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	// 动态 struct 数组保留字段布局与引用。
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindStruct))
	a.emit(code.OpSetGlobal, 3)
	a.emit(code.OpGetGlobal, 3)
	a.emit(code.OpConstant, 6)
	a.emit(code.OpStruct, 1)
	a.emit(code.OpArrayPush)
	a.emit(code.OpGetGlobal, 3)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpIndex)
	a.emit(code.OpGetField, 0)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(0), value.NewInt(10), value.NewInt(20), value.NewInt(30),
			value.NewInt(40), value.NewInt(50), value.NewInt(99),
		},
		GlobalsCount: 4,
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM12DynamicArrayFailures(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	tests := []struct {
		name       string
		capacity   int64
		wantOutput string
	}{
		{name: "negative", capacity: -1, wantOutput: "invalid array capacity"},
		{name: "oom", capacity: 200000, wantOutput: "allocation failed"},
		{name: "multiply-overflow", capacity: int64(^uint64(0) >> 1), wantOutput: "invalid array capacity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := newM3TestAsm()
			a.emit(code.OpConstant, 0)
			a.emit(code.OpArrayWithCapacity, int(value.KindInt))
			a.emit(code.OpSetGlobal, 0)
			program := &compiler.Bytecode{Constants: []value.Value{value.NewInt(test.capacity)}, GlobalsCount: 1, Instructions: a.build()}
			output, err := runM12Native(t, program)
			if err == nil {
				t.Fatalf("native program succeeded: %q", output)
			}
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				t.Fatalf("unexpected native failure: %v", err)
			}
			if !bytes.Contains(output, []byte(test.wantOutput)) {
				t.Fatalf("output=%q, want %q", output, test.wantOutput)
			}
		})
	}
	t.Run("reserve-length-overflow", func(t *testing.T) {
		a := newM3TestAsm()
		a.emit(code.OpConstant, 0)
		a.emit(code.OpArray, 1)
		a.emit(code.OpSetGlobal, 0)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, 1)
		a.emit(code.OpArrayReserve)
		program := &compiler.Bytecode{
			Constants:    []value.Value{value.NewInt(1), value.NewInt(int64(^uint64(0) >> 1))},
			GlobalsCount: 1,
			Instructions: a.build(),
		}
		output, err := runM12Native(t, program)
		if err == nil {
			t.Fatalf("native program succeeded: %q", output)
		}
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("unexpected native failure: %v", err)
		}
		if !bytes.Contains(output, []byte("invalid array capacity")) {
			t.Fatalf("missing overflow diagnostic: %q", output)
		}
	})
}

func TestM12DynamicArrayRejectsInvalidOperands(t *testing.T) {
	tests := []struct {
		name       string
		program    *compiler.Bytecode
		wantOutput string
	}{
		{
			name:       "capacity-not-int",
			program:    &compiler.Bytecode{Constants: []value.Value{value.NewString("bad")}, Instructions: append(append(mustM12Instruction(t, code.OpConstant, 0), mustM12Instruction(t, code.OpArrayWithCapacity, int(value.KindInt))...), byte(code.OpPop))},
			wantOutput: "type mismatch",
		},
		{
			name:       "push-wrong-element",
			program:    &compiler.Bytecode{Constants: []value.Value{value.NewString("bad")}, Instructions: append(append(mustM12Instruction(t, code.OpArrayTyped, 0, int(value.KindInt)), mustM12Instruction(t, code.OpConstant, 0)...), byte(code.OpArrayPush))},
			wantOutput: "type mismatch",
		},
		{
			name:       "nullable-deferred-to-m13",
			program:    &compiler.Bytecode{Constants: []value.Value{value.NewInt(0)}, Instructions: append(append(mustM12Instruction(t, code.OpConstant, 0), mustM12Instruction(t, code.OpArrayWithCapacity, int(value.KindInt)|0x80)...), byte(code.OpPop))},
			wantOutput: "require M13",
		},
		{
			name:       "clone-not-array",
			program:    &compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: append(mustM12Instruction(t, code.OpConstant, 0), byte(code.OpArrayClone), byte(code.OpPop))},
			wantOutput: "type mismatch",
		},
		{
			name: "truncate-length-not-int",
			program: &compiler.Bytecode{Constants: []value.Value{value.NewString("bad")}, Instructions: append(
				append(mustM12Instruction(t, code.OpArrayTyped, 0, int(value.KindInt)), mustM12Instruction(t, code.OpConstant, 0)...),
				byte(code.OpArrayTruncate),
			)},
			wantOutput: "type mismatch",
		},
		{
			name: "append-all-type-mismatch",
			program: &compiler.Bytecode{Instructions: append(
				append(mustM12Instruction(t, code.OpArrayTyped, 0, int(value.KindInt)), mustM12Instruction(t, code.OpArrayTyped, 0, int(value.KindString))...),
				byte(code.OpArrayAppendAll),
			)},
			wantOutput: "type mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileDarwinARM64(test.program)
			if err == nil || !strings.Contains(err.Error(), test.wantOutput) {
				t.Fatalf("error=%v, want %q", err, test.wantOutput)
			}
		})
	}
}

func mustM12Instruction(t *testing.T, op code.Opcode, operands ...int) code.Instructions {
	t.Helper()
	ins, err := code.Make(op, operands...)
	if err != nil {
		t.Fatal(err)
	}
	return ins
}

func TestM12NativeBoundsFailure(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArray, 1)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(7), value.NewInt(-1)},
		Instructions: a.build(),
	}
	output, err := runM12Native(t, program)
	if err == nil {
		t.Fatalf("out-of-bounds native program succeeded: %q", output)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("unexpected native failure: %v", err)
	}
	if !bytes.Contains(output, []byte("array index out of bounds")) {
		t.Fatalf("missing explicit bounds error: %q", output)
	}
}

func TestM12RejectsInvalidCompositeOperations(t *testing.T) {
	tests := []struct {
		name string
		code code.Instructions
		want string
	}{
		{
			name: "field-out-of-range",
			code: code.Instructions{
				byte(code.OpConstant), 0, 0,
				byte(code.OpStruct), 0, 1,
				byte(code.OpGetField), 0, 1,
				byte(code.OpChant),
			},
			want: "outside known layout",
		},
		{
			name: "mixed-array",
			code: code.Instructions{
				byte(code.OpConstant), 0, 0,
				byte(code.OpConstant), 0, 1,
				byte(code.OpArray), 0, 2,
				byte(code.OpLen), byte(code.OpChant),
			},
			want: "type mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileDarwinARM64(&compiler.Bytecode{
				Constants:    []value.Value{value.NewInt(1), value.NewString("bad")},
				Instructions: test.code,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestM12ObjectsUseIntegratedM11Runtime(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpSetGlobal, 0)
	program := &compiler.Bytecode{GlobalsCount: 1, Instructions: a.build()}
	if _, err := CompileDarwinARM64WithM11Runtime(program); err != nil {
		t.Fatalf("M12 should use integrated M11b roots: %v", err)
	}
}

func TestM12ArrayCloneIndependentDescriptorAndShallowReferences(t *testing.T) {
	a := newM3TestAsm()
	// g0=[1,2], g1=clone(g0); length changes must remain independent.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArray, 2)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayClone)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayTruncate)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpArrayPush)
	for global := 0; global <= 1; global++ {
		a.emit(code.OpGetGlobal, global)
		a.emit(code.OpLen)
		a.emit(code.OpChant)
	}
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	// g2=[7], g3=[g2], g4=clone(g3). The nested descriptor stays shared.
	a.emit(code.OpConstant, 3)
	a.emit(code.OpArray, 1)
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpArray, 1)
	a.emit(code.OpSetGlobal, 3)
	a.emit(code.OpGetGlobal, 3)
	a.emit(code.OpArrayClone)
	a.emit(code.OpSetGlobal, 4)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 4)
	a.emit(code.OpArrayPush)
	a.emit(code.OpGetGlobal, 4)
	a.emit(code.OpConstant, 5)
	a.emit(code.OpIndex)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(1), value.NewInt(2), value.NewInt(3), value.NewInt(7), value.NewInt(8), value.NewInt(0),
		},
		GlobalsCount: 5,
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM12ArrayTruncateCapacityClearAndPush(t *testing.T) {
	a := newM3TestAsm()
	for i := 0; i < 3; i++ {
		a.emit(code.OpConstant, i)
	}
	a.emit(code.OpArray, 3)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpSetGlobal, 1)
	// Truncating to the current length is a no-op.
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpArrayTruncate)
	// Truncate a reference-bearing tail, then append into the cleared slot.
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 4)
	a.emit(code.OpArrayTruncate)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 5)
	a.emit(code.OpArrayPush)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 6)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpChant)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 7)
	a.emit(code.OpArrayTruncate)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewString("a"), value.NewString("b"), value.NewString("c"), value.NewInt(3),
			value.NewInt(1), value.NewString("d"), value.NewInt(1), value.NewInt(0),
		},
		GlobalsCount: 2,
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM12ArrayAppendAllAliasesUint8AndReferences(t *testing.T) {
	t.Run("empty-and-string", func(t *testing.T) {
		a := newM3TestAsm()
		a.emit(code.OpArrayTyped, 0, int(value.KindString))
		a.emit(code.OpSetGlobal, 0)
		a.emit(code.OpConstant, 0)
		a.emit(code.OpConstant, 1)
		a.emit(code.OpArray, 2)
		a.emit(code.OpSetGlobal, 1)
		// Empty source is a no-op; empty destination then grows for strings.
		a.emit(code.OpGetGlobal, 1)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpArrayAppendAll)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpGetGlobal, 1)
		a.emit(code.OpArrayAppendAll)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpLen)
		a.emit(code.OpChant)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, 2)
		a.emit(code.OpIndex)
		a.emit(code.OpChant)
		program := &compiler.Bytecode{
			Constants:    []value.Value{value.NewString("Mio"), value.NewString("猫"), value.NewInt(1)},
			GlobalsCount: 2,
			Instructions: a.build(),
		}
		requireM12Differential(t, program)
		requireM12StressDifferential(t, program)
	})

	t.Run("uint8-self-and-alias", func(t *testing.T) {
		a := newM3TestAsm()
		a.emit(code.OpArrayTyped, 0, int(value.KindUint8))
		a.emit(code.OpSetGlobal, 0)
		a.emit(code.OpConstant, 0)
		a.emit(code.OpConstant, 1)
		a.emit(code.OpArrayTyped, 2, int(value.KindUint8))
		a.emit(code.OpSetGlobal, 1)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpGetGlobal, 1)
		a.emit(code.OpArrayAppendAll)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpArrayAppendAll)
		// g2 aliases the same descriptor as g0.
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpSetGlobal, 2)
		a.emit(code.OpGetGlobal, 2)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpArrayAppendAll)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpLen)
		a.emit(code.OpChant)
		for _, index := range []int{2, 3} {
			a.emit(code.OpGetGlobal, 0)
			a.emit(code.OpConstant, index)
			a.emit(code.OpIndex)
			a.emit(code.OpChant)
		}
		program := &compiler.Bytecode{
			Constants: []value.Value{
				value.NewUint8(1), value.NewUint8(255), value.NewInt(0), value.NewInt(7),
			},
			GlobalsCount: 3,
			Instructions: a.build(),
		}
		requireM12Differential(t, program)
		requireM12StressDifferential(t, program)
	})

	t.Run("array-and-struct-references", func(t *testing.T) {
		a := newM3TestAsm()
		// inner=[41]; source=[inner]; destination=[]Array; append and mutate inner.
		a.emit(code.OpConstant, 0)
		a.emit(code.OpArray, 1)
		a.emit(code.OpSetGlobal, 0)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpArray, 1)
		a.emit(code.OpSetGlobal, 1)
		a.emit(code.OpArrayTyped, 0, int(value.KindArray))
		a.emit(code.OpSetGlobal, 2)
		a.emit(code.OpGetGlobal, 2)
		a.emit(code.OpGetGlobal, 1)
		a.emit(code.OpArrayAppendAll)
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpConstant, 1)
		a.emit(code.OpArrayPush)
		a.emit(code.OpGetGlobal, 2)
		a.emit(code.OpConstant, 2)
		a.emit(code.OpIndex)
		a.emit(code.OpConstant, 4)
		a.emit(code.OpIndex)
		a.emit(code.OpChant)
		// Struct elements are also shallow references.
		a.emit(code.OpArrayTyped, 0, int(value.KindStruct))
		a.emit(code.OpSetGlobal, 3)
		a.emit(code.OpConstant, 3)
		a.emit(code.OpStruct, 1)
		a.emit(code.OpArray, 1)
		a.emit(code.OpSetGlobal, 4)
		a.emit(code.OpGetGlobal, 3)
		a.emit(code.OpGetGlobal, 4)
		a.emit(code.OpArrayAppendAll)
		a.emit(code.OpGetGlobal, 3)
		a.emit(code.OpConstant, 2)
		a.emit(code.OpIndex)
		a.emit(code.OpGetField, 0)
		a.emit(code.OpChant)
		program := &compiler.Bytecode{
			Constants:    []value.Value{value.NewInt(41), value.NewInt(42), value.NewInt(0), value.NewInt(99), value.NewInt(1)},
			GlobalsCount: 5,
			Instructions: a.build(),
		}
		requireM12Differential(t, program)
		requireM12StressDifferential(t, program)
	})
}

func TestM12TruncateReleasesReferenceUnderGCStress(t *testing.T) {
	a := newM3TestAsm()
	// outer owns the only remaining reference to a large child storage.
	a.emit(code.OpArrayTyped, 0, int(value.KindArray))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindInt))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpArrayPush)
	// Drop the direct root, then remove and clear outer[0].
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayTruncate)
	// A second large allocation only fits if the removed child storage is swept.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindInt))
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(90000), value.NewInt(0)},
		GlobalsCount: 3,
		Instructions: a.build(),
	}
	requireM12StressDifferential(t, program)
}

func TestM12ArrayTruncateFailureMatchesVMSemantics(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	for _, length := range []int64{-1, 3} {
		t.Run(fmt.Sprintf("length-%d", length), func(t *testing.T) {
			a := newM3TestAsm()
			a.emit(code.OpConstant, 0)
			a.emit(code.OpConstant, 1)
			a.emit(code.OpArray, 2)
			a.emit(code.OpConstant, 2)
			a.emit(code.OpArrayTruncate)
			program := &compiler.Bytecode{
				Constants:    []value.Value{value.NewInt(1), value.NewInt(2), value.NewInt(length)},
				Instructions: a.build(),
			}
			machine := vm.New(program)
			vmErr := machine.Run()
			if vmErr == nil || !strings.Contains(vmErr.Error(), "outside range") {
				t.Fatalf("VM error=%v", vmErr)
			}
			output, nativeErr := runM12Native(t, program)
			exit, ok := nativeErr.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 {
				t.Fatalf("native error=%v output=%q", nativeErr, output)
			}
			// Native diagnostics intentionally use the stable M12 runtime format;
			// semantic failure and exit status match the richer VM diagnostic.
			if !bytes.Equal(output, []byte("M12 invalid array length\n")) {
				t.Fatalf("native stderr=%q, VM diagnostic=%q", output, vmErr)
			}
		})
	}
}
