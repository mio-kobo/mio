package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
	"Mio/internal/vm"
)

const utf8BenchmarkSource = `yobu "kiso/array"; yobu "kiso/utf8"; kaku data = array.make[uint8](1024, uint8(65)); utf8.valid(data);`

var benchmarkUTF8VM *vm.VM

// BenchmarkKisoUTF8Compile 测量已检查程序的字节码编译成本
func BenchmarkKisoUTF8Compile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, utf8BenchmarkSource)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := compileProgram(program, resolver); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkKisoUTF8VMCreation 测量 UTF-8 字节码的 VM 创建成本
func BenchmarkKisoUTF8VMCreation(b *testing.B) {
	bytecode := compileUTF8Benchmark(b, utf8BenchmarkSource)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkUTF8VM = vm.NewWithOutput(bytecode, io.Discard)
	}
}

// BenchmarkKisoUTF8PreconstructedASCII 测量预构造输入后的 UTF-8 扫描
func BenchmarkKisoUTF8PreconstructedASCII(b *testing.B) {
	for _, size := range []int64{1 << 10, 64 << 10, 1 << 20} {
		for _, operation := range []string{"valid", "rune_count"} {
			b.Run(fmt.Sprintf("%s-%d", operation, size), func(b *testing.B) {
				base := compileUTF8Benchmark(b, fmt.Sprintf(`yobu "kiso/utf8"; utf8.%s([uint8(65)]);`, operation))
				bytecode := preconstructedUTF8Bytecode(b, base, operation, size)
				machine := vm.NewWithOutput(bytecode, io.Discard)
				b.ReportAllocs()
				b.SetBytes(size)
				bytecodeBytes := instructionBytes(bytecode)
				var encoded bytes.Buffer
				if err := mochi.Encode(&encoded, base); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				b.ReportMetric(float64(bytecodeBytes), "bytecode-B")
				b.ReportMetric(float64(encoded.Len()), "mochi-B")
				if os.Getenv("MIO_BENCH_INSTRUCTIONS") != "" {
					b.StopTimer()
					instructions := measureUTF8Instructions(b, bytecode, uint64(size)*256+10_000)
					b.ReportMetric(float64(instructions), "instructions/op")
					b.StartTimer()
				}
				for range b.N {
					if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: uint64(size)*256 + 10_000}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func measureUTF8Instructions(b *testing.B, bytecode *compiler.Bytecode, upper uint64) uint64 {
	b.Helper()
	lower := uint64(1)
	for lower < upper {
		middle := lower + (upper-lower)/2
		err := vm.NewWithOutput(bytecode, io.Discard).RunWithOptions(vm.ExecutionOptions{InstructionLimit: middle})
		if err == nil {
			upper = middle
			continue
		}
		if !strings.Contains(err.Error(), "instruction limit") {
			b.Fatal(err)
		}
		lower = middle + 1
	}
	if err := vm.NewWithOutput(bytecode, io.Discard).RunWithOptions(vm.ExecutionOptions{InstructionLimit: lower}); err != nil {
		b.Fatal(err)
	}
	return lower
}

func compileUTF8Benchmark(b *testing.B, source string) *compiler.Bytecode {
	b.Helper()
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, source)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		b.Fatal(err)
	}
	return bytecode
}

func preconstructedUTF8Bytecode(b *testing.B, base *compiler.Bytecode, operation string, size int64) *compiler.Bytecode {
	b.Helper()
	functionID := -1
	for index, function := range base.Functions {
		if strings.HasSuffix(function.Name, "::"+operation) {
			functionID = index
			break
		}
	}
	if functionID < 0 {
		b.Fatalf("找不到 UTF-8 函数 %s", operation)
	}
	input, err := value.NewFilledArray(size, value.NewUint8(65), value.KindUint8, false)
	if err != nil {
		b.Fatal(err)
	}
	constants := append([]value.Value(nil), base.Constants...)
	constantIndex := len(constants)
	constants = append(constants, input)
	instructions := append(code.Instructions{}, benchmarkCodeInstruction(b, code.OpConstant, constantIndex)...)
	instructions = append(instructions, benchmarkCodeInstruction(b, code.OpCall, functionID)...)
	instructions = append(instructions, byte(code.OpPop))
	return &compiler.Bytecode{
		Instructions: instructions,
		Constants:    constants,
		GlobalsCount: base.GlobalsCount,
		Functions:    base.Functions,
	}
}

func benchmarkCodeInstruction(b *testing.B, op code.Opcode, operands ...int) code.Instructions {
	b.Helper()
	instruction, err := code.Make(op, operands...)
	if err != nil {
		b.Fatal(err)
	}
	return instruction
}
