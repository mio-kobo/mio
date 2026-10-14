package lexer

import (
	"Mio/internal/token"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Lexer struct {
	input string

	position     int
	readPosition int
	ch           byte

	currentPos token.Position
	nextPos    token.Position

	version token.LanguageVersion

	lexErr *LexicalError
}

// New 创建一个 Lexer
func New(
	input string,
	version token.LanguageVersion,
) *Lexer {
	l := &Lexer{
		input:   input,
		version: version,

		nextPos: token.Position{
			Line:   1,
			Column: 1,
			Offset: 0,
		},
	}

	l.readChar()

	return l
}

func NewChecked(
	input string,
	version token.LanguageVersion,
) (*Lexer, error) {
	offset := firstInvalidUTF8(input)

	if offset >= 0 {
		// 使用合法的源码前缀计算错误位置
		prefix := New(input[:offset], version)

		for !prefix.atEOF() {
			prefix.readChar()
		}

		return nil, &LexicalError{
			Kind:     ErrInvalidUTF8,
			Position: prefix.currentPos,
			Message:  "invalid UTF-8 byte sequence",
		}
	}

	return New(input, version), nil
}

// readChar 读取下一个字节
func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}

	l.position = l.readPosition
	l.currentPos = l.nextPos

	if l.readPosition >= len(l.input) {
		return
	}

	l.readPosition++

	if l.ch == '\n' || (l.ch == '\r' && l.peekChar() != '\n') {
		l.nextPos = token.Position{
			Line:   l.currentPos.Line + 1,
			Offset: l.readPosition,
			Column: 1,
		}
	} else {
		nextColumn := l.currentPos.Column + 1
		if l.readPosition < len(l.input) {
			nextByte := l.input[l.readPosition]

			// UTF8
			if nextByte&0xC0 == 0x80 {
				nextColumn = l.currentPos.Column
			}
		}
		l.nextPos = token.Position{
			Offset: l.readPosition,
			Line:   l.currentPos.Line,
			Column: nextColumn,
		}
	}
}

