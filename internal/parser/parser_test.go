package parser

import (
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 测试当前 Token 与前瞻 Token 的移动顺序
func TestNextToken(t *testing.T) {
	input := "1+2*3;"

	l := lexer.New(input, token.V01)
	p := New(l)

	tests := []struct {
		cur  token.Type
		peek token.Type
	}{
		{token.Integer, token.Plus},
		{token.Plus, token.Integer},
		{token.Integer, token.Asterisk},
		{token.Asterisk, token.Integer},
		{token.Integer, token.Semicolon},
		{token.Semicolon, token.EOF},
	}

	for i, tt := range tests {
		if p.curToken.Type != tt.cur {
			t.Fatalf(
				"step %d: curToken=%s, want=%s",
				i, p.curToken.Type, tt.cur,
			)
		}

		if p.peekToken.Type != tt.peek {
			t.Fatalf(
				"step %d: peekToken=%s, want=%s",
				i, p.peekToken.Type, tt.peek,
			)
		}

		p.nextToken()
	}
}

// 测试多个整数表达式语句的解析
func TestParseIntegerProgram(t *testing.T) {
	input := "123; 456;"

	l, err := lexer.NewChecked(input, token.V01)
	if err != nil {
		t.Fatal(err)
	}

	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf(
			"expected 2 statements, got %d",
			len(program.Statements),
		)
	}

	expected := []int64{123, 456}

	// 验证语句类型及整数字面量
	for i, want := range expected {
		stmt, ok := program.Statements[i].(*ast.ExpressionStatement)
		if !ok {
			t.Fatalf("statement[%d] is not ExpressionStatement", i)
		}

		integer, ok := stmt.Expr.(*ast.IntegerLiteral)
		if !ok {
			t.Fatalf("statement[%d] is not IntegerLiteral", i)
		}

		if integer.Value != want {
			t.Fatalf(
				"statement[%d]: got %d, want %d",
				i,
				integer.Value,
				want,
			)
		}
	}
}

// 测试运算符优先级与前瞻查询
func TestPeekPrecedence(t *testing.T) {
	l := lexer.New("1 + 2 * 3;", token.V01)
	p := New(l)

	// 检查加法优先级
	if got := p.peekPrecedence(); got != precSum {
		t.Fatalf("got %d, want %d", got, precSum)
	}

	p.nextToken()
	p.nextToken()

	// 检查乘法优先级
	if got := p.peekPrecedence(); got != precProduct {
		t.Fatalf("got %d, want %d", got, precProduct)
	}
}

// 测试整数前缀解析函数的注册与调用
func TestPrefixParserRegistration(t *testing.T) {
	l := lexer.New("123;", token.V01)
	p := New(l)

	prefix, ok := p.prefixParseFns[token.Integer]

	if !ok {
		t.Fatal("INTEGER prefix parser not registered")
	}

	expr, err := prefix()
	if err != nil {
		t.Fatal(err)
	}

	integer, ok := expr.(*ast.IntegerLiteral)
	if !ok {
		t.Fatalf(
			"got %T, want *ast.IntegerLiteral",
			expr,
		)
	}

	if integer.Value != 123 {
		t.Fatalf(
			"got %d, want 123",
			integer.Value,
		)
	}
}

// 测试中缀表达式的左右操作数解析
func TestParseInfixExpression(t *testing.T) {
	l := lexer.New("1 + 2;", token.V01)
	p := New(l)

	// 解析左操作数
	left, err := p.parseIntegerLiteral()
	if err != nil {
		t.Fatal(err)
	}

	// 移动到中缀运算符
	p.nextToken()

	result, err := p.parseInfixExpression(left)
	if err != nil {
		t.Fatal(err)
	}

	// 验证加法表达式
	infix, ok := result.(*ast.InfixExpression)
	if !ok {
		t.Fatalf("expected InfixExpression, got %T", result)
	}

	if infix.Operator != token.Plus {
		t.Fatalf("expected PLUS, got %s", infix.Operator)
	}

	leftInt, ok := infix.Left.(*ast.IntegerLiteral)
	if !ok || leftInt.Value != 1 {
		t.Fatal("incorrect left operand")
	}

	rightInt, ok := infix.Right.(*ast.IntegerLiteral)
	if !ok || rightInt.Value != 2 {
		t.Fatal("incorrect right operand")
	}
}

