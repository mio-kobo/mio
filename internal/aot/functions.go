package aot

import (
	"fmt"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// m4State 记录每条控制流边的操作数类型及确定初始化的变量槽位。
// 局部变量只允许 int64，参数在函数入口就视为已初始化。
type m4State struct {
	stack   []m3Kind
	globals []bool
	locals  []bool
}

func (s *m4State) clone() *m4State {
	return &m4State{
		stack:   append([]m3Kind(nil), s.stack...),
		globals: append([]bool(nil), s.globals...),
		locals:  append([]bool(nil), s.locals...),
	}
}

// decodeM4Block 不改变任何 Mochi Opcode。每个函数的跳转目标都以自己的
// 字节码块为基准进行验证，主程序与函数的偏移命名空间彼此独立。
func decodeM4Block(source code.Instructions) ([]m3Instruction, map[int]int, error) {
	out := make([]m3Instruction, 0)
	boundaries := make(map[int]int)
	for pos := 0; pos < len(source); {
		op := code.Opcode(source[pos])
		def, err := code.Lookup(op)
		if err != nil {
			return nil, nil, fmt.Errorf("AOT M4 byte offset %d: %w", pos, err)
		}
		args, width, err := code.ReadOperands(def, source[pos+1:])
		if err != nil {
			return nil, nil, fmt.Errorf("AOT M4 byte offset %d: %w", pos, err)
		}
		switch op {
		case code.OpConstant, code.OpSetGlobal, code.OpGetGlobal,
			code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpNeg,
			code.OpTrue, code.OpFalse, code.OpNot,
			code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual,
			code.OpJump, code.OpJumpIfFalse, code.OpChant, code.OpPop,
			code.OpGetLocal, code.OpSetLocal, code.OpDefineLocal, code.OpCall, code.OpReturn, code.OpReturnValue,
			code.OpIntToFloat, code.OpFloatToInt, code.OpIntToFloat32, code.OpFloat32ToInt,
			code.OpFloat32ToFloat64, code.OpFloat64ToFloat32,
			code.OpArray, code.OpArrayTyped, code.OpIndex, code.OpLen, code.OpSetIndex,
			code.OpArrayMake, code.OpArrayWithCapacity, code.OpArrayPush,
			code.OpArrayCapacity, code.OpArrayReserve, code.OpArrayClone,
			code.OpArrayTruncate, code.OpArrayAppendAll,
			code.OpStruct, code.OpGetField,
			code.OpFunction, code.OpClosure, code.OpCallValue,
			code.OpGetUpvalue, code.OpSetUpvalue, code.OpNil:
		default:
			return nil, nil, fmt.Errorf("AOT M4 unsupported opcode %s at byte offset %d", def.Name, pos)
		}
		boundaries[pos] = len(out)
		out = append(out, m3Instruction{offset: pos, next: pos + 1 + width, op: op, name: def.Name, operands: args})
		pos += 1 + width
	}
	boundaries[len(source)] = len(out)
	for _, ins := range out {
		if ins.op == code.OpJump || ins.op == code.OpJumpIfFalse {
			if _, ok := boundaries[ins.operands[0]]; !ok {
				return nil, nil, fmt.Errorf("AOT M4 jump target %d is not an instruction boundary at %d", ins.operands[0], ins.offset)
			}
		}
	}
	return out, boundaries, nil
}

// analyzeM4Block 对所有分支做固定点分析。函数可递归调用，但函数签名是
// 静态的 int64 参数 / int64 或 unit 返回，数据流分析不需要递归展开函数体。
func analyzeM4Block(program *compiler.Bytecode, function *compiler.CompiledFunction, insts []m3Instruction, boundaries map[int]int) ([]*m4State, error) {
	states := make([]*m4State, len(insts)+1)
	initial := &m4State{globals: make([]bool, program.GlobalsCount)}
	location := "main"
	if function != nil {
		location = fmt.Sprintf("function %q", function.Name)
		initial.locals = make([]bool, function.LocalCount)
		for i := 0; i < function.ParameterCount; i++ {
			initial.locals[i] = true
		}
	}
	states[0] = initial
	queue := []int{0}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		if i == len(insts) {
			continue
		}
		ins := insts[i]
		st := states[i].clone()
		atErr := func(format string, args ...any) error {
			return fmt.Errorf("AOT M4 %s byte offset %d (%s): %s", location, ins.offset, ins.name, fmt.Sprintf(format, args...))
		}
		pop := func(want m3Kind) error {
			if len(st.stack) == 0 {
				return atErr("operand stack underflow")
			}
			got := st.stack[len(st.stack)-1]
			if want != 0 && want != got {
				return atErr("requires %s, got %s", want, got)
			}
			st.stack = st.stack[:len(st.stack)-1]
			return nil
		}
		push := func(kind m3Kind) error {
			if len(st.stack) >= darwinarm64.MaxM5OperandSlots {
				return atErr("operand stack limit exceeded (max %d)", darwinarm64.MaxM5OperandSlots)
			}
			st.stack = append(st.stack, kind)
			return nil
		}
		switch ins.op {
		case code.OpConstant:
			id := ins.operands[0]
			if id < 0 || id >= len(program.Constants) || program.Constants[id].Kind() != value.KindInt {
				return nil, atErr("only in-range int64 constants are supported")
			}
			if err := push(m3Int); err != nil {
				return nil, err
			}
		case code.OpSetGlobal, code.OpGetGlobal:
			if function != nil {
				return nil, atErr("global access inside functions is not supported in M4")
			}
			id := ins.operands[0]
			if id < 0 || id >= len(st.globals) {
				return nil, atErr("global slot out of range")
			}
			if ins.op == code.OpSetGlobal {
				if err := pop(m3Int); err != nil {
					return nil, err
				}
				st.globals[id] = true
			} else if err := push(m3Int); err != nil {
				return nil, err
			}
		case code.OpGetLocal, code.OpSetLocal, code.OpDefineLocal:
			if function == nil {
				return nil, atErr("local opcode outside function")
			}
			id := ins.operands[0]
			if id < 0 || id >= len(st.locals) {
				return nil, atErr("local slot %d out of range", id)
			}
			if ins.op == code.OpGetLocal {
				if err := push(m3Int); err != nil {
					return nil, err
				}
			} else {
				if err := pop(m3Int); err != nil {
					return nil, err
				}
				st.locals[id] = true
			}
		case code.OpTrue, code.OpFalse:
			if err := push(m3Bool); err != nil {
				return nil, err
			}
		case code.OpNot:
			if err := pop(m3Bool); err != nil {
				return nil, err
			}
			if err := push(m3Bool); err != nil {
				return nil, err
			}
		case code.OpNeg:
			if err := pop(m3Int); err != nil {
				return nil, err
			}
			if err := push(m3Int); err != nil {
				return nil, err
			}
		case code.OpAdd, code.OpSub, code.OpMul:
			if err := pop(m3Int); err != nil {
				return nil, err
			}
			if err := pop(m3Int); err != nil {
				return nil, err
			}
			if err := push(m3Int); err != nil {
				return nil, err
			}
		case code.OpEqual, code.OpNotEqual:
			if len(st.stack) < 2 {
				return nil, atErr("operand stack underflow")
			}
			right := st.stack[len(st.stack)-1]
			left := st.stack[len(st.stack)-2]
			if right != left {
				return nil, atErr("comparison type mismatch: %s and %s", left, right)
			}
			st.stack = st.stack[:len(st.stack)-2]
			if err := push(m3Bool); err != nil {
				return nil, err
			}
		case code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			if err := pop(m3Int); err != nil {
				return nil, err
			}
			if err := pop(m3Int); err != nil {
				return nil, err
			}
			if err := push(m3Bool); err != nil {
				return nil, err
			}
		case code.OpJumpIfFalse:
			if err := pop(m3Bool); err != nil {
				return nil, err
			}
		case code.OpChant, code.OpPop:
			if err := pop(0); err != nil {
				return nil, err
			}
		case code.OpCall:
			id := ins.operands[0]
			if id < 0 || id >= len(program.Functions) {
				return nil, atErr("unknown function %d", id)
			}
			callee := program.Functions[id]
			for j := 0; j < callee.ParameterCount; j++ {
				if err := pop(m3Int); err != nil {
					return nil, err
				}
			}
			if callee.ReturnsValue {
				if err := push(m3Int); err != nil {
					return nil, err
				}
			}
		case code.OpReturn, code.OpReturnValue:
			if function == nil {
				return nil, atErr("return outside function")
			}
			if ins.op == code.OpReturn {
				if function.ReturnsValue {
					return nil, atErr("value-returning function uses OpReturn")
				}
				if len(st.stack) != 0 {
					return nil, atErr("unit return must have an empty operand stack")
				}
			} else {
				if !function.ReturnsValue {
					return nil, atErr("unit function uses OpReturnValue")
				}
				if len(st.stack) != 1 || st.stack[0] != m3Int {
					return nil, atErr("OpReturnValue requires exactly one int64 result")
				}
			}
		}
		if ins.op == code.OpReturn || ins.op == code.OpReturnValue {
			continue
		}
		succ := []int{boundaries[ins.next]}
		if ins.op == code.OpJump {
			succ = []int{boundaries[ins.operands[0]]}
		}
		if ins.op == code.OpJumpIfFalse {
			succ = append(succ, boundaries[ins.operands[0]])
		}
		for _, to := range succ {
			existing := states[to]
			if existing == nil {
				states[to] = st.clone()
				queue = append(queue, to)
				continue
			}
			if len(existing.stack) != len(st.stack) {
				return nil, atErr("stack depth mismatch at branch target %d", to)
			}
			for j := range st.stack {
				if existing.stack[j] != st.stack[j] {
					return nil, atErr("stack type mismatch at branch target %d", to)
				}
			}
			changed := false
			for j := range st.globals {
				if existing.globals[j] && !st.globals[j] {
					existing.globals[j] = false
					changed = true
				}
			}
			for j := range st.locals {
				if existing.locals[j] && !st.locals[j] {
					existing.locals[j] = false
					changed = true
				}
			}
			if changed {
				queue = append(queue, to)
			}
		}
	}
	for i, st := range states {
		if st == nil {
			continue
		}
		if i == len(insts) {
			if function != nil {
				return nil, fmt.Errorf("AOT M4 function %q may fall through without return", function.Name)
			}
			if len(st.stack) != 0 {
				return nil, fmt.Errorf("AOT M4 main exited with %d operand stack entries", len(st.stack))
			}
			continue
		}
		ins := insts[i]
		if ins.op == code.OpGetGlobal && !st.globals[ins.operands[0]] {
			return nil, fmt.Errorf("AOT M4 global slot %d may be uninitialized at byte offset %d", ins.operands[0], ins.offset)
		}
		if ins.op == code.OpGetLocal && !st.locals[ins.operands[0]] {
			return nil, fmt.Errorf("AOT M4 local slot %d may be uninitialized in function %q at byte offset %d", ins.operands[0], function.Name, ins.offset)
		}
	}
	return states, nil
}

