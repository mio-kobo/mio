package aot

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
	"Mio/internal/value"
)

func compileM13bSource(t *testing.T, source string) *compiler.Bytecode {
	t.Helper()
	program, err := parser.New(lexer.New(source, token.V04)).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	bindings := resolver.New()
	if err := bindings.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	if err := typecheck.NewWithResolver(bindings).CheckProgram(program); err != nil {
		t.Fatal(err)
	}
	compiled := compiler.NewWithResolver(bindings)
	if err := compiled.Compile(program); err != nil {
		t.Fatal(err)
	}
	return compiled.Bytecode()
}

func TestM13bCheckedSourceVMNativeDifferential(t *testing.T) {
	program := compileM13bSource(t, `
sentaku Result<T, E> { Ok(T); Err(E); }
waza relay(result: Result<int64, string>) -> Result<int64, string> {
    awase (result) {
        baai Ok(value) { kaesu Result.Ok(value); }
        baai Err(message) { kaesu Result.Err(message); }
    }
    kaesu Result.Ok(0);
}
waza print(result: Result<int64, string>) -> unit {
    awase (result) {
        baai Ok(value) { chant(value); }
        baai Err(message) { chant(message); }
    }
}
print(relay(Result[int64, string].Ok(42)));
print(relay(Result[int64, string].Err("failed")));
`)
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bEnumTagPayloadAndEmptyDifferential(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpEnum, 0, 0)
	a.emit(code.OpEnumTag)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpEnum, 7, 2)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpEnumTag)
	a.emit(code.OpChant)
	for index := range 2 {
		a.emit(code.OpGetGlobal, 0)
		a.emit(code.OpGetEnumValue, index)
		a.emit(code.OpChant)
	}
	program := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants:    []value.Value{value.NewInt(42), value.NewString("mio")},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
	if got := runM12VM(t, program); !bytes.Equal(got, []byte("0\n7\n42\nmio\n")) {
		t.Fatalf("VM output=%q", got)
	}
}

