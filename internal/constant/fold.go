// Package constant 负责 teisu 常量求值与替换。
package constant

import (
	"fmt"
	"math"
	"strconv"

	"Mio/internal/ast"
	"Mio/internal/token"
)

type kind uint8

const (
	kindInvalid kind = iota
	kindInt8
	kindInt16
	kindInt32
	kindInt
	kindUint8
	kindUint16
	kindUint32
	kindFloat32
	kindFloat64
	kindBool
	kindString
)

type result struct {
	kind kind
	i    int64
	f    float64
	b    bool
	s    string
}

type evaluator struct {
	declarations map[string]*ast.ConstStatement
	values       map[string]result
	visiting     map[string]bool
}

// Fold 求值所有常量并移除运行时声明
func Fold(program *ast.Program) error {
	if program == nil {
		return fmt.Errorf("cannot fold constants in a nil program")
	}
	e := &evaluator{declarations: make(map[string]*ast.ConstStatement), values: make(map[string]result), visiting: make(map[string]bool)}
	for _, statement := range program.Statements {
		declaration, ok := statement.(*ast.ConstStatement)
		if !ok {
			continue
		}
		if _, exists := e.declarations[declaration.Name.Name]; exists {
			return fmt.Errorf("constant %q is declared more than once", declaration.Name.Name)
		}
		e.declarations[declaration.Name.Name] = declaration
	}
	for _, statement := range program.Statements {
		var name string
		switch declaration := statement.(type) {
		case *ast.LetStatement:
			name = declaration.Name.Name
		case *ast.FunctionStatement:
			name = declaration.Name.Name
		case *ast.StructStatement:
			name = declaration.Name.Name
		case *ast.EnumStatement:
			name = declaration.Name.Name
		}
		if name != "" {
			if _, exists := e.declarations[name]; exists {
				return fmt.Errorf("constant %q conflicts with another declaration", name)
			}
		}
	}
	for name := range e.declarations {
		if _, err := e.constant(name); err != nil {
			return err
		}
	}
	kept := program.Statements[:0]
	for _, statement := range program.Statements {
		if _, ok := statement.(*ast.ConstStatement); ok {
			continue
		}
		if err := e.statement(statement, make(map[string]bool)); err != nil {
			return err
		}
		kept = append(kept, statement)
	}
	program.Statements = kept
	return nil
}

func (e *evaluator) constant(name string) (result, error) {
	if value, ok := e.values[name]; ok {
		return value, nil
	}
	declaration, ok := e.declarations[name]
	if !ok {
		return result{}, fmt.Errorf("unknown constant %q", name)
	}
	if e.visiting[name] {
		return result{}, fmt.Errorf("constant dependency cycle includes %q", name)
	}
	e.visiting[name] = true
	expected, err := declaredKind(declaration.Name.DeclaredType)
	if err != nil {
		return result{}, fmt.Errorf("constant %q: %w", name, err)
	}
	value, err := e.expression(declaration.Value, expected)
	delete(e.visiting, name)
	if err != nil {
		return result{}, fmt.Errorf("constant %q: %w", name, err)
	}
	if expected != kindInvalid && value.kind != expected {
		return result{}, fmt.Errorf("constant %q requires %s, got %s", name, kindName(expected), kindName(value.kind))
	}
	e.values[name] = value
	return value, nil
}

func declaredKind(ref *ast.TypeRef) (kind, error) {
	if ref == nil {
		return kindInvalid, nil
	}
	switch ref.Name {
	case "int8":
		return kindInt8, nil
	case "int16":
		return kindInt16, nil
	case "int32", "rune":
		return kindInt32, nil
	case "int64":
		return kindInt, nil
	case "uint8":
		return kindUint8, nil
	case "uint16":
		return kindUint16, nil
	case "uint32":
		return kindUint32, nil
	case "float32":
		return kindFloat32, nil
	case "float64":
		return kindFloat64, nil
	case "bool":
		return kindBool, nil
	case "string":
		return kindString, nil
	default:
		return kindInvalid, fmt.Errorf("teisu does not support mutable or composite type %s", ref.Name)
	}
}

func kindName(value kind) string {
	switch value {
	case kindInt8:
		return "int8"
	case kindInt16:
		return "int16"
	case kindInt32:
		return "int32"
	case kindInt:
		return "int64"
	case kindUint8:
		return "uint8"
	case kindUint16:
		return "uint16"
	case kindUint32:
		return "uint32"
	case kindFloat32:
		return "float32"
	case kindFloat64:
		return "float64"
	case kindBool:
		return "bool"
	case kindString:
		return "string"
	default:
		return "unknown"
	}
}

