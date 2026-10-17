// Package aot 将经过验证的 Mochi 字节码编译为原生可执行文件。
// M1 仅支持 int64 常量、基础算术、输出和顶层表达式。
package aot

import (
	"fmt"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

// CompileDarwinARM64 将支持的 Mochi 指令编译为 macOS ARM64 机器码。
// 未实现的字节码、复合类型和函数必须明确报错，不能降级到 VM 或忽略操作。
func CompileDarwinARM64(program *compiler.Bytecode) ([]byte, error) {
	if err := mochi.Validate(program); err != nil {
		return nil, fmt.Errorf("invalid Mochi bytecode: %w", err)
	}
	if program.GlobalsCount != 0 || len(program.Functions) != 0 {
		return nil, fmt.Errorf("AOT M1 does not support globals or functions (globals=%d, functions=%d)",
			program.GlobalsCount, len(program.Functions))
	}
	for index, constant := range program.Constants {
		if constant.Kind() != value.KindInt {
			return nil, fmt.Errorf("AOT M1 constant %d has unsupported type %s (only int64)", index, constant.Kind())
		}
	}

	emitter := darwinarm64.NewIntegerEmitter()
	instructions := program.Instructions
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		definition, err := code.Lookup(op)
		if err != nil {
			return nil, fmt.Errorf("AOT M1 at byte offset %d: %w", offset, err)
		}
		operands, width, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return nil, fmt.Errorf("AOT M1 at byte offset %d: %w", offset, err)
		}
		switch op {
		case code.OpConstant:
			index := operands[0]
			if index < 0 || index >= len(program.Constants) {
				return nil, fmt.Errorf("AOT M1 invalid constant index %d at byte offset %d", index, offset)
			}
			n, ok := program.Constants[index].Int64()
			if !ok {
				return nil, fmt.Errorf("AOT M1 constant %d is not int64", index)
			}
			err = emitter.PushInt64(n)
		case code.OpAdd:
			err = emitter.Add()
		case code.OpSub:
			err = emitter.Sub()
		case code.OpMul:
			err = emitter.Mul()
		case code.OpNeg:
			err = emitter.Neg()
		case code.OpChant:
			err = emitter.Chant()
		case code.OpPop:
			err = emitter.Pop()
		default:
			return nil, fmt.Errorf("AOT M1 unsupported opcode %s at byte offset %d", definition.Name, offset)
		}
		if err != nil {
			return nil, fmt.Errorf("AOT M1 at byte offset %d: %w", offset, err)
		}
		offset += 1 + width
	}
	return emitter.Finish()
}
