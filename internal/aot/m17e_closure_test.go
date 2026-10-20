package aot

import (
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/stdlib"
	"Mio/internal/value"
	"Mio/internal/vm"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func requireM17eClosure(t *testing.T, p *compiler.Bytecode) {
	t.Helper()
	requireM17eClosureWithResult(t, p, nil)
}

func requireM17eClosureWithResult(t *testing.T, p *compiler.Bytecode, verify func()) {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	want := m17RunVM(t, p, 100_000_000)
	if verify != nil {
		verify()
	}
	image, err := CompileDarwinARM64WithOptions(p, DarwinARM64Options{GCStress: true})
	if err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(t.TempDir(), "go-reference")
	if err := os.WriteFile(reference, image, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--force", "--sign", "-", reference}, {"--verify", "--strict", reference}} {
		if out, err := exec.Command("/usr/bin/codesign", args...).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v %s", err, out)
		}
	}
	for _, path := range []string{reference, m17eArrayImage(t, p)} {
		cmd := exec.Command(path)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil || !bytes.Equal(stdout.Bytes(), want) || stderr.Len() != 0 || cmd.ProcessState.ExitCode() != 0 {
			t.Fatalf("%s stdout=%q stderr=%q err=%v VM=%q", filepath.Base(path), stdout.Bytes(), stderr.Bytes(), err, want)
		}
		if verify != nil {
			verify()
		}
		if evidence := m17eEvidenceDir(t); evidence != "" {
			m17eWriteJSON(t, filepath.Join(evidence, filepath.Base(path)+"-streams.json"), map[string]any{"stdout": stdout.String(), "stderr": stderr.String(), "exit_code": cmd.ProcessState.ExitCode()})
		}
	}

}
func TestM17ePureMioMutableCapture(t *testing.T) { requireM17eClosure(t, m15bCounterProgram()) }

func TestM17ePureMioUncapturedFunctionValueDifferential(t *testing.T) {
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
	requireM17eClosure(t, &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(21), value.NewInt(2)}, Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{ID: 0, Name: "double", Instructions: fn.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true}},
	})
}

