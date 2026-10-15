// Package mochi 编解码并验证 Mio 字节码文件。
//
// Mochi v1 使用确定性的大端字节序布局。
// Mochi v2 在 v1 主指令流之后追加函数表。
// Mochi v3 为常量表增加 IEEE 754 binary64 浮点值。
// Mochi v4 允许显式数值转换指令。
// Mochi v5 增加数组构造与索引指令。
// Mochi v6 增加数组长度与元素赋值指令。
// Mochi v7 增加空值检查指令。
// Mochi v8 增加结构体、枚举、Map 与函数值指令。
// Mochi v9 增加闭包捕获元数据与 Upvalue 指令。
// Mochi v10 增加 binary32 常量、转换与统一数学原语。
// Mochi v11 增加固定宽度整数常量与显式转换指令。
// Mochi v12 增加通用动态数组指令。
// Mochi v13 增加数组截断与批量追加指令。
//
//	5 字节     文件签名 "MOCHI"
//	uint16    格式版本
//	uint32    常量数量
//	重复的常量数据：
//	  uint8   类型标签（1 = int64，2 = UTF-8 字符串，v3 的 3 = float64）
//	  int64   整数值，或
//	  uint32  字符串字节数及对应的 UTF-8 数据
//	uint32    全局槽位数量
//	uint32    指令字节数
//	[]byte    指令数据
//
//	v2/v3/v4/v5/v6/v7/v8/v9：
//	uint32    函数数量
//	重复的函数数据：
//	  uint16 FunctionID
//	  uint16 名称字节数及对应的 UTF-8 数据
//	  uint32 参数数量
//	  uint32 局部槽位数量
//	  uint8  返回值标志
//	  v9: uint32 捕获数量，重复的 uint8 来源标志与 uint32 索引
//	  uint32 指令字节数及对应的指令数据
//
// 所有版本都不包含填充和校验和，也不允许文件尾数据。
package mochi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

const (
	// Version 表示最新的 Mochi 格式版本
	Version uint16 = VersionV13
	// VersionV13 表示数组批量操作格式版本
	VersionV13 uint16 = 13
	// VersionV12 表示通用动态数组格式版本
	VersionV12 uint16 = 12
	// VersionV11 表示固定宽度整数格式版本
	VersionV11 uint16 = 11
	// VersionV10 表示包含 binary32 与数学原语的旧格式版本
	VersionV10 uint16 = 10
	// VersionV1 表示兼容读取的旧格式版本
	VersionV1 uint16 = 1
	// VersionV2 表示包含函数表的旧格式版本
	VersionV2 uint16 = 2
	// VersionV3 表示包含浮点常量的旧格式版本
	VersionV3 uint16 = 3
	// VersionV4 表示包含数值转换指令的旧格式版本
	VersionV4 uint16 = 4
	// VersionV5 表示包含数组构造与索引指令的旧格式版本
	VersionV5 uint16 = 5
	// VersionV6 表示包含数组长度与元素赋值指令的旧格式版本
	VersionV6 uint16 = 6
	// VersionV7 表示包含可空类型指令的旧格式版本
	VersionV7 uint16 = 7
	// VersionV8 表示包含核心复合类型与函数值指令的旧格式版本
	VersionV8 uint16 = 8
	// VersionV9 表示包含闭包捕获的旧格式版本
	VersionV9 uint16 = 9
	// Extension 表示 Mochi 字节码文件扩展名
	Extension = ".mochi"

	// 编码和解码使用相同的资源限制
	MaxFileSize          = 64 << 20
	MaxConstants         = 1 << 16
	MaxGlobals           = 1 << 16
	MaxStringBytes       = 16 << 20
	MaxInstructionBytes  = 16 << 20
	MaxFunctions         = 1 << 16
	MaxFunctionNameBytes = 0xffff
	// MaxFunctionSlots 限制函数参数和局部槽位数量
	MaxFunctionSlots = 1 << 16

	constantTagInt     byte = 1
	constantTagString  byte = 2
	constantTagFloat   byte = 3
	constantTagFloat32 byte = 4
	constantTagInt8    byte = 5
	constantTagInt16   byte = 6
	constantTagInt32   byte = 7
	constantTagUint8   byte = 8
	constantTagUint16  byte = 9
	constantTagUint32  byte = 10
	fileSignature           = "MOCHI"
)

