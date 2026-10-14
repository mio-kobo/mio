package lexer

import (
	"Mio/internal/token"
	"testing"
)

func TestReadChar(t *testing.T) {
	l := New("abc", token.V01)

	expected := []byte{'a', 'b', 'c', 0}

	for i, want := range expected {
		if l.ch != want {
			t.Fatalf(
				"step %d: got %q, want %q",
				i, l.ch, want,
			)
		}

		l.readChar()
	}
}

// 测试核心类型语法的词法单元
func TestCoreTypeTokens(t *testing.T) {
	l := New(`kata Box<T> { value: T; } Box.value;`, token.V04)
	want := []token.Type{token.Struct, token.Ident, token.Less, token.Ident, token.Greater, token.LBrace, token.Ident, token.Colon, token.Ident, token.Semicolon, token.RBrace, token.Ident, token.Dot, token.Ident, token.Semicolon, token.EOF}
	for i, expected := range want {
		if got := l.NextToken(); got.Type != expected {
			t.Fatalf("token %d=%s want=%s", i, got.Type, expected)
		}
	}
}

func TestNextToken(t *testing.T) {
	input := "+-*/"

	tests := []struct {
		expectedType    token.Type
		expectedLiteral string
	}{
		{token.Plus, "+"},
		{token.Minus, "-"},
		{token.Asterisk, "*"},
		{token.Slash, "/"},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		tok := l.NextToken()

		if tok.Type != tt.expectedType {
			t.Fatalf(
				"tests[%d]: wrong token type. got=%q, want=%q",
				i,
				tok.Type,
				tt.expectedType,
			)
		}

		if tok.Literal != tt.expectedLiteral {
			t.Fatalf(
				"tests[%d]: wrong literal. got=%q, want=%q",
				i,
				tok.Literal,
				tt.expectedLiteral,
			)
		}
	}
}

func TestAssignAndSemicolon(t *testing.T) {
	l := New("=;", token.V01)

	first := l.NextToken()
	second := l.NextToken()

	if first.Type != token.Assign {
		t.Errorf("expected ASSIGN, got %s", first.Type)
	}

	if second.Type != token.Semicolon {
		t.Errorf("expected SEMICOLON, got %s", second.Type)
	}
}

func TestSkipWhitespace(t *testing.T) {
	input := " \t\n\r+  -"

	l := New(input, token.V01)

	tests := []token.Type{
		token.Plus,
		token.Minus,
		token.EOF,
	}

	for i, want := range tests {
		got := l.NextToken()

		if got.Type != want {
			t.Fatalf(
				"test[%d]: got %s, want %s",
				i,
				got.Type,
				want,
			)
		}
	}
}

func TestEqualOperator(t *testing.T) {
	input := "= == ==="

	tests := []struct {
		expectedType    token.Type
		expectedLiteral string
	}{
		{token.Assign, "="},
		{token.Equal, "=="},
		{token.Equal, "=="},
		{token.Assign, "="},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		tok := l.NextToken()

		if tok.Type != tt.expectedType {
			t.Fatalf(
				"test[%d]: wrong type, got=%q, want=%q",
				i,
				tok.Type,
				tt.expectedType,
			)
		}

		if tok.Literal != tt.expectedLiteral {
			t.Fatalf(
				"test[%d]: wrong literal, got=%q, want=%q",
				i,
				tok.Literal,
				tt.expectedLiteral,
			)
		}
	}
}

func TestNotEqualOperator(t *testing.T) {
	input := "! != !! !=="

	expected := []token.Type{
		token.Bang,
		token.NotEqual,
		token.Bang,
		token.Bang,
		token.NotEqual,
		token.Assign,
		token.EOF,
	}

	l := New(input, token.V01)

	for i, want := range expected {
		got := l.NextToken()

		if got.Type != want {
			t.Fatalf(
				"test[%d]: got=%s, want=%s",
				i,
				got.Type,
				want,
			)
		}
	}
}

