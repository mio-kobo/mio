package constant

import (
	"math"
	"strings"
	"testing"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

func parseConstants(t *testing.T, source string) map[string]Value {
	t.Helper()
	checked, err := lexer.NewChecked(source, token.V05)
	if err != nil {
		t.Fatal(err)
	}
	program, err := parser.New(checked).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	values, err := Evaluate(program)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

// 测试前向常量依赖与精确边界求值
func TestEvaluateDependenciesAndBoundaries(t *testing.T) {
	values := parseConstants(t, "teisu answer: int64 = next + 1; teisu next: int64 = 41; teisu minimum: int64 = -9223372036854775808;")
	if values["answer"].Integer != 42 || values["minimum"].Integer != math.MinInt64 {
		t.Fatalf("unexpected constants: %+v", values)
	}
}

// 测试常量循环依赖
func TestEvaluateCycle(t *testing.T) {
	checked, err := lexer.NewChecked("teisu left: int64 = right; teisu right: int64 = left;", token.V05)
	if err != nil {
		t.Fatal(err)
	}
	program, err := parser.New(checked).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Evaluate(program)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

// 测试 float32 只执行一次目标精度舍入
func TestEvaluateFloat32Precision(t *testing.T) {
	values := parseConstants(t, "teisu value: float32 = 1.000000059604644775390625;")
	if values["value"].Float32Bits != math.Float32bits(float32(1)) {
		t.Fatalf("unexpected float32 bits: %08x", values["value"].Float32Bits)
	}
}
