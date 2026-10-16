package typecheck

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/resolver"
	"Mio/internal/token"
)

func typeDiagnostic(code, message string, span ast.Span, expected, actual string, cause error) error {
	d := diagnostic.NewError(code, message, diagnostic.FromPositions(span.Start, span.End)).WithTypes(expected, actual).WithCause(cause)
	if numericTypeName(expected) && numericTypeName(actual) && expected != actual {
		d.WithHelp(fmt.Sprintf("请使用 %s(...) 进行显式转换", expected))
	}
	return d
}

func numericTypeName(name string) bool {
	switch name {
	case "int8", "int16", "int32", "int64", "rune", "uint8", "uint16", "uint32", "float32", "float64":
		return true
	default:
		return false
	}
}

// Checker 负责静态类型检查
type Checker struct {
	resolver     *resolver.Resolver
	types        map[resolver.Symbol]Kind
	signatures   map[resolver.Symbol]FunctionSignature
	narrowings   map[resolver.Symbol]Kind
	factVersions map[resolver.Symbol]uint64
	globals      map[resolver.Symbol]struct{}
	structs      map[string]StructDefinition
	enums        map[string]EnumDefinition
	nodeTypes    map[ast.Expression]Kind

	returnTypes         []Kind
	loopDepth           int
	functionDepth       int
	typeParameterScopes []map[string]struct{}
	constraintScopes    []map[string]string
}

func (c *Checker) numeric(kind Kind) bool {
	if kind.IsInteger() || kind == KindFloat32 || kind == KindFloat {
		return true
	}
	if !kind.IsTypeParameter() || len(c.constraintScopes) == 0 {
		return false
	}
	constraint := c.constraintScopes[len(c.constraintScopes)-1][kind.TypeParameterName()]
	return constraint == "Number" || constraint == "Integer" || constraint == "Signed" || constraint == "Float"
}

func (c *Checker) signed(kind Kind) bool {
	if kind.IsSignedInteger() || kind == KindFloat32 || kind == KindFloat {
		return true
	}
	if !kind.IsTypeParameter() || len(c.constraintScopes) == 0 {
		return false
	}
	constraint := c.constraintScopes[len(c.constraintScopes)-1][kind.TypeParameterName()]
	return constraint == "Signed" || constraint == "Float"
}

func (c *Checker) floating(kind Kind) bool {
	if kind == KindFloat32 || kind == KindFloat {
		return true
	}
	if !kind.IsTypeParameter() || len(c.constraintScopes) == 0 {
		return false
	}
	return c.constraintScopes[len(c.constraintScopes)-1][kind.TypeParameterName()] == "Float"
}

func (c *Checker) activeTypeParameters() map[string]struct{} {
	if len(c.typeParameterScopes) == 0 {
		return nil
	}
	return c.typeParameterScopes[len(c.typeParameterScopes)-1]
}

// New 创建类型检查器
func New() *Checker {
	return &Checker{
		types:        make(map[resolver.Symbol]Kind),
		signatures:   make(map[resolver.Symbol]FunctionSignature),
		narrowings:   make(map[resolver.Symbol]Kind),
		factVersions: make(map[resolver.Symbol]uint64),
		globals:      make(map[resolver.Symbol]struct{}),
		structs:      make(map[string]StructDefinition),
		enums:        make(map[string]EnumDefinition),
		nodeTypes:    make(map[ast.Expression]Kind),
	}
}

// NewWithResolver 创建带符号绑定的类型检查器
func NewWithResolver(r *resolver.Resolver) *Checker {
	c := New()
	c.resolver = r
	return c
}