// 测试 Pratt Parser 的算术运算符优先级
func TestPrattPrecedence(t *testing.T) {
	l := lexer.New("1 + 2 * 3;", token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatal("expected one statement")
	}

	stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatal("expected ExpressionStatement")
	}

	// 加法为根节点
	root, ok := stmt.Expr.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected InfixExpression")
	}

	if root.Operator != token.Plus {
		t.Fatalf("expected PLUS, got %s", root.Operator)
	}

	// 左子树为整数 1
	left, ok := root.Left.(*ast.IntegerLiteral)
	if !ok || left.Value != 1 {
		t.Fatal("expected IntegerLiteral(1)")
	}

	// 右子树为乘法表达式
	right, ok := root.Right.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected multiplication expression")
	}

	if right.Operator != token.Asterisk {
		t.Fatalf("expected ASTERISK, got %s", right.Operator)
	}

	// 验证乘法操作数
	two, ok := right.Left.(*ast.IntegerLiteral)
	if !ok || two.Value != 2 {
		t.Fatal("expected IntegerLiteral(2)")
	}

	three, ok := right.Right.(*ast.IntegerLiteral)
	if !ok || three.Value != 3 {
		t.Fatal("expected IntegerLiteral(3)")
	}
}

// 测试基础算术运算符的中缀注册
func TestInfixRegistration(t *testing.T) {
	l := lexer.New("1 + 2;", token.V01)
	p := New(l)

	operators := []token.Type{
		token.Plus,
		token.Minus,
		token.Asterisk,
		token.Slash,
	}

	for _, op := range operators {
		if _, ok := p.infixParseFns[op]; !ok {
			t.Errorf(
				"infix parser not registered for %s",
				op,
			)
		}
	}
}

