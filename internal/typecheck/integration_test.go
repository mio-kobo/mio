package typecheck

import (
	"strings"
	"testing"

	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
)

// checkMioSource 执行完整的前端语义检查
func checkMioSource(input string) error {
	// 创建词法分析器
	l, err := lexer.NewChecked(input, token.V04)
	if err != nil {
		return err
	}

	// 解析源码生成 AST
	p := parser.New(l)
	program, err := p.ParseProgram()
	if err != nil {
		return err
	}

	if err := l.Err(); err != nil {
		return err
	}

	// 解析符号声明与引用
	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		return err
	}

	// 执行静态类型检查
	checker := NewWithResolver(r)
	return checker.CheckProgram(program)
}

// 测试完整编译器前端的语义检查
func TestTypeCheckerIntegration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "函数提前调用与 unit 返回",
			input: `
kaku result = add(10, 20);

waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}

waza output(value: int64) -> unit {
    chant(value);
}

output(result);
`,
		},
		{
			name: "unit 函数提前退出",
			input: `
waza greet(name: string) -> unit {
    moshi (name == "Mio") {
        kaesu;
    }

    chant(name);
}

greet("Hello");
`,
		},
		{
			name: "不同作用域的变量遮蔽",
			input: `
kaku value = 10;

moshi (hai) {
    kaku value = "Mio";
    chant(value);
}

value = 20;
`,
		},
		{
			name: "函数调用参数类型错误",
			input: `
waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}

kaku result = add(10, "Mio");
`,
			wantErr: "argument 2",
		},
		{
			name: "函数返回类型错误",
			input: `
waza wrong() -> bool {
    kaesu 10;
}
`,
			wantErr: "return type mismatch",
		},
		{
			name: "非 unit 函数无值返回",
			input: `
waza wrong() -> int64 {
    kaesu;
}
`,
			wantErr: "return type mismatch",
		},
		{
			name: "条件表达式不是布尔值",
			input: `
moshi (123) {
    chant("Mio");
}
`,
			wantErr: "moshi condition requires bool",
		},
		{
			name: "禁止改变变量类型",
			input: `
kaku age = 18;
age = "Mio";
`,
			wantErr: "cannot assign string",
		},
		{
			name: "局部变量赋值保持声明类型",
			input: `
moshi (hai) {
    kaku local = 18;
    local = "Mio";
}
`,
			wantErr: "cannot assign string",
		},
		{
			name: "函数外禁止返回",
			input: `
kaesu;
`,
			wantErr: "kaesu outside function",
		},
		{
			name: "未定义变量",
			input: `
chant(missing);
`,
			wantErr: `undefined identifier "missing"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkMioSource(tt.input)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf(
						"unexpected error: %v",
						err,
					)
				}
				return
			}

			if err == nil {
				t.Fatal("expected semantic error")
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
