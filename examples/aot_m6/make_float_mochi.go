// M6a 浮点验证：构造未经常量折叠的 Mochi 指令序列。
// 浮点运算与转换在执行时进行，输出只使用 int64 / bool，不测试尚未实现的浮点格式化。
package main

import (
	"flag"
	"fmt"
	"os"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

type m6Asm struct{ ins code.Instructions }

func (a *m6Asm) emit(op code.Opcode, operands ...int) {
	b, err := code.Make(op, operands...)
	if err != nil {
		panic(err)
	}
	a.ins = append(a.ins, b...)
}
func (a *m6Asm) where() int { return len(a.ins) }
func (a *m6Asm) patch(position, target int) {
	a.ins[position+1], a.ins[position+2] = byte(target>>8), byte(target)
}
func main() {
	out := flag.String("o", "build/m6-strict.mochi", "Mochi 输出路径")
	flag.Parse()
	c := []value.Value{
		value.NewFloat(1.5), value.NewFloat(2.25), value.NewFloat(3.75),
		value.NewFloat(3.375), value.NewFloat(-0.75), value.NewFloat(-1.5),
		value.NewFloat(3.99), value.NewFloat32(1.5), value.NewFloat32(2.25),
		value.NewFloat32(3.75), value.NewFloat(0), value.NewFloat(1),
		value.NewFloat(3), value.NewFloat(6), value.NewFloat32(2),
		value.NewInt(2), value.NewFloat(2),
	}
	const (
		a      = 0
		b      = 1
		sum    = 2
		mul    = 3
		diff   = 4
		neg    = 5
		toInt  = 6
		a32    = 7
		b32    = 8
		sum32  = 9
		zero   = 10
		one    = 11
		three  = 12
		six    = 13
		two32  = 14
		twoInt = 15
		twoF64 = 16
	)
	main := &m6Asm{}
	// 4 种 float64 运算 + 负号
	main.emit(code.OpConstant, a)
	main.emit(code.OpConstant, b)
	main.emit(code.OpAdd)
	main.emit(code.OpConstant, sum)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a)
	main.emit(code.OpConstant, b)
	main.emit(code.OpMul)
	main.emit(code.OpConstant, mul)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, b)
	main.emit(code.OpConstant, a)
	main.emit(code.OpDiv)
	main.emit(code.OpConstant, a)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a)
	main.emit(code.OpConstant, b)
	main.emit(code.OpSub)
	main.emit(code.OpConstant, diff)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a)
	main.emit(code.OpNeg)
	main.emit(code.OpConstant, neg)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, toInt)
	main.emit(code.OpFloatToInt)
	main.emit(code.OpChant) // 3
	// float32 算术、两种精度互转，以及 int64 → 两种浮点类型
	main.emit(code.OpConstant, a32)
	main.emit(code.OpConstant, b32)
	main.emit(code.OpAdd)
	main.emit(code.OpConstant, sum32)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a32)
	main.emit(code.OpFloat32ToFloat64)
	main.emit(code.OpConstant, a)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a)
	main.emit(code.OpFloat64ToFloat32)
	main.emit(code.OpConstant, a32)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, twoInt)
	main.emit(code.OpIntToFloat)
	main.emit(code.OpConstant, twoF64)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, twoInt)
	main.emit(code.OpIntToFloat32)
	main.emit(code.OpConstant, two32)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a32)
	main.emit(code.OpFloat32ToInt)
	main.emit(code.OpChant) // 1
	// NaN、Infinity、-0 的 IEEE 754 比较行为
	main.emit(code.OpConstant, zero)
	main.emit(code.OpConstant, zero)
	main.emit(code.OpDiv)
	main.emit(code.OpSetGlobal, 0)
	for _, op := range []code.Opcode{code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual} {
		main.emit(code.OpGetGlobal, 0)
		main.emit(code.OpGetGlobal, 0)
		main.emit(op)
		main.emit(code.OpChant)
	}
	main.emit(code.OpConstant, one)
	main.emit(code.OpConstant, zero)
	main.emit(code.OpDiv)
	main.emit(code.OpSetGlobal, 1)
	main.emit(code.OpGetGlobal, 1)
	main.emit(code.OpConstant, one)
	main.emit(code.OpGreater)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, zero)
	main.emit(code.OpNeg)
	main.emit(code.OpSetGlobal, 2)
	main.emit(code.OpConstant, zero)
	main.emit(code.OpGetGlobal, 2)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, one)
	main.emit(code.OpGetGlobal, 2)
	main.emit(code.OpDiv)
	main.emit(code.OpConstant, zero)
	main.emit(code.OpLess)
	main.emit(code.OpChant)
	// 递归 float64 参数/返回值，以及 float32 双参数传递。
	main.emit(code.OpConstant, three)
	main.emit(code.OpCall, 0)
	main.emit(code.OpConstant, six)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, a32)
	main.emit(code.OpConstant, b32)
	main.emit(code.OpCall, 1)
	main.emit(code.OpConstant, sum32)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)

	rec := &m6Asm{}
	rec.emit(code.OpGetLocal, 0)
	rec.emit(code.OpConstant, zero)
	rec.emit(code.OpLessEqual)
	jf := rec.where()
	rec.emit(code.OpJumpIfFalse, 0)
	rec.emit(code.OpConstant, zero)
	rec.emit(code.OpReturnValue)
	rec.patch(jf, rec.where())
	rec.emit(code.OpGetLocal, 0)
	rec.emit(code.OpGetLocal, 0)
	rec.emit(code.OpConstant, one)
	rec.emit(code.OpSub)
	rec.emit(code.OpCall, 0)
	rec.emit(code.OpAdd)
	rec.emit(code.OpReturnValue)
	fn32 := &m6Asm{}
	fn32.emit(code.OpGetLocal, 0)
	fn32.emit(code.OpGetLocal, 1)
	fn32.emit(code.OpAdd)
	fn32.emit(code.OpReturnValue)

	p := &compiler.Bytecode{Constants: c, GlobalsCount: 3, Instructions: main.ins, Functions: []compiler.CompiledFunction{
		{ID: 0, Name: "float_recursive_sum", Instructions: rec.ins, ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
		{ID: 1, Name: "float32_add", Instructions: fn32.ins, ParameterCount: 2, LocalCount: 2, ReturnsValue: true},
	}}
	f, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	if err := mochi.Encode(f, p); err != nil {
		f.Close()
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("已生成 %s（float32/64、NaN/Inf/-0、转换、递归；未常量折叠）\n", *out)
}
