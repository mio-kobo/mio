package vm

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"Mio/mma"
)

const (
	// MaxCallDepth 限制同时存在的用户函数调用层数
	MaxCallDepth = 1024
	// MaxValueStack 限制共享 Value Stack 的元素数量
	MaxValueStack = 1 << 16
	// DefaultInstructionLimit 限制单次执行的指令数量
	DefaultInstructionLimit    uint64 = 10_000_000
	arrayStackReleaseThreshold        = 1024
)

// ExecutionOptions 配置单次字节码执行
type ExecutionOptions struct {
	// Context 用于取消执行；nil 表示不主动取消
	Context context.Context
	// InstructionLimit 限制指令数量；0 使用默认值
	InstructionLimit uint64
}

type frame struct {
	functionID   int
	instructions code.Instructions
	ip           int
	base         int
	upvalues     []*value.Cell
	cells        map[int]*value.Cell
}

// VM 执行 Mio 字节码
type VM struct {
	bytecode  *compiler.Bytecode
	stack     []value.Value
	frames    []frame
	globals   []value.Value
	output    io.Writer
	registry  *mma.ModuleRegistry
	native    []nativeBinding
	bound     bool
	validated bool

	lastPopped    value.Value
	hasLastPopped bool
}

type nativeBinding struct {
	function  mma.NativeFunction
	signature mma.FunctionSignature
}

// New 创建字节码虚拟机
func New(bytecode *compiler.Bytecode) *VM {
	return NewWithOutput(bytecode, os.Stdout)
}

// NewWithOutput 创建使用指定输出流的 VM；nil 输出流在执行 chant 时报告错误。
func NewWithOutput(bytecode *compiler.Bytecode, output io.Writer) *VM {
	return NewWithRegistryAndOutput(bytecode, nil, output)
}

// NewWithRegistryAndOutput 创建绑定原生模块注册表的 VM
func NewWithRegistryAndOutput(bytecode *compiler.Bytecode, registry *mma.ModuleRegistry, output io.Writer) *VM {
	return &VM{bytecode: bytecode, stack: make([]value.Value, 0), frames: make([]frame, 0), output: output, registry: registry}
}

// Run 使用默认预算解码并执行字节码
func (v *VM) Run() error {
	return v.RunWithOptions(ExecutionOptions{})
}

