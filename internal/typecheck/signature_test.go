package typecheck

import (
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
)

// 测试类型名称解析
func TestResolveTypeRef(t *testing.T) {
	tests := []struct {
		name    string
		want    Kind
		wantErr string
	}{
		{"int64", KindInt, ""},
		{"bool", KindBool, ""},
		{"string", KindString, ""},
		{"float64", KindFloat, ""},
		{"unknown", KindInvalid, "unknown type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := &ast.TypeRef{Name: tt.name}

			got, err := resolveTypeRef(ref)

			if got != tt.want {
				t.Fatalf(
					"got %s, want %s",
					got,
					tt.want,
				)
			}

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil ||
				!strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf(
					"expected %q, got %v",
					tt.wantErr,
					err,
				)
			}
		})
	}
}

// 解析源码并执行名称解析
func parseResolvedSignatureProgram(
	t *testing.T,
	input string,
) (*ast.Program, *resolver.Resolver) {
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

	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}

	return program, r
}

// 测试函数签名注册及提前调用的符号绑定
func TestRegisterFunctionSignature(t *testing.T) {
	input := `
kaku result = add(10, 20);

waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}
`

	program, r := parseResolvedSignatureProgram(t, input)

	fn := program.Statements[1].(*ast.FunctionStatement)

	checker := NewWithResolver(r)

	if err := checker.registerFunctionSignature(fn); err != nil {
		t.Fatal(err)
	}

	// 查询函数声明的签名
	signature, ok := checker.SignatureOf(fn.Name)
	if !ok {
		t.Fatal("expected function signature")
	}

	if len(signature.ParameterTypes) != 2 {
		t.Fatalf(
			"got %d parameters, want 2",
			len(signature.ParameterTypes),
		)
	}

	for i, kind := range signature.ParameterTypes {
		if kind != KindInt {
			t.Fatalf(
				"parameter[%d]: got %s, want int64",
				i,
				kind,
			)
		}
	}

	if signature.ReturnType != KindInt {
		t.Fatalf(
			"got return type %s, want int64",
			signature.ReturnType,
		)
	}

	// 提前出现的调用应当查到相同的签名
	let := program.Statements[0].(*ast.LetStatement)
	call := let.Value.(*ast.CallExpression)
	callName := call.Function.(*ast.Identifier)

	calledSignature, ok := checker.SignatureOf(callName)
	if !ok {
		t.Fatal("expected signature for forward call")
	}

	if calledSignature.ReturnType != KindInt ||
		len(calledSignature.ParameterTypes) != 2 {
		t.Fatalf(
			"incorrect forward call signature: %+v",
			calledSignature,
		)
	}
}

// 测试不完整及未知类型的函数签名
func TestInvalidFunctionSignatures(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "形参缺少类型",
			input: `
waza f(a) -> int64 {
    kaesu a;
}
`,
			wantErr: "requires a type annotation",
		},
		{
			name: "缺少返回类型",
			input: `
waza f(a: int64) {
    kaesu a;
}
`,
			wantErr: "requires a return type",
		},
		{
			name: "未知形参类型",
			input: `
waza f(a: decimal) -> int64 {
	kaesu 10;
}
`,
			wantErr: `unknown type "decimal"`,
		},
		{
			name: "未知返回类型",
			input: `
waza f(a: int64) -> result {
    kaesu a;
}
`,
			wantErr: `unknown type "result"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, r := parseResolvedSignatureProgram(
				t,
				tt.input,
			)

			fn := program.Statements[0].(*ast.FunctionStatement)
			checker := NewWithResolver(r)

			err := checker.registerFunctionSignature(fn)

			if err == nil {
				t.Fatal("expected signature error")
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

// 测试函数形参及返回类型检查
func TestCheckFunctionBodies(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name: "整数形参与整数返回",
			input: `
waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}
`,
		},
		{
			name: "布尔返回类型",
			input: `
waza greater(a: int64, b: int64) -> bool {
    kaesu a > b;
}
`,
		},
		{
			name: "字符串形参与返回",
			input: `
waza echo(message: string) -> string {
    kaesu message;
}
`,
		},
		{
			name: "两个分支均返回",
			input: `
waza choose(flag: bool) -> int64 {
    moshi (flag) {
        kaesu 1;
    } sore {
        kaesu 2;
    }
}
`,
		},
		{
			name: "返回类型不匹配",
			input: `
waza wrong(a: int64) -> bool {
    kaesu a;
}
`,
			wantErr: "return type mismatch",
		},
		{
			name: "形参参与非法算术运算",
			input: `
waza wrong(a: int64, b: bool) -> int64 {
    kaesu a + b;
}
`,
			wantErr: "requires int64 operands",
		},
		{
			name: "函数缺少返回语句",
			input: `
waza missing(a: int64) -> int64 {
    kaku value = a;
}
`,
			wantErr: "may exit without returning",
		},
		{
			name: "只有一个分支返回",
			input: `
waza maybe(flag: bool) -> int64 {
    moshi (flag) {
        kaesu 1;
    }
}
`,
			wantErr: "may exit without returning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, r := parseResolvedSignatureProgram(
				t,
				tt.input,
			)

			checker := NewWithResolver(r)
			err := checker.CheckProgram(program)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected type error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected type error")
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
