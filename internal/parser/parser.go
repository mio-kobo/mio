package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 前缀解析函数：从当前 Token 开始解析表达式。
type prefixParseFn func() (ast.Expression, error)

// 中缀解析函数：接收已经解析的左操作数。
type infixParseFn func(ast.Expression) (ast.Expression, error)

// Parser 使用双 Token 前瞻与 Pratt 算法构建 AST。
type Parser struct {
	l *lexer.Lexer

	curToken  token.Token
	peekToken token.Token

	prefixParseFns map[token.Type]prefixParseFn
	infixParseFns  map[token.Type]infixParseFn

	// recovering 为真时按语句粒度收集错误并继续解析。
	// 只有 LSP 的容错解析会启用它，编译器的 ParseProgram 行为不变。
	recovering bool
	errors     []error
}

// New 创建解析器，并注册表达式对应的解析函数。
func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:              l,
		prefixParseFns: make(map[token.Type]prefixParseFn),
		infixParseFns:  make(map[token.Type]infixParseFn),
	}

	// 基础表达式
	p.registerPrefix(token.Integer, p.parseIntegerLiteral)
	p.registerPrefix(token.Float, p.parseFloatLiteral)
	p.registerPrefix(token.String, p.parseStringLiteral)
	p.registerPrefix(token.Rune, p.parseRuneLiteral)
	p.registerPrefix(token.True, p.parseBooleanLiteral)
	p.registerPrefix(token.False, p.parseBooleanLiteral)
	p.registerPrefix(token.Nil, p.parseNilLiteral)
	p.registerPrefix(token.Ident, p.parseIdentifier)
	p.registerPrefix(token.Function, p.parseFunctionLiteral)

	// 一元运算与括号分组
	p.registerPrefix(token.Minus, p.parsePrefixExpression)
	p.registerPrefix(token.Bang, p.parsePrefixExpression)
	p.registerPrefix(token.LParen, p.parseGroupedExpression)
	p.registerPrefix(token.LBracket, p.parseArrayLiteral)

	// 算术运算
	p.registerInfix(token.Plus, p.parseInfixExpression)
	p.registerInfix(token.Minus, p.parseInfixExpression)
	p.registerInfix(token.Asterisk, p.parseInfixExpression)
	p.registerInfix(token.Slash, p.parseInfixExpression)

	// 比较运算
	p.registerInfix(token.Equal, p.parseInfixExpression)
	p.registerInfix(token.NotEqual, p.parseInfixExpression)
	p.registerInfix(token.Less, p.parseInfixExpression)
	p.registerInfix(token.LessEqual, p.parseInfixExpression)
	p.registerInfix(token.Greater, p.parseInfixExpression)
	p.registerInfix(token.GreaterEqual, p.parseInfixExpression)

	// 逻辑运算
	p.registerInfix(token.And, p.parseInfixExpression)
	p.registerInfix(token.Or, p.parseInfixExpression)

	// 函数调用：左侧已经存在表达式时，( 表示调用而非分组。
	p.registerInfix(token.LParen, p.parseCallExpression)
	p.registerInfix(token.LBracket, p.parseIndexExpression)
	p.registerInfix(token.Dot, p.parseFieldExpression)

	// 初始化当前 Token 和前瞻 Token。
	p.nextToken()
	p.nextToken()

	return p
}

// nextToken 将前瞻 Token 移入当前位置，并读取下一个 Token。
func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

// registerPrefix 注册表达式起始 Token 的解析函数。
func (p *Parser) registerPrefix(tokenType token.Type, fn prefixParseFn) {
	p.prefixParseFns[tokenType] = fn
}

// registerInfix 注册已有左操作数时的解析函数。
func (p *Parser) registerInfix(tokenType token.Type, fn infixParseFn) {
	p.infixParseFns[tokenType] = fn
}

func (p *Parser) expected(expected, context string, got token.Token) error {
	if got.Type == token.Illegal {
		if err := p.l.Err(); err != nil {
			return err
		}
	}
	cause := fmt.Errorf("expected %s%s, got %s", expected, context, got.Type)
	span := diagnostic.FromPositions(got.Start, got.End)
	return diagnostic.NewError("E0101", fmt.Sprintf("缺少 %s", expected), span).
		WithHelp(fmt.Sprintf("请在此处添加 %s", expected)).WithCause(cause)
}

// ParseProgram 解析整个程序，并记录源码范围。
func (p *Parser) ParseProgram() (*ast.Program, error) {
	program, err := p.ParseProgramPartial()
	if err != nil {
		return nil, err
	}
	return program, nil
}

// ParseProgramPartial 解析语句直到第一个错误，并返回已经完整解析的前缀。
// 它只服务于需要容错语义的工具链（例如 LSP）：错误时第一个返回值仍可能包含
// 合法语句，但调用方必须把第二个返回值当作"此源码不完整"的信号。
// ParseProgram 保持原有行为：出错时返回 nil。
func (p *Parser) ParseProgramPartial() (*ast.Program, error) {
	program := &ast.Program{}

	for p.curToken.Type != token.EOF {
		stmt, err := p.parseStatement()
		if err != nil {
			return program, err
		}

		program.Statements = append(program.Statements, stmt)
		p.nextToken()
	}

	if len(program.Statements) > 0 {
		first := program.Statements[0]
		last := program.Statements[len(program.Statements)-1]

		program.Range = ast.Span{
			Start: first.Span().Start,
			End:   last.Span().End,
		}
	} else {
		program.Range = ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		}
	}

	return program, nil
}

