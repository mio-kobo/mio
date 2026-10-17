package aot

import (
	"fmt"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// m3Kind 只区分 M3 真正支持的运行时类型，不引入通用 Value 装箱。
// 全局槽位暂时只存储 int64；布尔值只存在于表达式操作数栈中。
type m3Kind uint8

const (
	m3Int m3Kind = iota + 1
	m3Bool
)

func (k m3Kind) String() string {
	if k == m3Int {
		return "int64"
	}
	if k == m3Bool {
		return "bool"
	}
	return "invalid"
}

type m3Instruction struct {
	offset   int
	next     int
	op       code.Opcode
	name     string
	operands []int
}

type m3State struct {
	stack       []m3Kind
	initialized []bool
}

func (s *m3State) clone() *m3State {
	return &m3State{
		stack:       append([]m3Kind(nil), s.stack...),
		initialized: append([]bool(nil), s.initialized...),
	}
}

// decodeM3 在编译前先解析完整指令块。所有 Opcode 都经过白名单检查，
// 未实现的指令即使位于不可达代码中也不能被静默忽略。
func decodeM3(program *compiler.Bytecode) ([]m3Instruction, map[int]int, error) {
	insts := make([]m3Instruction, 0)
	boundaries := make(map[int]int)
	instructions := program.Instructions
	for pos := 0; pos < len(instructions); {
		op := code.Opcode(instructions[pos])
		def, err := code.Lookup(op)
		if err != nil {
			return nil, nil, fmt.Errorf("AOT M3 at byte offset %d: %w", pos, err)
		}
		operands, width, err := code.ReadOperands(def, instructions[pos+1:])
		if err != nil {
			return nil, nil, fmt.Errorf("AOT M3 at byte offset %d: %w", pos, err)
		}
		switch op {
		case code.OpConstant, code.OpSetGlobal, code.OpGetGlobal,
			code.OpAdd, code.OpSub, code.OpMul, code.OpNeg,
			code.OpTrue, code.OpFalse, code.OpNot,
			code.OpEqual, code.OpNotEqual,
			code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual,
			code.OpJump, code.OpJumpIfFalse, code.OpChant, code.OpPop:
		default:
			return nil, nil, fmt.Errorf("AOT M3 unsupported opcode %s at byte offset %d", def.Name, pos)
		}
		boundaries[pos] = len(insts)
		insts = append(insts, m3Instruction{offset: pos, next: pos + 1 + width, op: op, name: def.Name, operands: operands})
		pos += 1 + width
	}
	boundaries[len(instructions)] = len(insts)
	for _, ins := range insts {
		if ins.op == code.OpJump || ins.op == code.OpJumpIfFalse {
			target := ins.operands[0]
			if _, ok := boundaries[target]; !ok {
				return nil, nil, fmt.Errorf("AOT M3 jump target %d is not an instruction boundary at %d", target, ins.offset)
			}
		}
	}
	return insts, boundaries, nil
}

// analyzeM3 使用前向工作队列分析各分支的栈类型与全局槽位确定赋值。
// 汇合处的操作数栈必须完全一致；已初始化集合取交集，避免将仅在
// 某条路径上赋值的全局变量当作所有路径都已赋值。
func analyzeM3(program *compiler.Bytecode, insts []m3Instruction, boundaries map[int]int) ([]*m3State, error) {
	states := make([]*m3State, len(insts)+1)
	states[0] = &m3State{initialized: make([]bool, program.GlobalsCount)}
	queue := []int{0}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		if i == len(insts) {
			continue
		}
		ins := insts[i]
		st := states[i].clone()
		atErr := func(msg string) error {
			return fmt.Errorf("AOT M3 at byte offset %d (%s): %s", ins.offset, ins.name, msg)
		}
		pop := func(want m3Kind) error {
			if len(st.stack) < 1 {
				return atErr("operand stack underflow")
			}
			got := st.stack[len(st.stack)-1]
			if want != 0 && got != want {
				return atErr(fmt.Sprintf("requires %s, got %s", want, got))
			}
			st.stack = st.stack[:len(st.stack)-1]
			return nil
		}
		push := func(k m3Kind) error {
			if len(st.stack) >= 7 {
				return atErr("operand stack limit exceeded (max 7)")
			}
			st.stack = append(st.stack, k)
			return nil
		}
		switch ins.op {
		case code.OpConstant:
			index := ins.operands[0]
			if index < 0 || index >= len(program.Constants) {
				return nil, atErr("constant index out of range")
			}
			if program.Constants[index].Kind() != value.KindInt {
				return nil, atErr("only int64 constants are supported")
			}
			if err := push(m3Int); err != nil {
				return nil, err
			}
		case code.OpSetGlobal:
			id := ins.operands[0]
			if id < 0 || id >= len(st.initialized) {
				return nil, atErr("global slot out of range")
			}
			if err := pop(m3Int); err != nil {
				return nil, err
			}
			st.initialized[id] = true
		case code.OpGetGlobal:
			id := ins.operands[0]
			if id < 0 || id >= len(st.initialized) {
				return nil, atErr("global slot out of range")
			}
			// 未初始化检查必须在数据流收敛后进行，不能采用第一次访问时的状态。
			if err := push(m3Int); err != nil {
				return nil, err
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
			if left != right {
				return nil, atErr(fmt.Sprintf("comparison type mismatch: %s and %s", left, right))
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
				return nil, atErr(fmt.Sprintf("stack depth mismatch at branch target %d", to))
			}
			for j := range st.stack {
				if st.stack[j] != existing.stack[j] {
					return nil, atErr(fmt.Sprintf("stack type mismatch at branch target %d", to))
				}
			}
			changed := false
			for j := range existing.initialized {
				newVal := existing.initialized[j] && st.initialized[j]
				if existing.initialized[j] != newVal {
					existing.initialized[j] = newVal
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
			if len(st.stack) != 0 {
				return nil, fmt.Errorf("AOT M3 main exited with %d operand stack entries", len(st.stack))
			}
			continue
		}
		ins := insts[i]
		if ins.op == code.OpGetGlobal && !st.initialized[ins.operands[0]] {
			return nil, fmt.Errorf("AOT M3 global slot %d may be uninitialized at byte offset %d", ins.operands[0], ins.offset)
		}
	}
	return states, nil
}
