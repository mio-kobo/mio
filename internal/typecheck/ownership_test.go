package typecheck

import (
	"strings"
	"testing"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
)

// checkCurrent runs lexer, parser, resolver and checker at the current language version (where yuzuru is reserved).
func checkCurrent(input string) error {
	l, err := lexer.NewChecked(input, token.CurrentVersion)
	if err != nil {
		return err
	}
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		return err
	}
	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		return err
	}
	return NewWithResolver(r).CheckProgram(program)
}

// Every case is one complete program. want is the diagnostic code that must be reported ("" = accepted).
func TestYuzuruOwnership(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		// ---- accepted
		{"transfer into a new variable", "waza f() -> int64 { kaku a = [1, 2]; kaku b = yuzuru a; kaesu len(b); }", ""},
		{"transfer as a call argument", "waza take(x: []int64) -> int64 { kaesu len(x); }\nwaza f() -> int64 { kaku a = [1]; kaesu take(yuzuru a); }", ""},
		{"reassignment revives the variable", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; a = [2, 3]; kaesu len(a) + len(b); }", ""},
		{"moved in one branch, used in the other", "waza f(c: bool) -> int64 { kaku a = [1]; moshi (c) { kaku b = yuzuru a; kaesu len(b); } sore { kaesu len(a); } }", ""},
		{"moved on both branches, not used again", "waza f(c: bool) -> int64 { kaku a = [1]; moshi (c) { kaku b = yuzuru a; } sore { kaku d = yuzuru a; } kaesu 0; }", ""},
		{"an alias that is never used again is harmless", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = yuzuru a; kaesu len(c); }", ""},
		{"scalar results never alias", "waza f() -> int64 { kaku a = [1]; kaku n = len(a); kaku c = yuzuru a; kaesu n + len(c); }", ""},
		{"declared inside a loop each iteration", "waza f() -> int64 { kaku i = 0; kaku t = 0; aida (i < 3) { kaku a = [i]; kaku c = yuzuru a; t = t + len(c); i = i + 1; } kaesu t; }", ""},
		{"top level code may transfer a variable no function touches", "kaku a = [1, 2];\nkaku b = yuzuru a;\nchant(len(b));", ""},
		{"ordinary assignment keeps both names usable", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaesu len(a) + len(b); }", ""},
		{"ordinary call keeps the argument usable", "waza take(x: []int64) -> int64 { kaesu len(x); }\nwaza f() -> int64 { kaku a = [1]; kaku n = take(a); kaesu n + len(a); }", ""},
		{"yuzuru absent: nothing changes", "waza f() -> int64 { kaku a = [1]; kaku b = a; a = [2]; kaesu len(a) + len(b); }", ""},

		// ---- use after transfer
		{"use after transfer", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaesu len(a); }", "E0702"},
		{"use inside a nested expression", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaesu 1 + len(a) * 2; }", "E0702"},
		{"maybe moved: moved in one branch then used", "waza f(c: bool) -> int64 { kaku a = [1]; moshi (c) { kaku b = yuzuru a; } kaesu len(a); }", "E0702"},
		{"moved before a loop, used in it", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaku i = 0; aida (i < 2) { chant(len(a)); i = i + 1; } kaesu 0; }", "E0702"},
		{"moved late in a loop body, used at its top", "waza f() -> int64 { kaku a = [1]; kaku i = 0; aida (i < 2) { chant(len(a)); kaku b = yuzuru a; i = i + 1; } kaesu 0; }", "E0702"},
		{"element store into a moved container", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; a[0] = 2; kaesu 0; }", "E0702"},
		{"top level use after transfer", "kaku a = [1];\nkaku b = yuzuru a;\nchant(len(a));", "E0702"},

		// ---- double transfer
		{"double transfer", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaku c = yuzuru a; kaesu 0; }", "E0703"},
		{"transfer of an outer variable in a loop transfers twice", "waza f() -> int64 { kaku a = [1]; kaku i = 0; aida (i < 2) { kaku b = yuzuru a; i = i + 1; } kaesu 0; }", "E0703"},
		{"transfer after a branch that already moved", "waza f(c: bool) -> int64 { kaku a = [1]; moshi (c) { kaku b = yuzuru a; } kaku d = yuzuru a; kaesu 0; }", "E0703"},

		// ---- live aliases
		{"alias used after transfer", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = yuzuru a; kaesu len(b); }", "E0704"},
		{"alias created by the transitive chain", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = b; kaku d = yuzuru a; kaesu len(c); }", "E0704"},
		{"alias used when the transfer moved the other name", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = yuzuru b; kaesu len(a); }", "E0704"},
		{"values passed to the same call may alias", "waza hold(x: []int64, y: []int64) -> int64 { kaesu 0; }\nwaza f() -> int64 { kaku a = [1]; kaku b = [2]; kaku n = hold(a, b); kaku c = yuzuru a; kaesu len(b); }", "E0704"},
		{"alias through an element", "waza f() -> int64 { kaku a = [[1]]; kaku e = a[0]; kaku c = yuzuru a; kaesu len(e); }", "E0704"},

		// ---- operands that cannot be transferred
		{"parameter", "waza f(p: []int64) -> int64 { kaku b = yuzuru p; kaesu 0; }", "E0701"},
		{"function name", "waza g() -> int64 { kaesu 1; }\nwaza f() -> int64 { kaku h = yuzuru g; kaesu 0; }", "E0701"},
		{"captured by a closure", "waza f() -> int64 { kaku a = [1]; kaku g = waza() -> int64 { kaesu len(a); }; kaku b = yuzuru a; kaesu g(); }", "E0701"},
		{"outer variable inside a closure", "waza f() -> int64 { kaku a = [1]; kaku g = waza() -> int64 { kaku b = yuzuru a; kaesu 0; }; kaesu g(); }", "E0701"},
		{"top level variable reachable from a function", "kaku a = [1];\nwaza peek() -> int64 { kaesu len(a); }\nkaku b = yuzuru a;", "E0701"},
		{"top level variable transferred inside a function", "kaku a = [1];\nwaza f() -> int64 { kaku b = yuzuru a; kaesu 0; }", "E0701"},

		// ---- syntax: the operand is exactly one variable name
		{"field operand", "kata P { x: []int64; }\nwaza f() -> int64 { kaku p = P { x: [1] }; kaku b = yuzuru p.x; kaesu 0; }", "E0103"},
		{"index operand", "waza f() -> int64 { kaku a = [[1]]; kaku b = yuzuru a[0]; kaesu 0; }", "E0103"},
		{"call operand", "waza g() -> []int64 { kaesu [1]; }\nwaza f() -> int64 { kaku b = yuzuru g(); kaesu 0; }", "E0103"},
		{"grouped operand", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru (a); kaesu 0; }", "E0103"},
		{"literal operand", "waza f() -> int64 { kaku b = yuzuru 1; kaesu 0; }", "E0103"},
		{"missing operand", "waza f() -> int64 { kaku b = yuzuru; kaesu 0; }", "E0103"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkCurrent(c.src)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("expected the program to be accepted, got: %v", err)
			case c.want != "" && err == nil:
				t.Fatalf("expected %s, program was accepted", c.want)
			case c.want != "" && !strings.HasPrefix(err.Error(), c.want):
				t.Fatalf("expected %s, got: %v", c.want, err)
			}
		})
	}
}