// RunWithOptions 使用指定选项解码并执行字节码
func (v *VM) RunWithOptions(options ExecutionOptions) (runErr error) {
	v.resetExecutionState()
	defer func() {
		if runErr != nil {
			if _, ok := runErr.(*RuntimeError); !ok {
				runErr = v.wrapRuntimeError(runErr)
			}
			v.resetExecutionState()
		}
	}()

	if v.bytecode == nil {
		return fmt.Errorf("nil bytecode")
	}
	if v.bytecode.GlobalsCount < 0 || v.bytecode.GlobalsCount > 1<<16 {
		return fmt.Errorf("invalid global slot count %d", v.bytecode.GlobalsCount)
	}
	if len(v.bytecode.Functions) > 1<<16 {
		return fmt.Errorf("invalid function count %d", len(v.bytecode.Functions))
	}
	for index, function := range v.bytecode.Functions {
		if function.ID != index {
			return fmt.Errorf("invalid FunctionID %d at function index %d", function.ID, index)
		}
		if function.ParameterCount < 0 ||
			function.LocalCount < function.ParameterCount ||
			function.LocalCount > MaxValueStack {
			return fmt.Errorf("invalid frame metadata for function %d", index)
		}
	}
	if !v.bound {
		if err := v.bindNativeFunctions(); err != nil {
			return err
		}
		v.bound = true
	}
	if !v.validated {
		if err := validateInstructionBoundaries(v.bytecode.Instructions); err != nil {
			return fmt.Errorf("main instructions: %w", err)
		}
		for index, function := range v.bytecode.Functions {
			if err := validateInstructionBoundaries(function.Instructions); err != nil {
				return fmt.Errorf("function %d instructions: %w", index, err)
			}
		}
		v.validated = true
	}

	if cap(v.globals) < v.bytecode.GlobalsCount {
		v.globals = make([]value.Value, v.bytecode.GlobalsCount)
	} else {
		v.globals = v.globals[:v.bytecode.GlobalsCount]
		clear(v.globals)
	}
	v.frames = append(v.frames[:0], frame{functionID: -1, instructions: v.bytecode.Instructions})

	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	limit := options.InstructionLimit
	if limit == 0 {
		limit = DefaultInstructionLimit
	}
	var executed uint64
	var operands [2]int

	for len(v.frames) > 0 {
		frameIndex := len(v.frames) - 1
		current := &v.frames[frameIndex]
		if current.ip == len(current.instructions) {
			if current.functionID >= 0 {
				return fmt.Errorf("function %d exited without return", current.functionID)
			}
			if len(v.stack) != 0 {
				return fmt.Errorf("main exited with %d value stack entries", len(v.stack))
			}
			v.frames = v.frames[:frameIndex]
			continue
		}
		if current.ip < 0 || current.ip > len(current.instructions) {
			return fmt.Errorf("instruction pointer %d out of range", current.ip)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution interrupted: %w", ctx.Err())
		default:
		}
		if executed >= limit {
			return fmt.Errorf("instruction limit %d exceeded", limit)
		}
		executed++

		ip := current.ip
		op := code.Opcode(current.instructions[ip])
		def, err := code.Lookup(op)
		if err != nil {
			return fmt.Errorf("at offset %d: %w", ip, err)
		}
		read := 0
		if len(def.OperandWidths) != 0 {
			read, err = code.ReadOperandsInto(def, current.instructions[ip+1:], operands[:len(def.OperandWidths)])
			if err != nil {
				return fmt.Errorf("at offset %d: %w", ip, err)
			}
		}
		nextIP := ip + 1 + read

		switch op {
		case code.OpConstant:
			index := operands[0]
			if index < 0 || index >= len(v.bytecode.Constants) {
				return fmt.Errorf("constant index %d out of range at %d", index, ip)
			}
			constant := v.bytecode.Constants[index]
			if constant.Kind() == value.KindInvalid {
				return fmt.Errorf("invalid constant at index %d at %d", index, ip)
			}
			if err := v.push(constant); err != nil {
				return fmt.Errorf("OpConstant at %d: %w", ip, err)
			}

		case code.OpNil:
			if err := v.push(value.NewNil()); err != nil {
				return fmt.Errorf("OpNil at %d: %w", ip, err)
			}

		case code.OpChant:
			argument, err := v.pop("OpChant", ip)
			if err != nil {
				return err
			}
			if err := value.WriteLine(v.output, argument); err != nil {
				return fmt.Errorf("OpChant at offset %d: %w", ip, err)
			}

		case code.OpAdd, code.OpSub, code.OpMul, code.OpDiv:
			if err := v.executeArithmetic(op, def.Name, ip); err != nil {
				return err
			}

		case code.OpNeg:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpNeg at %d", ip)
			}
			operand := v.stack[len(v.stack)-1]
			switch operand.Kind() {
			case value.KindInt, value.KindInt8, value.KindInt16, value.KindInt32:
				result, err := negateInteger(operand)
				if err != nil {
					return fmt.Errorf("OpNeg at %d: %w", ip, err)
				}
				v.stack[len(v.stack)-1] = result
			case value.KindFloat:
				number, _ := operand.Float64()
				v.stack[len(v.stack)-1] = value.NewFloat(-number)
			case value.KindFloat32:
				number, _ := operand.Float32()
				v.stack[len(v.stack)-1] = value.NewFloat32(-number)
			default:
				return fmt.Errorf("OpNeg requires int64 or float64, got %s at %d", operand.Kind(), ip)
			}

		case code.OpIntToFloat:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpIntToFloat at %d", ip)
			}
			integer, ok := integerAsFloat(v.stack[len(v.stack)-1])
			if !ok {
				return fmt.Errorf("OpIntToFloat requires int64 or another integer, got %s at %d", v.stack[len(v.stack)-1].Kind(), ip)
			}
			v.stack[len(v.stack)-1] = value.NewFloat(integer)

		case code.OpFloatToInt:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpFloatToInt at %d", ip)
			}
			floating, ok := v.stack[len(v.stack)-1].Float64()
			if !ok {
				return fmt.Errorf("OpFloatToInt requires float64, got %s at %d", v.stack[len(v.stack)-1].Kind(), ip)
			}
			limit := math.Ldexp(1, 63)
			if math.IsNaN(floating) || math.IsInf(floating, 0) || floating < -limit || floating >= limit {
				return fmt.Errorf("OpFloatToInt value %g is outside int64 range at %d", floating, ip)
			}
			v.stack[len(v.stack)-1] = value.NewInt(int64(math.Trunc(floating)))

		case code.OpIntToFloat32:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpIntToFloat32 at %d", ip)
			}
			integer, ok := integerAsFloat(v.stack[len(v.stack)-1])
			if !ok {
				return fmt.Errorf("OpIntToFloat32 requires an integer at %d", ip)
			}
			v.stack[len(v.stack)-1] = value.NewFloat32(float32(integer))

		case code.OpIntegerConvert:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpIntegerConvert at %d", ip)
			}
			converted, err := convertInteger(v.stack[len(v.stack)-1], operands[0])
			if err != nil {
				return fmt.Errorf("OpIntegerConvert at %d: %w", ip, err)
			}
			v.stack[len(v.stack)-1] = converted

		case code.OpFloat32ToFloat64:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpFloat32ToFloat64 at %d", ip)
			}
			floating, ok := v.stack[len(v.stack)-1].Float32()
			if !ok {
				return fmt.Errorf("OpFloat32ToFloat64 requires float32 at %d", ip)
			}
			v.stack[len(v.stack)-1] = value.NewFloat(float64(floating))

		case code.OpFloat64ToFloat32:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpFloat64ToFloat32 at %d", ip)
			}
			floating, ok := v.stack[len(v.stack)-1].Float64()
			if !ok {
				return fmt.Errorf("OpFloat64ToFloat32 requires float64 at %d", ip)
			}
			v.stack[len(v.stack)-1] = value.NewFloat32(float32(floating))

		case code.OpFloat32ToInt:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpFloat32ToInt at %d", ip)
			}
			floating32, ok := v.stack[len(v.stack)-1].Float32()
			if !ok {
				return fmt.Errorf("OpFloat32ToInt requires float32 at %d", ip)
			}
			floating := float64(floating32)
			limit := math.Ldexp(1, 63)
			if math.IsNaN(floating) || math.IsInf(floating, 0) || floating < -limit || floating >= limit {
				return fmt.Errorf("OpFloat32ToInt value %g is outside int64 range at %d", floating32, ip)
			}
			v.stack[len(v.stack)-1] = value.NewInt(int64(math.Trunc(floating)))

		case code.OpMath:
			if err := v.executeMathPrimitive(operands[0], operands[1], ip); err != nil {
				return err
			}

		case code.OpArray:
			count := operands[0]
			if len(v.stack)-v.operandBase() < count {
				return fmt.Errorf("stack underflow for OpArray at %d", ip)
			}
			start := len(v.stack) - count
			array := value.NewArray(v.stack[start:])
			if count >= arrayStackReleaseThreshold {
				remaining := append([]value.Value(nil), v.stack[:start]...)
				clear(v.stack)
				v.stack = remaining
			} else {
				clear(v.stack[start:])
				v.stack = v.stack[:start]
			}
			if err := v.push(array); err != nil {
				return fmt.Errorf("OpArray at %d: %w", ip, err)
			}

		case code.OpArrayTyped:
			count := operands[0]
			if len(v.stack)-v.operandBase() < count {
				return fmt.Errorf("stack underflow for OpArrayTyped at %d", ip)
			}
			start := len(v.stack) - count
			elementKind, nullable, valid := arrayDescriptor(operands[1], true)
			if !valid {
				return fmt.Errorf("OpArrayTyped has invalid element descriptor %d at %d", operands[1], ip)
			}
			for _, element := range v.stack[start:] {
				if element.IsNil() {
					if !nullable && elementKind != value.KindInvalid {
						return fmt.Errorf("OpArrayTyped cannot store nil at %d", ip)
					}
				} else if elementKind != value.KindInvalid && element.Kind() != elementKind {
					return fmt.Errorf("OpArrayTyped requires %s value, got %s at %d", elementKind, element.Kind(), ip)
				}
			}
			array := value.NewTypedArray(v.stack[start:], elementKind, nullable)
			clear(v.stack[start:])
			v.stack = v.stack[:start]
			if err := v.push(array); err != nil {
				return fmt.Errorf("OpArrayTyped at %d: %w", ip, err)
			}

		case code.OpArrayMake:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpArrayMake at %d", ip)
			}
			lengthValue := v.stack[len(v.stack)-2]
			initial := v.stack[len(v.stack)-1]
			length, ok := lengthValue.Int64()
			if !ok {
				return fmt.Errorf("OpArrayMake requires int64 length at %d", ip)
			}
			elementKind, nullable, valid := arrayDescriptor(operands[0], true)
			if !valid {
				return fmt.Errorf("OpArrayMake has invalid element descriptor %d at %d", operands[0], ip)
			}
			array, err := value.NewFilledArray(length, initial, elementKind, nullable)
			if err != nil {
				return fmt.Errorf("OpArrayMake at %d: %w", ip, err)
			}
			v.stack = v.stack[:len(v.stack)-2]
			if err := v.push(array); err != nil {
				return fmt.Errorf("OpArrayMake at %d: %w", ip, err)
			}

		case code.OpArrayWithCapacity:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpArrayWithCapacity at %d", ip)
			}
			capacity, ok := v.stack[len(v.stack)-1].Int64()
			if !ok {
				return fmt.Errorf("OpArrayWithCapacity requires int64 capacity at %d", ip)
			}
			elementKind, nullable, valid := arrayDescriptor(operands[0], true)
			if !valid {
				return fmt.Errorf("OpArrayWithCapacity has invalid element descriptor %d at %d", operands[0], ip)
			}
			array, err := value.NewArrayWithCapacity(capacity, elementKind, nullable)
			if err != nil {
				return fmt.Errorf("OpArrayWithCapacity at %d: %w", ip, err)
			}
			v.stack[len(v.stack)-1] = array

		case code.OpArrayPush:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpArrayPush at %d", ip)
			}
			array := v.stack[len(v.stack)-2]
			element := v.stack[len(v.stack)-1]
			if err := array.AppendArrayElement(element); err != nil {
				return fmt.Errorf("OpArrayPush at %d: %w", ip, err)
			}
			v.stack = v.stack[:len(v.stack)-2]

		case code.OpArrayCapacity:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpArrayCapacity at %d", ip)
			}
			capacity, ok := v.stack[len(v.stack)-1].ArrayCapacity()
			if !ok {
				return fmt.Errorf("OpArrayCapacity requires array at %d", ip)
			}
			v.stack[len(v.stack)-1] = value.NewInt(int64(capacity))

		case code.OpArrayReserve:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpArrayReserve at %d", ip)
			}
			array := v.stack[len(v.stack)-2]
			additional, ok := v.stack[len(v.stack)-1].Int64()
			if !ok {
				return fmt.Errorf("OpArrayReserve requires int64 additional capacity at %d", ip)
			}
			if err := array.ReserveArray(additional); err != nil {
				return fmt.Errorf("OpArrayReserve at %d: %w", ip, err)
			}
			v.stack = v.stack[:len(v.stack)-2]

		case code.OpArrayClone:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpArrayClone at %d", ip)
			}
			clone, err := v.stack[len(v.stack)-1].CloneArray()
			if err != nil {
				return fmt.Errorf("OpArrayClone at %d: %w", ip, err)
			}
			v.stack[len(v.stack)-1] = clone

		case code.OpArrayTruncate:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpArrayTruncate at %d", ip)
			}
			array := v.stack[len(v.stack)-2]
			newLength, ok := v.stack[len(v.stack)-1].Int64()
			if !ok {
				return fmt.Errorf("OpArrayTruncate requires int64 length at %d", ip)
			}
			if err := array.TruncateArray(newLength); err != nil {
				return fmt.Errorf("OpArrayTruncate at %d: %w", ip, err)
			}
			v.stack = v.stack[:len(v.stack)-2]

		case code.OpArrayAppendAll:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpArrayAppendAll at %d", ip)
			}
			destination := v.stack[len(v.stack)-2]
			source := v.stack[len(v.stack)-1]
			if err := destination.AppendArray(source); err != nil {
				return fmt.Errorf("OpArrayAppendAll at %d: %w", ip, err)
			}
			v.stack = v.stack[:len(v.stack)-2]

		case code.OpStruct:
			count := operands[0]
			if len(v.stack)-v.operandBase() < count {
				return fmt.Errorf("stack underflow for OpStruct at %d", ip)
			}
			start := len(v.stack) - count
			structure := value.NewStruct("", v.stack[start:])
			clear(v.stack[start:])
			v.stack = v.stack[:start]
			if err := v.push(structure); err != nil {
				return fmt.Errorf("OpStruct at %d: %w", ip, err)
			}

		case code.OpGetField:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpGetField at %d", ip)
			}
			field, ok := v.stack[len(v.stack)-1].StructField(operands[0])
			if !ok {
				return fmt.Errorf("invalid struct field %d at %d", operands[0], ip)
			}
			v.stack[len(v.stack)-1] = field

		case code.OpEnum:
			count := operands[1]
			if len(v.stack)-v.operandBase() < count {
				return fmt.Errorf("stack underflow for OpEnum at %d", ip)
			}
			start := len(v.stack) - count
			enum := value.NewEnum("", operands[0], v.stack[start:])
			clear(v.stack[start:])
			v.stack = v.stack[:start]
			if err := v.push(enum); err != nil {
				return fmt.Errorf("OpEnum at %d: %w", ip, err)
			}

		case code.OpEnumTag:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpEnumTag at %d", ip)
			}
			tag, ok := v.stack[len(v.stack)-1].EnumTag()
			if !ok {
				return fmt.Errorf("OpEnumTag requires enum, got %s at %d", v.stack[len(v.stack)-1].Kind(), ip)
			}
			v.stack[len(v.stack)-1] = value.NewInt(int64(tag))

		case code.OpGetEnumValue:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpGetEnumValue at %d", ip)
			}
			associated, ok := v.stack[len(v.stack)-1].EnumValue(operands[0])
			if !ok {
				return fmt.Errorf("invalid enum value %d at %d", operands[0], ip)
			}
			v.stack[len(v.stack)-1] = associated

		case code.OpMap:
			if err := v.push(value.NewMap()); err != nil {
				return fmt.Errorf("OpMap at %d: %w", ip, err)
			}

		case code.OpMapGet:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpMapGet at %d", ip)
			}
			key := v.stack[len(v.stack)-1]
			mapping := v.stack[len(v.stack)-2]
			item, found, valid := mapping.MapGet(key)
			if !valid {
				return fmt.Errorf("OpMapGet requires Map and hashable key at %d", ip)
			}
			v.stack = v.stack[:len(v.stack)-2]
			if !found {
				item = value.NewNil()
			}
			if err := v.push(item); err != nil {
				return fmt.Errorf("OpMapGet at %d: %w", ip, err)
			}

		case code.OpMapSet:
			if len(v.stack)-v.operandBase() < 3 {
				return fmt.Errorf("stack underflow for OpMapSet at %d", ip)
			}
			item := v.stack[len(v.stack)-1]
			key := v.stack[len(v.stack)-2]
			mapping := v.stack[len(v.stack)-3]
			if !mapping.MapSet(key, item) {
				return fmt.Errorf("OpMapSet requires Map and hashable key at %d", ip)
			}
			v.stack = v.stack[:len(v.stack)-3]

		case code.OpIndex:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for OpIndex at %d", ip)
			}
			indexValue := v.stack[len(v.stack)-1]
			arrayValue := v.stack[len(v.stack)-2]
			index, ok := indexValue.Int64()
			if !ok {
				return fmt.Errorf("OpIndex requires int64 index, got %s at %d", indexValue.Kind(), ip)
			}
			length, ok := arrayValue.ArrayLen()
			if !ok {
				return fmt.Errorf("OpIndex requires array, got %s at %d", arrayValue.Kind(), ip)
			}
			if index < 0 || index >= int64(length) {
				return fmt.Errorf("array index %d out of bounds for length %d at %d", index, length, ip)
			}
			element, _ := arrayValue.ArrayElement(index)
			v.stack = v.stack[:len(v.stack)-2]
			if err := v.push(element); err != nil {
				return fmt.Errorf("OpIndex at %d: %w", ip, err)
			}

		case code.OpLen:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpLen at %d", ip)
			}
			arrayValue := v.stack[len(v.stack)-1]
			length, ok := arrayValue.ArrayLen()
			if !ok {
				return fmt.Errorf("OpLen requires array, got %s at %d", arrayValue.Kind(), ip)
			}
			v.stack[len(v.stack)-1] = value.NewInt(int64(length))

		case code.OpSetIndex, code.OpSetNullableIndex:
			name := def.Name
			if len(v.stack)-v.operandBase() < 3 {
				return fmt.Errorf("stack underflow for %s at %d", name, ip)
			}
			element := v.stack[len(v.stack)-1]
			indexValue := v.stack[len(v.stack)-2]
			arrayValue := v.stack[len(v.stack)-3]
			index, ok := indexValue.Int64()
			if !ok {
				return fmt.Errorf("%s requires int64 index, got %s at %d", name, indexValue.Kind(), ip)
			}
			length, ok := arrayValue.ArrayLen()
			if !ok {
				return fmt.Errorf("%s requires array, got %s at %d", name, arrayValue.Kind(), ip)
			}
			if index < 0 || index >= int64(length) {
				return fmt.Errorf("array index %d out of bounds for length %d at %d", index, length, ip)
			}
			currentElement, ok := arrayValue.ArrayElement(index)
			if !ok {
				return fmt.Errorf("array index %d out of bounds for length %d at %d", index, length, ip)
			}
			if op == code.OpSetIndex && currentElement.Kind() != element.Kind() {
				return fmt.Errorf("%s requires %s value, got %s at %d", name, currentElement.Kind(), element.Kind(), ip)
			}
			if op == code.OpSetNullableIndex && !currentElement.IsNil() && !element.IsNil() && currentElement.Kind() != element.Kind() {
				return fmt.Errorf("%s requires %s or nil value, got %s at %d", name, currentElement.Kind(), element.Kind(), ip)
			}
			var updated bool
			if op == code.OpSetNullableIndex {
				updated = arrayValue.SetNullableArrayElement(index, element)
			} else {
				updated = arrayValue.SetArrayElement(index, element)
			}
			if !updated {
				return fmt.Errorf("array index %d out of bounds for length %d at %d", index, length, ip)
			}
			v.stack = v.stack[:len(v.stack)-3]

		case code.OpPop:
			popped, err := v.pop("OpPop", ip)
			if err != nil {
				return err
			}
			if current.functionID < 0 {
				v.lastPopped = popped
				v.hasLastPopped = true
			}

		case code.OpTrue:
			if err := v.push(value.NewBool(true)); err != nil {
				return fmt.Errorf("OpTrue at %d: %w", ip, err)
			}

		case code.OpFalse:
			if err := v.push(value.NewBool(false)); err != nil {
				return fmt.Errorf("OpFalse at %d: %w", ip, err)
			}

		case code.OpNot:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpNot at %d", ip)
			}
			boolean, ok := v.stack[len(v.stack)-1].Bool()
			if !ok {
				return fmt.Errorf("OpNot requires bool, got %s at %d", v.stack[len(v.stack)-1].Kind(), ip)
			}
			v.stack[len(v.stack)-1] = value.NewBool(!boolean)

		case code.OpIsNil:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpIsNil at %d", ip)
			}
			v.stack[len(v.stack)-1] = value.NewBool(v.stack[len(v.stack)-1].IsNil())

		case code.OpEqual, code.OpNotEqual, code.OpLess, code.OpLessEqual, code.OpGreater, code.OpGreaterEqual:
			if len(v.stack)-v.operandBase() < 2 {
				return fmt.Errorf("stack underflow for %s at %d", def.Name, ip)
			}
			if err := v.executeComparison(op, def.Name); err != nil {
				return fmt.Errorf("at offset %d: %w", ip, err)
			}

		case code.OpJump:
			target := operands[0]
			if target < 0 || target > len(current.instructions) {
				return fmt.Errorf("jump target %d out of range at %d", target, ip)
			}
			current.ip = target
			continue

		case code.OpJumpIfFalse:
			target := operands[0]
			if target < 0 || target > len(current.instructions) {
				return fmt.Errorf("jump target %d out of range at %d", target, ip)
			}
			conditionValue, err := v.pop("OpJumpIfFalse", ip)
			if err != nil {
				return err
			}
			condition, ok := conditionValue.Bool()
			if !ok {
				return fmt.Errorf("OpJumpIfFalse requires bool, got %s at %d", conditionValue.Kind(), ip)
			}
			if !condition {
				current.ip = target
				continue
			}

		case code.OpSetGlobal:
			if err := v.setGlobal(operands[0], ip); err != nil {
				return err
			}

		case code.OpGetGlobal:
			if err := v.getGlobal(operands[0], ip); err != nil {
				return err
			}

		case code.OpSetLocal, code.OpDefineLocal:
			if current.functionID < 0 {
				return fmt.Errorf("OpSetLocal outside function at %d", ip)
			}
			if err := v.setLocal(current, operands[0], ip, op == code.OpDefineLocal); err != nil {
				return err
			}

		case code.OpGetLocal:
			if current.functionID < 0 {
				return fmt.Errorf("OpGetLocal outside function at %d", ip)
			}
			if err := v.getLocal(current, operands[0], ip); err != nil {
				return err
			}

		case code.OpCall:
			current.ip = nextIP
			if err := v.call(operands[0], ip); err != nil {
				return err
			}
			continue

		case code.OpFunction:
			if operands[0] < 0 || operands[0] >= len(v.bytecode.Functions) {
				return fmt.Errorf("FunctionID %d out of range at %d", operands[0], ip)
			}
			if err := v.push(value.NewFunction(operands[0])); err != nil {
				return fmt.Errorf("OpFunction at %d: %w", ip, err)
			}

		case code.OpClosure:
			functionID := operands[0]
			if functionID < 0 || functionID >= len(v.bytecode.Functions) {
				return fmt.Errorf("FunctionID %d out of range at %d", functionID, ip)
			}
			function := v.bytecode.Functions[functionID]
			cells := make([]*value.Cell, len(function.Captures))
			for i, capture := range function.Captures {
				if capture.Local {
					if current.functionID < 0 {
						return fmt.Errorf("local capture outside function at %d", ip)
					}
					cell, err := v.captureLocal(current, capture.Index, ip)
					if err != nil {
						return err
					}
					cells[i] = cell
				} else {
					if capture.Index < 0 || capture.Index >= len(current.upvalues) {
						return fmt.Errorf("upvalue index %d out of range at %d", capture.Index, ip)
					}
					cells[i] = current.upvalues[capture.Index]
				}
			}
			if err := v.push(value.NewClosure(functionID, cells)); err != nil {
				return fmt.Errorf("OpClosure at %d: %w", ip, err)
			}

		case code.OpGetUpvalue:
			if operands[0] < 0 || operands[0] >= len(current.upvalues) || current.upvalues[operands[0]] == nil {
				return fmt.Errorf("upvalue index %d out of range at %d", operands[0], ip)
			}
			if err := v.push(current.upvalues[operands[0]].Value); err != nil {
				return fmt.Errorf("OpGetUpvalue at %d: %w", ip, err)
			}

		case code.OpSetUpvalue:
			if operands[0] < 0 || operands[0] >= len(current.upvalues) || current.upvalues[operands[0]] == nil {
				return fmt.Errorf("upvalue index %d out of range at %d", operands[0], ip)
			}
			item, err := v.pop("OpSetUpvalue", ip)
			if err != nil {
				return err
			}
			current.upvalues[operands[0]].Value = item

		case code.OpCallValue:
			argumentCount := operands[0]
			functionPosition := len(v.stack) - argumentCount - 1
			if functionPosition < v.operandBase() {
				return fmt.Errorf("stack underflow for OpCallValue at %d", ip)
			}
			functionID, ok := v.stack[functionPosition].FunctionID()
			if !ok {
				return fmt.Errorf("OpCallValue requires function value at %d", ip)
			}
			if functionID < 0 || functionID >= len(v.bytecode.Functions) || v.bytecode.Functions[functionID].ParameterCount != argumentCount {
				return fmt.Errorf("invalid function value call at %d", ip)
			}
			cells, _ := v.stack[functionPosition].ClosureCells()
			copy(v.stack[functionPosition:], v.stack[functionPosition+1:])
			v.stack = v.stack[:len(v.stack)-1]
			current.ip = nextIP
			if err := v.callClosure(functionID, cells, ip); err != nil {
				return err
			}
			continue

		case code.OpReturn:
			if current.functionID < 0 {
				return fmt.Errorf("OpReturn outside function at %d", ip)
			}
			function := v.bytecode.Functions[current.functionID]
			if function.ReturnsValue {
				return fmt.Errorf("function %d returned without a value at %d", current.functionID, ip)
			}
			if len(v.stack) != v.operandBase() {
				return fmt.Errorf("unit function %d returned with %d value stack entries at %d", current.functionID, len(v.stack)-v.operandBase(), ip)
			}
			v.stack = v.stack[:current.base]
			v.frames = v.frames[:frameIndex]
			continue

		case code.OpReturnValue:
			if current.functionID < 0 {
				return fmt.Errorf("OpReturnValue outside function at %d", ip)
			}
			function := v.bytecode.Functions[current.functionID]
			if !function.ReturnsValue {
				return fmt.Errorf("unit function %d returned a value at %d", current.functionID, ip)
			}
			minimum := current.base + function.LocalCount
			if len(v.stack) < minimum+1 {
				return fmt.Errorf("stack underflow for OpReturnValue at %d", ip)
			}
			if len(v.stack) != minimum+1 {
				return fmt.Errorf("function %d returned with %d result values at %d", current.functionID, len(v.stack)-minimum, ip)
			}
			result := v.stack[len(v.stack)-1]
			v.stack = v.stack[:current.base]
			v.frames = v.frames[:frameIndex]
			if err := v.push(result); err != nil {
				return fmt.Errorf("OpReturnValue at %d: %w", ip, err)
			}
			continue

		default:
			return fmt.Errorf("unsupported opcode %s at %d", def.Name, ip)
		}

		v.frames[frameIndex].ip = nextIP
	}

	return nil
}

