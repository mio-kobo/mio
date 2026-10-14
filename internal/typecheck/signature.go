package typecheck

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/resolver"
)

// FunctionSignature 保存函数参数与返回类型
type FunctionSignature struct {
	TypeParams     []string
	ParameterTypes []Kind
	ReturnType     Kind
	HashableParams map[string]struct{}
}

// resolveTypeRef 将源码类型名称转换为内部类型
func resolveTypeRef(ref *ast.TypeRef) (Kind, error) {
	if ref == nil {
		return KindInvalid, fmt.Errorf(
			"missing type annotation",
		)
	}
	if ref.Name == "[]" {
		element, err := resolveTypeRef(ref.Element)
		if err != nil {
			return KindInvalid, err
		}
		kind := ArrayOf(element)
		if kind == KindInvalid {
			pos := ref.Span().Start
			return KindInvalid, fmt.Errorf("array element type must be a scalar type at %d:%d", pos.Line, pos.Column)
		}
		return kind, nil
	}
	if ref.Name == "?" {
		inner, err := resolveTypeRef(ref.Element)
		if err != nil {
			return KindInvalid, err
		}
		kind := NullableOf(inner)
		if kind == KindInvalid {
			pos := ref.Span().Start
			return KindInvalid, fmt.Errorf("type %s cannot be nullable at %d:%d", inner, pos.Line, pos.Column)
		}
		return kind, nil
	}

	switch ref.Name {
	case "int64":
		return KindInt, nil

	case "float64":
		return KindFloat, nil

	case "bool":
		return KindBool, nil

	case "string":
		return KindString, nil

	case "unit":
		return KindUnit, nil

	default:
		pos := ref.Span().Start

		return KindInvalid, fmt.Errorf(
			"unknown type %q at %d:%d",
			ref.Name,
			pos.Line,
			pos.Column,
		)
	}
}

// registerFunctionSignature 检查并登记函数签名
func (c *Checker) registerFunctionSignature(
	fn *ast.FunctionStatement,
) error {
	if c.resolver == nil {
		return fmt.Errorf(
			"function signature requires a name resolver",
		)
	}

	if fn == nil || fn.Name == nil {
		return fmt.Errorf("invalid function declaration")
	}

	// 获取函数声明对应的符号
	symbol, ok := c.resolver.BindingOf(fn.Name)
	if !ok {
		return fmt.Errorf(
			"missing symbol binding for function %q",
			fn.Name.Name,
		)
	}

	if symbol.Kind != resolver.SymbolFunction {
		return fmt.Errorf(
			"%q is not a function symbol",
			fn.Name.Name,
		)
	}

	// 函数必须声明返回类型
	if fn.ReturnType == nil {
		return fmt.Errorf(
			"function %q requires a return type",
			fn.Name.Name,
		)
	}
	if err := validateTypeParameters(fn.Name.Name, fn.TypeParams); err != nil {
		return err
	}

	parameters := parameterSet(fn.TypeParams)
	returnType, err := c.resolveType(fn.ReturnType, parameters)
	if err != nil {
		return err
	}

	signature := FunctionSignature{
		TypeParams:     append([]string(nil), fn.TypeParams...),
		ParameterTypes: make([]Kind, 0, len(fn.Parameters)),
		ReturnType:     returnType,
		HashableParams: make(map[string]struct{}),
	}
	c.collectHashableParameters(returnType, signature.HashableParams)

	// 检查每个形参的类型标注
	for _, param := range fn.Parameters {
		if param.DeclaredType == nil {
			return fmt.Errorf(
				"parameter %q requires a type annotation",
				param.Name,
			)
		}

		kind, err := c.resolveType(param.DeclaredType, parameters)
		if err != nil {
			return err
		}

		signature.ParameterTypes = append(
			signature.ParameterTypes,
			kind,
		)
		c.collectHashableParameters(kind, signature.HashableParams)
	}

	// 登记经过验证的完整函数签名
	c.signatures[symbol] = signature

	return nil
}

// SignatureOf 查询标识符绑定的函数签名
func (c *Checker) SignatureOf(
	ident *ast.Identifier,
) (FunctionSignature, bool) {
	if c.resolver == nil || ident == nil {
		return FunctionSignature{}, false
	}

	symbol, ok := c.resolver.BindingOf(ident)
	if !ok {
		return FunctionSignature{}, false
	}

	signature, ok := c.signatures[symbol]
	return signature, ok
}