// compileM4 为 M4 可表示的静态 int64 函数生成真正的原生调用指令。
// 参数/局部变量通过原生 Stack Frame 存放；递归与嵌套调用不回退到 VM。
func compileM4(program *compiler.Bytecode) ([]byte, error) {
	if program.GlobalsCount < 0 || program.GlobalsCount > darwinarm64.MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M4 global slot count %d exceeds limit %d", program.GlobalsCount, darwinarm64.MaxGlobalSlots)
	}
	for id, v := range program.Constants {
		if v.Kind() != value.KindInt {
			return nil, fmt.Errorf("AOT M4 constant %d has unsupported type %s (only int64)", id, v.Kind())
		}
	}
	for id, fn := range program.Functions {
		if fn.ID != id {
			return nil, fmt.Errorf("AOT M4 function id mismatch: index=%d id=%d", id, fn.ID)
		}
		if fn.ParameterCount < 0 || fn.ParameterCount > darwinarm64.MaxM15Parameters || fn.LocalCount < fn.ParameterCount || fn.LocalCount > darwinarm64.MaxM4Locals {
			return nil, fmt.Errorf("AOT M4 function %q exceeds native frame limits (parameters=%d locals=%d)", fn.Name, fn.ParameterCount, fn.LocalCount)
		}
	}

	needsStack, err := needsM5Stack(program)
	if err != nil {
		return nil, err
	}
	if needsStack {
		return compileM5(program)
	}

	main, err := compileM4Block(program, nil, program.Instructions)
	if err != nil {
		return nil, err
	}
	functions := make([]*darwinarm64.IntegerEmitter, len(program.Functions))
	for i := range program.Functions {
		fn := &program.Functions[i]
		functions[i], err = compileM4Block(program, fn, fn.Instructions)
		if err != nil {
			return nil, err
		}
	}
	return main.FinishWithFunctions(functions)
}