// 测试括号分组对 AST 结构的影响
func TestGroupedExpression(t *testing.T) {
	l := lexer.New("(1 + 2) * 3;", token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	stmt := program.Statements[0].(*ast.ExpressionStatement)

	// 乘法为根节点
	mul, ok := stmt.Expr.(*ast.InfixExpression)
	if !ok || mul.Operator != token.Asterisk {
		t.Fatal("expected multiplication expression")
	}

	// 左子树为括号表达式
	group, ok := mul.Left.(*ast.GroupedExpression)
	if !ok {
		t.Fatal("expected grouped expression")
	}

	// 括号内部为加法表达式
	add, ok := group.Expr.(*ast.InfixExpression)
	if !ok || add.Operator != token.Plus {
		t.Fatal("expected addition inside parentheses")
	}

	// 验证括号范围包含左右括号
	if group.Span().Start.Offset != 0 ||
		group.Span().End.Offset != 7 {
		t.Fatalf("incorrect group span: %+v", group.Span())
	}
}

// 测试缺少右括号时的语法错误
func TestMissingRightParen(t *testing.T) {
	l := lexer.New("(1 + 2;", token.V01)
	p := New(l)

	_, err := p.ParseProgram()

	if err == nil {
		t.Fatal("expected missing right parenthesis error")
	}
}

// 测试一元负号与乘法的优先级
func TestPrefixExpression(t *testing.T) {
	l := lexer.New("-10 * 2;", token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	stmt := program.Statements[0].(*ast.ExpressionStatement)

	// 乘法为根节点
	multiply, ok := stmt.Expr.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected InfixExpression")
	}

	if multiply.Operator != token.Asterisk {
		t.Fatal("expected multiplication")
	}

	// 乘法左侧为一元负号
	prefix, ok := multiply.Left.(*ast.PrefixExpression)
	if !ok {
		t.Fatal("expected PrefixExpression")
	}

	if prefix.Operator != token.Minus {
		t.Fatal("expected unary minus")
	}

	// 负号作用于整数 10
	integer, ok := prefix.Right.(*ast.IntegerLiteral)
	if !ok || integer.Value != 10 {
		t.Fatal("expected IntegerLiteral(10)")
	}

	// 乘法右侧为整数 2
	right, ok := multiply.Right.(*ast.IntegerLiteral)
	if !ok || right.Value != 2 {
		t.Fatal("expected IntegerLiteral(2)")
	}
}

// 测试标识符及基础字面量的解析
func TestAtomicExpressions(t *testing.T) {
	input := `age; hai; iie; mu; "Hello\nMio";`

	l, err := lexer.NewChecked(input, token.V01)
	if err != nil {
		t.Fatal(err)
	}

	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 5 {
		t.Fatalf(
			"expected 5 statements, got %d",
			len(program.Statements),
		)
	}

	// 提取表达式节点
	exprs := make([]ast.Expression, 0, 5)

	for i, statement := range program.Statements {
		stmt, ok := statement.(*ast.ExpressionStatement)
		if !ok {
			t.Fatalf(
				"statement[%d]: expected ExpressionStatement",
				i,
			)
		}

		exprs = append(exprs, stmt.Expr)
	}

	// 标识符
	ident, ok := exprs[0].(*ast.Identifier)
	if !ok || ident.Name != "age" {
		t.Fatal("expected Identifier(age)")
	}

	// hai 布尔真值
	trueLit, ok := exprs[1].(*ast.BooleanLiteral)
	if !ok || !trueLit.Value {
		t.Fatal("expected BooleanLiteral(true)")
	}

	// iie 布尔假值
	falseLit, ok := exprs[2].(*ast.BooleanLiteral)
	if !ok || falseLit.Value {
		t.Fatal("expected BooleanLiteral(false)")
	}

	// mu 空值
	if _, ok := exprs[3].(*ast.NilLiteral); !ok {
		t.Fatal("expected NilLiteral")
	}

	// 字符串转义解码
	str, ok := exprs[4].(*ast.StringLiteral)
	if !ok || str.Value != "Hello\nMio" {
		t.Fatal("expected decoded StringLiteral")
	}
}

// 测试比较与逻辑运算符的优先级
func TestLogicalPrecedence(t *testing.T) {
	input := "10 >= 5 && hai || iie;"

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatal("expected one statement")
	}

	stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatal("expected ExpressionStatement")
	}

	// 逻辑或为根节点
	root, ok := stmt.Expr.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected InfixExpression")
	}

	if root.Operator != token.Or {
		t.Fatalf("expected OR, got %s", root.Operator)
	}

	// 逻辑与位于左子树
	andExpr, ok := root.Left.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected AND expression")
	}

	if andExpr.Operator != token.And {
		t.Fatalf("expected AND, got %s", andExpr.Operator)
	}

	// 比较表达式位于逻辑与的左子树
	compare, ok := andExpr.Left.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected comparison expression")
	}

	if compare.Operator != token.GreaterEqual {
		t.Fatalf(
			"expected GREATER_EQUAL, got %s",
			compare.Operator,
		)
	}

	// 逻辑或右侧为 iie
	right, ok := root.Right.(*ast.BooleanLiteral)
	if !ok || right.Value {
		t.Fatal("expected BooleanLiteral(false)")
	}
}

// 测试 kaku 变量声明及初始化表达式
func TestLetStatement(t *testing.T) {
	l := lexer.New("kaku age = 18;", token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf(
			"expected 1 statement, got %d",
			len(program.Statements),
		)
	}

	stmt, ok := program.Statements[0].(*ast.LetStatement)
	if !ok {
		t.Fatalf(
			"expected LetStatement, got %T",
			program.Statements[0],
		)
	}

	// 验证变量名称
	if stmt.Name.Name != "age" {
		t.Fatalf(
			"expected age, got %s",
			stmt.Name.Name,
		)
	}

	// 验证初始化整数
	value, ok := stmt.Value.(*ast.IntegerLiteral)
	if !ok {
		t.Fatal("expected IntegerLiteral")
	}

	if value.Value != 18 {
		t.Fatalf(
			"expected 18, got %d",
			value.Value,
		)
	}
}