func arrayDescriptor(descriptor int, allowDynamic bool) (value.Kind, bool, bool) {
	kind := value.Kind(descriptor & 0x7f)
	return kind, descriptor&0x80 != 0, validArrayElementKind(kind) || allowDynamic && kind == value.KindInvalid
}

func validArrayElementKind(kind value.Kind) bool {
	switch kind {
	case value.KindInt, value.KindFloat, value.KindFloat32, value.KindBool, value.KindString,
		value.KindArray, value.KindStruct, value.KindEnum, value.KindMap, value.KindFunction,
		value.KindInt8, value.KindInt16, value.KindInt32, value.KindUint8, value.KindUint16, value.KindUint32:
		return true
	default:
		return false
	}
}

// executeMathPrimitive 执行共享数学原语入口
func (v *VM) executeMathPrimitive(primitive, argumentCount, ip int) error {
	if argumentCount < 1 || len(v.stack)-v.operandBase() < argumentCount {
		return fmt.Errorf("stack underflow for OpMath at %d", ip)
	}
	start := len(v.stack) - argumentCount
	arguments := v.stack[start:]
	var output value.Value
	if primitive == code.MathError {
		if argumentCount != 1 {
			return fmt.Errorf("invalid argument count for math error primitive at %d", ip)
		}
		message, ok := arguments[0].Text()
		if !ok {
			return fmt.Errorf("math error primitive requires string at %d", ip)
		}
		return fmt.Errorf("%s", message)
	} else if primitive == code.MathAddOverflows {
		if argumentCount != 2 {
			return fmt.Errorf("invalid argument count for integer overflow primitive at %d", ip)
		}
		left, leftOK := arguments[0].Int64()
		right, rightOK := arguments[1].Int64()
		if !leftOK || !rightOK {
			return fmt.Errorf("integer overflow primitive requires int64 at %d", ip)
		}
		sum := uint64(left) + uint64(right)
		result := int64(sum)
		overflow := (right > 0 && result < left) || (right < 0 && result > left)
		output = value.NewBool(overflow)
	} else if primitive == code.MathIsNegative || primitive == code.MathNegOverflows {
		if argumentCount != 1 {
			return fmt.Errorf("invalid argument count for sign primitive at %d", ip)
		}
		input := arguments[0]
		if integer, ok := input.SignedInteger(); ok {
			if primitive == code.MathNegOverflows {
				output = value.NewBool(integer == signedMinimum(input.Kind()))
			} else {
				output = value.NewBool(integer < 0)
			}
		} else if input.Kind().IsUnsignedInteger() {
			output = value.NewBool(false)
		} else {
			floating, is32 := floatValue(input)
			if input.Kind() != value.KindFloat && !is32 {
				return fmt.Errorf("sign primitive requires a number at %d", ip)
			}
			if primitive == code.MathNegOverflows {
				output = value.NewBool(false)
			} else {
				output = value.NewBool(math.Signbit(floating))
			}
		}
	} else {
		if argumentCount != 1 {
			return fmt.Errorf("invalid argument count for floating primitive at %d", ip)
		}
		input := arguments[0]
		floating, is32 := floatValue(input)
		if input.Kind() != value.KindFloat && !is32 {
			return fmt.Errorf("floating primitive requires float32 or float64 at %d", ip)
		}
		switch primitive {
		case code.MathSqrt:
			output = roundedFloat(math.Sqrt(floating), is32)
		case code.MathFloor:
			output = roundedFloat(math.Floor(floating), is32)
		case code.MathCeil:
			output = roundedFloat(math.Ceil(floating), is32)
		case code.MathRoundEven:
			output = roundedFloat(math.RoundToEven(floating), is32)
		case code.MathTrunc:
			output = roundedFloat(math.Trunc(floating), is32)
		case code.MathIsNaN:
			output = value.NewBool(math.IsNaN(floating))
		case code.MathIsInf:
			output = value.NewBool(math.IsInf(floating, 0))
		case code.MathIsFinite:
			output = value.NewBool(!math.IsNaN(floating) && !math.IsInf(floating, 0))
		case code.MathIsNegativeZero:
			output = value.NewBool(floating == 0 && math.Signbit(floating))
		default:
			return fmt.Errorf("unknown math primitive %d at %d", primitive, ip)
		}
	}
	v.stack = v.stack[:start]
	return v.push(output)
}