// ParseProgramRecovering 尽力解析整个程序：单条语句失败时记录错误并跳过，
// 其余语句仍然进入 AST。它只供 LSP 处理未完成源码使用；
// ParseProgram 与 ParseProgramPartial 的严格行为不受影响。
func (p *Parser) ParseProgramRecovering() (*ast.Program, []error) {
	p.recovering = true
	p.errors = nil
	defer func() { p.recovering = false }()

	program := &ast.Program{}
	for p.curToken.Type != token.EOF {
		stmt, err := p.parseStatement()
		if err != nil {
			p.errors = append(p.errors, err)
			if !p.skipToNextTopLevel() {
				break
			}
			continue
		}
		program.Statements = append(program.Statements, stmt)
		p.nextToken()
	}
	if len(program.Statements) > 0 {
		first := program.Statements[0]
		last := program.Statements[len(program.Statements)-1]
		program.Range = ast.Span{Start: first.Span().Start, End: last.Span().End}
	} else {
		program.Range = ast.Span{Start: p.curToken.Start, End: p.curToken.End}
	}
	return program, p.errors
}

// skipToNextTopLevel 跳到下一个顶层语句起点。
// 返回 false 表示已经到达 EOF。
func (p *Parser) skipToNextTopLevel() bool {
	depth := 0
	for p.curToken.Type != token.EOF {
		switch p.curToken.Type {
		case token.LBrace:
			depth++
		case token.RBrace:
			if depth > 0 {
				depth--
			}
		case token.Semicolon:
			if depth == 0 {
				p.nextToken()
				return true
			}
		}
		p.nextToken()
	}
	return false
}

// skipToStatementEnd 在代码块内跳过一条损坏的语句。
// 返回时停在当前代码块的右大括号或下一条语句起点。
func (p *Parser) skipToStatementEnd() bool {
	depth := 0
	for p.curToken.Type != token.EOF {
		switch p.curToken.Type {
		case token.LBrace:
			depth++
		case token.RBrace:
			if depth == 0 {
				return true
			}
			depth--
		case token.Semicolon:
			if depth == 0 {
				p.nextToken()
				return true
			}
		}
		p.nextToken()
	}
	return false
}

// parseStatement 根据当前 Token 分发语句解析。
func (p *Parser) parseStatement() (ast.Statement, error) {
	switch p.curToken.Type {
	case token.Import:
		return p.parseImportStatement()

	case token.Export:
		return p.parseExportStatement()

	case token.Const:
		return p.parseConstStatement(false)

	case token.Let:
		return p.parseLetStatement()

	case token.Function:
		return p.parseFunctionStatement()

	case token.Return:
		return p.parseReturnStatement()

	case token.If:
		return p.parseIfStatement()

	case token.While:
		return p.parseWhileStatement()

	case token.Break:
		return p.parseBreakStatement()

	case token.Continue:
		return p.parseContinueStatement()

	case token.Struct:
		return p.parseStructStatement()

	case token.Enum:
		return p.parseEnumStatement()

	case token.Match:
		return p.parseMatchStatement()

	case token.Ident:
		// 区分变量赋值（=）和普通表达式（如 ==）。
		if p.peekToken.Type == token.Assign {
			return p.parseAssignStatement()
		}
	}

	return p.parseExpressionStatement()
}

// parseImportStatement 解析 yobu 包导入
func (p *Parser) parseImportStatement() (*ast.ImportStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.String {
		return nil, p.expected("package path", " after yobu", p.peekToken)
	}
	p.nextToken()
	path := p.curToken.Literal
	end := p.curToken.End
	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", " after package path", p.peekToken)
	}
	p.nextToken()
	end = p.curToken.End
	parts := strings.Split(path, "/")
	return &ast.ImportStatement{Path: path, Alias: parts[len(parts)-1], Range: ast.Span{Start: start, End: end}}, nil
}

// parseExportStatement 解析 koukai 导出声明
func (p *Parser) parseExportStatement() (ast.Statement, error) {
	switch p.peekToken.Type {
	case token.Const:
		p.nextToken()
		return p.parseConstStatement(true)
	case token.Function:
		p.nextToken()
		declaration, err := p.parseFunctionStatement()
		if err == nil {
			declaration.Exported = true
		}
		return declaration, err
	case token.Struct:
		p.nextToken()
		declaration, err := p.parseStructStatement()
		if err == nil {
			declaration.Exported = true
		}
		return declaration, err
	case token.Enum:
		p.nextToken()
		declaration, err := p.parseEnumStatement()
		if err == nil {
			declaration.Exported = true
		}
		return declaration, err
	default:
		return nil, p.expected("teisu, waza, kata or sentaku", " after koukai", p.peekToken)
	}
}

// parseConstStatement 解析 teisu 编译期常量
func (p *Parser) parseConstStatement(exported bool) (*ast.ConstStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.Ident {
		return nil, p.expected("identifier", " after teisu", p.peekToken)
	}
	p.nextToken()
	name := &ast.Identifier{Name: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End}}
	if p.peekToken.Type == token.Colon {
		p.nextToken()
		ref, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		name.DeclaredType = ref
	}
	if p.peekToken.Type != token.Assign {
		return nil, p.expected("'='", " after constant name", p.peekToken)
	}
	p.nextToken()
	p.nextToken()
	value, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", " after constant value", p.peekToken)
	}
	p.nextToken()
	return &ast.ConstStatement{Name: name, Value: value, Exported: exported, Range: ast.Span{Start: start, End: p.curToken.End}}, nil
}

// parseWhileStatement 解析 aida 循环语句。
func (p *Parser) parseWhileStatement() (*ast.WhileStatement, error) {
	start := p.curToken.Start

	if p.peekToken.Type != token.LParen {
		return nil, p.expected("'('", " after aida", p.peekToken)
	}
	p.nextToken()
	p.nextToken()

	condition, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.RParen {
		return nil, p.expected("')'", " after aida condition", p.peekToken)
	}
	p.nextToken()

	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after aida condition", p.peekToken)
	}
	p.nextToken()

	body, err := p.parseBlockStatement()
	if err != nil {
		return nil, err
	}

	return &ast.WhileStatement{
		Condition: condition,
		Body:      body,
		Range: ast.Span{
			Start: start,
			End:   body.Span().End,
		},
	}, nil
}

