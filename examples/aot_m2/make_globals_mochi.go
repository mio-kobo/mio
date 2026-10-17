// M2 测试程序：直接生成未做常量折叠的 Mochi 指令，验证真实的全局变量读写。
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
	output := flag.String("o", "build/m2-globals.mochi", "输出 Mochi 字节码路径")
	flag.Parse()

	program := &compiler.Bytecode{
		GlobalsCount: 1025, // 覆盖 __DATA 中不止一个 4KiB 子页。
		Constants: []value.Value{
			value.NewInt(40), value.NewInt(2), value.NewInt(10),
			value.NewInt(-3), value.NewInt(-9223372036854775808),
			value.NewInt(-1), value.NewInt(99), value.NewInt(7),
		},
		Instructions: code.Instructions{
			// g0 = 40, g1 = 2。
			byte(code.OpConstant), 0, 0, byte(code.OpSetGlobal), 0, 0,
			byte(code.OpConstant), 0, 1, byte(code.OpSetGlobal), 0, 1,
			// chant(g0 + g1) = 42。
			byte(code.OpGetGlobal), 0, 0, byte(code.OpGetGlobal), 0, 1,
			byte(code.OpAdd), byte(code.OpChant),
			// g0 = g0 + 10, chant(g0) = 50。
			byte(code.OpGetGlobal), 0, 0,
			byte(code.OpConstant), 0, 2, byte(code.OpAdd),
			byte(code.OpSetGlobal), 0, 0,
			byte(code.OpGetGlobal), 0, 0, byte(code.OpChant),
			// chant(g1 - 3) = -1。
			byte(code.OpGetGlobal), 0, 1,
			byte(code.OpConstant), 0, 3, byte(code.OpAdd), byte(code.OpChant),
			// 跨越 4KiB 子页：g512 = MinInt64。
			byte(code.OpConstant), 0, 4, byte(code.OpSetGlobal), 2, 0,
			byte(code.OpGetGlobal), 2, 0, byte(code.OpChant),
			// g512 = g512 - 1（按 int64 回绕得到 MaxInt64）。
			byte(code.OpGetGlobal), 2, 0,
			byte(code.OpConstant), 0, 5, byte(code.OpAdd),
			byte(code.OpSetGlobal), 2, 0,
			byte(code.OpGetGlobal), 2, 0, byte(code.OpChant),
			// g1024 = 99，访问第三个 4KiB 子页。
			byte(code.OpConstant), 0, 6, byte(code.OpSetGlobal), 4, 0,
			byte(code.OpGetGlobal), 4, 0, byte(code.OpChant),
			// 顶层 OpPop 与 VM 一致：最后打印 7。
			byte(code.OpConstant), 0, 7, byte(code.OpPop),
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
	fmt.Printf("已写入 %s（全局槽位 0、1、512、1024；无常量折叠）\n", *output)
}