func floatValue(input value.Value) (float64, bool) {
	if floating, ok := input.Float32(); ok {
		return float64(floating), true
	}
	floating, _ := input.Float64()
	return floating, false
}

func roundedFloat(floating float64, asFloat32 bool) value.Value {
	if asFloat32 {
		return value.NewFloat32(float32(floating))
	}
	return value.NewFloat(floating)
}

func (v *VM) wrapRuntimeError(cause error) *RuntimeError {
	stack := make([]RuntimeFrame, 0, len(v.frames))
	for index := len(v.frames) - 1; index >= 0; index-- {
		current := v.frames[index]
		name := "<main>"
		if current.functionID >= 0 && current.functionID < len(v.bytecode.Functions) {
			name = nativeFunctionName(v.bytecode.Functions[current.functionID].Name)
		}
		stack = append(stack, RuntimeFrame{Function: name, Offset: current.ip})
	}
	return &RuntimeError{Cause: cause, Stack: stack}
}

func (v *VM) resetExecutionState() {
	v.stack = v.stack[:0]
	v.frames = v.frames[:0]
	clear(v.globals)
	v.globals = v.globals[:0]
	v.lastPopped = value.Value{}
	v.hasLastPopped = false
}

func validateInstructionBoundaries(instructions code.Instructions) error {
	boundaries := make([]bool, len(instructions)+1)
	boundaries[len(instructions)] = true
	type jumpReference struct {
		offset int
		target int
	}
	var jumps []jumpReference
	var operands [2]int

	for offset := 0; offset < len(instructions); {
		boundaries[offset] = true
		op := code.Opcode(instructions[offset])
		definition, err := code.Lookup(op)
		if err != nil {
			return fmt.Errorf("at offset %d: %w", offset, err)
		}
		read, err := code.ReadOperandsInto(definition, instructions[offset+1:], operands[:len(definition.OperandWidths)])
		if err != nil {
			return fmt.Errorf("at offset %d: %w", offset, err)
		}
		if op == code.OpJump || op == code.OpJumpIfFalse {
			jumps = append(jumps, jumpReference{offset: offset, target: operands[0]})
		}
		offset += 1 + read
	}

	for _, jump := range jumps {
		if jump.target < 0 || jump.target >= len(boundaries) {
			return fmt.Errorf("jump target %d out of range at %d", jump.target, jump.offset)
		}
		if !boundaries[jump.target] {
			return fmt.Errorf("jump target %d is not an instruction boundary at %d", jump.target, jump.offset)
		}
	}
	return nil
}

