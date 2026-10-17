// M7 原生字符串差分测试，直接构造 Mochi 字节码，不进行常量折叠。
// 这里特意包含 UTF-8、内嵌 NUL、空字符串、重复常量和跨函数传参。
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

type stringWriter struct{ instructions code.Instructions }

func (w *stringWriter) emit(op code.Opcode, operands ...int) {
	ins, err := code.Make(op, operands...)
	if err != nil {
		panic(err)
	}
	w.instructions = append(w.instructions, ins...)
}
func (w *stringWriter) here() int { return len(w.instructions) }
func (w *stringWriter) patch(at, target int) {
	w.instructions[at+1], w.instructions[at+2] = byte(target>>8), byte(target)
}

func main() {
	output := flag.String("o", "build/m7-strict.mochi", "输出的 Mochi 文件路径")
	chooseInt := flag.Bool("int-pop", false, "选择最终的 int64 OpPop 分支")
	flag.Parse()
	constants := []value.Value{
		value.NewString("Hello, Mio!"), // 0
		value.NewString("你好，Mio！"),     // 1
		value.NewString("こんにちは、Mio！"),  // 2
		value.NewString("a\x00b"),      // 3
		value.NewString(""),            // 4
		value.NewString("duplicate"),   // 5
		value.NewString("duplicate"),   // 6: 不同常量索引，相同字符串内容
		value.NewString("branch yes"),  // 7
		value.NewString("ignored"),     // 8
		value.NewInt(123),              // 9: 条件 Pop 另一分支为 int64
	}

	main := &stringWriter{}
	main.emit(code.OpConstant, 0)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 1)
	main.emit(code.OpSetGlobal, 0)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpCall, 1) // unit say(string)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpCall, 0) // echo(string) -> string
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 3)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant) // 内嵌 NUL 必须完整输出 3 字节 + 换行
	main.emit(code.OpConstant, 4)
	main.emit(code.OpChant) // 空字符串也必须输出一个换行
	main.emit(code.OpConstant, 5)
	main.emit(code.OpConstant, 6)
	main.emit(code.OpEqual)
	main.emit(code.OpChant) // 静态驻留字符串相等 => hai
	main.emit(code.OpConstant, 6)
	main.emit(code.OpCall, 1) // 字符串作为 unit 函数参数

	// 不同运行路径上的 OpPop 类型不同，退出前必须输出真实执行的那个值。
	if *chooseInt {
		main.emit(code.OpTrue)
	} else {
		main.emit(code.OpFalse)
	}
	jumpFalse := main.here()
	main.emit(code.OpJumpIfFalse, 0)
	main.emit(code.OpConstant, 9)
	main.emit(code.OpPop) // int64 (不会执行)
	jumpDone := main.here()
	main.emit(code.OpJump, 0)
	main.patch(jumpFalse, main.here())
	main.emit(code.OpConstant, 7)
	main.emit(code.OpPop) // string (会执行)
	main.patch(jumpDone, main.here())

	echo := &stringWriter{}
	echo.emit(code.OpGetLocal, 0)
	echo.emit(code.OpDefineLocal, 1)
	echo.emit(code.OpGetLocal, 1)
	echo.emit(code.OpReturnValue)

	say := &stringWriter{}
	say.emit(code.OpGetLocal, 0)
	say.emit(code.OpChant)
	say.emit(code.OpReturn)

	program := &compiler.Bytecode{
		Constants:    constants,
		GlobalsCount: 1,
		Instructions: main.instructions,
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "echo", ParameterCount: 1, LocalCount: 2, ReturnsValue: true, Instructions: echo.instructions},
			{ID: 1, Name: "say", ParameterCount: 1, LocalCount: 1, ReturnsValue: false, Instructions: say.instructions},
		},
	}
	f, err := os.Create(*output)
	if err != nil {
		panic(err)
	}
	if err := mochi.Encode(f, program); err != nil {
		f.Close()
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("已生成 %s（静态字符串、NUL、全局槽位、函数、条件 Pop；未常量折叠；int-pop=%v）\n", *output, *chooseInt)
}
