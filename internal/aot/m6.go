package aot

import (
	"fmt"
	"slices"

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
	m6String
	m6Uint8
	m6Array
	m6Struct
	m6Function
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
	case m6String:
		return "string"
	case m6Uint8:
		return "uint8"
	case m6Array:
		return "array"
	case m6Struct:
		return "struct"
	case m6Function:
		return "function"
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
	case value.KindString:
		return m6String
	case value.KindInt8, value.KindInt16, value.KindInt32,
		value.KindUint16, value.KindUint32:
		return m6Int
	case value.KindUint8:
		return m6Uint8
	default:
		return m6Unknown
	}
}

func needsM6(program *compiler.Bytecode) bool {
	for _, fn := range program.Functions {
		if len(fn.Captures) != 0 {
			return true
		}
	}
	for _, c := range program.Constants {
		if c.Kind() == value.KindFloat32 || c.Kind() == value.KindFloat || c.Kind() == value.KindString ||
			c.Kind() == value.KindUint8 || c.Kind() == value.KindInt8 || c.Kind() == value.KindInt16 ||
			c.Kind() == value.KindInt32 || c.Kind() == value.KindUint16 || c.Kind() == value.KindUint32 {
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
				code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32,
				code.OpArray, code.OpArrayTyped, code.OpIndex, code.OpLen, code.OpSetIndex,
				code.OpArrayMake, code.OpArrayWithCapacity, code.OpArrayPush,
				code.OpArrayCapacity, code.OpArrayReserve, code.OpArrayClone,
				code.OpArrayTruncate, code.OpArrayAppendAll,
				code.OpStruct, code.OpGetField,
				code.OpFunction, code.OpClosure, code.OpCallValue,
				code.OpGetUpvalue, code.OpSetUpvalue, code.OpDefineLocal:
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

func usesM12Objects(program *compiler.Bytecode) bool {
	for _, fn := range program.Functions {
		if len(fn.Captures) != 0 {
			return true
		}
	}
	blocks := []code.Instructions{program.Instructions}
	for _, fn := range program.Functions {
		blocks = append(blocks, fn.Instructions)
	}
	for _, src := range blocks {
		for offset := 0; offset < len(src); {
			op := code.Opcode(src[offset])
			switch op {
			case code.OpArray, code.OpArrayTyped, code.OpIndex, code.OpSetIndex,
				code.OpArrayMake, code.OpArrayWithCapacity, code.OpArrayPush,
				code.OpArrayCapacity, code.OpArrayReserve, code.OpArrayClone,
				code.OpArrayTruncate, code.OpArrayAppendAll,
				code.OpStruct, code.OpGetField,
				code.OpFunction, code.OpClosure, code.OpCallValue,
				code.OpGetUpvalue, code.OpSetUpvalue:
				return true
			}
			definition, err := code.Lookup(op)
			if err != nil {
				return false
			}
			_, width, err := code.ReadOperands(definition, src[offset+1:])
			if err != nil {
				return false
			}
			offset += 1 + width
		}
	}
	return false
}

func usesM15Closures(program *compiler.Bytecode) bool {
	for _, fn := range program.Functions {
		if len(fn.Captures) != 0 {
			return true
		}
	}
	return false
}

// m6Union 把函数参数、返回值、局部槽位和操作数类型约束在一起。
// 函数签名目前没有携带参数的静态类型，这里通过全部调用点与指令约束求解。
type m6Union struct {
	parent      []int
	kind        []m6Kind
	structArity []int
	fields      []map[int]int
	element     []int
	functions   []map[int]struct{}
}

func (u *m6Union) newKind(k m6Kind) int {
	i := len(u.parent)
	u.parent = append(u.parent, i)
	u.kind = append(u.kind, k)
	u.structArity = append(u.structArity, -1)
	u.fields = append(u.fields, nil)
	u.element = append(u.element, -1)
	u.functions = append(u.functions, nil)
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
	if u.structArity[a] >= 0 && u.structArity[b] >= 0 && u.structArity[a] != u.structArity[b] {
		return fmt.Errorf("struct field count mismatch: %d and %d", u.structArity[a], u.structArity[b])
	}
	u.parent[b] = a
	if u.kind[a] == m6Unknown {
		u.kind[a] = u.kind[b]
	}
	if u.structArity[a] < 0 {
		u.structArity[a] = u.structArity[b]
	}
	if u.fields[a] == nil && u.fields[b] != nil {
		u.fields[a] = make(map[int]int, len(u.fields[b]))
		for index, node := range u.fields[b] {
			u.fields[a][index] = node
		}
	} else {
		for index, node := range u.fields[b] {
			if current, ok := u.fields[a][index]; ok {
				if err := u.join(current, node); err != nil {
					return err
				}
			} else {
				u.fields[a][index] = node
			}
		}
	}
	if u.element[a] < 0 {
		u.element[a] = u.element[b]
	} else if u.element[b] >= 0 {
		if err := u.join(u.element[a], u.element[b]); err != nil {
			return err
		}
	}
	if len(u.functions[b]) != 0 {
		if u.functions[a] == nil {
			u.functions[a] = make(map[int]struct{}, len(u.functions[b]))
		}
		for id := range u.functions[b] {
			u.functions[a][id] = struct{}{}
		}
	}
	return nil
}
func (u *m6Union) require(a int, k m6Kind) error {
	return u.join(a, u.newKind(k))
}

func (u *m6Union) newStruct(fields []int) int {
	node := u.newKind(m6Struct)
	u.structArity[node] = len(fields)
	u.fields[node] = make(map[int]int, len(fields))
	for index, field := range fields {
		u.fields[node][index] = field
	}
	return node
}

func (u *m6Union) newFunction(id int) int {
	node := u.newKind(m6Function)
	u.functions[node] = map[int]struct{}{id: {}}
	return node
}

func (u *m6Union) functionTargets(node int) []int {
	root := u.root(node)
	result := make([]int, 0, len(u.functions[root]))
	for id := range u.functions[root] {
		result = append(result, id)
	}
	slices.Sort(result)
	return result
}

func (u *m6Union) structField(node, index int) (int, error) {
	root := u.root(node)
	if err := u.require(root, m6Struct); err != nil {
		return 0, err
	}
	root = u.root(root)
	if u.structArity[root] < 0 || index < 0 || index >= u.structArity[root] {
		return 0, fmt.Errorf("struct field %d is outside known layout", index)
	}
	field, ok := u.fields[root][index]
	if !ok {
		field = u.newKind(m6Unknown)
		u.fields[root][index] = field
	}
	return field, nil
}

func (u *m6Union) newArray(elements []int, kind m6Kind) (int, error) {
	node := u.newKind(m6Array)
	element := u.newKind(kind)
	u.element[node] = element
	for _, item := range elements {
		if err := u.join(element, item); err != nil {
			return 0, err
		}
	}
	return node, nil
}

func (u *m6Union) arrayElement(node int) (int, error) {
	root := u.root(node)
	if err := u.require(root, m6Array); err != nil {
		return 0, err
	}
	root = u.root(root)
	if u.element[root] < 0 {
		u.element[root] = u.newKind(m6Unknown)
	}
	return u.element[root], nil
}

func (u *m6Union) structFieldCount(node int) int {
	return u.structArity[u.root(node)]
}

func (u *m6Union) arrayElementKind(node int) m6Kind {
	root := u.root(node)
	if u.element[root] < 0 {
		return m6Unknown
	}
	return u.get(u.element[root])
}

func m12ReferenceKind(kind m6Kind) bool {
	return kind == m6Array || kind == m6Struct || kind == m6Function
}

func m12ElementBytes(kind m6Kind) int {
	if kind == m6Uint8 {
		return 1
	}
	return 8
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
	u        *m6Union
	blocks   []*m6Block
	globals  []int
	locals   [][]int
	returns  []int
	captures [][]int
}

type m6Liveness struct {
	inLocals, outLocals   [][]bool
	inGlobals, outGlobals [][]bool
}

func analyzeM6Liveness(p *compiler.Bytecode, b *m6Block) *m6Liveness {
	n := len(b.inst)
	localCount := 0
	if b.fn != nil {
		localCount = b.fn.LocalCount
	}
	l := &m6Liveness{
		inLocals: make([][]bool, n), outLocals: make([][]bool, n),
		inGlobals: make([][]bool, n), outGlobals: make([][]bool, n),
	}
	for i := 0; i < n; i++ {
		l.inLocals[i], l.outLocals[i] = make([]bool, localCount), make([]bool, localCount)
		l.inGlobals[i], l.outGlobals[i] = make([]bool, p.GlobalsCount), make([]bool, p.GlobalsCount)
	}
	changed := true
	for changed {
		changed = false
		for i := n - 1; i >= 0; i-- {
			if b.reach[i] == nil {
				continue
			}
			ins := b.inst[i]
			succ := []int{}
			if ins.op != code.OpReturn && ins.op != code.OpReturnValue {
				succ = append(succ, b.boundaries[ins.next])
				if ins.op == code.OpJump {
					succ = []int{b.boundaries[ins.operands[0]]}
				} else if ins.op == code.OpJumpIfFalse {
					succ = append(succ, b.boundaries[ins.operands[0]])
				}
			}
			newOutL, newOutG := make([]bool, localCount), make([]bool, p.GlobalsCount)
			for _, s := range succ {
				if s >= n {
					continue
				}
				for j := range newOutL {
					newOutL[j] = newOutL[j] || l.inLocals[s][j]
				}
				for j := range newOutG {
					newOutG[j] = newOutG[j] || l.inGlobals[s][j]
				}
			}
			newInL, newInG := append([]bool(nil), newOutL...), append([]bool(nil), newOutG...)
			switch ins.op {
			case code.OpSetLocal, code.OpDefineLocal:
				newInL[ins.operands[0]] = false
			case code.OpGetLocal:
				newInL[ins.operands[0]] = true
			case code.OpClosure:
				for _, capture := range p.Functions[ins.operands[0]].Captures {
					if capture.Local {
						newInL[capture.Index] = true
					}
				}
			case code.OpSetGlobal:
				newInG[ins.operands[0]] = false
			case code.OpGetGlobal:
				newInG[ins.operands[0]] = true
			}
			if !slices.Equal(newOutL, l.outLocals[i]) || !slices.Equal(newOutG, l.outGlobals[i]) ||
				!slices.Equal(newInL, l.inLocals[i]) || !slices.Equal(newInG, l.inGlobals[i]) {
				l.outLocals[i], l.outGlobals[i], l.inLocals[i], l.inGlobals[i] = newOutL, newOutG, newInL, newInG
				changed = true
			}
		}
	}
	return l
}

func m6CallValueReturns(p *compiler.Bytecode, arguments int) (bool, error) {
	found := false
	returns := false
	for _, fn := range p.Functions {
		if fn.ParameterCount != arguments {
			continue
		}
		if !found {
			found, returns = true, fn.ReturnsValue
			continue
		}
		if returns != fn.ReturnsValue {
			return false, fmt.Errorf("function values with %d parameters have ambiguous return shape", arguments)
		}
	}
	if !found {
		return false, fmt.Errorf("no function accepts %d arguments", arguments)
	}
	return returns, nil
}

type m15TargetSet map[int]struct{}

func m15MergeTargets(dst *m15TargetSet, src m15TargetSet) bool {
	if len(src) == 0 {
		return false
	}
	if *dst == nil {
		*dst = make(m15TargetSet)
	}
	changed := false
	for id := range src {
		if _, exists := (*dst)[id]; !exists {
			(*dst)[id] = struct{}{}
			changed = true
		}
	}
	return changed
}

// inferM15CallReturns resolves the common straight-line forms produced for
// function literals (direct values, locals/globals, and static factory
// returns). The full union analysis below still validates every target; these
// hints only disambiguate CallValue's stack effect when equal-arity unit and
// value-returning functions coexist in one Mochi image.
func inferM15CallReturns(p *compiler.Bytecode, blocks []*m6Block) []map[int]bool {
	hints := make([]map[int]bool, len(blocks))
	returns := make([]m15TargetSet, len(p.Functions))
	for i := range hints {
		hints[i] = make(map[int]bool)
	}
	for pass := 0; pass <= len(p.Functions)+1; pass++ {
		changed := false
		for bi, b := range blocks {
			branchless := true
			for _, ins := range b.inst {
				if ins.op == code.OpJump || ins.op == code.OpJumpIfFalse {
					branchless = false
					break
				}
			}
			if !branchless {
				continue
			}
			locals := make([]m15TargetSet, 0)
			if b.fn != nil {
				locals = make([]m15TargetSet, b.fn.LocalCount)
			}
			globals := make([]m15TargetSet, p.GlobalsCount)
			stack := make([]m15TargetSet, 0)
			pop := func() m15TargetSet {
				if len(stack) == 0 {
					return nil
				}
				v := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				return v
			}
			pushUnknown := func() { stack = append(stack, nil) }
			for _, ins := range b.inst {
				switch ins.op {
				case code.OpFunction, code.OpClosure:
					stack = append(stack, m15TargetSet{ins.operands[0]: {}})
				case code.OpConstant, code.OpTrue, code.OpFalse, code.OpNil:
					pushUnknown()
				case code.OpGetLocal:
					stack = append(stack, locals[ins.operands[0]])
				case code.OpSetLocal, code.OpDefineLocal:
					locals[ins.operands[0]] = pop()
				case code.OpGetGlobal:
					stack = append(stack, globals[ins.operands[0]])
				case code.OpSetGlobal:
					globals[ins.operands[0]] = pop()
				case code.OpGetUpvalue:
					pushUnknown()
				case code.OpSetUpvalue, code.OpChant, code.OpPop:
					pop()
				case code.OpNeg, code.OpNot, code.OpIntToFloat, code.OpFloatToInt,
					code.OpIntToFloat32, code.OpFloat32ToInt, code.OpFloat32ToFloat64,
					code.OpFloat64ToFloat32, code.OpLen, code.OpGetField,
					code.OpArrayCapacity, code.OpArrayClone:
					pop()
					pushUnknown()
				case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual,
					code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater,
					code.OpGreaterEqual, code.OpIndex, code.OpArrayMake:
					pop()
					pop()
					pushUnknown()
				case code.OpSetIndex:
					pop()
					pop()
					pop()
				case code.OpArray, code.OpArrayTyped, code.OpStruct:
					for range ins.operands[0] {
						pop()
					}
					pushUnknown()
				case code.OpArrayWithCapacity:
					pop()
					pushUnknown()
				case code.OpArrayPush, code.OpArrayReserve, code.OpArrayTruncate, code.OpArrayAppendAll:
					pop()
					pop()
				case code.OpCall:
					target := ins.operands[0]
					for range p.Functions[target].ParameterCount {
						pop()
					}
					if p.Functions[target].ReturnsValue {
						stack = append(stack, returns[target])
					}
				case code.OpCallValue:
					arguments := ins.operands[0]
					for range arguments {
						pop()
					}
					targets := pop()
					known, result, first := len(targets) != 0, false, true
					var resultTargets m15TargetSet
					for target := range targets {
						fn := p.Functions[target]
						if fn.ParameterCount != arguments {
							known = false
							break
						}
						if first {
							result = fn.ReturnsValue
							first = false
						} else if result != fn.ReturnsValue {
							known = false
							break
						}
						m15MergeTargets(&resultTargets, returns[target])
					}
					if !known {
						stack = nil
						break
					}
					hints[bi][ins.offset] = result
					if result {
						stack = append(stack, resultTargets)
					}
				case code.OpReturnValue:
					if b.fn != nil {
						changed = m15MergeTargets(&returns[b.fn.ID], pop()) || changed
					}
				case code.OpReturn:
					stack = nil
				}
			}
		}
		if !changed {
			break
		}
	}
	return hints
}

// m6Reachability 先独立验证栈深度、局部初始化与每条跳转的边界。
// 这样类型约束求解不需要将递归函数在编译期展开。
func m6Reachability(p *compiler.Bytecode, b *m6Block, callReturns map[int]bool) error {
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
		case code.OpConstant, code.OpTrue, code.OpFalse,
			code.OpGetGlobal, code.OpGetLocal, code.OpGetUpvalue,
			code.OpFunction, code.OpClosure:
			push = 1
		case code.OpNil:
			if !usesM15Closures(p) {
				return fail("unsupported operation OpNil")
			}
			push = 1
		case code.OpSetGlobal, code.OpSetLocal, code.OpDefineLocal, code.OpSetUpvalue,
			code.OpJumpIfFalse, code.OpChant, code.OpPop:
			need = 1
		case code.OpNeg, code.OpNot, code.OpIntToFloat, code.OpFloatToInt,
			code.OpIntToFloat32, code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32:
			need, push = 1, 1
		case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual, code.OpNotEqual,
			code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			need, push = 2, 1
		case code.OpArray, code.OpArrayTyped, code.OpStruct:
			need, push = ins.operands[0], 1
		case code.OpIndex:
			need, push = 2, 1
		case code.OpSetIndex:
			need = 3
		case code.OpArrayMake:
			need, push = 2, 1
		case code.OpArrayWithCapacity, code.OpArrayCapacity, code.OpArrayClone:
			need, push = 1, 1
		case code.OpArrayPush, code.OpArrayReserve, code.OpArrayTruncate, code.OpArrayAppendAll:
			need = 2
		case code.OpLen, code.OpGetField:
			need, push = 1, 1
		case code.OpCall:
			id := ins.operands[0]
			if id < 0 || id >= len(p.Functions) {
				return fail("function index out of range")
			}
			need = p.Functions[id].ParameterCount
			if p.Functions[id].ReturnsValue {
				push = 1
			}
		case code.OpCallValue:
			need = ins.operands[0] + 1
			returns, known := callReturns[ins.offset]
			if !known {
				var err error
				returns, err = m6CallValueReturns(p, ins.operands[0])
				if err != nil {
					return fail(err.Error())
				}
			}
			if returns {
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
		case code.OpGetUpvalue, code.OpSetUpvalue:
			if b.fn == nil || ins.operands[0] < 0 || ins.operands[0] >= len(b.fn.Captures) {
				return fail("upvalue slot out of range")
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
	closuresEnabled := usesM15Closures(p)
	type pendingCallValue struct {
		block, offset int
		function      int
		arguments     []int
		result        int
		returns       bool
	}
	pendingCalls := make([]pendingCallValue, 0)
	for range p.GlobalsCount {
		a.globals = append(a.globals, a.u.newKind(m6Unknown))
	}
	for i, fn := range p.Functions {
		if fn.ID != i {
			return nil, fmt.Errorf("AOT M6 function id mismatch: index=%d id=%d", i, fn.ID)
		}
		if fn.ParameterCount < 0 || fn.ParameterCount > darwinarm64.MaxM15Parameters {
			return nil, fmt.Errorf("AOT M6 function %q parameter count %d exceeds native ABI limit %d", fn.Name, fn.ParameterCount, darwinarm64.MaxM15Parameters)
		}
		if fn.LocalCount < fn.ParameterCount || fn.LocalCount > darwinarm64.MaxM4Locals {
			return nil, fmt.Errorf("AOT M6 function %q local count %d is invalid for %d parameters (limit %d)", fn.Name, fn.LocalCount, fn.ParameterCount, darwinarm64.MaxM4Locals)
		}
		locals := make([]int, fn.LocalCount)
		for j := range locals {
			locals[j] = a.u.newKind(m6Unknown)
		}
		a.locals = append(a.locals, locals)
		captures := make([]int, len(fn.Captures))
		for j := range captures {
			captures[j] = a.u.newKind(m6Unknown)
		}
		a.captures = append(a.captures, captures)
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
	}
	callReturnHints := inferM15CallReturns(p, blocks)
	for bi, b := range blocks {
		if err := m6Reachability(p, b, callReturnHints[bi]); err != nil {
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
			pushUnknown := func() { stack = append(stack, a.u.newKind(m6Unknown)) }
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
			case code.OpNil:
				if !closuresEnabled {
					return nil, fail(fmt.Errorf("unsupported operation OpNil"))
				}
				pushUnknown()
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
			case code.OpGetUpvalue:
				stack = append(stack, a.captures[bi-1][ins.operands[0]])
			case code.OpSetUpvalue:
				if err := join(pop(), a.captures[bi-1][ins.operands[0]]); err != nil {
					return nil, fail(err)
				}
			case code.OpFunction:
				stack = append(stack, a.u.newFunction(ins.operands[0]))
			case code.OpClosure:
				target := ins.operands[0]
				if b.fn == nil {
					if len(p.Functions[target].Captures) != 0 {
						return nil, fail(fmt.Errorf("capturing closure outside function"))
					}
				} else {
					for captureIndex, capture := range p.Functions[target].Captures {
						outer := -1
						if capture.Local {
							outer = a.locals[bi-1][capture.Index]
						} else {
							outer = a.captures[bi-1][capture.Index]
						}
						if err := join(a.captures[target][captureIndex], outer); err != nil {
							return nil, fail(err)
						}
					}
				}
				stack = append(stack, a.u.newFunction(target))
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
			case code.OpStruct:
				count := ins.operands[0]
				fields := append([]int(nil), stack[len(stack)-count:]...)
				stack = stack[:len(stack)-count]
				stack = append(stack, a.u.newStruct(fields))
			case code.OpGetField:
				field, err := a.u.structField(pop(), ins.operands[0])
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, field)
			case code.OpArray, code.OpArrayTyped:
				count := ins.operands[0]
				items := append([]int(nil), stack[len(stack)-count:]...)
				stack = stack[:len(stack)-count]
				elementKind := m6Unknown
				if ins.op == code.OpArrayTyped {
					elementKind = m12DescriptorKind(ins.operands[1])
					if elementKind == m6Unknown && ins.operands[1]&0x7f != int(value.KindInvalid) {
						return nil, fail(fmt.Errorf("unsupported array element descriptor %d", ins.operands[1]))
					}
				}
				array, err := a.u.newArray(items, elementKind)
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, array)
			case code.OpArrayMake:
				initial, length := pop(), pop()
				if err := require(length, m6Int); err != nil {
					return nil, fail(err)
				}
				elementKind, err := m12DynamicDescriptorKind(ins.operands[0])
				if err != nil {
					return nil, fail(err)
				}
				array, err := a.u.newArray([]int{initial}, elementKind)
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, array)
			case code.OpArrayWithCapacity:
				if err := require(pop(), m6Int); err != nil {
					return nil, fail(err)
				}
				elementKind, err := m12DynamicDescriptorKind(ins.operands[0])
				if err != nil {
					return nil, fail(err)
				}
				array, err := a.u.newArray(nil, elementKind)
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, array)
			case code.OpArrayPush:
				element, array := pop(), pop()
				arrayElement, err := a.u.arrayElement(array)
				if err != nil {
					return nil, fail(err)
				}
				if err := join(element, arrayElement); err != nil {
					return nil, fail(err)
				}
			case code.OpArrayCapacity:
				if _, err := a.u.arrayElement(pop()); err != nil {
					return nil, fail(err)
				}
				push(m6Int)
			case code.OpArrayReserve:
				additional, array := pop(), pop()
				if err := require(additional, m6Int); err != nil {
					return nil, fail(err)
				}
				if _, err := a.u.arrayElement(array); err != nil {
					return nil, fail(err)
				}
			case code.OpArrayClone:
				array := pop()
				if _, err := a.u.arrayElement(array); err != nil {
					return nil, fail(err)
				}
				stack = append(stack, array)
			case code.OpArrayTruncate:
				length, array := pop(), pop()
				if err := require(length, m6Int); err != nil {
					return nil, fail(err)
				}
				if _, err := a.u.arrayElement(array); err != nil {
					return nil, fail(err)
				}
			case code.OpArrayAppendAll:
				source, destination := pop(), pop()
				sourceElement, err := a.u.arrayElement(source)
				if err != nil {
					return nil, fail(err)
				}
				destinationElement, err := a.u.arrayElement(destination)
				if err != nil {
					return nil, fail(err)
				}
				if err := join(destinationElement, sourceElement); err != nil {
					return nil, fail(err)
				}
			case code.OpIndex:
				index, array := pop(), pop()
				if err := require(index, m6Int); err != nil {
					return nil, fail(err)
				}
				element, err := a.u.arrayElement(array)
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, element)
			case code.OpSetIndex:
				element, index, array := pop(), pop(), pop()
				if err := require(index, m6Int); err != nil {
					return nil, fail(err)
				}
				arrayElement, err := a.u.arrayElement(array)
				if err != nil {
					return nil, fail(err)
				}
				if err := join(element, arrayElement); err != nil {
					return nil, fail(err)
				}
			case code.OpLen:
				input := pop()
				kind := a.u.get(input)
				if kind != m6String && kind != m6Array {
					return nil, fail(fmt.Errorf("OpLen requires string or array, got %s", kind))
				}
				push(m6Int)
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
			case code.OpCallValue:
				argumentCount := ins.operands[0]
				functionPosition := len(stack) - argumentCount - 1
				functionNode := stack[functionPosition]
				if err := require(functionNode, m6Function); err != nil {
					return nil, fail(err)
				}
				arguments := append([]int(nil), stack[functionPosition+1:]...)
				stack = stack[:functionPosition]
				returns, known := callReturnHints[bi][ins.offset]
				if !known {
					var err error
					returns, err = m6CallValueReturns(p, argumentCount)
					if err != nil {
						return nil, fail(err)
					}
				}
				result := -1
				if returns {
					result = a.u.newKind(m6Unknown)
					stack = append(stack, result)
				}
				pendingCalls = append(pendingCalls, pendingCallValue{
					block: bi, offset: ins.offset, function: functionNode,
					arguments: arguments, result: result, returns: returns,
				})
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
	for pass := 0; pass <= len(p.Functions); pass++ {
		for _, call := range pendingCalls {
			for _, target := range a.u.functionTargets(call.function) {
				callee := p.Functions[target]
				if callee.ParameterCount != len(call.arguments) || callee.ReturnsValue != call.returns {
					return nil, fmt.Errorf("AOT M6 block %d offset %d has incompatible function value target %d", call.block, call.offset, target)
				}
				for j := range call.arguments {
					if err := a.u.join(call.arguments[j], a.locals[target][j]); err != nil {
						return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", call.block, call.offset, err)
					}
				}
				if call.returns {
					if err := a.u.join(call.result, a.returns[target]); err != nil {
						return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", call.block, call.offset, err)
					}
				}
			}
		}
	}
	for _, call := range pendingCalls {
		if len(a.u.functionTargets(call.function)) == 0 {
			return nil, fmt.Errorf("AOT M6 block %d offset %d function value target is unknown", call.block, call.offset)
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
				if k == m6Bool || k == m6String || k == m6Array || k == m6Struct || k == m6Uint8 || ins.op == code.OpDiv && k == m6Int {
					return nil, fmt.Errorf("AOT M6 unsupported arithmetic type %s for %s at %s offset %d", k, ins.name, b.name, ins.offset)
				}
			}
		}
	}
	return a, nil
}

func m12DescriptorKind(descriptor int) m6Kind {
	switch value.Kind(descriptor & 0x7f) {
	case value.KindInt, value.KindInt8, value.KindInt16, value.KindInt32,
		value.KindUint16, value.KindUint32:
		return m6Int
	case value.KindUint8:
		return m6Uint8
	case value.KindBool:
		return m6Bool
	case value.KindFloat32:
		return m6Float32
	case value.KindFloat:
		return m6Float64
	case value.KindString:
		return m6String
	case value.KindArray:
		return m6Array
	case value.KindStruct:
		return m6Struct
	default:
		return m6Unknown
	}
}

func m12DynamicDescriptorKind(descriptor int) (m6Kind, error) {
	if descriptor&0x80 != 0 {
		return m6Unknown, fmt.Errorf("nullable array descriptors require M13")
	}
	kind := m12DescriptorKind(descriptor)
	if kind == m6Unknown && descriptor&0x7f != int(value.KindInvalid) {
		return m6Unknown, fmt.Errorf("unsupported array element descriptor %d", descriptor)
	}
	return kind, nil
}

func m15CapturedLocals(p *compiler.Bytecode, b *m6Block) []int {
	if b.fn == nil {
		return nil
	}
	seen := make(map[int]bool)
	result := make([]int, 0)
	for _, ins := range b.inst {
		if ins.op != code.OpClosure {
			continue
		}
		for _, capture := range p.Functions[ins.operands[0]].Captures {
			if capture.Local && !seen[capture.Index] {
				seen[capture.Index] = true
				result = append(result, capture.Index)
			}
		}
	}
	slices.Sort(result)
	return result
}

func compileM6(p *compiler.Bytecode) ([]byte, error) {
	return compileM6WithOptions(p, false, false)
}

func compileM6WithM11(p *compiler.Bytecode, enableM11 bool) ([]byte, error) {
	return compileM6WithOptions(p, enableM11, false)
}

func compileM6WithOptions(p *compiler.Bytecode, enableM11, gcStress bool) ([]byte, error) {
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
	m12Objects := usesM12Objects(p)
	// 静态字符串按 UTF-8 原始字节去重，保证相同内容共用一个只读 ID。
	stringIDs := make(map[int]int)
	stringTable := make([]string, 0)
	seen := make(map[string]int)
	for i, constant := range p.Constants {
		if constant.Kind() != value.KindString {
			continue
		}
		text, _ := constant.Text()
		id, exists := seen[text]
		if !exists {
			id = len(stringTable)
			seen[text] = id
			stringTable = append(stringTable, text)
		}
		stringIDs[i] = id
	}
	all := make([]*darwinarm64.StackEmitter, len(a.blocks))
	for bi, b := range a.blocks {
		live := analyzeM6Liveness(p, b)
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
		var e *darwinarm64.StackEmitter
		if m12Objects {
			capturedLocals := m15CapturedLocals(p, b)
			upvalues := 0
			if b.fn != nil {
				upvalues = len(b.fn.Captures)
			}
			if len(capturedLocals) != 0 || upvalues != 0 {
				e, err = darwinarm64.NewStackEmitterWithClosures(p.GlobalsCount, locals, params, maximum, true, gcStress, capturedLocals, upvalues)
			} else {
				e, err = darwinarm64.NewStackEmitterWithGC(p.GlobalsCount, locals, params, maximum, b.fn != nil, gcStress)
			}
		} else {
			e, err = darwinarm64.NewStackEmitter(p.GlobalsCount, locals, params, maximum, b.fn != nil)
		}
		if err != nil {
			return nil, err
		}
		if m12Objects {
			if err := e.EnableM12Runtime(bi == 0); err != nil {
				return nil, err
			}
		}
		if bi == 0 && enableM11 && !m12Objects {
			if err := e.EnableM11Runtime(); err != nil {
				return nil, err
			}
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
			setRoots := func(stackKinds []m6Kind, localsLive, globalsLive []bool) error {
				frameSlots := make([]int, 0)
				if b.fn != nil {
					for slot, yes := range localsLive {
						if yes && m12ReferenceKind(a.u.get(a.locals[bi-1][slot])) {
							frameSlots = append(frameSlots, slot)
						}
					}
				}
				for slot, kind := range stackKinds {
					if m12ReferenceKind(kind) {
						frameSlots = append(frameSlots, locals+slot)
					}
				}
				globalSlots := make([]int, 0)
				for slot, yes := range globalsLive {
					if yes && m12ReferenceKind(a.u.get(a.globals[slot])) {
						globalSlots = append(globalSlots, slot)
					}
				}
				return e.SetGCRoots(frameSlots, globalSlots)
			}
			if m12Objects {
				switch ins.op {
				case code.OpStruct, code.OpArray, code.OpArrayTyped, code.OpArrayMake,
					code.OpArrayWithCapacity, code.OpArrayPush, code.OpArrayReserve,
					code.OpArrayClone, code.OpArrayAppendAll, code.OpFunction, code.OpClosure:
					emitErr = setRoots(st, live.inLocals[i], live.inGlobals[i])
				case code.OpCall:
					params := p.Functions[ins.operands[0]].ParameterCount
					emitErr = setRoots(st[:len(st)-params], live.outLocals[i], live.outGlobals[i])
				case code.OpCallValue:
					params := ins.operands[0]
					emitErr = setRoots(st[:len(st)-params-1], live.outLocals[i], live.outGlobals[i])
				}
				if emitErr != nil {
					return nil, fmt.Errorf("AOT M11b %s at offset %d: %w", b.name, ins.offset, emitErr)
				}
			}
			switch ins.op {
			case code.OpConstant:
				v := p.Constants[ins.operands[0]]
				switch m6ValueKind(v) {
				case m6Int, m6Uint8:
					n, _ := v.IntegerBits()
					emitErr = e.PushInt64(int64(n))
				case m6Float64:
					n, _ := v.Float64()
					emitErr = e.PushFloat64(n)
				case m6Float32:
					n, _ := v.Float32()
					emitErr = e.PushFloat32(n)
				case m6Bool:
					n, _ := v.Bool()
					emitErr = e.PushBool(n)
				case m6String:
					emitErr = e.PushInt64(int64(stringIDs[ins.operands[0]]))
				}
			case code.OpTrue, code.OpFalse:
				emitErr = e.PushBool(ins.op == code.OpTrue)
			case code.OpNil:
				emitErr = e.PushInt64(0)
			case code.OpSetGlobal:
				emitErr = e.SetGlobal(ins.operands[0])
			case code.OpGetGlobal:
				emitErr = e.GetGlobal(ins.operands[0])
			case code.OpSetLocal:
				emitErr = e.SetLocal(ins.operands[0])
			case code.OpDefineLocal:
				emitErr = e.DefineLocal(ins.operands[0])
			case code.OpGetLocal:
				emitErr = e.GetLocal(ins.operands[0])
			case code.OpGetUpvalue:
				emitErr = e.GetUpvalue(ins.operands[0])
			case code.OpSetUpvalue:
				emitErr = e.SetUpvalue(ins.operands[0])
			case code.OpFunction, code.OpClosure:
				target := ins.operands[0]
				captures := make([]darwinarm64.ClosureCapture, len(p.Functions[target].Captures))
				for captureIndex, capture := range p.Functions[target].Captures {
					captures[captureIndex] = darwinarm64.ClosureCapture{
						Local: capture.Local, Index: capture.Index,
						Reference: m12ReferenceKind(a.u.get(a.captures[target][captureIndex])),
					}
				}
				emitErr = e.NewClosure(target, p.Functions[target].ParameterCount, p.Functions[target].ReturnsValue, captures)
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
				if k == m6Array || k == m6Struct {
					return nil, fmt.Errorf("AOT M12 cannot chant composite value at %s offset %d", b.name, ins.offset)
				}
				if k == m6String {
					emitErr = e.ChantString()
				} else if k == m6Float32 || k == m6Float64 {
					emitErr = e.ChantFloat(uint8(k))
				} else {
					emitErr = e.Chant(k == m6Bool)
				}
			case code.OpPop:
				if bi == 0 && (k == m6Array || k == m6Struct) {
					return nil, fmt.Errorf("AOT M12 cannot print composite expression result at %s offset %d", b.name, ins.offset)
				}
				if k == m6String {
					emitErr = e.PopString()
				} else if k == m6Float32 || k == m6Float64 {
					emitErr = e.PopFloat(uint8(k))
				} else {
					emitErr = e.Pop(k == m6Bool)
				}
			case code.OpCall:
				callee := p.Functions[ins.operands[0]]
				emitErr = e.CallNative(ins.operands[0], callee.ParameterCount, callee.ReturnsValue)
			case code.OpCallValue:
				position := len(b.entry[i]) - ins.operands[0] - 1
				targets := a.u.functionTargets(b.entry[i][position])
				if len(targets) == 0 {
					return nil, fmt.Errorf("AOT M15b unknown function value at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.CallClosure(ins.operands[0], p.Functions[targets[0]].ReturnsValue)
			case code.OpReturn:
				emitErr = e.ReturnUnit()
			case code.OpReturnValue:
				emitErr = e.ReturnValue()
			case code.OpStruct:
				count := ins.operands[0]
				bitmap := make([]uint64, (count+63)/64)
				for field, kind := range st[len(st)-count:] {
					if m12ReferenceKind(kind) {
						bitmap[field/64] |= uint64(1) << uint(field%64)
					}
				}
				emitErr = e.NewStructObject(count, bitmap)
			case code.OpGetField:
				node := b.entry[i][len(b.entry[i])-1]
				emitErr = e.GetStructField(ins.operands[0], a.u.structFieldCount(node))
			case code.OpArray, code.OpArrayTyped:
				count := ins.operands[0]
				elementKind := m6Unknown
				if count > 0 {
					elementKind = st[len(st)-count]
				} else if ins.op == code.OpArrayTyped {
					elementKind = m12DescriptorKind(ins.operands[1])
				}
				if elementKind == m6Unknown {
					elementKind = m6Int
				}
				emitErr = e.NewArrayObject(count, m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayMake:
				elementKind := m12DescriptorKind(ins.operands[0])
				if elementKind == m6Unknown {
					elementKind = st[len(st)-1]
				}
				emitErr = e.ArrayMake(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayWithCapacity:
				elementKind := m12DescriptorKind(ins.operands[0])
				if elementKind == m6Unknown {
					next := b.boundaries[ins.next]
					if next < len(b.entry) && len(b.entry[next]) != 0 {
						elementKind = a.u.arrayElementKind(b.entry[next][len(b.entry[next])-1])
					}
					if elementKind == m6Unknown {
						elementKind = m6Int
					}
				}
				emitErr = e.ArrayWithCapacity(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayPush:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayPush(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayCapacity:
				emitErr = e.ArrayCapacity()
			case code.OpArrayReserve:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayReserve(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayClone:
				arrayNode := b.entry[i][len(b.entry[i])-1]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayClone(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayTruncate:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayTruncate(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpArrayAppendAll:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayAppendAll(m12ElementBytes(elementKind), m12ReferenceKind(elementKind))
			case code.OpIndex:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayIndex(m12ElementBytes(elementKind))
			case code.OpSetIndex:
				arrayNode := b.entry[i][len(b.entry[i])-3]
				elementKind := a.u.arrayElementKind(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArraySetIndex(m12ElementBytes(elementKind))
			case code.OpLen:
				if k == m6String {
					emitErr = e.StringLen()
				} else {
					emitErr = e.ArrayLen()
				}
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
	return all[0].FinishWithFunctionsAndStrings(all[1:], stringTable)
}
