package typecheck

import (
	"fmt"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/cabi"
	"Mio/internal/ffi"
	"Mio/internal/resolver"
)

// FunctionSignature 保存函数参数与返回类型
type FunctionSignature struct {
	TypeParams     []string
	Constraints    map[string]string
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
	case "cptr":
		return KindCPtr, nil
	case "uint64":
		return KindUint64, nil
	case "int8":
		return KindInt8, nil
	case "int16":
		return KindInt16, nil
	case "int32", "rune":
		return KindInt32, nil
	case "int64":
		return KindInt, nil
	case "uint8":
		return KindUint8, nil
	case "uint16":
		return KindUint16, nil
	case "uint32":
		return KindUint32, nil

	case "float64":
		return KindFloat, nil

	case "float32":
		return KindFloat32, nil

	case "bool":
		return KindBool, nil

	case "string":
		return KindString, nil

	case "unit":
		return KindUnit, nil

	case "resource":
		return KindResource, nil

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
	if fn.CABI {
		params := make([]string, len(fn.Parameters))
		for i, p := range fn.Parameters {
			if p.DeclaredType == nil || p.DeclaredType.Element != nil || len(p.DeclaredType.Args) != 0 {
				return fmt.Errorf("E_FFI_TYPE explicit scalar C parameter required")
			}
			params[i] = p.DeclaredType.Name
		}
		if fn.ReturnType == nil || fn.ReturnType.Element != nil || len(fn.ReturnType.Args) != 0 || len(fn.TypeParams) != 0 {
			return fmt.Errorf("E_FFI_TYPE explicit scalar C return required")
		}
		if err := ffi.Validate(ffi.Key(fn.CDLL, fn.CSymbol, fn.ReturnType.Name, params), len(params), fn.ReturnType.Name != "unit"); err != nil {
			return err
		}
		if c.cPrototypes == nil {
			c.cPrototypes = make(map[string]string)
		}
		identity := strings.ToLower(fn.CDLL) + "|" + fn.CSymbol
		prototype := strings.Join(append([]string{fn.ReturnType.Name}, params...), "|")
		if previous, ok := c.cPrototypes[identity]; ok && previous != prototype {
			return fmt.Errorf("E_FFI_PROTOTYPE conflicting C export prototypes")
		}
		c.cPrototypes[identity] = prototype
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

	if fn.CSymbol != "" && !fn.CABI {
		const prototypeError = "E_CABI C ABI requires a fixed prototype with explicit scalar parameters and a scalar or unit result"
		if fn.ReturnType == nil || len(fn.TypeParams) != 0 || len(fn.Parameters) > 256 ||
			!cabi.ValidSymbol(fn.CSymbol) || len(fn.Body.Statements) != 0 ||
			cabi.TypeCode(fn.ReturnType.Name) == "" || len(fn.ReturnType.Args) != 0 {
			return fmt.Errorf("%s", prototypeError)
		}
		for _, param := range fn.Parameters {
			if param.DeclaredType == nil || len(param.DeclaredType.Args) != 0 {
				return fmt.Errorf("%s", prototypeError)
			}
			if code := cabi.TypeCode(param.DeclaredType.Name); code == "" || code == "v" {
				return fmt.Errorf("%s", prototypeError)
			}
		}
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
	for parameter, constraint := range fn.Constraints {
		if _, ok := parameterSet(fn.TypeParams)[parameter]; !ok {
			return fmt.Errorf("constraint for unknown type parameter %s", parameter)
		}
		if constraint != "Number" && constraint != "Integer" && constraint != "Signed" && constraint != "Float" {
			return fmt.Errorf("unsupported generic constraint %q", constraint)
		}
	}

	parameters := parameterSet(fn.TypeParams)
	returnType, err := c.resolveType(fn.ReturnType, parameters)
	if err != nil {
		return err
	}

	signature := FunctionSignature{
		TypeParams:     append([]string(nil), fn.TypeParams...),
		Constraints:    fn.Constraints,
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