// peekChar 查看下一个字节，但不移动扫描位置
func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) NextToken() token.Token {
	var tok token.Token

	for {
		l.skipWhitespace()

		// 跳过单行注释
		if l.ch == '/' && l.peekChar() == '/' {
			l.skipLineComment()
			continue
		}

		// 跳过多行注释
		if l.ch == '/' && l.peekChar() == '*' {

			// 记录注释开始的地方
			commentStart := l.currentPos

			if !l.skipBlockComment() {

				if l.lexErr == nil {
					l.lexErr = &LexicalError{
						Kind:     ErrUnterminatedBlockComment,
						Position: commentStart,
						Message:  "unterminated block comment",
					}
				}

				return token.Token{
					Type:    token.Illegal,
					Literal: l.input[commentStart.Offset:l.position],
					Start:   commentStart,
					End:     l.currentPos,
				}
			}
			continue
		}
		break
	}

	start := l.currentPos

	switch l.ch {
	case '+':
		tok = token.Token{
			Type:    token.Plus,
			Literal: "+",
		}
	case '-':
		if l.peekChar() == '>' {
			l.readChar()

			tok = token.Token{
				Type:    token.Arrow,
				Literal: "->",
			}
		} else {
			tok = token.Token{
				Type:    token.Minus,
				Literal: "-",
			}
		}
	case '*':
		tok = token.Token{
			Type:    token.Asterisk,
			Literal: "*",
		}
	case '/':
		tok = token.Token{
			Type:    token.Slash,
			Literal: "/",
		}
	case 0:
		if l.atEOF() {
			tok = token.Token{
				Type:    token.EOF,
				Literal: "",
			}
		} else {
			tok = token.Token{
				Type:    token.Illegal,
				Literal: string(l.ch),
			}
		}

	case '=':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{
				Type:    token.Equal,
				Literal: "==",
			}
		} else {
			tok = token.Token{
				Type:    token.Assign,
				Literal: "=",
			}
		}
	case ';':
		tok = token.Token{
			Type:    token.Semicolon,
			Literal: ";",
		}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{
				Type:    token.NotEqual,
				Literal: "!=",
			}
		} else {
			tok = token.Token{
				Type:    token.Bang,
				Literal: "!",
			}
		}
	case '?':
		tok = token.Token{
			Type:    token.Question,
			Literal: "?",
		}

	case '<':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{
				Type:    token.LessEqual,
				Literal: "<=",
			}
		} else {
			tok = token.Token{
				Type:    token.Less,
				Literal: "<",
			}
		}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = token.Token{
				Type:    token.GreaterEqual,
				Literal: ">=",
			}
		} else {
			tok = token.Token{
				Type:    token.Greater,
				Literal: ">",
			}
		}
	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok = token.Token{
				Type:    token.And,
				Literal: "&&",
			}
		} else {
			tok = token.Token{
				Type:    token.Illegal,
				Literal: "&",
			}
		}
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok = token.Token{
				Type:    token.Or,
				Literal: "||",
			}
		} else {
			tok = token.Token{
				Type:    token.Illegal,
				Literal: "|",
			}
		}
	case '"':
		content, ok := l.readString()
		if !ok {
			tok = token.Token{
				Type:    token.Illegal,
				Literal: content,
			}
		} else {
			tok = token.Token{
				Type:    token.String,
				Literal: content,
			}
		}
	case '\'':
		content, ok := l.readRune()
		if !ok {
			tok = token.Token{Type: token.Illegal, Literal: content}
		} else {
			tok = token.Token{Type: token.Rune, Literal: content}
		}

	case '(':
		tok = token.Token{
			Type:    token.LParen,
			Literal: "(",
		}
	case ')':
		tok = token.Token{
			Type:    token.RParen,
			Literal: ")",
		}
	case '{':
		tok = token.Token{
			Type:    token.LBrace,
			Literal: "{",
		}
	case '}':
		tok = token.Token{
			Type:    token.RBrace,
			Literal: "}",
		}
	case '[':
		tok = token.Token{
			Type:    token.LBracket,
			Literal: "[",
		}
	case ']':
		tok = token.Token{
			Type:    token.RBracket,
			Literal: "]",
		}
	case ',':
		tok = token.Token{
			Type:    token.Comma,
			Literal: ",",
		}
	case '.':
		tok = token.Token{Type: token.Dot, Literal: "."}

	case ':':
		tok = token.Token{
			Type:    token.Colon,
			Literal: ":",
		}

	default:
		if isLetter(l.ch) { // 关键字判断
			literal := l.readIdentifier()
			tok = token.Token{
				Type: token.LookupIdent(
					literal,
					l.version),
				Literal: literal,
				Start:   start,
				End:     l.currentPos,
			}
			return tok
		}

		if isDigit(l.ch) { // 数字判断
			literal, tokenType, valid := l.readNumber()

			if !valid {
				tokenType = token.Illegal

				if l.lexErr == nil {
					l.lexErr = &LexicalError{
						Kind:     ErrInvalidNumber,
						Position: start,
						Message:  "invalid number literal: " + literal,
					}
				}
			}

			return token.Token{
				Type:    tokenType,
				Literal: literal,
				Start:   start,
				End:     l.currentPos,
			}

		}

		tok = token.Token{ // 未知指令
			Type:    token.Illegal,
			Literal: string(l.ch),
		}

	}

	if tok.Type == token.Illegal && l.lexErr == nil {
		l.lexErr = &LexicalError{
			Kind:     ErrUnexpectedCharacter,
			Position: start,
			Message:  fmt.Sprintf("unexpected character '%q'", tok.Literal),
		}
	}

	l.readChar()

	tok.Start = start
	tok.End = l.currentPos

	return tok
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' ||
		l.ch == '\t' ||
		l.ch == '\r' ||
		l.ch == '\n' {
		l.readChar()
	}
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' ||
		'A' <= ch && ch <= 'Z' ||
		ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func (l *Lexer) readIdentifier() string {
	start := l.position

	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.position]
}

func (l *Lexer) readNumber() (string, token.Type, bool) {
	start := l.position
	valid := true
	tokenType := token.Integer

	for isDigit(l.ch) {
		l.readChar()
	}

	if l.ch == '.' {
		tokenType = token.Float
		l.readChar()
		if !isDigit(l.ch) {
			valid = false
		}
		for isDigit(l.ch) {
			l.readChar()
		}
	}

	if l.ch == 'e' || l.ch == 'E' {
		tokenType = token.Float
		l.readChar()
		if l.ch == '+' || l.ch == '-' {
			l.readChar()
		}
		if !isDigit(l.ch) {
			valid = false
		}
		for isDigit(l.ch) {
			l.readChar()
		}
	}

	if isLetter(l.ch) || isDigit(l.ch) || l.ch == '.' {
		valid = false
		for isLetter(l.ch) || isDigit(l.ch) || l.ch == '.' {
			l.readChar()
		}
	}

	literal := l.input[start:l.position]

	integerDigits := literal
	if index := strings.IndexAny(integerDigits, ".eE"); index >= 0 {
		integerDigits = integerDigits[:index]
	}
	if len(integerDigits) > 1 && integerDigits[0] == '0' {
		valid = false
	}

	return literal, tokenType, valid
}

