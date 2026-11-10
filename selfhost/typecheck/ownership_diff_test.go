package typecheck_test

import (
	"errors"
	"testing"

	"Mio/internal/diagnostic"
)

// The self-hosted checker (which compiles native targets) must accept and reject exactly what the Go reference does for
// yuzuru ownership programs, with the same first diagnostic code. Parse-level operand errors (E0103) are covered by the
// parser differential corpus.
func TestMioTypecheckerOwnershipDifferential(t *testing.T) {
	cases := []struct{ name, source, want string }{
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
		{"use after transfer", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaesu len(a); }", "E0702"},
		{"use inside a nested expression", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaesu 1 + len(a) * 2; }", "E0702"},
		{"maybe moved: moved in one branch then used", "waza f(c: bool) -> int64 { kaku a = [1]; moshi (c) { kaku b = yuzuru a; } kaesu len(a); }", "E0702"},
		{"moved before a loop, used in it", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaku i = 0; aida (i < 2) { chant(len(a)); i = i + 1; } kaesu 0; }", "E0702"},
		{"moved late in a loop body, used at its top", "waza f() -> int64 { kaku a = [1]; kaku i = 0; aida (i < 2) { chant(len(a)); kaku b = yuzuru a; i = i + 1; } kaesu 0; }", "E0702"},
		{"element store into a moved container", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; a[0] = 2; kaesu 0; }", "E0702"},
		{"top level use after transfer", "kaku a = [1];\nkaku b = yuzuru a;\nchant(len(a));", "E0702"},
		{"double transfer", "waza f() -> int64 { kaku a = [1]; kaku b = yuzuru a; kaku c = yuzuru a; kaesu 0; }", "E0703"},
		{"transfer of an outer variable in a loop transfers twice", "waza f() -> int64 { kaku a = [1]; kaku i = 0; aida (i < 2) { kaku b = yuzuru a; i = i + 1; } kaesu 0; }", "E0703"},
		{"transfer after a branch that already moved", "waza f(c: bool) -> int64 { kaku a = [1]; moshi (c) { kaku b = yuzuru a; } kaku d = yuzuru a; kaesu 0; }", "E0703"},
		{"alias used after transfer", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = yuzuru a; kaesu len(b); }", "E0704"},
		{"alias created by the transitive chain", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = b; kaku d = yuzuru a; kaesu len(c); }", "E0704"},
		{"alias used when the transfer moved the other name", "waza f() -> int64 { kaku a = [1]; kaku b = a; kaku c = yuzuru b; kaesu len(a); }", "E0704"},
		{"values passed to the same call may alias", "waza hold(x: []int64, y: []int64) -> int64 { kaesu 0; }\nwaza f() -> int64 { kaku a = [1]; kaku b = [2]; kaku n = hold(a, b); kaku c = yuzuru a; kaesu len(b); }", "E0704"},
		{"alias through an element", "waza f() -> int64 { kaku a = [[1]]; kaku e = a[0]; kaku c = yuzuru a; kaesu len(e); }", "E0704"},
		{"parameter", "waza f(p: []int64) -> int64 { kaku b = yuzuru p; kaesu 0; }", "E0701"},
		{"function name", "waza g() -> int64 { kaesu 1; }\nwaza f() -> int64 { kaku h = yuzuru g; kaesu 0; }", "E0701"},
		{"captured by a closure", "waza f() -> int64 { kaku a = [1]; kaku g = waza() -> int64 { kaesu len(a); }; kaku b = yuzuru a; kaesu g(); }", "E0701"},
		{"outer variable inside a closure", "waza f() -> int64 { kaku a = [1]; kaku g = waza() -> int64 { kaku b = yuzuru a; kaesu 0; }; kaesu g(); }", "E0701"},
		{"top level variable reachable from a function", "kaku a = [1];\nwaza peek() -> int64 { kaesu len(a); }\nkaku b = yuzuru a;", "E0701"},
		{"top level variable transferred inside a function", "kaku a = [1];\nwaza f() -> int64 { kaku b = yuzuru a; kaesu 0; }", "E0701"},
	}
	sources := make([]string, len(cases))
	for index, c := range cases {
		sources[index] = c.source
	}
	mio, _ := runMioTypecheck(t, sources)
	rejected := 0
	for index, c := range cases {
		goErr := runGoTypecheck(c.source)
		goCode := ""
		var detailed *diagnostic.Diagnostic
		if errors.As(goErr, &detailed) {
			goCode = detailed.Code
		} else if goErr != nil {
			goCode = "ERR"
		}
		mioCode := ""
		if len(mio[index].errorDetails) > 0 {
			mioCode = mio[index].errorDetails[0].code
		}
		if goCode != c.want {
			t.Fatalf("%s: the Go reference reports %q, expected %q", c.name, goCode, c.want)
		}
		if mioCode != "" {
			rejected++
		}
		if mioCode != goCode {
			t.Fatalf("%s: Mio checker reports %q, Go reference %q (source %q)", c.name, mioCode, goCode, c.source)
		}
	}
	if rejected < 15 {
		t.Fatalf("the Mio checker rejected only %d programs; the ownership pass is not running", rejected)
	}
}
