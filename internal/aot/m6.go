package aot

import (
	"fmt"
	"slices"
	"strings"

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
	m6Int8
	m6Int16
	m6Int32
	m6Uint8
	m6Uint16
	m6Uint32
	m6Array
	m6Struct
	m6Enum
	m6Function
	m6Resource
	m6FloatAny
	m6Nil
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
	case m6Int8:
		return "int8"
	case m6Int16:
		return "int16"
	case m6Int32:
		return "int32"
	case m6Uint8:
		return "uint8"
	case m6Uint16:
		return "uint16"
	case m6Uint32:
		return "uint32"
	case m6Array:
		return "array"
	case m6Struct:
		return "struct"
	case m6Enum:
		return "enum"
	case m6Function:
		return "function"
	case m6Resource:
		return "resource"
	case m6FloatAny:
		return "float"
	case m6Nil:
		return "nil"
	default:
		return "unknown"
	}
}

func m14NativeName(name string) (string, bool) {
	const prefix = "$mma::mio/fs/runtime::"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name, prefix)
	operation, _, ok := strings.Cut(rest, "::")
	return operation, ok
}

func m14NativeSignature(name string) ([]m6Kind, m6Kind, bool) {
	op, ok := m14NativeName(name)
	if !ok {
		return nil, m6Unknown, false
	}
	switch op {
	case "close", "dir_close", "file_status", "dir_status":
		return []m6Kind{m6Resource, m6Array}, m6String, true
	case "stat", "mkdir":
		return []m6Kind{m6String, m6Array}, m6String, true
	case "dir_is_dir", "dir_is_file", "dir_next":
		return []m6Kind{m6Resource}, m6Bool, true
	case "dir_name":
		return []m6Kind{m6Resource}, m6String, true
	case "open":
		return []m6Kind{m6String, m6Int}, m6Resource, true
	case "open_dir":
		return []m6Kind{m6String}, m6Resource, true
	case "read", "write":
		return []m6Kind{m6Resource, m6Array, m6Int, m6Int, m6Array}, m6String, true
	default:
		return nil, m6Unknown, false
	}
}

func m13PolymorphicFloatPredicate(name string) bool {
	return name == "kiso/math::is_inf"
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
	case value.KindInt8:
		return m6Int8
	case value.KindInt16:
		return m6Int16
	case value.KindInt32:
		return m6Int32
	case value.KindUint8:
		return m6Uint8
	case value.KindUint16:
		return m6Uint16
	case value.KindUint32:
		return m6Uint32
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
			case code.OpNil, code.OpIsNil, code.OpSetNullableIndex:
				return true
			case code.OpIntToFloat, code.OpFloatToInt, code.OpIntToFloat32,
				code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32,
				code.OpIntegerConvert, code.OpMath, code.OpDataConvert,
				code.OpArray, code.OpArrayTyped, code.OpIndex, code.OpLen, code.OpSetIndex,
				code.OpArrayMake, code.OpArrayWithCapacity, code.OpArrayPush,
				code.OpArrayCapacity, code.OpArrayReserve, code.OpArrayClone,
				code.OpArrayTruncate, code.OpArrayAppendAll,
				code.OpStruct, code.OpGetField,
				code.OpEnum, code.OpEnumTag, code.OpGetEnumValue,
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
			case code.OpNil, code.OpIsNil, code.OpSetNullableIndex,
				code.OpDataConvert,
				code.OpArray, code.OpArrayTyped, code.OpIndex, code.OpSetIndex,
				code.OpArrayMake, code.OpArrayWithCapacity, code.OpArrayPush,
				code.OpArrayCapacity, code.OpArrayReserve, code.OpArrayClone,
				code.OpArrayTruncate, code.OpArrayAppendAll,
				code.OpStruct, code.OpGetField,
				code.OpEnum, code.OpEnumTag, code.OpGetEnumValue,
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
	enumArities []map[int]int
	enumFields  []map[int]map[int]int
	element     []int
	functions   []map[int]struct{}
	nullable    []bool
}

