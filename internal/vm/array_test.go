package vm

import (
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试数组指令的运行时类型保护
func TestVMArrayRuntimeGuards(t *testing.T) {
	tests := []struct {
		name      string
		constants []value.Value
		ins       code.Instructions
		want      string
	}{
		{
			name: "构造时栈下溢",
			ins:  code.Instructions{byte(code.OpArray), 0, 1},
			want: "stack underflow for OpArray",
		},
		{
			name:      "索引不是整数",
			constants: []value.Value{value.NewFloat(0)},
			ins: code.Instructions{
				byte(code.OpArray), 0, 0,
				byte(code.OpConstant), 0, 0,
				byte(code.OpIndex),
			},
			want: "requires int64 index",
		},
		{
			name:      "目标不是数组",
			constants: []value.Value{value.NewInt(1), value.NewInt(0)},
			ins: code.Instructions{
				byte(code.OpConstant), 0, 0,
				byte(code.OpConstant), 0, 1,
				byte(code.OpIndex),
			},
			want: "requires array",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := New(&compiler.Bytecode{Constants: test.constants, Instructions: test.ins})
			err := machine.Run()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
			if len(machine.stack) != 0 || len(machine.frames) != 0 || machine.globals != nil {
				t.Fatalf("运行时错误后状态未清理: stack=%v frames=%v globals=%v", machine.stack, machine.frames, machine.globals)
			}
		})
	}
}

// 测试动态数组拒绝非法类型描述符
func TestVMDynamicArrayDescriptorGuard(t *testing.T) {
	instruction := mustVMInstruction(t, code.OpArrayWithCapacity, 127)
	bytecode := &compiler.Bytecode{Instructions: append(mustVMInstruction(t, code.OpConstant, 0), instruction...), Constants: []value.Value{value.NewInt(0)}}
	err := New(bytecode).Run()
	if err == nil || !strings.Contains(err.Error(), "invalid element descriptor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 测试数组长度与赋值指令拒绝非法字节码状态
func TestVMArrayMutationRuntimeGuards(t *testing.T) {
	tests := []struct {
		name      string
		constants []value.Value
		ins       code.Instructions
		want      string
	}{
		{name: "长度栈下溢", ins: code.Instructions{byte(code.OpLen)}, want: "stack underflow for OpLen"},
		{
			name:      "长度目标不是数组",
			constants: []value.Value{value.NewInt(1)},
			ins:       append(mustVMInstruction(t, code.OpConstant, 0), byte(code.OpLen)),
			want:      "OpLen requires array",
		},
		{name: "赋值栈下溢", ins: code.Instructions{byte(code.OpSetIndex)}, want: "stack underflow for OpSetIndex"},
		{
			name:      "赋值索引不是整数",
			constants: []value.Value{value.NewFloat(0), value.NewInt(2)},
			ins:       append(append(append(mustVMInstruction(t, code.OpArray, 0), mustVMInstruction(t, code.OpConstant, 0)...), mustVMInstruction(t, code.OpConstant, 1)...), byte(code.OpSetIndex)),
			want:      "OpSetIndex requires int64 index",
		},
		{
			name:      "赋值类型不一致",
			constants: []value.Value{value.NewInt(1), value.NewInt(0), value.NewString("Mio")},
			ins:       append(append(append(append(mustVMInstruction(t, code.OpConstant, 0), mustVMInstruction(t, code.OpArray, 1)...), mustVMInstruction(t, code.OpConstant, 1)...), mustVMInstruction(t, code.OpConstant, 2)...), byte(code.OpSetIndex)),
			want:      "OpSetIndex requires int64 value, got string",
		},
		{
			name:      "赋值越界",
			constants: []value.Value{value.NewInt(1), value.NewInt(2), value.NewInt(3)},
			ins:       append(append(append(append(mustVMInstruction(t, code.OpConstant, 0), mustVMInstruction(t, code.OpArray, 1)...), mustVMInstruction(t, code.OpConstant, 1)...), mustVMInstruction(t, code.OpConstant, 2)...), byte(code.OpSetIndex)),
			want:      "array index 2 out of bounds for length 1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := New(&compiler.Bytecode{Constants: test.constants, Instructions: test.ins})
			err := machine.Run()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
			if len(machine.stack) != 0 || len(machine.frames) != 0 || machine.globals != nil {
				t.Fatalf("运行时错误后状态未清理: stack=%v frames=%v globals=%v", machine.stack, machine.frames, machine.globals)
			}
		})
	}
}

func mustVMInstruction(t *testing.T, op code.Opcode, operands ...int) code.Instructions {
	t.Helper()
	ins, err := code.Make(op, operands...)
	if err != nil {
		t.Fatal(err)
	}
	return ins
}