func (v *VM) push(item value.Value) error {
	if len(v.stack) >= MaxValueStack {
		return fmt.Errorf("value stack limit %d exceeded", MaxValueStack)
	}
	v.stack = append(v.stack, item)
	return nil
}

func (v *VM) pop(name string, ip int) (value.Value, error) {
	if len(v.stack) <= v.operandBase() {
		return value.Value{}, fmt.Errorf("stack underflow for %s at %d", name, ip)
	}
	item := v.stack[len(v.stack)-1]
	v.stack = v.stack[:len(v.stack)-1]
	return item, nil
}

func (v *VM) executeArithmetic(op code.Opcode, name string, ip int) error {
	if len(v.stack)-v.operandBase() < 2 {
		return fmt.Errorf("stack underflow for %s at %d", name, ip)
	}
	rightValue := v.stack[len(v.stack)-1]
	leftValue := v.stack[len(v.stack)-2]
	if leftValue.Kind() != rightValue.Kind() {
		return fmt.Errorf("%s requires int64 operands, matching float32 operands, or matching float64 operands, got %s and %s at %d", name, leftValue.Kind(), rightValue.Kind(), ip)
	}

	if leftValue.Kind() == value.KindFloat {
		right, _ := rightValue.Float64()
		left, _ := leftValue.Float64()
		var result float64
		switch op {
		case code.OpAdd:
			result = left + right
		case code.OpSub:
			result = left - right
		case code.OpMul:
			result = left * right
		case code.OpDiv:
			result = left / right
		}
		v.stack = v.stack[:len(v.stack)-2]
		return v.push(value.NewFloat(result))
	}
	if leftValue.Kind() == value.KindFloat32 {
		right, _ := rightValue.Float32()
		left, _ := leftValue.Float32()
		var result float32
		switch op {
		case code.OpAdd:
			result = left + right
		case code.OpSub:
			result = left - right
		case code.OpMul:
			result = left * right
		case code.OpDiv:
			result = left / right
		}
		v.stack = v.stack[:len(v.stack)-2]
		return v.push(value.NewFloat32(result))
	}

	if !leftValue.Kind().IsInteger() {
		return fmt.Errorf("%s requires int64 operands or matching numeric operands, got %s at %d", name, leftValue.Kind(), ip)
	}
	result, err := arithmeticInteger(op, leftValue, rightValue)
	if err != nil {
		return fmt.Errorf("%s at %d: %w", name, ip, err)
	}
	v.stack = v.stack[:len(v.stack)-2]
	return v.push(result)
}