func (e *evaluator) expression(expression ast.Expression, expected kind) (result, error) {
	switch n := expression.(type) {
	case *ast.IntegerLiteral:
		integerKind := kindInt
		if isIntegerKind(expected) {
			integerKind = expected
			if err := checkConstantIntegerRange(n.Value, expected, false); err != nil {
				return result{}, err
			}
		}
		return result{kind: integerKind, i: n.Value}, nil
	case *ast.RuneLiteral:
		integerKind := kindInt32
		if isIntegerKind(expected) {
			integerKind = expected
			if err := checkConstantIntegerRange(n.Value, expected, false); err != nil {
				return result{}, err
			}
		}
		return result{kind: integerKind, i: n.Value}, nil
	case *ast.FloatLiteral:
		if expected == kindFloat32 {
			value, err := strconv.ParseFloat(n.Raw, 32)
			if err != nil {
				return result{}, err
			}
			return result{kind: kindFloat32, f: float64(float32(value))}, nil
		}
		return result{kind: kindFloat64, f: n.Value}, nil
	case *ast.BooleanLiteral:
		return result{kind: kindBool, b: n.Value}, nil
	case *ast.StringLiteral:
		return result{kind: kindString, s: n.Value}, nil
	case *ast.Identifier:
		return e.constant(n.Name)
	case *ast.GroupedExpression:
		return e.expression(n.Expr, expected)
	case *ast.PrefixExpression:
		if literal, ok := n.Right.(*ast.IntegerLiteral); ok && n.Operator == token.Minus && isIntegerKind(expected) {
			if err := checkConstantIntegerRange(literal.Value, expected, true); err != nil {
				return result{}, err
			}
			return result{kind: expected, i: wrapConstantInteger(-literal.Value, expected)}, nil
		}
		value, err := e.expression(n.Right, expected)
		if err != nil {
			return result{}, err
		}
		switch n.Operator {
		case token.Minus:
			switch value.kind {
			case kindInt8, kindInt16, kindInt32, kindInt:
				value.i = wrapConstantInteger(-value.i, value.kind)
			case kindFloat32:
				value.f = float64(-float32(value.f))
			case kindFloat64:
				value.f = -value.f
			default:
				return result{}, fmt.Errorf("unary '-' requires a number")
			}
			return value, nil
		case token.Bang:
			if value.kind != kindBool {
				return result{}, fmt.Errorf("unary '!' requires bool")
			}
			value.b = !value.b
			return value, nil
		}
	case *ast.InfixExpression:
		left, err := e.expression(n.Left, expected)
		if err != nil {
			return result{}, err
		}
		if left.kind == kindBool {
			if n.Operator == token.And && !left.b {
				return result{kind: kindBool, b: false}, nil
			}
			if n.Operator == token.Or && left.b {
				return result{kind: kindBool, b: true}, nil
			}
		}
		right, err := e.expression(n.Right, left.kind)
		if err != nil {
			return result{}, err
		}
		return binary(n.Operator, left, right)
	case *ast.ConversionExpression:
		if len(n.Arguments) != 1 {
			return result{}, fmt.Errorf("conversion %s expects one argument", n.Target)
		}
		value, err := e.expression(n.Arguments[0], kindInvalid)
		if err != nil {
			return result{}, err
		}
		return convert(n.Target, value)
	}
	return result{}, fmt.Errorf("expression %T is not a pure compile-time expression", expression)
}

