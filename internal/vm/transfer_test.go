package vm

import (
	"bytes"
	"fmt"
	"testing"

	"Mio/internal/compiler"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

func runCurrent(t *testing.T, source string) string {
	t.Helper()
	l, err := lexer.NewChecked(source, token.CurrentVersion)
	if err != nil {
		t.Fatal(err)
	}
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(program); err != nil {
		t.Fatal(err)
	}
	c := compiler.NewWithResolver(r)
	if err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := NewWithOutput(c.Bytecode(), &out).Run(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// yuzuru has no runtime effect of its own: the receiver sees the very same graph (identity, topology, sharing and cycles
// are untouched), so a program with the transfer prints exactly what the same program with plain assignment prints.
func TestTransferKeepsTheGraphAndValues(t *testing.T) {
	body := `
kata Node { value: int64; items: []int64; }
waza build() -> []Node {
    kaku shared = [7, 8, 9];
    kaku nodes = [Node { value: 1, items: shared }, Node { value: 2, items: shared }];
    kaku moved = %s;
    kaesu moved;
}
kaku graph = build();
chant(len(graph));
chant(graph[0].value + graph[1].value);
chant(graph[0].items[2]);
`
	with := runCurrent(t, fmtSprintf(body, "yuzuru nodes"))
	without := runCurrent(t, fmtSprintf(body, "nodes"))
	if with != without {
		t.Fatalf("transfer changed the observable result:\nwith yuzuru: %q\nplain:       %q", with, without)
	}
	if with != "2\n3\n9\n" {
		t.Fatalf("unexpected output %q", with)
	}
}

func TestTransferInsideCallArgumentsAndLoops(t *testing.T) {
	out := runCurrent(t, `
waza total(x: []int64) -> int64 { kaku i = 0; kaku s = 0; aida (i < len(x)) { s = s + x[i]; i = i + 1; } kaesu s; }
waza run() -> int64 {
    kaku i = 0;
    kaku sum = 0;
    aida (i < 3) {
        kaku a = [i, i + 1, i + 2];
        sum = sum + total(yuzuru a);
        i = i + 1;
    }
    kaesu sum;
}
chant(run());
`)
	if out != "18\n" {
		t.Fatalf("unexpected output %q", out)
	}
}

func fmtSprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
