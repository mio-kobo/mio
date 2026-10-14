package vm

import "fmt"

// RuntimeFrame 保存运行时调用帧
type RuntimeFrame struct {
	Function string `json:"function"`
	Offset   int    `json:"offset"`
}

// RuntimeError 保存 VM 错误原因与调用栈
type RuntimeError struct {
	Cause error          `json:"-"`
	Stack []RuntimeFrame `json:"stack"`
}

// Error 返回紧凑的运行时错误
func (e *RuntimeError) Error() string {
	if e == nil || e.Cause == nil {
		return "runtime error"
	}
	if len(e.Stack) == 0 {
		return e.Cause.Error()
	}
	return fmt.Sprintf("%s in %s at bytecode offset %d", e.Cause, e.Stack[0].Function, e.Stack[0].Offset)
}

// Unwrap 返回原始运行时错误
func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
