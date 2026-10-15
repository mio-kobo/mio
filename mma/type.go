// Package mma 定义 Mio Module API。
package mma

import (
	"fmt"
	"strings"
)

// Type 表示原生函数边界支持的 Mio 类型
type Type string

const (
	TypeUnit     Type = "unit"
	TypeBool     Type = "bool"
	TypeInt8     Type = "int8"
	TypeInt16    Type = "int16"
	TypeInt32    Type = "int32"
	TypeInt64    Type = "int64"
	TypeUint8    Type = "uint8"
	TypeUint16   Type = "uint16"
	TypeUint32   Type = "uint32"
	TypeFloat32  Type = "float32"
	TypeFloat64  Type = "float64"
	TypeString   Type = "string"
	TypeBytes    Type = "[]uint8"
	TypeResource Type = "resource"
)

// FunctionSignature 描述原生函数的静态类型
type FunctionSignature struct {
	Parameters []Type
	Return     Type
}

// Validate 检查函数签名是否可用于 MMA
func (s FunctionSignature) Validate() error {
	for index, parameter := range s.Parameters {
		if parameter == TypeUnit || !parameter.valid() {
			return fmt.Errorf("mma: invalid parameter %d type %q", index+1, parameter)
		}
	}
	if !s.Return.valid() {
		return fmt.Errorf("mma: invalid return type %q", s.Return)
	}
	return nil
}

func (t Type) valid() bool {
	switch t {
	case TypeUnit, TypeBool, TypeInt8, TypeInt16, TypeInt32, TypeInt64,
		TypeUint8, TypeUint16, TypeUint32, TypeFloat32, TypeFloat64, TypeString, TypeBytes, TypeResource:
		return true
	default:
		return false
	}
}

func (s FunctionSignature) canonical() string {
	parameters := make([]string, len(s.Parameters))
	for index, parameter := range s.Parameters {
		parameters[index] = string(parameter)
	}
	return strings.Join(parameters, ",") + "->" + string(s.Return)
}
