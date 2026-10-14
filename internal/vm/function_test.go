package vm

import (
	"bytes"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
	"Mio/internal/value"
)

// 测试用户函数参数、局部变量与返回值
func TestVMUserFunctionLocalsAndReturn(t *testing.T) {
	output := runFunctionSource(t, `
waza add(a: int64, b: int64) -> int64 {
    kaku result = a + b;
    kaesu result;
}
chant(add(10, 20));
`)
	if output != "30\n" {
		t.Fatalf("output = %q, want %q", output, "30\n")
	}
}

// 测试递归和相互递归共享 Value Stack
func TestVMRecursiveFunctions(t *testing.T) {
	output := runFunctionSource(t, `
waza factorial(n: int64) -> int64 {
    moshi (n <= 1) {
        kaesu 1;
    } sore {
		kaku next = n - 1;
		kaesu n * factorial(next);
    }
}
waza even(n: int64) -> bool {
    moshi (n == 0) { kaesu hai; } sore { kaesu odd(n - 1); }
}
waza odd(n: int64) -> bool {
    moshi (n == 0) { kaesu iie; } sore { kaesu even(n - 1); }
}
chant(factorial(6));
chant(even(10));
chant(odd(9));
`)
	if output != "720\nhai\nhai\n" {
		t.Fatalf("output = %q", output)
	}
}

// 测试用户函数读写全局变量
func TestVMFunctionGlobalAccess(t *testing.T) {
	output := runFunctionSource(t, `
kaku total = 10;
waza addToTotal(value: int64) -> int64 {
    total = total + value;
    kaesu total;
}
chant(addToTotal(5));
chant(total);
`)
	if output != "15\n15\n" {
		t.Fatalf("output = %q", output)
	}
}

// 测试 unit 返回不创建运行时值
func TestVMUnitFunctionDoesNotProduceValue(t *testing.T) {
	bytecode := compileFunctionSource(t, `waza noop() -> unit { kaesu; } noop();`)
	machine := NewWithOutput(bytecode, &bytes.Buffer{})
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if _, ok := machine.LastPoppedValue(); ok {
		t.Fatal("unit function produced a popped runtime value")
	}
	if len(machine.stack) != 0 {
		t.Fatalf("stack length = %d, want 0", len(machine.stack))
	}
}

// 测试递归调用深度限制
func TestVMCallDepthLimit(t *testing.T) {
	bytecode := compileFunctionSource(t, `
waza loop(n: int64) -> int64 { kaesu loop(n); }
chant(loop(1));
`)
	machine := NewWithOutput(bytecode, &bytes.Buffer{})
	err := machine.Run()
	if err == nil || !strings.Contains(err.Error(), "call depth limit") {
		t.Fatalf("Run() error = %v", err)
	}
}

// 测试 Value Stack 空间限制
func TestVMValueStackLimit(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(1)},
		Instructions: code.Instructions{byte(code.OpCall), 0, 0},
		Functions: []compiler.CompiledFunction{{
			ID:           0,
			Name:         "overflow",
			LocalCount:   MaxValueStack,
			Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpReturn)},
		}},
	}
	err := NewWithOutput(bytecode, &bytes.Buffer{}).Run()
	if err == nil || !strings.Contains(err.Error(), "value stack limit") {
		t.Fatalf("Run() error = %v", err)
	}
}

func runFunctionSource(t *testing.T, source string) string {
	t.Helper()
	bytecode := compileFunctionSource(t, source)
	var output bytes.Buffer
	machine := NewWithOutput(bytecode, &output)
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func compileFunctionSource(t *testing.T, source string) *compiler.Bytecode {
	t.Helper()
	l, err := lexer.NewChecked(source, token.V04)
	if err != nil {
		t.Fatal(err)
	}
	program, err := parser.New(l).ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New()
	if err := r.ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(program); err != nil {
		t.Fatal(err)
	}
	c := compiler.NewWithResolver(r)
	if err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	return c.Bytecode()
}