func TestM13bEnumMatchFlowDifferential(t *testing.T) {
	a := newM3TestAsm()
	// This is the exact awase lowering: store the enum, compare its tag, then
	// unpack only on the matching edge. The two variants intentionally use
	// incompatible payload types at index zero.
	a.emit(code.OpFalse)
	a.emit(code.OpCall, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpEnumTag)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpEqual)
	a.jump(code.OpJumpIfFalse, "next")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetEnumValue, 0)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpChant)
	a.jump(code.OpJump, "done")
	a.label("next")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpEnumTag)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpEqual)
	a.jump(code.OpJumpIfFalse, "done")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetEnumValue, 0)
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpChant)
	a.label("done")
	program := &compiler.Bytecode{
		GlobalsCount: 3,
		Constants: []value.Value{
			value.NewInt(42), value.NewInt(0), value.NewInt(1), value.NewString("error"),
		},
		Instructions: a.build(),
		Functions: []compiler.CompiledFunction{func() compiler.CompiledFunction {
			factory := newM3TestAsm()
			factory.emit(code.OpGetLocal, 0)
			factory.jump(code.OpJumpIfFalse, "error")
			factory.emit(code.OpConstant, 0)
			factory.emit(code.OpEnum, 0, 1)
			factory.emit(code.OpReturnValue)
			factory.label("error")
			factory.emit(code.OpConstant, 3)
			factory.emit(code.OpEnum, 1, 1)
			factory.emit(code.OpReturnValue)
			return compiler.CompiledFunction{ID: 0, Name: "result", ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Instructions: factory.build()}
		}()},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bEnumFunctionArrayStructNullableAndNested(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpEnum, 2, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	// Enum in array in struct, then nested enum.
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpArray, 1)
	main.emit(code.OpStruct, 1)
	main.emit(code.OpEnum, 3, 1)
	main.emit(code.OpGetEnumValue, 0)
	main.emit(code.OpGetField, 0)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpIndex)
	main.emit(code.OpGetEnumValue, 0)
	main.emit(code.OpChant)
	// Nullable enum uses nil directly and a non-nil enum pointer otherwise.
	main.emit(code.OpNil)
	main.emit(code.OpSetGlobal, 1)
	main.emit(code.OpGetGlobal, 1)
	main.emit(code.OpIsNil)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpSetGlobal, 1)
	main.emit(code.OpGetGlobal, 1)
	main.emit(code.OpIsNil)
	main.emit(code.OpChant)

	identity := newM3TestAsm()
	identity.emit(code.OpGetLocal, 0)
	identity.emit(code.OpReturnValue)
	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(99), value.NewInt(0)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "enum_identity", ParameterCount: 1, LocalCount: 1,
			ReturnsValue: true, Instructions: identity.build(),
		}},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bClosureCapturesEnum(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpGetEnumValue, 0)
	main.emit(code.OpChant)

	maker := newM3TestAsm()
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpEnum, 4, 1)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)

	reader := newM3TestAsm()
	reader.emit(code.OpGetUpvalue, 0)
	reader.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(123)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "maker", LocalCount: 1, ReturnsValue: true, Instructions: maker.build()},
			{ID: 1, Name: "reader", ReturnsValue: true, Instructions: reader.build(), Captures: []compiler.Capture{{Local: true, Index: 0}}},
		},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bEnumContainsClosureAndGCStressPayloadRoots(t *testing.T) {
	main := newM3TestAsm()
	// The closure, array and struct are all live only on the operand stack when
	// OpEnum performs its stress collection.
	main.emit(code.OpFunction, 0)
	main.emit(code.OpConstant, 0)
	main.emit(code.OpArray, 1)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpStruct, 1)
	main.emit(code.OpEnum, 6, 3)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpGetEnumValue, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpGetEnumValue, 1)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpIndex)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpGetEnumValue, 2)
	main.emit(code.OpGetField, 0)
	main.emit(code.OpChant)
	callee := newM3TestAsm()
	callee.emit(code.OpConstant, 3)
	callee.emit(code.OpReturnValue)
	program := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants: []value.Value{
			value.NewInt(22), value.NewInt(33), value.NewInt(0), value.NewInt(11),
		},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "eleven", ReturnsValue: true, Instructions: callee.build(),
		}},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bRecursiveEnumArgumentAndReturn(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpEnum, 0, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpEnumTag)
	main.emit(code.OpChant)

	recurse := newM3TestAsm()
	recurse.emit(code.OpGetLocal, 0)
	recurse.emit(code.OpConstant, 1)
	recurse.emit(code.OpGreater)
	recurse.jump(code.OpJumpIfFalse, "base")
	recurse.emit(code.OpGetLocal, 1)
	recurse.emit(code.OpEnum, 9, 1)
	recurse.emit(code.OpSetLocal, 2)
	recurse.emit(code.OpGetLocal, 0)
	recurse.emit(code.OpConstant, 2)
	recurse.emit(code.OpSub)
	recurse.emit(code.OpGetLocal, 2)
	recurse.emit(code.OpCall, 0)
	recurse.emit(code.OpReturnValue)
	recurse.label("base")
	recurse.emit(code.OpGetLocal, 1)
	recurse.emit(code.OpReturnValue)
	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(32), value.NewInt(0), value.NewInt(1)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "nest", ParameterCount: 2, LocalCount: 3,
			ReturnsValue: true, Instructions: recurse.build(),
		}},
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bAutomaticGCReclaimsEnumsAndCycles(t *testing.T) {
	a := newM3TestAsm()
	// array -> enum -> array cycle, then both roots become nil.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindEnum))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpEnum, 5, 1)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpArrayPush)
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 1)
	// More than one heap of unreachable enum objects must be reclaimed.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 2)
	a.label("loop")
	a.emit(code.OpEnum, 0, 0)
	a.emit(code.OpSetGlobal, 3)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "done")
	a.jump(code.OpJump, "loop")
	a.label("done")
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 4,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(30000)},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13bRetainedEnumChainReportsOOMSafely(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	a := newM3TestAsm()
	a.emit(code.OpEnum, 1, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 1)
	a.label("loop")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpEnum, 0, 1)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "done")
	a.jump(code.OpJump, "loop")
	a.label("done")
	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(40000)},
		Instructions: a.build(),
	}
	output, err := runM12Native(t, program)
	if err == nil {
		t.Fatalf("retained enum chain unexpectedly fit in heap: %q", output)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("unexpected native failure: %v", err)
	}
	if !bytes.Contains(output, []byte("allocation failed")) {
		t.Fatalf("missing OOM diagnostic: %q", output)
	}
}

func TestM13bNullableNilUnwrapFailsSafely(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	a := newM3TestAsm()
	a.emit(code.OpEnum, 0, 0)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpNil)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpEnumTag)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{GlobalsCount: 1, Instructions: a.build()}
	output, err := runM12Native(t, program)
	if err == nil {
		t.Fatalf("nil enum tag unexpectedly succeeded: %q", output)
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("unexpected native failure: %v", err)
	}
	if !bytes.Contains(output, []byte("nullable value is nil")) {
		t.Fatalf("missing nil diagnostic: %q", output)
	}
}

func TestM13bRejectsInvalidEnumBytecode(t *testing.T) {
	tests := []struct {
		name    string
		program *compiler.Bytecode
		want    string
	}{
		{"payload-underflow", &compiler.Bytecode{Instructions: mustM12Instruction(t, code.OpEnum, 0, 1)}, "operand stack underflow"},
		{"tag-on-scalar", &compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: append(mustM12Instruction(t, code.OpConstant, 0), byte(code.OpEnumTag), byte(code.OpPop))}, "type mismatch"},
		{"payload-index", &compiler.Bytecode{Instructions: append(append(mustM12Instruction(t, code.OpEnum, 0, 0), mustM12Instruction(t, code.OpGetEnumValue, 0)...), byte(code.OpPop))}, "outside known layout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileDarwinARM64(test.program)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}
}
