package aot

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func runM11Image(t *testing.T, image []byte) []byte {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	path := t.TempDir() + "/m11-gc"
	if err := os.WriteFile(path, image, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("codesign", "--force", "--sign", "-", path).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, out)
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("native: %v: %q", err, out)
	}
	return out
}

func TestM11AutomaticGCReusesUnreachableObjects(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpSetGlobal, 0) // counter
	a.label("loop")
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpSetGlobal, 1)
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
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	image, err := CompileDarwinARM64(&compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(0), value.NewInt(1), value.NewInt(12000)},
		Instructions: a.build(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := runM11Image(t, image); !bytes.Equal(got, []byte("0\n")) {
		t.Fatalf("output=%q", got)
	}
}

func TestM11GCStressPreservesNestedGraph(t *testing.T) {
	a := newM3TestAsm()
	// g0 = [[42]]; every allocation forces a collection.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArray, 1)
	a.emit(code.OpArray, 1)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpSetGlobal, 2)
	a.label("stress")
	a.emit(code.OpArrayTyped, 0, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "check")
	a.jump(code.OpJump, "stress")
	a.label("check")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 3,
		Constants: []value.Value{
			value.NewInt(42), value.NewInt(0), value.NewInt(32), value.NewInt(1),
		},
		Instructions: a.build(),
	}
	image, err := CompileDarwinARM64WithGCStress(program)
	if err != nil {
		t.Fatal(err)
	}
	if got := runM11Image(t, image); !bytes.Equal(got, []byte("42\n")) {
		t.Fatalf("output=%q", got)
	}
}

func TestM11GCStressAcrossRecursiveFrames(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0) // depth
	main.emit(code.OpConstant, 1)
	main.emit(code.OpArray, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpIndex)
	main.emit(code.OpChant)

	fn := newM3TestAsm()
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 2)
	fn.emit(code.OpGreater)
	fn.jump(code.OpJumpIfFalse, "base")
	fn.emit(code.OpArrayTyped, 0, int(value.KindInt))
	fn.emit(code.OpSetLocal, 2) // dead temporary must not keep prior objects alive
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 3)
	fn.emit(code.OpSub)
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpCall, 0)
	fn.emit(code.OpReturnValue)
	fn.label("base")
	fn.emit(code.OpGetLocal, 1)
	fn.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(48), value.NewInt(55), value.NewInt(0), value.NewInt(1),
		},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "recursive_gc", Instructions: fn.build(),
			ParameterCount: 2, LocalCount: 3, ReturnsValue: true,
		}},
	}
	image, err := CompileDarwinARM64WithGCStress(program)
	if err != nil {
		t.Fatal(err)
	}
	if got := runM11Image(t, image); !bytes.Equal(got, []byte("55\n")) {
		t.Fatalf("output=%q", got)
	}
}

func TestM11GCStressArrayGrowthKeepsReferences(t *testing.T) {
	a := newM3TestAsm()
	// g0=[99], g1=Array<Array>(capacity:1), then grow g1 repeatedly.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArray, 1)
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayWithCapacity, int(value.KindArray))
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayPush)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpSetGlobal, 2)
	a.label("grow")
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpArrayTyped, 0, int(value.KindInt))
	a.emit(code.OpArrayPush)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 2)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "check")
	a.jump(code.OpJump, "grow")
	a.label("check")
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIndex)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 3,
		Constants: []value.Value{
			value.NewInt(99), value.NewInt(0), value.NewInt(1), value.NewInt(80),
		},
		Instructions: a.build(),
	}
	image, err := CompileDarwinARM64WithGCStress(program)
	if err != nil {
		t.Fatal(err)
	}
	if got := runM11Image(t, image); !bytes.Equal(got, []byte("99\n")) {
		t.Fatalf("output=%q", got)
	}
}

func TestM11AutomaticGCCollectsUnreachableCycle(t *testing.T) {
	a := newM3TestAsm()
	// Create a self-cycle in g0, then overwrite the only external root.
	a.emit(code.OpConstant, 0)
	a.emit(code.OpArrayWithCapacity, int(value.KindArray))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayPush)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayWithCapacity, int(value.KindArray))
	a.emit(code.OpSetGlobal, 0)
	// Allocate enough temporary descriptor/storage pairs to require several GCs.
	a.emit(code.OpConstant, 1)
	a.emit(code.OpSetGlobal, 1)
	a.label("cycle_gc")
	a.emit(code.OpConstant, 1)
	a.emit(code.OpArrayWithCapacity, int(value.KindArray))
	a.emit(code.OpSetGlobal, 2)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 0)
	a.emit(code.OpAdd)
	a.emit(code.OpSetGlobal, 1)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "cycle_done")
	a.jump(code.OpJump, "cycle_gc")
	a.label("cycle_done")
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpArrayCapacity)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 3,
		Constants: []value.Value{
			value.NewInt(1), value.NewInt(0), value.NewInt(12000),
		},
		Instructions: a.build(),
	}
	image, err := CompileDarwinARM64(program)
	if err != nil {
		t.Fatal(err)
	}
	if got := runM11Image(t, image); !bytes.Equal(got, []byte("0\n")) {
		t.Fatalf("output=%q", got)
	}
}
