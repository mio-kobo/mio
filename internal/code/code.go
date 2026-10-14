package code

import (
	"encoding/binary"
	"fmt"
)

// Opcode 表示一个字节码操作码
type Opcode byte

// Instructions 是编码后的字节码序列
type Instructions []byte

const (
	OpConstant     Opcode = 1
	OpAdd          Opcode = 2
	OpPop          Opcode = 3
	OpSub          Opcode = 4
	OpMul          Opcode = 5
	OpDiv          Opcode = 6
	OpNeg          Opcode = 7
	OpTrue         Opcode = 8
	OpFalse        Opcode = 9
	OpNot          Opcode = 10
	OpEqual        Opcode = 11
	OpNotEqual     Opcode = 12
	OpLess         Opcode = 13
	OpLessEqual    Opcode = 14
	OpGreater      Opcode = 15
	OpGreaterEqual Opcode = 16
	OpJump         Opcode = 17
	OpJumpIfFalse  Opcode = 18
	OpSetGlobal    Opcode = 19
	OpGetGlobal    Opcode = 20
	OpNil          Opcode = 21
	OpChant        Opcode = 22

	// 函数相关 Opcode 从 23 开始固定编号
	OpSetLocal         Opcode = 23
	OpGetLocal         Opcode = 24
	OpCall             Opcode = 25
	OpReturn           Opcode = 26
	OpReturnValue      Opcode = 27
	OpIntToFloat       Opcode = 28
	OpFloatToInt       Opcode = 29
	OpArray            Opcode = 30
	OpIndex            Opcode = 31
	OpLen              Opcode = 32
	OpSetIndex         Opcode = 33
	OpIsNil            Opcode = 34
	OpSetNullableIndex Opcode = 35
	OpStruct           Opcode = 36
	OpGetField         Opcode = 37
	OpEnum             Opcode = 38
	OpEnumTag          Opcode = 39
	OpGetEnumValue     Opcode = 40
	OpMap              Opcode = 41
	OpMapGet           Opcode = 42
	OpMapSet           Opcode = 43
	OpFunction         Opcode = 44
	OpCallValue        Opcode = 45
)

// Definition 描述指令名称及操作数长度
type Definition struct {
	Name          string
	OperandWidths []int
}

// 指令定义表
var definitions = map[Opcode]Definition{
	OpConstant: {
		Name:          "OpConstant",
		OperandWidths: []int{2},
	},
	OpAdd: {
		Name: "OpAdd",
	},
	OpPop: {
		Name: "OpPop",
	},
	OpSub: {
		Name: "OpSub",
	},
	OpMul: {
		Name: "OpMul",
	},
	OpDiv: {
		Name: "OpDiv",
	},
	OpNeg: {
		Name: "OpNeg",
	},
	OpTrue: {
		Name: "OpTrue",
	},
	OpFalse: {
		Name: "OpFalse",
	},
	OpNot: {
		Name: "OpNot",
	},
	OpEqual: {
		Name: "OpEqual",
	},
	OpNotEqual: {
		Name: "OpNotEqual",
	},
	OpLess: {
		Name: "OpLess",
	},
	OpLessEqual: {
		Name: "OpLessEqual",
	},
	OpGreater: {
		Name: "OpGreater",
	},
	OpGreaterEqual: {
		Name: "OpGreaterEqual",
	},

	OpJump: {
		Name:          "OpJump",
		OperandWidths: []int{2},
	},
	OpJumpIfFalse: {
		Name:          "OpJumpIfFalse",
		OperandWidths: []int{2},
	},

	OpSetGlobal: {
		Name:          "OpSetGlobal",
		OperandWidths: []int{2},
	},
	OpGetGlobal: {
		Name:          "OpGetGlobal",
		OperandWidths: []int{2},
	},
	OpNil: {
		Name: "OpNil",
	},
	OpChant: {
		Name: "OpChant",
	},
	OpSetLocal: {
		Name:          "OpSetLocal",
		OperandWidths: []int{2},
	},
	OpGetLocal: {
		Name:          "OpGetLocal",
		OperandWidths: []int{2},
	},
	OpIsNil: {
		Name: "OpIsNil",
	},
	OpCall: {
		Name:          "OpCall",
		OperandWidths: []int{2},
	},
	OpReturn: {
		Name: "OpReturn",
	},
	OpReturnValue: {
		Name: "OpReturnValue",
	},
	OpIntToFloat: {
		Name: "OpIntToFloat",
	},
	OpFloatToInt: {
		Name: "OpFloatToInt",
	},
	OpArray: {
		Name:          "OpArray",
		OperandWidths: []int{2},
	},
	OpIndex: {
		Name: "OpIndex",
	},
	OpLen: {
		Name: "OpLen",
	},
	OpSetIndex: {
		Name: "OpSetIndex",
	},
	OpSetNullableIndex: {
		Name: "OpSetNullableIndex",
	},
	OpStruct: {
		Name:          "OpStruct",
		OperandWidths: []int{2},
	},
	OpGetField: {
		Name:          "OpGetField",
		OperandWidths: []int{2},
	},
	OpEnum: {
		Name:          "OpEnum",
		OperandWidths: []int{2, 2},
	},
	OpEnumTag: {Name: "OpEnumTag"},
	OpGetEnumValue: {
		Name:          "OpGetEnumValue",
		OperandWidths: []int{2},
	},
	OpMap:    {Name: "OpMap"},
	OpMapGet: {Name: "OpMapGet"},
	OpMapSet: {Name: "OpMapSet"},
	OpFunction: {
		Name:          "OpFunction",
		OperandWidths: []int{2},
	},
	OpCallValue: {
		Name:          "OpCallValue",
		OperandWidths: []int{2},
	},
}

