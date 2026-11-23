package parser

import (
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

func parseAsync(t *testing.T, src string) (*ast.Program, error) {
	t.Helper()
	return New(lexer.New(src, token.CurrentVersion)).ParseProgram()
}

func TestAsyncDeclarationsAndAwait(t *testing.T) {
	src := `hidouki waza a() -> int64 { kaesu 1; }
koukai hidouki waza b(x: int64) -> int64 { kaku v = matsu a(); kaesu v + matsu a() * 2; }
waza c() -> int64 { kaesu 0; }
`
	program, err := parseAsync(t, src)
	if err != nil {
		t.Fatal(err)
	}
	a := program.Statements[0].(*ast.FunctionStatement)
	b := program.Statements[1].(*ast.FunctionStatement)
	c := program.Statements[2].(*ast.FunctionStatement)
	if !a.Async || a.Exported || !b.Async || !b.Exported || c.Async {
		t.Fatalf("flags: %v %v %v", a, b, c)
	}
	ret := b.Body.Statements[1].(*ast.ReturnStatement)
	sum := ret.Value.(*ast.InfixExpression)
	if _, ok := sum.Left.(*ast.Identifier); !ok {
		t.Fatalf("left should be v, got %T", sum.Left)
	}
	mul := sum.Right.(*ast.InfixExpression)
	if _, ok := mul.Left.(*ast.AwaitExpression); !ok || mul.Operator != token.Asterisk {
		t.Fatalf("matsu must bind tighter than *: %T", mul.Left)
	}
}

func TestAwaitPrecedenceAndNesting(t *testing.T) {
	program, err := parseAsync(t, `hidouki waza f() -> int64 { kaesu matsu g(matsu h()) + (matsu k()); }`)
	if err != nil {
		t.Fatal(err)
	}
	fn := program.Statements[0].(*ast.FunctionStatement)
	sum := fn.Body.Statements[0].(*ast.ReturnStatement).Value.(*ast.InfixExpression)
	left := sum.Left.(*ast.AwaitExpression)
	call := left.Operand.(*ast.CallExpression)
	if _, ok := call.Arguments[0].(*ast.AwaitExpression); !ok {
		t.Fatalf("nested await missing")
	}
	if _, ok := sum.Right.(*ast.GroupedExpression).Expr.(*ast.AwaitExpression); !ok {
		t.Fatalf("parenthesized await missing")
	}
}

func TestAsyncModifierOrdering(t *testing.T) {
	for _, bad := range []string{
		`hidouki koukai waza f() -> int64 { kaesu 1; }`,
		`hidouki hidouki waza f() -> int64 { kaesu 1; }`,
		`hidouki kaku x = 1;`,
		`koukai hidouki kaku x = 1;`,
		`koukai hidouki;`,
		`matsu;`,
		`kaku x = matsu;`,
	} {
		if _, err := parseAsync(t, bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		} else if !strings.Contains(err.Error(), "E01") && !strings.Contains(err.Error(), "expected") {
			t.Errorf("%q: unexpected error %v", bad, err)
		}
	}
}

func TestSimilarIdentifiersRemainValid(t *testing.T) {
	if _, err := parseAsync(t, `kaku matsuri = 1; kaku hidoukis = 2; kaku Matsu = matsuri + hidoukis;`); err != nil {
		t.Fatal(err)
	}
}
