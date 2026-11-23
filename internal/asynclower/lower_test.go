package asynclower

import (
	"testing"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

func parse(t *testing.T, source string) *ast.Program {
	t.Helper()
	program, err := parser.New(lexer.New(source, token.CurrentVersion)).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func lower(t *testing.T, source string) (*ast.Program, []error) {
	t.Helper()
	return Lower(parse(t, source))
}

func codes(errs []error) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.(*diagnostic.Diagnostic).Code)
	}
	return out
}

// countKinds walks the whole program (function literals included).
func countKinds(program *ast.Program) (awaits, asyncFns, literals int) {
	for _, s := range program.Statements {
		if fn, ok := s.(*ast.FunctionStatement); ok && fn.Async {
			asyncFns++
		}
		walkStatement(s, func(e ast.Expression) {
			switch e.(type) {
			case *ast.AwaitExpression:
				awaits++
			case *ast.FunctionLiteral:
				literals++
			}
		}, nil)
	}
	return
}

func TestProgramsWithoutAsyncAreUntouched(t *testing.T) {
	program := parse(t, `waza f(x: int64) -> int64 { kaesu x; } kaku matsuri = 1;`)
	out, errs := Lower(program)
	if errs != nil || out != program {
		t.Fatalf("a program without hidouki / matsu must come back unchanged (same pointer), errs=%v", errs)
	}
}

func TestLoweringRemovesAsyncAndInjectsTheRuntime(t *testing.T) {
	out, errs := lower(t, `
hidouki waza first() -> int64 { kaesu 10; }
hidouki waza second() -> int64 { kaku v = matsu first(); kaesu v + 20; }
`)
	if errs != nil {
		t.Fatal(errs)
	}
	awaits, asyncFns, literals := countKinds(out)
	if awaits != 0 || asyncFns != 0 {
		t.Fatalf("lowered program still has %d awaits and %d async functions", awaits, asyncFns)
	}
	if literals < 3 { // two self literals and the continuation of the await in second
		t.Fatalf("expected closure literals for the task bodies and the continuation, got %d", literals)
	}
	imp, ok := out.Statements[0].(*ast.ImportStatement)
	if !ok || imp.Path != RuntimePackage || imp.Alias != "async" {
		t.Fatalf("the runtime import must be injected first, got %#v", out.Statements[0])
	}
	second := out.Statements[2].(*ast.FunctionStatement)
	if second.ReturnType.Name != "async.Task" || len(second.ReturnType.Args) != 1 || second.ReturnType.Args[0].Name != "int64" {
		t.Fatalf("the declared result must become async.Task<int64>, got %#v", second.ReturnType)
	}
}

func TestLoweringKeepsAnExistingRuntimeImport(t *testing.T) {
	out, errs := lower(t, `yobu "kiso/async"; hidouki waza f() -> int64 { kaesu 1; }`)
	if errs != nil {
		t.Fatal(errs)
	}
	imports := 0
	for _, s := range out.Statements {
		if _, ok := s.(*ast.ImportStatement); ok {
			imports++
		}
	}
	if imports != 1 {
		t.Fatalf("expected the single existing import, got %d", imports)
	}
}

func TestAsyncMainIsDrivenByTheExecutorAndExits(t *testing.T) {
	out, errs := lower(t, `hidouki waza main() -> int64 { kaesu 3; } main();`)
	if errs != nil {
		t.Fatal(errs)
	}
	last := out.Statements[len(out.Statements)-1].(*ast.ExpressionStatement)
	call := last.Expr.(*ast.CallExpression)
	field, ok := call.Function.(*ast.FieldExpression)
	if !ok || field.Name != "exit" {
		t.Fatalf("main(); must become os.exit(async.block_on(main())), got %#v", call.Function)
	}
	if _, ok := call.Arguments[0].(*ast.CallExpression); !ok {
		t.Fatal("os.exit must receive the async.block_on call")
	}
	if imp, ok := out.Statements[1].(*ast.ImportStatement); !ok || imp.Path != "kiso/os" {
		t.Fatalf("kiso/os must be imported for the exit status, got %#v", out.Statements[1])
	}
}

