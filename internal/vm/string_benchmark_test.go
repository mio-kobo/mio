package vm

import (
	"fmt"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func benchmarkSingleValueOperation(b *testing.B, input value.Value, operation code.Opcode, operand int, size int64) {
	b.Helper()
	constant, _ := code.Make(code.OpConstant, 0)
	var instruction code.Instructions
	if operation == code.OpDataConvert {
		instruction, _ = code.Make(operation, operand)
	} else {
		instruction, _ = code.Make(operation)
	}
	pop, _ := code.Make(code.OpPop)
	instructions := append(constant, instruction...)
	instructions = append(instructions, pop...)
	bytecode := &compiler.Bytecode{Instructions: instructions, Constants: []value.Value{input}}
	machine := New(bytecode)
	b.ReportAllocs()
	b.SetBytes(size)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStringDataOperationsVM 测量底层操作的 VM 执行成本
func BenchmarkStringDataOperationsVM(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		text := strings.Repeat("a", size)
		b.Run(fmt.Sprintf("to-bytes/%d", size), func(b *testing.B) {
			benchmarkSingleValueOperation(b, value.NewString(text), code.OpDataConvert, code.DataStringToBytes, int64(size))
		})
		array, err := value.NewByteArrayFromString(text)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("from-utf8/%d", size), func(b *testing.B) {
			benchmarkSingleValueOperation(b, array, code.OpDataConvert, code.DataBytesToString, int64(size))
		})
		b.Run(fmt.Sprintf("byte-len/%d", size), func(b *testing.B) {
			benchmarkSingleValueOperation(b, value.NewString(text), code.OpLen, 0, int64(size))
		})
	}
}
