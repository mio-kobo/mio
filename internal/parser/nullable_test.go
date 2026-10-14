package parser

import (
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 测试可空类型标注保留数组层级
func TestParseNullableTypeAnnotations(t *testing.T) {
	l := lexer.New(`kaku elements: []int64? = [mu]; kaku array: ([]int64)? = mu;`, token.V01)
	program, err := New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	elements := program.Statements[0].(*ast.LetStatement).Name.DeclaredType
	if elements.Name != "[]" || elements.Element == nil || elements.Element.Name != "?" || elements.Element.Element.Name != "int64" {
		t.Fatalf("elements type=%+v", elements)
	}
	array := program.Statements[1].(*ast.LetStatement).Name.DeclaredType
	if array.Name != "?" || array.Element == nil || array.Element.Name != "[]" || array.Element.Element.Name != "int64" {
		t.Fatalf("array type=%+v", array)
	}
}