func binary(operator token.Type, left, right result) (result, error) {
	if left.kind != right.kind {
		return result{}, fmt.Errorf("constant operands require matching types, got %s and %s", kindName(left.kind), kindName(right.kind))
	}
	if operator == token.Equal || operator == token.NotEqual {
		equal := false
		switch left.kind {
		case kindInt8, kindInt16, kindInt32, kindInt, kindUint8, kindUint16, kindUint32:
			equal = left.i == right.i
		case kindFloat32, kindFloat64:
			equal = left.f == right.f
		case kindBool:
			equal = left.b == right.b
		case kindString:
			equal = left.s == right.s
		}
		if operator == token.NotEqual {
			equal = !equal
		}
		return result{kind: kindBool, b: equal}, nil
	}
	if left.kind == kindBool {
		switch operator {
		case token.And:
			return result{kind: kindBool, b: left.b && right.b}, nil
		case token.Or:
			return result{kind: kindBool, b: left.b || right.b}, nil
		}
	}
	if isIntegerKind(left.kind) {
		switch operator {
		case token.Plus:
			return result{kind: left.kind, i: wrapConstantInteger(left.i+right.i, left.kind)}, nil
		case token.Minus:
			return result{kind: left.kind, i: wrapConstantInteger(left.i-right.i, left.kind)}, nil
		case token.Asterisk:
			return result{kind: left.kind, i: wrapConstantInteger(left.i*right.i, left.kind)}, nil
		case token.Slash:
			if right.i == 0 {
				return result{}, fmt.Errorf("integer division by zero")
			}
			if isSignedConstantInteger(left.kind) && left.i == constantMinimum(left.kind) && right.i == -1 {
				return result{kind: left.kind, i: left.i}, nil
			}
			if !isSignedConstantInteger(left.kind) {
				return result{kind: left.kind, i: int64(uint64(left.i) / uint64(right.i))}, nil
			}
			return result{kind: left.kind, i: left.i / right.i}, nil
		case token.Less, token.LessEqual, token.Greater, token.GreaterEqual:
			return result{kind: kindBool, b: compareInt(operator, left.i, right.i)}, nil
		}
	}
	if left.kind == kindFloat32 || left.kind == kindFloat64 {
		if operator == token.Less || operator == token.LessEqual || operator == token.Greater || operator == token.GreaterEqual {
			return result{kind: kindBool, b: compareFloat(operator, left.f, right.f)}, nil
		}
		if left.kind == kindFloat32 {
			left32, right32 := float32(left.f), float32(right.f)
			var value32 float32
			switch operator {
			case token.Plus:
				value32 = left32 + right32
			case token.Minus:
				value32 = left32 - right32
			case token.Asterisk:
				value32 = left32 * right32
			case token.Slash:
				value32 = left32 / right32
			default:
				return result{}, fmt.Errorf("unsupported constant operator %s", operator)
			}
			return result{kind: kindFloat32, f: float64(value32)}, nil
		}
		value := 0.0
		switch operator {
		case token.Plus:
			value = left.f + right.f
		case token.Minus:
			value = left.f - right.f
		case token.Asterisk:
			value = left.f * right.f
		case token.Slash:
			value = left.f / right.f
		default:
			return result{}, fmt.Errorf("unsupported constant operator %s", operator)
		}
		return result{kind: left.kind, f: value}, nil
	}
	return result{}, fmt.Errorf("unsupported constant operator %s", operator)
}

func compareInt(operator token.Type, left, right int64) bool {
	switch operator {
	case token.Less:
		return left < right
	case token.LessEqual:
		return left <= right
	case token.Greater:
		return left > right
	default:
		return left >= right
	}
}

func constantIntegerKind(name string) (kind, bool) {
	switch name {
	case "int8":
		return kindInt8, true
	case "int16":
		return kindInt16, true
	case "int32", "rune":
		return kindInt32, true
	case "int64":
		return kindInt, true
	case "uint8":
		return kindUint8, true
	case "uint16":
		return kindUint16, true
	case "uint32":
		return kindUint32, true
	default:
		return kindInvalid, false
	}
}

func isIntegerKind(value kind) bool {
	_, ok := constantIntegerKind(kindName(value))
	return ok
}

func isSignedConstantInteger(value kind) bool {
	return value == kindInt8 || value == kindInt16 || value == kindInt32 || value == kindInt
}

func constantWidth(value kind) uint {
	switch value {
	case kindInt8, kindUint8:
		return 8
	case kindInt16, kindUint16:
		return 16
	case kindInt32, kindUint32:
		return 32
	default:
		return 64
	}
}

func constantMinimum(value kind) int64 {
	if !isSignedConstantInteger(value) {
		return 0
	}
	width := constantWidth(value)
	if width == 64 {
		return math.MinInt64
	}
	return -int64(uint64(1) << (width - 1))
}

func constantMaximum(value kind) int64 {
	width := constantWidth(value)
	if isSignedConstantInteger(value) {
		if width == 64 {
			return math.MaxInt64
		}
		return int64(uint64(1)<<(width-1)) - 1
	}
	return int64(uint64(1)<<width) - 1
}

