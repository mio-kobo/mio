// Package format implements the deterministic Mio source formatter.
//
// The formatter works on the lexer's token stream. Tokens are copied verbatim
// from the source (strings, runes, numbers keep their exact spelling); only the
// whitespace between tokens and the indentation of lines are rewritten. Comments
// live in the gaps between tokens and are re-emitted unchanged. The original line
// structure is preserved (runs of blank lines collapse to one), so the formatter
// never joins or splits statements.
//
// Safety: Format refuses sources that do not lex or parse, and before returning
// it verifies that (1) the token sequence is identical, (2) the comment sequence
// is identical and (3) formatting the output again is a no-op. Any violation is
// reported as an error and the caller must leave the file untouched.
package format

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

const utf8BOM = "\xef\xbb\xbf"

// IndentUnit is the single indentation level (four spaces, as in the examples).
const IndentUnit = "    "

type piece struct {
	typ  token.Type
	text string
	// gap is the raw text between the previous token and this one.
	gap string
}

// Format returns the canonical formatting of src.
func Format(src []byte) ([]byte, error) {
	out, err := formatOnce(src)
	if err != nil {
		return nil, err
	}
	if err := verify(src, out); err != nil {
		return nil, err
	}
	again, err := formatOnce(out)
	if err != nil {
		return nil, fmt.Errorf("internal formatter error: output does not parse: %w", err)
	}
	if string(again) != string(out) {
		return nil, errors.New("internal formatter error: formatting is not idempotent for this file")
	}
	return out, nil
}

func tokenize(text string) ([]piece, error) {
	if !utf8.ValidString(text) {
		return nil, errors.New("source is not valid UTF-8")
	}
	lex, err := lexer.NewChecked(text, token.V05)
	if err != nil {
		return nil, err
	}
	var pieces []piece
	prev := 0
	for {
		tok := lex.NextToken()
		if tok.Type == token.Illegal {
			return nil, fmt.Errorf("illegal token at line %d column %d", tok.Start.Line, tok.Start.Column)
		}
		if tok.Type == token.EOF {
			pieces = append(pieces, piece{typ: token.EOF, gap: text[prev:]})
			break
		}
		start, end := tok.Start.Offset, tok.End.Offset
		if start < prev || end < start || end > len(text) {
			return nil, fmt.Errorf("inconsistent token offsets at line %d", tok.Start.Line)
		}
		pieces = append(pieces, piece{typ: tok.Type, text: text[start:end], gap: text[prev:start]})
		prev = end
	}
	if err := lex.Err(); err != nil {
		return nil, err
	}
	return pieces, nil
}

func formatOnce(src []byte) ([]byte, error) {
	text := string(src)
	bom := ""
	if strings.HasPrefix(text, utf8BOM) {
		bom, text = utf8BOM, text[len(utf8BOM):]
	}
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.Contains(text, "\r") {
		return nil, errors.New("bare carriage return in source")
	}
	// The source must be a valid program; formatting never "fixes" errors.
	if _, err := parser.New(lexer.New(text, token.V05)).ParseProgram(); err != nil {
		return nil, fmt.Errorf("source does not parse: %w", err)
	}
	pieces, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	var b builder
	b.eol = "\n"
	depth := 0
	for i, p := range pieces {
		comments, err := splitGap(p.gap)
		if err != nil {
			return nil, err
		}
		prev := piece{typ: ""}
		if i > 0 {
			prev = pieces[i-1]
		}
		closers := 0
		if p.typ == token.RBrace || p.typ == token.RParen || p.typ == token.RBracket {
			closers = 1
		}
		for _, c := range comments {
			switch {
			case c.newlinesBefore == 0 && b.hasContent():
				b.trailingComment(c.text, c.spacesBefore)
			default:
				b.ownLineComment(c.newlinesBefore, depth, c.text, b.hasContent())
			}
		}
		if p.typ == token.EOF {
			break
		}
		newlines := trailingNewlines(p.gap, comments)
		if newlines > 0 && b.hasContent() {
			b.breakLines(newlines)
		}
		if b.atLineStart() {
			level := depth
			if closers > 0 {
				level--
			}
			if level < 0 {
				level = 0
			}
			b.indent(level)
		} else {
			if len(comments) > 0 && newlines == 0 {
				b.space(1)
			} else {
				b.space(spacing(i, pieces, p.typ, prev.typ, p.gap))
			}
		}
		b.write(p.text)
		switch p.typ {
		case token.LBrace, token.LParen, token.LBracket:
			depth++
		case token.RBrace, token.RParen, token.RBracket:
			if depth > 0 {
				depth--
			}
		}
	}
	out := b.finish()
	if eol != "\n" {
		out = strings.ReplaceAll(out, "\n", eol)
	}
	return []byte(bom + out), nil
}

// ---- gap handling ----

type gapComment struct {
	text           string
	newlinesBefore int // newlines between the previous item (token/comment) and this comment
	spacesBefore   int // horizontal blanks directly before the comment (tabs count as one)
}

