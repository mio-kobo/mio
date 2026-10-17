// M4 测试专用：直接使用 Mochi v14 编码器生成未经常量折叠的函数字节码。
// 此文件不会修改 Mochi Opcode，也不会生成任何伪造的原生输出。
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

type assembler struct {
	code   code.Instructions
	labels map[string]int
	fixups []struct {
		offset int
		opcode code.Opcode
		label  string
	}
}

func newAssembler() *assembler { return &assembler{labels: make(map[string]int)} }
func (a *assembler) emit(op code.Opcode, operands ...int) {
	instruction, err := code.Make(op, operands...)
	if err != nil {
		panic(err)
	}
	a.code = append(a.code, instruction...)
}
func (a *assembler) label(name string) { a.labels[name] = len(a.code) }
func (a *assembler) jump(op code.Opcode, label string) {
	a.fixups = append(a.fixups, struct {
		offset int
		opcode code.Opcode
		label  string
	}{len(a.code), op, label})
	a.emit(op, 0)
}
func (a *assembler) build() code.Instructions {
	for _, fix := range a.fixups {
		target, ok := a.labels[fix.label]
		if !ok {
			panic("undefined label: " + fix.label)
		}
		instruction, err := code.Make(fix.opcode, target)
		if err != nil {
			panic(err)
		}
		copy(a.code[fix.offset:], instruction)
	}
	return a.code
}

func main() {
	output := flag.String("o", "build/m4-functions.mochi", "输出的 Mochi 文件")
	flag.Parse()
	// c0=1, c1=5, c2=40, c3=2, c4=7, c5=0, c6=3, c7=99, c8=6
	p := &compiler.Bytecode{Constants: []value.Value{
		value.NewInt(1), value.NewInt(5), value.NewInt(40), value.NewInt(2),
		value.NewInt(7), value.NewInt(0), value.NewInt(3), value.NewInt(99), value.NewInt(6),
	}}
	main := newAssembler()
	main.emit(code.OpConstant, 1)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 2)
	main.emit(code.OpConstant, 3)
	main.emit(code.OpCall, 1)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 4)
	main.emit(code.OpCall, 2)
	main.emit(code.OpConstant, 5)
	main.emit(code.OpCall, 3)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 8)
	main.emit(code.OpCall, 4)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 8)
	main.emit(code.OpCall, 5)
	main.emit(code.OpChant)
	main.emit(code.OpConstant, 7)
	main.emit(code.OpPop)
	p.Instructions = main.build()

	fact := newAssembler()
	fact.emit(code.OpGetLocal, 0)
	fact.emit(code.OpConstant, 0)
	fact.emit(code.OpLessEqual)
	fact.jump(code.OpJumpIfFalse, "recurse")
	fact.emit(code.OpConstant, 0)
	fact.emit(code.OpReturnValue)
	fact.label("recurse")
	fact.emit(code.OpGetLocal, 0)
	fact.emit(code.OpGetLocal, 0)
	fact.emit(code.OpConstant, 0)
	fact.emit(code.OpSub)
	fact.emit(code.OpCall, 0)
	fact.emit(code.OpMul)
	fact.emit(code.OpReturnValue)

	add := newAssembler()
	add.emit(code.OpGetLocal, 0)
	add.emit(code.OpGetLocal, 1)
	add.emit(code.OpAdd)
	add.emit(code.OpReturnValue)
	show := newAssembler()
	show.emit(code.OpGetLocal, 0)
	show.emit(code.OpChant)
	show.emit(code.OpReturn)
	count := newAssembler()
	count.emit(code.OpGetLocal, 0)
	count.emit(code.OpSetLocal, 1)
	count.label("head")
	count.emit(code.OpGetLocal, 1)
	count.emit(code.OpConstant, 6)
	count.emit(code.OpLess)
	count.jump(code.OpJumpIfFalse, "done")
	count.emit(code.OpConstant, 7)
	count.emit(code.OpDefineLocal, 2)
	count.emit(code.OpGetLocal, 1)
	count.emit(code.OpConstant, 0)
	count.emit(code.OpAdd)
	count.emit(code.OpSetLocal, 1)
	count.jump(code.OpJump, "head")
	count.label("done")
	count.emit(code.OpGetLocal, 1)
	count.emit(code.OpReturnValue)

	even := newAssembler()
	even.emit(code.OpGetLocal, 0)
	even.emit(code.OpConstant, 5)
	even.emit(code.OpEqual)
	even.jump(code.OpJumpIfFalse, "recursive")
	even.emit(code.OpConstant, 0)
	even.emit(code.OpReturnValue)
	even.label("recursive")
	even.emit(code.OpGetLocal, 0)
	even.emit(code.OpConstant, 0)
	even.emit(code.OpSub)
	even.emit(code.OpCall, 5)
	even.emit(code.OpReturnValue)
	odd := newAssembler()
	odd.emit(code.OpGetLocal, 0)
	odd.emit(code.OpConstant, 5)
	odd.emit(code.OpEqual)
	odd.jump(code.OpJumpIfFalse, "recursive")
	odd.emit(code.OpConstant, 5)
	odd.emit(code.OpReturnValue)
	odd.label("recursive")
	odd.emit(code.OpGetLocal, 0)
	odd.emit(code.OpConstant, 0)
	odd.emit(code.OpSub)
	odd.emit(code.OpCall, 4)
	odd.emit(code.OpReturnValue)

	p.Functions = []compiler.CompiledFunction{
		{ID: 0, Name: "factorial", Instructions: fact.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
		{ID: 1, Name: "add", Instructions: add.build(), ParameterCount: 2, LocalCount: 2, ReturnsValue: true},
		{ID: 2, Name: "show", Instructions: show.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: false},
		{ID: 3, Name: "count", Instructions: count.build(), ParameterCount: 1, LocalCount: 3, ReturnsValue: true},
		{ID: 4, Name: "even", Instructions: even.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
		{ID: 5, Name: "odd", Instructions: odd.build(), ParameterCount: 1, LocalCount: 1, ReturnsValue: true},
	}
	file, err := os.Create(*output)
	if err != nil {
		panic(err)
	}
	if err := mochi.Encode(file, p); err != nil {
		file.Close()
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("已生成 %s（原生函数、递归与局部槽位；未做常量折叠）\n", *output)
}
