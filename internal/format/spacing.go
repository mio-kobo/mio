package format

import "Mio/internal/token"

// spacing returns 1 when a single space belongs between prev and cur on the
// same line, otherwise 0. Ambiguous pairs (generic angle brackets versus
// comparison operators) keep whatever the author wrote, so the formatter
// never has to guess at grammar and stays idempotent.
func spacing(i int, pieces []piece, cur, prev token.Type, gap string) int {
	preserved := 0
	if hasHorizontalSpace(gap) {
		preserved = 1
	}
	switch cur {
	case token.Comma, token.Semicolon, token.RParen, token.RBracket, token.Dot, token.Colon, token.Question:
		return 0
	}
	switch prev {
	case token.LParen, token.LBracket, token.Dot:
		return 0
	case token.Colon, token.Comma, token.Semicolon:
		return 1
	case token.Question:
		return preserved
	}
	if cur == token.LBrace {
		return 1
	}
	if prev == token.LBrace {
		if cur == token.RBrace {
			return preserved
		}
		return 1
	}
	if cur == token.RBrace {
		return 1
	}
	if isBinary(cur) || isBinary(prev) || cur == token.Arrow || prev == token.Arrow {
		return 1
	}
	if cur == token.Less || cur == token.Greater || prev == token.Less || prev == token.Greater {
		return preserved
	}
	if cur == token.Minus || cur == token.Asterisk {
		return 1
	}
	if prev == token.Minus || prev == token.Asterisk {
		if i >= 2 && !operandEnd(pieces[i-2].typ) {
			return 0 // unary
		}
		return 1
	}
	if prev == token.Bang {
		return 0
	}
	if cur == token.Bang {
		return 1
	}
	switch cur {
	case token.LParen:
		switch prev {
		case token.Ident, token.RParen, token.RBracket:
			return 0
		}
		return 1
	case token.LBracket:
		switch prev {
		case token.Ident, token.RParen, token.RBracket:
			return 0
		}
		return 1
	}
	if prev == token.RBracket && cur == token.Ident && i >= 2 && pieces[i-2].typ == token.LBracket {
		return 0 // []T
	}
	return 1
}

func hasHorizontalSpace(gap string) bool {
	for _, r := range gap {
		if r == ' ' || r == '\t' {
			return true
		}
	}
	return false
}

func isBinary(t token.Type) bool {
	switch t {
	case token.Assign, token.Plus, token.Slash, token.Equal, token.NotEqual,
		token.LessEqual, token.GreaterEqual, token.And, token.Or:
		return true
	}
	return false
}

func operandEnd(t token.Type) bool {
	switch t {
	case token.Ident, token.Integer, token.Float, token.String, token.Rune,
		token.True, token.False, token.Nil, token.RParen, token.RBracket, token.Question:
		return true
	}
	return false
}
