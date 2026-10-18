// Package aot 将通过验证的 Mochi 字节码编译为原生程序。
// M7 增加 UTF-8 静态字符串和 Mach-O 只读常量区，不引入 GC。
package aot

import (
	"fmt"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

// CompileDarwinARM64 为 Mochi 生成 macOS ARM64 可执行文件。
// 尚未支持的类型或指令必须明确失败，不能回退 VM 执行。
func CompileDarwinARM64(program *compiler.Bytecode) ([]byte, error) {
	if err := mochi.Validate(program); err != nil {
		return nil, fmt.Errorf("invalid Mochi bytecode: %w", err)
	}
	if needsM6(program) {
		return compileM6(program)
	}
	if len(program.Functions) != 0 {
		return compileM4(program)
	}
	if program.GlobalsCount < 0 || program.GlobalsCount > darwinarm64.MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M3 global slot count %d exceeds limit %d", program.GlobalsCount, darwinarm64.MaxGlobalSlots)
	}
	for i, v := range program.Constants {
		if v.Kind() != value.KindInt {
			return nil, fmt.Errorf("AOT M3 constant %d has unsupported type %s (only int64)", i, v.Kind())
		}
	}
	insts, boundaries, err := decodeM3(program)
	if err != nil {
		return nil, err
	}
	// 纯空程序仍使用 M0 的三指令退出映像，保持已验证的字节布局。
	if len(insts) == 0 && program.GlobalsCount == 0 {
		return darwinarm64.ExitProgram(0)
	}
	states, err := analyzeM3(program, insts, boundaries)
	if err != nil {
		return nil, err
	}
	// 浅表达式保持 M0–M4 已经在实机验证的机器码布局。
	// 深度超过 7 后统一切换到 M5 帧内操作数栈。
	for _, st := range states {
		if st != nil && len(st.stack) > 7 {
			return compileM5(program)
		}
	}

	emitter := darwinarm64.NewIntegerEmitter()
	if err := emitter.SetGlobalCount(program.GlobalsCount); err != nil {
		return nil, err
	}
	if err := emitter.EnableControlFlow(); err != nil {
		return nil, err
	}
	nativeOffsets := make(map[int]int, len(insts)+1)
	for i, ins := range insts {
		nativeOffsets[ins.offset] = emitter.CodeOffset()
		if states[i] == nil {
			continue
		} // 跳转不可达的指令不产生机器码。
		st := states[i]
		if err := emitter.SetOperandDepth(len(st.stack)); err != nil {
			return nil, err
		}
		var emitErr error
		switch ins.op {
		case code.OpConstant:
			n, _ := program.Constants[ins.operands[0]].Int64()
			emitErr = emitter.PushInt64(n)
		case code.OpSetGlobal:
			emitErr = emitter.SetGlobal(ins.operands[0])
		case code.OpGetGlobal:
			emitErr = emitter.GetGlobal(ins.operands[0])
		case code.OpAdd:
			emitErr = emitter.Add()
		case code.OpSub:
			emitErr = emitter.Sub()
		case code.OpMul:
			emitErr = emitter.Mul()
		case code.OpNeg:
			emitErr = emitter.Neg()
		case code.OpTrue:
			emitErr = emitter.PushBool(true)
		case code.OpFalse:
			emitErr = emitter.PushBool(false)
		case code.OpNot:
			emitErr = emitter.NotBool()
		case code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			cond := uint32(0)
			switch ins.op {
			case code.OpEqual:
				cond = 0 // EQ
			case code.OpNotEqual:
				cond = 1 // NE
			case code.OpLess:
				cond = 11 // LT (signed)
			case code.OpLessEqual:
				cond = 13 // LE (signed)
			case code.OpGreater:
				cond = 12 // GT (signed)
			case code.OpGreaterEqual:
				cond = 10 // GE (signed)
			}
			emitErr = emitter.Compare(cond)
		case code.OpJump:
			emitErr = emitter.Jump(ins.operands[0])
		case code.OpJumpIfFalse:
			emitErr = emitter.JumpIfFalse(ins.operands[0])
		case code.OpChant:
			if st.stack[len(st.stack)-1] == m3Bool {
				emitErr = emitter.ChantBool()
			} else {
				emitErr = emitter.Chant()
			}
		case code.OpPop:
			emitErr = emitter.PopTyped(st.stack[len(st.stack)-1] == m3Bool)
		default:
			return nil, fmt.Errorf("AOT M3 unsupported opcode %s at byte offset %d", ins.name, ins.offset)
		}
		if emitErr != nil {
			return nil, fmt.Errorf("AOT M3 at byte offset %d: %w", ins.offset, emitErr)
		}
	}
	nativeOffsets[len(program.Instructions)] = emitter.CodeOffset()
	if err := emitter.PatchControlFlow(nativeOffsets); err != nil {
		return nil, err
	}
	if err := emitter.SetOperandDepth(0); err != nil {
		return nil, err
	}
	return emitter.Finish()
}

// CompileDarwinARM64WithM11Runtime retains the historical M11a lifecycle
// probe for scalar programs. Programs containing M12 objects use their normal
// M11b/M11c precise collector instead, so two independent heaps never overlap.
func CompileDarwinARM64WithM11Runtime(program *compiler.Bytecode) ([]byte, error) {
	if err := mochi.Validate(program); err != nil {
		return nil, fmt.Errorf("invalid Mochi bytecode: %w", err)
	}
	return compileM6WithM11(program, true)
}

// CompileDarwinARM64WithGCStress forces a collection attempt at every safe
// M12 allocation. It is intended for root-map and object-scanner testing.
func CompileDarwinARM64WithGCStress(program *compiler.Bytecode) ([]byte, error) {
	if err := mochi.Validate(program); err != nil {
		return nil, fmt.Errorf("invalid Mochi bytecode: %w", err)
	}
	return compileM6WithOptions(program, false, true)
}
