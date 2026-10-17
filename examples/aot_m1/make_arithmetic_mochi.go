// 此文件仅用于 M1 验证：构造未经常量折叠的合法 Mochi 算术字节码。
// 这样能确认 ADD、SUB、MUL、NEG 确实由 CPU 在运行时执行，而非预先计算。
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

func main() {
	output := flag.String("o", "build/m1-arithmetic.mochi", "输出的 Mochi 文件")
	flag.Parse()

	program := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(40),
			value.NewInt(2),
			value.NewInt(7),
			value.NewInt(6),
			value.NewInt(5),
			value.NewInt(8),
			value.NewInt(-4),
			value.NewInt(1),
			value.NewInt(-9223372036854775808),
			value.NewInt(9223372036854775807),
			value.NewInt(99),
		},
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0, // 40
			byte(code.OpConstant), 0, 1, // 2
			byte(code.OpAdd), byte(code.OpChant),
			byte(code.OpConstant), 0, 2, // 7
			byte(code.OpConstant), 0, 3, // 6
			byte(code.OpMul), byte(code.OpChant),
			byte(code.OpConstant), 0, 4, // 5
			byte(code.OpConstant), 0, 5, // 8
			byte(code.OpSub), byte(code.OpChant),
			byte(code.OpConstant), 0, 8, // MinInt64
			byte(code.OpChant),
			byte(code.OpConstant), 0, 6, // -4
			byte(code.OpNeg), byte(code.OpChant), // 4
			byte(code.OpConstant), 0, 9, // MaxInt64
			byte(code.OpConstant), 0, 7, // 1
			byte(code.OpAdd), byte(code.OpChant), // MinInt64 (回绕)
			byte(code.OpConstant), 0, 10, // 99
			byte(code.OpPop), // 与 mio exec 一样，在程序末尾打印最后一次 Pop
		},
	}
	file, err := os.Create(*output)
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
	fmt.Printf("已写入 %s（未做常量折叠）\n", *output)
}