func TestM17ePureMioMultipleCompositeCapturesGCStress(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioSharedCellAndNestedForwarding(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioClosureCallsSixteenArgumentFunction(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioNestedCaptureReturnsClosure(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioClosureAsFunctionParameter(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioRecursiveClosureDeepShadowFrames(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioDefineLocalCreatesIndependentLoopCells(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioUnitClosureStackBalance(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioMutuallyRecursiveClosures(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioUnreachableClosuresAreCollected(t *testing.T) {
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
	requireM17eClosure(t, program)
}

func TestM17ePureMioClosureSixteenVisibleArguments(t *testing.T) {
	main, maker, closure := newM3TestAsm(), newM3TestAsm(), newM3TestAsm()
	main.emit(code.OpCall, 0)
	for i := 0; i < 16; i++ {
		main.emit(code.OpConstant, i)
	}
	main.emit(code.OpCallValue, 16)
	main.emit(code.OpChant)
	maker.emit(code.OpConstant, 16)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)
	closure.emit(code.OpGetUpvalue, 0)
	for i := 0; i < 16; i++ {
		closure.emit(code.OpGetLocal, i)
		closure.emit(code.OpAdd)
	}
	// Allocate while all arguments and environment must survive the call.
	closure.emit(code.OpStruct, 1)
	closure.emit(code.OpGetField, 0)
	closure.emit(code.OpReturnValue)
	constants := make([]value.Value, 17)
	for i := range constants {
		constants[i] = value.NewInt(int64(i + 1))
	}
	requireM17eClosure(t, &compiler.Bytecode{Constants: constants, Instructions: main.build(), Functions: []compiler.CompiledFunction{
		{ID: 0, Name: "maker16", LocalCount: 1, ReturnsValue: true, Instructions: maker.build()},
		{ID: 1, Name: "closure16", ParameterCount: 16, LocalCount: 16, ReturnsValue: true, Instructions: closure.build(), Captures: []compiler.Capture{{Local: true, Index: 0}}},
	}})
}

func TestM17ePureMioIOWriteAll(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "closure-書き込み.bin")
	source := fmt.Sprintf(`yobu "kiso/fs"; yobu "kiso/io";
 waza exercise(file: fs.File) -> unit {
   kaku writer = fs.writer(file);
   kaku data: []uint8 = [77, 0, 255, 228, 184, 173, 10];
   kaku result = io.write_all(writer, data, 0, len(data));
   chant(result.count); chant(result.error == mu);
   kaku invalid = io.write_all(writer, data, -1, 1); chant(invalid.error != mu);
   kaku closed = fs.close(file); chant(closed.error == mu);
 }
 kaku opened = fs.open_write(%q); kaku file = opened.file; moshi (file != mu) { exercise(file); }`, path)
	verify := func() {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, []byte{77, 0, 255, 228, 184, 173, 10}) {
			t.Fatalf("file=%v err=%v", got, err)
		}
	}
	requireM17eClosureWithResult(t, m17CompileMio(t, root, source), verify)
}

func TestM17ePureMioDataConversion(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	p := m17CompileMio(t, root, `yobu "kiso/string"; yobu "kiso/array";
 waza check_copy(text: string) -> unit { chant(text); kaku copy = string.to_bytes(text); chant(len(copy)); copy[0] = uint8(90); chant(text); chant(text == "A中😀"); }
 kaku data = string.to_bytes("A中😀"); kaku text = string.from_utf8(data);
 moshi (text != mu) { check_copy(text); }
 kaku invalid: []uint8 = [237,160,128]; chant(string.from_utf8(invalid) == mu);
 kaku overlong: []uint8 = [224,128,128]; chant(string.from_utf8(overlong) == mu);
 kaku truncated: []uint8 = [240,144,128]; chant(string.from_utf8(truncated) == mu);
 kaku empty: []uint8 = []; chant(string.from_utf8(empty) != mu);
 `)
	requireM17eClosure(t, p)
}

func TestM17ePureMioClosureReferenceReplacementAndSurvival(t *testing.T) {
	main, maker, update, junk := newM3TestAsm(), newM3TestAsm(), newM3TestAsm(), newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	for round := 0; round < 2; round++ {
		for i := 0; i < 400; i++ {
			main.emit(code.OpCall, 2)
		}
		main.emit(code.OpGetGlobal, 0)
		main.emit(code.OpCallValue, 0)
		main.emit(code.OpChant)
	}
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpStruct, 1)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpConstant, 2)
	maker.emit(code.OpSetLocal, 1)
	maker.emit(code.OpConstant, 3)
	maker.emit(code.OpSetLocal, 2)
	maker.emit(code.OpConstant, 3)
	maker.emit(code.OpDataConvert, 1)
	maker.emit(code.OpDataConvert, 2)
	maker.emit(code.OpSetLocal, 3)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpReturnValue)
	update.emit(code.OpGetUpvalue, 0)
	update.emit(code.OpGetField, 0)
	update.emit(code.OpConstant, 1)
	update.emit(code.OpAdd)
	update.emit(code.OpStruct, 1)
	update.emit(code.OpSetUpvalue, 0)
	update.emit(code.OpCall, 2)
	update.emit(code.OpGetUpvalue, 1)
	update.emit(code.OpConstant, 2)
	update.emit(code.OpEqual)
	update.emit(code.OpChant)
	update.emit(code.OpGetUpvalue, 2)
	update.emit(code.OpChant)
	update.emit(code.OpGetUpvalue, 3)
	update.emit(code.OpChant)
	update.emit(code.OpGetUpvalue, 0)
	update.emit(code.OpGetField, 0)
	update.emit(code.OpReturnValue)
	junk.emit(code.OpConstant, 0)
	junk.emit(code.OpStruct, 1)
	junk.emit(code.OpPop)
	junk.emit(code.OpReturn)
	requireM17eClosure(t, &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(40), value.NewInt(1), value.NewFloat(3.25), value.NewString("live-中")}, Instructions: main.build(), Functions: []compiler.CompiledFunction{
		{ID: 0, Name: "make_roots", LocalCount: 4, ReturnsValue: true, Instructions: maker.build()},
		{ID: 1, Name: "replace_roots", ReturnsValue: true, Instructions: update.build(), Captures: []compiler.Capture{{Local: true, Index: 0}, {Local: true, Index: 1}, {Local: true, Index: 2}, {Local: true, Index: 3}}},
		{ID: 2, Name: "allocate_garbage", Instructions: junk.build()},
	}})
}

func TestM17ePureMioFloatConversions(t *testing.T) {
	main := newM3TestAsm()
	for _, op := range []code.Opcode{code.OpIntToFloat, code.OpIntToFloat32} {
		main.emit(code.OpConstant, 0)
		main.emit(op)
		if op == code.OpIntToFloat {
			main.emit(code.OpFloat64ToFloat32)
			main.emit(code.OpFloat32ToFloat64)
			main.emit(code.OpFloatToInt)
		} else {
			main.emit(code.OpFloat32ToInt)
		}
		main.emit(code.OpChant)
	}
	main.emit(code.OpConstant, 1)
	main.emit(code.OpNeg)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpMul)
	main.emit(code.OpFloatToInt)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpConstant, 3)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 4)
	main.emit(code.OpConstant, 4)
	main.emit(code.OpLess)
	main.emit(code.OpChant)
	requireM17eClosure(t, &compiler.Bytecode{Instructions: main.build(), Constants: []value.Value{value.NewInt(42), value.NewFloat(3.5), value.NewFloat(0), value.NewFloat(math.Copysign(0, -1)), value.NewFloat(math.NaN())}})
}