func integerConstantTag(kind value.Kind) byte {
	switch kind {
	case value.KindInt8:
		return constantTagInt8
	case value.KindInt16:
		return constantTagInt16
	case value.KindInt32:
		return constantTagInt32
	case value.KindUint8:
		return constantTagUint8
	case value.KindUint16:
		return constantTagUint16
	default:
		return constantTagUint32
	}
}

// Encode 验证字节码并写入最低可用的 Mochi 版本
func Encode(w io.Writer, bytecode *compiler.Bytecode) error {
	if w == nil {
		return fmt.Errorf("mochi: nil writer")
	}
	if err := Validate(bytecode); err != nil {
		return fmt.Errorf("mochi: invalid bytecode: %w", err)
	}

	size, err := encodedSize(bytecode)
	if err != nil {
		return fmt.Errorf("mochi: %w", err)
	}
	if size > MaxFileSize {
		return fmt.Errorf("mochi: encoded file size %d exceeds limit %d", size, MaxFileSize)
	}

	if err := writeFull(w, []byte(fileSignature)); err != nil {
		return fmt.Errorf("mochi: write signature: %w", err)
	}
	version := encodedVersion(bytecode)
	if err := writeUint16(w, version); err != nil {
		return fmt.Errorf("mochi: write version: %w", err)
	}
	if err := writeUint32(w, uint32(len(bytecode.Constants))); err != nil {
		return fmt.Errorf("mochi: write constant count: %w", err)
	}

	for i, constant := range bytecode.Constants {
		switch constant.Kind() {
		case value.KindInt:
			integer, _ := constant.Int64()
			if err := writeFull(w, []byte{constantTagInt}); err != nil {
				return fmt.Errorf("mochi: write constant %d tag: %w", i, err)
			}
			if err := writeUint64(w, uint64(integer)); err != nil {
				return fmt.Errorf("mochi: write constant %d integer: %w", i, err)
			}

		case value.KindString:
			text, _ := constant.Text()
			if err := writeFull(w, []byte{constantTagString}); err != nil {
				return fmt.Errorf("mochi: write constant %d tag: %w", i, err)
			}
			if err := writeUint32(w, uint32(len(text))); err != nil {
				return fmt.Errorf("mochi: write constant %d string length: %w", i, err)
			}
			if err := writeFull(w, []byte(text)); err != nil {
				return fmt.Errorf("mochi: write constant %d string: %w", i, err)
			}

		case value.KindFloat:
			floating, _ := constant.Float64()
			if err := writeFull(w, []byte{constantTagFloat}); err != nil {
				return fmt.Errorf("mochi: write constant %d tag: %w", i, err)
			}
			if err := writeUint64(w, math.Float64bits(floating)); err != nil {
				return fmt.Errorf("mochi: write constant %d float: %w", i, err)
			}
		case value.KindFloat32:
			floating, _ := constant.Float32()
			if err := writeFull(w, []byte{constantTagFloat32}); err != nil {
				return fmt.Errorf("mochi: write constant %d tag: %w", i, err)
			}
			if err := writeUint32(w, math.Float32bits(floating)); err != nil {
				return fmt.Errorf("mochi: write constant %d float32: %w", i, err)
			}
		case value.KindInt8, value.KindUint8:
			bits, _ := constant.IntegerBits()
			if err := writeFull(w, []byte{integerConstantTag(constant.Kind()), byte(bits)}); err != nil {
				return fmt.Errorf("mochi: write constant %d integer: %w", i, err)
			}
		case value.KindInt16, value.KindUint16:
			bits, _ := constant.IntegerBits()
			if err := writeFull(w, []byte{integerConstantTag(constant.Kind())}); err != nil {
				return fmt.Errorf("mochi: write constant %d tag: %w", i, err)
			}
			if err := writeUint16(w, uint16(bits)); err != nil {
				return fmt.Errorf("mochi: write constant %d integer: %w", i, err)
			}
		case value.KindInt32, value.KindUint32:
			bits, _ := constant.IntegerBits()
			if err := writeFull(w, []byte{integerConstantTag(constant.Kind())}); err != nil {
				return fmt.Errorf("mochi: write constant %d tag: %w", i, err)
			}
			if err := writeUint32(w, uint32(bits)); err != nil {
				return fmt.Errorf("mochi: write constant %d integer: %w", i, err)
			}
		}
	}

	if err := writeUint32(w, uint32(bytecode.GlobalsCount)); err != nil {
		return fmt.Errorf("mochi: write global slot count: %w", err)
	}
	if err := writeUint32(w, uint32(len(bytecode.Instructions))); err != nil {
		return fmt.Errorf("mochi: write instruction length: %w", err)
	}
	if err := writeFull(w, bytecode.Instructions); err != nil {
		return fmt.Errorf("mochi: write instructions: %w", err)
	}
	if err := writeUint32(w, uint32(len(bytecode.Functions))); err != nil {
		return fmt.Errorf("mochi: write function count: %w", err)
	}
	for i, function := range bytecode.Functions {
		if err := writeUint16(w, uint16(function.ID)); err != nil {
			return fmt.Errorf("mochi: write function %d ID: %w", i, err)
		}
		if err := writeUint16(w, uint16(len(function.Name))); err != nil {
			return fmt.Errorf("mochi: write function %d name length: %w", i, err)
		}
		if err := writeFull(w, []byte(function.Name)); err != nil {
			return fmt.Errorf("mochi: write function %d name: %w", i, err)
		}
		if err := writeUint32(w, uint32(function.ParameterCount)); err != nil {
			return fmt.Errorf("mochi: write function %d parameter count: %w", i, err)
		}
		if err := writeUint32(w, uint32(function.LocalCount)); err != nil {
			return fmt.Errorf("mochi: write function %d local count: %w", i, err)
		}
		flags := byte(0)
		if function.ReturnsValue {
			flags = 1
		}
		if err := writeFull(w, []byte{flags}); err != nil {
			return fmt.Errorf("mochi: write function %d flags: %w", i, err)
		}
		if version >= VersionV9 {
			if err := writeUint32(w, uint32(len(function.Captures))); err != nil {
				return fmt.Errorf("mochi: write function %d capture count: %w", i, err)
			}
			for captureIndex, capture := range function.Captures {
				local := byte(0)
				if capture.Local {
					local = 1
				}
				if err := writeFull(w, []byte{local}); err != nil {
					return fmt.Errorf("mochi: write function %d capture %d source: %w", i, captureIndex, err)
				}
				if err := writeUint32(w, uint32(capture.Index)); err != nil {
					return fmt.Errorf("mochi: write function %d capture %d index: %w", i, captureIndex, err)
				}
			}
		}
		if err := writeUint32(w, uint32(len(function.Instructions))); err != nil {
			return fmt.Errorf("mochi: write function %d instruction length: %w", i, err)
		}
		if err := writeFull(w, function.Instructions); err != nil {
			return fmt.Errorf("mochi: write function %d instructions: %w", i, err)
		}
	}

	return nil
}

