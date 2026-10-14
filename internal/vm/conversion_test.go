package vm

import (
	"math"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试 int64 到 float64 的 IEEE 754 转换
func TestVMIntToFloatConversion(t *testing.T) {
	values := []int64{0, 42, -42, 9007199254740993, math.MaxInt64, math.MinInt64}
	for _, integer := range values {
		bytecode := conversionBytecode(t, value.NewInt(integer), code.OpIntToFloat)
		machine := New(bytecode)
		if err := machine.Run(); err != nil {
			t.Fatal(err)
		}
		result, ok := machine.LastPoppedValue()
		floating, floatOK := result.Float64()
		if !ok || !floatOK || math.Float64bits(floating) != math.Float64bits(float64(integer)) {
			t.Fatalf("int64 %d converted to %v", integer, result)
		}
	}
}

// 测试 float64 到 int64 向零截断及边界
func TestVMFloatToIntConversion(t *testing.T) {
	tests := []struct {
		floating float64
		want     int64
	}{
		{3.9, 3},
		{-3.9, -3},
		{0, 0},
		{math.Copysign(0, -1), 0},
		{math.Ldexp(1, 63) - 1024, math.MaxInt64 - 1023},
		{-math.Ldexp(1, 63), math.MinInt64},
	}
	for _, test := range tests {
		machine := New(conversionBytecode(t, value.NewFloat(test.floating), code.OpFloatToInt))
		if err := machine.Run(); err != nil {
			t.Fatalf("float64 %g: %v", test.floating, err)
		}
		result, ok := machine.LastPoppedValue()
		integer, intOK := result.Int64()
		if !ok || !intOK || integer != test.want {
			t.Fatalf("float64 %g converted to %v, want %d", test.floating, result, test.want)
		}
	}
}

// 测试 float64 到 int64 拒绝特殊值和越界值
func TestVMFloatToIntRejectsInvalidValues(t *testing.T) {
	values := []float64{
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
		math.Ldexp(1, 63),
		-math.Ldexp(1, 63) - 2048,
	}
	for _, floating := range values {
		machine := New(conversionBytecode(t, value.NewFloat(floating), code.OpFloatToInt))
		err := machine.Run()
		if err == nil || !strings.Contains(err.Error(), "outside int64 range") {
			t.Fatalf("float64 %g error=%v", floating, err)
		}
		if len(machine.stack) != 0 {
			t.Fatalf("failed conversion left stack values: %v", machine.stack)
		}
	}
}

// 测试转换指令保持运行时类型与栈安全
func TestVMConversionRuntimeGuards(t *testing.T) {
	tests := []struct {
		bytecode *compiler.Bytecode
		want     string
	}{
		{&compiler.Bytecode{Instructions: code.Instructions{byte(code.OpIntToFloat)}}, "stack underflow"},
		{conversionBytecode(t, value.NewFloat(1), code.OpIntToFloat), "requires int64"},
		{conversionBytecode(t, value.NewInt(1), code.OpFloatToInt), "requires float64"},
	}
	for _, test := range tests {
		err := New(test.bytecode).Run()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("error=%v, want containing %q", err, test.want)
		}
	}
}

func conversionBytecode(t *testing.T, constant value.Value, opcode code.Opcode) *compiler.Bytecode {
	t.Helper()
	constantInstruction, err := code.Make(code.OpConstant, 0)
	if err != nil {
		t.Fatal(err)
	}
	conversionInstruction, err := code.Make(opcode)
	if err != nil {
		t.Fatal(err)
	}
	popInstruction, err := code.Make(code.OpPop)
	if err != nil {
		t.Fatal(err)
	}
	instructions := append(code.Instructions{}, constantInstruction...)
	instructions = append(instructions, conversionInstruction...)
	instructions = append(instructions, popInstruction...)
	return &compiler.Bytecode{Constants: []value.Value{constant}, Instructions: instructions}
}
