package typecheck

import "Mio/internal/ast"

// forEachChild 对表达式 e 的每个直接子表达式调用 fn（不会进入 FunctionLiteral 函数体内部）。
func forEachChild(e ast.Expression, fn func(ast.Expression)) {
	switch n := e.(type) {
	case *ast.InfixExpression:
		fn(n.Left)
		fn(n.Right)
	case *ast.GroupedExpression:
		fn(n.Expr)
	case *ast.PrefixExpression:
		fn(n.Right)
	case *ast.CallExpression:
		fn(n.Function)
		for _, a := range n.Arguments {
			fn(a)
		}
	case *ast.TypeApplyExpression:
		fn(n.Function)
	case *ast.ConversionExpression:
		for _, a := range n.Arguments {
			fn(a)
		}
	case *ast.ArrayLiteral:
		for _, a := range n.Elements {
			fn(a)
		}
	case *ast.IndexExpression:
		fn(n.Left)
		fn(n.Index)
	case *ast.FieldExpression:
		fn(n.Left)
	case *ast.StructLiteral:
		for _, v := range n.Fields {
			fn(v.Value)
		}
	case *ast.EnumLiteral:
		for _, v := range n.Values {
			fn(v)
		}
	case *ast.TransferExpression:
		fn(n.Operand)
	}
}

// walkAllExpressions 遍历程序中的所有表达式，包括嵌套函数与闭包字面量内部。
func walkAllExpressions(program *ast.Program, visit func(ast.Expression)) {
	var expr func(e ast.Expression)
	var stmt func(s ast.Statement)
	var block func(b *ast.BlockStatement)
	expr = func(e ast.Expression) {
		if e == nil {
			return
		}
		visit(e)
		if lit, ok := e.(*ast.FunctionLiteral); ok {
			block(lit.Body)
			return
		}
		forEachChild(e, expr)
	}
	block = func(b *ast.BlockStatement) {
		if b == nil {
			return
		}
		for _, s := range b.Statements {
			stmt(s)
		}
	}
	stmt = func(s ast.Statement) {
		switch n := s.(type) {
		case *ast.LetStatement:
			expr(n.Value)
		case *ast.AssignStatement:
			expr(n.Value)
		case *ast.IndexAssignStatement:
			expr(n.Target)
			expr(n.Value)
		case *ast.ExpressionStatement:
			expr(n.Expr)
		case *ast.ReturnStatement:
			expr(n.Value)
		case *ast.ConstStatement:
			expr(n.Value)
		case *ast.BlockStatement:
			block(n)
		case *ast.IfStatement:
			expr(n.Condition)
			block(n.Consequence)
			block(n.Alternative)
		case *ast.WhileStatement:
			expr(n.Condition)
			block(n.Body)
		case *ast.MatchStatement:
			expr(n.Value)
			for _, arm := range n.Arms {
				block(arm.Body)
			}
		case *ast.FunctionStatement:
			block(n.Body)
		}
	}
	for _, s := range program.Statements {
		stmt(s)
	}
}

// visitIdentifiers 对语句块内读取的每个标识符调用 fn（包含 yuzuru 操作数与嵌套函数字面量内部）。
func (p *ownershipPass) visitIdentifiers(b *ast.BlockStatement, fn func(*ast.Identifier)) {
	if b == nil {
		return
	}
	tmp := &ast.Program{Statements: []ast.Statement{b}}
	walkAllExpressions(tmp, func(e ast.Expression) {
		if id, ok := e.(*ast.Identifier); ok {
			fn(id)
		}
	})
}
