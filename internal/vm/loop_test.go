package vm

import (
	"bytes"
	"strings"
	"testing"

	"Mio/internal/ast"
	"Mio/internal/compiler"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

// compileLoopSource 编译包含 v0.2 关键字的源码
func compileLoopSource(t *testing.T, source string) *compiler.Bytecode {
	t.Helper()
	program, r := parseAndCheckLoopSource(t, source)
	c := compiler.NewWithResolver(r)
	if err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	return c.Bytecode()
}

// parseAndCheckLoopSource 解析并检查包含 v0.2 关键字的源码
func parseAndCheckLoopSource(t *testing.T, source string) (*ast.Program, *resolver.Resolver) {
	t.Helper()
	l, err := lexer.NewChecked(source, token.V02)
	if err != nil {
		t.Fatal(err)
	}
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(program); err != nil {
		t.Fatal(err)
	}
	return program, r
}

// runLoopSource 执行循环源码并返回输出
func runLoopSource(t *testing.T, source string) string {
	t.Helper()
	var output bytes.Buffer
	machine := NewWithOutput(compileLoopSource(t, source), &output)
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if len(machine.stack) != 0 {
		t.Fatalf("value stack length = %d, want 0", len(machine.stack))
	}
	return output.String()
}

// 测试普通循环和初始假条件
func TestVMWhileLoop(t *testing.T) {
	source := `
kaku count = 0;
aida (count < 3) {
    count = count + 1;
    chant(count);
}
aida (iie) { chant(99); }
`
	if got := runLoopSource(t, source); got != "1\n2\n3\n" {
		t.Fatalf("output = %q", got)
	}
}

// 测试 nukeru 和 tsuzuku 控制当前循环
func TestVMBreakAndContinue(t *testing.T) {
	source := `
kaku count = 0;
aida (count < 10) {
    count = count + 1;
    moshi (count == 3) { tsuzuku; }
    moshi (count == 8) { nukeru; }
    chant(count);
}
`
	if got := runLoopSource(t, source); got != "1\n2\n4\n5\n6\n7\n" {
		t.Fatalf("output = %q", got)
	}
}

// 测试嵌套循环仅退出最内层
func TestVMNestedWhileLoop(t *testing.T) {
	source := `
kaku outer = 0;
aida (outer < 3) {
    kaku inner = 0;
    aida (inner < 3) {
        inner = inner + 1;
        moshi (inner == 2) { nukeru; }
        chant(outer * 10 + inner);
    }
    outer = outer + 1;
}
`
	if got := runLoopSource(t, source); got != "1\n11\n21\n" {
		t.Fatalf("output = %q", got)
	}
}

// 测试循环局部变量遮蔽外层变量
func TestVMWhileLocalShadowing(t *testing.T) {
	source := `
kaku value = 100;
kaku count = 0;
aida (count < 2) {
    kaku value = count;
    chant(value);
    count = count + 1;
}
chant(value);
`
	if got := runLoopSource(t, source); got != "0\n1\n100\n" {
		t.Fatalf("output = %q", got)
	}
}

// 测试循环调用用户函数并从循环体返回
func TestVMWhileFunctionCallAndReturn(t *testing.T) {
	source := `
waza increment(value: int64) -> int64 { kaesu value + 1; }
waza choose(value: int64) -> int64 {
    aida (value > 0) { kaesu value; }
    kaesu 0;
}
kaku count = 0;
aida (count < 2) {
    count = increment(count);
    chant(choose(count));
}
`
	if got := runLoopSource(t, source); got != "1\n2\n" {
		t.Fatalf("output = %q", got)
	}
}

// 测试循环静态错误
func TestWhileStaticErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "条件不是 bool", source: `aida (1) {}`, want: "aida condition requires bool"},
		{name: "循环外 nukeru", source: `nukeru;`, want: "nukeru outside loop"},
		{name: "循环外 tsuzuku", source: `tsuzuku;`, want: "tsuzuku outside loop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := lexer.NewChecked(tt.source, token.V02)
			if err != nil {
				t.Fatal(err)
			}
			program, err := parser.New(l).ParseProgram()
			if err != nil {
				t.Fatal(err)
			}
			r := resolver.New()
			err = r.ResolveProgram(program)
			if err == nil {
				err = typecheck.NewWithResolver(r).CheckProgram(program)
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// 测试无限循环受指令预算限制
func TestVMWhileInstructionLimit(t *testing.T) {
	machine := New(compileLoopSource(t, `aida (hai) {}`))
	err := machine.RunWithOptions(ExecutionOptions{InstructionLimit: 10})
	if err == nil || !strings.Contains(err.Error(), "instruction limit 10 exceeded") {
		t.Fatalf("RunWithOptions() error = %v", err)
	}
}
