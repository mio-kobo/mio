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
)

const (
	// MaxCallDepth 限制同时存在的用户函数调用层数
	MaxCallDepth = 1024
	// MaxValueStack 限制共享 Value Stack 的元素数量
	MaxValueStack = 1 << 16
	// DefaultInstructionLimit 限制单次执行的指令数量
	DefaultInstructionLimit uint64 = 10_000_000
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
}

// VM 执行 Mio 字节码
type VM struct {
	bytecode *compiler.Bytecode
	stack    []value.Value
	frames   []frame
	globals  []value.Value
	output   io.Writer

	lastPopped    value.Value
	hasLastPopped bool
}

// New 创建字节码虚拟机
func New(bytecode *compiler.Bytecode) *VM {
	return NewWithOutput(bytecode, os.Stdout)
}

// NewWithOutput 创建使用指定输出流的 VM；nil 输出流在执行 chant 时报告错误。
func NewWithOutput(bytecode *compiler.Bytecode, output io.Writer) *VM {
	return &VM{bytecode: bytecode, stack: make([]value.Value, 0), frames: make([]frame, 0), output: output}
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
	if err := validateInstructionBoundaries(v.bytecode.Instructions); err != nil {
		return fmt.Errorf("main instructions: %w", err)
	}
	for index, function := range v.bytecode.Functions {
		if err := validateInstructionBoundaries(function.Instructions); err != nil {
			return fmt.Errorf("function %d instructions: %w", index, err)
		}
	}

	v.globals = make([]value.Value, v.bytecode.GlobalsCount)
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
		operands, read, err := code.ReadOperands(def, current.instructions[ip+1:])
		if err != nil {
			return fmt.Errorf("at offset %d: %w", ip, err)
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
			case value.KindInt:
				number, _ := operand.Int64()
				v.stack[len(v.stack)-1] = value.NewInt(-number)
			case value.KindFloat:
				number, _ := operand.Float64()
				v.stack[len(v.stack)-1] = value.NewFloat(-number)
			default:
				return fmt.Errorf("OpNeg requires int64 or float64, got %s at %d", operand.Kind(), ip)
			}

		case code.OpIntToFloat:
			if len(v.stack) <= v.operandBase() {
				return fmt.Errorf("stack underflow for OpIntToFloat at %d", ip)
			}
			integer, ok := v.stack[len(v.stack)-1].Int64()
			if !ok {
				return fmt.Errorf("OpIntToFloat requires int64, got %s at %d", v.stack[len(v.stack)-1].Kind(), ip)
			}
			v.stack[len(v.stack)-1] = value.NewFloat(float64(integer))

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

		case code.OpArray:
			count := operands[0]
			if len(v.stack)-v.operandBase() < count {
				return fmt.Errorf("stack underflow for OpArray at %d", ip)
			}
			start := len(v.stack) - count
			elements := append([]value.Value(nil), v.stack[start:]...)
			v.stack = v.stack[:start]
			if err := v.push(value.NewArray(elements)); err != nil {
				return fmt.Errorf("OpArray at %d: %w", ip, err)
			}

		case code.OpStruct:
			count := operands[0]
			if len(v.stack)-v.operandBase() < count {
				return fmt.Errorf("stack underflow for OpStruct at %d", ip)
			}
			start := len(v.stack) - count
			fields := append([]value.Value(nil), v.stack[start:]...)
			v.stack = v.stack[:start]
			if err := v.push(value.NewStruct("", fields)); err != nil {
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
			values := append([]value.Value(nil), v.stack[start:]...)
			v.stack = v.stack[:start]
			if err := v.push(value.NewEnum("", operands[0], values)); err != nil {
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

		case code.OpSetLocal:
			if current.functionID < 0 {
				return fmt.Errorf("OpSetLocal outside function at %d", ip)
			}
			if err := v.setLocal(current, operands[0], ip); err != nil {
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
			copy(v.stack[functionPosition:], v.stack[functionPosition+1:])
			v.stack = v.stack[:len(v.stack)-1]
			current.ip = nextIP
			if err := v.call(functionID, ip); err != nil {
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

func (v *VM) wrapRuntimeError(cause error) *RuntimeError {
	stack := make([]RuntimeFrame, 0, len(v.frames))
	for index := len(v.frames) - 1; index >= 0; index-- {
		current := v.frames[index]
		name := "<main>"
		if current.functionID >= 0 && current.functionID < len(v.bytecode.Functions) {
			name = v.bytecode.Functions[current.functionID].Name
		}
		stack = append(stack, RuntimeFrame{Function: name, Offset: current.ip})
	}
	return &RuntimeError{Cause: cause, Stack: stack}
}

func (v *VM) resetExecutionState() {
	v.stack = v.stack[:0]
	v.frames = v.frames[:0]
	v.globals = nil
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

	for offset := 0; offset < len(instructions); {
		boundaries[offset] = true
		op := code.Opcode(instructions[offset])
		definition, err := code.Lookup(op)
		if err != nil {
			return fmt.Errorf("at offset %d: %w", offset, err)
		}
		operands, read, err := code.ReadOperands(definition, instructions[offset+1:])
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
		return fmt.Errorf("%s requires int64 operands or matching float64 operands, got %s and %s at %d", name, leftValue.Kind(), rightValue.Kind(), ip)
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

	right, ok := rightValue.Int64()
	if !ok {
		return fmt.Errorf("%s requires int64 operands or matching float64 operands, got %s at %d", name, rightValue.Kind(), ip)
	}
	left, ok := leftValue.Int64()
	if !ok {
		return fmt.Errorf("%s requires int64 operands or matching float64 operands, got %s at %d", name, leftValue.Kind(), ip)
	}

	var result int64
	switch op {
	case code.OpAdd:
		result = left + right
	case code.OpSub:
		result = left - right
	case code.OpMul:
		result = left * right
	case code.OpDiv:
		if right == 0 {
			return fmt.Errorf("division by zero at %d", ip)
		}
		result = left / right
	}
	v.stack = v.stack[:len(v.stack)-2]
	return v.push(value.NewInt(result))
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

func (v *VM) setLocal(current *frame, index int, ip int) error {
	function := v.bytecode.Functions[current.functionID]
	if index < 0 || index >= function.LocalCount {
		return fmt.Errorf("local slot %d out of range at %d", index, ip)
	}
	item, err := v.pop("OpSetLocal", ip)
	if err != nil {
		return err
	}
	v.stack[current.base+index] = item
	return nil
}

func (v *VM) getLocal(current *frame, index int, ip int) error {
	function := v.bytecode.Functions[current.functionID]
	if index < 0 || index >= function.LocalCount {
		return fmt.Errorf("local slot %d out of range at %d", index, ip)
	}
	item := v.stack[current.base+index]
	if item.Kind() == value.KindInvalid {
		return fmt.Errorf("local slot %d is uninitialized at %d", index, ip)
	}
	return v.push(item)
}

func (v *VM) call(functionID int, ip int) error {
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
	required := base + function.LocalCount
	if required > MaxValueStack {
		return fmt.Errorf("value stack limit %d exceeded", MaxValueStack)
	}
	for len(v.stack) < required {
		v.stack = append(v.stack, value.Value{})
	}
	v.frames = append(v.frames, frame{functionID: functionID, instructions: function.Instructions, base: base})
	return nil
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