// checkExpression 推断并检查表达式的类型
func (c *Checker) checkExpression(expr ast.Expression) (result Kind, err error) {
	defer func() {
		if err == nil && expr != nil {
			c.nodeTypes[expr] = result
		}
	}()
	switch n := expr.(type) {

	// 基础字面量
	case *ast.IntegerLiteral:
		if kind := integerKind(n.Type); kind != KindInvalid {
			return kind, nil
		}
		if err := checkIntegerLiteral(n, KindInt, false); err != nil {
			return KindInvalid, err
		}
		return KindInt, nil

	case *ast.RuneLiteral:
		if kind := integerKind(n.Type); kind != KindInvalid {
			return kind, nil
		}
		return KindInt32, nil

	case *ast.FloatLiteral:
		if n.Bits == 32 {
			return KindFloat32, nil
		}
		return KindFloat, nil

	case *ast.BooleanLiteral:
		return KindBool, nil

	case *ast.StringLiteral:
		return KindString, nil

	case *ast.NilLiteral:
		return KindNil, nil

	case *ast.FunctionLiteral:
		return c.checkFunctionLiteral(n)

	case *ast.ArrayLiteral:
		if len(n.Elements) == 0 {
			cause := fmt.Errorf("cannot infer element type of empty array")
			return KindInvalid, diagnostic.NewError("E0305", "无法推断空数组的元素类型", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		elementType, err := c.checkExpression(n.Elements[0])
		if err != nil {
			return KindInvalid, err
		}
		for i, element := range n.Elements[1:] {
			actual, err := c.checkExpressionExpected(element, elementType)
			if err != nil {
				return KindInvalid, err
			}
			if elementType == KindInt && actual.IsInteger() && actual != KindInt && contextualIntegerExpression(n.Elements[0]) {
				elementType, err = c.checkExpressionExpected(n.Elements[0], actual)
				if err != nil {
					return KindInvalid, err
				}
			}
			common := commonArrayElement(elementType, actual)
			if common == KindInvalid {
				cause := fmt.Errorf("array element %d: expected %s, got %s", i+2, elementType, actual)
				return KindInvalid, typeDiagnostic("E0301", "数组元素类型不一致", element.Span(), elementType.String(), actual.String(), cause)
			}
			elementType = common
		}
		arrayType := ArrayOf(elementType)
		if arrayType == KindInvalid {
			return KindInvalid, fmt.Errorf("array element type must be a scalar type, got %s", elementType)
		}
		n.ElementType = c.runtimeTypeName(elementType)
		return arrayType, nil

	case *ast.IndexExpression:
		arrayType, err := c.checkExpression(n.Left)
		if err != nil {
			return KindInvalid, err
		}
		if arrayType.IsMap() {
			_, args, _ := arrayType.NamedParts()
			indexType, err := c.checkExpressionExpected(n.Index, args[0])
			if err != nil {
				return KindInvalid, err
			}
			if !isAssignable(args[0], indexType) {
				return KindInvalid, typeDiagnostic("E0301", "Map 键类型不一致", n.Index.Span(), args[0].String(), indexType.String(), fmt.Errorf("map key requires %s, got %s", args[0], indexType))
			}
			n.Map = true
			if args[1].IsNullable() {
				return args[1], nil
			}
			return NullableOf(args[1]), nil
		}
		if !arrayType.IsArray() || arrayType.IsNullable() {
			cause := fmt.Errorf("index target must be an array, got %s", arrayType)
			return KindInvalid, typeDiagnostic("E0305", "只能对数组使用索引", n.Left.Span(), "array", arrayType.String(), cause)
		}
		indexType, err := c.checkExpression(n.Index)
		if err != nil {
			return KindInvalid, err
		}
		if indexType != KindInt {
			cause := fmt.Errorf("array index must be int64, got %s", indexType)
			return KindInvalid, typeDiagnostic("E0305", "数组索引必须是 int64", n.Index.Span(), KindInt.String(), indexType.String(), cause)
		}
		return arrayType.ElementKind(), nil

	case *ast.StructLiteral:
		return c.checkStructLiteral(n, KindInvalid)

	case *ast.FieldExpression:
		left, err := c.checkExpression(n.Left)
		if err != nil {
			return KindInvalid, err
		}
		if left.IsNullable() {
			return KindInvalid, fmt.Errorf("nullable value %s must be narrowed before field access", left)
		}
		name, args, ok := left.NamedParts()
		definition, exists := c.structs[name]
		if !ok || !exists {
			return KindInvalid, fmt.Errorf("type %s has no readable fields", left)
		}
		bindings := make(map[string]Kind, len(args))
		for i, parameter := range definition.TypeParams {
			bindings[parameter] = args[i]
		}
		for index, field := range definition.Fields {
			if field.Name == n.Name {
				n.Index = index
				return substitute(field.Type, bindings), nil
			}
		}
		return KindInvalid, fmt.Errorf("struct %s has no field %s", name, n.Name)

	// 括号不会改变内部表达式的类型
	case *ast.GroupedExpression:
		return c.checkExpression(n.Expr)

	// 一元表达式
	case *ast.PrefixExpression:
		if literal := groupedIntegerLiteral(n.Right); literal != nil && n.Operator == token.Minus {
			if err := checkIntegerLiteral(literal, KindInt, true); err == nil {
				literal.Type = KindInt.String()
				return KindInt, nil
			}
		}
		right, err := c.checkExpression(n.Right)
		if err != nil {
			return KindInvalid, err
		}

		switch n.Operator {
		case token.Minus:
			if !c.signed(right) {
				return KindInvalid, fmt.Errorf(
					"operator '-' requires int64 or another signed number, got %s",
					right,
				)
			}

			return right, nil

		case token.Bang:
			if right != KindBool {
				return KindInvalid, fmt.Errorf(
					"operator '!' requires bool, got %s",
					right,
				)
			}

			return KindBool, nil

		default:
			return KindInvalid, fmt.Errorf(
				"unsupported prefix operator %s",
				n.Operator,
			)
		}

	// 二元表达式
	case *ast.InfixExpression:
		// 检查左操作数类型
		left, err := c.checkExpression(n.Left)
		if err != nil {
			return KindInvalid, err
		}

		// 检查右操作数类型
		right, err := c.checkExpression(n.Right)
		if err != nil {
			return KindInvalid, err
		}
		if left != right {
			if left.IsInteger() && left != KindInt && contextualIntegerExpression(n.Right) {
				right, err = c.checkExpressionExpected(n.Right, left)
			} else if right.IsInteger() && right != KindInt && contextualIntegerExpression(n.Left) {
				left, err = c.checkExpressionExpected(n.Left, right)
			}
			if err != nil {
				return KindInvalid, err
			}
		}

		// 根据运算符验证操作数类型
		switch n.Operator {

		// 算术运算
		case token.Plus,
			token.Minus,
			token.Asterisk,
			token.Slash:

			if left != right || !c.numeric(left) {
				cause := fmt.Errorf(
					"operator %s requires int64 operands, matching float32 operands, or matching float64 operands, got %s and %s",
					n.Operator,
					left,
					right,
				)
				return KindInvalid, typeDiagnostic("E0302", "数值运算不允许隐式类型转换", n.Span(), left.String(), right.String(), cause)
			}

			return left, nil

		// 大小比较
		case token.Less,
			token.LessEqual,
			token.Greater,
			token.GreaterEqual:

			if left != right || !c.numeric(left) {
				cause := fmt.Errorf(
					"operator %s requires int64 operands, matching float32 operands, or matching float64 operands, got %s and %s",
					n.Operator,
					left,
					right,
				)
				return KindInvalid, typeDiagnostic("E0302", "数值比较不允许隐式类型转换", n.Span(), left.String(), right.String(), cause)
			}

			return KindBool, nil

		// 相等比较
		case token.Equal, token.NotEqual:
			if (left == KindNil && (right.IsNullable() || c.declaredNullableExpression(n.Right))) ||
				(right == KindNil && (left.IsNullable() || c.declaredNullableExpression(n.Left))) {
				return KindBool, nil
			}
			if left != right {
				cause := fmt.Errorf(
					"operator %s requires matching types, got %s and %s",
					n.Operator,
					left,
					right,
				)
				return KindInvalid, typeDiagnostic("E0301", "比较两侧的类型不一致", n.Right.Span(), left.String(), right.String(), cause)
			}
			if c.numeric(left) {
				return KindBool, nil
			}

			// 基础非空类型继续使用原有相等规则
			switch left {
			case KindInt8, KindInt16, KindInt32, KindInt, KindUint8, KindUint16, KindUint32, KindFloat32, KindFloat, KindBool, KindString:
				return KindBool, nil

			default:
				return KindInvalid, fmt.Errorf(
					"operator %s does not support %s operands",
					n.Operator,
					left,
				)
			}

		// 逻辑运算
		case token.And, token.Or:
			if left != KindBool || right != KindBool {
				return KindInvalid, fmt.Errorf(
					"operator %s requires bool operands, got %s and %s",
					n.Operator,
					left,
					right,
				)
			}

			return KindBool, nil

		default:
			return KindInvalid, fmt.Errorf(
				"unsupported infix operator %s",
				n.Operator,
			)
		}

	case *ast.Identifier:
		if c.resolver == nil {
			return KindInvalid, fmt.Errorf(
				"identifier %q requires resolved symbols",
				n.Name,
			)
		}

		// 获取 Resolver 确定的符号
		symbol, ok := c.resolver.BindingOf(n)
		if !ok {
			return KindInvalid, fmt.Errorf(
				"missing symbol binding for %q",
				n.Name,
			)
		}

		if symbol.Kind == resolver.SymbolFunction {
			signature, ok := c.signatures[symbol]
			if !ok {
				return KindInvalid, fmt.Errorf("function signature for %q is not available", n.Name)
			}
			if len(signature.TypeParams) != 0 {
				return KindInvalid, fmt.Errorf("generic function %q requires call-site inference", n.Name)
			}
			return FunctionOf(signature.ParameterTypes, signature.ReturnType), nil
		}
		if symbol.Kind == resolver.SymbolType {
			return KindInvalid, fmt.Errorf("type %q is not a value", n.Name)
		}

		// 查询符号对应的静态类型
		kind, ok := c.types[symbol]
		if !ok {
			return KindInvalid, fmt.Errorf(
				"type of identifier %q is not available",
				n.Name,
			)
		}

		if narrowed, ok := c.narrowings[symbol]; ok {
			return narrowed, nil
		}
		return kind, nil

	case *ast.CallExpression:
		return c.checkCall(n, KindInvalid)

	case *ast.TypeApplyExpression:
		return KindInvalid, fmt.Errorf("explicit type arguments must be followed by a call or variant")

	case *ast.ConversionExpression:
		if len(n.Arguments) != 1 {
			cause := fmt.Errorf(
				"conversion %q expects 1 argument, got %d",
				n.Target,
				len(n.Arguments),
			)
			return KindInvalid, diagnostic.NewError("E0303", "数值转换需要一个参数", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		source, err := c.checkExpression(n.Arguments[0])
		if err != nil {
			return KindInvalid, err
		}
		target := integerKind(n.Target)
		if target.IsInteger() {
			if target == KindInt && (!source.IsInteger() || source == KindInt) && source != KindFloat && source != KindFloat32 {
				return KindInvalid, fmt.Errorf("conversion to int64 requires float64, got %s", source)
			}
			if target == KindInt && source == KindInt {
				return KindInvalid, fmt.Errorf("conversion to int64 requires float64, got int64")
			}
			if !source.IsInteger() && source != KindFloat && source != KindFloat32 {
				return KindInvalid, fmt.Errorf("conversion to %s requires a numeric value, got %s", target, source)
			}
			if source == target {
				return KindInvalid, fmt.Errorf("conversion to %s requires a different numeric type", target)
			}
			n.Source = source.String()
			return target, nil
		}
		switch n.Target {
		case "float32":
			if !source.IsInteger() && source != KindFloat {
				return KindInvalid, fmt.Errorf("conversion to float32 requires an integer or float64, got %s", source)
			}
			n.Source = source.String()
			return KindFloat32, nil
		case "float64":
			if !source.IsInteger() && source != KindFloat32 {
				cause := fmt.Errorf("conversion to float64 requires int64, got %s", source)
				return KindInvalid, typeDiagnostic("E0302", "无效的数值转换", n.Arguments[0].Span(), KindInt.String(), source.String(), cause)
			}
			n.Source = source.String()
			return KindFloat, nil
		default:
			return KindInvalid, fmt.Errorf("unsupported conversion target %q", n.Target)
		}

	default:
		return KindInvalid, fmt.Errorf(
			"unsupported expression %T",
			expr,
		)
	}
}

// TypeOf 查询已检查表达式的静态类型
func (c *Checker) TypeOf(expr ast.Expression) (Kind, bool) {
	kind, ok := c.nodeTypes[expr]
	return kind, ok
}

// TypeOfIdentifier 查询已绑定标识符的声明类型
func (c *Checker) TypeOfIdentifier(identifier *ast.Identifier) (Kind, bool) {
	if c.resolver == nil || identifier == nil {
		return KindInvalid, false
	}
	symbol, ok := c.resolver.BindingOf(identifier)
	if !ok {
		return KindInvalid, false
	}
	kind, ok := c.types[symbol]
	return kind, ok
}

func integerKind(name string) Kind {
	switch name {
	case "int8":
		return KindInt8
	case "int16":
		return KindInt16
	case "int32":
		return KindInt32
	case "rune":
		return KindInt32
	case "int64":
		return KindInt
	case "uint8":
		return KindUint8
	case "uint16":
		return KindUint16
	case "uint32":
		return KindUint32
	default:
		return KindInvalid
	}
}
