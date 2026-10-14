package resolver

import (
	"testing"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

// 测试枚举类型名称与分支绑定解析
func TestResolveEnumMatchBindings(t *testing.T) {
	source := `sentaku Option<T> { Some(T); None; } waza read(value: Option<int64>) -> int64 { awase (value) { baai Some(item) { kaesu item; } baai None { kaesu 0; } } }`
	p := parser.New(lexer.New(source, token.V04))
	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	if err := New().ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
}
