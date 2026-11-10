package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/stdlib"
	"Mio/internal/token"
)

func lexerForTest(text string) (*ast.Program, error) {
	return parser.New(lexer.New(text, token.V05)).ParseProgram()
}

func lintSource(t *testing.T, source string) ([]Finding, error) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "lint 日本語")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.mio")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../bunko")
	if err != nil {
		t.Fatal(err)
	}
	return Run(path, root, registry)
}

func rules(findings []Finding) string {
	list := make([]string, len(findings))
	for i, f := range findings {
		list[i] = f.Rule
	}
	return strings.Join(list, ",")
}

func TestUnusedVariable(t *testing.T) {
	findings, err := lintSource(t, `waza f(p: int64) -> int64 {
    kaku unused = 1;
    kaku _ignored = 2;
    kaku used = p + 1;
    kaku assigned = 0;
    assigned = 5;
    kaesu used;
}
kaku top = 3;
chant(f(1));
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Rule != "unused-variable" || !strings.Contains(findings[0].Message, `"unused"`) || findings[0].Line != 2 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestUnusedVariableInClosureAndShadowing(t *testing.T) {
	findings, err := lintSource(t, `waza f() -> int64 {
    kaku x = 1;
    moshi (hai == hai) {
        kaku x = 2;
        kaesu x;
    }
    kaesu 0;
}
chant(f());
`)
	if err != nil {
		t.Fatal(err)
	}
	// 外层 x 从未使用（内层 x 是另一个符号）；不应把内层 x 的使用算到外层。
	var names []string
	for _, f := range findings {
		if f.Rule == "unused-variable" {
			names = append(names, f.Message+"@"+string(rune('0'+f.Line)))
		}
	}
	if len(names) != 1 || !strings.HasSuffix(names[0], "@2") {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestUnusedImport(t *testing.T) {
	findings, err := lintSource(t, "yobu \"kiso/path\";\nyobu \"kiso/string\";\nchant(path.base(\"a/b.mio\"));\n")
	if err != nil {
		t.Fatal(err)
	}
	if rules(findings) != "unused-import" || !strings.Contains(findings[0].Message, "kiso/string") || findings[0].Line != 2 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestUnreachableCode(t *testing.T) {
	findings, err := lintSource(t, `waza f() -> int64 {
    kaesu 1;
    chant(2);
}
aida (hai) {
    nukeru;
    chant(3);
}
chant(f());
`)
	if err != nil {
		t.Fatal(err)
	}
	if rules(findings) != "unreachable-code,unreachable-code" || findings[0].Line != 3 || findings[1].Line != 7 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestConstantCondition(t *testing.T) {
	findings, err := lintSource(t, `teisu DEBUG = hai;
moshi (hai) { chant(1); }
moshi ((iie)) { chant(2); }
aida (iie) { chant(3); }
aida (hai) { nukeru; }
moshi (DEBUG) { chant(4); }
`)
	if err != nil {
		t.Fatal(err)
	}
	if rules(findings) != "constant-condition,constant-condition,constant-condition" {
		t.Fatalf("findings = %#v", findings)
	}
	for i, line := range []int{2, 3, 4} {
		if findings[i].Line != line {
			t.Fatalf("finding %d line = %d, want %d (%#v)", i, findings[i].Line, line, findings)
		}
	}
}

func TestCleanFileHasNoFindings(t *testing.T) {
	findings, err := lintSource(t, "waza add(a: int64, b: int64) -> int64 {\n    kaesu a + b;\n}\nchant(add(1, 2));\n")
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings = %#v, err = %v", findings, err)
	}
}

func TestCompilerErrorsAreNotLintFindings(t *testing.T) {
	findings, err := lintSource(t, "kaku x = ;\n")
	if err == nil || len(findings) != 0 {
		t.Fatalf("syntax error must be returned as an error, got findings=%#v err=%v", findings, err)
	}
	if _, err := lintSource(t, "kaku x: int64 = \"text\";\n"); err == nil {
		t.Fatal("type error must be returned as an error")
	}
}

func TestExamplesDoNotCrashAndHaveNoFalseUnusedImports(t *testing.T) {
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../../bunko")
	files, _ := filepath.Glob("../../examples/*.mio")
	if len(files) < 10 {
		t.Fatalf("examples missing: %d", len(files))
	}
	checked := 0
	for _, file := range files {
		findings, err := Run(file, root, registry)
		if err != nil {
			continue // examples that need runtime input may not pass `mio check` standalone
		}
		checked++
		for _, f := range findings {
			if f.Rule == "unused-import" {
				t.Errorf("%s: shipped example reported an unused import: %s", file, f.Message)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("only %d examples linted", checked)
	}
}

func TestUnusedImportRecognizesQualifiedCallInFunction(t *testing.T) {
	// 包名作为方法调用的限定符出现在函数体内，仍然算作已使用。
	findings, err := lintSource(t, "yobu \"kiso/path\";\nwaza f(p: []string) -> unit {\n    chant(path.base(\"a\"));\n}\nf([]);\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestReferencedNamesCollectsTypeQualifiers(t *testing.T) {
	lex, err := lexerForTest("waza f(x: shape.Point) -> shape.Point {\n    kaesu shape.Point { x: 1 };\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	names := referencedNames(lex)
	if _, ok := names["shape"]; !ok {
		t.Fatalf("qualifier missing: %v", names)
	}
}