func m17eClosureLowerDiagnostic(t *testing.T, p *compiler.Bytecode) []byte {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, p); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "invalid-call.mochi")
	if err := os.WriteFile(input, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`yobu "kiso/aot/lower";yobu "kiso/encoding/mochi";
waza show(problem: lower.LowerError) -> unit { chant(lower.error_code(problem)); chant(lower.error_detail(problem)); }
 kaku decoded=mochi.decode_file(%q);kaku file=decoded.value;
 moshi(file!=mu){kaku problem=lower.compile_gc_stress(file).error;moshi(problem!=mu){show(problem);}sore{chant(0);}}sore{chant(-1);}`, input)
	builder := m17CompileMio(t, root, source)
	want := m17RunVM(t, builder, 100_000_000)
	got, err := runM12NativeCompile(t, builder, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{HeapCapacity: 128 << 20})
	})
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("diagnostic VM=%q Native Builder=%q err=%v", want, got, err)
	}
	return want
}

func TestM17ePureMioCallValueRejectsInvalidTypesAndArity(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	for _, name := range []string{"integer", "nil-without-target", "arity"} {
		t.Run(name, func(t *testing.T) {
			main := newM3TestAsm()
			fn := newM3TestAsm()
			fn.emit(code.OpConstant, 0)
			fn.emit(code.OpReturnValue)
			p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(7)}}
			switch name {
			case "integer":
				main.emit(code.OpConstant, 0)
			case "nil-without-target":
				main.emit(code.OpNil)
			case "arity":
				main.emit(code.OpFunction, 0)
				p.Functions = []compiler.CompiledFunction{{ID: 0, Name: "one_argument", ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Instructions: fn.build()}, {ID: 1, Name: "zero_arguments", ReturnsValue: true, Instructions: fn.build()}}
			}
			main.emit(code.OpCallValue, 0)
			main.emit(code.OpChant)
			p.Instructions = main.build()
			got := m17eClosureLowerDiagnostic(t, p)
			offset := 3
			if name == "nil-without-target" {
				offset = 1
			}
			want := fmt.Sprintf("8\n%d\n", offset*100+45)
			if string(got) != want {
				t.Fatalf("got=%q want=%q", got, want)
			}
			if _, err := CompileDarwinARM64(p); err == nil {
				t.Fatal("Go AOT accepted invalid call")
			}
		})
	}
}

func TestM17ePureMioNullableFunctionRuntimeError(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	main, fn := newM3TestAsm(), newM3TestAsm()
	main.emit(code.OpFalse)
	main.jump(code.OpJumpIfFalse, "nil")
	main.emit(code.OpFunction, 0)
	main.emit(code.OpSetGlobal, 0)
	main.jump(code.OpJump, "call")
	main.label("nil")
	main.emit(code.OpNil)
	main.emit(code.OpSetGlobal, 0)
	main.label("call")
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpChant)
	fn.emit(code.OpConstant, 0)
	fn.emit(code.OpReturnValue)
	p := &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(42)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{{ID: 0, Name: "value", ReturnsValue: true, Instructions: fn.build()}}}
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	machine := vm.NewWithRegistryAndOutput(p, registry, &stdout)
	vmErr := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: 100_000})
	if vmErr == nil || !strings.Contains(vmErr.Error(), "requires function value") || stdout.Len() != 0 {
		t.Fatalf("VM stdout=%q error=%v", stdout.String(), vmErr)
	}
	gold, err := runM12NativeCompile(t, p, func(p *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(p, DarwinARM64Options{GCStress: true})
	})
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(gold) != 0 {
		t.Fatalf("Go AOT output=%q err=%v", gold, err)
	}
	cmd := exec.Command(m17eArrayImage(t, p))
	var nativeOut, nativeErr bytes.Buffer
	cmd.Stdout = &nativeOut
	cmd.Stderr = &nativeErr
	err = cmd.Run()
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || nativeOut.Len() != 0 || nativeErr.Len() != 0 {
		t.Fatalf("pure Mio stdout=%q stderr=%q err=%v", nativeOut.String(), nativeErr.String(), err)
	}
	t.Logf("VM API error=%q; both native ABIs exit 1 with empty stdout/stderr", vmErr)
}

