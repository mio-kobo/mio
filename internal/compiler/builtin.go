package compiler

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/code"
	"Mio/internal/diagnostic"
	"Mio/internal/resolver"
)

// compileExpressionStatement 丢弃普通表达式的结果；unit 调用没有栈值可弹出。
func (c *Compiler) compileExpressionStatement(expr ast.Expression) error {
	switch n := expr.(type) {
	case *ast.GroupedExpression:
		return c.compileExpressionStatement(n.Expr)
	case *ast.CallExpression:
		if err := c.compileCall(n); err != nil {
			return err
		}
		returnsValue, err := c.callReturnsValue(n)
		if err != nil {
			return err
		}
		if returnsValue {
			return c.emit(code.OpPop)
		}
		return nil
	default:
		if err := c.Compile(expr); err != nil {
			return err
		}
		return c.emit(code.OpPop)
	}
}

// compileCall 编译 Resolver 绑定的内置函数或用户函数调用。
// 与其他表达式一样，实参类型由编译前的语义检查保证。
func (c *Compiler) compileCall(call *ast.CallExpression) error {
	if call.EnumType != "" {
		for _, argument := range call.Arguments {
			if err := c.Compile(argument); err != nil {
				return err
			}
		}
		return c.emit(code.OpEnum, call.Variant, len(call.Arguments))
	}
	if call.MapCreate {
		return c.emit(code.OpMap)
	}
	if call.Dynamic {
		if err := c.Compile(call.Function); err != nil {
			return err
		}
		for _, argument := range call.Arguments {
			if err := c.Compile(argument); err != nil {
				return err
			}
		}
		return c.emit(code.OpCallValue, len(call.Arguments))
	}
	function := call.Function
	if apply, ok := function.(*ast.TypeApplyExpression); ok {
		function = apply.Function
	}
	callee, ok := function.(*ast.Identifier)
	if !ok {
		return fmt.Errorf("call target must be a function name, got %T", call.Function)
	}
	if c.resolver == nil {
		return fmt.Errorf("function call compilation requires a name resolver")
	}
	symbol, ok := c.resolver.BindingOf(callee)
	if !ok {
		return fmt.Errorf("missing symbol binding for function %q", callee.Name)
	}
	if symbol.Kind == resolver.SymbolFunction {
		id, ok := c.functionIDs[symbol]
		if !ok {
			return fmt.Errorf("function %q has no FunctionID", symbol.Name)
		}
		parameterCount := c.functions[id].ParameterCount
		if len(call.Arguments) != parameterCount {
			cause := fmt.Errorf("function %q expects %d arguments, got %d", symbol.Name, parameterCount, len(call.Arguments))
			return diagnostic.NewError("E0401", "函数参数数量不正确", diagnostic.FromPositions(call.Span().Start, call.Span().End)).WithCause(cause)
		}
		for _, argument := range call.Arguments {
			if err := c.Compile(argument); err != nil {
				return err
			}
		}
		return c.emit(code.OpCall, id)
	}
	if symbol.Kind != resolver.SymbolBuiltin {
		return fmt.Errorf("cannot call %s %q", symbol.Kind, symbol.Name)
	}
	expectedArguments := 1
	if symbol.Name == "$math.add_overflows" {
		expectedArguments = 2
	}
	if len(call.Arguments) != expectedArguments {
		cause := fmt.Errorf("builtin %q expects 1 argument, got %d", symbol.Name, len(call.Arguments))
		return diagnostic.NewError("E0401", "内置函数参数数量不正确", diagnostic.FromPositions(call.Span().Start, call.Span().End)).WithCause(cause)
	}
	for _, argument := range call.Arguments {
		if err := c.Compile(argument); err != nil {
			return err
		}
	}
	switch symbol.Name {
	case "chant":
		return c.emit(code.OpChant)
	case "len":
		return c.emit(code.OpLen)
	case "$math.sqrt":
		return c.emit(code.OpMath, code.MathSqrt, 1)
	case "$math.floor":
		return c.emit(code.OpMath, code.MathFloor, 1)
	case "$math.ceil":
		return c.emit(code.OpMath, code.MathCeil, 1)
	case "$math.round_even":
		return c.emit(code.OpMath, code.MathRoundEven, 1)
	case "$math.trunc":
		return c.emit(code.OpMath, code.MathTrunc, 1)
	case "$math.is_nan":
		return c.emit(code.OpMath, code.MathIsNaN, 1)
	case "$math.is_inf":
		return c.emit(code.OpMath, code.MathIsInf, 1)
	case "$math.is_finite":
		return c.emit(code.OpMath, code.MathIsFinite, 1)
	case "$math.is_negative_zero":
		return c.emit(code.OpMath, code.MathIsNegativeZero, 1)
	case "$math.add_overflows":
		return c.emit(code.OpMath, code.MathAddOverflows, 2)
	case "$math.is_negative":
		return c.emit(code.OpMath, code.MathIsNegative, 1)
	case "$math.neg_overflows":
		return c.emit(code.OpMath, code.MathNegOverflows, 1)
	case "$math.error":
		return c.emit(code.OpMath, code.MathError, 1)
	default:
		return fmt.Errorf("unsupported builtin function %q", symbol.Name)
	}
}

// callReturnsValue 判断调用是否产生运行时值
func (c *Compiler) callReturnsValue(call *ast.CallExpression) (bool, error) {
	if call.EnumType != "" || call.MapCreate || call.Dynamic {
		return call.HasValue, nil
	}
	function := call.Function
	if apply, ok := function.(*ast.TypeApplyExpression); ok {
		function = apply.Function
	}
	callee, ok := function.(*ast.Identifier)
	if !ok || c.resolver == nil {
		return false, fmt.Errorf("call target must be a resolved function name")
	}
	symbol, ok := c.resolver.BindingOf(callee)
	if !ok {
		return false, fmt.Errorf("missing symbol binding for function %q", callee.Name)
	}
	if symbol.Kind == resolver.SymbolBuiltin {
		return symbol.Name != "chant" && symbol.Name != "$math.error", nil
	}
	returnsValue, ok := c.functionReturns[symbol]
	if !ok {
		return false, fmt.Errorf("function %q has no return metadata", callee.Name)
	}
	return returnsValue, nil
}
