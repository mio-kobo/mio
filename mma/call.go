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
	active    bool
	resources *ResourceManager
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
	return &CallContext{arguments: arguments, signature: signature, active: true}, nil
}

// Invoke 在统一错误边界内同步调用原生函数
func Invoke(function NativeFunction, arguments []Value, signature FunctionSignature) (result Value, hasResult bool, callErr error) {
	return InvokeWithResources(function, arguments, signature, nil)
}

// InvokeWithResources 在指定 Runtime 资源表中调用原生函数
func InvokeWithResources(function NativeFunction, arguments []Value, signature FunctionSignature, resources *ResourceManager) (result Value, hasResult bool, callErr error) {
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
	context.resources = resources
	defer context.invalidate()
	if err := function(context); err != nil {
		return Value{}, false, err
	}
	return context.Result()
}

// CreateResource 创建绑定当前 Runtime 的资源
func (c *CallContext) CreateResource(typeName string, value any, close func(any) error) (Value, error) {
	if !c.active || c.resources == nil {
		return Value{}, fmt.Errorf("mma: resource manager is unavailable")
	}
	handle, err := c.resources.Create(typeName, value, close)
	if err != nil {
		return Value{}, err
	}
	return Values.Resource(handle), nil
}

// UseResource 校验并读取资源参数
func (c *CallContext) UseResource(index int, typeName string) (any, error) {
	argument, err := c.Argument(index)
	if err != nil {
		return nil, err
	}
	handle, err := Values.AsResource(argument)
	if err != nil {
		return nil, err
	}
	if c.resources == nil {
		return nil, fmt.Errorf("mma: resource manager is unavailable")
	}
	return c.resources.Use(handle, typeName)
}

// CloseResource 关闭资源参数
func (c *CallContext) CloseResource(index int, typeName string) error {
	argument, err := c.Argument(index)
	if err != nil {
		return err
	}
	handle, err := Values.AsResource(argument)
	if err != nil {
		return err
	}
	if c.resources == nil {
		return fmt.Errorf("mma: resource manager is unavailable")
	}
	return c.resources.Close(handle, typeName)
}

// ArgumentCount 返回参数数量
func (c *CallContext) ArgumentCount() int { return len(c.arguments) }

// Argument 安全读取指定参数
func (c *CallContext) Argument(index int) (Value, error) {
	if !c.active {
		return Value{}, fmt.Errorf("mma: call context is no longer active")
	}
	if index < 0 || index >= len(c.arguments) {
		return Value{}, fmt.Errorf("mma: argument index %d out of range", index)
	}
	return c.arguments[index], nil
}

// ReadBytes 复制指定 []uint8 参数范围
func (c *CallContext) ReadBytes(index int, offset, length int64) ([]byte, error) {
	argument, err := c.Argument(index)
	if err != nil {
		return nil, err
	}
	if argument.typeOf != TypeBytes {
		return nil, valueTypeError(TypeBytes, argument.typeOf)
	}
	if offset < 0 || length < 0 {
		return nil, Errors.Argument(index, "offset and length must be non-negative")
	}
	if argument.access == nil {
		if offset > int64(len(argument.bytes)) || length > int64(len(argument.bytes))-offset {
			return nil, Errors.Argument(index, "byte range is out of bounds")
		}
		return append([]byte(nil), argument.bytes[offset:offset+length]...), nil
	}
	if offset > argument.access.length || length > argument.access.length-offset {
		return nil, Errors.Argument(index, "byte range is out of bounds")
	}
	if argument.access.read == nil {
		return nil, Errors.Argument(index, "byte array is not readable")
	}
	return argument.access.read(offset, length)
}

// WriteBytes 将字节写回指定 []uint8 参数的已有范围
func (c *CallContext) WriteBytes(index int, offset int64, source []byte) error {
	argument, err := c.Argument(index)
	if err != nil {
		return err
	}
	if argument.typeOf != TypeBytes {
		return valueTypeError(TypeBytes, argument.typeOf)
	}
	if offset < 0 {
		return Errors.Argument(index, "offset must be non-negative")
	}
	if argument.access == nil || argument.access.write == nil {
		return Errors.Argument(index, "byte array is not writable")
	}
	if argument.access.length < 0 || offset > argument.access.length || int64(len(source)) > argument.access.length-offset {
		return Errors.Argument(index, "byte range is out of bounds")
	}
	return argument.access.write(offset, source)
}

// Return 设置原生调用结果
func (c *CallContext) Return(result Value) error {
	if !c.active {
		return fmt.Errorf("mma: call context is no longer active")
	}
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

func (c *CallContext) invalidate() {
	if c == nil {
		return
	}
	c.active = false
	for _, argument := range c.arguments {
		if argument.access != nil && argument.access.active != nil {
			*argument.access.active = false
		}
	}
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
