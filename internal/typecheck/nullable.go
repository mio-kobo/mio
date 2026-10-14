package typecheck

import (
	"fmt"

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
		if needsContext && arrayType.IsArray() {
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
			return arrayType, nil
		}
	}
	return c.checkExpression(expr)
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
