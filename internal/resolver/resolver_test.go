package resolver

import (
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/token"
)

// 将 Mio 源码解析为测试用 AST
func parseProgram(t *testing.T, input string) *ast.Program {
	t.Helper()

	l, err := lexer.NewChecked(input, token.V01)
	if err != nil {
		t.Fatal(err)
	}

	p := parser.New(l)

	program, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}

	return program
}

// 测试变量声明与名称解析
func TestResolveProgram(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "已声明变量",
			input:   "kaku age = 18; age;",
			wantErr: false,
		},
		{
			name:    "未声明变量",
			input:   "score;",
			wantErr: true,
		},
		{
			name:    "重复声明",
			input:   "kaku age = 18; kaku age = 20;",
			wantErr: true,
		},
		{
			name:    "声明时引用自身",
			input:   "kaku age = age;",
			wantErr: true,
		},
		{
			name:    "引用先前声明的变量",
			input:   "kaku age = 18; kaku score = age;",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)

			r := New()
			err := r.ResolveProgram(program)

			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"ResolveProgram() error = %v, wantErr = %v",
					err,
					tt.wantErr,
				)
			}
		})
	}
}

// 测试复合表达式中的名称引用
func TestResolveCompositeExpressions(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "中缀表达式",
			input:   "kaku a = 10; kaku b = a + 2 * 3;",
			wantErr: false,
		},
		{
			name:    "中缀表达式引用未定义变量",
			input:   "kaku a = 10; kaku b = a + missing;",
			wantErr: true,
		},
		{
			name:    "一元表达式与括号",
			input:   "kaku a = 10; kaku b = -(a + 2);",
			wantErr: false,
		},
		{
			name:    "括号内部引用未定义变量",
			input:   "kaku b = -(missing + 2);",
			wantErr: true,
		},
		{
			name:    "函数调用实参",
			input:   "kaku f = mu; kaku a = 1; f(a + 2);",
			wantErr: false,
		},
		{
			name:    "函数调用实参未定义",
			input:   "kaku f = mu; f(missing);",
			wantErr: true,
		},
		{
			name:    "函数调用目标未定义",
			input:   "unknown(1);",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)

			r := New()
			err := r.ResolveProgram(program)

			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"ResolveProgram() error = %v, wantErr = %v",
					err,
					tt.wantErr,
				)
			}
		})
	}
}

func TestUndefinedIdentifierInGroupedExpression(t *testing.T) {
	input := "kaku b = -(missing + 2);"

	program := parseProgram(t, input)
	r := New()

	err := r.ResolveProgram(program)

	if err == nil {
		t.Fatal("expected undefined identifier error")
	}

	want := `undefined identifier "missing"`

	if !strings.Contains(err.Error(), want) {
		t.Fatalf(
			"expected %q, got %q",
			want,
			err.Error(),
		)
	}
}

