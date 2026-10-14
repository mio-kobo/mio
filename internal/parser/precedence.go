package parser

import "Mio/internal/token"

type precedence uint8

const (
	precLowest precedence = iota
	precOr
	precAnd
	precEquals
	precCompare
	precSum
	precProduct
	precPrefix
	precCall
	precIndex
	precField
)

var precedences = map[token.Type]precedence{
	token.Or:  precOr,
	token.And: precAnd,

	token.Equal:    precEquals,
	token.NotEqual: precEquals,

	token.Less:         precCompare,
	token.LessEqual:    precCompare,
	token.Greater:      precCompare,
	token.GreaterEqual: precCompare,

	token.Plus:  precSum,
	token.Minus: precSum,

	token.Asterisk: precProduct,
	token.Slash:    precProduct,

	token.LParen:   precCall,
	token.LBracket: precIndex,
	token.Dot:      precField,
}

func (p *Parser) peekPrecedence() precedence {
	if prec, ok := precedences[p.peekToken.Type]; ok {
		return prec
	}
	return precLowest
}

func (p *Parser) curPrecedence() precedence {
	if prec, ok := precedences[p.curToken.Type]; ok {
		return prec
	}
	return precLowest
}