// parseBreakStatement 解析 nukeru 语句。
func (p *Parser) parseBreakStatement() (*ast.BreakStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", " after nukeru", p.peekToken)
	}
	p.nextToken()
	return &ast.BreakStatement{Range: ast.Span{Start: start, End: p.curToken.End}}, nil
}

// parseContinueStatement 解析 tsuzuku 语句。
func (p *Parser) parseContinueStatement() (*ast.ContinueStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", " after tsuzuku", p.peekToken)
	}
	p.nextToken()
	return &ast.ContinueStatement{Range: ast.Span{Start: start, End: p.curToken.End}}, nil
}

// parseExpression 使用 Pratt 优先级规则解析表达式。
func (p *Parser) parseExpression(prec precedence) (ast.Expression, error) {
	prefix, ok := p.prefixParseFns[p.curToken.Type]
	if !ok {
		if err := p.l.Err(); err != nil {
			return nil, err
		}
		cause := fmt.Errorf(
			"no prefix parser for %s at %d:%d",
			p.curToken.Type,
			p.curToken.Start.Line,
			p.curToken.Start.Column,
		)
		return nil, diagnostic.NewError("E0102", "此处不是有效的表达式", diagnostic.FromPositions(p.curToken.Start, p.curToken.End)).WithCause(cause)
	}

	left, err := prefix()
	if err != nil {
		return nil, err
	}

	// 只有下一个运算符的优先级更高，才继续组合表达式。
	for prec < p.peekPrecedence() {
		infix, ok := p.infixParseFns[p.peekToken.Type]
		if !ok {
			break
		}

		p.nextToken()
		left, err = infix(left)
		if err != nil {
			return nil, err
		}
	}

	return left, nil
}

// parseIntegerLiteral 解析整数字面量。
func (p *Parser) parseIntegerLiteral() (ast.Expression, error) {
	if p.curToken.Type != token.Integer {
		return nil, fmt.Errorf("expected INTEGER, got %s", p.curToken.Type)
	}

	magnitude, err := strconv.ParseUint(p.curToken.Literal, 10, 64)

	return &ast.IntegerLiteral{
		Value:     int64(magnitude),
		Magnitude: magnitude,
		Overflow:  err != nil,
		Raw:       p.curToken.Literal,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseFloatLiteral 解析 float64 浮点字面量。
func (p *Parser) parseFloatLiteral() (ast.Expression, error) {
	value, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid float %q: %w", p.curToken.Literal, err)
	}

	return &ast.FloatLiteral{
		Value: value,
		Raw:   p.curToken.Literal,
		Bits:  64,
		Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End},
	}, nil
}

