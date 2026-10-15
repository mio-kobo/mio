package vm

import (
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

var benchmarkDynamicVM *VM

// BenchmarkDynamicArrayVMInitialization 测量 VM 初始化
func BenchmarkDynamicArrayVMInitialization(b *testing.B) {
	bytecode := &compiler.Bytecode{}
	b.ReportAllocs()
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkDynamicVM = New(bytecode)
	}
}

// BenchmarkDynamicArrayVMOperation 测量 VM 数组创建
func BenchmarkDynamicArrayVMOperation(b *testing.B) {
	length, _ := code.Make(code.OpConstant, 0)
	initial, _ := code.Make(code.OpConstant, 1)
	makeArray, _ := code.Make(code.OpArrayMake, int(value.KindUint8))
	pop, _ := code.Make(code.OpPop)
	instructions := append(length, initial...)
	instructions = append(instructions, makeArray...)
	instructions = append(instructions, pop...)
	bytecode := &compiler.Bytecode{Instructions: instructions, Constants: []value.Value{value.NewInt(1 << 20), value.NewUint8(0)}}
	b.ReportAllocs()
	b.SetBytes(1 << 20)
	for iteration := 0; iteration < b.N; iteration++ {
		machine := New(bytecode)
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
		benchmarkDynamicVM = machine
	}
}