func integerAsFloat(input value.Value) (float64, bool) {
	if signed, ok := input.SignedInteger(); ok {
		return float64(signed), true
	}
	if unsigned, ok := input.UnsignedInteger(); ok {
		return float64(unsigned), true
	}
	return 0, false
}

func integerKindForCode(target int) (value.Kind, bool) {
	switch target {
	case code.IntegerInt8:
		return value.KindInt8, true
	case code.IntegerInt16:
		return value.KindInt16, true
	case code.IntegerInt32:
		return value.KindInt32, true
	case code.IntegerInt64:
		return value.KindInt, true
	case code.IntegerUint8:
		return value.KindUint8, true
	case code.IntegerUint16:
		return value.KindUint16, true
	case code.IntegerUint32:
		return value.KindUint32, true
	default:
		return value.KindInvalid, false
	}
}

func integerWidth(kind value.Kind) uint {
	switch kind {
	case value.KindInt8, value.KindUint8:
		return 8
	case value.KindInt16, value.KindUint16:
		return 16
	case value.KindInt32, value.KindUint32:
		return 32
	default:
		return 64
	}
}

func integerMask(kind value.Kind) uint64 {
	width := integerWidth(kind)
	if width == 64 {
		return ^uint64(0)
	}
	return uint64(1)<<width - 1
}

func signedMinimum(kind value.Kind) int64 {
	width := integerWidth(kind)
	if width == 64 {
		return math.MinInt64
	}
	return -int64(uint64(1) << (width - 1))
}

func signedMaximum(kind value.Kind) int64 {
	width := integerWidth(kind)
	if width == 64 {
		return math.MaxInt64
	}
	return int64(uint64(1)<<(width-1)) - 1
}

