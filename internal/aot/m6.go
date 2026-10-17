package aot

import (
	"fmt"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// M6 只增加 AOT 可以理解的类型与指令，不修改 Mochi 格式。
// 所有浮点值都以 IEEE 754 位模式保存在原有的 8 字节槽位里。
type m6Kind uint8

const (
	m6Unknown m6Kind = iota
	m6Int
	m6Bool
	m6Float32
	m6Float64
)

func (k m6Kind) String() string {
	switch k {
	case m6Int:
		return "int64"
	case m6Bool:
		return "bool"
	case m6Float32:
		return "float32"
	case m6Float64:
		return "float64"
	default:
		return "unknown"
	}
}

func m6ValueKind(v value.Value) m6Kind {
	switch v.Kind() {
	case value.KindInt:
		return m6Int
	case value.KindBool:
		return m6Bool
	case value.KindFloat32:
		return m6Float32
	case value.KindFloat:
		return m6Float64
	default:
		return m6Unknown
	}
}

func needsM6(program *compiler.Bytecode) bool {
	for _, c := range program.Constants {
		if c.Kind() == value.KindFloat32 || c.Kind() == value.KindFloat {
			return true
		}
	}
	blocks := []code.Instructions{program.Instructions}
	for _, fn := range program.Functions {
		blocks = append(blocks, fn.Instructions)
	}
	for _, src := range blocks {
		for i := 0; i < len(src); {
			op := code.Opcode(src[i])
			switch op {
			case code.OpIntToFloat, code.OpFloatToInt, code.OpIntToFloat32,
				code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32:
				return true
			}
			def, err := code.Lookup(op)
			if err != nil {
				return false // 交给既有解析器报告无效指令
			}
			_, width, err := code.ReadOperands(def, src[i+1:])
			if err != nil {
				return false
			}
			i += 1 + width
		}
	}
	return false
}

// m6Union 把函数参数、返回值、局部槽位和操作数类型约束在一起。
// 函数签名目前没有携带参数的静态类型，这里通过全部调用点与指令约束求解。
type m6Union struct {
	parent []int
	kind   []m6Kind
}

func (u *m6Union) newKind(k m6Kind) int {
	i := len(u.parent)
	u.parent = append(u.parent, i)
	u.kind = append(u.kind, k)
	return i
}
func (u *m6Union) root(i int) int {
	if u.parent[i] != i {
		u.parent[i] = u.root(u.parent[i])
	}
	return u.parent[i]
}
func (u *m6Union) get(i int) m6Kind { return u.kind[u.root(i)] }
func (u *m6Union) join(a, b int) error {
	a, b = u.root(a), u.root(b)
	if a == b {
		return nil
	}
	if u.kind[a] != m6Unknown && u.kind[b] != m6Unknown && u.kind[a] != u.kind[b] {
		return fmt.Errorf("type mismatch: %s and %s", u.kind[a], u.kind[b])
	}
	u.parent[b] = a
	if u.kind[a] == m6Unknown {
		u.kind[a] = u.kind[b]
	}
	return nil
}
func (u *m6Union) require(a int, k m6Kind) error {
	return u.join(a, u.newKind(k))
}

type m6Reach struct {
	depth   int
	globals []bool
	locals  []bool
}

func (s *m6Reach) copy() *m6Reach {
	return &m6Reach{s.depth, append([]bool(nil), s.globals...), append([]bool(nil), s.locals...)}
}

type m6Block struct {
	name       string
	fn         *compiler.CompiledFunction
	inst       []m3Instruction
	boundaries map[int]int
	reach      []*m6Reach
	entry      [][]int
	endKind    [][]m6Kind
}

type m6Analysis struct {
	u       *m6Union
	blocks  []*m6Block
	globals []int
	locals  [][]int
	returns []int
}

// m6Reachability 先独立验证栈深度、局部初始化与每条跳转的边界。
// 这样类型约束求解不需要将递归函数在编译期展开。
func m6Reachability(p *compiler.Bytecode, b *m6Block) error {
	n := len(b.inst)
	b.reach = make([]*m6Reach, n+1)
	init := &m6Reach{globals: make([]bool, p.GlobalsCount)}
	if b.fn != nil {
		init.locals = make([]bool, b.fn.LocalCount)
		for j := 0; j < b.fn.ParameterCount; j++ {
			init.locals[j] = true
		}
	}
	b.reach[0] = init
	queue := []int{0}
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		if i == n {
			continue
		}
		ins := b.inst[i]
		st := b.reach[i].copy()
		fail := func(msg string) error {
			return fmt.Errorf("AOT M6 %s offset %d (%s): %s", b.name, ins.offset, ins.name, msg)
		}
		need := 0
		push := 0
		switch ins.op {
		case code.OpConstant, code.OpTrue, code.OpFalse, code.OpGetGlobal, code.OpGetLocal:
			push = 1
		case code.OpSetGlobal, code.OpSetLocal, code.OpDefineLocal, code.OpJumpIfFalse, code.OpChant, code.OpPop:
			need = 1
		case code.OpNeg, code.OpNot, code.OpIntToFloat, code.OpFloatToInt,
			code.OpIntToFloat32, code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32:
			need, push = 1, 1
		case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual, code.OpNotEqual,
			code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			need, push = 2, 1
		case code.OpCall:
			id := ins.operands[0]
			if id < 0 || id >= len(p.Functions) {
				return fail("function index out of range")
			}
			need = p.Functions[id].ParameterCount
			if p.Functions[id].ReturnsValue {
				push = 1
			}
		case code.OpReturn:
			if b.fn == nil || b.fn.ReturnsValue || st.depth != 0 {
				return fail("invalid unit return")
			}
		case code.OpReturnValue:
			if b.fn == nil || !b.fn.ReturnsValue || st.depth != 1 {
				return fail("value return requires one stack item")
			}
		case code.OpJump:
		default:
			return fail("unsupported operation")
		}
		if st.depth < need {
			return fail("operand stack underflow")
		}
		st.depth += push - need
		if st.depth > darwinarm64.MaxM5OperandSlots {
			return fail("operand stack exceeds M5 frame limit")
		}
		switch ins.op {
		case code.OpSetGlobal, code.OpGetGlobal:
			if b.fn != nil {
				return fail("global access inside functions is not supported yet")
			}
			id := ins.operands[0]
			if id < 0 || id >= len(st.globals) {
				return fail("global slot out of range")
			}
			if ins.op == code.OpSetGlobal {
				st.globals[id] = true
			}
		case code.OpGetLocal, code.OpSetLocal, code.OpDefineLocal:
			if b.fn == nil {
				return fail("local instruction outside function")
			}
			id := ins.operands[0]
			if id < 0 || id >= len(st.locals) {
				return fail("local slot out of range")
			}
			if ins.op != code.OpGetLocal {
				st.locals[id] = true
			}
		}
		if ins.op == code.OpReturn || ins.op == code.OpReturnValue {
			continue
		}
		succ := []int{b.boundaries[ins.next]}
		if ins.op == code.OpJump {
			succ = []int{b.boundaries[ins.operands[0]]}
		} else if ins.op == code.OpJumpIfFalse {
			succ = append(succ, b.boundaries[ins.operands[0]])
		}
		for _, to := range succ {
			current := b.reach[to]
			if current == nil {
				b.reach[to] = st.copy()
				queue = append(queue, to)
				continue
			}
			if current.depth != st.depth {
				return fail("operand depth mismatch at control-flow merge")
			}
			changed := false
			for j := range st.globals {
				if current.globals[j] && !st.globals[j] {
					current.globals[j], changed = false, true
				}
			}
			for j := range st.locals {
				if current.locals[j] && !st.locals[j] {
					current.locals[j], changed = false, true
				}
			}
			if changed {
				queue = append(queue, to)
			}
		}
	}
	for i, st := range b.reach {
		if st == nil {
			continue
		}
		if i == n {
			if b.fn != nil {
				return fmt.Errorf("AOT M6 function %q falls through without return", b.name)
			}
			if st.depth != 0 {
				return fmt.Errorf("AOT M6 main stack not empty at exit")
			}
			continue
		}
		ins := b.inst[i]
		if ins.op == code.OpGetGlobal && !st.globals[ins.operands[0]] {
			return fmt.Errorf("AOT M6 uninitialized global %d at %s offset %d", ins.operands[0], b.name, ins.offset)
		}
		if ins.op == code.OpGetLocal && !st.locals[ins.operands[0]] {
			return fmt.Errorf("AOT M6 uninitialized local %d at %s offset %d", ins.operands[0], b.name, ins.offset)
		}
	}
	return nil
}