func TestComparisonAndLogicalOperators(t *testing.T) {
	input := "< <= > >= && || & |"

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.Less, "<"},
		{token.LessEqual, "<="},
		{token.Greater, ">"},
		{token.GreaterEqual, ">="},
		{token.And, "&&"},
		{token.Or, "||"},
		{token.Illegal, "&"},
		{token.Illegal, "|"},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: got type %s, want %s",
				i,
				got.Type,
				tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: got literal %q, want %q",
				i,
				got.Literal,
				tt.wantLiteral,
			)
		}
	}
}

func TestLongestMatch(t *testing.T) {
	input := "<<=>>=&&&|||"

	expected := []token.Type{
		token.Less,
		token.LessEqual,
		token.Greater,
		token.GreaterEqual,
		token.And,
		token.Illegal,
		token.Or,
		token.Illegal,
		token.EOF,
	}

	l := New(input, token.V01)

	for i, want := range expected {
		got := l.NextToken()

		if got.Type != want {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i,
				got.Type,
				want,
			)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	input := "kaku counter123=hai; chant"

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.Let, "kaku"},
		{token.Ident, "counter123"},
		{token.Assign, "="},
		{token.True, "hai"},
		{token.Semicolon, ";"},
		{token.Ident, "chant"},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i, got.Type, tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: literal %q, want %q",
				i, got.Literal, tt.wantLiteral,
			)
		}
	}
}

func TestKeywordsVersion(t *testing.T) {
	l := New("aida", token.V01)

	tok := l.NextToken()

	if tok.Type != token.Ident {
		t.Errorf(
			"got %s, want %s",
			tok.Type,
			token.Ident)
	}
}

func TestKeywordVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		version token.LanguageVersion
		want    token.Type
	}{
		{"v0.1 aida", "aida", token.V01, token.Ident},
		{"v0.2 aida", "aida", token.V02, token.While},
		{"v0.2 yobu", "yobu", token.V02, token.Ident},
		{"v0.3 yobu", "yobu", token.V03, token.Import},
		{"v0.1 kaku", "kaku", token.V01, token.Let},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.input, tt.version)
			tok := l.NextToken()

			if tok.Type != tt.want {
				t.Errorf(
					"input=%q: got %s, want %s",
					tt.input,
					tok.Type,
					tt.want,
				)
			}
		})
	}
}

func TestMioKeywords(t *testing.T) {
	input := "kaku waza moshi sore kaesu hai iie mu"

	l := New(input, token.V01)

	for {
		tok := l.NextToken()

		t.Logf("Type=%s, Literal=%q", tok.Type, tok.Literal)

		if tok.Type == token.EOF {
			break
		}
	}
}

func TestIntegerLiteral(t *testing.T) {
	input := "kaku age=18; kaku score=100;"

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.Let, "kaku"},
		{token.Ident, "age"},
		{token.Assign, "="},
		{token.Integer, "18"},
		{token.Semicolon, ";"},
		{token.Let, "kaku"},
		{token.Ident, "score"},
		{token.Assign, "="},
		{token.Integer, "100"},
		{token.Semicolon, ";"},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: got type=%s, want=%s",
				i,
				got.Type,
				tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: got literal=%q, want=%q",
				i,
				got.Literal,
				tt.wantLiteral,
			)
		}
	}
}

func TestNumberValidation(t *testing.T) {
	tests := []struct {
		input       string
		wantType    token.Type
		wantLiteral string
	}{
		{"123", token.Integer, "123"},
		{"0", token.Integer, "0"},
		{"0123", token.Illegal, "0123"},
		{"123abc", token.Illegal, "123abc"},
		{"3.14", token.Float, "3.14"},
		{"1e3", token.Float, "1e3"},
		{"2.5e-4", token.Float, "2.5e-4"},
		{"1.", token.Illegal, "1."},
		{"1e", token.Illegal, "1e"},
		{"0xFF", token.Illegal, "0xFF"},
	}

	for _, tt := range tests {
		l := New(tt.input, token.V01)
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Errorf(
				"input=%q: got type=%s, want=%s",
				tt.input,
				got.Type,
				tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Errorf(
				"input=%q: got literal=%q, want=%q",
				tt.input,
				got.Literal,
				tt.wantLiteral,
			)
		}
	}
}

