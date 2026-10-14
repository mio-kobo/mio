package compiler

import (
	"testing"

	"Mio/internal/code"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

func compileCheckedCoreSource(t *testing.T, source string) *Bytecode {
	t.Helper()
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
	c := NewWithResolver(r)
	if err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	return c.Bytecode()
}

// 测试核心类型生成追加的 Opcode
func TestCompileCoreTypeOpcodes(t *testing.T) {
	bytecode := compileCheckedCoreSource(t, `
kata Point { x: int64; }
sentaku Option<T> { Some(T); None; }
waza twice(value: int64) -> int64 { kaesu value * 2; }
waza apply(value: int64, f: (int64) -> int64) -> int64 { kaesu f(value); }
kaku p = Point { x: 21 };
kaku option: Option<int64> = Option.Some(p.x);
kaku table: Map<string, int64> = Map();
table["x"] = apply(2, twice);
table["x"];
`)
	want := map[code.Opcode]bool{
		code.OpStruct: false, code.OpGetField: false, code.OpEnum: false,
		code.OpMap: false, code.OpMapSet: false, code.OpMapGet: false,
		code.OpFunction: false, code.OpCallValue: false,
	}
	streams := []code.Instructions{bytecode.Instructions}
	for _, function := range bytecode.Functions {
		streams = append(streams, function.Instructions)
	}
	for _, instructions := range streams {
		for offset := 0; offset < len(instructions); {
			op := code.Opcode(instructions[offset])
			if _, ok := want[op]; ok {
				want[op] = true
			}
			definition, err := code.Lookup(op)
			if err != nil {
				t.Fatal(err)
			}
			_, width, err := code.ReadOperands(definition, instructions[offset+1:])
			if err != nil {
				t.Fatal(err)
			}
			offset += 1 + width
		}
	}
	for op, found := range want {
		if !found {
			t.Fatalf("missing %v", op)
		}
	}
}
