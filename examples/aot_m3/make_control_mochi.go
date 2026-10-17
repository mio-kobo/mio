// M3 测试数据：使用现有 Mochi Opcode 直接构造未折叠的分支和循环。
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

type jumpPatch struct {
	at   int
	name string
	op   code.Opcode
}
type assembler struct {
	instructions code.Instructions
	labels       map[string]int
	patches      []jumpPatch
}

func newAssembler() *assembler { return &assembler{labels: make(map[string]int)} }
func (a *assembler) mark(name string) {
	if _, found := a.labels[name]; found {
		panic("duplicate label: " + name)
	}
	a.labels[name] = len(a.instructions)
}
func (a *assembler) emit(op code.Opcode, operands ...int) {
	b, err := code.Make(op, operands...)
	if err != nil {
		panic(err)
	}
	a.instructions = append(a.instructions, b...)
}
func (a *assembler) jump(op code.Opcode, label string) {
	a.patches = append(a.patches, jumpPatch{at: len(a.instructions), name: label, op: op})
	a.emit(op, 0)
}
func (a *assembler) build() code.Instructions {
	for _, j := range a.patches {
		target, ok := a.labels[j.name]
		if !ok {
			panic("unknown label: " + j.name)
		}
		replacement, err := code.Make(j.op, target)
		if err != nil {
			panic(err)
		}
		copy(a.instructions[j.at:], replacement)
	}
	return a.instructions
}

func main() {
	output := flag.String("o", "build/m3-control.mochi", "输出 Mochi 文件")
	flag.Parse()

	// 所有运算数均由 Mochi 指令在运行期间计算，不做常量折叠。
	p := &compiler.Bytecode{GlobalsCount: 2, Constants: []value.Value{
		value.NewInt(0), value.NewInt(1), value.NewInt(2), value.NewInt(4), value.NewInt(5),
		value.NewInt(42), value.NewInt(-1), value.NewInt(-9223372036854775808),
		value.NewInt(9223372036854775807), value.NewInt(99),
	}}
	a := newAssembler()
	push := func(i int) { a.emit(code.OpConstant, i) }
	get := func(i int) { a.emit(code.OpGetGlobal, i) }
	set := func(i int) { a.emit(code.OpSetGlobal, i) }
	// g0 = 0; g1 = 0
	push(0)
	set(0)
	push(0)
	set(1)
	a.mark("loop")
	get(0)
	push(4)
	a.emit(code.OpLess)
	a.jump(code.OpJumpIfFalse, "end")
	get(0)
	push(1)
	a.emit(code.OpAdd)
	set(0)
	get(0)
	push(2)
	a.emit(code.OpEqual)
	a.jump(code.OpJumpIfFalse, "not_continue")
	a.jump(code.OpJump, "loop")
	a.mark("not_continue")
	get(0)
	push(3)
	a.emit(code.OpEqual)
	a.jump(code.OpJumpIfFalse, "not_break")
	a.jump(code.OpJump, "end")
	a.mark("not_break")
	get(1)
	get(0)
	a.emit(code.OpAdd)
	set(1)
	a.jump(code.OpJump, "loop")
	a.mark("end")
	get(1)
	a.emit(code.OpChant) // 4
	// 分支与短路产生真实 bool，并由原生输出例程显示 hai/iie。
	get(1)
	push(3)
	a.emit(code.OpEqual)
	a.emit(code.OpChant) // hai
	get(1)
	push(3)
	a.emit(code.OpNotEqual)
	a.emit(code.OpChant) // iie
	push(7)
	push(8)
	a.emit(code.OpLess)
	a.emit(code.OpChant) // hai，带符号极值比较
	a.emit(code.OpTrue)
	a.emit(code.OpNot)
	a.emit(code.OpChant) // iie
	// 构造短路 false && side-effect：跳过 g0=99。
	a.emit(code.OpFalse)
	a.jump(code.OpJumpIfFalse, "skip_write")
	push(9)
	set(0)
	a.mark("skip_write")
	get(0)
	a.emit(code.OpChant) // 4
	// 记录并打印当前执行路径最后一个 OpPop。
	a.emit(code.OpTrue)
	a.jump(code.OpJumpIfFalse, "skip_pop")
	push(9)
	a.emit(code.OpPop) // 99
	a.mark("skip_pop")
	p.Instructions = a.build()

	file, err := os.Create(*output)
	if err != nil {
		panic(err)
	}
	if err = mochi.Encode(file, p); err != nil {
		file.Close()
		panic(err)
	}
	if err = file.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("已生成 %s（分支、循环与条件 Pop 均未折叠）\n", *output)
}