func (l *Lexer) readString() (string, bool) {
	stringStart := l.currentPos

	// 跳过开始的双引号
	l.readChar()

	var result []byte

	for !l.atEOF() {
		switch l.ch {

		case '"':
			// 正常结束字符串
			return string(result), true

		case '\n', '\r':
			l.reportUnterminatedString(stringStart)
			// 不允许字符串中直接换行
			return string(result), false

		case '\\':
			// 保存反斜杠的位置
			escapeStart := l.currentPos

			// 读取转义字符
			l.readChar()

			if l.atEOF() {
				l.reportUnterminatedString(stringStart)
				return string(result), false
			}

			switch l.ch {
			case 'n':
				result = append(result, '\n')

			case 'r':
				result = append(result, '\r')

			case 't':
				result = append(result, '\t')

			case '"':
				result = append(result, '"')

			case '\\':
				result = append(result, '\\')

			default:
				// 非法转义
				if l.lexErr == nil {
					l.lexErr = &LexicalError{
						Kind:     ErrInvalidEscape,
						Position: escapeStart,
						Message: "invalid escape sequence: \\" +
							string(l.ch),
					}
				}
				l.reportUnterminatedString(stringStart)
				return string(result), false
			}

		default:
			// 保存普通字符
			result = append(result, l.ch)
		}

		// 移动到下一个字节
		l.readChar()
	}
	l.reportUnterminatedString(stringStart)
	// EOF 前没有找到结束引号
	return string(result), false
}

// readRune 读取并验证单个 Unicode 标量值
func (l *Lexer) readRune() (string, bool) {
	start := l.currentPos
	l.readChar()
	escape := false
	escapeStart := token.Position{}

	for !l.atEOF() {
		if l.ch == '\n' || l.ch == '\r' {
			l.reportRuneError(ErrUnterminatedRune, start, "unterminated rune literal")
			return l.input[start.Offset:l.position], false
		}
		if !escape && l.ch == '\'' {
			raw := l.input[start.Offset : l.position+1]
			decoded, err := strconv.Unquote(raw)
			if err != nil {
				position := start
				kind := ErrInvalidRune
				if escapeStart != (token.Position{}) {
					position = escapeStart
					if !wellFormedUnicodeEscape(raw) {
						kind = ErrInvalidEscape
					}
				}
				l.reportRuneError(kind, position, err.Error())
				return raw, false
			}
			value, size := utf8.DecodeRuneInString(decoded)
			if size == 0 || size != len(decoded) || !utf8.ValidRune(value) {
				l.reportRuneError(ErrInvalidRune, start, "rune literal must contain exactly one Unicode scalar value")
				return raw, false
			}
			return string(value), true
		}
		if escape {
			escape = false
		} else if l.ch == '\\' {
			escape = true
			escapeStart = l.currentPos
		}
		l.readChar()
	}

	l.reportRuneError(ErrUnterminatedRune, start, "unterminated rune literal")
	return l.input[start.Offset:l.position], false
}

func wellFormedUnicodeEscape(raw string) bool {
	digits := 0
	if strings.HasPrefix(raw, `'\u`) {
		digits = 4
	} else if strings.HasPrefix(raw, `'\U`) {
		digits = 8
	} else {
		return false
	}
	if len(raw) != digits+4 || raw[len(raw)-1] != '\'' {
		return false
	}
	_, err := strconv.ParseUint(raw[3:3+digits], 16, 32)
	return err == nil
}

// skipLineComment 跳过单行注释
func (l *Lexer) skipLineComment() {
	for l.ch != '\n' && l.ch != '\r' && !l.atEOF() {
		l.readChar()
	}
}

// skipBlockComment 跳过 /* ... */ 注释

func (l *Lexer) skipBlockComment() bool {

	l.readChar()
	l.readChar()

	for !l.atEOF() {
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar()
			l.readChar()
			return true
		}
		l.readChar()
	}
	return false
}

func (l *Lexer) atEOF() bool {
	return l.position >= len(l.input)
}

// firstInvalidUTF8 返回第一个非法 UTF-8 字节的位置
func firstInvalidUTF8(input string) int {
	offset := 0
	for offset < len(input) {
		r, size := utf8.DecodeRuneInString(input[offset:])

		if r == utf8.RuneError && size == 1 {
			return offset
		}
		offset += size
	}
	return -1
}

func (l *Lexer) Err() error {
	if l.lexErr == nil {
		return nil
	}

	return l.lexErr
}

func (l *Lexer) reportUnterminatedString(start token.Position) {
	if l.lexErr == nil {
		l.lexErr = &LexicalError{
			Kind:     ErrUnterminatedString,
			Position: start,
			Message:  "unterminated string literal",
		}
	}
}

func (l *Lexer) reportRuneError(kind ErrorKind, position token.Position, message string) {
	if l.lexErr == nil {
		l.lexErr = &LexicalError{Kind: kind, Position: position, Message: message}
	}
}
