package vm

import (
	"encoding/binary"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

const benchmarkOperationCount = 10_000

// BenchmarkMioFunctionCall 测量纯 Mio 函数调用成本
func BenchmarkMioFunctionCall(b *testing.B) {
	instructions := make(code.Instructions, 0, benchmarkOperationCount*3)
	for range benchmarkOperationCount {
		instructions = append(instructions, benchmarkInstruction(b, code.OpCall, 0)...)
	}
	bytecode := &compiler.Bytecode{
		Instructions: instructions,
		Functions: []compiler.CompiledFunction{{
			ID:           0,
			Instructions: code.Instructions{byte(code.OpReturn)},
		}},
	}
	machine := New(bytecode)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := machine.RunWithOptions(ExecutionOptions{InstructionLimit: benchmarkOperationCount*2 + 1}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*benchmarkOperationCount), "ns/call")
}

// BenchmarkVMInstructionDispatch 测量无操作数指令执行成本
func BenchmarkVMInstructionDispatch(b *testing.B) {
	const pairs = benchmarkOperationCount
	instructions := make(code.Instructions, 0, pairs*2)
	for range pairs {
		instructions = append(instructions, byte(code.OpTrue), byte(code.OpPop))
	}
	bytecode := &compiler.Bytecode{Instructions: instructions}
	machine := New(bytecode)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := machine.RunWithOptions(ExecutionOptions{InstructionLimit: pairs*2 + 1}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*pairs*2), "ns/instruction")
}

// BenchmarkVMLoopIndex 测量预构造数组的循环索引成本
func BenchmarkVMLoopIndex(b *testing.B) {
	const count = 64 << 10
	array, err := value.NewFilledArray(count, value.NewUint8(65), value.KindUint8, false)
	if err != nil {
		b.Fatal(err)
	}
	bytecode := loopIndexBytecode(b, array, count)
	machine := New(bytecode)
	b.ReportAllocs()
	b.SetBytes(count)
	b.ResetTimer()
	for range b.N {
		if err := machine.RunWithOptions(ExecutionOptions{InstructionLimit: count*16 + 16}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*count), "ns/index")
}

func loopIndexBytecode(b *testing.B, array value.Value, count int) *compiler.Bytecode {
	b.Helper()
	instructions := make(code.Instructions, 0, 48)
	appendInstruction := func(op code.Opcode, operands ...int) int {
		offset := len(instructions)
		instructions = append(instructions, benchmarkInstruction(b, op, operands...)...)
		return offset
	}
	appendInstruction(code.OpConstant, 0)
	appendInstruction(code.OpSetGlobal, 0)
	appendInstruction(code.OpConstant, 1)
	appendInstruction(code.OpSetGlobal, 1)
	loop := len(instructions)
	appendInstruction(code.OpGetGlobal, 1)
	appendInstruction(code.OpConstant, 2)
	appendInstruction(code.OpLess)
	exitJump := appendInstruction(code.OpJumpIfFalse, 0)
	appendInstruction(code.OpGetGlobal, 0)
	appendInstruction(code.OpGetGlobal, 1)
	appendInstruction(code.OpIndex)
	appendInstruction(code.OpPop)
	appendInstruction(code.OpGetGlobal, 1)
	appendInstruction(code.OpConstant, 3)
	appendInstruction(code.OpAdd)
	appendInstruction(code.OpSetGlobal, 1)
	appendInstruction(code.OpJump, loop)
	binary.BigEndian.PutUint16(instructions[exitJump+1:exitJump+3], uint16(len(instructions)))
	return &compiler.Bytecode{
		Instructions: instructions,
		Constants: []value.Value{
			array,
			value.NewInt(0),
			value.NewInt(int64(count)),
			value.NewInt(1),
		},
		GlobalsCount: 2,
	}
}

func benchmarkInstruction(b *testing.B, op code.Opcode, operands ...int) code.Instructions {
	b.Helper()
	instruction, err := code.Make(op, operands...)
	if err != nil {
		b.Fatal(err)
	}
	return instruction
}