// 测试非法 kaku 声明的语法错误
func TestInvalidLetStatement(t *testing.T) {
	inputs := []string{
		"kaku = 18;",
		"kaku age 18;",
		"kaku age = 18",
	}

	for _, input := range inputs {
		l := lexer.New(input, token.V01)
		p := New(l)

		_, err := p.ParseProgram()

		if err == nil {
			t.Errorf(
				"expected error for %q",
				input,
			)
		}
	}
}

// 测试已有变量赋值及右侧表达式
func TestAssignStatement(t *testing.T) {
	input := "kaku score = 10; score = score + 5;"

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf(
			"expected 2 statements, got %d",
			len(program.Statements),
		)
	}

	// 第二条语句为变量赋值
	assign, ok := program.Statements[1].(*ast.AssignStatement)
	if !ok {
		t.Fatalf(
			"expected AssignStatement, got %T",
			program.Statements[1],
		)
	}

	if assign.Name.Name != "score" {
		t.Fatalf(
			"expected score, got %s",
			assign.Name.Name,
		)
	}

	// 赋值右侧为加法表达式
	value, ok := assign.Value.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected InfixExpression")
	}

	if value.Operator != token.Plus {
		t.Fatalf(
			"expected PLUS, got %s",
			value.Operator,
		)
	}

	// 加法左侧引用 score
	left, ok := value.Left.(*ast.Identifier)
	if !ok || left.Name != "score" {
		t.Fatal("expected Identifier(score)")
	}

	// 加法右侧为整数 5
	right, ok := value.Right.(*ast.IntegerLiteral)
	if !ok || right.Value != 5 {
		t.Fatal("expected IntegerLiteral(5)")
	}
}

// 测试数组元素赋值语句
func TestIndexAssignStatement(t *testing.T) {
	p := New(lexer.New("values[index + 1] = value;", token.V01))
	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	assign, ok := program.Statements[0].(*ast.IndexAssignStatement)
	if !ok {
		t.Fatalf("expected IndexAssignStatement, got %T", program.Statements[0])
	}
	target, ok := assign.Target.Left.(*ast.Identifier)
	if !ok || target.Name != "values" {
		t.Fatalf("target=%T", assign.Target.Left)
	}
	if _, ok := assign.Target.Index.(*ast.InfixExpression); !ok {
		t.Fatalf("index=%T", assign.Target.Index)
	}
	value, ok := assign.Value.(*ast.Identifier)
	if !ok || value.Name != "value" {
		t.Fatalf("value=%T", assign.Value)
	}
}

// 测试相等比较与赋值语句的区分
func TestEqualityIsNotAssignment(t *testing.T) {
	l := lexer.New("score == 10;", token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatal("expected ExpressionStatement")
	}

	expr, ok := stmt.Expr.(*ast.InfixExpression)
	if !ok || expr.Operator != token.Equal {
		t.Fatal("expected equality expression")
	}
}

// 测试代码块内的多种语句解析
func TestParseBlockStatement(t *testing.T) {
	input := "{ kaku score = 10; score = score + 5; score; }"

	l := lexer.New(input, token.V01)
	p := New(l)

	block, err := p.parseBlockStatement()
	if err != nil {
		t.Fatal(err)
	}

	if len(block.Statements) != 3 {
		t.Fatalf(
			"expected 3 statements, got %d",
			len(block.Statements),
		)
	}

	// 变量声明
	if _, ok := block.Statements[0].(*ast.LetStatement); !ok {
		t.Fatal("expected LetStatement")
	}

	// 变量赋值
	if _, ok := block.Statements[1].(*ast.AssignStatement); !ok {
		t.Fatal("expected AssignStatement")
	}

	// 表达式语句
	if _, ok := block.Statements[2].(*ast.ExpressionStatement); !ok {
		t.Fatal("expected ExpressionStatement")
	}

	// 验证代码块包含左右大括号
	span := block.Span()

	if span.Start.Offset != 0 || span.End.Offset != len(input) {
		t.Fatalf("incorrect block span: %+v", span)
	}

	// 解析结束后停在右大括号
	if p.curToken.Type != token.RBrace {
		t.Fatalf(
			"expected RBrace, got %s",
			p.curToken.Type,
		)
	}
}

