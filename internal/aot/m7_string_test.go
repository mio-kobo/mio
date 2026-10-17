package aot

import (
	"bytes"
	"debug/macho"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func TestM7StaticStringThroughGlobalFunctionAndPop(t *testing.T) {
	fn := compiler.CompiledFunction{
		ID: 0, Name: "echo", ParameterCount: 1, LocalCount: 2, ReturnsValue: true,
		Instructions: code.Instructions{
			byte(code.OpGetLocal), 0, 0,
			byte(code.OpDefineLocal), 0, 1,
			byte(code.OpGetLocal), 0, 1,
			byte(code.OpReturnValue),
		},
	}
	p := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewString("你好，Mio！"), value.NewString("a\x00b"),
			value.NewString("你好，Mio！"), value.NewString(""),
		},
		GlobalsCount: 1,
		Functions:    []compiler.CompiledFunction{fn},
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpSetGlobal), 0, 0,
			byte(code.OpGetGlobal), 0, 0,
			byte(code.OpCall), 0, 0, byte(code.OpChant),
			byte(code.OpConstant), 0, 1, byte(code.OpChant),
			byte(code.OpConstant), 0, 0,
			byte(code.OpConstant), 0, 2, byte(code.OpEqual), byte(code.OpChant),
			byte(code.OpConstant), 0, 3, byte(code.OpPop),
		},
	}
	image, err := CompileDarwinARM64(p)
	if err != nil {
		t.Fatal(err)
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ro := m.Section("__const")
	if ro == nil || ro.Size < 3*8 {
		t.Fatalf("expected deduplicated __const, got %+v", ro)
	}
	if bytes.Count(image[ro.Offset:ro.Offset+uint32(ro.Size)], []byte("你好，Mio！")) != 1 {
		t.Fatal("duplicate literal should be interned")
	}
}

func TestM7RejectsStringArithmeticAndOrdering(t *testing.T) {
	for _, tc := range []struct {
		op   code.Opcode
		want string
	}{
		{code.OpAdd, "unsupported arithmetic type"},
		{code.OpLess, "unsupported arithmetic type"},
	} {
		p := &compiler.Bytecode{
			Constants: []value.Value{value.NewString("a"), value.NewString("b")},
			Instructions: code.Instructions{
				byte(code.OpConstant), 0, 0, byte(code.OpConstant), 0, 1,
				byte(tc.op), byte(code.OpChant),
			},
		}
		_, err := CompileDarwinARM64(p)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d expected %q, got %v", tc.op, tc.want, err)
		}
	}
}
