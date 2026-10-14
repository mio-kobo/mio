package lexer

import (
	"strings"
	"testing"

	"Mio/internal/token"
)

// 测试字符字面量及转义解码
func TestRuneLiterals(t *testing.T) {
	input := `'A' '你' '日' '🍡' '\n' '\t' '\\' '\'' '\x41' '\u4F60' '\U0001F361'`
	want := []string{"A", "你", "日", "🍡", "\n", "\t", "\\", "'", "A", "你", "🍡"}
	l, err := NewChecked(input, token.V05)
	if err != nil {
		t.Fatal(err)
	}
	for i, expected := range want {
		got := l.NextToken()
		if got.Type != token.Rune || got.Literal != expected {
			t.Fatalf("token[%d]=(%s, %q), want (RUNE, %q)", i, got.Type, got.Literal, expected)
		}
	}
	if got := l.NextToken(); got.Type != token.EOF {
		t.Fatalf("last token=%s, want EOF", got.Type)
	}
}

// 测试非法字符字面量诊断
func TestInvalidRuneLiterals(t *testing.T) {
	tests := []struct {
		name string
		text string
		kind ErrorKind
	}{
		{"空字符", `''`, ErrInvalidRune},
		{"多个 ASCII", `'AB'`, ErrInvalidRune},
		{"多个 Unicode", `'🇯🇵'`, ErrInvalidRune},
		{"代理码点", `'\uD800'`, ErrInvalidRune},
		{"超过上限", `'\U00110000'`, ErrInvalidRune},
		{"非法转义", `'\q'`, ErrInvalidEscape},
		{"未闭合", `'A`, ErrUnterminatedRune},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l, err := NewChecked(test.text, token.V05)
			if err != nil {
				t.Fatal(err)
			}
			got := l.NextToken()
			if got.Type != token.Illegal {
				t.Fatalf("token=%s, want ILLEGAL", got.Type)
			}
			lexical, ok := l.Err().(*LexicalError)
			if !ok || lexical.Kind != test.kind {
				t.Fatalf("error=%v, want %s", l.Err(), test.kind)
			}
			if lexical.Position.Line != 1 || lexical.Position.Column < 1 {
				t.Fatalf("position=%+v", lexical.Position)
			}
			if strings.HasPrefix(test.text, `'\`) && lexical.Position.Column != 2 {
				t.Fatalf("position=%+v, want escape at column 2", lexical.Position)
			}
		})
	}
}