// 测试变量赋值中的名称引用
func TestResolveAssignment(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:  "已声明变量赋值",
			input: "kaku score = 10; score = score + 5;",
		},
		{
			name:    "赋值目标未定义",
			input:   "score = 100;",
			wantErr: `undefined assignment target "score"`,
		},
		{
			name:    "赋值表达式引用未定义变量",
			input:   "kaku score = 10; score = missing + 5;",
			wantErr: `undefined identifier "missing"`,
		},
		{
			name:  "复杂表达式赋值",
			input: "kaku score = 10; score = -(score + 2);",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)

			r := New()
			err := r.ResolveProgram(program)

			// 合法程序
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			// 非法程序必须报告正确的错误原因
			if err == nil {
				t.Fatal("expected resolver error")
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

// 测试条件语句中的作用域隔离
func TestResolveIfScope(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "代码块访问外层变量",
			input: `
kaku age = 18;
moshi (age >= 18) {
    kaku score = age + 10;
    score = score + 1;
}
`,
		},
		{
			name: "局部变量不能在外层访问",
			input: `
moshi (hai) {
    kaku score = 100;
}
score;
`,
			wantErr: `undefined identifier "score"`,
		},
		{
			name: "moshi 和 sore 作用域隔离",
			input: `
moshi (hai) {
    kaku value = 10;
} sore {
    value;
}
`,
			wantErr: `undefined identifier "value"`,
		},
		{
			name: "不同分支允许独立声明",
			input: `
moshi (hai) {
    kaku value = 10;
} sore {
    kaku value = 20;
}
`,
		},
		{
			name: "条件引用未定义变量",
			input: `
moshi (missing) {
    kaku value = 10;
}
`,
			wantErr: `undefined identifier "missing"`,
		},
		{
			name: "代码块允许修改外层变量",
			input: `
kaku score = 10;
moshi (hai) {
    score = score + 5;
}
`,
		},
		{
			name: "离开代码块后恢复作用域",
			input: `
moshi (hai) {
    kaku value = 10;
}
kaku value = 20;
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			r := New()

			err := r.ResolveProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected resolver error")
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

// 测试函数定义、形参及局部作用域的名称解析
func TestResolveFunctionStatement(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "函数形参与外层变量",
			input: `
kaku base = 10;

waza add(a, b) {
    kaku result = a + b + base;
    kaesu result;
}

kaku answer = add(10, 20);
`,
		},
		{
			name: "函数形参未定义引用",
			input: `
waza add(a) {
    kaesu a + missing;
}
`,
			wantErr: `undefined identifier "missing"`,
		},
		{
			name: "函数形参不能泄漏到外层",
			input: `
waza example(a) {
    kaesu a;
}
a;
`,
			wantErr: `undefined identifier "a"`,
		},
		{
			name: "重复定义函数形参",
			input: `
waza add(a, a) {
    kaesu a;
}
`,
			wantErr: `already defined`,
		},
		{
			name: "函数递归引用",
			input: `
waza countdown(n) {
    moshi (n > 0) {
        kaesu countdown(n - 1);
    } sore {
        kaesu 0;
    }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			r := New()

			err := r.ResolveProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected resolver error")
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

// 测试内置函数的名称解析
func TestResolveBuiltinFunction(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:  "调用内置函数",
			input: `chant("Hello Mio!");`,
		},
		{
			name:  "调用数组长度函数",
			input: `len([1, 2]);`,
		},
		{
			name: "内置函数访问全局变量",
			input: `
kaku age = 18;
chant(age);
`,
		},
		{
			name: "内置函数访问局部变量",
			input: `
moshi (hai) {
    kaku value = 100;
    chant(value);
}
`,
		},
		{
			name:    "函数参数未定义",
			input:   "chant(missing);",
			wantErr: `undefined identifier "missing"`,
		},
		{
			name:    "调用不存在的函数",
			input:   "unknown(10);",
			wantErr: `undefined identifier "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)

			r := New()
			err := r.ResolveProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected resolver error")
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

// 测试用户函数遮蔽内置 len
func TestResolveLenShadowing(t *testing.T) {
	program := parseProgram(t, `
waza len(value: int64) -> int64 { kaesu value; }
len(1);
`)
	r := New()
	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	stmt := program.Statements[1].(*ast.ExpressionStatement)
	call := stmt.Expr.(*ast.CallExpression)
	name := call.Function.(*ast.Identifier)
	symbol, ok := r.BindingOf(name)
	if !ok || symbol.Kind != SymbolFunction {
		t.Fatalf("symbol=%+v ok=%v", symbol, ok)
	}
}

// 测试 kaesu 返回语句的函数上下文
func TestReturnContext(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "全局作用域返回",
			input:   "kaesu 42;",
			wantErr: "kaesu outside function",
		},
		{
			name: "条件代码块内返回",
			input: `
moshi (hai) {
    kaesu 42;
}
`,
			wantErr: "kaesu outside function",
		},
		{
			name: "函数内部返回",
			input: `
waza answer() {
    kaesu 42;
}
`,
		},
		{
			name: "函数条件分支内返回",
			input: `
waza example(a) {
    moshi (a > 0) {
        kaesu a;
    } sore {
        kaesu 0;
    }
}
`,
		},
		{
			name: "函数结束后不能返回",
			input: `
waza answer() {
    kaesu 42;
}

kaesu 10;
`,
			wantErr: "kaesu outside function",
		},
		{
			name: "返回未定义变量",
			input: `
waza example() {
    kaesu missing;
}
`,
			wantErr: `undefined identifier "missing"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			r := New()

			err := r.ResolveProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected resolver error")
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

// 测试函数预声明与相互递归
func TestFunctionPredeclaration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "先调用后定义",
			input: `
kaku result = add(10, 20);

waza add(a, b) {
    kaesu a + b;
}
`,
		},
		{
			name: "函数相互递归",
			input: `
waza first() {
    kaesu second();
}

waza second() {
    kaesu first();
}
`,
		},
		{
			name: "拒绝代码块内函数声明",
			input: `
moshi (hai) {
    inner();

    waza inner() {
        kaesu 42;
    }
}
`,
			wantErr: "nested function declaration",
		},
		{
			name: "局部函数不能在外层访问",
			input: `
moshi (hai) {
    waza inner() {
        kaesu 42;
    }
}

inner();
`,
			wantErr: `nested function declaration`,
		},
		{
			name: "变量仍需先声明",
			input: `
kaku a = b;
kaku b = 10;
`,
			wantErr: `undefined identifier "b"`,
		},
		{
			name: "重复定义函数",
			input: `
waza add() {
    kaesu 1;
}

waza add() {
    kaesu 2;
}
`,
			wantErr: "already defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			r := New()

			err := r.ResolveProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected resolver error")
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

// 测试完整 Mio 程序的名称解析
func TestResolverIntegration(t *testing.T) {
	input := `
kaku base = 10;

kaku result = calculate(5);

waza calculate(n) {
    moshi (n > 0) {
        chant("Calculating");
        kaesu n + base;
    } sore {
        kaesu fallback();
    }
}

waza fallback() {
    kaesu 0;
}

chant(result);
`

	program := parseProgram(t, input)
	r := New()

	if err := r.ResolveProgram(program); err != nil {
		t.Fatalf("unexpected resolver error: %v", err)
	}
}

// 测试代码块局部变量的可见范围
func TestResolverScopeLeak(t *testing.T) {
	input := `
kaku base = 10;

moshi (hai) {
    kaku temp = base + 1;
}

chant(temp);
`

	program := parseProgram(t, input)
	r := New()

	err := r.ResolveProgram(program)
	if err == nil {
		t.Fatal("expected undefined identifier error")
	}

	want := `undefined identifier "temp"`

	if !strings.Contains(err.Error(), want) {
		t.Fatalf(
			"expected error containing %q, got %q",
			want,
			err.Error(),
		)
	}
}

// 测试变量遮蔽与作用域声明规则
func TestVariableShadowing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "内层变量遮蔽外层变量",
			input: `
kaku value = 10;

moshi (hai) {
    kaku value = 20;
    chant(value);
}

chant(value);
`,
		},
		{
			name: "不同代码块允许同名变量",
			input: `
moshi (hai) {
    kaku value = 10;
} sore {
    kaku value = 20;
}
`,
		},
		{
			name: "同一作用域禁止重复声明",
			input: `
kaku value = 10;
kaku value = 20;
`,
			wantErr: "already defined",
		},
		{
			name: "局部作用域禁止重复声明",
			input: `
moshi (hai) {
    kaku value = 10;
    kaku value = 20;
}
`,
			wantErr: "already defined",
		},
		{
			name: "函数形参与局部变量重复",
			input: `
waza example(a) {
    kaku a = 10;
    kaesu a;
}
`,
			wantErr: "already defined",
		},
		{
			name: "局部变量不能泄漏",
			input: `
moshi (hai) {
    kaku value = 10;
}
chant(value);
`,
			wantErr: `undefined identifier "value"`,
		},
		{
			name: "初始化表达式允许引用外层同名变量",
			input: `
kaku value = 10;

moshi (hai) {
    kaku value = value + 1;
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := parseProgram(t, tt.input)
			r := New()

			err := r.ResolveProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected resolver error")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf(
					"expected %q, got %q",
					tt.wantErr,
					err.Error(),
				)
			}
		})
	}
}

// 测试变量遮蔽时的符号绑定
func TestIdentifierBinding(t *testing.T) {
	input := `
kaku value = 10;

moshi (hai) {
    kaku value = value + 1;
    chant(value);
}

chant(value);
`

	program := parseProgram(t, input)

	r := New()

	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}

	// 获取全局变量声明
	outerDecl := program.Statements[0].(*ast.LetStatement).Name

	// 获取内层代码块
	ifStmt := program.Statements[1].(*ast.IfStatement)
	block := ifStmt.Consequence

	// 获取内层变量声明
	innerLet := block.Statements[0].(*ast.LetStatement)
	innerDecl := innerLet.Name

	// 内层初始化表达式中的 value
	initExpr := innerLet.Value.(*ast.InfixExpression)
	initUse := initExpr.Left.(*ast.Identifier)

	// 内层 chant(value) 中的 value
	innerStmt := block.Statements[1].(*ast.ExpressionStatement)
	innerCall := innerStmt.Expr.(*ast.CallExpression)
	innerUse := innerCall.Arguments[0].(*ast.Identifier)

	// 外层 chant(value) 中的 value
	outerStmt := program.Statements[2].(*ast.ExpressionStatement)
	outerCall := outerStmt.Expr.(*ast.CallExpression)
	outerUse := outerCall.Arguments[0].(*ast.Identifier)

	// 检查标识符绑定的声明位置
	tests := []struct {
		name string
		use  *ast.Identifier
		decl *ast.Identifier
	}{
		{
			name: "初始化表达式引用外层变量",
			use:  initUse,
			decl: outerDecl,
		},
		{
			name: "代码块内部引用内层变量",
			use:  innerUse,
			decl: innerDecl,
		},
		{
			name: "代码块外部引用全局变量",
			use:  outerUse,
			decl: outerDecl,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, ok := r.BindingOf(tt.use)

			if !ok {
				t.Fatal("expected symbol binding")
			}

			if symbol.Kind != SymbolVariable {
				t.Fatalf(
					"expected variable symbol, got %s",
					symbol.Kind,
				)
			}

			if symbol.Decl != tt.decl.Span().Start {
				t.Fatalf(
					"incorrect declaration binding: got %+v, want %+v",
					symbol.Decl,
					tt.decl.Span().Start,
				)
			}
		})
	}
}

// 测试函数、形参与内置函数的符号绑定
func TestFunctionSymbolBinding(t *testing.T) {
	input := `
kaku result = add(10, 20);

waza add(a, b) {
    kaesu a + b;
}

chant(result);
`

	program := parseProgram(t, input)
	r := New()

	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}

	// 获取函数定义
	fn := program.Statements[1].(*ast.FunctionStatement)

	// 获取提前出现的函数调用
	let := program.Statements[0].(*ast.LetStatement)
	call := let.Value.(*ast.CallExpression)
	callName := call.Function.(*ast.Identifier)

	// 获取函数体中的形参引用
	ret := fn.Body.Statements[0].(*ast.ReturnStatement)
	add := ret.Value.(*ast.InfixExpression)

	paramA := add.Left.(*ast.Identifier)
	paramB := add.Right.(*ast.Identifier)

	// 获取内置函数调用
	stmt := program.Statements[2].(*ast.ExpressionStatement)
	builtinCall := stmt.Expr.(*ast.CallExpression)
	builtinName := builtinCall.Function.(*ast.Identifier)

	// 检查符号种类与声明位置
	tests := []struct {
		name  string
		ident *ast.Identifier
		kind  SymbolKind
		decl  *ast.Identifier
	}{
		{
			name:  "函数声明",
			ident: fn.Name,
			kind:  SymbolFunction,
			decl:  fn.Name,
		},
		{
			name:  "函数调用",
			ident: callName,
			kind:  SymbolFunction,
			decl:  fn.Name,
		},
		{
			name:  "形参 a",
			ident: paramA,
			kind:  SymbolParameter,
			decl:  fn.Parameters[0],
		},
		{
			name:  "形参 b",
			ident: paramB,
			kind:  SymbolParameter,
			decl:  fn.Parameters[1],
		},
		{
			name:  "内置函数 chant",
			ident: builtinName,
			kind:  SymbolBuiltin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbol, ok := r.BindingOf(tt.ident)
			if !ok {
				t.Fatal("expected symbol binding")
			}

			if symbol.Kind != tt.kind {
				t.Fatalf(
					"got symbol kind %s, want %s",
					symbol.Kind,
					tt.kind,
				)
			}

			if tt.decl != nil &&
				symbol.Decl != tt.decl.Span().Start {
				t.Fatalf(
					"incorrect declaration: got %+v, want %+v",
					symbol.Decl,
					tt.decl.Span().Start,
				)
			}
		})
	}
}