func TestEveryMatsuInsideLoopsBranchesAndExpressionsIsLowered(t *testing.T) {
	out, errs := lower(t, `
sentaku C { A(int64); B; }
hidouki waza leaf(x: int64) -> int64 { kaesu x; }
hidouki waza f(c: C) -> int64 {
    kaku t = matsu leaf(1) + matsu leaf(2) * matsu leaf(3);
    kaku i = 0;
    aida (matsu leaf(i) < 3) {
        moshi (i == 1) { i = i + 1; tsuzuku; }
        moshi (matsu leaf(i) > 5) { nukeru; }
        i = i + 1;
    }
    awase (c) { baai A(v) { t = t + matsu leaf(v); } baai B { t = 0; } }
    kaesu hai && matsu leaf(1) == 1 ? 0 : t;
}`[:0]+`
sentaku C { A(int64); B; }
hidouki waza leaf(x: int64) -> int64 { kaesu x; }
hidouki waza f(c: C) -> int64 {
    kaku t = matsu leaf(1) + matsu leaf(2) * matsu leaf(3);
    kaku i = 0;
    aida (matsu leaf(i) < 3) {
        moshi (i == 1) { i = i + 1; tsuzuku; }
        moshi (matsu leaf(i) > 5) { nukeru; }
        i = i + 1;
    }
    awase (c) { baai A(v) { t = t + matsu leaf(v); } baai B { t = 0; } }
    kaku flag = hai && matsu leaf(1) == 1;
    kaesu t;
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	if awaits, _, _ := countKinds(out); awaits != 0 {
		t.Fatalf("%d awaits survived the lowering", awaits)
	}
}

func TestDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, code string
		line, column       int
	}{
		{"await in sync function", "waza f() -> int64 {\n    kaku v = matsu g();\n    kaesu 1;\n}", CodeAwaitOutside, 2, 14},
		{"await at top level", "hidouki waza g() -> int64 { kaesu 1; }\nkaku v = matsu g();", CodeAwaitOutside, 2, 10},
		{"await in a literal inside async", "hidouki waza f() -> int64 {\n    kaku c = waza () -> int64 { kaesu matsu g(); };\n    kaesu 1;\n}", CodeAwaitOutside, 2, 39},
		{"unit result", "hidouki waza f() -> unit { kaesu; }", CodeInvalidDecl, 1, 1},
		{"missing result", "hidouki waza f() { kaesu; }", CodeInvalidDecl, 1, 1},
		{"bare return", "hidouki waza f() -> int64 { kaesu; }", CodeInvalidDecl, 1, 29},
		{"entry parameters", "hidouki waza main(x: int64) -> int64 { kaesu x; }", CodeInvalidEntry, 1, 1},
		{"entry result", "hidouki waza main() -> int32 { kaesu int32(1); }", CodeInvalidEntry, 1, 1},
		{"entry generic", "hidouki waza main<T>() -> int64 { kaesu 1; }", CodeInvalidEntry, 1, 1},
		{"reserved name", "hidouki waza f() -> int64 {\n    kaku async = 1;\n    kaesu async;\n}", CodeInvalidDecl, 2, 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, errs := lower(t, test.source)
			if len(errs) == 0 {
				t.Fatal("expected an error")
			}
			d := errs[0].(*diagnostic.Diagnostic)
			start := d.Labels[0].Span.Start
			if d.Code != test.code || start.Line != test.line || start.Column != test.column {
				t.Fatalf("got %s at %d:%d, want %s at %d:%d", d.Code, start.Line, start.Column, test.code, test.line, test.column)
			}
		})
	}
}

func TestAllErrorsAreReportedInSourceOrder(t *testing.T) {
	_, errs := lower(t, "waza a() -> int64 { kaesu matsu x(); }\nhidouki waza main(p: int64) -> int64 { kaesu 1; }\nwaza b() -> int64 { kaesu matsu y(); }")
	got := codes(errs)
	want := []string{CodeAwaitOutside, CodeInvalidEntry, CodeAwaitOutside}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