// parseBooleanLiteral 解析 hai / iie 布尔字面量。
func (p *Parser) parseBooleanLiteral() (ast.Expression, error) {
	if p.curToken.Type != token.True && p.curToken.Type != token.False {
		return nil, fmt.Errorf("expected BOOLEAN, got %s", p.curToken.Type)
	}

	return &ast.BooleanLiteral{
		Value: p.curToken.Type == token.True,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseStringLiteral 保存 Lexer 已解码的字符串内容。
func (p *Parser) parseStringLiteral() (ast.Expression, error) {
	return &ast.StringLiteral{
		Value: p.curToken.Literal,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseRuneLiteral 保存 Lexer 已验证的 Unicode 标量值。
func (p *Parser) parseRuneLiteral() (ast.Expression, error) {
	value, _ := utf8.DecodeRuneInString(p.curToken.Literal)
	return &ast.RuneLiteral{
		Value: int64(value),
		Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End},
	}, nil
}

// parseNilLiteral 解析 mu 空值字面量。
func (p *Parser) parseNilLiteral() (ast.Expression, error) {
	return &ast.NilLiteral{
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseIdentifier 解析标识符。
func (p *Parser) parseIdentifier() (ast.Expression, error) {
	identifier := &ast.Identifier{
		Name: p.curToken.Literal,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}
	if p.peekToken.Type == token.LBrace {
		return p.parseStructLiteral(identifier)
	}
	return identifier, nil
}

// parseStructLiteral 解析结构体构造
func (p *Parser) parseStructLiteral(name *ast.Identifier) (ast.Expression, error) {
	return p.parseStructLiteralWithArgs(name, nil)
}

func (p *Parser) parseStructLiteralWithArgs(name *ast.Identifier, typeArgs []*ast.TypeRef) (ast.Expression, error) {
	p.nextToken()
	fields := make([]ast.FieldValue, 0)
	if p.peekToken.Type == token.RBrace {
		p.nextToken()
		return &ast.StructLiteral{TypeName: name.Name, TypeArgs: typeArgs, Fields: fields, Range: ast.Span{Start: name.Span().Start, End: p.curToken.End}}, nil
	}
	for {
		p.nextToken()
		if p.curToken.Type != token.Ident {
			return nil, p.expected("field name", " in struct literal", p.curToken)
		}
		fieldName := p.curToken
		if p.peekToken.Type != token.Colon {
			return nil, p.expected("':'", " after field name", p.peekToken)
		}
		p.nextToken()
		p.nextToken()
		value, err := p.parseExpression(precLowest)
		if err != nil {
			return nil, err
		}
		fields = append(fields, ast.FieldValue{Name: fieldName.Literal, Value: value, Range: ast.Span{Start: fieldName.Start, End: value.Span().End}})
		if p.peekToken.Type != token.Comma {
			break
		}
		p.nextToken()
		if p.peekToken.Type == token.RBrace {
			break
		}
	}
	if p.peekToken.Type != token.RBrace {
		return nil, p.expected("'}'", " after struct fields", p.peekToken)
	}
	p.nextToken()
	return &ast.StructLiteral{TypeName: name.Name, TypeArgs: typeArgs, Fields: fields, Range: ast.Span{Start: name.Span().Start, End: p.curToken.End}}, nil
}

// parseFieldExpression 解析字段或枚举变体名称
func (p *Parser) parseFieldExpression(left ast.Expression) (ast.Expression, error) {
	if p.peekToken.Type != token.Ident {
		return nil, p.expected("field name", " after '.'", p.peekToken)
	}
	p.nextToken()
	field := &ast.FieldExpression{Left: left, Name: p.curToken.Literal, Range: ast.Span{Start: left.Span().Start, End: p.curToken.End}}
	if packageName, ok := left.(*ast.Identifier); ok && p.peekToken.Type == token.LBrace {
		qualified := &ast.Identifier{Name: packageName.Name + "." + field.Name, Range: field.Range}
		return p.parseStructLiteral(qualified)
	}
	return field, nil
}

// parseArrayLiteral 解析数组字面量
func (p *Parser) parseArrayLiteral() (ast.Expression, error) {
	open := p.curToken
	elements := make([]ast.Expression, 0)
	if p.peekToken.Type == token.RBracket {
		p.nextToken()
		return &ast.ArrayLiteral{
			Elements: elements,
			Range:    ast.Span{Start: open.Start, End: p.curToken.End},
		}, nil
	}

	p.nextToken()
	for {
		element, err := p.parseExpression(precLowest)
		if err != nil {
			return nil, err
		}
		elements = append(elements, element)
		if p.peekToken.Type != token.Comma {
			break
		}
		p.nextToken()
		if p.peekToken.Type == token.RBracket {
			return nil, fmt.Errorf("expected array element after ','")
		}
		p.nextToken()
	}
	if p.peekToken.Type != token.RBracket {
		return nil, p.expected("']'", " after array elements", p.peekToken)
	}
	p.nextToken()
	return &ast.ArrayLiteral{
		Elements: elements,
		Range:    ast.Span{Start: open.Start, End: p.curToken.End},
	}, nil
}

// parseIndexExpression 解析数组索引读取
func (p *Parser) parseIndexExpression(left ast.Expression) (ast.Expression, error) {
	_, identifier := left.(*ast.Identifier)
	_, field := left.(*ast.FieldExpression)
	if (identifier || field) && p.typeArgumentStartsAtPeek() {
		args := make([]*ast.TypeRef, 0, 1)
		for {
			arg, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.peekToken.Type != token.Comma {
				break
			}
			p.nextToken()
		}
		if p.peekToken.Type != token.RBracket {
			return nil, p.expected("']'", " after type arguments", p.peekToken)
		}
		p.nextToken()
		switch p.peekToken.Type {
		case token.LBrace:
			if named, ok := left.(*ast.Identifier); ok {
				return p.parseStructLiteralWithArgs(named, args)
			}
			if qualified, ok := left.(*ast.FieldExpression); ok {
				if packageName, ok := qualified.Left.(*ast.Identifier); ok {
					name := &ast.Identifier{Name: packageName.Name + "." + qualified.Name, Range: qualified.Range}
					return p.parseStructLiteralWithArgs(name, args)
				}
			}
			return nil, fmt.Errorf("invalid generic struct literal target")
		case token.LParen, token.Dot:
			return &ast.TypeApplyExpression{Function: left, Args: args, Range: ast.Span{Start: left.Span().Start, End: p.curToken.End}}, nil
		default:
			return nil, fmt.Errorf("explicit type arguments must be followed by a call, variant, or struct literal")
		}
	}
	p.nextToken()
	index, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.RBracket {
		return nil, p.expected("']'", " after array index", p.peekToken)
	}
	p.nextToken()
	return &ast.IndexExpression{
		Left:  left,
		Index: index,
		Range: ast.Span{Start: left.Span().Start, End: p.curToken.End},
	}, nil
}

func (p *Parser) typeArgumentStartsAtPeek() bool {
	if p.peekToken.Type == token.LBracket || p.peekToken.Type == token.LParen {
		return true
	}
	return p.peekToken.Type == token.Ident && looksLikeTypeName(p.peekToken.Literal)
}

func looksLikeTypeName(name string) bool {
	switch name {
	case "int8", "int16", "int32", "int64", "rune", "uint8", "uint16", "uint32", "float32", "float64", "bool", "string", "unit", "resource", "Map":
		return true
	}
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// parseGroupedExpression 解析括号分组，并保留括号的源码范围。
func (p *Parser) parseGroupedExpression() (ast.Expression, error) {
	open := p.curToken
	p.nextToken()

	// 括号内从最低优先级重新解析。
	expr, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}

	if p.peekToken.Type != token.RParen {
		return nil, p.expected("')'", "", p.peekToken)
	}
	p.nextToken()

	return &ast.GroupedExpression{
		Expr: expr,
		Ranges: ast.Span{
			Start: open.Start,
			End:   p.curToken.End,
		},
	}, nil
}

// parsePrefixExpression 解析一元负号与逻辑非。
func (p *Parser) parsePrefixExpression() (ast.Expression, error) {
	operator := p.curToken
	p.nextToken()

	// 一元运算使用 precPrefix，避免吞掉后面的低优先级运算。
	right, err := p.parseExpression(precPrefix)
	if err != nil {
		return nil, err
	}

	return &ast.PrefixExpression{
		Operator: operator.Type,
		Right:    right,
		Range: ast.Span{
			Start: operator.Start,
			End:   right.Span().End,
		},
	}, nil
}

// parseInfixExpression 解析算术、比较和逻辑中缀表达式。
func (p *Parser) parseInfixExpression(left ast.Expression) (ast.Expression, error) {
	operator := p.curToken.Type
	prec := p.curPrecedence()
	p.nextToken()

	// 使用当前运算符优先级解析右侧，保持左结合性。
	right, err := p.parseExpression(prec)
	if err != nil {
		return nil, err
	}

	return &ast.InfixExpression{
		Left:     left,
		Operator: operator,
		Right:    right,
		Range: ast.Span{
			Start: left.Span().Start,
			End:   right.Span().End,
		},
	}, nil
}

// parseCallExpression 解析函数调用和逗号分隔的实参。
func (p *Parser) parseCallExpression(function ast.Expression) (ast.Expression, error) {
	call := &ast.CallExpression{
		Function:  function,
		Arguments: make([]ast.Expression, 0),
	}

	// 空实参列表，例如 chant()。
	if p.peekToken.Type == token.RParen {
		p.nextToken()
		call.Range = ast.Span{
			Start: function.Span().Start,
			End:   p.curToken.End,
		}
		if identifier, ok := function.(*ast.Identifier); ok && isNumericTypeName(identifier.Name) {
			return &ast.ConversionExpression{
				Target:    identifier.Name,
				Arguments: call.Arguments,
				Range:     call.Range,
			}, nil
		}
		return call, nil
	}

	p.nextToken()
	arg, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	call.Arguments = append(call.Arguments, arg)

	// 每遇到一个逗号，就继续解析下一个实参。
	for p.peekToken.Type == token.Comma {
		p.nextToken() // 到逗号
		p.nextToken() // 到下一个实参

		arg, err = p.parseExpression(precLowest)
		if err != nil {
			return nil, err
		}
		call.Arguments = append(call.Arguments, arg)
	}

	if p.peekToken.Type != token.RParen {
		return nil, p.expected("')'", " after function arguments", p.peekToken)
	}
	p.nextToken()

	call.Range = ast.Span{
		Start: function.Span().Start,
		End:   p.curToken.End,
	}
	if identifier, ok := function.(*ast.Identifier); ok && isNumericTypeName(identifier.Name) {
		return &ast.ConversionExpression{
			Target:    identifier.Name,
			Arguments: call.Arguments,
			Range:     call.Range,
		}, nil
	}
	return call, nil
}

func isNumericTypeName(name string) bool {
	switch name {
	case "int8", "int16", "int32", "int64", "rune", "uint8", "uint16", "uint32", "float32", "float64":
		return true
	default:
		return false
	}
}

// parseExpressionStatement 解析以分号结束的表达式语句。
func (p *Parser) parseExpressionStatement() (ast.Statement, error) {
	expr, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type == token.Assign {
		target, ok := expr.(*ast.IndexExpression)
		if !ok {
			return nil, p.expected("';'", "", p.peekToken)
		}
		return p.parseIndexAssignStatement(target)
	}

	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", "", p.peekToken)
	}
	p.nextToken()

	return &ast.ExpressionStatement{
		Expr: expr,
		Range: ast.Span{
			Start: expr.Span().Start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseIndexAssignStatement 解析数组元素赋值语句
func (p *Parser) parseIndexAssignStatement(target *ast.IndexExpression) (*ast.IndexAssignStatement, error) {
	p.nextToken()
	p.nextToken()
	value, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", "", p.peekToken)
	}
	p.nextToken()
	return &ast.IndexAssignStatement{
		Target: target,
		Value:  value,
		Range:  ast.Span{Start: target.Span().Start, End: p.curToken.End},
	}, nil
}

// parseLetStatement 解析 kaku 变量声明语句。
func (p *Parser) parseLetStatement() (*ast.LetStatement, error) {
	start := p.curToken.Start

	if p.peekToken.Type != token.Ident {
		return nil, p.expected("identifier", " after kaku", p.peekToken)
	}
	p.nextToken()

	name := &ast.Identifier{
		Name: p.curToken.Literal,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}
	if p.peekToken.Type == token.Colon {
		p.nextToken()
		ref, err := p.parseTypeRef()
		if err != nil {
			return nil, fmt.Errorf("expected variable type after ':': %w", err)
		}
		name.DeclaredType = ref
	}

	if p.peekToken.Type != token.Assign {
		return nil, p.expected("'='", fmt.Sprintf(" after %q", name.Name), p.peekToken)
	}

	// 越过 =，到初始化表达式。
	p.nextToken()
	p.nextToken()

	value, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}

	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", "", p.peekToken)
	}
	p.nextToken()

	return &ast.LetStatement{
		Name:  name,
		Value: value,
		Range: ast.Span{
			Start: start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseAssignStatement 解析已有变量的赋值语句。
func (p *Parser) parseAssignStatement() (*ast.AssignStatement, error) {
	start := p.curToken.Start

	name := &ast.Identifier{
		Name: p.curToken.Literal,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}

	if p.peekToken.Type != token.Assign {
		return nil, p.expected("'='", fmt.Sprintf(" after %q", name.Name), p.peekToken)
	}

	// 越过 =，到右侧表达式。
	p.nextToken()
	p.nextToken()

	value, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}

	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", "", p.peekToken)
	}
	p.nextToken()

	return &ast.AssignStatement{
		Name:  name,
		Value: value,
		Range: ast.Span{
			Start: start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseBlockStatement 解析大括号中的多条语句。
func (p *Parser) parseBlockStatement() (*ast.BlockStatement, error) {
	open := p.curToken
	block := &ast.BlockStatement{
		Statements: make([]ast.Statement, 0),
	}

	p.nextToken()
	for p.curToken.Type != token.RBrace {
		if p.curToken.Type == token.EOF {
			if p.recovering {
				// 容错模式：未闭合的代码块保留已经解析的语句。
				block.Range = ast.Span{Start: open.Start, End: p.curToken.End}
				return block, nil
			}
			cause := fmt.Errorf("expected '}' to close block at %d:%d", open.Start.Line, open.Start.Column)
			return nil, diagnostic.NewError("E0101", "缺少 '}'", diagnostic.FromPositions(p.curToken.Start, p.curToken.End)).
				WithLabel(diagnostic.FromPositions(open.Start, open.End), "代码块从这里开始").WithHelp("请添加 '}' 关闭代码块").WithCause(cause)
		}

		stmt, err := p.parseStatement()
		if err != nil {
			if !p.recovering {
				return nil, err
			}
			p.errors = append(p.errors, err)
			if !p.skipToStatementEnd() {
				block.Range = ast.Span{Start: open.Start, End: p.curToken.End}
				return block, nil
			}
			continue
		}
		block.Statements = append(block.Statements, stmt)
		p.nextToken()
	}

	// 返回时停在当前代码块的右大括号。
	block.Range = ast.Span{
		Start: open.Start,
		End:   p.curToken.End,
	}
	return block, nil
}

// parseIfStatement 解析 moshi / sore 条件语句。
func (p *Parser) parseIfStatement() (*ast.IfStatement, error) {
	start := p.curToken.Start

	if p.peekToken.Type != token.LParen {
		return nil, p.expected("'('", " after moshi", p.peekToken)
	}

	// 越过左括号，进入条件表达式。
	p.nextToken()
	p.nextToken()

	condition, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}

	if p.peekToken.Type != token.RParen {
		return nil, p.expected("')'", " after condition", p.peekToken)
	}
	p.nextToken()

	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after moshi condition", p.peekToken)
	}
	p.nextToken()

	consequence, err := p.parseBlockStatement()
	if err != nil {
		return nil, err
	}

	// sore 可以省略；它出现时必须跟随代码块。
	var alternative *ast.BlockStatement
	end := consequence.Span().End
	if p.peekToken.Type == token.Else {
		p.nextToken()
		if p.peekToken.Type != token.LBrace {
			return nil, p.expected("'{'", " after sore", p.peekToken)
		}
		p.nextToken()

		alternative, err = p.parseBlockStatement()
		if err != nil {
			return nil, err
		}
		end = alternative.Span().End
	}

	return &ast.IfStatement{
		Condition:   condition,
		Consequence: consequence,
		Alternative: alternative,
		Range: ast.Span{
			Start: start,
			End:   end,
		},
	}, nil
}

// parseReturnStatement 解析带值或无值返回语句
func (p *Parser) parseReturnStatement() (*ast.ReturnStatement, error) {
	start := p.curToken.Start

	// kaesu; 无返回表达式
	if p.peekToken.Type == token.Semicolon {
		p.nextToken()

		return &ast.ReturnStatement{
			Value: nil,
			Range: ast.Span{
				Start: start,
				End:   p.curToken.End,
			},
		}, nil
	}

	// 移动到返回表达式
	p.nextToken()

	// 解析返回表达式
	value, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}

	// 检查语句结束符
	if p.peekToken.Type != token.Semicolon {
		return nil, p.expected("';'", " after kaesu expression", p.peekToken)
	}

	p.nextToken()

	return &ast.ReturnStatement{
		Value: value,
		Range: ast.Span{
			Start: start,
			End:   p.curToken.End,
		},
	}, nil
}

// parseFunctionParameters 解析函数形参及可选类型标注
func (p *Parser) parseFunctionParameters() ([]*ast.Identifier, error) {
	params := make([]*ast.Identifier, 0)

	// 处理空形参列表
	if p.peekToken.Type == token.RParen {
		p.nextToken()
		return params, nil
	}

	for {
		// 移动到形参名称
		p.nextToken()

		if p.curToken.Type != token.Ident {
			return nil, fmt.Errorf(
				"expected parameter name, got %s",
				p.curToken.Type,
			)
		}

		param := &ast.Identifier{
			Name: p.curToken.Literal,
			Range: ast.Span{
				Start: p.curToken.Start,
				End:   p.curToken.End,
			},
		}

		// 解析可选的类型标注
		if p.peekToken.Type == token.Colon {
			// 移动到冒号
			p.nextToken()

			ref, err := p.parseTypeRef()
			if err != nil {
				return nil, fmt.Errorf("expected parameter type after ':': %w", err)
			}
			param.DeclaredType = ref
		}

		params = append(params, param)

		// 没有逗号则结束形参解析
		if p.peekToken.Type != token.Comma {
			break
		}

		p.nextToken()
	}

	// 形参列表必须以右括号结束
	if p.peekToken.Type != token.RParen {
		return nil, p.expected("')'", " after parameters", p.peekToken)
	}

	p.nextToken()

	return params, nil
}

// parseFunctionStatement 解析函数定义及类型标注
func (p *Parser) parseFunctionStatement() (*ast.FunctionStatement, error) {
	start := p.curToken.Start

	// 检查函数名称
	if p.peekToken.Type != token.Ident {
		return nil, p.expected("function name", " after waza", p.peekToken)
	}

	p.nextToken()

	name := &ast.Identifier{
		Name: p.curToken.Literal,
		Range: ast.Span{
			Start: p.curToken.Start,
			End:   p.curToken.End,
		},
	}
	typeParams, constraints, err := p.parseTypeParameters()
	if err != nil {
		return nil, err
	}

	// 检查形参列表的左括号
	if p.peekToken.Type != token.LParen {
		return nil, p.expected("'('", " after function name", p.peekToken)
	}

	p.nextToken()

	// 解析函数形参
	params, err := p.parseFunctionParameters()
	if err != nil {
		return nil, err
	}

	// 解析可选的返回类型
	var returnType *ast.TypeRef

	if p.peekToken.Type == token.Arrow {
		// 移动到 ->
		p.nextToken()

		returnType, err = p.parseTypeRef()
		if err != nil {
			return nil, fmt.Errorf("expected return type after '->': %w", err)
		}
	}

	// 检查函数体左大括号
	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after function signature", p.peekToken)
	}

	p.nextToken()

	// 解析函数体
	body, err := p.parseBlockStatement()
	if err != nil {
		return nil, err
	}

	return &ast.FunctionStatement{
		Name:        name,
		TypeParams:  typeParams,
		Constraints: constraints,
		Parameters:  params,
		ReturnType:  returnType,
		Body:        body,
		Range: ast.Span{
			Start: start,
			End:   body.Span().End,
		},
	}, nil
}

// parseFunctionLiteral 解析匿名函数表达式
func (p *Parser) parseFunctionLiteral() (ast.Expression, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.LParen {
		return nil, p.expected("'('", " after waza", p.peekToken)
	}
	p.nextToken()
	params, err := p.parseFunctionParameters()
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.Arrow {
		return nil, p.expected("'->'", " after anonymous function parameters", p.peekToken)
	}
	p.nextToken()
	returnType, err := p.parseTypeRef()
	if err != nil {
		return nil, fmt.Errorf("expected return type after '->': %w", err)
	}
	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after anonymous function signature", p.peekToken)
	}
	p.nextToken()
	body, err := p.parseBlockStatement()
	if err != nil {
		return nil, err
	}
	return &ast.FunctionLiteral{
		Parameters: params,
		ReturnType: returnType,
		Body:       body,
		Range:      ast.Span{Start: start, End: body.Span().End},
	}, nil
}

// parseTypeParameters 解析声明上的类型参数
func (p *Parser) parseTypeParameters() ([]string, map[string]string, error) {
	params := make([]string, 0)
	constraints := make(map[string]string)
	if p.peekToken.Type != token.Less {
		return params, constraints, nil
	}
	p.nextToken()
	for {
		if p.peekToken.Type != token.Ident {
			return nil, nil, p.expected("type parameter", " in '<...>'", p.peekToken)
		}
		p.nextToken()
		name := p.curToken.Literal
		params = append(params, name)
		if p.peekToken.Type == token.Colon {
			p.nextToken()
			if p.peekToken.Type != token.Ident {
				return nil, nil, p.expected("constraint", " after ':'", p.peekToken)
			}
			p.nextToken()
			constraints[name] = p.curToken.Literal
		}
		if p.peekToken.Type != token.Comma {
			break
		}
		p.nextToken()
	}
	if p.peekToken.Type != token.Greater {
		return nil, nil, p.expected("'>'", " after type parameters", p.peekToken)
	}
	p.nextToken()
	return params, constraints, nil
}

// parseStructStatement 解析结构体声明
func (p *Parser) parseStructStatement() (*ast.StructStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.Ident {
		return nil, p.expected("struct name", " after kata", p.peekToken)
	}
	p.nextToken()
	name := &ast.Identifier{Name: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End}}
	typeParams, constraints, err := p.parseTypeParameters()
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after struct name", p.peekToken)
	}
	p.nextToken()
	fields := make([]*ast.Identifier, 0)
	for p.peekToken.Type != token.RBrace {
		p.nextToken()
		if p.curToken.Type != token.Ident {
			return nil, p.expected("field name", " in struct declaration", p.curToken)
		}
		field := &ast.Identifier{Name: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End}}
		if p.peekToken.Type != token.Colon {
			return nil, p.expected("':'", " after field name", p.peekToken)
		}
		p.nextToken()
		ref, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		field.DeclaredType = ref
		fields = append(fields, field)
		if p.peekToken.Type != token.Comma && p.peekToken.Type != token.Semicolon {
			return nil, p.expected("',' or ';'", " after field declaration", p.peekToken)
		}
		p.nextToken()
	}
	p.nextToken()
	return &ast.StructStatement{Name: name, TypeParams: typeParams, Constraints: constraints, Fields: fields, Range: ast.Span{Start: start, End: p.curToken.End}}, nil
}

