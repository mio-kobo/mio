package mma

import "fmt"

// RuntimeError 保存原生模块产生的结构化错误
type RuntimeError struct {
	Code    string
	Message string
	Cause   error
}

// Error 返回原生模块错误描述
func (e *RuntimeError) Error() string {
	if e == nil {
		return "mma: runtime error"
	}
	if e.Cause != nil {
		return fmt.Sprintf("mma %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("mma %s: %s", e.Code, e.Message)
}

// Unwrap 返回原始错误
func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrorAPI 统一创建原生模块错误
type ErrorAPI struct{}

// Errors 提供无状态的错误构造接口
var Errors ErrorAPI

// New 创建结构化原生运行时错误
func (ErrorAPI) New(code, message string, cause error) error {
	if code == "" {
		code = "native_error"
	}
	return &RuntimeError{Code: code, Message: message, Cause: cause}
}

// Argument 创建参数诊断错误
func (ErrorAPI) Argument(index int, message string) error {
	return &RuntimeError{Code: "invalid_argument", Message: fmt.Sprintf("argument %d: %s", index+1, message)}
}
