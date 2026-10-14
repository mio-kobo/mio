package parser

import (
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 测试数值转换解析为独立表达式
func TestParseNumericConversionExpression(t *testing.T) {
	l := lexer.New(`int64(float64(10));`, token.V01)
	program, err := New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	statement := program.Statements[0].(*ast.ExpressionStatement)
	outer, ok := statement.Expr.(*ast.ConversionExpression)
	if !ok || outer.Target != "int64" || len(outer.Arguments) != 1 {
		t.Fatalf("outer conversion = %#v", statement.Expr)
	}
	inner, ok := outer.Arguments[0].(*ast.ConversionExpression)
	if !ok || inner.Target != "float64" || len(inner.Arguments) != 1 {
		t.Fatalf("inner conversion = %#v", outer.Arguments[0])
	}
}
