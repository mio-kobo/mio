package vm

import (
	"bytes"
	"testing"

	"Mio/internal/compiler"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

// 测试 VM 执行枚举、函数值和 Map
func TestVMCoreTypeRuntime(t *testing.T) {
	source := `
sentaku Option<T> { Some(T); None; }
waza twice(value: int64) -> int64 { kaesu value * 2; }
waza apply(value: int64, f: (int64) -> int64) -> int64 { kaesu f(value); }
waza show(value: Option<int64>) -> unit {
    awase (value) {
        baai Some(item) { chant(item); }
        baai None { chant("none"); }
    }
}
kaku table: Map<string, int64> = Map();
table["answer"] = apply(21, twice);
kaku answer = table["answer"];
moshi (answer != mu) { show(Option.Some(answer)); }
`
	p := parser.New(lexer.New(source, token.V04))
	program, err := p.ParseProgram()
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
	var output bytes.Buffer
	machine := NewWithOutput(c.Bytecode(), &output)
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "42\n" {
		t.Fatalf("output=%q", output.String())
	}
}