func TestInvalidNumberBoundary(t *testing.T) {
	l := New("123abc+45", token.V01)

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.Illegal, "123abc"},
		{token.Plus, "+"},
		{token.Integer, "45"},
		{token.EOF, ""},
	}

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i, got.Type, tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: got %q, want %q",
				i, got.Literal, tt.wantLiteral,
			)
		}
	}
}

func TestStringLiteral(t *testing.T) {
	input := `"Mio" "Hello World!" ""`

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.String, "Mio"},
		{token.String, "Hello World!"},
		{token.String, ""},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: type=%s, want=%s",
				i, got.Type, tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: literal=%q, want=%q",
				i, got.Literal, tt.wantLiteral,
			)
		}
	}
}

func TestStringWithOperators(t *testing.T) {
	l := New(`"1 + 2 == 3"`, token.V01)

	got := l.NextToken()

	if got.Type != token.String {
		t.Fatalf("got %s, want STRING", got.Type)
	}

	if got.Literal != "1 + 2 == 3" {
		t.Fatalf("got %q", got.Literal)
	}
}

func TestStringEscape(t *testing.T) {
	tests := []struct {
		input       string
		wantType    token.Type
		wantLiteral string
	}{
		{`"Hello\nMio"`, token.String, "Hello\nMio"},
		{`"Hello\tMio"`, token.String, "Hello\tMio"},
		{`"Hello\rMio"`, token.String, "Hello\rMio"},
		{`"She said: \"Hi\"!"`, token.String, `She said: "Hi"!`},
		{`"C:\\Mio"`, token.String, `C:\Mio`},
		{`""`, token.String, ""},
		{`"Hello\q"`, token.Illegal, "Hello"},
		{`"Hello`, token.Illegal, "Hello"},
	}

	for _, tt := range tests {
		l := New(tt.input, token.V01)
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Errorf(
				"input=%q: got type=%s, want=%s",
				tt.input, got.Type, tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Errorf(
				"input=%q: got literal=%q, want=%q",
				tt.input, got.Literal, tt.wantLiteral,
			)
		}
	}
}

func TestDelimiters(t *testing.T) {
	input := "(){},"

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.LParen, "("},
		{token.RParen, ")"},
		{token.LBrace, "{"},
		{token.RBrace, "}"},
		{token.Comma, ","},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i, got.Type, tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: got %q, want %q",
				i, got.Literal, tt.wantLiteral,
			)
		}
	}
}

func TestLineComment(t *testing.T) {
	input := `// This is a comment
kaku x = 10; // another comment
chant(x);`

	tests := []struct {
		wantType    token.Type
		wantLiteral string
	}{
		{token.Let, "kaku"},
		{token.Ident, "x"},
		{token.Assign, "="},
		{token.Integer, "10"},
		{token.Semicolon, ";"},
		{token.Ident, "chant"},
		{token.LParen, "("},
		{token.Ident, "x"},
		{token.RParen, ")"},
		{token.Semicolon, ";"},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.wantType {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i, got.Type, tt.wantType,
			)
		}

		if got.Literal != tt.wantLiteral {
			t.Fatalf(
				"token[%d]: got %q, want %q",
				i, got.Literal, tt.wantLiteral,
			)
		}
	}
}

func TestBlockComment(t *testing.T) {
	input := `/* This is a comment
    kaku hidden = 999;
    */

    kaku x = 10;`

	expected := []token.Type{
		token.Let,
		token.Ident,
		token.Assign,
		token.Integer,
		token.Semicolon,
		token.EOF,
	}

	l := New(input, token.V01)

	for i, want := range expected {
		got := l.NextToken()

		if got.Type != want {
			t.Fatalf(
				"token[%d]: got=%s, want=%s",
				i, got.Type, want,
			)
		}
	}
}