func (u *m6Union) newKind(k m6Kind) int {
	i := len(u.parent)
	u.parent = append(u.parent, i)
	u.kind = append(u.kind, k)
	u.structArity = append(u.structArity, -1)
	u.fields = append(u.fields, nil)
	u.enumArities = append(u.enumArities, nil)
	u.enumFields = append(u.enumFields, nil)
	u.element = append(u.element, -1)
	u.functions = append(u.functions, nil)
	u.nullable = append(u.nullable, false)
	return i
}
func (u *m6Union) root(i int) int {
	if u.parent[i] != i {
		u.parent[i] = u.root(u.parent[i])
	}
	return u.parent[i]
}
func (u *m6Union) get(i int) m6Kind      { return u.kind[u.root(i)] }
func (u *m6Union) isNullable(i int) bool { return u.nullable[u.root(i)] }
func (u *m6Union) makeNullable(i int)    { u.nullable[u.root(i)] = true }
func (u *m6Union) join(a, b int) error {
	a, b = u.root(a), u.root(b)
	if a == b {
		return nil
	}
	if u.kind[a] == m6Nil && u.kind[b] == m6Unknown {
		// Control-flow entry nodes start unknown; they do not by themselves
		// provide a non-nil alternative.
	} else if u.kind[a] == m6Unknown && u.kind[b] == m6Nil {
		u.kind[a] = m6Nil
	} else if u.kind[a] == m6Nil && u.kind[b] != m6Nil {
		u.kind[a] = u.kind[b]
		u.nullable[a] = true
	} else if u.kind[b] == m6Nil && u.kind[a] != m6Nil {
		u.nullable[a] = true
	} else if u.kind[a] != m6Unknown && u.kind[b] != m6Unknown && u.kind[a] != u.kind[b] {
		if (u.kind[a] == m6FloatAny && (u.kind[b] == m6Float32 || u.kind[b] == m6Float64)) ||
			(u.kind[b] == m6FloatAny && (u.kind[a] == m6Float32 || u.kind[a] == m6Float64)) {
			u.kind[a] = m6FloatAny
		} else {
			return fmt.Errorf("type mismatch: %s and %s", u.kind[a], u.kind[b])
		}
	}
	if u.structArity[a] >= 0 && u.structArity[b] >= 0 && u.structArity[a] != u.structArity[b] {
		return fmt.Errorf("struct field count mismatch: %d and %d", u.structArity[a], u.structArity[b])
	}
	u.parent[b] = a
	if u.kind[a] == m6Unknown {
		u.kind[a] = u.kind[b]
	}
	u.nullable[a] = u.nullable[a] || u.nullable[b]
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
	if u.enumArities[a] == nil && u.enumArities[b] != nil {
		u.enumArities[a] = make(map[int]int, len(u.enumArities[b]))
		u.enumFields[a] = make(map[int]map[int]int, len(u.enumFields[b]))
		for tag, arity := range u.enumArities[b] {
			u.enumArities[a][tag] = arity
			fields := make(map[int]int, len(u.enumFields[b][tag]))
			for index, node := range u.enumFields[b][tag] {
				fields[index] = node
			}
			u.enumFields[a][tag] = fields
		}
	} else {
		for tag, arity := range u.enumArities[b] {
			if current, ok := u.enumArities[a][tag]; ok && current != arity {
				return fmt.Errorf("enum tag %d payload count mismatch: %d and %d", tag, current, arity)
			}
			if _, ok := u.enumArities[a][tag]; !ok {
				u.enumArities[a][tag] = arity
				u.enumFields[a][tag] = make(map[int]int, len(u.enumFields[b][tag]))
			}
			for index, node := range u.enumFields[b][tag] {
				if current, ok := u.enumFields[a][tag][index]; ok {
					if err := u.join(current, node); err != nil {
						return err
					}
				} else {
					u.enumFields[a][tag][index] = node
				}
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

func (u *m6Union) newEnum(tag int, values []int) int {
	node := u.newKind(m6Enum)
	u.enumArities[node] = map[int]int{tag: len(values)}
	u.enumFields[node] = map[int]map[int]int{tag: {}}
	for index, value := range values {
		u.enumFields[node][tag][index] = value
	}
	return node
}

func (u *m6Union) enumValue(node, tag, index int) (int, int, error) {
	root := u.root(node)
	if err := u.require(root, m6Enum); err != nil {
		return 0, 0, err
	}
	root = u.root(root)
	if tag >= 0 {
		arity, ok := u.enumArities[root][tag]
		if !ok {
			return 0, 0, fmt.Errorf("enum tag %d is outside known layout", tag)
		}
		if index < 0 || index >= arity {
			return 0, arity, fmt.Errorf("enum value %d is outside tag %d layout", index, tag)
		}
		return u.enumFields[root][tag][index], arity, nil
	}
	result, arity := -1, -1
	for candidateTag, candidateArity := range u.enumArities[root] {
		if index < 0 || index >= candidateArity {
			continue
		}
		candidate := u.enumFields[root][candidateTag][index]
		if result < 0 {
			result, arity = candidate, candidateArity
			continue
		}
		if err := u.join(result, candidate); err != nil {
			return 0, 0, fmt.Errorf("ambiguous enum value %d without tag narrowing: %w", index, err)
		}
	}
	if result < 0 {
		return 0, 0, fmt.Errorf("enum value %d is outside known layout", index)
	}
	return result, arity, nil
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

func (u *m6Union) newArray(elements []int, kind m6Kind, nullable bool) (int, error) {
	node := u.newKind(m6Array)
	element := u.newKind(kind)
	if nullable {
		u.makeNullable(element)
	}
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

func (u *m6Union) arrayElementNullable(node int) bool {
	root := u.root(node)
	return u.element[root] >= 0 && u.isNullable(u.element[root])
}

func m12ReferenceKind(kind m6Kind) bool {
	return kind == m6String || kind == m6Array || kind == m6Struct || kind == m6Enum || kind == m6Function
}

func m6IntegerKind(kind m6Kind) bool {
	return kind == m6Int || kind == m6Int8 || kind == m6Int16 || kind == m6Int32 ||
		kind == m6Uint8 || kind == m6Uint16 || kind == m6Uint32
}

func m6UnsignedKind(kind m6Kind) bool {
	return kind == m6Uint8 || kind == m6Uint16 || kind == m6Uint32
}

func m6IntegerWidth(kind m6Kind) uint32 {
	switch kind {
	case m6Int8, m6Uint8:
		return 8
	case m6Int16, m6Uint16:
		return 16
	case m6Int32, m6Uint32:
		return 32
	default:
		return 64
	}
}

func m6IntegerTarget(target int) (m6Kind, bool) {
	switch target {
	case code.IntegerInt8:
		return m6Int8, true
	case code.IntegerInt16:
		return m6Int16, true
	case code.IntegerInt32:
		return m6Int32, true
	case code.IntegerInt64:
		return m6Int, true
	case code.IntegerUint8:
		return m6Uint8, true
	case code.IntegerUint16:
		return m6Uint16, true
	case code.IntegerUint32:
		return m6Uint32, true
	default:
		return m6Unknown, false
	}
}

func m13BoxedKind(kind m6Kind) bool {
	return kind != m6Unknown && kind != m6Nil && !m12ReferenceKind(kind)
}

func m13ReferenceNode(u *m6Union, node int) bool {
	kind := u.get(node)
	return m12ReferenceKind(kind) || u.isNullable(node) && m13BoxedKind(kind)
}

func m13RawProducer(op code.Opcode) bool {
	switch op {
	case code.OpConstant, code.OpTrue, code.OpFalse, code.OpNot, code.OpIsNil,
		code.OpNeg, code.OpAdd, code.OpSub, code.OpMul, code.OpDiv,
		code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual,
		code.OpGreater, code.OpGreaterEqual, code.OpIntToFloat,
		code.OpFloatToInt, code.OpIntToFloat32, code.OpFloat32ToInt,
		code.OpFloat32ToFloat64, code.OpFloat64ToFloat32, code.OpLen,
		code.OpArrayCapacity, code.OpIntegerConvert, code.OpMath:
		return true
	default:
		return false
	}
}

func m13OutputKind(kind m6Kind) uint8 {
	switch kind {
	case m6Bool:
		return 1
	case m6Float32:
		return 3
	case m6Float64:
		return 4
	case m6String:
		return 5
	default:
		return 0
	}
}

func m12ElementBytes(kind m6Kind) int {
	if kind == m6Uint8 {
		return 1
	}
	return 8
}

func m13ElementBytes(kind m6Kind, nullable bool) int {
	if nullable {
		return 8
	}
	return m12ElementBytes(kind)
}

func m13StoredReference(kind m6Kind, nullable bool) bool {
	return m12ReferenceKind(kind) || nullable && m13BoxedKind(kind)
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
	exit       [][]int
	endKind    [][]m6Kind
	enumTags   map[int]int
}

type m6Analysis struct {
	u                   *m6Union
	blocks              []*m6Block
	globals             []int
	locals              [][]int
	returns             []int
	captures            [][]int
	genericArrayWrapper map[int]code.Opcode
	genericMathWrapper  map[int]int
	reachableFunctions  []bool
}

// m16GenericArrayWrapper recognizes the tiny generic kiso/array forwarding
// functions emitted by the self-hosted compiler. Mochi v14 does not carry
// instantiated type arguments, so sharing one inferred function signature
// across all calls would incorrectly merge []T for unrelated T. These exact
// wrappers are instead specialized at each static call site.
func m16GenericArrayWrapper(fn compiler.CompiledFunction) (code.Opcode, bool) {
	wantName := map[code.Opcode]string{
		code.OpArrayMake: "make", code.OpArrayWithCapacity: "with_capacity",
		code.OpArrayPush: "push", code.OpArrayCapacity: "capacity",
		code.OpArrayReserve: "reserve", code.OpArrayClone: "clone",
		code.OpArrayTruncate: "truncate", code.OpArrayAppendAll: "append_all",
	}
	inst, _, err := decodeM4Block(fn.Instructions)
	if err != nil {
		return 0, false
	}
	for op, name := range wantName {
		if fn.Name != name && !strings.HasSuffix(fn.Name, "::"+name) {
			continue
		}
		parameters := 1
		returns := true
		if op == code.OpArrayMake || op == code.OpArrayPush || op == code.OpArrayReserve ||
			op == code.OpArrayTruncate || op == code.OpArrayAppendAll {
			parameters = 2
		}
		if op == code.OpArrayPush || op == code.OpArrayReserve || op == code.OpArrayTruncate || op == code.OpArrayAppendAll {
			returns = false
		}
		if fn.ParameterCount != parameters || fn.LocalCount != parameters || fn.ReturnsValue != returns || len(inst) != parameters+2 {
			return 0, false
		}
		for index := 0; index < parameters; index++ {
			if inst[index].op != code.OpGetLocal || inst[index].operands[0] != index {
				return 0, false
			}
		}
		if inst[parameters].op != op {
			return 0, false
		}
		last := code.OpReturn
		if returns {
			last = code.OpReturnValue
		}
		if inst[parameters+1].op != last {
			return 0, false
		}
		return op, true
	}
	return 0, false
}

func m16GenericMathWrapper(fn compiler.CompiledFunction) (int, bool) {
	if fn.ParameterCount != 1 || fn.LocalCount != 1 || !fn.ReturnsValue {
		return 0, false
	}
	inst, _, err := decodeM4Block(fn.Instructions)
	if err != nil || len(inst) != 3 || inst[0].op != code.OpGetLocal || inst[0].operands[0] != 0 ||
		inst[1].op != code.OpMath || inst[1].operands[1] != 1 || inst[2].op != code.OpReturnValue {
		return 0, false
	}
	primitive := inst[1].operands[0]
	return primitive, primitive >= code.MathSqrt && primitive <= code.MathIsFinite
}

func m16ReachableFunctions(p *compiler.Bytecode) ([]bool, error) {
	reachable := make([]bool, len(p.Functions))
	queue := []int{-1}
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		source := p.Instructions
		name := "main"
		if id >= 0 {
			source = p.Functions[id].Instructions
			name = p.Functions[id].Name
		}
		inst, _, err := decodeM4Block(source)
		if err != nil {
			return nil, fmt.Errorf("AOT M16b reachability %s: %w", name, err)
		}
		for _, instruction := range inst {
			if instruction.op != code.OpCall && instruction.op != code.OpFunction && instruction.op != code.OpClosure {
				continue
			}
			target := instruction.operands[0]
			if target < 0 || target >= len(p.Functions) {
				return nil, fmt.Errorf("AOT M16b reachability %s has invalid function %d", name, target)
			}
			if !reachable[target] {
				reachable[target] = true
				queue = append(queue, target)
			}
		}
	}
	return reachable, nil
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
				case code.OpNeg, code.OpNot, code.OpIsNil, code.OpIntToFloat, code.OpFloatToInt,
					code.OpIntToFloat32, code.OpFloat32ToInt, code.OpFloat32ToFloat64,
					code.OpFloat64ToFloat32, code.OpIntegerConvert, code.OpDataConvert, code.OpLen, code.OpGetField,
					code.OpArrayCapacity, code.OpArrayClone, code.OpEnumTag, code.OpGetEnumValue:
					pop()
					pushUnknown()
				case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual,
					code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater,
					code.OpGreaterEqual, code.OpIndex, code.OpArrayMake:
					pop()
					pop()
					pushUnknown()
				case code.OpSetIndex, code.OpSetNullableIndex:
					pop()
					pop()
					pop()
				case code.OpArray, code.OpArrayTyped, code.OpStruct:
					for range ins.operands[0] {
						pop()
					}
					pushUnknown()
				case code.OpEnum:
					for range ins.operands[1] {
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
	// A function value carried in a struct or parameter may not have a target
	// set until the full type pass, but an immediately consuming instruction
	// still makes the return shape unambiguous in validated compiler output.
	for bi, block := range blocks {
		for index, ins := range block.inst {
			if ins.op != code.OpCallValue || index+1 >= len(block.inst) {
				continue
			}
			if _, known := hints[bi][ins.offset]; known {
				continue
			}
			switch block.inst[index+1].op {
			case code.OpSetLocal, code.OpDefineLocal, code.OpSetGlobal, code.OpSetUpvalue,
				code.OpPop, code.OpChant, code.OpReturnValue:
				hints[bi][ins.offset] = true
			}
		}
	}
	return hints
}

// inferEnumMatchTags recognizes the exact awase sequence emitted by Compiler:
// load temporary, enum_tag, integer tag, equal, jump_if_false, then one
// load/get_enum_value/store triple per binding. The bytecode carries no enum
// type table, so this tag narrowing is what keeps differently typed payloads
// in different variants precise without changing the Mochi format.
func inferEnumMatchTags(p *compiler.Bytecode, b *m6Block) map[int]int {
	result := make(map[int]int)
	for i := 4; i < len(b.inst); i++ {
		jump := b.inst[i]
		if jump.op != code.OpJumpIfFalse || b.inst[i-1].op != code.OpEqual ||
			b.inst[i-3].op != code.OpEnumTag || b.inst[i-2].op != code.OpConstant {
			continue
		}
		load := b.inst[i-4].op
		if load != code.OpGetLocal && load != code.OpGetGlobal {
			continue
		}
		constant := b.inst[i-2].operands[0]
		if constant < 0 || constant >= len(p.Constants) {
			continue
		}
		tag64, ok := p.Constants[constant].Int64()
		if !ok || tag64 < 0 || tag64 > 0xffff {
			continue
		}
		for j := i + 1; j+2 < len(b.inst); j += 3 {
			get := b.inst[j].op
			set := b.inst[j+2].op
			if (get != code.OpGetLocal && get != code.OpGetGlobal) ||
				b.inst[j+1].op != code.OpGetEnumValue ||
				(set != code.OpSetLocal && set != code.OpSetGlobal) {
				break
			}
			result[b.inst[j+1].offset] = int(tag64)
		}
	}
	return result
}

// m6Reachability 先独立验证栈深度、局部初始化与每条跳转的边界。
// 这样类型约束求解不需要将递归函数在编译期展开。
func m6Reachability(p *compiler.Bytecode, b *m6Block, callReturns map[int]bool) error {
	n := len(b.inst)
	if n == 0 && b.fn != nil {
		if _, _, ok := m14NativeSignature(b.fn.Name); ok {
			return nil
		}
	}
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
			push = 1
		case code.OpSetGlobal, code.OpSetLocal, code.OpDefineLocal, code.OpSetUpvalue,
			code.OpJumpIfFalse, code.OpChant, code.OpPop:
			need = 1
		case code.OpNeg, code.OpNot, code.OpIsNil, code.OpIntToFloat, code.OpFloatToInt,
			code.OpIntToFloat32, code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32,
			code.OpIntegerConvert, code.OpDataConvert:
			need, push = 1, 1
		case code.OpMath:
			need = ins.operands[1]
			if ins.operands[0] != code.MathError {
				push = 1
			}
		case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual, code.OpNotEqual,
			code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			need, push = 2, 1
		case code.OpArray, code.OpArrayTyped, code.OpStruct:
			need, push = ins.operands[0], 1
		case code.OpEnum:
			need, push = ins.operands[1], 1
		case code.OpIndex:
			need, push = 2, 1
		case code.OpSetIndex, code.OpSetNullableIndex:
			need = 3
		case code.OpArrayMake:
			need, push = 2, 1
		case code.OpArrayWithCapacity, code.OpArrayCapacity, code.OpArrayClone:
			need, push = 1, 1
		case code.OpArrayPush, code.OpArrayReserve, code.OpArrayTruncate, code.OpArrayAppendAll:
			need = 2
		case code.OpLen, code.OpGetField, code.OpEnumTag, code.OpGetEnumValue:
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
	reachableFunctions, err := m16ReachableFunctions(p)
	if err != nil {
		return nil, err
	}
	a := &m6Analysis{
		u: &m6Union{}, genericArrayWrapper: make(map[int]code.Opcode),
		genericMathWrapper: make(map[int]int), reachableFunctions: reachableFunctions,
	}
	type pendingCallValue struct {
		block, offset int
		function      int
		arguments     []int
		result        int
		returns       bool
	}
	type pendingEnumValue struct {
		block, offset int
		enum, result  int
		tag, index    int
	}
	type pendingStructField struct {
		block, offset int
		structure     int
		result        int
		index         int
	}
	type pendingLen struct {
		block, offset int
		input         int
	}
	type pendingNumeric struct {
		block, offset int
		input         int
	}
	type pendingFloat struct {
		block, offset int
		input         int
	}
	type pendingInteger struct {
		block, offset int
		input         int
	}
	pendingCalls := make([]pendingCallValue, 0)
	pendingEnums := make([]pendingEnumValue, 0)
	pendingStructs := make([]pendingStructField, 0)
	pendingLengths := make([]pendingLen, 0)
	pendingNumerics := make([]pendingNumeric, 0)
	pendingFloats := make([]pendingFloat, 0)
	pendingIntegers := make([]pendingInteger, 0)
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
		if op, ok := m16GenericArrayWrapper(fn); ok {
			a.genericArrayWrapper[i] = op
			array, arrayErr := a.u.newArray(nil, m6Int, false)
			if arrayErr != nil {
				return nil, arrayErr
			}
			switch op {
			case code.OpArrayMake:
				if err := a.u.require(locals[0], m6Int); err != nil {
					return nil, err
				}
				if err := a.u.require(locals[1], m6Int); err != nil {
					return nil, err
				}
				if err := a.u.join(a.returns[i], array); err != nil {
					return nil, err
				}
			case code.OpArrayWithCapacity:
				if err := a.u.require(locals[0], m6Int); err != nil {
					return nil, err
				}
				if err := a.u.join(a.returns[i], array); err != nil {
					return nil, err
				}
			case code.OpArrayPush:
				if err := a.u.join(locals[0], array); err != nil {
					return nil, err
				}
				if err := a.u.require(locals[1], m6Int); err != nil {
					return nil, err
				}
			case code.OpArrayCapacity:
				if err := a.u.join(locals[0], array); err != nil {
					return nil, err
				}
				if err := a.u.require(a.returns[i], m6Int); err != nil {
					return nil, err
				}
			case code.OpArrayReserve, code.OpArrayTruncate:
				if err := a.u.join(locals[0], array); err != nil {
					return nil, err
				}
				if err := a.u.require(locals[1], m6Int); err != nil {
					return nil, err
				}
			case code.OpArrayClone:
				if err := a.u.join(locals[0], array); err != nil {
					return nil, err
				}
				if err := a.u.join(a.returns[i], array); err != nil {
					return nil, err
				}
			case code.OpArrayAppendAll:
				if err := a.u.join(locals[0], array); err != nil {
					return nil, err
				}
				if err := a.u.join(locals[1], array); err != nil {
					return nil, err
				}
			}
		}
		if primitive, ok := m16GenericMathWrapper(fn); ok {
			a.genericMathWrapper[i] = primitive
			if err := a.u.require(locals[0], m6Float64); err != nil {
				return nil, err
			}
			result := m6Float64
			if primitive >= code.MathIsNaN {
				result = m6Bool
			}
			if err := a.u.require(a.returns[i], result); err != nil {
				return nil, err
			}
		}
		if params, result, native := m14NativeSignature(fn.Name); native {
			if len(params) != fn.ParameterCount || !fn.ReturnsValue {
				return nil, fmt.Errorf("AOT M14 native signature mismatch for %q", fn.Name)
			}
			for index, kind := range params {
				if err := a.u.require(locals[index], kind); err != nil {
					return nil, err
				}
			}
			if err := a.u.require(a.returns[i], result); err != nil {
				return nil, err
			}
		}
		if m13PolymorphicFloatPredicate(fn.Name) {
			if fn.ParameterCount != 1 || !fn.ReturnsValue {
				return nil, fmt.Errorf("AOT M13c invalid polymorphic predicate %q", fn.Name)
			}
			if err := a.u.require(locals[0], m6FloatAny); err != nil {
				return nil, err
			}
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
			return nil, fmt.Errorf("AOT M6 %s: %w", b.name, err)
		}
		b.enumTags = inferEnumMatchTags(p, b)
	}
	callReturnHints := inferM15CallReturns(p, blocks)
	for bi, b := range blocks {
		if err := m6Reachability(p, b, callReturnHints[bi]); err != nil {
			return nil, err
		}
		b.entry = make([][]int, len(b.reach))
		b.exit = make([][]int, len(b.inst))
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
		if bi > 0 && !reachableFunctions[bi-1] {
			continue
		}
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
			case code.OpNil:
				push(m6Nil)
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
			case code.OpIsNil:
				value := pop()
				a.u.makeNullable(value)
				push(m6Bool)
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
				input := pop()
				if a.u.get(input) == m6Unknown {
					pendingIntegers = append(pendingIntegers, pendingInteger{block: bi, offset: ins.offset, input: input})
				} else if !m6IntegerKind(a.u.get(input)) {
					return nil, fail(fmt.Errorf("integer-to-float requires integer, got %s", a.u.get(input)))
				}
				if ins.op == code.OpIntToFloat {
					push(m6Float64)
				} else {
					push(m6Float32)
				}
			case code.OpIntegerConvert:
				input := pop()
				kind := a.u.get(input)
				if kind == m6Unknown {
					pendingNumerics = append(pendingNumerics, pendingNumeric{block: bi, offset: ins.offset, input: input})
				} else if !m6IntegerKind(kind) && kind != m6Float32 && kind != m6Float64 {
					return nil, fail(fmt.Errorf("integer conversion requires numeric value, got %s", kind))
				}
				target, ok := m6IntegerTarget(ins.operands[0])
				if !ok {
					return nil, fail(fmt.Errorf("unknown integer target %d", ins.operands[0]))
				}
				push(target)
			case code.OpDataConvert:
				input := pop()
				switch ins.operands[0] {
				case code.DataStringToBytes:
					if err := require(input, m6String); err != nil {
						return nil, fail(err)
					}
					array, err := a.u.newArray(nil, m6Uint8, false)
					if err != nil {
						return nil, fail(err)
					}
					stack = append(stack, array)
				case code.DataBytesToString:
					element, err := a.u.arrayElement(input)
					if err != nil {
						return nil, fail(err)
					}
					if err := require(element, m6Uint8); err != nil {
						return nil, fail(err)
					}
					result := a.u.newKind(m6String)
					a.u.makeNullable(result)
					stack = append(stack, result)
				default:
					return nil, fail(fmt.Errorf("unknown data conversion %d", ins.operands[0]))
				}
			case code.OpMath:
				primitive, arguments := ins.operands[0], ins.operands[1]
				if arguments != 1 {
					return nil, fail(fmt.Errorf("unsupported math argument count %d", arguments))
				}
				input := pop()
				switch primitive {
				case code.MathError:
					if err := require(input, m6String); err != nil {
						return nil, fail(err)
					}
				case code.MathSqrt, code.MathFloor, code.MathCeil, code.MathRoundEven, code.MathTrunc:
					kind := a.u.get(input)
					if kind == m6Unknown {
						pendingFloats = append(pendingFloats, pendingFloat{block: bi, offset: ins.offset, input: input})
					} else if kind != m6Float32 && kind != m6Float64 {
						return nil, fail(fmt.Errorf("floating primitive requires float32 or float64, got %s", kind))
					}
					stack = append(stack, input)
				case code.MathIsNaN, code.MathIsInf, code.MathIsFinite:
					kind := a.u.get(input)
					if kind == m6Unknown {
						pendingFloats = append(pendingFloats, pendingFloat{block: bi, offset: ins.offset, input: input})
					} else if kind != m6Float32 && kind != m6Float64 && kind != m6FloatAny {
						return nil, fail(fmt.Errorf("is_inf requires float32 or float64, got %s", kind))
					}
					push(m6Bool)
				case code.MathIsNegative, code.MathNegOverflows:
					kind := a.u.get(input)
					if kind == m6Unknown {
						pendingNumerics = append(pendingNumerics, pendingNumeric{block: bi, offset: ins.offset, input: input})
					} else if !m6IntegerKind(kind) && kind != m6Float32 && kind != m6Float64 {
						return nil, fail(fmt.Errorf("sign primitive requires a number, got %s", kind))
					}
					push(m6Bool)
				default:
					return nil, fail(fmt.Errorf("unsupported math primitive %d", primitive))
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
				structure := pop()
				field, err := a.u.structField(structure, ins.operands[0])
				if err != nil {
					field = a.u.newKind(m6Unknown)
					pendingStructs = append(pendingStructs, pendingStructField{
						block: bi, offset: ins.offset, structure: structure,
						result: field, index: ins.operands[0],
					})
				}
				stack = append(stack, field)
			case code.OpEnum:
				count := ins.operands[1]
				values := append([]int(nil), stack[len(stack)-count:]...)
				stack = stack[:len(stack)-count]
				stack = append(stack, a.u.newEnum(ins.operands[0], values))
			case code.OpEnumTag:
				if err := require(pop(), m6Enum); err != nil {
					return nil, fail(err)
				}
				push(m6Int)
			case code.OpGetEnumValue:
				enum := pop()
				tag := -1
				if narrowed, ok := b.enumTags[ins.offset]; ok {
					tag = narrowed
				}
				field, _, fieldErr := a.u.enumValue(enum, tag, ins.operands[0])
				if fieldErr != nil {
					field = a.u.newKind(m6Unknown)
					pendingEnums = append(pendingEnums, pendingEnumValue{
						block: bi, offset: ins.offset, enum: enum, result: field,
						tag: tag, index: ins.operands[0],
					})
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
				nullable := ins.op == code.OpArrayTyped && ins.operands[1]&0x80 != 0
				array, err := a.u.newArray(items, elementKind, nullable)
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, array)
			case code.OpArrayMake:
				initial, length := pop(), pop()
				if err := require(length, m6Int); err != nil {
					return nil, fail(err)
				}
				elementKind, nullable, err := m12DynamicDescriptorKind(ins.operands[0])
				if err != nil {
					return nil, fail(err)
				}
				array, err := a.u.newArray([]int{initial}, elementKind, nullable)
				if err != nil {
					return nil, fail(err)
				}
				stack = append(stack, array)
			case code.OpArrayWithCapacity:
				if err := require(pop(), m6Int); err != nil {
					return nil, fail(err)
				}
				elementKind, nullable, err := m12DynamicDescriptorKind(ins.operands[0])
				if err != nil {
					return nil, fail(err)
				}
				array, err := a.u.newArray(nil, elementKind, nullable)
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
			case code.OpSetIndex, code.OpSetNullableIndex:
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
				if ins.op == code.OpSetNullableIndex {
					a.u.makeNullable(arrayElement)
				}
			case code.OpLen:
				input := pop()
				kind := a.u.get(input)
				if kind == m6Unknown {
					pendingLengths = append(pendingLengths, pendingLen{block: bi, offset: ins.offset, input: input})
				} else if kind != m6String && kind != m6Array {
					return nil, fail(fmt.Errorf("OpLen requires string or array, got %s", kind))
				}
				push(m6Int)
			case code.OpCall:
				target := ins.operands[0]
				callee := p.Functions[target]
				if primitive, generic := a.genericMathWrapper[target]; generic {
					input := pop()
					kind := a.u.get(input)
					if kind == m6Unknown {
						pendingFloats = append(pendingFloats, pendingFloat{block: bi, offset: ins.offset, input: input})
					} else if kind != m6Float32 && kind != m6Float64 {
						return nil, fail(fmt.Errorf("generic math primitive %d requires float32 or float64, got %s", primitive, kind))
					}
					if primitive >= code.MathIsNaN {
						push(m6Bool)
					} else {
						stack = append(stack, input)
					}
					break
				}
				if op, generic := a.genericArrayWrapper[target]; generic {
					switch op {
					case code.OpArrayMake:
						initial, length := pop(), pop()
						if err := require(length, m6Int); err != nil {
							return nil, fail(err)
						}
						array, err := a.u.newArray([]int{initial}, m6Unknown, a.u.isNullable(initial))
						if err != nil {
							return nil, fail(err)
						}
						stack = append(stack, array)
					case code.OpArrayWithCapacity:
						if err := require(pop(), m6Int); err != nil {
							return nil, fail(err)
						}
						array, err := a.u.newArray(nil, m6Unknown, false)
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
					case code.OpArrayReserve, code.OpArrayTruncate:
						length, array := pop(), pop()
						if err := require(length, m6Int); err != nil {
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
						if err := join(sourceElement, destinationElement); err != nil {
							return nil, fail(err)
						}
					}
					break
				}
				for j := callee.ParameterCount - 1; j >= 0; j-- {
					argument := pop()
					if m13PolymorphicFloatPredicate(callee.Name) && j == 0 {
						kind := a.u.get(argument)
						if kind != m6Float32 && kind != m6Float64 {
							return nil, fail(fmt.Errorf("polymorphic float predicate requires float32 or float64, got %s", kind))
						}
						continue
					}
					if err := join(argument, a.locals[ins.operands[0]][j]); err != nil {
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
			b.exit[i] = append([]int(nil), stack...)
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
		for _, pending := range pendingStructs {
			field, err := a.u.structField(pending.structure, pending.index)
			if err != nil {
				continue
			}
			if err := a.u.join(pending.result, field); err != nil {
				return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", pending.block, pending.offset, err)
			}
		}
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
	for _, pending := range pendingStructs {
		field, err := a.u.structField(pending.structure, pending.index)
		if err != nil {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", pending.block, pending.offset, err)
		}
		if err := a.u.join(pending.result, field); err != nil {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", pending.block, pending.offset, err)
		}
	}
	for _, pending := range pendingLengths {
		kind := a.u.get(pending.input)
		if kind != m6String && kind != m6Array {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: OpLen requires string or array, got %s", pending.block, pending.offset, kind)
		}
	}
	for _, pending := range pendingNumerics {
		kind := a.u.get(pending.input)
		if !m6IntegerKind(kind) && kind != m6Float32 && kind != m6Float64 {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: integer conversion requires numeric value, got %s", pending.block, pending.offset, kind)
		}
	}
	for _, pending := range pendingFloats {
		kind := a.u.get(pending.input)
		if kind != m6Float32 && kind != m6Float64 && kind != m6FloatAny {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: floating primitive requires float32 or float64, got %s", pending.block, pending.offset, kind)
		}
	}
	for _, pending := range pendingIntegers {
		kind := a.u.get(pending.input)
		if !m6IntegerKind(kind) {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: integer-to-float requires integer, got %s", pending.block, pending.offset, kind)
		}
	}
	for _, pending := range pendingEnums {
		field, _, err := a.u.enumValue(pending.enum, pending.tag, pending.index)
		if err != nil {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", pending.block, pending.offset, err)
		}
		if err := a.u.join(pending.result, field); err != nil {
			return nil, fmt.Errorf("AOT M6 block %d offset %d: %w", pending.block, pending.offset, err)
		}
	}
	for bi, b := range a.blocks {
		b.endKind = make([][]m6Kind, len(b.entry))
		if bi > 0 && !reachableFunctions[bi-1] {
			continue
		}
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
				if k == m6Bool || k == m6String || k == m6Array || k == m6Struct || k == m6Enum || k == m6Function || k == m6Nil ||
					(m6UnsignedKind(k) && ins.op == code.OpNeg) {
					return nil, fmt.Errorf("AOT M6 unsupported arithmetic type %s for %s at %s offset %d", k, ins.name, b.name, ins.offset)
				}
			}
		}
	}
	return a, nil
}

func m12DescriptorKind(descriptor int) m6Kind {
	switch value.Kind(descriptor & 0x7f) {
	case value.KindInt:
		return m6Int
	case value.KindInt8:
		return m6Int8
	case value.KindInt16:
		return m6Int16
	case value.KindInt32:
		return m6Int32
	case value.KindUint8:
		return m6Uint8
	case value.KindUint16:
		return m6Uint16
	case value.KindUint32:
		return m6Uint32
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
	case value.KindEnum:
		return m6Enum
	default:
		return m6Unknown
	}
}

func m12DynamicDescriptorKind(descriptor int) (m6Kind, bool, error) {
	kind := m12DescriptorKind(descriptor)
	if kind == m6Unknown && descriptor&0x7f != int(value.KindInvalid) {
		return m6Unknown, false, fmt.Errorf("unsupported array element descriptor %d", descriptor)
	}
	return kind, descriptor&0x80 != 0, nil
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
	return compileM6WithOptions(p, false, false, darwinarm64.M8DefaultHeapCapacity, false)
}

func compileM6WithM11(p *compiler.Bytecode, enableM11 bool) ([]byte, error) {
	return compileM6WithOptions(p, enableM11, false, darwinarm64.M8DefaultHeapCapacity, false)
}

func compileM6WithOptions(p *compiler.Bytecode, enableM11, gcStress bool, heapCapacity uint64, gcTelemetry bool) ([]byte, error) {
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
	nativeFS := false
	for _, fn := range p.Functions {
		if _, ok := m14NativeName(fn.Name); ok {
			nativeFS = true
			break
		}
	}
	if nativeFS {
		if _, ok := seen[""]; !ok {
			seen[""] = len(stringTable)
			stringTable = append(stringTable, "")
		}
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
		if bi > 0 && !a.reachableFunctions[bi-1] && b.fn.ReturnsValue {
			maximum = 1
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
		if err := e.ConfigureRuntime(heapCapacity, gcTelemetry); err != nil {
			return nil, err
		}
		if bi == 0 && nativeFS {
			e.EnableNativeFS()
		}
		if b.fn != nil && (bi == 0 || a.reachableFunctions[bi-1]) {
			if op, ok := m14NativeName(b.fn.Name); ok {
				if err := e.EmitNativeFSFunction(op, b.fn.ParameterCount); err != nil {
					return nil, err
				}
			}
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
		if bi > 0 && !a.reachableFunctions[bi-1] {
			if b.fn.ReturnsValue {
				if err := e.PushInt64(0); err != nil {
					return nil, err
				}
				if err := e.ReturnValue(); err != nil {
					return nil, err
				}
			} else if err := e.ReturnUnit(); err != nil {
				return nil, err
			}
			if err := e.SetOperandDepth(0); err != nil {
				return nil, err
			}
			if err := e.FinishFunction(); err != nil {
				return nil, err
			}
			all[bi] = e
			continue
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
			setRoots := func(stackNodes []int, localsLive, globalsLive []bool) error {
				frameSlots := make([]int, 0)
				if b.fn != nil {
					for slot, yes := range localsLive {
						if yes && m13ReferenceNode(a.u, a.locals[bi-1][slot]) {
							frameSlots = append(frameSlots, slot)
						}
					}
				}
				for slot, node := range stackNodes {
					if m13ReferenceNode(a.u, node) {
						frameSlots = append(frameSlots, locals+slot)
					}
				}
				globalSlots := make([]int, 0)
				for slot, yes := range globalsLive {
					if yes && m13ReferenceNode(a.u, a.globals[slot]) {
						globalSlots = append(globalSlots, slot)
					}
				}
				return e.SetGCRoots(frameSlots, globalSlots)
			}
			if m12Objects {
				boxesResult := len(b.exit[i]) != 0 && m13RawProducer(ins.op) &&
					a.u.isNullable(b.exit[i][len(b.exit[i])-1]) && m13BoxedKind(a.u.get(b.exit[i][len(b.exit[i])-1]))
				switch ins.op {
				case code.OpStruct, code.OpArray, code.OpArrayTyped, code.OpArrayMake,
					code.OpArrayWithCapacity, code.OpArrayPush, code.OpArrayReserve,
					code.OpArrayClone, code.OpArrayAppendAll, code.OpFunction, code.OpClosure,
					code.OpEnum, code.OpDataConvert:
					emitErr = setRoots(b.entry[i], live.inLocals[i], live.inGlobals[i])
				case code.OpCall:
					if _, generic := a.genericArrayWrapper[ins.operands[0]]; generic {
						emitErr = setRoots(b.entry[i], live.inLocals[i], live.inGlobals[i])
					} else {
						params := p.Functions[ins.operands[0]].ParameterCount
						emitErr = setRoots(b.entry[i][:len(st)-params], live.outLocals[i], live.outGlobals[i])
					}
				case code.OpCallValue:
					params := ins.operands[0]
					emitErr = setRoots(b.entry[i][:len(st)-params-1], live.outLocals[i], live.outGlobals[i])
				default:
					if boxesResult {
						emitErr = setRoots(b.entry[i], live.inLocals[i], live.inGlobals[i])
					}
				}
				if emitErr != nil {
					return nil, fmt.Errorf("AOT M11b %s at offset %d: %w", b.name, ins.offset, emitErr)
				}
			}
			unbox := func(slot int) error {
				node := b.entry[i][slot]
				if a.u.isNullable(node) && m13BoxedKind(a.u.get(node)) {
					return e.UnboxNullableOperand(slot)
				}
				if m13BoxedKind(a.u.get(node)) {
					return e.UnboxScalarOperand(slot)
				}
				return nil
			}
			if emitErr == nil {
				switch ins.op {
				case code.OpNot, code.OpNeg, code.OpIntToFloat, code.OpFloatToInt,
					code.OpIntToFloat32, code.OpFloat32ToInt, code.OpFloat32ToFloat64,
					code.OpFloat64ToFloat32, code.OpIntegerConvert, code.OpMath, code.OpDataConvert,
					code.OpJumpIfFalse, code.OpChant, code.OpLen, code.OpIndex,
					code.OpArrayWithCapacity, code.OpArrayReserve, code.OpArrayTruncate:
					emitErr = unbox(len(st) - 1)
				case code.OpArrayMake:
					emitErr = unbox(len(st) - 2)
				case code.OpSetIndex, code.OpSetNullableIndex:
					emitErr = unbox(len(st) - 2)
				case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv, code.OpEqual,
					code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater,
					code.OpGreaterEqual:
					if emitErr = unbox(len(st) - 2); emitErr == nil {
						emitErr = unbox(len(st) - 1)
					}
				}
			}
			if emitErr != nil {
				return nil, fmt.Errorf("AOT M13 %s at offset %d: %w", b.name, ins.offset, emitErr)
			}
			switch ins.op {
			case code.OpConstant:
				v := p.Constants[ins.operands[0]]
				switch m6ValueKind(v) {
				case m6Int, m6Int8, m6Int16, m6Int32, m6Uint8, m6Uint16, m6Uint32:
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
					emitErr = e.PushInt64(int64(stringIDs[ins.operands[0]]<<1 | 1))
				}
			case code.OpTrue, code.OpFalse:
				emitErr = e.PushBool(ins.op == code.OpTrue)
			case code.OpNil:
				emitErr = e.PushInt64(0)
			case code.OpIsNil:
				node := b.entry[i][len(b.entry[i])-1]
				emitErr = e.IsNil(a.u.get(node) == m6Nil || a.u.isNullable(node))
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
					captureNode := a.captures[target][captureIndex]
					captures[captureIndex] = darwinarm64.ClosureCapture{
						Local: capture.Local, Index: capture.Index,
						Reference: m13ReferenceNode(a.u, captureNode),
					}
				}
				emitErr = e.NewClosure(target, p.Functions[target].ParameterCount, p.Functions[target].ReturnsValue, captures)
			case code.OpNot:
				emitErr = e.NotBool()
			case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv:
				if m6IntegerKind(k) {
					switch ins.op {
					case code.OpAdd:
						emitErr = e.Add()
					case code.OpSub:
						emitErr = e.Sub()
					case code.OpMul:
						emitErr = e.Mul()
					case code.OpDiv:
						emitErr = e.DivideInteger(m6UnsignedKind(k))
					}
					if emitErr == nil {
						emitErr = e.NormalizeInteger(m6IntegerWidth(k), !m6UnsignedKind(k))
					}
				} else {
					emitErr = e.FloatBinary(uint8(k), uint8(ins.op))
				}
			case code.OpNeg:
				if m6IntegerKind(k) {
					emitErr = e.Neg()
					if emitErr == nil {
						emitErr = e.NormalizeInteger(m6IntegerWidth(k), true)
					}
				} else {
					emitErr = e.FloatNeg(uint8(k))
				}
			case code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
				leftKind := st[len(st)-2]
				if leftKind == m6String {
					emitErr = e.StringCompare(ins.op == code.OpNotEqual)
				} else if leftKind == m6Float32 || leftKind == m6Float64 {
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
						if m6UnsignedKind(leftKind) {
							cond = 3
						}
					case code.OpLessEqual:
						cond = 13
						if m6UnsignedKind(leftKind) {
							cond = 9
						}
					case code.OpGreater:
						cond = 12
						if m6UnsignedKind(leftKind) {
							cond = 8
						}
					case code.OpGreaterEqual:
						cond = 10
						if m6UnsignedKind(leftKind) {
							cond = 2
						}
					}
					emitErr = e.Compare(cond)
				}
			case code.OpIntToFloat, code.OpIntToFloat32, code.OpFloatToInt,
				code.OpFloat32ToInt, code.OpFloat32ToFloat64, code.OpFloat64ToFloat32:
				emitErr = e.FloatConvert(uint8(ins.op))
			case code.OpIntegerConvert:
				source := k
				if source == m6Float32 || source == m6Float64 {
					convert := code.OpFloatToInt
					if source == m6Float32 {
						convert = code.OpFloat32ToInt
					}
					emitErr = e.FloatConvert(uint8(convert))
				}
				if emitErr == nil {
					emitErr = e.IntegerConvert(m6UnsignedKind(source), ins.operands[0])
				}
			case code.OpDataConvert:
				switch ins.operands[0] {
				case code.DataBytesToString:
					emitErr = e.BytesToString()
				case code.DataStringToBytes:
					emitErr = e.StringToBytes()
				default:
					return nil, fmt.Errorf("AOT M13c unknown data conversion %d at %s offset %d", ins.operands[0], b.name, ins.offset)
				}
			case code.OpMath:
				switch ins.operands[0] {
				case code.MathSqrt, code.MathFloor, code.MathCeil, code.MathRoundEven, code.MathTrunc:
					emitErr = e.MathFloatUnary(uint8(k), ins.operands[0])
				case code.MathIsNaN, code.MathIsFinite:
					emitErr = e.MathFloatPredicate(uint8(k), ins.operands[0], k == m6FloatAny)
				case code.MathIsInf:
					emitErr = e.MathIsInf(uint8(k), k == m6FloatAny)
				case code.MathIsNegative, code.MathNegOverflows:
					floating := k == m6Float32 || k == m6Float64
					width := m6IntegerWidth(k)
					if floating {
						width = 64
						if k == m6Float32 {
							width = 32
						}
					}
					emitErr = e.MathSign(width, !m6UnsignedKind(k), floating, ins.operands[0] == code.MathNegOverflows)
				case code.MathError:
					emitErr = e.MathError()
				default:
					return nil, fmt.Errorf("AOT M13c unsupported math primitive %d at %s offset %d", ins.operands[0], b.name, ins.offset)
				}
			case code.OpJump:
				emitErr = e.Jump(ins.operands[0])
			case code.OpJumpIfFalse:
				emitErr = e.JumpIfFalse(ins.operands[0])
			case code.OpChant:
				if k == m6Nil {
					emitErr = e.ChantNil()
				} else if k == m6Array || k == m6Struct || k == m6Enum {
					return nil, fmt.Errorf("AOT M12 cannot chant composite value at %s offset %d", b.name, ins.offset)
				} else if k == m6String {
					emitErr = e.ChantString()
				} else if k == m6Float32 || k == m6Float64 {
					emitErr = e.ChantFloat(uint8(k))
				} else {
					emitErr = e.Chant(k == m6Bool)
				}
			case code.OpPop:
				node := b.entry[i][len(b.entry[i])-1]
				if k == m6Nil {
					emitErr = e.PopNil()
				} else if a.u.isNullable(node) && m13BoxedKind(k) {
					emitErr = e.PopNullable(m13OutputKind(k))
				} else if bi == 0 && (k == m6Array || k == m6Struct || k == m6Enum) {
					return nil, fmt.Errorf("AOT M12 cannot print composite expression result at %s offset %d", b.name, ins.offset)
				} else if k == m6String {
					emitErr = e.PopString()
				} else if k == m6Float32 || k == m6Float64 {
					emitErr = e.PopFloat(uint8(k))
				} else {
					emitErr = e.Pop(k == m6Bool)
				}
			case code.OpCall:
				target := ins.operands[0]
				if primitive, generic := a.genericMathWrapper[target]; generic {
					kind := a.u.get(b.entry[i][len(b.entry[i])-1])
					switch primitive {
					case code.MathSqrt, code.MathFloor, code.MathCeil, code.MathRoundEven, code.MathTrunc:
						emitErr = e.MathFloatUnary(uint8(kind), primitive)
					case code.MathIsInf:
						emitErr = e.MathIsInf(uint8(kind), false)
					case code.MathIsNaN, code.MathIsFinite:
						emitErr = e.MathFloatPredicate(uint8(kind), primitive, false)
					}
				} else if op, generic := a.genericArrayWrapper[target]; generic {
					arrayNode := -1
					switch op {
					case code.OpArrayMake:
						element := b.entry[i][len(b.entry[i])-1]
						emitErr = e.ArrayMake(m13ElementBytes(a.u.get(element), a.u.isNullable(element)), m13StoredReference(a.u.get(element), a.u.isNullable(element)))
					case code.OpArrayWithCapacity:
						arrayNode = b.exit[i][len(b.exit[i])-1]
					case code.OpArrayPush, code.OpArrayReserve, code.OpArrayTruncate, code.OpArrayAppendAll:
						arrayNode = b.entry[i][len(b.entry[i])-2]
					case code.OpArrayCapacity, code.OpArrayClone:
						arrayNode = b.entry[i][len(b.entry[i])-1]
					}
					if emitErr == nil && arrayNode >= 0 {
						elementKind := a.u.arrayElementKind(arrayNode)
						nullable := a.u.arrayElementNullable(arrayNode)
						if elementKind == m6Unknown {
							return nil, fmt.Errorf("AOT M16b unknown specialized array element type at %s offset %d", b.name, ins.offset)
						}
						elementBytes := m13ElementBytes(elementKind, nullable)
						reference := m13StoredReference(elementKind, nullable)
						switch op {
						case code.OpArrayWithCapacity:
							emitErr = e.ArrayWithCapacity(elementBytes, reference)
						case code.OpArrayPush:
							emitErr = e.ArrayPush(elementBytes, reference)
						case code.OpArrayCapacity:
							emitErr = e.ArrayCapacity()
						case code.OpArrayReserve:
							emitErr = e.ArrayReserve(elementBytes, reference)
						case code.OpArrayClone:
							emitErr = e.ArrayClone(elementBytes, reference)
						case code.OpArrayTruncate:
							emitErr = e.ArrayTruncate(elementBytes, reference)
						case code.OpArrayAppendAll:
							emitErr = e.ArrayAppendAll(elementBytes, reference)
						}
					}
				} else {
					callee := p.Functions[target]
					emitErr = e.CallNative(target, callee.ParameterCount, callee.ReturnsValue)
				}
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
				for field, node := range b.entry[i][len(st)-count:] {
					if m13ReferenceNode(a.u, node) {
						bitmap[field/64] |= uint64(1) << uint(field%64)
					}
				}
				emitErr = e.NewStructObject(count, bitmap)
			case code.OpGetField:
				node := b.entry[i][len(b.entry[i])-1]
				emitErr = e.GetStructField(ins.operands[0], a.u.structFieldCount(node))
			case code.OpEnum:
				count := ins.operands[1]
				bitmap := make([]uint64, (count+63)/64)
				for payload, node := range b.entry[i][len(st)-count:] {
					if m13ReferenceNode(a.u, node) {
						bitmap[payload/64] |= uint64(1) << uint(payload%64)
					}
				}
				emitErr = e.NewEnumObject(ins.operands[0], count, bitmap)
			case code.OpEnumTag:
				node := b.entry[i][len(b.entry[i])-1]
				emitErr = e.EnumTag(a.u.isNullable(node))
			case code.OpGetEnumValue:
				enumNode := b.entry[i][len(b.entry[i])-1]
				tag := -1
				if narrowed, ok := b.enumTags[ins.offset]; ok {
					tag = narrowed
				}
				_, count, fieldErr := a.u.enumValue(enumNode, tag, ins.operands[0])
				if fieldErr != nil {
					emitErr = fieldErr
				} else {
					emitErr = e.GetEnumValue(ins.operands[0], count, a.u.isNullable(enumNode))
				}
			case code.OpArray, code.OpArrayTyped:
				count := ins.operands[0]
				elementKind := m6Unknown
				nullable := ins.op == code.OpArrayTyped && ins.operands[1]&0x80 != 0
				if count > 0 {
					elementNode := b.entry[i][len(st)-count]
					elementKind = a.u.get(elementNode)
					nullable = a.u.isNullable(elementNode)
				} else if ins.op == code.OpArrayTyped {
					elementKind = m12DescriptorKind(ins.operands[1])
				}
				if elementKind == m6Unknown {
					elementKind = m6Int
				}
				emitErr = e.NewArrayObject(count, m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayMake:
				elementKind := m12DescriptorKind(ins.operands[0])
				nullable := ins.operands[0]&0x80 != 0
				if elementKind == m6Unknown {
					elementNode := b.entry[i][len(st)-1]
					elementKind = a.u.get(elementNode)
					nullable = a.u.isNullable(elementNode)
				}
				emitErr = e.ArrayMake(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayWithCapacity:
				elementKind := m12DescriptorKind(ins.operands[0])
				nullable := ins.operands[0]&0x80 != 0
				if elementKind == m6Unknown {
					next := b.boundaries[ins.next]
					if next < len(b.entry) && len(b.entry[next]) != 0 {
						elementKind = a.u.arrayElementKind(b.entry[next][len(b.entry[next])-1])
					}
					if elementKind == m6Unknown {
						elementKind = m6Int
					}
				}
				if len(b.exit[i]) != 0 {
					nullable = a.u.arrayElementNullable(b.exit[i][len(b.exit[i])-1])
				}
				emitErr = e.ArrayWithCapacity(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayPush:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayPush(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayCapacity:
				emitErr = e.ArrayCapacity()
			case code.OpArrayReserve:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayReserve(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayClone:
				arrayNode := b.entry[i][len(b.entry[i])-1]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayClone(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayTruncate:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayTruncate(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpArrayAppendAll:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayAppendAll(m13ElementBytes(elementKind, nullable), m13StoredReference(elementKind, nullable))
			case code.OpIndex:
				arrayNode := b.entry[i][len(b.entry[i])-2]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArrayIndex(m13ElementBytes(elementKind, nullable))
			case code.OpSetIndex, code.OpSetNullableIndex:
				arrayNode := b.entry[i][len(b.entry[i])-3]
				elementKind := a.u.arrayElementKind(arrayNode)
				nullable := a.u.arrayElementNullable(arrayNode)
				if elementKind == m6Unknown {
					return nil, fmt.Errorf("AOT M12 unknown array element type at %s offset %d", b.name, ins.offset)
				}
				emitErr = e.ArraySetIndex(m13ElementBytes(elementKind, nullable))
			case code.OpLen:
				if k == m6String {
					emitErr = e.StringLen()
				} else {
					emitErr = e.ArrayLen()
				}
			default:
				return nil, fmt.Errorf("AOT M6 unsupported opcode %s at %s offset %d", ins.name, b.name, ins.offset)
			}
			if emitErr == nil && len(b.exit[i]) != 0 && m13RawProducer(ins.op) {
				result := b.exit[i][len(b.exit[i])-1]
				if a.u.isNullable(result) && m13BoxedKind(a.u.get(result)) {
					emitErr = e.BoxNullableOperand(len(b.exit[i]) - 1)
				}
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
