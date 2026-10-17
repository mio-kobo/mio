// M5 测试专用：直接生成包含深层表达式与递归的 Mochi 字节码。
// 不调用 Mio 源码编译器，确保 ARM64 ADD 和 CALL 确实在运行时执行。
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

type builder struct {
	instructions code.Instructions
	labels       map[string]int
	jumps        []struct {
		pos int
		op  code.Opcode
		to  string
	}
}

func newBuilder() *builder { return &builder{labels: make(map[string]int)} }
func (b *builder) emit(op code.Opcode, args ...int) {
	ins, err := code.Make(op, args...)
	if err != nil {
		panic(err)
	}
	b.instructions = append(b.instructions, ins...)
}
func (b *builder) label(name string) { b.labels[name] = len(b.instructions) }
func (b *builder) jump(op code.Opcode, target string) {
	b.jumps = append(b.jumps, struct {
		pos int
		op  code.Opcode
		to  string
	}{len(b.instructions), op, target})
	b.emit(op, 0)
}
func (b *builder) done() code.Instructions {
	for _, jump := range b.jumps {
		at, ok := b.labels[jump.to]
		if !ok {
			panic("undefined label " + jump.to)
		}
		ins, err := code.Make(jump.op, at)
		if err != nil {
			panic(err)
		}
		copy(b.instructions[jump.pos:], ins)
	}
	return b.instructions
}

func main() {
	output := flag.String("o", "build/m5-stack.mochi", "生成的 Mochi 文件")
	flag.Parse()
	const (
		one = iota
		five
		seven
		zero
		three
		ninetyNine
		eightyEight
	)
	p := &compiler.Bytecode{
		GlobalsCount: 1,
		Constants: []value.Value{
			value.NewInt(1), value.NewInt(5), value.NewInt(7),
			value.NewInt(0), value.NewInt(3), value.NewInt(99), value.NewInt(88),
		},
	}

	main := newBuilder()
	// 16 个同时存活的整数，超过 M4 的寄存器栈上限 7。
	for i := 0; i < 16; i++ {
		main.emit(code.OpConstant, one)
	}
	for i := 1; i < 16; i++ {
		main.emit(code.OpAdd)
	}
	main.emit(code.OpChant)
	main.emit(code.OpConstant, five)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	main.emit(code.OpCall, 1)
	main.emit(code.OpConstant, zero)
	main.emit(code.OpSetGlobal, 0)
	main.label("loop")
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, three)
	main.emit(code.OpLess)
	main.jump(code.OpJumpIfFalse, "end")
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, one)
	main.emit(code.OpAdd)
	main.emit(code.OpSetGlobal, 0)
	main.jump(code.OpJump, "loop")
	main.label("end")
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpChant)
	main.emit(code.OpGetGlobal, 0)
	main.emit(code.OpConstant, three)
	main.emit(code.OpEqual)
	main.emit(code.OpChant)
	main.emit(code.OpTrue)
	main.emit(code.OpNot)
	main.emit(code.OpChant)
	// 不执行的 Pop 不得覆盖后续真正执行的顶层表达式结果。
	main.emit(code.OpFalse)
	main.jump(code.OpJumpIfFalse, "skip_pop")
	main.emit(code.OpConstant, eightyEight)
	main.emit(code.OpPop)
	main.label("skip_pop")
	main.emit(code.OpConstant, ninetyNine)
	main.emit(code.OpPop)
	p.Instructions = main.done()

	weighted := newBuilder()
	weighted.emit(code.OpGetLocal, 0)
	weighted.emit(code.OpConstant, one)
	weighted.emit(code.OpLessEqual)
	weighted.jump(code.OpJumpIfFalse, "recursion")
	weighted.emit(code.OpConstant, one)
	weighted.emit(code.OpReturnValue)
	weighted.label("recursion")
	for i := 0; i < 8; i++ {
		weighted.emit(code.OpGetLocal, 0)
	}
	weighted.emit(code.OpGetLocal, 0)
	weighted.emit(code.OpConstant, one)
	weighted.emit(code.OpSub)
	weighted.emit(code.OpCall, 0)
	for i := 0; i < 8; i++ {
		weighted.emit(code.OpAdd)
	}
	weighted.emit(code.OpReturnValue)

	show := newBuilder()
	show.emit(code.OpConstant, seven)
	show.emit(code.OpChant)
	show.emit(code.OpReturn)

	p.Functions = []compiler.CompiledFunction{
		{ID: 0, Name: "weighted", Instructions: weighted.done(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
		{ID: 1, Name: "show", Instructions: show.done(), ReturnsValue: false},
	}

	f, err := os.Create(*output)
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
	fmt.Printf("已生成 %s（16 层操作数、递归、函数调用与循环；未常量折叠）\n", *output)
}