// 测试 moshi / sore 条件判断语句
func TestIfStatement(t *testing.T) {
	input := `
moshi (age >= 18) {
    kaku status = "adult";
} sore {
    kaku status = "minor";
}
`

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf(
			"expected 1 statement, got %d",
			len(program.Statements),
		)
	}

	// 构造条件判断节点
	stmt, ok := program.Statements[0].(*ast.IfStatement)
	if !ok {
		t.Fatalf(
			"expected IfStatement, got %T",
			program.Statements[0],
		)
	}

	// 条件表达式为 >=
	condition, ok := stmt.Condition.(*ast.InfixExpression)
	if !ok {
		t.Fatal("expected InfixExpression condition")
	}

	if condition.Operator != token.GreaterEqual {
		t.Fatal("expected >= condition")
	}

	// 验证条件的左右操作数
	left, ok := condition.Left.(*ast.Identifier)
	if !ok || left.Name != "age" {
		t.Fatal("expected Identifier(age)")
	}

	right, ok := condition.Right.(*ast.IntegerLiteral)
	if !ok || right.Value != 18 {
		t.Fatal("expected IntegerLiteral(18)")
	}

	// moshi 成立分支
	if len(stmt.Consequence.Statements) != 1 {
		t.Fatal("expected 1 consequence statement")
	}

	if _, ok := stmt.Consequence.Statements[0].(*ast.LetStatement); !ok {
		t.Fatal("expected LetStatement in consequence")
	}

	// sore 备选分支
	if stmt.Alternative == nil {
		t.Fatal("expected sore block")
	}

	if len(stmt.Alternative.Statements) != 1 {
		t.Fatal("expected 1 alternative statement")
	}

	if _, ok := stmt.Alternative.Statements[0].(*ast.LetStatement); !ok {
		t.Fatal("expected LetStatement in alternative")
	}
}

// 测试省略 sore 后的语句边界
func TestIfWithoutElse(t *testing.T) {
	input := `
moshi (hai) {
    kaku value = 100;
}
kaku result = 200;
`

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf(
			"expected 2 statements, got %d",
			len(program.Statements),
		)
	}

	stmt, ok := program.Statements[0].(*ast.IfStatement)
	if !ok {
		t.Fatal("expected IfStatement")
	}

	// 没有 sore 时备选分支为空
	if stmt.Alternative != nil {
		t.Fatal("expected no sore block")
	}

	// moshi 后的变量声明必须独立解析
	if _, ok := program.Statements[1].(*ast.LetStatement); !ok {
		t.Fatal("expected LetStatement after moshi")
	}
}

// 测试嵌套 moshi / sore 的 AST 结构
func TestNestedIfStatement(t *testing.T) {
	input := `
moshi (hai) {
    moshi (iie) {
        kaku value = 10;
    } sore {
        kaku value = 20;
    }
}
`

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	// 外层条件判断
	outer, ok := program.Statements[0].(*ast.IfStatement)
	if !ok {
		t.Fatal("expected outer IfStatement")
	}

	if len(outer.Consequence.Statements) != 1 {
		t.Fatal("expected one nested statement")
	}

	// 内层条件判断
	inner, ok := outer.Consequence.Statements[0].(*ast.IfStatement)
	if !ok {
		t.Fatal("expected nested IfStatement")
	}

	// 内层 sore 分支
	if inner.Alternative == nil {
		t.Fatal("expected nested sore block")
	}

	if len(inner.Alternative.Statements) != 1 {
		t.Fatal("expected one statement in nested sore block")
	}
}

