package parser

import (
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 测试循环及控制语句生成对应 AST
func TestParseWhileBreakAndContinue(t *testing.T) {
	l := lexer.New(`aida (hai) { tsuzuku; nukeru; }`, token.V02)
	program, err := New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Statements) != 1 {
		t.Fatalf("statement count = %d, want 1", len(program.Statements))
	}
	loop, ok := program.Statements[0].(*ast.WhileStatement)
	if !ok {
		t.Fatalf("statement = %T, want WhileStatement", program.Statements[0])
	}
	if _, ok := loop.Condition.(*ast.BooleanLiteral); !ok {
		t.Fatalf("condition = %T, want BooleanLiteral", loop.Condition)
	}
	if len(loop.Body.Statements) != 2 {
		t.Fatalf("body statement count = %d, want 2", len(loop.Body.Statements))
	}
	if _, ok := loop.Body.Statements[0].(*ast.ContinueStatement); !ok {
		t.Fatalf("first body statement = %T, want ContinueStatement", loop.Body.Statements[0])
	}
	if _, ok := loop.Body.Statements[1].(*ast.BreakStatement); !ok {
		t.Fatalf("second body statement = %T, want BreakStatement", loop.Body.Statements[1])
	}
}
