package aot

import (
	"math"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func TestM13cIntegerConvertAndMathDifferential(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpIntegerConvert, code.IntegerUint8)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 1)
	a.emit(code.OpIntegerConvert, code.IntegerInt8)
	a.emit(code.OpIntegerConvert, code.IntegerInt64)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 2)
	a.emit(code.OpMath, code.MathIsInf, 1)
	a.emit(code.OpChant)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpMath, code.MathIsInf, 1)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(255), value.NewInt(-128), value.NewFloat(math.Inf(-1)), value.NewFloat(math.NaN())},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM16bMathPrimitivesDifferential(t *testing.T) {
	a := newM3TestAsm()
	for _, item := range []struct {
		constant  int
		primitive int
	}{
		{0, code.MathSqrt}, {1, code.MathFloor}, {1, code.MathCeil},
		{2, code.MathRoundEven}, {3, code.MathTrunc}, {4, code.MathIsNaN},
		{5, code.MathIsFinite}, {6, code.MathIsNegative}, {7, code.MathNegOverflows},
	} {
		a.emit(code.OpConstant, item.constant)
		a.emit(code.OpMath, item.primitive, 1)
		a.emit(code.OpChant)
	}
	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewFloat(9), value.NewFloat(-1.2), value.NewFloat(2.5), value.NewFloat(-1.9),
			value.NewFloat(math.NaN()), value.NewFloat(math.Inf(1)), value.NewFloat(math.Copysign(0, -1)),
			value.NewInt(math.MinInt64),
		},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
}

func TestM13cDataConvertUTF8CopyDifferential(t *testing.T) {
	a := newM3TestAsm()
	// source = UTF-8 bytes for 猫; text = from_utf8(source)
	for _, id := range []int{0, 1, 2} {
		a.emit(code.OpConstant, id)
	}
	a.emit(code.OpArrayTyped, 3, int(value.KindUint8))
	a.emit(code.OpSetGlobal, 0)
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpDataConvert, code.DataBytesToString)
	a.emit(code.OpSetGlobal, 1)
	// Mutating the source must not affect the dynamic string copy.
	a.emit(code.OpGetGlobal, 0)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpConstant, 4)
	a.emit(code.OpSetIndex)
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpChant)
	// Dynamic string -> bytes must produce another independent array.
	a.emit(code.OpGetGlobal, 1)
	a.emit(code.OpDataConvert, code.DataStringToBytes)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	// Invalid UTF-8 returns nullable nil.
	a.emit(code.OpConstant, 4)
	a.emit(code.OpArrayTyped, 1, int(value.KindUint8))
	a.emit(code.OpDataConvert, code.DataBytesToString)
	a.emit(code.OpIsNil)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{
		GlobalsCount: 2,
		Constants: []value.Value{
			value.NewUint8(0xe7), value.NewUint8(0x8c), value.NewUint8(0xab),
			value.NewInt(0), value.NewUint8(0xff),
		},
		Instructions: a.build(),
	}
	requireM12Differential(t, program)
	requireM12StressDifferential(t, program)
}

func TestM13cStaticStringToBytesDifferential(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpDataConvert, code.DataStringToBytes)
	a.emit(code.OpLen)
	a.emit(code.OpChant)
	program := &compiler.Bytecode{Constants: []value.Value{value.NewString("A\x00猫🙂")}, Instructions: a.build()}
	requireM12Differential(t, program)
}

func TestM13cDynamicStringContentComparison(t *testing.T) {
	a := newM3TestAsm()
	// Produce two distinct heap-backed strings with identical UTF-8 bytes.
	for copy := 0; copy < 2; copy++ {
		for _, id := range []int{0, 1, 2} {
			a.emit(code.OpConstant, id)
		}
		a.emit(code.OpArrayTyped, 3, int(value.KindUint8))
		a.emit(code.OpDataConvert, code.DataBytesToString)
	}
	a.emit(code.OpEqual)
	a.emit(code.OpChant)
	// Dynamic and tagged-static representations compare by content as well.
	for _, id := range []int{0, 1, 2} {
		a.emit(code.OpConstant, id)
	}
	a.emit(code.OpArrayTyped, 3, int(value.KindUint8))
	a.emit(code.OpDataConvert, code.DataBytesToString)
	a.emit(code.OpConstant, 3)
	a.emit(code.OpNotEqual)
	a.emit(code.OpChant)
	p := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewUint8('M'), value.NewUint8('i'), value.NewUint8('o'), value.NewString("Mio"),
		},
		Instructions: a.build(),
	}
	requireM12Differential(t, p)
}