// 测试函数调用及复杂实参表达式
func TestCallExpression(t *testing.T) {
	input := "add(1 + 2, 3 * 4);"

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf(
			"expected 1 statement, got %d",
			len(program.Statements),
		)
	}

	stmt, ok := program.Statements[0].(*ast.ExpressionStatement)
	if !ok {
		t.Fatal("expected ExpressionStatement")
	}

	call, ok := stmt.Expr.(*ast.CallExpression)
	if !ok {
		t.Fatalf(
			"expected CallExpression, got %T",
			stmt.Expr,
		)
	}

	// 函数名称
	function, ok := call.Function.(*ast.Identifier)
	if !ok || function.Name != "add" {
		t.Fatal("expected Identifier(add)")
	}

	// 实参数量
	if len(call.Arguments) != 2 {
		t.Fatalf(
			"expected 2 arguments, got %d",
			len(call.Arguments),
		)
	}

	// 第一个实参为加法表达式
	first, ok := call.Arguments[0].(*ast.InfixExpression)
	if !ok || first.Operator != token.Plus {
		t.Fatal("expected PLUS expression")
	}

	// 第二个实参为乘法表达式
	second, ok := call.Arguments[1].(*ast.InfixExpression)
	if !ok || second.Operator != token.Asterisk {
		t.Fatal("expected MULTIPLY expression")
	}
}

// 测试 kaesu 返回语句及函数调用返回值
func TestReturnStatement(t *testing.T) {
	input := "kaesu add(10, 20);"

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf(
			"expected 1 statement, got %d",
			len(program.Statements),
		)
	}

	stmt, ok := program.Statements[0].(*ast.ReturnStatement)
	if !ok {
		t.Fatalf(
			"expected ReturnStatement, got %T",
			program.Statements[0],
		)
	}

	// 返回值为函数调用
	call, ok := stmt.Value.(*ast.CallExpression)
	if !ok {
		t.Fatal("expected CallExpression as return value")
	}

	function, ok := call.Function.(*ast.Identifier)
	if !ok || function.Name != "add" {
		t.Fatal("expected Identifier(add)")
	}

	if len(call.Arguments) != 2 {
		t.Fatalf(
			"expected 2 arguments, got %d",
			len(call.Arguments),
		)
	}
}

// 测试非法返回语句的语法检查
func TestInvalidReturnStatement(t *testing.T) {
	inputs := []string{
		"kaesu 123",
		"kaesu 1 +;",
		"kaesu",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			l := lexer.New(input, token.V01)
			p := New(l)

			_, err := p.ParseProgram()
			if err == nil {
				t.Fatalf(
					"expected error for %q",
					input,
				)
			}
		})
	}
}

// 测试 waza 函数定义与函数调用组合
func TestFunctionStatement(t *testing.T) {
	input := `
waza add(a, b) {
    kaesu a + b;
}

kaku result = add(10, 20);
`

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 2 {
		t.Fatalf(
			"expected 2 statements, got %d",
			len(program.Statements),
		)
	}

	// 函数定义节点
	fn, ok := program.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf(
			"expected FunctionStatement, got %T",
			program.Statements[0],
		)
	}

	if fn.Name.Name != "add" {
		t.Fatalf(
			"expected function add, got %s",
			fn.Name.Name,
		)
	}

	// 函数形参
	if len(fn.Parameters) != 2 {
		t.Fatalf(
			"expected 2 parameters, got %d",
			len(fn.Parameters),
		)
	}

	if fn.Parameters[0].Name != "a" ||
		fn.Parameters[1].Name != "b" {
		t.Fatal("expected parameters a and b")
	}

	// 函数体中的返回语句
	if len(fn.Body.Statements) != 1 {
		t.Fatal("expected one body statement")
	}

	ret, ok := fn.Body.Statements[0].(*ast.ReturnStatement)
	if !ok {
		t.Fatal("expected ReturnStatement")
	}

	expr, ok := ret.Value.(*ast.InfixExpression)
	if !ok || expr.Operator != token.Plus {
		t.Fatal("expected addition return expression")
	}

	// 函数定义之后的变量声明
	let, ok := program.Statements[1].(*ast.LetStatement)
	if !ok {
		t.Fatal("expected LetStatement after function")
	}

	// 变量初始值为函数调用
	call, ok := let.Value.(*ast.CallExpression)
	if !ok {
		t.Fatal("expected CallExpression")
	}

	if len(call.Arguments) != 2 {
		t.Fatal("expected two call arguments")
	}
}