// splitGap extracts comments from a whitespace/comment gap.
func splitGap(gap string) ([]gapComment, error) {
	var out []gapComment
	newlines := 0
	blanks := 0
	i := 0
	for i < len(gap) {
		switch {
		case gap[i] == '\n':
			newlines++
			blanks = 0
			i++
		case gap[i] == ' ' || gap[i] == '\t' || gap[i] == '\f' || gap[i] == '\v':
			blanks++
			i++
		case strings.HasPrefix(gap[i:], "//"):
			end := strings.IndexByte(gap[i:], '\n')
			if end < 0 {
				end = len(gap) - i
			}
			out = append(out, gapComment{text: strings.TrimRight(gap[i:i+end], " \t\f\v"), newlinesBefore: newlines, spacesBefore: blanks})
			i += end
			newlines = 0
			blanks = 0
		case strings.HasPrefix(gap[i:], "/*"):
			end := strings.Index(gap[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("unterminated block comment")
			}
			text := gap[i : i+2+end+2]
			out = append(out, gapComment{text: text, newlinesBefore: newlines, spacesBefore: blanks})
			i += len(text)
			newlines = 0
			blanks = 0
		default:
			return nil, fmt.Errorf("unexpected character %q between tokens", gap[i])
		}
	}
	return out, nil
}

// trailingNewlines counts newlines after the last comment (or in the whole gap
// when there is no comment) — the line break that separates it from the token.
func trailingNewlines(gap string, comments []gapComment) int {
	if len(comments) == 0 {
		return strings.Count(gap, "\n")
	}
	last := comments[len(comments)-1]
	idx := strings.LastIndex(gap, last.text)
	if idx < 0 {
		return 0
	}
	n := strings.Count(gap[idx+len(last.text):], "\n")
	// A line comment always ends its line.
	if strings.HasPrefix(last.text, "//") && n == 0 {
		n = 1
	}
	return n
}

// ---- output builder ----

type builder struct {
	sb          strings.Builder
	eol         string
	lineHasText bool
	any         bool
}

func (b *builder) hasContent() bool { return b.any }

func (b *builder) atLineStart() bool { return !b.lineHasText }

func (b *builder) write(s string) {
	b.sb.WriteString(s)
	b.lineHasText = true
	b.any = true
}

func (b *builder) space(n int) {
	if n > 0 {
		b.sb.WriteString(" ")
	}
}

func (b *builder) indent(level int) {
	b.sb.WriteString(strings.Repeat(IndentUnit, level))
}

// breakLines ends the current line; n>=2 keeps exactly one blank line.
func (b *builder) breakLines(n int) {
	if n > 2 {
		n = 2
	}
	if b.lineHasText {
		b.sb.WriteString(b.eol)
		n--
	} else {
		// A line comment already consumed one newline.
		n--
	}
	for ; n > 0; n-- {
		b.sb.WriteString(b.eol)
	}
	b.lineHasText = false
}

func (b *builder) trailingComment(text string, spaces int) {
	if b.lineHasText {
		if spaces < 1 {
			spaces = 1
		}
		b.sb.WriteString(strings.Repeat(" ", spaces))
	}
	b.sb.WriteString(text)
	b.lineHasText = true
}

func (b *builder) ownLineComment(newlinesBefore, level int, text string, started bool) {
	if started && newlinesBefore > 0 {
		b.breakLines(newlinesBefore)
	}
	if level < 0 {
		level = 0
	}
	b.indent(level)
	b.sb.WriteString(text)
	b.lineHasText = true
	b.any = true
	if strings.HasPrefix(text, "//") {
		// a line comment must be followed by a line break
		b.sb.WriteString(b.eol)
		b.lineHasText = false
	}
}

func (b *builder) finish() string {
	s := b.sb.String()
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + b.eol
}

// ---- verification ----

func verify(src, out []byte) error {
	a, err := tokenize(strings.TrimPrefix(strings.ReplaceAll(string(src), "\r\n", "\n"), utf8BOM))
	if err != nil {
		return err
	}
	b, err := tokenize(strings.TrimPrefix(strings.ReplaceAll(string(out), "\r\n", "\n"), utf8BOM))
	if err != nil {
		return fmt.Errorf("internal formatter error: output does not lex: %w", err)
	}
	if len(a) != len(b) {
		return errors.New("internal formatter error: token count changed")
	}
	var ca, cb []string
	for i := range a {
		if a[i].typ != b[i].typ || a[i].text != b[i].text {
			return fmt.Errorf("internal formatter error: token %d changed (%q -> %q)", i, a[i].text, b[i].text)
		}
		x, _ := splitGap(a[i].gap)
		y, _ := splitGap(b[i].gap)
		for _, c := range x {
			ca = append(ca, c.text)
		}
		for _, c := range y {
			cb = append(cb, c.text)
		}
	}
	if strings.Join(ca, "\x00") != strings.Join(cb, "\x00") {
		return errors.New("internal formatter error: comments changed")
	}
	return nil
}