// Lookup 查询操作码对应的指令定义
func Lookup(op Opcode) (Definition, error) {
	definition, ok := definitions[op]
	if !ok {
		return Definition{}, fmt.Errorf(
			"unknown opcode %d",
			op,
		)
	}

	return definition, nil
}

// Make 根据操作码和操作数生成字节码
func Make(op Opcode, operands ...int) (Instructions, error) {
	definition, err := Lookup(op)
	if err != nil {
		return nil, err
	}

	// 检查操作数数量
	if len(operands) != len(definition.OperandWidths) {
		return nil, fmt.Errorf(
			"%s expects %d operands, got %d",
			definition.Name,
			len(definition.OperandWidths),
			len(operands),
		)
	}

	// 计算指令总长度
	length := 1
	for _, width := range definition.OperandWidths {
		length += width
	}

	ins := make(Instructions, length)
	ins[0] = byte(op)

	// 按照定义编码操作数
	offset := 1
	for i, width := range definition.OperandWidths {
		value := operands[i]

		switch width {
		case 2:
			if value < 0 || value > 0xffff {
				return nil, fmt.Errorf(
					"%s operand %d out of range: %d",
					definition.Name,
					i+1,
					value,
				)
			}

			binary.BigEndian.PutUint16(
				ins[offset:offset+2],
				uint16(value),
			)

		default:
			return nil, fmt.Errorf(
				"unsupported operand width %d",
				width,
			)
		}

		offset += width
	}

	return ins, nil
}

// ReadOperands 根据指令定义解码操作数
func ReadOperands(
	def Definition,
	ins Instructions,
) ([]int, int, error) {
	operands := make([]int, 0, len(def.OperandWidths))
	offset := 0

	// 按照定义依次读取操作数
	for i, width := range def.OperandWidths {
		switch width {
		case 2:
			// 检查剩余字节是否足够
			if len(ins)-offset < 2 {
				return nil, 0, fmt.Errorf(
					"%s operand %d: not enough bytes",
					def.Name,
					i+1,
				)
			}

			value := binary.BigEndian.Uint16(
				ins[offset : offset+2],
			)

			operands = append(operands, int(value))
			offset += 2

		default:
			return nil, 0, fmt.Errorf(
				"unsupported operand width %d",
				width,
			)
		}
	}

	return operands, offset, nil
}
