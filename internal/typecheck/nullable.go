package typecheck

import (
	"fmt"
	"strconv"

	"Mio/internal/ast"
	"Mio/internal/resolver"
	"Mio/internal/token"
)

// isAssignable 判断实际类型能否安全用于目标类型
func isAssignable(expected, actual Kind) bool {
	if expected == actual {
		return true
	}
	if expected.IsNullable() {
		return actual == KindNil || actual == expected.NonNullable()
	}
	return false
}

// checkExpressionExpected 在明确类型上下文中检查表达式
func (c *Checker) checkExpressionExpected(expr ast.Expression, expected Kind) (Kind, error) {
	if expected.IsNullable() {
		if _, nilLiteral := expr.(*ast.NilLiteral); !nilLiteral {
			return c.checkExpressionExpected(expr, expected.NonNullable())
		}
	}
	if literal, ok := expr.(*ast.IntegerLiteral); ok && expected.IsInteger() {
		if err := checkIntegerLiteralRange(literal.Value, expected, false); err != nil {
			return KindInvalid, err
		}
		literal.Type = expected.String()
		return expected, nil
	}
	if literal, ok := expr.(*ast.RuneLiteral); ok && expected.IsInteger() {
		if err := checkIntegerLiteralRange(literal.Value, expected, false); err != nil {
			return KindInvalid, err
		}
		literal.Type = expected.String()
		return expected, nil
	}
	if literal, ok := expr.(*ast.FloatLiteral); ok && expected == KindFloat32 {
		value, err := strconv.ParseFloat(literal.Raw, 32)
		if err != nil {
			return KindInvalid, err
		}
		literal.Value = float64(float32(value))
		literal.Bits = 32
		return KindFloat32, nil
	}
	if grouped, ok := expr.(*ast.GroupedExpression); ok {
		return c.checkExpressionExpected(grouped.Expr, expected)
	}
	if prefix, ok := expr.(*ast.PrefixExpression); ok && (expected.IsInteger() || expected == KindFloat32 || expected == KindFloat) {
		if literal, direct := prefix.Right.(*ast.IntegerLiteral); direct && prefix.Operator == token.Minus && expected.IsInteger() {
			if err := checkIntegerLiteralRange(literal.Value, expected, true); err != nil {
				return KindInvalid, err
			}
			literal.Type = expected.String()
			return expected, nil
		}
		if _, err := c.checkExpressionExpected(prefix.Right, expected); err != nil {
			return KindInvalid, err
		}
		return c.checkExpression(expr)
	}
	if infix, ok := expr.(*ast.InfixExpression); ok && (expected.IsInteger() || expected == KindFloat32 || expected == KindFloat) {
		if _, err := c.checkExpressionExpected(infix.Left, expected); err != nil {
			return KindInvalid, err
		}
		if _, err := c.checkExpressionExpected(infix.Right, expected); err != nil {
			return KindInvalid, err
		}
		return c.checkExpression(expr)
	}
	if literal, ok := expr.(*ast.StructLiteral); ok {
		return c.checkStructLiteral(literal, expected)
	}
	if call, ok := expr.(*ast.CallExpression); ok {
		return c.checkCall(call, expected)
	}
	if literal, ok := expr.(*ast.ArrayLiteral); ok {
		needsContext := len(literal.Elements) == 0
		for _, element := range literal.Elements {
			if _, ok := element.(*ast.NilLiteral); ok {
				needsContext = true
				break
			}
		}
		arrayType := expected
		if arrayType.IsNullable() {
			arrayType = arrayType.NonNullable()
		}
		if arrayType.IsArray() && (needsContext || arrayType.ElementKind() == KindFloat32 || arrayType.ElementKind().IsInteger()) {
			for _, element := range literal.Elements {
				actual, err := c.checkExpressionExpected(element, arrayType.ElementKind())
				if err != nil {
					return KindInvalid, err
				}
				if !isAssignable(arrayType.ElementKind(), actual) {
					cause := fmt.Errorf("array element requires %s, got %s", arrayType.ElementKind(), actual)
					return KindInvalid, typeDiagnostic("E0301", "数组元素类型不一致", element.Span(), arrayType.ElementKind().String(), actual.String(), cause)
				}
			}
			literal.ElementType = c.runtimeTypeName(arrayType.ElementKind())
			return arrayType, nil
		}
	}
	return c.checkExpression(expr)
}

func contextualIntegerExpression(expr ast.Expression) bool {
	switch n := expr.(type) {
	case *ast.IntegerLiteral:
		return n.Type == "" || n.Type == "int64"
	case *ast.RuneLiteral:
		return n.Type == "" || n.Type == "int32"
	case *ast.GroupedExpression:
		return contextualIntegerExpression(n.Expr)
	case *ast.PrefixExpression:
		return n.Operator == token.Minus && contextualIntegerExpression(n.Right)
	default:
		return false
	}
}

