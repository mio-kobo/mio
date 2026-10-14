package vm

import (
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试 OpIsNil 的运行时语义和栈保护
func TestVMIsNil(t *testing.T) {
	for _, test := range []struct {
		name string
		ins  code.Instructions
		want bool
	}{
		{name: "mu", ins: code.Instructions{byte(code.OpNil), byte(code.OpIsNil), byte(code.OpPop)}, want: true},
		{name: "非空", ins: append(append(mustVMInstruction(t, code.OpConstant, 0), byte(code.OpIsNil)), byte(code.OpPop)), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			machine := New(&compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: test.ins})
			if err := machine.Run(); err != nil {
				t.Fatal(err)
			}
			result, ok := machine.LastPoppedValue()
			if !ok {
				t.Fatal("缺少结果")
			}
			boolean, ok := result.Bool()
			if !ok || boolean != test.want {
				t.Fatalf("result=%v", result)
			}
		})
	}

	machine := New(&compiler.Bytecode{Instructions: code.Instructions{byte(code.OpIsNil)}})
	if err := machine.Run(); err == nil || !strings.Contains(err.Error(), "stack underflow for OpIsNil") {
		t.Fatalf("error=%v", err)
	}
}

// 测试可空数组元素赋值允许在值与 mu 之间切换
func TestVMSetNullableIndex(t *testing.T) {
	instructions := append(mustVMInstruction(t, code.OpConstant, 0), mustVMInstruction(t, code.OpArray, 1)...)
	instructions = append(instructions, mustVMInstruction(t, code.OpSetGlobal, 0)...)
	instructions = append(instructions, mustVMInstruction(t, code.OpGetGlobal, 0)...)
	instructions = append(instructions, mustVMInstruction(t, code.OpConstant, 1)...)
	instructions = append(instructions, byte(code.OpNil), byte(code.OpSetNullableIndex))
	instructions = append(instructions, mustVMInstruction(t, code.OpGetGlobal, 0)...)
	instructions = append(instructions, mustVMInstruction(t, code.OpConstant, 1)...)
	instructions = append(instructions, mustVMInstruction(t, code.OpConstant, 2)...)
	instructions = append(instructions, byte(code.OpSetNullableIndex))
	machine := New(&compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(10), value.NewInt(0), value.NewInt(20)},
		GlobalsCount: 1,
		Instructions: instructions,
	})
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}

	bad := append(mustVMInstruction(t, code.OpConstant, 0), mustVMInstruction(t, code.OpArray, 1)...)
	bad = append(bad, mustVMInstruction(t, code.OpConstant, 1)...)
	bad = append(bad, mustVMInstruction(t, code.OpConstant, 2)...)
	bad = append(bad, byte(code.OpSetNullableIndex))
	machine = New(&compiler.Bytecode{Constants: []value.Value{value.NewInt(10), value.NewInt(0), value.NewString("bad")}, Instructions: bad})
	if err := machine.Run(); err == nil || !strings.Contains(err.Error(), "requires int64 or nil value") {
		t.Fatalf("error=%v", err)
	}
}
