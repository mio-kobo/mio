package mma

import "fmt"

// NativeFunction 是同步原生函数入口
type NativeFunction func(*CallContext) error

// CallContext 提供一次原生调用的安全参数和返回值访问
type CallContext struct {
	arguments []Value
	signature FunctionSignature
	result    Value
	returned  bool
}

// NewCallContext 创建受签名约束的调用上下文
func NewCallContext(arguments []Value, signature FunctionSignature) (*CallContext, error) {
	context, err := newCallContext(arguments, signature)
	if err != nil {
		return nil, err
	}
	context.arguments = append([]Value(nil), context.arguments...)
	return context, nil
}

func newCallContext(arguments []Value, signature FunctionSignature) (*CallContext, error) {
	if err := signature.Validate(); err != nil {
		return nil, err
	}
	if len(arguments) != len(signature.Parameters) {
		return nil, fmt.Errorf("mma: expected %d arguments, got %d", len(signature.Parameters), len(arguments))
	}
	for index, argument := range arguments {
		if argument.Type() != signature.Parameters[index] {
			return nil, fmt.Errorf("mma: argument %d expects %s, got %s", index+1, signature.Parameters[index], argument.Type())
		}
	}
	return &CallContext{arguments: arguments, signature: signature}, nil
}

// Invoke 在统一错误边界内同步调用原生函数
func Invoke(function NativeFunction, arguments []Value, signature FunctionSignature) (result Value, hasResult bool, callErr error) {
	if function == nil {
		return Value{}, false, fmt.Errorf("mma: nil native function")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			callErr = Errors.New("native_panic", "原生函数发生 panic", fmt.Errorf("%v", recovered))
		}
	}()
	context, err := newCallContext(arguments, signature)
	if err != nil {
		return Value{}, false, err
	}
	if err := function(context); err != nil {
		return Value{}, false, err
	}
	return context.Result()
}

// ArgumentCount 返回参数数量
func (c *CallContext) ArgumentCount() int { return len(c.arguments) }

// Argument 安全读取指定参数
func (c *CallContext) Argument(index int) (Value, error) {
	if index < 0 || index >= len(c.arguments) {
		return Value{}, fmt.Errorf("mma: argument index %d out of range", index)
	}
	return c.arguments[index], nil
}

// Return 设置原生调用结果
func (c *CallContext) Return(result Value) error {
	if c.signature.Return == TypeUnit {
		return fmt.Errorf("mma: unit function cannot return a value")
	}
	if c.returned {
		return fmt.Errorf("mma: result already returned")
	}
	if result.Type() != c.signature.Return {
		return fmt.Errorf("mma: result expects %s, got %s", c.signature.Return, result.Type())
	}
	c.result = result
	c.returned = true
	return nil
}

// Result 完成调用并校验返回值
func (c *CallContext) Result() (Value, bool, error) {
	if c.signature.Return == TypeUnit {
		if c.returned {
			return Value{}, false, fmt.Errorf("mma: unit function returned a value")
		}
		return Value{}, false, nil
	}
	if !c.returned {
		return Value{}, false, fmt.Errorf("mma: native function did not return %s", c.signature.Return)
	}
	return c.result, true, nil
}