// parseEnumStatement 解析枚举声明
func (p *Parser) parseEnumStatement() (*ast.EnumStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.Ident {
		return nil, p.expected("enum name", " after sentaku", p.peekToken)
	}
	p.nextToken()
	name := &ast.Identifier{Name: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End}}
	typeParams, constraints, err := p.parseTypeParameters()
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after enum name", p.peekToken)
	}
	p.nextToken()
	variants := make([]ast.EnumVariant, 0)
	for p.peekToken.Type != token.RBrace {
		p.nextToken()
		if p.curToken.Type != token.Ident {
			return nil, p.expected("variant name", " in enum declaration", p.curToken)
		}
		variant := ast.EnumVariant{Name: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End}}
		if p.peekToken.Type == token.LParen {
			p.nextToken()
			for p.peekToken.Type != token.RParen {
				ref, err := p.parseTypeRef()
				if err != nil {
					return nil, err
				}
				variant.Fields = append(variant.Fields, ref)
				if p.peekToken.Type != token.Comma {
					break
				}
				p.nextToken()
			}
			if p.peekToken.Type != token.RParen {
				return nil, p.expected("')'", " after variant fields", p.peekToken)
			}
			p.nextToken()
			variant.Range.End = p.curToken.End
		}
		variants = append(variants, variant)
		if p.peekToken.Type != token.Comma && p.peekToken.Type != token.Semicolon {
			return nil, p.expected("',' or ';'", " after enum variant", p.peekToken)
		}
		p.nextToken()
	}
	p.nextToken()
	return &ast.EnumStatement{Name: name, TypeParams: typeParams, Constraints: constraints, Variants: variants, Range: ast.Span{Start: start, End: p.curToken.End}}, nil
}

