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
		case KindInt, KindFloat, KindBool, KindString, KindNil:
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

	default:
		return KindInvalid, fmt.Errorf(
			"unsupported builtin function %q",
			name,
		)
	}
}
