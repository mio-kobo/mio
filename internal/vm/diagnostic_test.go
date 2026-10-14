package vm

import (
	"errors"
	"strings"
	"testing"
)

// 测试运行时错误保留函数名、偏移与调用栈
func TestRuntimeErrorCallStack(t *testing.T) {
	bytecode := compileFunctionSource(t, `
waza inner() -> int64 { kaesu 1 / 0; }
waza outer() -> int64 { kaesu inner(); }
outer();
`)
	machine := New(bytecode)
	err := machine.Run()
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("错误类型为 %T", err)
	}
	if len(runtimeErr.Stack) != 3 {
		t.Fatalf("调用栈为 %#v", runtimeErr.Stack)
	}
	if runtimeErr.Stack[0].Function != "inner" || runtimeErr.Stack[1].Function != "outer" || runtimeErr.Stack[2].Function != "<main>" {
		t.Fatalf("调用栈为 %#v", runtimeErr.Stack)
	}
	if !strings.Contains(runtimeErr.Error(), "division by zero") {
		t.Fatalf("运行时错误为 %q", runtimeErr.Error())
	}
}