// 测试非法 waza 形参列表及函数体
func TestInvalidFunctionParameters(t *testing.T) {
	inputs := []string{
		"waza (a) { kaesu a; }",
		"waza f(a b) { kaesu a; }",
		"waza f(a,) { kaesu a; }",
		"waza f(a) kaesu a;",
		"waza f(a) { kaesu a;",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			l := lexer.New(input, token.V01)
			p := New(l)

			_, err := p.ParseProgram()
			if err == nil {
				t.Fatalf(
					"expected error for %q",
					input,
				)
			}
		})
	}
}

// 测试完整 Mio 程序的 AST 集成解析
func TestProgramIntegration(t *testing.T) {
	input := `waza max(a, b) {
    moshi (a > b) {
        kaesu a;
    } sore {
        kaesu b;
    }
}
kaku result = max(10, max(20, 30));
chant(result);`

	l := lexer.New(input, token.V01)
	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	// 验证顶层语句数量
	if len(program.Statements) != 3 {
		t.Fatalf(
			"expected 3 statements, got %d",
			len(program.Statements),
		)
	}

	// 第一条：waza 函数定义
	fn, ok := program.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatal("expected FunctionStatement")
	}

	if fn.Name.Name != "max" {
		t.Fatal("expected function max")
	}

	// 函数体中的 moshi 判断
	if len(fn.Body.Statements) != 1 {
		t.Fatal("expected one statement in function body")
	}

	ifStmt, ok := fn.Body.Statements[0].(*ast.IfStatement)
	if !ok {
		t.Fatal("expected IfStatement")
	}

	if ifStmt.Alternative == nil {
		t.Fatal("expected sore block")
	}

	// 第二条：kaku 变量声明
	let, ok := program.Statements[1].(*ast.LetStatement)
	if !ok {
		t.Fatal("expected LetStatement")
	}

	// 初始值为外层函数调用
	outerCall, ok := let.Value.(*ast.CallExpression)
	if !ok {
		t.Fatal("expected outer CallExpression")
	}

	if len(outerCall.Arguments) != 2 {
		t.Fatal("expected 2 arguments in outer call")
	}

	// 第二个实参为嵌套函数调用
	innerCall, ok := outerCall.Arguments[1].(*ast.CallExpression)
	if !ok {
		t.Fatal("expected nested CallExpression")
	}

	if len(innerCall.Arguments) != 2 {
		t.Fatal("expected 2 arguments in inner call")
	}

	// 第三条：chant 表达式语句
	exprStmt, ok := program.Statements[2].(*ast.ExpressionStatement)
	if !ok {
		t.Fatal("expected ExpressionStatement")
	}

	if _, ok := exprStmt.Expr.(*ast.CallExpression); !ok {
		t.Fatal("expected chant CallExpression")
	}

	// 验证 Program 覆盖整个源码范围
	span := program.Span()

	if span.Start.Offset != 0 ||
		span.End.Offset != len(input) {
		t.Fatalf("incorrect program span: %+v", span)
	}
}

