package parser

import (
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 测试字符与字符串生成不同 AST 节点
func TestParseRuneLiteral(t *testing.T) {
	l, err := lexer.NewChecked(`'你'; "你";`, token.V05)
	if err != nil {
		t.Fatal(err)
	}
	program, err := New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	runeStatement := program.Statements[0].(*ast.ExpressionStatement)
	runeLiteral, ok := runeStatement.Expr.(*ast.RuneLiteral)
	if !ok || runeLiteral.Value != int64('你') {
		t.Fatalf("rune=%#v", runeStatement.Expr)
	}
	stringStatement := program.Statements[1].(*ast.ExpressionStatement)
	if _, ok := stringStatement.Expr.(*ast.StringLiteral); !ok {
		t.Fatalf("string=%T", stringStatement.Expr)
	}
}
