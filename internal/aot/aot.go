// Package aot 将经过验证的 Mochi 字节码编译为原生可执行文件。
// M2 支持 int64 常量、基础算术、全局变量、输出和顶层表达式。
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
	if len(program.Functions) != 0 {
		return nil, fmt.Errorf("AOT M2 does not support functions (functions=%d)", len(program.Functions))
	}
	if program.GlobalsCount > darwinarm64.MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M2 global slot count %d exceeds limit %d",
			program.GlobalsCount, darwinarm64.MaxGlobalSlots)
	}
	for index, constant := range program.Constants {
		if constant.Kind() != value.KindInt {
			return nil, fmt.Errorf("AOT M2 constant %d has unsupported type %s (only int64)", index, constant.Kind())
		}
	}

	emitter := darwinarm64.NewIntegerEmitter()
	if err := emitter.SetGlobalCount(program.GlobalsCount); err != nil {
		return nil, err
	}
	// M2 暂不支持分支，因此可以静态检查全局槽位是否先赋值再读取。
	// 防止将未初始化的 0 错当作有效的 int64，保持 Mochi VM 的错误语义。
	initialized := make([]bool, program.GlobalsCount)
	instructions := program.Instructions
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		definition, err := code.Lookup(op)
		if err != nil {
			return nil, fmt.Errorf("AOT M2 at byte offset %d: %w", offset, err)
		}
		operands, width, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return nil, fmt.Errorf("AOT M2 at byte offset %d: %w", offset, err)
		}
		switch op {
		case code.OpConstant:
			index := operands[0]
			if index < 0 || index >= len(program.Constants) {
				return nil, fmt.Errorf("AOT M2 invalid constant index %d at byte offset %d", index, offset)
			}
			n, ok := program.Constants[index].Int64()
			if !ok {
				return nil, fmt.Errorf("AOT M2 constant %d is not int64", index)
			}
			err = emitter.PushInt64(n)
		case code.OpSetGlobal:
			index := operands[0]
			err = emitter.SetGlobal(index)
			if err == nil {
				initialized[index] = true
			}
		case code.OpGetGlobal:
			index := operands[0]
			if index < 0 || index >= len(initialized) {
				return nil, fmt.Errorf("AOT M2 global slot %d out of range at byte offset %d", index, offset)
			}
			if !initialized[index] {
				return nil, fmt.Errorf("AOT M2 global slot %d is uninitialized at byte offset %d", index, offset)
			}
			err = emitter.GetGlobal(index)
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
			return nil, fmt.Errorf("AOT M2 unsupported opcode %s at byte offset %d", definition.Name, offset)
		}
		if err != nil {
			return nil, fmt.Errorf("AOT M2 at byte offset %d: %w", offset, err)
		}
		offset += 1 + width
	}
	return emitter.Finish()
}