func TestSourcePosition(t *testing.T) {
	l := New("ab\ncd", token.V01)

	tests := []struct {
		ch     byte
		offset int
		line   int
		column int
	}{
		{'a', 0, 1, 1},
		{'b', 1, 1, 2},
		{'\n', 2, 1, 3},
		{'c', 3, 2, 1},
		{'d', 4, 2, 2},
	}

	for i, tt := range tests {
		if l.ch != tt.ch {
			t.Fatalf(
				"step %d: got char %q, want %q",
				i, l.ch, tt.ch,
			)
		}

		pos := l.currentPos

		if pos.Offset != tt.offset ||
			pos.Line != tt.line ||
			pos.Column != tt.column {
			t.Fatalf(
				"step %d: got position %+v, want offset=%d line=%d column=%d",
				i,
				pos,
				tt.offset,
				tt.line,
				tt.column,
			)
		}

		l.readChar()
	}
}

func TestTokenPosition(t *testing.T) {
	l := New("  kaku==12;", token.V01)

	tests := []struct {
		kind  token.Type
		start int
		end   int
	}{
		{token.Let, 2, 6},
		{token.Equal, 6, 8},
		{token.Integer, 8, 10},
		{token.Semicolon, 10, 11},
		{token.EOF, 11, 11},
	}

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.kind {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i, got.Type, tt.kind,
			)
		}

		if got.Start.Offset != tt.start ||
			got.End.Offset != tt.end {
			t.Fatalf(
				"token[%d]: got [%d,%d), want [%d,%d)",
				i,
				got.Start.Offset,
				got.End.Offset,
				tt.start,
				tt.end,
			)
		}
	}
}

func TestNULIsNotEOF(t *testing.T) {
	l := New("kaku\x00x;", token.V01)

	expected := []token.Type{
		token.Let,
		token.Illegal,
		token.Ident,
		token.Semicolon,
		token.EOF,
	}

	for i, want := range expected {
		got := l.NextToken()

		if got.Type != want {
			t.Fatalf(
				"token[%d]: got %s, want %s",
				i, got.Type, want,
			)
		}
	}
}

func TestNULInsideBlockComment(t *testing.T) {
	l := New("/* a\x00b */kaku", token.V01)

	got := l.NextToken()

	if got.Type != token.Let {
		t.Fatalf(
			"got %s, want LET",
			got.Type,
		)
	}

	got = l.NextToken()

	if got.Type != token.EOF {
		t.Fatalf(
			"got %s, want EOF",
			got.Type,
		)
	}
}

func TestUTF8Position(t *testing.T) {
	input := `"你好";`

	l := New(input, token.V01)

	// 第一个 Token：字符串
	str := l.NextToken()

	if str.Type != token.String {
		t.Fatalf(
			"got %s, want STRING",
			str.Type,
		)
	}

	if str.Start.Offset != 0 ||
		str.Start.Column != 1 ||
		str.End.Offset != 8 ||
		str.End.Column != 5 {

		t.Fatalf(
			"string position: start=%+v, end=%+v",
			str.Start,
			str.End,
		)
	}

	// 第二个 Token：分号
	semicolon := l.NextToken()

	if semicolon.Start.Offset != 8 ||
		semicolon.Start.Column != 5 {
		t.Fatalf(
			"semicolon position: %+v",
			semicolon.Start,
		)
	}
}

func TestNewlinePositions(t *testing.T) {
	input := "kaku\r\nx\ry\nz;"

	l := New(input, token.V01)

	tests := []struct {
		literal string
		offset  int
		line    int
		column  int
	}{
		{"kaku", 0, 1, 1},
		{"x", 6, 2, 1},
		{"y", 8, 3, 1},
		{"z", 10, 4, 1},
		{";", 11, 4, 2},
	}

	for i, tt := range tests {
		got := l.NextToken()

		if got.Literal != tt.literal {
			t.Fatalf(
				"token[%d]: got %q, want %q",
				i, got.Literal, tt.literal,
			)
		}

		pos := got.Start

		if pos.Offset != tt.offset ||
			pos.Line != tt.line ||
			pos.Column != tt.column {

			t.Fatalf(
				"token[%d]: got %+v, want offset=%d line=%d column=%d",
				i,
				pos,
				tt.offset,
				tt.line,
				tt.column,
			)
		}
	}
}