// Decode 读取并验证 Mochi v1 至 v11 数据
// 返回的常量和指令切片均为新分配。
func Decode(r io.Reader) (*compiler.Bytecode, error) {
	if r == nil {
		return nil, fmt.Errorf("mochi: nil reader")
	}

	limited := &io.LimitedReader{R: r, N: MaxFileSize + 1}

	var signature [len(fileSignature)]byte
	if err := readFull(limited, signature[:], "signature"); err != nil {
		return nil, err
	}
	if string(signature[:]) != fileSignature {
		return nil, fmt.Errorf("mochi: invalid file signature")
	}

	version, err := readUint16(limited, "version")
	if err != nil {
		return nil, err
	}
	if version < VersionV1 || version > Version {
		return nil, fmt.Errorf("mochi: unsupported format version %d", version)
	}

	constantCount, err := readUint32(limited, "constant count")
	if err != nil {
		return nil, err
	}
	if constantCount > MaxConstants {
		return nil, fmt.Errorf("mochi: constant count %d exceeds limit %d", constantCount, MaxConstants)
	}
	declaredSize := uint64(len(fileSignature) + 2 + 4)

	constants := make([]value.Value, int(constantCount))
	for i := range constants {
		var tag [1]byte
		if err := readFull(limited, tag[:], fmt.Sprintf("constant %d tag", i)); err != nil {
			return nil, err
		}
		declaredSize++

		switch tag[0] {
		case constantTagInt:
			declaredSize += 8
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			bits, err := readUint64(limited, fmt.Sprintf("constant %d integer", i))
			if err != nil {
				return nil, err
			}
			constants[i] = value.NewInt(int64(bits))

		case constantTagString:
			length, err := readUint32(limited, fmt.Sprintf("constant %d string length", i))
			if err != nil {
				return nil, err
			}
			if length > MaxStringBytes {
				return nil, fmt.Errorf("mochi: constant %d string length %d exceeds limit %d", i, length, MaxStringBytes)
			}
			declaredSize += 4 + uint64(length)
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}

			data := make([]byte, int(length))
			if err := readFull(limited, data, fmt.Sprintf("constant %d string data", i)); err != nil {
				return nil, err
			}
			if !utf8.Valid(data) {
				return nil, fmt.Errorf("mochi: constant %d contains invalid UTF-8", i)
			}
			constants[i] = value.NewString(string(data))

		case constantTagFloat:
			if version < VersionV3 {
				return nil, fmt.Errorf("mochi: constant %d has invalid type tag %d for format version %d", i, tag[0], version)
			}
			declaredSize += 8
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			bits, err := readUint64(limited, fmt.Sprintf("constant %d float", i))
			if err != nil {
				return nil, err
			}
			constants[i] = value.NewFloat(math.Float64frombits(bits))

		case constantTagFloat32:
			if version < VersionV10 {
				return nil, fmt.Errorf("mochi: constant %d has invalid float32 tag for format version %d", i, version)
			}
			declaredSize += 4
			bits, err := readUint32(limited, fmt.Sprintf("constant %d float32", i))
			if err != nil {
				return nil, err
			}
			constants[i] = value.NewFloat32(math.Float32frombits(bits))

		case constantTagInt8, constantTagUint8:
			if version < VersionV11 {
				return nil, fmt.Errorf("mochi: constant %d has invalid fixed integer tag for format version %d", i, version)
			}
			var data [1]byte
			if err := readFull(limited, data[:], fmt.Sprintf("constant %d integer", i)); err != nil {
				return nil, err
			}
			declaredSize++
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			kind := value.KindInt8
			if tag[0] == constantTagUint8 {
				kind = value.KindUint8
			}
			constants[i], _ = value.NewIntegerBits(kind, uint64(data[0]))

		case constantTagInt16, constantTagUint16:
			if version < VersionV11 {
				return nil, fmt.Errorf("mochi: constant %d has invalid fixed integer tag for format version %d", i, version)
			}
			bits, err := readUint16(limited, fmt.Sprintf("constant %d integer", i))
			if err != nil {
				return nil, err
			}
			declaredSize += 2
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			kind := value.KindInt16
			if tag[0] == constantTagUint16 {
				kind = value.KindUint16
			}
			constants[i], _ = value.NewIntegerBits(kind, uint64(bits))

		case constantTagInt32, constantTagUint32:
			if version < VersionV11 {
				return nil, fmt.Errorf("mochi: constant %d has invalid fixed integer tag for format version %d", i, version)
			}
			bits, err := readUint32(limited, fmt.Sprintf("constant %d integer", i))
			if err != nil {
				return nil, err
			}
			declaredSize += 4
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			kind := value.KindInt32
			if tag[0] == constantTagUint32 {
				kind = value.KindUint32
			}
			constants[i], _ = value.NewIntegerBits(kind, uint64(bits))

		default:
			return nil, fmt.Errorf("mochi: constant %d has invalid type tag %d", i, tag[0])
		}
	}

	globalCount, err := readUint32(limited, "global slot count")
	if err != nil {
		return nil, err
	}
	if globalCount > MaxGlobals {
		return nil, fmt.Errorf("mochi: global slot count %d exceeds limit %d", globalCount, MaxGlobals)
	}
	declaredSize += 4

	instructionLength, err := readUint32(limited, "instruction length")
	if err != nil {
		return nil, err
	}
	if instructionLength > MaxInstructionBytes {
		return nil, fmt.Errorf("mochi: instruction length %d exceeds limit %d", instructionLength, MaxInstructionBytes)
	}
	declaredSize += 4 + uint64(instructionLength)
	if err := checkDeclaredSize(declaredSize); err != nil {
		return nil, err
	}

	instructions := make([]byte, int(instructionLength))
	if err := readFull(limited, instructions, "instruction data"); err != nil {
		return nil, err
	}

	var functions []compiler.CompiledFunction
	if version >= VersionV2 {
		functionCount, err := readUint32(limited, "function count")
		if err != nil {
			return nil, err
		}
		if functionCount > MaxFunctions {
			return nil, fmt.Errorf("mochi: function count %d exceeds limit %d", functionCount, MaxFunctions)
		}
		declaredSize += 4
		if functionCount > 0 {
			functions = make([]compiler.CompiledFunction, int(functionCount))
		}
		for i := range functions {
			id, err := readUint16(limited, fmt.Sprintf("function %d ID", i))
			if err != nil {
				return nil, err
			}
			nameLength, err := readUint16(limited, fmt.Sprintf("function %d name length", i))
			if err != nil {
				return nil, err
			}
			declaredSize += 4 + uint64(nameLength)
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			name := make([]byte, int(nameLength))
			if err := readFull(limited, name, fmt.Sprintf("function %d name", i)); err != nil {
				return nil, err
			}
			if !utf8.Valid(name) {
				return nil, fmt.Errorf("mochi: function %d name contains invalid UTF-8", i)
			}
			parameterCount, err := readUint32(limited, fmt.Sprintf("function %d parameter count", i))
			if err != nil {
				return nil, err
			}
			localCount, err := readUint32(limited, fmt.Sprintf("function %d local count", i))
			if err != nil {
				return nil, err
			}
			if parameterCount > MaxFunctionSlots {
				return nil, fmt.Errorf("mochi: function %d parameter count %d exceeds limit %d", i, parameterCount, MaxFunctionSlots)
			}
			if localCount < parameterCount || localCount > MaxFunctionSlots {
				return nil, fmt.Errorf("mochi: function %d local count %d is invalid", i, localCount)
			}
			var flags [1]byte
			if err := readFull(limited, flags[:], fmt.Sprintf("function %d flags", i)); err != nil {
				return nil, err
			}
			if flags[0]&^byte(1) != 0 {
				return nil, fmt.Errorf("mochi: function %d has invalid flags %d", i, flags[0])
			}
			var captures []compiler.Capture
			if version >= VersionV9 {
				captureCount, err := readUint32(limited, fmt.Sprintf("function %d capture count", i))
				if err != nil {
					return nil, err
				}
				if captureCount > MaxFunctionSlots {
					return nil, fmt.Errorf("mochi: function %d capture count %d exceeds limit %d", i, captureCount, MaxFunctionSlots)
				}
				declaredSize += 4 + uint64(captureCount)*5
				if err := checkDeclaredSize(declaredSize); err != nil {
					return nil, err
				}
				if captureCount > 0 {
					captures = make([]compiler.Capture, int(captureCount))
				}
				for captureIndex := range captures {
					var source [1]byte
					if err := readFull(limited, source[:], fmt.Sprintf("function %d capture %d source", i, captureIndex)); err != nil {
						return nil, err
					}
					if source[0] > 1 {
						return nil, fmt.Errorf("mochi: function %d capture %d has invalid source %d", i, captureIndex, source[0])
					}
					captureSlot, err := readUint32(limited, fmt.Sprintf("function %d capture %d index", i, captureIndex))
					if err != nil {
						return nil, err
					}
					captures[captureIndex] = compiler.Capture{Local: source[0] == 1, Index: int(captureSlot)}
				}
			}
			length, err := readUint32(limited, fmt.Sprintf("function %d instruction length", i))
			if err != nil {
				return nil, err
			}
			if length > MaxInstructionBytes {
				return nil, fmt.Errorf("mochi: function %d instruction length %d exceeds limit %d", i, length, MaxInstructionBytes)
			}
			declaredSize += 4 + 4 + 1 + 4 + uint64(length)
			if err := checkDeclaredSize(declaredSize); err != nil {
				return nil, err
			}
			functionInstructions := make([]byte, int(length))
			if err := readFull(limited, functionInstructions, fmt.Sprintf("function %d instruction data", i)); err != nil {
				return nil, err
			}
			functions[i] = compiler.CompiledFunction{
				ID:             int(id),
				Name:           string(name),
				Instructions:   functionInstructions,
				ParameterCount: int(parameterCount),
				LocalCount:     int(localCount),
				ReturnsValue:   flags[0]&1 != 0,
				Captures:       captures,
			}
		}
	}

	var extra [1]byte
	if _, err := io.ReadFull(limited, extra[:]); err == nil {
		return nil, fmt.Errorf("mochi: trailing file data")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("mochi: check trailing data: %w", err)
	}

	bytecode := &compiler.Bytecode{
		Instructions: instructions,
		Constants:    constants,
		GlobalsCount: int(globalCount),
		Functions:    functions,
	}
	if err := validateVersion(bytecode, version); err != nil {
		return nil, fmt.Errorf("mochi: invalid bytecode: %w", err)
	}

	return bytecode, nil
}

