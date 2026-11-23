package asynclower

import (
	"Mio/internal/ast"
	"Mio/internal/token"
)

const (
	tokenBang = token.Bang
	tokenAnd  = token.And
	tokenOr   = token.Or
)

// children calls fn for every direct sub-expression of e (function literal bodies are not entered).
func children(e ast.Expression, fn func(ast.Expression)) {
	switch x := e.(type) {
	case *ast.InfixExpression:
		fn(x.Left)
		fn(x.Right)
	case *ast.GroupedExpression:
		fn(x.Expr)
	case *ast.PrefixExpression:
		fn(x.Right)
	case *ast.AwaitExpression:
		fn(x.Operand)
	case *ast.CallExpression:
		fn(x.Function)
		for _, a := range x.Arguments {
			fn(a)
		}
	case *ast.TypeApplyExpression:
		fn(x.Function)
	case *ast.ConversionExpression:
		for _, a := range x.Arguments {
			fn(a)
		}
	case *ast.ArrayLiteral:
		for _, a := range x.Elements {
			fn(a)
		}
	case *ast.IndexExpression:
		fn(x.Left)
		fn(x.Index)
	case *ast.FieldExpression:
		fn(x.Left)
	case *ast.StructLiteral:
		for _, f := range x.Fields {
			fn(f.Value)
		}
	case *ast.EnumLiteral:
		for _, v := range x.Values {
			fn(v)
		}
	}
}

// statementChildren calls exprFn for the expressions directly owned by s and stmtFn for nested statements.
func statementChildren(s ast.Statement, exprFn func(ast.Expression), stmtFn func(ast.Statement)) {
	expr := func(e ast.Expression) {
		if e != nil && exprFn != nil {
			exprFn(e)
		}
	}
	stmt := func(st ast.Statement) {
		if st != nil && stmtFn != nil {
			stmtFn(st)
		}
	}
	block := func(b *ast.BlockStatement) {
		if b != nil {
			for _, st := range b.Statements {
				stmt(st)
			}
		}
	}
	switch x := s.(type) {
	case *ast.ExpressionStatement:
		expr(x.Expr)
	case *ast.ConstStatement:
		expr(x.Value)
	case *ast.LetStatement:
		expr(x.Value)
	case *ast.AssignStatement:
		expr(x.Value)
	case *ast.IndexAssignStatement:
		expr(x.Target.Left)
		expr(x.Target.Index)
		expr(x.Value)
	case *ast.BlockStatement:
		block(x)
	case *ast.IfStatement:
		expr(x.Condition)
		block(x.Consequence)
		block(x.Alternative)
	case *ast.WhileStatement:
		expr(x.Condition)
		block(x.Body)
	case *ast.ReturnStatement:
		expr(x.Value)
	case *ast.FunctionStatement:
		block(x.Body)
	case *ast.MatchStatement:
		expr(x.Value)
		for _, arm := range x.Arms {
			block(arm.Body)
		}
	}
}

// walkStatement visits every expression (including those inside function literals) and statement below s.
func walkStatement(s ast.Statement, exprFn func(ast.Expression), stmtFn func(ast.Statement)) {
	if s == nil {
		return
	}
	if stmtFn != nil {
		stmtFn(s)
	}
	var visit func(e ast.Expression)
	visit = func(e ast.Expression) {
		if exprFn != nil {
			exprFn(e)
		}
		if lit, ok := e.(*ast.FunctionLiteral); ok {
			walkStatement(lit.Body, exprFn, stmtFn)
			return
		}
		children(e, visit)
	}
	statementChildren(s, visit, func(st ast.Statement) { walkStatement(st, exprFn, stmtFn) })
}

func expressionHasAwait(e ast.Expression) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *ast.AwaitExpression:
		return true
	case *ast.FunctionLiteral:
		return false
	default:
		found := false
		children(x, func(c ast.Expression) {
			if !found && expressionHasAwait(c) {
				found = true
			}
		})
		return found
	}
}

// statementHasAwait reports whether s contains matsu outside nested function literals.
func statementHasAwait(s ast.Statement) bool {
	found := false
	statementChildren(s, func(e ast.Expression) {
		if !found && expressionHasAwait(e) {
			found = true
		}
	}, func(st ast.Statement) {
		if !found && statementHasAwait(st) {
			found = true
		}
	})
	return found
}

// statementTouchesControl reports whether s contains kaesu, nukeru or tsuzuku (outside function literals).
func statementTouchesControl(s ast.Statement) bool {
	switch s.(type) {
	case *ast.ReturnStatement, *ast.BreakStatement, *ast.ContinueStatement:
		return true
	}
	found := false
	statementChildren(s, nil, func(st ast.Statement) {
		if !found && statementTouchesControl(st) {
			found = true
		}
	})
	return found
}