func checkConstantIntegerRange(value int64, target kind, negative bool) error {
	if negative {
		if target == kindInt {
			return nil
		}
		if !isSignedConstantInteger(target) || value > -constantMinimum(target) {
			return fmt.Errorf("integer literal -%d is outside %s range", value, kindName(target))
		}
		return nil
	}
	if value < 0 || value > constantMaximum(target) {
		return fmt.Errorf("integer literal %d is outside %s range", value, kindName(target))
	}
	return nil
}

func checkConvertedIntegerRange(number int64, source, target kind) error {
	if !isSignedConstantInteger(source) && number < 0 {
		return fmt.Errorf("integer value is outside %s range", kindName(target))
	}
	if number < constantMinimum(target) || number > constantMaximum(target) {
		return fmt.Errorf("integer value is outside %s range", kindName(target))
	}
	return nil
}

func wrapConstantInteger(number int64, target kind) int64 {
	width := constantWidth(target)
	if width == 64 {
		return number
	}
	mask := uint64(1)<<width - 1
	bits := uint64(number) & mask
	if isSignedConstantInteger(target) && bits&(uint64(1)<<(width-1)) != 0 {
		return int64(bits | ^mask)
	}
	return int64(bits)
}

func compareFloat(operator token.Type, left, right float64) bool {
	switch operator {
	case token.Less:
		return left < right
	case token.LessEqual:
		return left <= right
	case token.Greater:
		return left > right
	default:
		return left >= right
	}
}

func convert(target string, value result) (result, error) {
	targetKind, targetInteger := constantIntegerKind(target)
	if targetInteger {
		if isIntegerKind(value.kind) {
			if err := checkConvertedIntegerRange(value.i, value.kind, targetKind); err != nil {
				return result{}, err
			}
			return result{kind: targetKind, i: value.i}, nil
		}
		if value.kind == kindFloat32 || value.kind == kindFloat64 {
			truncated := math.Trunc(value.f)
			outside := truncated < float64(constantMinimum(targetKind)) || truncated > float64(constantMaximum(targetKind))
			if targetKind == kindInt {
				limit := math.Ldexp(1, 63)
				outside = truncated < -limit || truncated >= limit
			}
			if math.IsNaN(truncated) || math.IsInf(truncated, 0) || outside {
				return result{}, fmt.Errorf("floating value is outside %s range", target)
			}
			return result{kind: targetKind, i: int64(truncated)}, nil
		}
	}
	switch target {
	case "float32":
		switch value.kind {
		case kindInt8, kindInt16, kindInt32, kindInt, kindUint8, kindUint16, kindUint32:
			return result{kind: kindFloat32, f: float64(float32(value.i))}, nil
		case kindFloat64:
			return result{kind: kindFloat32, f: float64(float32(value.f))}, nil
		}
	case "float64":
		switch value.kind {
		case kindInt8, kindInt16, kindInt32, kindInt, kindUint8, kindUint16, kindUint32:
			return result{kind: kindFloat64, f: float64(value.i)}, nil
		case kindFloat32:
			return result{kind: kindFloat64, f: value.f}, nil
		}
	case "int64":
		if value.kind == kindFloat32 || value.kind == kindFloat64 {
			limit := math.Ldexp(1, 63)
			if math.IsNaN(value.f) || math.IsInf(value.f, 0) || value.f < -limit || value.f >= limit {
				return result{}, fmt.Errorf("floating value is outside int64 range")
			}
			return result{kind: kindInt, i: int64(math.Trunc(value.f))}, nil
		}
	}
	return result{}, fmt.Errorf("cannot convert %s to %s", kindName(value.kind), target)
}

func literal(value result, span ast.Span) ast.Expression {
	switch value.kind {
	case kindInt8, kindInt16, kindInt32, kindInt, kindUint8, kindUint16, kindUint32:
		return &ast.IntegerLiteral{Value: value.i, Type: kindName(value.kind), Range: span}
	case kindFloat32:
		return &ast.FloatLiteral{Value: value.f, Raw: strconv.FormatFloat(value.f, 'g', -1, 32), Bits: 32, Range: span}
	case kindFloat64:
		return &ast.FloatLiteral{Value: value.f, Raw: strconv.FormatFloat(value.f, 'g', -1, 64), Bits: 64, Range: span}
	case kindBool:
		return &ast.BooleanLiteral{Value: value.b, Range: span}
	case kindString:
		return &ast.StringLiteral{Value: value.s, Range: span}
	default:
		return nil
	}
}

