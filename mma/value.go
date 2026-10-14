package mma

import "fmt"

// Value 保存一次原生调用边界上的 Mio 值
type Value struct {
	typeOf   Type
	integer  int64
	floating float64
	boolean  bool
	text     string
}

// ValueAPI 提供基本 Mio 值的安全构造和读取
type ValueAPI struct{}

// Values 提供无状态的值转换接口
var Values ValueAPI

// Type 返回值的 Mio 类型
func (v Value) Type() Type { return v.typeOf }

// Bool 创建 bool 值
func (ValueAPI) Bool(value bool) Value { return Value{typeOf: TypeBool, boolean: value} }

// Int8 创建 int8 值
func (ValueAPI) Int8(value int8) Value { return Value{typeOf: TypeInt8, integer: int64(value)} }

// Int16 创建 int16 值
func (ValueAPI) Int16(value int16) Value { return Value{typeOf: TypeInt16, integer: int64(value)} }

// Int32 创建 int32 值
func (ValueAPI) Int32(value int32) Value { return Value{typeOf: TypeInt32, integer: int64(value)} }

// Int64 创建 int64 值
func (ValueAPI) Int64(value int64) Value { return Value{typeOf: TypeInt64, integer: value} }

// Uint8 创建 uint8 值
func (ValueAPI) Uint8(value uint8) Value { return Value{typeOf: TypeUint8, integer: int64(value)} }

// Uint16 创建 uint16 值
func (ValueAPI) Uint16(value uint16) Value { return Value{typeOf: TypeUint16, integer: int64(value)} }

// Uint32 创建 uint32 值
func (ValueAPI) Uint32(value uint32) Value { return Value{typeOf: TypeUint32, integer: int64(value)} }

// Float32 创建 float32 值
func (ValueAPI) Float32(value float32) Value {
	return Value{typeOf: TypeFloat32, floating: float64(value)}
}

// Float64 创建 float64 值
func (ValueAPI) Float64(value float64) Value { return Value{typeOf: TypeFloat64, floating: value} }

// String 创建 string 值
func (ValueAPI) String(value string) Value { return Value{typeOf: TypeString, text: value} }

// AsBool 读取 bool 值
func (ValueAPI) AsBool(value Value) (bool, error) {
	if value.typeOf != TypeBool {
		return false, valueTypeError(TypeBool, value.typeOf)
	}
	return value.boolean, nil
}

// AsInt64 读取 int64 值
func (ValueAPI) AsInt64(value Value) (int64, error) {
	if value.typeOf != TypeInt64 {
		return 0, valueTypeError(TypeInt64, value.typeOf)
	}
	return value.integer, nil
}

// AsInt8 读取 int8 值
func (ValueAPI) AsInt8(value Value) (int8, error) {
	if value.typeOf != TypeInt8 {
		return 0, valueTypeError(TypeInt8, value.typeOf)
	}
	return int8(value.integer), nil
}

// AsInt16 读取 int16 值
func (ValueAPI) AsInt16(value Value) (int16, error) {
	if value.typeOf != TypeInt16 {
		return 0, valueTypeError(TypeInt16, value.typeOf)
	}
	return int16(value.integer), nil
}

// AsInt32 读取 int32 值
func (ValueAPI) AsInt32(value Value) (int32, error) {
	if value.typeOf != TypeInt32 {
		return 0, valueTypeError(TypeInt32, value.typeOf)
	}
	return int32(value.integer), nil
}

// AsUint8 读取 uint8 值
func (ValueAPI) AsUint8(value Value) (uint8, error) {
	if value.typeOf != TypeUint8 {
		return 0, valueTypeError(TypeUint8, value.typeOf)
	}
	return uint8(value.integer), nil
}

// AsUint16 读取 uint16 值
func (ValueAPI) AsUint16(value Value) (uint16, error) {
	if value.typeOf != TypeUint16 {
		return 0, valueTypeError(TypeUint16, value.typeOf)
	}
	return uint16(value.integer), nil
}

// AsUint32 读取 uint32 值
func (ValueAPI) AsUint32(value Value) (uint32, error) {
	if value.typeOf != TypeUint32 {
		return 0, valueTypeError(TypeUint32, value.typeOf)
	}
	return uint32(value.integer), nil
}

// AsFloat64 读取 float64 值
func (ValueAPI) AsFloat64(value Value) (float64, error) {
	if value.typeOf != TypeFloat64 {
		return 0, valueTypeError(TypeFloat64, value.typeOf)
	}
	return value.floating, nil
}

// AsString 读取 string 值
func (ValueAPI) AsString(value Value) (string, error) {
	if value.typeOf != TypeString {
		return "", valueTypeError(TypeString, value.typeOf)
	}
	return value.text, nil
}

// IntegerBits 读取整数值的位模式
func (ValueAPI) IntegerBits(value Value) (uint64, error) {
	switch value.typeOf {
	case TypeInt8, TypeInt16, TypeInt32, TypeInt64, TypeUint8, TypeUint16, TypeUint32:
		return uint64(value.integer), nil
	default:
		return 0, fmt.Errorf("mma: expected integer, got %s", value.typeOf)
	}
}

// AsFloat32 读取 float32 值
func (ValueAPI) AsFloat32(value Value) (float32, error) {
	if value.typeOf != TypeFloat32 {
		return 0, valueTypeError(TypeFloat32, value.typeOf)
	}
	return float32(value.floating), nil
}

func valueTypeError(expected, actual Type) error {
	return fmt.Errorf("mma: expected %s, got %s", expected, actual)
}
