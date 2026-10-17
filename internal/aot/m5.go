package aot

import (
	"fmt"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
)

// m5MaxDepth 返回已经固定点分析的最大操作数深度。
// 这里使用可达状态，而不是简单统计源码中的 Push 数量。
func m5MaxDepth(states []*m4State) int {
	maximum := 0
	for _, state := range states {
		if state != nil && len(state.stack) > maximum {
			maximum = len(state.stack)
		}
	}
	return maximum
}

type m5Block struct {
	name         string
	function     *compiler.CompiledFunction
	instructions code.Instructions
}

// needsM5Stack 判断是否需要超过 7 个寄存器槽的表达式栈。
// 所有块先经过固定点分析，再让整个程序选用同一种函数调用布局。
func needsM5Stack(program *compiler.Bytecode) (bool, error) {
	blocks := []m5Block{{name: "main", instructions: program.Instructions}}
	for i := range program.Functions {
		fn := &program.Functions[i]
		blocks = append(blocks, m5Block{name: fn.Name, function: fn, instructions: fn.Instructions})
	}
	large := false
	for _, block := range blocks {
		ins, boundaries, err := decodeM4Block(block.instructions)
		if err != nil {
			return false, fmt.Errorf("AOT M5 %s: %w", block.name, err)
		}
		states, err := analyzeM4Block(program, block.function, ins, boundaries)
		if err != nil {
			return false, err
		}
		if m5MaxDepth(states) > 7 {
			large = true
		}
	}
	return large, nil
}

// compileM5 将全部函数切换到相同的内存操作数栈约定，避免调用边界混用 ABI。
func compileM5(program *compiler.Bytecode) ([]byte, error) {
	main, err := compileM5Block(program, nil, program.Instructions)
	if err != nil {
		return nil, err
	}
	functions := make([]*darwinarm64.StackEmitter, len(program.Functions))
	for i := range program.Functions {
		functions[i], err = compileM5Block(program, &program.Functions[i], program.Functions[i].Instructions)
		if err != nil {
			return nil, err
		}
	}
	return main.FinishWithFunctions(functions)
}

// compileM5Block 为每个 Mochi 指令块独立建立帧布局和跳转地址映射。
func compileM5Block(program *compiler.Bytecode, fn *compiler.CompiledFunction, source code.Instructions) (*darwinarm64.StackEmitter, error) {
	ins, boundaries, err := decodeM4Block(source)
	if err != nil {
		return nil, err
	}
	states, err := analyzeM4Block(program, fn, ins, boundaries)
	if err != nil {
		return nil, err
	}
	localCount, paramCount := 0, 0
	if fn != nil {
		localCount, paramCount = fn.LocalCount, fn.ParameterCount
	}
	e, err := darwinarm64.NewStackEmitter(program.GlobalsCount, localCount, paramCount, m5MaxDepth(states), fn != nil)
	if err != nil {
		return nil, err
	}
	nativeOffsets := make(map[int]int, len(ins)+1)
	for i, inst := range ins {
		nativeOffsets[inst.offset] = e.CodeOffset()
		st := states[i]
		if st == nil {
			continue
		}
		if err := e.SetOperandDepth(len(st.stack)); err != nil {
			return nil, err
		}
		var emitErr error
		switch inst.op {
		case code.OpConstant:
			n, _ := program.Constants[inst.operands[0]].Int64()
			emitErr = e.PushInt64(n)
		case code.OpSetGlobal:
			emitErr = e.SetGlobal(inst.operands[0])
		case code.OpGetGlobal:
			emitErr = e.GetGlobal(inst.operands[0])
		case code.OpSetLocal, code.OpDefineLocal:
			emitErr = e.SetLocal(inst.operands[0])
		case code.OpGetLocal:
			emitErr = e.GetLocal(inst.operands[0])
		case code.OpAdd:
			emitErr = e.Add()
		case code.OpSub:
			emitErr = e.Sub()
		case code.OpMul:
			emitErr = e.Mul()
		case code.OpNeg:
			emitErr = e.Neg()
		case code.OpTrue:
			emitErr = e.PushBool(true)
		case code.OpFalse:
			emitErr = e.PushBool(false)
		case code.OpNot:
			emitErr = e.NotBool()
		case code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			cond := uint32(0)
			switch inst.op {
			case code.OpEqual:
				cond = 0
			case code.OpNotEqual:
				cond = 1
			case code.OpLess:
				cond = 11
			case code.OpLessEqual:
				cond = 13
			case code.OpGreater:
				cond = 12
			case code.OpGreaterEqual:
				cond = 10
			}
			emitErr = e.Compare(cond)
		case code.OpJump:
			emitErr = e.Jump(inst.operands[0])
		case code.OpJumpIfFalse:
			emitErr = e.JumpIfFalse(inst.operands[0])
		case code.OpChant:
			emitErr = e.Chant(st.stack[len(st.stack)-1] == m3Bool)
		case code.OpPop:
			emitErr = e.Pop(st.stack[len(st.stack)-1] == m3Bool)
		case code.OpCall:
			callee := program.Functions[inst.operands[0]]
			emitErr = e.CallNative(inst.operands[0], callee.ParameterCount, callee.ReturnsValue)
		case code.OpReturn:
			emitErr = e.ReturnUnit()
		case code.OpReturnValue:
			emitErr = e.ReturnValue()
		default:
			return nil, fmt.Errorf("AOT M5 unsupported opcode %s at offset %d", inst.name, inst.offset)
		}
		if emitErr != nil {
			return nil, fmt.Errorf("AOT M5 byte offset %d (%s): %w", inst.offset, inst.name, emitErr)
		}
	}
	nativeOffsets[len(source)] = e.CodeOffset()
	if err := e.PatchControlFlow(nativeOffsets); err != nil {
		return nil, err
	}
	if err := e.SetOperandDepth(0); err != nil {
		return nil, err
	}
	if fn != nil {
		if err := e.FinishFunction(); err != nil {
			return nil, err
		}
	}
	return e, nil
}