// 测试带类型标注的函数解析
func TestParseTypedFunction(t *testing.T) {
	input := `waza add(a: int64, b: int64) -> int64 {
        kaesu a + b;
    }`

	l, err := lexer.NewChecked(input, token.V01)
	if err != nil {
		t.Fatal(err)
	}

	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	if len(program.Statements) != 1 {
		t.Fatalf(
			"got %d statements, want 1",
			len(program.Statements),
		)
	}

	fn, ok := program.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf(
			"got %T, want FunctionStatement",
			program.Statements[0],
		)
	}

	if fn.Name.Name != "add" {
		t.Fatalf("got function %q, want add", fn.Name.Name)
	}

	if len(fn.Parameters) != 2 {
		t.Fatalf(
			"got %d parameters, want 2",
			len(fn.Parameters),
		)
	}

	// 检查形参名称与类型
	names := []string{"a", "b"}

	for i, name := range names {
		param := fn.Parameters[i]

		if param.Name != name {
			t.Fatalf(
				"parameter[%d]: got %q, want %q",
				i, param.Name, name,
			)
		}

		if param.DeclaredType == nil {
			t.Fatalf(
				"parameter %q has no type",
				param.Name,
			)
		}

		if param.DeclaredType.Name != "int64" {
			t.Fatalf(
				"parameter %q: got type %q",
				param.Name,
				param.DeclaredType.Name,
			)
		}
	}

	// 检查返回类型
	if fn.ReturnType == nil {
		t.Fatal("expected return type")
	}

	if fn.ReturnType.Name != "int64" {
		t.Fatalf(
			"got return type %q, want int64",
			fn.ReturnType.Name,
		)
	}

	// 检查第一个类型标注的源码范围
	firstType := fn.Parameters[0].DeclaredType.Span()
	wantOffset := strings.Index(input, "int64")

	if firstType.Start.Offset != wantOffset ||
		firstType.End.Offset != wantOffset+len("int64") {
		t.Fatalf(
			"incorrect type span: %+v",
			firstType,
		)
	}

	// 检查函数体
	if len(fn.Body.Statements) != 1 {
		t.Fatalf(
			"got %d body statements, want 1",
			len(fn.Body.Statements),
		)
	}

	if _, ok := fn.Body.Statements[0].(*ast.ReturnStatement); !ok {
		t.Fatal("expected ReturnStatement")
	}
}

// 测试旧函数语法的解析兼容性
func TestParseUntypedFunctionCompatibility(t *testing.T) {
	input := `waza identity(value) {
        kaesu value;
    }`

	l, err := lexer.NewChecked(input, token.V01)
	if err != nil {
		t.Fatal(err)
	}

	p := New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	fn, ok := program.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf("expected FunctionStatement, got %T",
			program.Statements[0])
	}

	if len(fn.Parameters) != 1 {
		t.Fatal("expected one parameter")
	}

	if fn.Parameters[0].DeclaredType != nil {
		t.Fatal("expected no parameter type")
	}

	if fn.ReturnType != nil {
		t.Fatal("expected no return type")
	}
}

// 测试非法函数类型标注
func TestInvalidFunctionTypeAnnotations(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "缺少形参类型",
			input: `waza f(a: ) -> int64 {
                kaesu 1;
            }`,
			wantErr: "expected parameter type",
		},
		{
			name: "缺少返回类型",
			input: `waza f(a: int64) -> {
                kaesu a;
            }`,
			wantErr: "expected return type",
		},
		{
			name: "形参缺少冒号",
			input: `waza f(a int64) -> int64 {
                kaesu a;
            }`,
			wantErr: "expected ')'",
		},
		{
			name: "形参末尾多余逗号",
			input: `waza f(a: int64,) -> int64 {
                kaesu a;
            }`,
			wantErr: "expected parameter name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := lexer.NewChecked(tt.input, token.V01)
			if err != nil {
				t.Fatal(err)
			}

			p := New(l)

			_, err = p.ParseProgram()
			if err == nil {
				t.Fatal("expected parser error")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf(
					"expected error containing %q, got %q",
					tt.wantErr,
					err.Error(),
				)
			}
		})
	}
}