func (e *evaluator) replace(expression *ast.Expression, locals map[string]bool) error {
	if identifier, ok := (*expression).(*ast.Identifier); ok {
		if _, constant := e.declarations[identifier.Name]; constant && !locals[identifier.Name] {
			value, err := e.constant(identifier.Name)
			if err != nil {
				return err
			}
			*expression = literal(value, identifier.Span())
			return nil
		}
	}
	switch n := (*expression).(type) {
	case *ast.GroupedExpression:
		return e.replace(&n.Expr, locals)
	case *ast.PrefixExpression:
		return e.replace(&n.Right, locals)
	case *ast.InfixExpression:
		if err := e.replace(&n.Left, locals); err != nil {
			return err
		}
		return e.replace(&n.Right, locals)
	case *ast.CallExpression:
		if err := e.replace(&n.Function, locals); err != nil {
			return err
		}
		for i := range n.Arguments {
			if err := e.replace(&n.Arguments[i], locals); err != nil {
				return err
			}
		}
	case *ast.TypeApplyExpression:
		return e.replace(&n.Function, locals)
	case *ast.ConversionExpression:
		for i := range n.Arguments {
			if err := e.replace(&n.Arguments[i], locals); err != nil {
				return err
			}
		}
	case *ast.ArrayLiteral:
		for i := range n.Elements {
			if err := e.replace(&n.Elements[i], locals); err != nil {
				return err
			}
		}
	case *ast.IndexExpression:
		if err := e.replace(&n.Left, locals); err != nil {
			return err
		}
		return e.replace(&n.Index, locals)
	case *ast.FieldExpression:
		return e.replace(&n.Left, locals)
	case *ast.StructLiteral:
		for i := range n.Fields {
			if err := e.replace(&n.Fields[i].Value, locals); err != nil {
				return err
			}
		}
	case *ast.FunctionLiteral:
		inner := cloneLocals(locals)
		for _, parameter := range n.Parameters {
			inner[parameter.Name] = true
		}
		return e.block(n.Body, inner)
	}
	return nil
}

func cloneLocals(locals map[string]bool) map[string]bool {
	copy := make(map[string]bool, len(locals))
	for name := range locals {
		copy[name] = true
	}
	return copy
}

func (e *evaluator) statement(statement ast.Statement, locals map[string]bool) error {
	switch n := statement.(type) {
	case *ast.LetStatement:
		if err := e.replace(&n.Value, locals); err != nil {
			return err
		}
		locals[n.Name.Name] = true
		return nil
	case *ast.AssignStatement:
		return e.replace(&n.Value, locals)
	case *ast.IndexAssignStatement:
		var target ast.Expression = n.Target
		if err := e.replace(&target, locals); err != nil {
			return err
		}
		return e.replace(&n.Value, locals)
	case *ast.ExpressionStatement:
		return e.replace(&n.Expr, locals)
	case *ast.ReturnStatement:
		if n.Value != nil {
			return e.replace(&n.Value, locals)
		}
	case *ast.IfStatement:
		if err := e.replace(&n.Condition, locals); err != nil {
			return err
		}
		if err := e.block(n.Consequence, cloneLocals(locals)); err != nil {
			return err
		}
		if n.Alternative != nil {
			return e.block(n.Alternative, cloneLocals(locals))
		}
	case *ast.WhileStatement:
		if err := e.replace(&n.Condition, locals); err != nil {
			return err
		}
		return e.block(n.Body, cloneLocals(locals))
	case *ast.FunctionStatement:
		inner := make(map[string]bool)
		for _, parameter := range n.Parameters {
			inner[parameter.Name] = true
		}
		return e.block(n.Body, inner)
	case *ast.MatchStatement:
		if err := e.replace(&n.Value, locals); err != nil {
			return err
		}
		for i := range n.Arms {
			inner := cloneLocals(locals)
			for _, binding := range n.Arms[i].Bindings {
				inner[binding.Name] = true
			}
			if err := e.block(n.Arms[i].Body, inner); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *evaluator) block(block *ast.BlockStatement, locals map[string]bool) error {
	for _, statement := range block.Statements {
		if err := e.statement(statement, locals); err != nil {
			return err
		}
	}
	return nil
}
