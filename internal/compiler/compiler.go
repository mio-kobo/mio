package compiler

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/code"
	"Mio/internal/diagnostic"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/value"
)

// Bytecode 保存编译结果
type Bytecode struct {
	Instructions code.Instructions
	Constants    []value.Value
	GlobalsCount int
	Functions    []CompiledFunction
}

// CompiledFunction 保存独立函数指令流及栈帧元数据
type CompiledFunction struct {
	ID             int
	Name           string
	Instructions   code.Instructions
	ParameterCount int
	LocalCount     int
	ReturnsValue   bool
}

// FunctionArtifact 保存可搬移的函数编译结果
type FunctionArtifact struct {
	Instructions   code.Instructions
	Constants      []value.Value
	ParameterCount int
	LocalCount     int
	ReturnsValue   bool
}

// FunctionCache 保存函数编译结果
type FunctionCache interface {
	LoadFunction(key string) (FunctionArtifact, bool)
	StoreFunction(key string, artifact FunctionArtifact)
}

// IncrementalStats 记录函数编译复用情况
type IncrementalStats struct {
	Hits       int
	Recomputed int
}

type loopContext struct {
	conditionStart int
	breakPositions []int
}

// Compiler 将 AST 编译为字节码
type Compiler struct {
	instructions code.Instructions
	constants    []value.Value

	resolver *resolver.Resolver
	globals  map[resolver.Symbol]int
	locals   map[resolver.Symbol]int

	functionIDs     map[resolver.Symbol]int
	functionReturns map[resolver.Symbol]bool
	functions       []CompiledFunction
	currentFunction *ast.FunctionStatement
	loops           []loopContext

	functionKeys  map[string]string
	functionCache FunctionCache
	cacheStats    IncrementalStats
	tempCounter   int
}

// ConfigureFunctionCache 配置函数级编译缓存
func (c *Compiler) ConfigureFunctionCache(keys map[string]string, cache FunctionCache) {
	c.functionKeys = keys
	c.functionCache = cache
}

// IncrementalStats 返回函数编译复用统计
func (c *Compiler) IncrementalStats() IncrementalStats {
	return c.cacheStats
}

// New 创建字节码编译器
func New() *Compiler {
	return &Compiler{
		instructions:    make(code.Instructions, 0),
		constants:       make([]value.Value, 0),
		globals:         make(map[resolver.Symbol]int),
		functionIDs:     make(map[resolver.Symbol]int),
		functionReturns: make(map[resolver.Symbol]bool),
	}
}

// NewWithResolver 创建带符号绑定的编译器
func NewWithResolver(r *resolver.Resolver) *Compiler {
	c := New()
	c.resolver = r
	return c
}