func TestUTF8Validation(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOffset int
	}{
		{
			name:       "ASCII",
			input:      "kaku x = 10;",
			wantOffset: -1,
		},
		{
			name:       "Chinese",
			input:      `chant("你好");`,
			wantOffset: -1,
		},
		{
			name:       "Valid replacement character",
			input:      "\uFFFD",
			wantOffset: -1,
		},
		{
			name:       "Invalid byte",
			input:      "\"你\xff好\"",
			wantOffset: 4,
		},
		{
			name:       "Incomplete UTF8",
			input:      string([]byte{0xE4, 0xBD}),
			wantOffset: 0,
		},
		{
			name:       "NUL byte",
			input:      "a\x00b",
			wantOffset: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstInvalidUTF8(tt.input)

			if got != tt.wantOffset {
				t.Fatalf(
					"got offset=%d, want=%d",
					got,
					tt.wantOffset,
				)
			}
		})
	}
}

func TestInvalidNumberDiagnostic(t *testing.T) {
	l, err := NewChecked("kaku x = 123abc;", token.V01)
	if err != nil {
		t.Fatal(err)
	}

	// 跳过 kaku、x、=
	for i := 0; i < 3; i++ {
		l.NextToken()
	}

	got := l.NextToken()

	if got.Type != token.Illegal {
		t.Fatalf("got %s, want ILLEGAL", got.Type)
	}

	lexErr, ok := l.Err().(*LexicalError)
	if !ok {
		t.Fatal("expected LexicalError")
	}

	if lexErr.Kind != ErrInvalidNumber {
		t.Fatalf("unexpected error: %s", lexErr.Kind)
	}

	want := token.Position{
		Offset: 9,
		Line:   1,
		Column: 10,
	}

	if lexErr.Position != want {
		t.Fatalf(
			"got %+v, want %+v",
			lexErr.Position, want,
		)
	}
}

func TestLexerIntegration(t *testing.T) {
	input := `// Mio demo
kaku name = "你好";
/* Print message */
chant(name);`

	expected := []token.Type{
		token.Let,
		token.Ident,
		token.Assign,
		token.String,
		token.Semicolon,
		token.Ident,
		token.LParen,
		token.Ident,
		token.RParen,
		token.Semicolon,
		token.EOF,
	}

	l, err := NewChecked(input, token.V01)
	if err != nil {
		t.Fatal(err)
	}

	for i, want := range expected {
		got := l.NextToken()

		if got.Type != want {
			t.Fatalf(
				"token[%d]: got=%s, want=%s",
				i, got.Type, want,
			)
		}

		if err := l.Err(); err != nil {
			t.Fatalf("lexer error: %v", err)
		}

		t.Logf(
			"%s %q [%+v, %+v)",
			got.Type,
			got.Literal,
			got.Start,
			got.End,
		)
	}
}

// 测试函数类型标注符号与减号的识别
func TestFunctionTypeTokens(t *testing.T) {
	input := "a: int64 -> bool; -10; x->string;"

	tests := []struct {
		typ     token.Type
		literal string
	}{
		{token.Ident, "a"},
		{token.Colon, ":"},
		{token.Ident, "int64"},
		{token.Arrow, "->"},
		{token.Ident, "bool"},
		{token.Semicolon, ";"},
		{token.Minus, "-"},
		{token.Integer, "10"},
		{token.Semicolon, ";"},
		{token.Ident, "x"},
		{token.Arrow, "->"},
		{token.Ident, "string"},
		{token.Semicolon, ";"},
		{token.EOF, ""},
	}

	l := New(input, token.V01)

	for i, tt := range tests {
		got := l.NextToken()

		if got.Type != tt.typ {
			t.Fatalf(
				"token[%d]: got type %s, want %s",
				i,
				got.Type,
				tt.typ,
			)
		}

		if got.Literal != tt.literal {
			t.Fatalf(
				"token[%d]: got literal %q, want %q",
				i,
				got.Literal,
				tt.literal,
			)
		}
	}

	if err := l.Err(); err != nil {
		t.Fatalf("unexpected lexical error: %v", err)
	}
}
