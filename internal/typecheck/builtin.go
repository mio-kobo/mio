package typecheck

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
)

// checkBuiltinCall 检查内置函数调用
func (c *Checker) checkBuiltinCall(
	name string,
	call *ast.CallExpression,
) (Kind, error) {
	args := call.Arguments
	switch name {

	// chant 输出一个基础类型的值
	case "chant":
		if len(args) != 1 {
			cause := fmt.Errorf(
				"builtin %q expects 1 argument, got %d",
				name,
				len(args),
			)
			return KindInvalid, diagnostic.NewError("E0303", "内置函数 chant 需要一个参数", diagnostic.FromPositions(call.Span().Start, call.Span().End)).WithCause(cause)
		}

		// 检查实参表达式的类型
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}

		// 仅允许可输出的基础类型
		switch kind {
		case KindInt8, KindInt16, KindInt32, KindInt, KindUint8, KindUint16, KindUint32, KindFloat32, KindFloat, KindBool, KindString, KindNil:
			return KindUnit, nil

		default:
			return KindInvalid, fmt.Errorf(
				"builtin %q cannot accept %s",
				name,
				kind,
			)
		}

	case "len":
		if len(args) != 1 {
			cause := fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
			return KindInvalid, diagnostic.NewError("E0303", "内置函数 len 需要一个参数", diagnostic.FromPositions(call.Span().Start, call.Span().End)).WithCause(cause)
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if !kind.IsArray() || kind.IsNullable() {
			cause := fmt.Errorf("builtin %q requires array, got %s", name, kind)
			return KindInvalid, typeDiagnostic("E0305", "内置函数 len 只接受数组", args[0].Span(), "array", kind.String(), cause)
		}
		return KindInt, nil

	case "$math.sqrt", "$math.floor", "$math.ceil", "$math.round_even", "$math.trunc":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if !c.floating(kind) {
			return KindInvalid, fmt.Errorf("builtin %q requires float32 or float64, got %s", name, kind)
		}
		return kind, nil

	case "$math.is_nan", "$math.is_inf", "$math.is_finite", "$math.is_negative_zero":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if !c.floating(kind) {
			return KindInvalid, fmt.Errorf("builtin %q requires float32 or float64, got %s", name, kind)
		}
		return KindBool, nil

	case "$math.add_overflows":
		if len(args) != 2 {
			return KindInvalid, fmt.Errorf("builtin %q expects 2 arguments, got %d", name, len(args))
		}
		for _, argument := range args {
			kind, err := c.checkExpression(argument)
			if err != nil {
				return KindInvalid, err
			}
			if kind != KindInt {
				return KindInvalid, fmt.Errorf("builtin %q requires int64, got %s", name, kind)
			}
		}
		return KindBool, nil

	case "$math.is_negative", "$math.neg_overflows":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if !c.numeric(kind) {
			return KindInvalid, fmt.Errorf("builtin %q requires a number, got %s", name, kind)
		}
		return KindBool, nil

	case "$math.error":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if kind != KindString {
			return KindInvalid, fmt.Errorf("builtin %q requires string, got %s", name, kind)
		}
		return KindUnit, nil

	case "$data.string_to_bytes":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if kind != KindString {
			return KindInvalid, fmt.Errorf("builtin %q requires string, got %s", name, kind)
		}
		return ArrayOf(KindUint8), nil

	case "$data.bytes_to_string":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if kind != ArrayOf(KindUint8) {
			return KindInvalid, fmt.Errorf("builtin %q requires []uint8, got %s", name, kind)
		}
		return NullableOf(KindString), nil

	case "$data.string_byte_len":
		if len(args) != 1 {
			return KindInvalid, fmt.Errorf("builtin %q expects 1 argument, got %d", name, len(args))
		}
		kind, err := c.checkExpression(args[0])
		if err != nil {
			return KindInvalid, err
		}
		if kind != KindString {
			return KindInvalid, fmt.Errorf("builtin %q requires string, got %s", name, kind)
		}
		return KindInt, nil

	default:
		return KindInvalid, fmt.Errorf(
			"unsupported builtin function %q",
			name,
		)
	}
}