// parseMatchStatement 解析枚举穷尽分支
func (p *Parser) parseMatchStatement() (*ast.MatchStatement, error) {
	start := p.curToken.Start
	if p.peekToken.Type != token.LParen {
		return nil, p.expected("'('", " after awase", p.peekToken)
	}
	p.nextToken()
	p.nextToken()
	value, err := p.parseExpression(precLowest)
	if err != nil {
		return nil, err
	}
	if p.peekToken.Type != token.RParen {
		return nil, p.expected("')'", " after match value", p.peekToken)
	}
	p.nextToken()
	if p.peekToken.Type != token.LBrace {
		return nil, p.expected("'{'", " after match value", p.peekToken)
	}
	p.nextToken()
	arms := make([]ast.MatchArm, 0)
	for p.peekToken.Type != token.RBrace {
		p.nextToken()
		if p.curToken.Type != token.Case {
			return nil, p.expected("baai", " in awase", p.curToken)
		}
		if p.peekToken.Type != token.Ident {
			return nil, p.expected("variant name", " after baai", p.peekToken)
		}
		p.nextToken()
		arm := ast.MatchArm{Variant: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start}}
		if p.peekToken.Type == token.LParen {
			p.nextToken()
			for p.peekToken.Type != token.RParen {
				p.nextToken()
				if p.curToken.Type != token.Ident {
					return nil, p.expected("binding name", " in baai", p.curToken)
				}
				arm.Bindings = append(arm.Bindings, &ast.Identifier{Name: p.curToken.Literal, Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End}})
				if p.peekToken.Type != token.Comma {
					break
				}
				p.nextToken()
			}
			if p.peekToken.Type != token.RParen {
				return nil, p.expected("')'", " after baai bindings", p.peekToken)
			}
			p.nextToken()
		}
		if p.peekToken.Type != token.LBrace {
			return nil, p.expected("'{'", " after baai pattern", p.peekToken)
		}
		p.nextToken()
		arm.Body, err = p.parseBlockStatement()
		if err != nil {
			return nil, err
		}
		arm.Range.End = arm.Body.Span().End
		arms = append(arms, arm)
	}
	p.nextToken()
	return &ast.MatchStatement{Value: value, Arms: arms, Range: ast.Span{Start: start, End: p.curToken.End}}, nil
}

