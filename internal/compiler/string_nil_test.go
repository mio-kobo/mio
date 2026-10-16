package compiler

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/code"
	"Mio/internal/token"
	"Mio/internal/value"
)

func TestCompileStringAndNil(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		constants    []value.Value
		instructions code.Instructions
		globals      int
	}{
		{"字符串", `"Hello, Mio!";`, []value.Value{value.NewString("Hello, Mio!")}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpPop),
		}, 0},
		{"空字符串", `"";`, []value.Value{value.NewString("")}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpPop),
		}, 0},
		{"Unicode 与转义", `"你好\n\t\r\"\\";`, []value.Value{value.NewString("你好\n\t\r\"\\")}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpPop),
		}, 0},
		{"空值与分组", `mu; (mu);`, nil, code.Instructions{
			byte(code.OpNil), byte(code.OpPop), byte(code.OpNil), byte(code.OpPop),
		}, 0},
		{"混合常量", `1; "Mio"; mu; hai; 2;`, []value.Value{value.NewInt(1), value.NewString("Mio"), value.NewInt(2)}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpPop),
			byte(code.OpConstant), 0, 1, byte(code.OpPop),
			byte(code.OpNil), byte(code.OpPop), byte(code.OpTrue), byte(code.OpPop),
			byte(code.OpConstant), 0, 2, byte(code.OpPop),
		}, 0},
		{"字符串相等", `"Mio" == "Mio";`, []value.Value{value.NewString("Mio"), value.NewString("Mio")}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpConstant), 0, 1,
			byte(code.OpEqual), byte(code.OpPop),
		}, 0},
		{"字符串不等", `"Mio" != "Hello";`, []value.Value{value.NewString("Mio"), value.NewString("Hello")}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpConstant), 0, 1,
			byte(code.OpNotEqual), byte(code.OpPop),
		}, 0},
		{"字符串变量", `kaku s = "Mio"; s = "Hello"; s;`, []value.Value{value.NewString("Mio"), value.NewString("Hello")}, code.Instructions{
			byte(code.OpConstant), 0, 0, byte(code.OpSetGlobal), 0, 0,
			byte(code.OpConstant), 0, 1, byte(code.OpSetGlobal), 0, 0,
			byte(code.OpGetGlobal), 0, 0, byte(code.OpPop),
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compileGlobalSource(t, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Constants, tt.constants) {
				t.Fatalf("constants: got %v, want %v", got.Constants, tt.constants)
			}
			if !bytes.Equal(got.Instructions, tt.instructions) {
				t.Fatalf("instructions:\ngot:\n%swant:\n%s", got.Instructions, tt.instructions)
			}
			if got.GlobalsCount != tt.globals {
				t.Fatalf("globals: got %d, want %d", got.GlobalsCount, tt.globals)
			}
		})
	}
}

func TestCompileStringConstantBoundary(t *testing.T) {
	c := New()
	c.constants = make([]value.Value, (1<<16)-1)
	if err := c.Compile(&ast.StringLiteral{Value: "last"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.instructions, code.Instructions{byte(code.OpConstant), 255, 255}) {
		t.Fatalf("unexpected last index: %v", c.instructions)
	}
	if c.constants[65535] != value.NewString("last") {
		t.Fatal("missing last constant")
	}
	err := c.Compile(&ast.StringLiteral{Value: "overflow"})
	if err == nil || !strings.Contains(err.Error(), "constant pool index overflow") {
		t.Fatalf("expected overflow, got %v", err)
	}
	if len(c.constants) != 1<<16 || len(c.instructions) != 3 {
		t.Fatal("overflow changed bytecode")
	}
	// mu 不占用常量池，满池时仍能编译。
	if err := c.Compile(&ast.NilLiteral{}); err != nil {
		t.Fatal(err)
	}
}

func TestTypedBytecodeSnapshot(t *testing.T) {
	c := New()
	if err := c.Compile(&ast.StringLiteral{Value: "Mio"}); err != nil {
		t.Fatal(err)
	}
	snapshot := c.Bytecode()
	snapshot.Constants[0] = value.NewInt(42)
	snapshot.Instructions[0] = byte(code.OpNil)
	got := c.Bytecode()
	if got.Constants[0] != value.NewString("Mio") || got.Instructions[0] != byte(code.OpConstant) {
		t.Fatal("bytecode snapshot shares mutable storage with compiler")
	}
}

// 测试可空结构字段与 mu 比较使用专用空值检查指令。
func TestCompileNullableFieldNilComparison(t *testing.T) {
	for _, operator := range []token.Type{token.Equal, token.NotEqual} {
		compiler := New()
		expression := &ast.InfixExpression{
			Left: &ast.FieldExpression{
				Left: &ast.StructLiteral{Fields: []ast.FieldValue{{Name: "value", Value: &ast.NilLiteral{}}}},
				Name: "value", Index: 0,
			},
			Operator: operator,
			Right:    &ast.NilLiteral{},
		}
		if err := compiler.Compile(expression); err != nil {
			t.Fatal(err)
		}
		disassembly := compiler.Bytecode().Instructions.String()
		if strings.Count(disassembly, "OpIsNil") != 1 {
			t.Fatalf("nullable field comparison %s must use OpIsNil:\n%s", operator, disassembly)
		}
		if strings.Contains(disassembly, "OpEqual") || strings.Contains(disassembly, "OpNotEqual") {
			t.Fatalf("nullable field comparison %s used ordinary equality:\n%s", operator, disassembly)
		}
	}
}