// analyzeM6 把同一物理槽位及所有调用点的类型统一求解。
// 无法确定类型时拒绝编译，避免把 float 位模式误当成 int64。
func analyzeM6(p *compiler.Bytecode) (*m6Analysis, error) {
	a := &m6Analysis{u: &m6Union{}}
	for range p.GlobalsCount {
		a.globals = append(a.globals, a.u.newKind(m6Unknown))
	}
	for i, fn := range p.Functions {
		if fn.ID != i || fn.ParameterCount < 0 || fn.ParameterCount > darwinarm64.MaxM4Parameters || fn.LocalCount < fn.ParameterCount || fn.LocalCount > darwinarm64.MaxM4Locals || len(fn.Captures) != 0 {
			return nil, fmt.Errorf("AOT M6 unsupported function frame %q", fn.Name)
		}
		locals := make([]int, fn.LocalCount)
		for j := range locals {
			locals[j] = a.u.newKind(m6Unknown)
		}
		a.locals = append(a.locals, locals)
		if fn.ReturnsValue {
			a.returns = append(a.returns, a.u.newKind(m6Unknown))
		} else {
			a.returns = append(a.returns, -1)
		}
	}
	blocks := []*m6Block{{name: "main"}}
	for i := range p.Functions {
		blocks = append(blocks, &m6Block{name: p.Functions[i].Name, fn: &p.Functions[i]})
	}
	for _, b := range blocks {
		src := p.Instructions
		if b.fn != nil {
			src = b.fn.Instructions
		}
		var err error
		b.inst, b.boundaries, err = decodeM4Block(src)
		if err != nil {
			return nil, err
		}
		if err := m6Reachability(p, b); err != nil {
			return nil, err
		}
		b.entry = make([][]int, len(b.reach))
		for i, st := range b.reach {
			if st == nil {
				continue
			}
			b.entry[i] = make([]int, st.depth)
			for j := range b.entry[i] {
				b.entry[i][j] = a.u.newKind(m6Unknown)
			}
		}
	}
	a.blocks = blocks
	for bi, b := range a.blocks {
		for i, ins := range b.inst {
			if b.reach[i] == nil {
				continue
			}
			stack := append([]int(nil), b.entry[i]...)
			fail := func(err error) error {
				return fmt.Errorf("AOT M6 %s offset %d (%s): %w", b.name, ins.offset, ins.name, err)
			}
			pop := func() int {
				last := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				return last
			}
			push := func(k m6Kind) { stack = append(stack, a.u.newKind(k)) }
			require := func(v int, k m6Kind) error { return a.u.require(v, k) }
			join := func(x, y int) error { return a.u.join(x, y) }
			switch ins.op {
			case code.OpConstant:
				id := ins.operands[0]
				if id < 0 || id >= len(p.Constants) {
					return nil, fail(fmt.Errorf("constant index out of range"))
				}
				k := m6ValueKind(p.Constants[id])
				if k == m6Unknown {
					return nil, fail(fmt.Errorf("unsupported constant type %s", p.Constants[id].Kind()))
				}
				push(k)
			case code.OpTrue, code.OpFalse:
				push(m6Bool)
			case code.OpGetGlobal:
				stack = append(stack, a.globals[ins.operands[0]])
			case code.OpSetGlobal:
				if err := join(pop(), a.globals[ins.operands[0]]); err != nil {
					return nil, fail(err)
				}
			case code.OpGetLocal:
				stack = append(stack, a.locals[bi-1][ins.operands[0]])
			case code.OpSetLocal, code.OpDefineLocal:
				if err := join(pop(), a.locals[bi-1][ins.operands[0]]); err != nil {
					return nil, fail(err)
				}
			case code.OpNot, code.OpJumpIfFalse:
				if err := require(pop(), m6Bool); err != nil {
					return nil, fail(err)
				}
				if ins.op == code.OpNot {
					push(m6Bool)
				}
			case code.OpNeg:
				v := pop()
				stack = append(stack, v)
			case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv:
				right, left := pop(), pop()
				if err := join(left, right); err != nil {
					return nil, fail(err)
				}
				stack = append(stack, left)
			case code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
				right, left := pop(), pop()
				if err := join(left, right); err != nil {
					return nil, fail(err)
				}
				push(m6Bool)
			case code.OpIntToFloat, code.OpIntToFloat32:
				if err := require(pop(), m6Int); err != nil {
					return nil, fail(err)
				}
				if ins.op == code.OpIntToFloat {
					push(m6Float64)
				} else {
					push(m6Float32)
				}
			case code.OpFloatToInt, code.OpFloat32ToInt:
				k := m6Float64
				if ins.op == code.OpFloat32ToInt {
					k = m6Float32
				}
				if err := require(pop(), k); err != nil {
					return nil, fail(err)
				}
				push(m6Int)
			case code.OpFloat32ToFloat64:
				if err := require(pop(), m6Float32); err != nil {
					return nil, fail(err)
				}
				push(m6Float64)
			case code.OpFloat64ToFloat32:
				if err := require(pop(), m6Float64); err != nil {
					return nil, fail(err)
				}
				push(m6Float32)
			case code.OpChant, code.OpPop:
				pop()
			case code.OpCall:
				callee := p.Functions[ins.operands[0]]
				for j := callee.ParameterCount - 1; j >= 0; j-- {
					if err := join(pop(), a.locals[ins.operands[0]][j]); err != nil {
						return nil, fail(err)
					}
				}
				if callee.ReturnsValue {
					stack = append(stack, a.returns[ins.operands[0]])
				}
			case code.OpReturnValue:
				if err := join(pop(), a.returns[bi-1]); err != nil {
					return nil, fail(err)
				}
			case code.OpJump, code.OpReturn:
			default:
				return nil, fail(fmt.Errorf("unsupported operation"))
			}
			if ins.op == code.OpReturn || ins.op == code.OpReturnValue {
				continue
			}
			succ := []int{b.boundaries[ins.next]}
			if ins.op == code.OpJump {
				succ = []int{b.boundaries[ins.operands[0]]}
			} else if ins.op == code.OpJumpIfFalse {
				succ = append(succ, b.boundaries[ins.operands[0]])
			}
			for _, to := range succ {
				if len(stack) != len(b.entry[to]) {
					return nil, fail(fmt.Errorf("stack depth mismatch"))
				}
				for j := range stack {
					if err := join(stack[j], b.entry[to][j]); err != nil {
						return nil, fail(err)
					}
				}
			}
		}
	}
	for _, b := range a.blocks {
		b.endKind = make([][]m6Kind, len(b.entry))
		for i, st := range b.reach {
			if st == nil {
				continue
			}
			b.endKind[i] = make([]m6Kind, len(b.entry[i]))
			for j, node := range b.entry[i] {
				k := a.u.get(node)
				if k == m6Unknown {
					return nil, fmt.Errorf("AOT M6 unknown operand type at %s byte offset %d", b.name, i)
				}
				b.endKind[i][j] = k
			}
		}
		for i, ins := range b.inst {
			if b.reach[i] == nil {
				continue
			}
			if ins.op == code.OpAdd || ins.op == code.OpSub || ins.op == code.OpMul || ins.op == code.OpDiv || ins.op == code.OpNeg || ins.op == code.OpLess || ins.op == code.OpLessEqual || ins.op == code.OpGreater || ins.op == code.OpGreaterEqual {
				k := b.endKind[i][len(b.endKind[i])-1]
				if k == m6Bool || ins.op == code.OpDiv && k == m6Int {
					return nil, fmt.Errorf("AOT M6 unsupported arithmetic type %s for %s at %s offset %d", k, ins.name, b.name, ins.offset)
				}
			}
		}
	}
	return a, nil
}