func integerFromBits(kind value.Kind, bits uint64) value.Value {
	result, _ := value.NewIntegerBits(kind, bits&integerMask(kind))
	return result
}

func arithmeticInteger(op code.Opcode, leftValue, rightValue value.Value) (value.Value, error) {
	kind := leftValue.Kind()
	if kind.IsUnsignedInteger() {
		left, _ := leftValue.UnsignedInteger()
		right, _ := rightValue.UnsignedInteger()
		var result uint64
		switch op {
		case code.OpAdd:
			result = left + right
		case code.OpSub:
			result = left - right
		case code.OpMul:
			result = left * right
		case code.OpDiv:
			if right == 0 {
				return value.Value{}, fmt.Errorf("division by zero")
			}
			result = left / right
		}
		return integerFromBits(kind, result), nil
	}
	left, _ := leftValue.SignedInteger()
	right, _ := rightValue.SignedInteger()
	if op == code.OpDiv {
		if right == 0 {
			return value.Value{}, fmt.Errorf("division by zero")
		}
		if left == signedMinimum(kind) && right == -1 {
			return integerFromBits(kind, uint64(left)), nil
		}
		return integerFromBits(kind, uint64(left/right)), nil
	}
	var bits uint64
	switch op {
	case code.OpAdd:
		bits = uint64(left) + uint64(right)
	case code.OpSub:
		bits = uint64(left) - uint64(right)
	case code.OpMul:
		bits = uint64(left) * uint64(right)
	}
	return integerFromBits(kind, bits), nil
}

func negateInteger(input value.Value) (value.Value, error) {
	number, ok := input.SignedInteger()
	if !ok {
		return value.Value{}, fmt.Errorf("requires a signed integer, got %s", input.Kind())
	}
	return integerFromBits(input.Kind(), ^uint64(number)+1), nil
}

func convertInteger(input value.Value, targetCode int) (value.Value, error) {
	target, ok := integerKindForCode(targetCode)
	if !ok {
		return value.Value{}, fmt.Errorf("unknown integer target %d", targetCode)
	}
	var signed int64
	var unsigned uint64
	sourceUnsigned := false
	if number, ok := input.SignedInteger(); ok {
		signed = number
	} else if number, ok := input.UnsignedInteger(); ok {
		unsigned = number
		sourceUnsigned = true
	} else {
		var number float64
		if floating, ok := input.Float32(); ok {
			number = float64(floating)
		} else {
			var ok bool
			number, ok = input.Float64()
			if !ok {
				return value.Value{}, fmt.Errorf("requires a numeric value, got %s", input.Kind())
			}
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return value.Value{}, fmt.Errorf("floating value is outside %s range", target)
		}
		number = math.Trunc(number)
		if target.IsUnsignedInteger() {
			maximum := float64(integerMask(target))
			if number < 0 || number > maximum {
				return value.Value{}, fmt.Errorf("floating value is outside %s range", target)
			}
			return integerFromBits(target, uint64(number)), nil
		}
		if number < float64(signedMinimum(target)) || number > float64(signedMaximum(target)) {
			return value.Value{}, fmt.Errorf("floating value is outside %s range", target)
		}
		return integerFromBits(target, uint64(int64(number))), nil
	}
	if target.IsUnsignedInteger() {
		if (!sourceUnsigned && signed < 0) || (sourceUnsigned && unsigned > integerMask(target)) || (!sourceUnsigned && uint64(signed) > integerMask(target)) {
			return value.Value{}, fmt.Errorf("integer value is outside %s range", target)
		}
		if !sourceUnsigned {
			unsigned = uint64(signed)
		}
		return integerFromBits(target, unsigned), nil
	}
	if sourceUnsigned {
		if unsigned > uint64(signedMaximum(target)) {
			return value.Value{}, fmt.Errorf("integer value is outside %s range", target)
		}
		signed = int64(unsigned)
	}
	if signed < signedMinimum(target) || signed > signedMaximum(target) {
		return value.Value{}, fmt.Errorf("integer value is outside %s range", target)
	}
	return integerFromBits(target, uint64(signed)), nil
}

func (v *VM) setGlobal(index int, ip int) error {
	if index < 0 || index >= len(v.globals) {
		return fmt.Errorf("global slot %d out of range at %d", index, ip)
	}
	item, err := v.pop("OpSetGlobal", ip)
	if err != nil {
		return err
	}
	v.globals[index] = item
	return nil
}

func (v *VM) getGlobal(index int, ip int) error {
	if index < 0 || index >= len(v.globals) {
		return fmt.Errorf("global slot %d out of range at %d", index, ip)
	}
	item := v.globals[index]
	if item.Kind() == value.KindInvalid {
		return fmt.Errorf("global slot %d is uninitialized at %d", index, ip)
	}
	return v.push(item)
}

func (v *VM) setLocal(current *frame, index int, ip int, fresh bool) error {
	function := v.bytecode.Functions[current.functionID]
	if index < 0 || index >= function.LocalCount {
		return fmt.Errorf("local slot %d out of range at %d", index, ip)
	}
	item, err := v.pop("OpSetLocal", ip)
	if err != nil {
		return err
	}
	if fresh && current.cells != nil {
		delete(current.cells, index)
	}
	if cell := current.cells[index]; cell != nil {
		cell.Value = item
	} else {
		v.stack[current.base+index] = item
	}
	return nil
}

func (v *VM) getLocal(current *frame, index int, ip int) error {
	function := v.bytecode.Functions[current.functionID]
	if index < 0 || index >= function.LocalCount {
		return fmt.Errorf("local slot %d out of range at %d", index, ip)
	}
	item := v.stack[current.base+index]
	if cell := current.cells[index]; cell != nil {
		item = cell.Value
	}
	if item.Kind() == value.KindInvalid {
		return fmt.Errorf("local slot %d is uninitialized at %d", index, ip)
	}
	return v.push(item)
}

func (v *VM) call(functionID int, ip int) error {
	return v.callClosure(functionID, nil, ip)
}

// callClosure 创建携带捕获环境的调用帧
func (v *VM) callClosure(functionID int, upvalues []*value.Cell, ip int) error {
	if functionID < 0 || functionID >= len(v.bytecode.Functions) {
		return fmt.Errorf("function %d out of range at %d", functionID, ip)
	}
	if len(v.frames)-1 >= MaxCallDepth {
		return fmt.Errorf("call depth limit %d exceeded", MaxCallDepth)
	}
	function := v.bytecode.Functions[functionID]
	if len(v.stack)-v.operandBase() < function.ParameterCount {
		return fmt.Errorf("stack underflow for OpCall at %d", ip)
	}
	base := len(v.stack) - function.ParameterCount
	if functionID < len(v.native) && v.native[functionID].function != nil {
		if len(upvalues) != 0 || len(function.Captures) != 0 {
			return fmt.Errorf("native function %d cannot capture values at %d", functionID, ip)
		}
		v.frames = append(v.frames, frame{functionID: functionID, base: base})
		result, hasResult, err := invokeNative(v.native[functionID], v.stack[base:])
		if err != nil {
			return fmt.Errorf("native function %s failed: %w", nativeFunctionName(function.Name), err)
		}
		v.frames = v.frames[:len(v.frames)-1]
		v.stack = v.stack[:base]
		if hasResult {
			if err := v.push(result); err != nil {
				return fmt.Errorf("native function %s result: %w", nativeFunctionName(function.Name), err)
			}
		}
		return nil
	}
	required := base + function.LocalCount
	if required > MaxValueStack {
		return fmt.Errorf("value stack limit %d exceeded", MaxValueStack)
	}
	for len(v.stack) < required {
		v.stack = append(v.stack, value.Value{})
	}
	if len(upvalues) != len(function.Captures) {
		return fmt.Errorf("function %d requires %d captures, got %d at %d", functionID, len(function.Captures), len(upvalues), ip)
	}
	v.frames = append(v.frames, frame{functionID: functionID, instructions: function.Instructions, base: base, upvalues: upvalues})
	return nil
}

