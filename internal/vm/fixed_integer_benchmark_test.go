package vm

import (
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测量固定宽度整数算术的执行成本
func BenchmarkFixedIntegerExecution(b *testing.B) {
	left := value.NewUint8(255)
	right := value.NewUint8(1)
	bytecode := &compiler.Bytecode{
		Constants:    []value.Value{left, right},
		Instructions: append(append(append(code.Instructions{}, mustInstruction(b, code.OpConstant, 0)...), mustInstruction(b, code.OpConstant, 1)...), append(mustInstruction(b, code.OpAdd), mustInstruction(b, code.OpPop)...)...),
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(len(bytecode.Instructions)), "bytecode-bytes/op")
	for i := 0; i < b.N; i++ {
		machine := New(bytecode)
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func mustInstruction(b *testing.B, opcode code.Opcode, operands ...int) code.Instructions {
	b.Helper()
	instruction, err := code.Make(opcode, operands...)
	if err != nil {
		b.Fatal(err)
	}
	return instruction
}
