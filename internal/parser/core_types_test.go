package parser

import (
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 测试核心类型声明与表达式语法
func TestParseCoreTypeSyntax(t *testing.T) {
	source := `
kata Box<T> { value: T; }
sentaku Result<T, E> { Ok(T); Err(E); }
waza apply<T, U>(value: T, f: (T) -> U) -> U { kaesu f(value); }
kaku box = Box { value: 42 };
awase (Result[int64, string].Ok(box.value)) {
    baai Ok(value) { chant(value); }
    baai Err(message) { chant(message); }
}
`
	p := New(lexer.New(source, token.V04))
	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Statements) != 5 {
		t.Fatalf("statements=%d", len(program.Statements))
	}
	if declaration, ok := program.Statements[0].(*ast.StructStatement); !ok || declaration.Name.Name != "Box" || len(declaration.TypeParams) != 1 {
		t.Fatalf("struct=%#v", program.Statements[0])
	}
	if declaration, ok := program.Statements[1].(*ast.EnumStatement); !ok || len(declaration.Variants) != 2 {
		t.Fatalf("enum=%#v", program.Statements[1])
	}
	if function, ok := program.Statements[2].(*ast.FunctionStatement); !ok || function.Parameters[1].DeclaredType.Name != "fn" {
		t.Fatalf("function=%#v", program.Statements[2])
	}
	if match, ok := program.Statements[4].(*ast.MatchStatement); !ok || len(match.Arms) != 2 {
		t.Fatalf("match=%#v", program.Statements[4])
	}
}