func nativeFunctionName(bindingKey string) string {
	id, _, ok := mma.ParseBindingKey(bindingKey)
	if !ok {
		return bindingKey
	}
	return id.Module + "." + id.Name
}

func (v *VM) bindNativeFunctions() error {
	v.native = make([]nativeBinding, len(v.bytecode.Functions))
	for index, function := range v.bytecode.Functions {
		id, fingerprint, native := mma.ParseBindingKey(function.Name)
		if !native {
			continue
		}
		if len(function.Instructions) != 0 || len(function.Captures) != 0 || function.LocalCount != function.ParameterCount {
			return fmt.Errorf("invalid native function stub %q", function.Name)
		}
		registered, ok := v.registry.Resolve(id)
		if !ok || !registered.Public {
			return fmt.Errorf("missing native binding %q.%s", id.Module, id.Name)
		}
		if mma.SignatureFingerprint(registered.Signature) != fingerprint ||
			len(registered.Signature.Parameters) != function.ParameterCount ||
			(registered.Signature.Return != mma.TypeUnit) != function.ReturnsValue {
			return fmt.Errorf("native binding signature mismatch for %q.%s", id.Module, id.Name)
		}
		v.native[index] = nativeBinding{function: registered.Function, signature: registered.Signature}
	}
	return nil
}

func invokeNative(binding nativeBinding, arguments []value.Value) (result value.Value, hasResult bool, callErr error) {
	converted := make([]mma.Value, len(arguments))
	for index, argument := range arguments {
		item, err := toMMAValue(argument)
		if err != nil {
			return value.Value{}, false, fmt.Errorf("argument %d: %w", index+1, err)
		}
		converted[index] = item
	}
	returned, hasResult, err := mma.Invoke(binding.function, converted, binding.signature)
	if err != nil {
		return value.Value{}, false, err
	}
	if !hasResult {
		return value.Value{}, false, nil
	}
	result, err = fromMMAValue(returned)
	return result, true, err
}

func toMMAValue(input value.Value) (mma.Value, error) {
	switch input.Kind() {
	case value.KindBool:
		item, _ := input.Bool()
		return mma.Values.Bool(item), nil
	case value.KindInt:
		item, _ := input.Int64()
		return mma.Values.Int64(item), nil
	case value.KindInt8:
		item, _ := input.Int8()
		return mma.Values.Int8(item), nil
	case value.KindInt16:
		item, _ := input.Int16()
		return mma.Values.Int16(item), nil
	case value.KindInt32:
		item, _ := input.Int32()
		return mma.Values.Int32(item), nil
	case value.KindUint8:
		item, _ := input.Uint8()
		return mma.Values.Uint8(item), nil
	case value.KindUint16:
		item, _ := input.Uint16()
		return mma.Values.Uint16(item), nil
	case value.KindUint32:
		item, _ := input.Uint32()
		return mma.Values.Uint32(item), nil
	case value.KindFloat:
		item, _ := input.Float64()
		return mma.Values.Float64(item), nil
	case value.KindFloat32:
		item, _ := input.Float32()
		return mma.Values.Float32(item), nil
	case value.KindString:
		item, _ := input.Text()
		return mma.Values.String(item), nil
	default:
		return mma.Value{}, fmt.Errorf("MMA v0.1 does not support %s values", input.Kind())
	}
}

func fromMMAValue(input mma.Value) (value.Value, error) {
	bits, integerErr := mma.Values.IntegerBits(input)
	switch input.Type() {
	case mma.TypeBool:
		item, err := mma.Values.AsBool(input)
		return value.NewBool(item), err
	case mma.TypeInt8:
		return value.NewInt8(int8(bits)), integerErr
	case mma.TypeInt16:
		return value.NewInt16(int16(bits)), integerErr
	case mma.TypeInt32:
		return value.NewInt32(int32(bits)), integerErr
	case mma.TypeInt64:
		return value.NewInt(int64(bits)), integerErr
	case mma.TypeUint8:
		return value.NewUint8(uint8(bits)), integerErr
	case mma.TypeUint16:
		return value.NewUint16(uint16(bits)), integerErr
	case mma.TypeUint32:
		return value.NewUint32(uint32(bits)), integerErr
	case mma.TypeFloat32:
		item, err := mma.Values.AsFloat32(input)
		return value.NewFloat32(item), err
	case mma.TypeFloat64:
		item, err := mma.Values.AsFloat64(input)
		return value.NewFloat(item), err
	case mma.TypeString:
		item, err := mma.Values.AsString(input)
		return value.NewString(item), err
	default:
		return value.Value{}, fmt.Errorf("MMA v0.1 does not support %s results", input.Type())
	}
}

// captureLocal 获取或创建局部变量的共享 Cell
func (v *VM) captureLocal(current *frame, index int, ip int) (*value.Cell, error) {
	function := v.bytecode.Functions[current.functionID]
	if index < 0 || index >= function.LocalCount {
		return nil, fmt.Errorf("local capture index %d out of range at %d", index, ip)
	}
	if current.cells == nil {
		current.cells = make(map[int]*value.Cell)
	}
	if cell := current.cells[index]; cell != nil {
		return cell, nil
	}
	item := v.stack[current.base+index]
	if item.Kind() == value.KindInvalid {
		return nil, fmt.Errorf("local slot %d is uninitialized at %d", index, ip)
	}
	cell := &value.Cell{Value: item}
	current.cells[index] = cell
	return cell, nil
}

func (v *VM) operandBase() int {
	if len(v.frames) == 0 {
		return 0
	}
	current := v.frames[len(v.frames)-1]
	if current.functionID < 0 {
		return 0
	}
	return current.base + v.bytecode.Functions[current.functionID].LocalCount
}

// LastPoppedValue 获取最后弹出的运行时值
func (v *VM) LastPoppedValue() (value.Value, bool) {
	return v.lastPopped, v.hasLastPopped
}

// LastPoppedStackElem 兼容原有的整数结果接口
func (v *VM) LastPoppedStackElem() (int64, bool) {
	if !v.hasLastPopped {
		return 0, false
	}
	return v.lastPopped.Int64()
}
