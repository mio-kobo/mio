// M6b 差分测试：生成包含真实浮点算术、递归及打印 Opcode 的 Mochi 文件。
// 这里不进行常量折叠，以确保输出的数值来自目标程序的原生执行过程。
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

type m6bWriter struct{ instructions code.Instructions }

func (w *m6bWriter) emit(op code.Opcode, operand ...int) {
	encoded, err := code.Make(op, operand...)
	if err != nil {
		panic(err)
	}
	w.instructions = append(w.instructions, encoded...)
}
func (w *m6bWriter) here() int { return len(w.instructions) }
func (w *m6bWriter) fix(at, target int) {
	w.instructions[at+1], w.instructions[at+2] = byte(target>>8), byte(target)
}

func main() {
	out := flag.String("o", "build/m6b-strict.mochi", "输出路径")
	flag.Parse()

	constants := []value.Value{
		value.NewFloat(3.141592653589793),           // 0
		value.NewFloat(1.5),                         // 1
		value.NewFloat(2.25),                        // 2
		value.NewFloat(0),                           // 3
		value.NewFloat(1),                           // 4
		value.NewFloat(-1),                          // 5
		value.NewFloat32(1.23456789),                // 6
		value.NewFloat32(0.1),                       // 7
		value.NewFloat(1e-5),                        // 8
		value.NewFloat(1e6),                         // 9
		value.NewFloat(math.SmallestNonzeroFloat64), // 10
		value.NewFloat(math.MaxFloat64),             // 11
		value.NewFloat(3),                           // 12
	}

	m := &m6bWriter{}
	m.emit(code.OpConstant, 0)
	m.emit(code.OpChant)
	m.emit(code.OpConstant, 1)
	m.emit(code.OpConstant, 2)
	m.emit(code.OpAdd)
	m.emit(code.OpChant)
	m.emit(code.OpConstant, 3)
	m.emit(code.OpConstant, 3)
	m.emit(code.OpDiv)
	m.emit(code.OpChant)
	m.emit(code.OpConstant, 4)
	m.emit(code.OpConstant, 3)
	m.emit(code.OpDiv)
	m.emit(code.OpChant)
	m.emit(code.OpConstant, 5)
	m.emit(code.OpConstant, 3)
	m.emit(code.OpDiv)
	m.emit(code.OpChant)
	m.emit(code.OpConstant, 3)
	m.emit(code.OpConstant, 5)
	m.emit(code.OpDiv)
	m.emit(code.OpChant)
	for _, id := range []int{6, 7, 8, 9, 10, 11} {
		m.emit(code.OpConstant, id)
		m.emit(code.OpChant)
	}
	m.emit(code.OpConstant, 12)
	m.emit(code.OpCall, 0)
	m.emit(code.OpChant)
	m.emit(code.OpConstant, 0)
	m.emit(code.OpPop)

	fn := &m6bWriter{}
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 3)
	fn.emit(code.OpLessEqual)
	endIf := fn.here()
	fn.emit(code.OpJumpIfFalse, 0)
	fn.emit(code.OpConstant, 3)
	fn.emit(code.OpReturnValue)
	fn.fix(endIf, fn.here())
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpGetLocal, 0)
	fn.emit(code.OpConstant, 4)
	fn.emit(code.OpSub)
	fn.emit(code.OpCall, 0)
	fn.emit(code.OpAdd)
	fn.emit(code.OpReturnValue)

	program := &compiler.Bytecode{
		Constants:    constants,
		Instructions: m.instructions,
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "accumulate", Instructions: fn.instructions,
			ParameterCount: 1, LocalCount: 1, ReturnsValue: true,
		}},
	}
	file, err := os.Create(*out)
	if err != nil {
		panic(err)
	}
	if err := mochi.Encode(file, program); err != nil {
		file.Close()
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("已生成 %s（float32/float64 输出、特殊值、递归、OpPop；未常量折叠）\n", *out)
}