func compileM6(p *compiler.Bytecode) ([]byte, error) {
	if p.GlobalsCount < 0 || p.GlobalsCount > darwinarm64.MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M6 invalid global count %d", p.GlobalsCount)
	}
	if len(p.Functions) > 1<<16 {
		return nil, fmt.Errorf("AOT M6 function count out of range")
	}
	a, err := analyzeM6(p)
	if err != nil {
		return nil, err
	}
	all := make([]*darwinarm64.StackEmitter, len(a.blocks))
	for bi, b := range a.blocks {
		maximum := 0
		for _, st := range b.reach {
			if st != nil && st.depth > maximum {
				maximum = st.depth
			}
		}
		locals, params := 0, 0
		if b.fn != nil {
			locals, params = b.fn.LocalCount, b.fn.ParameterCount
		}
		e, err := darwinarm64.NewStackEmitter(p.GlobalsCount, locals, params, maximum, b.fn != nil)
		if err != nil {
			return nil, err
		}
		mapping := make(map[int]int, len(b.inst)+1)
		for i, ins := range b.inst {
			mapping[ins.offset] = e.CodeOffset()
			if b.reach[i] == nil {
				continue
			}
			st := b.endKind[i]
			if err := e.SetOperandDepth(len(st)); err != nil {
				return nil, err
			}
			k := m6Unknown
			if len(st) != 0 {
				k = st[len(st)-1]
			}
			var emitErr error
			switch ins.op {
			case code.OpConstant:
				v := p.Constants[ins.operands[0]]
				switch m6ValueKind(v) {
				case m6Int:
					n, _ := v.Int64()
					emitErr = e.PushInt64(n)
				case m6Float64:
					n, _ := v.Float64()
					emitErr = e.PushFloat64(n)
				case m6Float32:
					n, _ := v.Float32()
					emitErr = e.PushFloat32(n)
				case m6Bool:
					n, _ := v.Bool()
					emitErr = e.PushBool(n)
				}
			case code.OpTrue, code.OpFalse:
				emitErr = e.PushBool(ins.op == code.OpTrue)
			case code.OpSetGlobal:
				emitErr = e.SetGlobal(ins.operands[0])
			case code.OpGetGlobal:
				emitErr = e.GetGlobal(ins.operands[0])
			case code.OpSetLocal, code.OpDefineLocal:
				emitErr = e.SetLocal(ins.operands[0])
			case code.OpGetLocal:
				emitErr = e.GetLocal(ins.operands[0])
			case code.OpNot:
				emitErr = e.NotBool()
			case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv:
				if k == m6Int {
					switch ins.op {
					case code.OpAdd:
						emitErr = e.Add()
					case code.OpSub:
						emitErr = e.Sub()
					case code.OpMul:
						emitErr = e.Mul()
					}
				} else {
					emitErr = e.FloatBinary(uint8(k), uint8(ins.op))
				}
			case code.OpNeg:
				if k == m6Int {
					emitErr = e.Neg()
				} else {
					emitErr = e.FloatNeg(uint8(k))
				}
			case code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
				leftKind := st[len(st)-2]
				if leftKind == m6Float32 || leftKind == m6Float64 {
					emitErr = e.FloatCompare(uint8(leftKind), uint8(ins.op))
				} else {
					cond := uint32(0)
					switch ins.op {
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
				}
			case code.OpIntToFloat, code.OpIntToFloat32, code.OpFloatToInt,
				code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32:
				emitErr = e.FloatConvert(uint8(ins.op))
			case code.OpJump:
				emitErr = e.Jump(ins.operands[0])
			case code.OpJumpIfFalse:
				emitErr = e.JumpIfFalse(ins.operands[0])
			case code.OpChant:
				if k == m6Float32 || k == m6Float64 {
					emitErr = e.ChantFloat(uint8(k))
				} else {
					emitErr = e.Chant(k == m6Bool)
				}
			case code.OpPop:
				if k == m6Float32 || k == m6Float64 {
					emitErr = e.PopFloat(uint8(k))
				} else {
					emitErr = e.Pop(k == m6Bool)
				}
			case code.OpCall:
				callee := p.Functions[ins.operands[0]]
				emitErr = e.CallNative(ins.operands[0], callee.ParameterCount, callee.ReturnsValue)
			case code.OpReturn:
				emitErr = e.ReturnUnit()
			case code.OpReturnValue:
				emitErr = e.ReturnValue()
			default:
				return nil, fmt.Errorf("AOT M6 unsupported opcode %s at %s offset %d", ins.name, b.name, ins.offset)
			}
			if emitErr != nil {
				return nil, fmt.Errorf("AOT M6 %s at offset %d: %w", b.name, ins.offset, emitErr)
			}
		}
		source := p.Instructions
		if b.fn != nil {
			source = b.fn.Instructions
		}
		mapping[len(source)] = e.CodeOffset()
		if err := e.PatchControlFlow(mapping); err != nil {
			return nil, err
		}
		if err := e.SetOperandDepth(0); err != nil {
			return nil, err
		}
		if b.fn != nil {
			if err := e.FinishFunction(); err != nil {
				return nil, err
			}
		}
		all[bi] = e
	}
	return all[0].FinishWithFunctions(all[1:])
}
