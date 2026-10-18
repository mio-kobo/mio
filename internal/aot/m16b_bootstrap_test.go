package aot

import (
	"bytes"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func TestM16bSpecializesGenericArrayWrapperAtCallSites(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpArrayTyped, 0, int(value.KindInt))
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpArrayTyped, 0, int(value.KindString))
	main.emit(code.OpSetGlobal, 1)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpGetGlobal, 1)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpCall, 0)
	for global := range 2 {
		main.emit(code.OpGetGlobal, global)
		main.emit(code.OpConstant, 2)
		main.emit(code.OpIndex)
		main.emit(code.OpChant)
	}

	push := newM3TestAsm()
	push.emit(code.OpGetLocal, 0)
	push.emit(code.OpGetLocal, 1)
	push.emit(code.OpArrayPush)
	push.emit(code.OpReturn)

	// This library-shaped callback function is not referenced from main. Its
	// abstract function field must not block compilation of the reachable set.
	unused := newM3TestAsm()
	unused.emit(code.OpGetLocal, 0)
	unused.emit(code.OpGetField, 0)
	unused.emit(code.OpCallValue, 0)
	unused.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants:    []value.Value{value.NewInt(7), value.NewString("x"), value.NewInt(0)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "push", Instructions: push.build(), ParameterCount: 2, LocalCount: 2},
			{ID: 1, Name: "unused_callback", Instructions: unused.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
		},
	}
	got, err := runM12Native(t, program)
	if err != nil {
		t.Fatalf("native execution failed: %v output=%q", err, got)
	}
	if want := []byte("7\nx\n"); !bytes.Equal(got, want) {
		t.Fatalf("native output=%q want=%q", got, want)
	}
}