// Compile 将 AST 节点编译为字节码
func (c *Compiler) Compile(node ast.Node) error {
	switch n := node.(type) {

	// 编译整个程序
	case *ast.Program:
		if err := c.registerFunctions(n); err != nil {
			return err
		}
		for _, stmt := range n.Statements {
			if err := c.Compile(stmt); err != nil {
				return err
			}
		}

		return nil

	// 编译表达式语句
	case *ast.ExpressionStatement:
		return c.compileExpressionStatement(n.Expr)

	case *ast.CallExpression:
		return c.compileCall(n)

	case *ast.ConversionExpression:
		if len(n.Arguments) != 1 {
			cause := fmt.Errorf("conversion %q expects 1 argument, got %d", n.Target, len(n.Arguments))
			return diagnostic.NewError("E0401", "数值转换需要一个参数", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		if err := c.Compile(n.Arguments[0]); err != nil {
			return err
		}
		switch n.Target {
		case "float64":
			return c.emit(code.OpIntToFloat)
		case "int64":
			return c.emit(code.OpFloatToInt)
		default:
			return fmt.Errorf("unsupported conversion target %q", n.Target)
		}

	case *ast.ArrayLiteral:
		if len(n.Elements) > 0xffff {
			return fmt.Errorf("array literal has too many elements: %d", len(n.Elements))
		}
		for _, element := range n.Elements {
			if err := c.Compile(element); err != nil {
				return err
			}
		}
		return c.emit(code.OpArray, len(n.Elements))

	case *ast.IndexExpression:
		if err := c.Compile(n.Left); err != nil {
			return err
		}
		if err := c.Compile(n.Index); err != nil {
			return err
		}
		if n.Map {
			return c.emit(code.OpMapGet)
		}
		return c.emit(code.OpIndex)

	case *ast.StructLiteral:
		for _, field := range n.Fields {
			if err := c.Compile(field.Value); err != nil {
				return err
			}
		}
		return c.emit(code.OpStruct, len(n.Fields))

	case *ast.FieldExpression:
		if err := c.Compile(n.Left); err != nil {
			return err
		}
		return c.emit(code.OpGetField, n.Index)

	case *ast.FunctionStatement:
		return c.compileFunction(n)

	case *ast.StructStatement, *ast.EnumStatement:
		return nil

	case *ast.MatchStatement:
		return c.compileMatchStatement(n)

	case *ast.ReturnStatement:
		if c.currentFunction == nil {
			cause := fmt.Errorf("kaesu outside function")
			return diagnostic.NewError("E0401", "无法编译函数外的 kaesu", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		if n.Value == nil {
			return c.emit(code.OpReturn)
		}
		if err := c.Compile(n.Value); err != nil {
			return err
		}
		return c.emit(code.OpReturnValue)

	// 编译整数字面量
	case *ast.IntegerLiteral:
		index, err := c.addConstant(value.NewInt(n.Value))
		if err != nil {
			return err
		}

		return c.emit(code.OpConstant, index)

	// 编译浮点字面量
	case *ast.FloatLiteral:
		index, err := c.addConstant(value.NewFloat(n.Value))
		if err != nil {
			return err
		}
		return c.emit(code.OpConstant, index)

	// 编译已解码的字符串字面量
	case *ast.StringLiteral:
		index, err := c.addConstant(value.NewString(n.Value))
		if err != nil {
			return err
		}

		return c.emit(code.OpConstant, index)

	// mu 是空值，不是 unit
	case *ast.NilLiteral:
		return c.emit(code.OpNil)

	// 括号分组不产生额外指令
	case *ast.GroupedExpression:
		return c.Compile(n.Expr)

		// 编译二元算术表达式
	case *ast.InfixExpression:
		var opcode code.Opcode

		// 逻辑运算使用短路编译
		if n.Operator == token.And || n.Operator == token.Or {
			return c.compileLogicalExpression(n)
		}
		if n.Operator == token.Equal || n.Operator == token.NotEqual {
			operand, ok := nilComparisonOperand(n)
			if ok {
				if err := c.Compile(operand); err != nil {
					return err
				}
				if err := c.emit(code.OpIsNil); err != nil {
					return err
				}
				if n.Operator == token.NotEqual {
					return c.emit(code.OpNot)
				}
				return nil
			}
		}

		// 根据运算符选择字节码指令
		switch n.Operator {
		case token.Plus:
			opcode = code.OpAdd

		case token.Minus:
			opcode = code.OpSub

		case token.Asterisk:
			opcode = code.OpMul

		case token.Slash:
			opcode = code.OpDiv

		case token.Equal:
			opcode = code.OpEqual

		case token.NotEqual:
			opcode = code.OpNotEqual

		case token.Less:
			opcode = code.OpLess

		case token.LessEqual:
			opcode = code.OpLessEqual

		case token.Greater:
			opcode = code.OpGreater

		case token.GreaterEqual:
			opcode = code.OpGreaterEqual

		default:
			return fmt.Errorf(
				"unsupported infix operator %s",
				n.Operator,
			)
		}

		// 先编译左操作数
		if err := c.Compile(n.Left); err != nil {
			return err
		}

		// 再编译右操作数
		if err := c.Compile(n.Right); err != nil {
			return err
		}

		// 生成对应算术指令
		return c.emit(opcode)

		// 编译一元表达式
	case *ast.PrefixExpression:
		var opcode code.Opcode

		// 根据运算符选择指令
		switch n.Operator {
		case token.Minus:
			opcode = code.OpNeg

		case token.Bang:
			opcode = code.OpNot

		default:
			return fmt.Errorf(
				"unsupported prefix operator %s",
				n.Operator,
			)
		}

		// 先编译操作数
		if err := c.Compile(n.Right); err != nil {
			return err
		}

		// 生成一元运算指令
		return c.emit(opcode)

	// 编译布尔字面量
	case *ast.BooleanLiteral:
		if n.Value {
			return c.emit(code.OpTrue)
		}

		return c.emit(code.OpFalse)

		// 编译代码块中的语句
	case *ast.BlockStatement:
		for _, stmt := range n.Statements {
			if err := c.Compile(stmt); err != nil {
				return err
			}
		}

		return nil

	// 编译条件语句
	case *ast.IfStatement:
		return c.compileIfStatement(n)

	case *ast.WhileStatement:
		return c.compileWhileStatement(n)

	case *ast.BreakStatement:
		if len(c.loops) == 0 {
			cause := fmt.Errorf("nukeru outside loop")
			return diagnostic.NewError("E0401", "无法编译循环外的 nukeru", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		position := len(c.instructions)
		if err := c.emit(code.OpJump, 0xffff); err != nil {
			return err
		}
		index := len(c.loops) - 1
		c.loops[index].breakPositions = append(c.loops[index].breakPositions, position)
		return nil

	case *ast.ContinueStatement:
		if len(c.loops) == 0 {
			cause := fmt.Errorf("tsuzuku outside loop")
			return diagnostic.NewError("E0401", "无法编译循环外的 tsuzuku", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		return c.emit(code.OpJump, c.loops[len(c.loops)-1].conditionStart)

	// 编译变量声明
	case *ast.LetStatement:
		symbol, err := c.boundVariable(n.Name)
		if err != nil {
			return err
		}

		slots := c.globals
		op := code.OpSetGlobal
		if c.currentFunction != nil {
			slots = c.locals
			op = code.OpSetLocal
		}

		if _, exists := slots[symbol]; exists {
			return fmt.Errorf(
				"variable %q already compiled",
				n.Name.Name,
			)
		}

		if len(slots) >= 1<<16 {
			return fmt.Errorf("variable slot overflow")
		}

		// 编译变量初始化表达式
		if err := c.Compile(n.Value); err != nil {
			return err
		}

		// 为声明符号分配独立物理槽位
		index := len(slots)

		if err := c.emit(op, index); err != nil {
			return err
		}

		slots[symbol] = index

		return nil

	// 编译变量引用
	case *ast.Identifier:
		if c.resolver != nil {
			if symbol, ok := c.resolver.BindingOf(n); ok && symbol.Kind == resolver.SymbolFunction {
				id, exists := c.functionIDs[symbol]
				if !exists {
					return fmt.Errorf("function %q has no FunctionID", n.Name)
				}
				return c.emit(code.OpFunction, id)
			}
		}
		index, local, err := c.variableSlot(n)
		if err != nil {
			return err
		}

		if local {
			return c.emit(code.OpGetLocal, index)
		}
		return c.emit(code.OpGetGlobal, index)

		// 编译变量赋值
	case *ast.AssignStatement:
		index, local, err := c.variableSlot(n.Name)
		if err != nil {
			return err
		}

		// 先计算赋值表达式
		if err := c.Compile(n.Value); err != nil {
			return err
		}

		// 将新值写回原变量槽位
		if local {
			return c.emit(code.OpSetLocal, index)
		}
		return c.emit(code.OpSetGlobal, index)

	case *ast.IndexAssignStatement:
		if err := c.Compile(n.Target.Left); err != nil {
			return err
		}
		if err := c.Compile(n.Target.Index); err != nil {
			return err
		}
		if err := c.Compile(n.Value); err != nil {
			return err
		}
		if n.Target.Map {
			return c.emit(code.OpMapSet)
		}
		if n.Nullable {
			return c.emit(code.OpSetNullableIndex)
		}
		return c.emit(code.OpSetIndex)

	default:
		return fmt.Errorf(
			"unsupported AST node %T",
			node,
		)
	}
}

// compileMatchStatement 编译枚举穷尽分支
func (c *Compiler) compileMatchStatement(statement *ast.MatchStatement) error {
	if err := c.Compile(statement.Value); err != nil {
		return err
	}
	temp, local, err := c.allocateTemporary()
	if err != nil {
		return err
	}
	setOp, getOp := code.OpSetGlobal, code.OpGetGlobal
	if local {
		setOp, getOp = code.OpSetLocal, code.OpGetLocal
	}
	if err := c.emit(setOp, temp); err != nil {
		return err
	}
	endJumps := make([]int, 0, len(statement.Arms))
	for armIndex := range statement.Arms {
		arm := &statement.Arms[armIndex]
		if err := c.emit(getOp, temp); err != nil {
			return err
		}
		if err := c.emit(code.OpEnumTag); err != nil {
			return err
		}
		constant, err := c.addConstant(value.NewInt(int64(arm.Tag)))
		if err != nil {
			return err
		}
		if err := c.emit(code.OpConstant, constant); err != nil {
			return err
		}
		if err := c.emit(code.OpEqual); err != nil {
			return err
		}
		falseJump := len(c.instructions)
		if err := c.emit(code.OpJumpIfFalse, 0xffff); err != nil {
			return err
		}
		for index, binding := range arm.Bindings {
			if err := c.emit(getOp, temp); err != nil {
				return err
			}
			if err := c.emit(code.OpGetEnumValue, index); err != nil {
				return err
			}
			symbol, ok := c.resolver.BindingOf(binding)
			if !ok {
				return fmt.Errorf("missing match binding %q", binding.Name)
			}
			slot, bindingLocal, err := c.allocateSymbol(symbol)
			if err != nil {
				return err
			}
			op := code.OpSetGlobal
			if bindingLocal {
				op = code.OpSetLocal
			}
			if err := c.emit(op, slot); err != nil {
				return err
			}
		}
		if err := c.Compile(arm.Body); err != nil {
			return err
		}
		jump := len(c.instructions)
		if err := c.emit(code.OpJump, 0xffff); err != nil {
			return err
		}
		endJumps = append(endJumps, jump)
		if err := c.patchJump(falseJump, len(c.instructions)); err != nil {
			return err
		}
	}
	end := len(c.instructions)
	for _, jump := range endJumps {
		if err := c.patchJump(jump, end); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) allocateTemporary() (int, bool, error) {
	c.tempCounter++
	symbol := resolver.Symbol{Name: fmt.Sprintf("$temp%d", c.tempCounter), Kind: resolver.SymbolVariable}
	return c.allocateSymbol(symbol)
}

func (c *Compiler) allocateSymbol(symbol resolver.Symbol) (int, bool, error) {
	slots := c.globals
	local := false
	if c.currentFunction != nil {
		slots = c.locals
		local = true
	}
	if len(slots) >= 1<<16 {
		return 0, local, fmt.Errorf("variable slot overflow")
	}
	index := len(slots)
	slots[symbol] = index
	return index, local, nil
}

func nilComparisonOperand(expression *ast.InfixExpression) (ast.Expression, bool) {
	if isNilExpression(expression.Left) {
		if supportsNilCheck(expression.Right) {
			return expression.Right, true
		}
		return nil, false
	}
	if isNilExpression(expression.Right) {
		if supportsNilCheck(expression.Left) {
			return expression.Left, true
		}
	}
	return nil, false
}

func isNilExpression(expression ast.Expression) bool {
	for {
		group, ok := expression.(*ast.GroupedExpression)
		if !ok {
			break
		}
		expression = group.Expr
	}
	_, ok := expression.(*ast.NilLiteral)
	return ok
}

func supportsNilCheck(expression ast.Expression) bool {
	switch n := expression.(type) {
	case *ast.Identifier, *ast.CallExpression, *ast.IndexExpression:
		return true
	case *ast.GroupedExpression:
		return supportsNilCheck(n.Expr)
	default:
		return false
	}
}

// addConstant 将带类型的值加入常量池
func (c *Compiler) addConstant(v value.Value) (int, error) {
	// OpConstant 使用两个字节表示索引
	if len(c.constants) >= 1<<16 {
		return 0, fmt.Errorf("constant pool index overflow")
	}

	index := len(c.constants)
	c.constants = append(c.constants, v)

	return index, nil
}

// emit 编码并追加一条指令
func (c *Compiler) emit(
	op code.Opcode,
	operands ...int,
) error {
	ins, err := code.Make(op, operands...)
	if err != nil {
		return err
	}

	c.instructions = append(c.instructions, ins...)

	return nil
}

// Bytecode 获取当前编译结果
func (c *Compiler) Bytecode() *Bytecode {
	functions := make([]CompiledFunction, len(c.functions))
	for index, function := range c.functions {
		functions[index] = function
		functions[index].Instructions = append(code.Instructions(nil), function.Instructions...)
	}
	return &Bytecode{
		Instructions: append(
			code.Instructions(nil),
			c.instructions...,
		),
		Constants: append(
			[]value.Value(nil),
			c.constants...,
		),
		GlobalsCount: len(c.globals),
		Functions:    functions,
	}
}

// patchJump 回填已生成跳转指令的目标地址
func (c *Compiler) patchJump(position int, target int) error {
	if position < 0 || position >= len(c.instructions) {
		return fmt.Errorf(
			"invalid jump position %d",
			position,
		)
	}

	// 只能修改跳转指令
	op := code.Opcode(c.instructions[position])

	if op != code.OpJump && op != code.OpJumpIfFalse {
		return fmt.Errorf(
			"instruction at %d is not a jump",
			position,
		)
	}

	// 重新编码带有真实地址的指令
	ins, err := code.Make(op, target)
	if err != nil {
		return err
	}

	if position+len(ins) > len(c.instructions) {
		return fmt.Errorf(
			"incomplete jump instruction at %d",
			position,
		)
	}

	// 原地覆盖占位指令
	copy(c.instructions[position:position+len(ins)], ins)

	return nil
}

// compileIfStatement 编译条件语句及分支跳转
func (c *Compiler) compileIfStatement(
	n *ast.IfStatement,
) error {
	// 先编译条件表达式
	if err := c.Compile(n.Condition); err != nil {
		return err
	}

	// 记录条件跳转的位置
	jumpFalsePos := len(c.instructions)

	// 暂时使用占位地址
	if err := c.emit(code.OpJumpIfFalse, 0xffff); err != nil {
		return err
	}

	// 编译 moshi 分支
	if err := c.Compile(n.Consequence); err != nil {
		return err
	}

	// 没有 sore 时，条件为假直接跳到末尾
	if n.Alternative == nil {
		return c.patchJump(
			jumpFalsePos,
			len(c.instructions),
		)
	}

	// 有 sore 时，moshi 执行完需要跳过 sore
	jumpEndPos := len(c.instructions)

	if err := c.emit(code.OpJump, 0xffff); err != nil {
		return err
	}

	// 此时当前位置就是 sore 分支的起点
	if err := c.patchJump(
		jumpFalsePos,
		len(c.instructions),
	); err != nil {
		return err
	}

	// 编译 sore 分支
	if err := c.Compile(n.Alternative); err != nil {
		return err
	}

	// 回填整个条件语句的结束地址
	return c.patchJump(
		jumpEndPos,
		len(c.instructions),
	)
}

// compileWhileStatement 编译循环条件及回跳。
func (c *Compiler) compileWhileStatement(n *ast.WhileStatement) error {
	conditionStart := len(c.instructions)
	if err := c.Compile(n.Condition); err != nil {
		return err
	}

	jumpFalsePos := len(c.instructions)
	if err := c.emit(code.OpJumpIfFalse, 0xffff); err != nil {
		return err
	}

	c.loops = append(c.loops, loopContext{conditionStart: conditionStart})
	loopIndex := len(c.loops) - 1
	if err := c.Compile(n.Body); err != nil {
		c.loops = c.loops[:loopIndex]
		return err
	}
	context := c.loops[loopIndex]
	c.loops = c.loops[:loopIndex]

	if err := c.emit(code.OpJump, conditionStart); err != nil {
		return err
	}
	end := len(c.instructions)
	if err := c.patchJump(jumpFalsePos, end); err != nil {
		return err
	}
	for _, position := range context.breakPositions {
		if err := c.patchJump(position, end); err != nil {
			return err
		}
	}
	return nil
}

// compileLogicalExpression 编译短路逻辑表达式
func (c *Compiler) compileLogicalExpression(
	n *ast.InfixExpression,
) error {
	// 先编译左操作数
	if err := c.Compile(n.Left); err != nil {
		return err
	}

	// 左侧为 false 时进行条件跳转
	jumpFalsePos := len(c.instructions)

	if err := c.emit(code.OpJumpIfFalse, 0xffff); err != nil {
		return err
	}

	switch n.Operator {
	case token.And:
		// 左侧为 true 时才执行右侧
		if err := c.Compile(n.Right); err != nil {
			return err
		}

		// 执行完右侧后跳过 false 分支
		jumpEndPos := len(c.instructions)

		if err := c.emit(code.OpJump, 0xffff); err != nil {
			return err
		}

		// 左侧为 false 时，整个表达式为 false
		falsePos := len(c.instructions)

		if err := c.emit(code.OpFalse); err != nil {
			return err
		}

		// 回填短路跳转地址
		if err := c.patchJump(jumpFalsePos, falsePos); err != nil {
			return err
		}

		return c.patchJump(
			jumpEndPos,
			len(c.instructions),
		)

	case token.Or:
		// 左侧为 true 时直接产生 true
		if err := c.emit(code.OpTrue); err != nil {
			return err
		}

		// 跳过右侧表达式
		jumpEndPos := len(c.instructions)

		if err := c.emit(code.OpJump, 0xffff); err != nil {
			return err
		}

		// 左侧为 false 时才执行右侧
		rightPos := len(c.instructions)

		if err := c.patchJump(jumpFalsePos, rightPos); err != nil {
			return err
		}

		if err := c.Compile(n.Right); err != nil {
			return err
		}

		return c.patchJump(
			jumpEndPos,
			len(c.instructions),
		)

	default:
		return fmt.Errorf(
			"unsupported logical operator %s",
			n.Operator,
		)
	}
}

// registerFunctions 为顶层函数分配稳定的 FunctionID
func (c *Compiler) registerFunctions(program *ast.Program) error {
	if len(c.functionIDs) != 0 || len(c.functions) != 0 {
		return fmt.Errorf("functions already registered")
	}
	hasFunctions := false
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*ast.FunctionStatement); ok {
			hasFunctions = true
			break
		}
	}
	if !hasFunctions {
		return nil
	}
	if c.resolver == nil {
		return fmt.Errorf("function compilation requires a name resolver")
	}

	for _, stmt := range program.Statements {
		fn, ok := stmt.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		if len(c.functions) >= 1<<16 {
			return fmt.Errorf("function count overflow")
		}

		symbol, ok := c.resolver.BindingOf(fn.Name)
		if !ok || symbol.Kind != resolver.SymbolFunction {
			return fmt.Errorf("missing function binding for %q", fn.Name.Name)
		}
		id := len(c.functions)
		c.functionIDs[symbol] = id
		returnsValue := fn.ReturnType != nil && fn.ReturnType.Name != "unit"
		c.functionReturns[symbol] = returnsValue
		c.functions = append(c.functions, CompiledFunction{
			ID:             id,
			Name:           fn.Name.Name,
			ParameterCount: len(fn.Parameters),
			ReturnsValue:   returnsValue,
		})
	}

	return nil
}

// compileFunction 编译独立函数指令流
func (c *Compiler) compileFunction(fn *ast.FunctionStatement) error {
	if c.currentFunction != nil {
		return fmt.Errorf("nested function declaration %q is not supported", fn.Name.Name)
	}
	symbol, ok := c.resolver.BindingOf(fn.Name)
	if !ok {
		return fmt.Errorf("missing function binding for %q", fn.Name.Name)
	}
	id, ok := c.functionIDs[symbol]
	if !ok {
		return fmt.Errorf("function %q has no FunctionID", fn.Name.Name)
	}

	key := c.functionKeys[fn.Name.Name]
	if c.functionCache != nil && key != "" {
		if artifact, found := c.functionCache.LoadFunction(key); found {
			instructions, err := relocateFunctionConstants(artifact.Instructions, len(c.constants))
			if err == nil && len(c.constants)+len(artifact.Constants) <= 1<<16 {
				c.constants = append(c.constants, artifact.Constants...)
				c.functions[id] = CompiledFunction{
					ID:             id,
					Name:           fn.Name.Name,
					Instructions:   instructions,
					ParameterCount: artifact.ParameterCount,
					LocalCount:     artifact.LocalCount,
					ReturnsValue:   artifact.ReturnsValue,
				}
				c.cacheStats.Hits++
				return nil
			}
		}
	}
	c.cacheStats.Recomputed++

	outerInstructions := c.instructions
	outerLocals := c.locals
	outerLoops := c.loops
	c.instructions = make(code.Instructions, 0)
	c.locals = make(map[resolver.Symbol]int)
	c.loops = nil
	c.currentFunction = fn
	constantStart := len(c.constants)
	defer func() {
		c.instructions = outerInstructions
		c.locals = outerLocals
		c.loops = outerLoops
		c.currentFunction = nil
	}()

	for index, parameter := range fn.Parameters {
		parameterSymbol, ok := c.resolver.BindingOf(parameter)
		if !ok || parameterSymbol.Kind != resolver.SymbolParameter {
			return fmt.Errorf("missing parameter binding for %q", parameter.Name)
		}
		c.locals[parameterSymbol] = index
	}

	if err := c.Compile(fn.Body); err != nil {
		return err
	}
	if !c.functionReturns[symbol] {
		if err := c.emit(code.OpReturn); err != nil {
			return err
		}
	}

	c.functions[id] = CompiledFunction{
		ID:             id,
		Name:           fn.Name.Name,
		Instructions:   append(code.Instructions(nil), c.instructions...),
		ParameterCount: len(fn.Parameters),
		LocalCount:     len(c.locals),
		ReturnsValue:   c.functionReturns[symbol],
	}
	if c.functionCache != nil && key != "" {
		normalized, err := normalizeFunctionConstants(c.instructions, constantStart)
		if err == nil {
			c.functionCache.StoreFunction(key, FunctionArtifact{
				Instructions:   normalized,
				Constants:      append([]value.Value(nil), c.constants[constantStart:]...),
				ParameterCount: len(fn.Parameters),
				LocalCount:     len(c.locals),
				ReturnsValue:   c.functionReturns[symbol],
			})
		}
	}
	return nil
}

func normalizeFunctionConstants(instructions code.Instructions, base int) (code.Instructions, error) {
	return rewriteFunctionConstants(instructions, func(index int) (int, error) {
		if index < base {
			return 0, fmt.Errorf("function constant %d precedes base %d", index, base)
		}
		return index - base, nil
	})
}

func relocateFunctionConstants(instructions code.Instructions, base int) (code.Instructions, error) {
	return rewriteFunctionConstants(instructions, func(index int) (int, error) {
		if index > 0xffff-base {
			return 0, fmt.Errorf("constant pool index overflow")
		}
		return base + index, nil
	})
}

func rewriteFunctionConstants(instructions code.Instructions, rewrite func(int) (int, error)) (code.Instructions, error) {
	result := append(code.Instructions(nil), instructions...)
	for offset := 0; offset < len(result); {
		op := code.Opcode(result[offset])
		definition, err := code.Lookup(op)
		if err != nil {
			return nil, err
		}
		operands, width, err := code.ReadOperands(definition, result[offset+1:])
		if err != nil {
			return nil, err
		}
		if op == code.OpConstant {
			index, err := rewrite(operands[0])
			if err != nil {
				return nil, err
			}
			encoded, err := code.Make(op, index)
			if err != nil {
				return nil, err
			}
			copy(result[offset:offset+len(encoded)], encoded)
		}
		offset += 1 + width
	}
	return result, nil
}

// boundVariable 获取变量标识符绑定的符号
func (c *Compiler) boundVariable(
	ident *ast.Identifier,
) (resolver.Symbol, error) {
	if c.resolver == nil {
		return resolver.Symbol{}, fmt.Errorf(
			"variable compilation requires a name resolver",
		)
	}

	if ident == nil {
		return resolver.Symbol{}, fmt.Errorf(
			"missing variable identifier",
		)
	}

	symbol, ok := c.resolver.BindingOf(ident)
	if !ok {
		return resolver.Symbol{}, fmt.Errorf(
			"missing symbol binding for %q",
			ident.Name,
		)
	}

	if symbol.Kind != resolver.SymbolVariable &&
		symbol.Kind != resolver.SymbolParameter {
		return resolver.Symbol{}, fmt.Errorf(
			"%q is not a variable or parameter symbol",
			ident.Name,
		)
	}

	return symbol, nil
}

// variableSlot 查询变量符号对应的物理槽位
func (c *Compiler) variableSlot(
	ident *ast.Identifier,
) (int, bool, error) {
	symbol, err := c.boundVariable(ident)
	if err != nil {
		return 0, false, err
	}

	if c.currentFunction != nil {
		if index, ok := c.locals[symbol]; ok {
			return index, true, nil
		}
	}
	if index, ok := c.globals[symbol]; ok {
		return index, false, nil
	}

	return 0, false, fmt.Errorf(
		"variable %q has no allocated slot",
		ident.Name,
	)
}
