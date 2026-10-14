package lexer

import (
	"Mio/internal/token"
	"fmt"
)

type ErrorKind string

const (
	ErrInvalidUTF8              ErrorKind = "invalid UTF-8"
	ErrInvalidNumber            ErrorKind = "invalid number"
	ErrInvalidEscape            ErrorKind = "invalid escape"
	ErrInvalidRune              ErrorKind = "invalid rune"
	ErrUnterminatedRune         ErrorKind = "unterminated rune"
	ErrUnterminatedString       ErrorKind = "unterminated string"
	ErrUnterminatedBlockComment ErrorKind = "unterminated block comment"
	ErrUnexpectedCharacter      ErrorKind = "unexpected character"
)

type LexicalError struct {
	Kind     ErrorKind
	Position token.Position
	Message  string
}

func (e *LexicalError) Error() string {
	return fmt.Sprintf(
		"%s at %d:%d (byte %d): %s",
		e.Kind,
		e.Position.Line,
		e.Position.Column,
		e.Position.Offset,
		e.Message)
}
