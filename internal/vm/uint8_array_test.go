package vm

import (
	"bytes"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试紧凑 uint8 数组贯穿函数、闭包、结构体与泛型
func TestVMUint8ArrayCompositeBehavior(t *testing.T) {
	output := runFunctionSource(t, `
kata ByteBox { values: []uint8; }
waza make_bytes() -> []uint8 { kaesu [0, 1, 255]; }
waza replace<T>(values: []T, index: int64, replacement: T) -> T {
    values[index] = replacement;
    kaesu values[index];
}
waza make_writer(values: []uint8) -> () -> uint8 {
    kaesu waza () -> uint8 { values[1] = 255; kaesu values[1]; };
}
kaku bytes = make_bytes();
kaku alias = bytes;
kaku box = ByteBox { values: bytes };
kaku writer = make_writer(alias);
chant(replace[uint8](box.values, 0, 255));
chant(writer());
chant(bytes[0]);
chant(bytes[1]);
chant(len(box.values));
`)
	if output != "255\n255\n255\n255\n3\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试紧凑 uint8 数组循环读写并与普通数组共存
func TestVMUint8ArrayLoopAndCoexistence(t *testing.T) {
	output := runFunctionSource(t, `
kaku bytes: []uint8 = [0, 255, 1, 2];
kaku integers: []int64 = [10, 20];
kaku floats: []float32 = [1.5, 2.5];
kaku strings: []string = ["Mio", "Mochi"];
kaku index = 0;
kaku total: uint8 = 0;
aida (index < 10000) {
    kaku slot = index - (index / 4) * 4;
    bytes[slot] = bytes[slot] + uint8(1);
    total = total + bytes[slot];
    index = index + 1;
}
chant(total);
chant(integers[1]);
chant(floats[0]);
chant(strings[1]);
`)
	if output != "48\n20\n1.5\nMochi\n" {
		t.Fatalf("output=%q", output)
	}
}

// 测试 uint8 数组索引越界保持原有错误
func TestVMUint8ArrayBoundsError(t *testing.T) {
	bytecode := compileFunctionSource(t, `kaku values: []uint8 = [0, 255]; chant(values[2]);`)
	err := NewWithOutput(bytecode, &bytes.Buffer{}).Run()
	if err == nil || !strings.Contains(err.Error(), "array index 2 out of bounds for length 2") {
		t.Fatalf("error=%v", err)
	}
}

// 测试大型数组构造后释放临时 Value Stack
func TestVMLargeArrayReleasesOperandStack(t *testing.T) {
	bytecode := uint8ArrayBenchmarkBytecode(t, arrayStackReleaseThreshold)
	machine := NewWithOutput(bytecode, &bytes.Buffer{})
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if capacity := cap(machine.stack); capacity > 1 {
		t.Fatalf("stack capacity=%d", capacity)
	}
}

var benchmarkVM *VM

// 测量包含 VM 创建的 uint8 数组端到端执行成本
func BenchmarkVMUint8ArrayEndToEnd1024(b *testing.B) {
	bytecode := uint8ArrayBenchmarkBytecode(b, 1024)
	b.ReportAllocs()
	for iteration := 0; iteration < b.N; iteration++ {
		machine := NewWithOutput(bytecode, &bytes.Buffer{})
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
		benchmarkVM = machine
	}
}

func uint8ArrayBenchmarkBytecode(tb testing.TB, size int) *compiler.Bytecode {
	tb.Helper()
	constants := make([]value.Value, 256)
	for index := range constants {
		constants[index] = value.NewUint8(uint8(index))
	}
	instructions := make(code.Instructions, 0, size*3+4)
	for index := 0; index < size; index++ {
		instruction, err := code.Make(code.OpConstant, index&255)
		if err != nil {
			tb.Fatal(err)
		}
		instructions = append(instructions, instruction...)
	}
	arrayInstruction, err := code.Make(code.OpArray, size)
	if err != nil {
		tb.Fatal(err)
	}
	instructions = append(instructions, arrayInstruction...)
	instructions = append(instructions, byte(code.OpPop))
	return &compiler.Bytecode{Constants: constants, Instructions: instructions}
}
