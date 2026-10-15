package compiler

import (
	"fmt"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/code"
	"Mio/internal/diagnostic"
	"Mio/internal/resolver"
	"Mio/internal/value"
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
		if intrinsic := c.functionIntrinsics[symbol]; intrinsic != "" {
			return c.compileIntrinsic(call, intrinsic)
		}
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
	case "$data.string_to_bytes":
		return c.emit(code.OpDataConvert, code.DataStringToBytes)
	case "$data.bytes_to_string":
		return c.emit(code.OpDataConvert, code.DataBytesToString)
	case "$data.string_byte_len":
		return c.emit(code.OpLen)
	default:
		return fmt.Errorf("unsupported builtin function %q", symbol.Name)
	}
}

func (c *Compiler) compileIntrinsic(call *ast.CallExpression, intrinsic string) error {
	for _, argument := range call.Arguments {
		if err := c.Compile(argument); err != nil {
			return err
		}
	}
	descriptor := 0
	if len(call.TypeArgs) != 0 {
		descriptor = arrayTypeDescriptor(call.TypeArgs[0])
	}
	switch intrinsic {
	case "$array.make":
		return c.emit(code.OpArrayMake, descriptor)
	case "$array.with_capacity":
		return c.emit(code.OpArrayWithCapacity, descriptor)
	case "$array.push":
		return c.emit(code.OpArrayPush)
	case "$array.capacity":
		return c.emit(code.OpArrayCapacity)
	case "$array.reserve":
		return c.emit(code.OpArrayReserve)
	case "$array.clone":
		return c.emit(code.OpArrayClone)
	case "$array.truncate":
		return c.emit(code.OpArrayTruncate)
	case "$array.append_all":
		return c.emit(code.OpArrayAppendAll)
	case "$data.string_to_bytes":
		return c.emit(code.OpDataConvert, code.DataStringToBytes)
	case "$data.bytes_to_string":
		return c.emit(code.OpDataConvert, code.DataBytesToString)
	case "$data.string_byte_len":
		return c.emit(code.OpLen)
	default:
		return fmt.Errorf("unsupported intrinsic %q", intrinsic)
	}
}

func arrayTypeDescriptor(name string) int {
	nullable := false
	if strings.HasPrefix(name, "nullable<") && strings.HasSuffix(name, ">") {
		nullable = true
		name = name[len("nullable<") : len(name)-1]
	}
	kinds := map[string]value.Kind{
		"int8": value.KindInt8, "int16": value.KindInt16, "int32": value.KindInt32,
		"int64": value.KindInt, "uint8": value.KindUint8, "uint16": value.KindUint16,
		"uint32": value.KindUint32, "float32": value.KindFloat32, "float64": value.KindFloat,
		"bool": value.KindBool, "string": value.KindString, "array": value.KindArray,
		"struct": value.KindStruct, "enum": value.KindEnum, "map": value.KindMap,
		"function": value.KindFunction,
	}
	kind := kinds[name]
	descriptor := int(kind)
	if nullable {
		descriptor |= 0x80
	}
	return descriptor
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