func encodedSize(bytecode *compiler.Bytecode) (uint64, error) {
	size := uint64(len(fileSignature) + 2 + 4 + 4 + 4 + 4)
	for i, constant := range bytecode.Constants {
		switch constant.Kind() {
		case value.KindInt:
			size += 1 + 8
		case value.KindFloat:
			size += 1 + 8
		case value.KindFloat32:
			size += 1 + 4
		case value.KindInt8, value.KindUint8:
			size += 1 + 1
		case value.KindInt16, value.KindUint16:
			size += 1 + 2
		case value.KindInt32, value.KindUint32:
			size += 1 + 4
		case value.KindString:
			text, _ := constant.Text()
			size += 1 + 4 + uint64(len(text))
		default:
			return 0, fmt.Errorf("constant %d has unsupported kind %s", i, constant.Kind())
		}
	}
	size += uint64(len(bytecode.Instructions))
	version := encodedVersion(bytecode)
	for _, function := range bytecode.Functions {
		size += 2 + 2 + uint64(len(function.Name)) + 4 + 4 + 1 + 4 + uint64(len(function.Instructions))
		if version >= VersionV9 {
			size += 4 + uint64(len(function.Captures))*5
		}
	}
	return size, nil
}

// encodedVersion 选择可表示当前字节码的最低版本
func encodedVersion(bytecode *compiler.Bytecode) uint16 {
	if hasArrayBulkInstructions(bytecode.Instructions) {
		return VersionV13
	}
	for _, function := range bytecode.Functions {
		if hasArrayBulkInstructions(function.Instructions) {
			return VersionV13
		}
	}
	if hasDynamicArrayInstructions(bytecode.Instructions) {
		return VersionV12
	}
	for _, function := range bytecode.Functions {
		if hasDynamicArrayInstructions(function.Instructions) {
			return VersionV12
		}
	}
	for _, constant := range bytecode.Constants {
		if constant.Kind().IsInteger() && constant.Kind() != value.KindInt {
			return VersionV11
		}
	}
	if hasFixedIntegerInstructions(bytecode.Instructions) {
		return VersionV11
	}
	for _, function := range bytecode.Functions {
		if hasFixedIntegerInstructions(function.Instructions) {
			return VersionV11
		}
	}
	for _, constant := range bytecode.Constants {
		if constant.Kind() == value.KindFloat32 {
			return VersionV10
		}
	}
	if hasFloat32Instructions(bytecode.Instructions) {
		return VersionV10
	}
	for _, function := range bytecode.Functions {
		if hasFloat32Instructions(function.Instructions) {
			return VersionV10
		}
	}
	for _, function := range bytecode.Functions {
		if len(function.Captures) != 0 || hasClosureInstructions(function.Instructions) {
			return VersionV10
		}
	}
	if hasClosureInstructions(bytecode.Instructions) {
		return VersionV10
	}
	if hasCoreTypeInstructions(bytecode.Instructions) {
		return VersionV10
	}
	for _, function := range bytecode.Functions {
		if hasCoreTypeInstructions(function.Instructions) {
			return VersionV10
		}
	}
	if hasNullableInstructions(bytecode.Instructions) {
		return VersionV10
	}
	for _, function := range bytecode.Functions {
		if hasNullableInstructions(function.Instructions) {
			return VersionV10
		}
	}
	if hasArrayMutationInstructions(bytecode.Instructions) {
		return VersionV6
	}
	for _, function := range bytecode.Functions {
		if hasArrayMutationInstructions(function.Instructions) {
			return VersionV6
		}
	}
	if hasArrayInstructions(bytecode.Instructions) {
		return VersionV5
	}
	for _, function := range bytecode.Functions {
		if hasArrayInstructions(function.Instructions) {
			return VersionV5
		}
	}
	if hasConversionInstructions(bytecode.Instructions) {
		return VersionV4
	}
	for _, function := range bytecode.Functions {
		if hasConversionInstructions(function.Instructions) {
			return VersionV4
		}
	}
	for _, constant := range bytecode.Constants {
		if constant.Kind() == value.KindFloat {
			return VersionV3
		}
	}
	return VersionV2
}

func hasArrayBulkInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op == code.OpArrayTruncate || op == code.OpArrayAppendAll {
			return true
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return false
		}
		_, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return false
		}
		offset += 1 + read
	}
	return false
}

func hasDynamicArrayInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op >= code.OpArrayTyped && op <= code.OpArrayClone {
			return true
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return false
		}
		_, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return false
		}
		offset += 1 + read
	}
	return false
}

func hasFloat32Instructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op >= code.OpIntToFloat32 && op <= code.OpMath {
			return true
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return false
		}
		_, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return false
		}
		offset += 1 + read
	}
	return false
}

func hasFixedIntegerInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op == code.OpIntegerConvert {
			return true
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return false
		}
		_, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return false
		}
		offset += 1 + read
	}
	return false
}

// hasClosureInstructions 检查指令流是否使用闭包指令
func hasClosureInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op >= code.OpClosure && op <= code.OpDefineLocal {
			return true
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return false
		}
		_, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return false
		}
		offset += 1 + read
	}
	return false
}

func hasCoreTypeInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op >= code.OpStruct && op <= code.OpCallValue {
			return true
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return false
		}
		_, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return false
		}
		offset += 1 + read
	}
	return false
}

func hasNullableInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op == code.OpIsNil || op == code.OpSetNullableIndex {
			return true
		}
		definition, _ := code.Lookup(op)
		_, read, _ := code.ReadOperands(definition, instructions[offset+1:])
		offset += 1 + read
	}
	return false
}

func hasArrayMutationInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op == code.OpLen || op == code.OpSetIndex {
			return true
		}
		definition, _ := code.Lookup(op)
		_, read, _ := code.ReadOperands(definition, instructions[offset+1:])
		offset += 1 + read
	}
	return false
}

func hasArrayInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op == code.OpArray || op == code.OpIndex {
			return true
		}
		definition, _ := code.Lookup(op)
		_, read, _ := code.ReadOperands(definition, instructions[offset+1:])
		offset += 1 + read
	}
	return false
}

func hasConversionInstructions(instructions []byte) bool {
	for offset := 0; offset < len(instructions); {
		op := code.Opcode(instructions[offset])
		if op == code.OpIntToFloat || op == code.OpFloatToInt {
			return true
		}
		definition, _ := code.Lookup(op)
		_, read, _ := code.ReadOperands(definition, instructions[offset+1:])
		offset += 1 + read
	}
	return false
}

func checkDeclaredSize(size uint64) error {
	if size > MaxFileSize {
		return fmt.Errorf("mochi: declared file size %d exceeds limit %d", size, MaxFileSize)
	}
	return nil
}

func readFull(r io.Reader, data []byte, field string) error {
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("mochi: truncated %s: %w", field, err)
	}
	return nil
}

func readUint16(r io.Reader, field string) (uint16, error) {
	var data [2]byte
	if err := readFull(r, data[:], field); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data[:]), nil
}

func readUint32(r io.Reader, field string) (uint32, error) {
	var data [4]byte
	if err := readFull(r, data[:], field); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data[:]), nil
}

func readUint64(r io.Reader, field string) (uint64, error) {
	var data [8]byte
	if err := readFull(r, data[:], field); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(data[:]), nil
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(data) {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func writeUint16(w io.Writer, value uint16) error {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], value)
	return writeFull(w, data[:])
}

func writeUint32(w io.Writer, value uint32) error {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	return writeFull(w, data[:])
}

func writeUint64(w io.Writer, value uint64) error {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], value)
	return writeFull(w, data[:])
}