// parseTypeRef 解析当前位置之后的类型标注
func (p *Parser) parseTypeRef() (*ast.TypeRef, error) {
	var ref *ast.TypeRef
	if p.peekToken.Type == token.Ident {
		p.nextToken()
		ref = &ast.TypeRef{
			Name:  p.curToken.Literal,
			Range: ast.Span{Start: p.curToken.Start, End: p.curToken.End},
		}
		if p.peekToken.Type == token.Dot {
			p.nextToken()
			if p.peekToken.Type != token.Ident {
				return nil, p.expected("type name", " after '.'", p.peekToken)
			}
			p.nextToken()
			ref.Name += "." + p.curToken.Literal
			ref.Range.End = p.curToken.End
		}
		if p.peekToken.Type == token.Less {
			p.nextToken()
			for {
				arg, err := p.parseTypeRef()
				if err != nil {
					return nil, err
				}
				ref.Args = append(ref.Args, arg)
				if p.peekToken.Type != token.Comma {
					break
				}
				p.nextToken()
			}
			if p.peekToken.Type != token.Greater {
				return nil, p.expected("'>'", " after type arguments", p.peekToken)
			}
			p.nextToken()
			ref.Range.End = p.curToken.End
		}
	} else if p.peekToken.Type == token.LBracket {
		p.nextToken()
		start := p.curToken.Start
		if p.peekToken.Type != token.RBracket {
			return nil, p.expected("']'", " in array type", p.peekToken)
		}
		p.nextToken()
		element, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		ref = &ast.TypeRef{
			Name:    "[]",
			Element: element,
			Range:   ast.Span{Start: start, End: element.Span().End},
		}
	} else if p.peekToken.Type == token.LParen {
		p.nextToken()
		start := p.curToken.Start
		params := make([]*ast.TypeRef, 0)
		if p.peekToken.Type != token.RParen {
			for {
				inner, err := p.parseTypeRef()
				if err != nil {
					return nil, err
				}
				params = append(params, inner)
				if p.peekToken.Type != token.Comma {
					break
				}
				p.nextToken()
			}
		}
		if p.peekToken.Type != token.RParen {
			return nil, p.expected("')'", " in grouped type", p.peekToken)
		}
		p.nextToken()
		if p.peekToken.Type == token.Arrow {
			p.nextToken()
			result, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			ref = &ast.TypeRef{Name: "fn", Params: params, Result: result, Range: ast.Span{Start: start, End: result.Span().End}}
		} else if len(params) == 1 {
			ref = params[0]
			ref.Range = ast.Span{Start: start, End: p.curToken.End}
		} else {
			return nil, fmt.Errorf("grouped type requires one type or a function arrow")
		}
	} else {
		return nil, p.expected("type", "", p.peekToken)
	}

	for p.peekToken.Type == token.Question {
		p.nextToken()
		ref = &ast.TypeRef{
			Name:    "?",
			Element: ref,
			Range:   ast.Span{Start: ref.Span().Start, End: p.curToken.End},
		}
	}
	return ref, nil
}
