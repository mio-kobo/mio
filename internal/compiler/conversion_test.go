package compiler

import (
	"strings"
	"testing"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

// 测试数值转换生成专用指令
func TestCompileNumericConversions(t *testing.T) {
	l := lexer.New(`int64(float64(10));`, token.V01)
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	compiler := New()
	if err := compiler.Compile(program); err != nil {
		t.Fatal(err)
	}
	disassembly := compiler.Bytecode().Instructions.String()
	for _, name := range []string{"OpIntToFloat", "OpFloatToInt", "OpPop"} {
		if !strings.Contains(disassembly, name) {
			t.Fatalf("disassembly %q does not contain %s", disassembly, name)
		}
	}
	if strings.Contains(disassembly, "OpCall") {
		t.Fatalf("conversion compiled as function call: %q", disassembly)
	}
}