// yuzuru is a fully reserved keyword (decision D-3): it is rejected as every kind of identifier, with no contextual behaviour.
func TestYuzuruIsFullyReserved(t *testing.T) {
	for name, src := range map[string]string{
		"variable":        "kaku yuzuru = 1;",
		"parameter":       "waza f(yuzuru: int64) -> int64 { kaesu 1; }",
		"function":        "waza yuzuru() -> int64 { kaesu 1; }",
		"struct field":    "kata P { yuzuru: int64; }",
		"struct type":     "kata yuzuru { x: int64; }",
		"assignment":      "kaku a = 1;\nyuzuru = 2;",
		"closure binding": "kaku g = waza(yuzuru: int64) -> int64 { kaesu 1; };",
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkCurrent(src); err == nil {
				t.Fatalf("%q was accepted but yuzuru is reserved", src)
			}
		})
	}
	// Older language versions keep accepting it as an identifier, so existing version-pinned sources are unaffected.
	l, err := lexer.NewChecked("kaku yuzuru = 1;", token.V05)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.New(l).ParseProgram(); err != nil {
		t.Fatalf("V05 must still treat yuzuru as an identifier: %v", err)
	}
}

// Programs that never write yuzuru are not analysed at all: the pass cannot change their meaning or diagnostics.
func TestOwnershipPassIsInertWithoutYuzuru(t *testing.T) {
	src := "waza f() -> int64 { kaku a = [1]; kaku b = a; a = [2]; kaesu len(a) + len(b); }\nkaku x = [1];\nkaku y = x;\nkaku z = x;"
	l, _ := lexer.NewChecked(src, token.CurrentVersion)
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	if programUsesTransfer(program) {
		t.Fatal("a program without yuzuru was reported as using it")
	}
	if err := checkCurrent(src); err != nil {
		t.Fatalf("ordinary aliasing must be accepted exactly as before: %v", err)
	}
}