func checkIntegerLiteralRange(value int64, kind Kind, negative bool) error {
	if negative {
		if !kind.IsSignedInteger() {
			return fmt.Errorf("negative integer literal is outside %s range", kind)
		}
		limits := map[Kind]int64{KindInt8: 128, KindInt16: 32768, KindInt32: 2147483648, KindInt: -1 << 63}
		limit := limits[kind]
		if kind == KindInt {
			return nil
		}
		if value > limit {
			return fmt.Errorf("integer literal -%d is outside %s range", value, kind)
		}
		return nil
	}
	maxima := map[Kind]int64{
		KindInt8: 127, KindInt16: 32767, KindInt32: 2147483647, KindInt: 1<<63 - 1,
		KindUint8: 255, KindUint16: 65535, KindUint32: 4294967295,
	}
	if value < 0 || value > maxima[kind] {
		return fmt.Errorf("integer literal %d is outside %s range", value, kind)
	}
	return nil
}

func commonArrayElement(left, right Kind) Kind {
	if left == right {
		return left
	}
	if left == KindNil && right != KindNil {
		return NullableOf(right)
	}
	if right == KindNil && left != KindNil {
		return NullableOf(left)
	}
	if left.IsNullable() && left.NonNullable() == right {
		return left
	}
	if right.IsNullable() && right.NonNullable() == left {
		return right
	}
	return KindInvalid
}

func (c *Checker) declaredNullableExpression(expr ast.Expression) bool {
	identifier, ok := groupedIdentifier(expr)
	if !ok || c.resolver == nil {
		return false
	}
	symbol, ok := c.resolver.BindingOf(identifier)
	return ok && c.types[symbol].IsNullable()
}

func groupedIdentifier(expr ast.Expression) (*ast.Identifier, bool) {
	for {
		group, ok := expr.(*ast.GroupedExpression)
		if !ok {
			break
		}
		expr = group.Expr
	}
	identifier, ok := expr.(*ast.Identifier)
	return identifier, ok
}

func groupedNil(expr ast.Expression) bool {
	for {
		group, ok := expr.(*ast.GroupedExpression)
		if !ok {
			break
		}
		expr = group.Expr
	}
	_, ok := expr.(*ast.NilLiteral)
	return ok
}

func (c *Checker) conditionNarrowing(expr ast.Expression) (resolver.Symbol, bool, bool) {
	group, ok := expr.(*ast.GroupedExpression)
	if ok {
		return c.conditionNarrowing(group.Expr)
	}
	infix, ok := expr.(*ast.InfixExpression)
	if !ok || (infix.Operator != token.Equal && infix.Operator != token.NotEqual) {
		return resolver.Symbol{}, false, false
	}
	identifier, nilOnRight := groupedIdentifier(infix.Left)
	if !groupedNil(infix.Right) {
		identifier, nilOnRight = groupedIdentifier(infix.Right)
		if !groupedNil(infix.Left) {
			return resolver.Symbol{}, false, false
		}
	}
	if !nilOnRight || identifier == nil {
		return resolver.Symbol{}, false, false
	}
	symbol, ok := c.resolver.BindingOf(identifier)
	if !ok || !c.types[symbol].IsNullable() {
		return resolver.Symbol{}, false, false
	}
	return symbol, infix.Operator == token.NotEqual, true
}

func copyNarrowings(source map[resolver.Symbol]Kind) map[resolver.Symbol]Kind {
	result := make(map[resolver.Symbol]Kind, len(source))
	for symbol, kind := range source {
		result[symbol] = kind
	}
	return result
}

func copyVersions(source map[resolver.Symbol]uint64) map[resolver.Symbol]uint64 {
	result := make(map[resolver.Symbol]uint64, len(source))
	for symbol, version := range source {
		result[symbol] = version
	}
	return result
}

// checkBlockWithNarrowing 在分支范围内应用非空事实
func (c *Checker) checkBlockWithNarrowing(block *ast.BlockStatement, symbol resolver.Symbol, apply bool) error {
	savedFacts := copyNarrowings(c.narrowings)
	savedVersions := copyVersions(c.factVersions)
	if apply {
		c.narrowings[symbol] = c.types[symbol].NonNullable()
	}
	err := c.checkBlock(block)
	currentVersions := copyVersions(c.factVersions)
	c.narrowings = savedFacts
	for narrowed := range c.narrowings {
		if currentVersions[narrowed] != savedVersions[narrowed] {
			delete(c.narrowings, narrowed)
		}
	}
	return err
}

func (c *Checker) invalidateGlobalFacts() {
	for symbol := range c.globals {
		delete(c.narrowings, symbol)
		c.factVersions[symbol]++
	}
}

// invalidateAllFacts 清除可能被闭包修改的收窄事实
func (c *Checker) invalidateAllFacts() {
	for symbol := range c.narrowings {
		delete(c.narrowings, symbol)
		c.factVersions[symbol]++
	}
}
