package vm

import (
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试 VM 拒绝混合数值类型的算术与比较
func TestVMRejectsMixedNumericTypes(t *testing.T) {
	for _, opcode := range []code.Opcode{code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual, code.OpLess} {
		var instructions code.Instructions
		for _, instruction := range []code.Instructions{
			floatInstruction(t, code.OpConstant, 0),
			floatInstruction(t, code.OpConstant, 1),
			floatInstruction(t, opcode),
			floatInstruction(t, code.OpPop),
		} {
			instructions = append(instructions, instruction...)
		}
		machine := New(&compiler.Bytecode{
			Constants:    []value.Value{value.NewInt(1), value.NewFloat(1)},
			Instructions: instructions,
		})
		err := machine.Run()
		if err == nil || (!strings.Contains(err.Error(), "matching") && !strings.Contains(err.Error(), "types")) {
			t.Fatalf("opcode %d error=%v", opcode, err)
		}
	}
}

// floatInstruction 生成浮点运行时测试指令
func floatInstruction(t *testing.T, opcode code.Opcode, operands ...int) code.Instructions {
	t.Helper()
	instruction, err := code.Make(opcode, operands...)
	if err != nil {
		t.Fatal(err)
	}
	return instruction
}