func TestM17ePureMioEnumVariantPayloads(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	p := m17CompileMio(t, root, `sentaku E { Number(int64); Text(string); }
 waza show(value: E) -> unit {awase(value){baai Number(n){chant(n);}baai Text(s){chant(s);}}}
 show(E.Number(42));show(E.Text("variant-中"));`)
	requireM17eClosure(t, p)
}

func TestM17ePureMioClosureNormalAllocatorCollection(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires Apple Silicon")
	}
	p := m15bCounterProgram()
	p.GlobalsCount = 2
	main := newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	for i := 0; i < 400; i++ {
		main.emit(code.OpCall, 0)
		main.emit(code.OpSetGlobal, 1)
	}
	for i := 0; i < 2; i++ {
		main.emit(code.OpGetGlobal, 0)
		main.emit(code.OpCallValue, 0)
		main.emit(code.OpChant)
	}
	p.Instructions = main.build()
	want := m17RunVM(t, p, 100_000_000)
	got, err := runM12NativeCompile(t, p, CompileDarwinARM64)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Go AOT=%q VM=%q err=%v", got, want, err)
	}
	cmd := exec.Command(m17eArrayImageMode(t, p, false))
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil || !bytes.Equal(out.Bytes(), want) || stderr.Len() != 0 {
		t.Fatalf("pure Mio stdout=%q stderr=%q VM=%q err=%v", out.String(), stderr.String(), want, err)
	}
}

func TestM17ePureMioLocalAndUpvalueAliasUpdates(t *testing.T) {
	main, maker, inc := newM3TestAsm(), newM3TestAsm(), newM3TestAsm()
	main.emit(code.OpCall, 0)
	main.emit(code.OpSetGlobal, 0)
	for i := 0; i < 2; i++ {
		main.emit(code.OpGetGlobal, 0)
		main.emit(code.OpCallValue, 0)
		main.emit(code.OpChant)
	}
	maker.emit(code.OpConstant, 0)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpClosure, 1)
	maker.emit(code.OpSetLocal, 1)
	maker.emit(code.OpConstant, 1)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpCallValue, 0)
	maker.emit(code.OpPop)
	maker.emit(code.OpGetLocal, 0)
	maker.emit(code.OpChant)
	maker.emit(code.OpConstant, 2)
	maker.emit(code.OpSetLocal, 0)
	maker.emit(code.OpGetLocal, 1)
	maker.emit(code.OpReturnValue)
	inc.emit(code.OpGetUpvalue, 0)
	inc.emit(code.OpConstant, 3)
	inc.emit(code.OpAdd)
	inc.emit(code.OpSetUpvalue, 0)
	inc.emit(code.OpGetUpvalue, 0)
	inc.emit(code.OpReturnValue)
	requireM17eClosure(t, &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(40), value.NewInt(41), value.NewInt(43), value.NewInt(1)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{
		{ID: 0, Name: "local_alias", LocalCount: 2, ReturnsValue: true, Instructions: maker.build()},
		{ID: 1, Name: "upvalue_alias", ReturnsValue: true, Instructions: inc.build(), Captures: []compiler.Capture{{Local: true, Index: 0}}},
	}})
}

func TestM17ePureMioIndirectCallPreservesOperandRoot(t *testing.T) {
	main, fn := newM3TestAsm(), newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpStruct, 1)
	main.emit(code.OpFunction, 0)
	main.emit(code.OpCallValue, 0)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetField, 0)
	main.emit(code.OpChant)
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpStruct, 1)
	fn.emit(code.OpPop)
	fn.emit(code.OpConstant, 1)
	fn.emit(code.OpReturnValue)
	requireM17eClosure(t, &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewInt(42), value.NewInt(0)}, Instructions: main.build(), Functions: []compiler.CompiledFunction{{ID: 0, Name: "allocate_during_indirect_call", ReturnsValue: true, Instructions: fn.build()}}})
}