func compileM4Block(program *compiler.Bytecode, fn *compiler.CompiledFunction, source code.Instructions) (*darwinarm64.IntegerEmitter, error) {
	ins, boundaries, err := decodeM4Block(source)
	if err != nil {
		return nil, err
	}
	states, err := analyzeM4Block(program, fn, ins, boundaries)
	if err != nil {
		return nil, err
	}
	e := darwinarm64.NewIntegerEmitter()
	if err := e.SetGlobalCount(program.GlobalsCount); err != nil {
		return nil, err
	}
	if fn == nil {
		if err := e.EnableControlFlow(); err != nil {
			return nil, err
		}
		if err := e.EnableFunctionCalls(); err != nil {
			return nil, err
		}
	} else if err := e.EnableFunction(fn.LocalCount, fn.ParameterCount); err != nil {
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
			if st.stack[len(st.stack)-1] == m3Bool {
				emitErr = e.ChantBool()
			} else {
				emitErr = e.Chant()
			}
		case code.OpPop:
			if fn != nil {
				emitErr = e.Discard()
			} else {
				emitErr = e.PopTyped(st.stack[len(st.stack)-1] == m3Bool)
			}
		case code.OpCall:
			callee := program.Functions[inst.operands[0]]
			emitErr = e.CallNative(inst.operands[0], callee.ParameterCount, callee.ReturnsValue)
		case code.OpReturn:
			emitErr = e.ReturnUnit()
		case code.OpReturnValue:
			emitErr = e.ReturnValue()
		default:
			return nil, fmt.Errorf("AOT M4 unsupported opcode %s at offset %d", inst.name, inst.offset)
		}
		if emitErr != nil {
			return nil, fmt.Errorf("AOT M4 at byte offset %d: %w", inst.offset, emitErr)
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
