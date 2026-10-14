package mochi

import (
	"fmt"
	"unicode/utf8"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

type instructionReference struct {
	offset int
	value  int
}

// Validate 验证字节码结构及索引引用
// 该函数不检查 Stack 深度和运行时类型。
func Validate(bytecode *compiler.Bytecode) error {
	return validateVersion(bytecode, Version)
}

func validateVersion(bytecode *compiler.Bytecode, version uint16) error {
	if bytecode == nil {
		return fmt.Errorf("nil bytecode")
	}
	if len(bytecode.Constants) > MaxConstants {
		return fmt.Errorf("constant count %d exceeds limit %d", len(bytecode.Constants), MaxConstants)
	}
	if bytecode.GlobalsCount < 0 || bytecode.GlobalsCount > MaxGlobals {
		return fmt.Errorf("global slot count %d is outside range 0..%d", bytecode.GlobalsCount, MaxGlobals)
	}
	if len(bytecode.Instructions) > MaxInstructionBytes {
		return fmt.Errorf("instruction length %d exceeds limit %d", len(bytecode.Instructions), MaxInstructionBytes)
	}
	if len(bytecode.Functions) > MaxFunctions {
		return fmt.Errorf("function count %d exceeds limit %d", len(bytecode.Functions), MaxFunctions)
	}

	for i, constant := range bytecode.Constants {
		switch constant.Kind() {
		case value.KindInt:
		case value.KindInt8, value.KindInt16, value.KindInt32, value.KindUint8, value.KindUint16, value.KindUint32:
			if version < Version {
				return fmt.Errorf("constant %d kind %s requires Mochi v11", i, constant.Kind())
			}
		case value.KindFloat:
		case value.KindFloat32:
		case value.KindString:
			text, _ := constant.Text()
			if len(text) > MaxStringBytes {
				return fmt.Errorf("constant %d string length %d exceeds limit %d", i, len(text), MaxStringBytes)
			}
			if !utf8.ValidString(text) {
				return fmt.Errorf("constant %d contains invalid UTF-8", i)
			}
		default:
			return fmt.Errorf("constant %d has unsupported kind %s", i, constant.Kind())
		}
	}

	if err := validateInstructions(bytecode, bytecode.Instructions, nil, version); err != nil {
		return fmt.Errorf("main instructions: %w", err)
	}
	for index := range bytecode.Functions {
		function := &bytecode.Functions[index]
		if function.ID != index {
			return fmt.Errorf("function %d has non-canonical FunctionID %d", index, function.ID)
		}
		if function.Name == "" {
			return fmt.Errorf("function %d has empty name", index)
		}
		if len(function.Name) > MaxFunctionNameBytes {
			return fmt.Errorf("function %d name length %d exceeds limit %d", index, len(function.Name), MaxFunctionNameBytes)
		}
		if !utf8.ValidString(function.Name) {
			return fmt.Errorf("function %d name contains invalid UTF-8", index)
		}
		if function.ParameterCount < 0 || function.ParameterCount > MaxFunctionSlots {
			return fmt.Errorf("function %d parameter count %d is outside range", index, function.ParameterCount)
		}
		if function.LocalCount < function.ParameterCount || function.LocalCount > MaxFunctionSlots {
			return fmt.Errorf("function %d local count %d is invalid", index, function.LocalCount)
		}
		if version < VersionV9 && len(function.Captures) != 0 {
			return fmt.Errorf("function %d captures require Mochi v9", index)
		}
		if len(function.Captures) > MaxFunctionSlots {
			return fmt.Errorf("function %d capture count %d exceeds limit %d", index, len(function.Captures), MaxFunctionSlots)
		}
		for captureIndex, capture := range function.Captures {
			if capture.Index < 0 || capture.Index >= MaxFunctionSlots {
				return fmt.Errorf("function %d capture %d index %d is outside range", index, captureIndex, capture.Index)
			}
		}
		if len(function.Instructions) > MaxInstructionBytes {
			return fmt.Errorf("function %d instruction length %d exceeds limit %d", index, len(function.Instructions), MaxInstructionBytes)
		}
		if err := validateInstructions(bytecode, function.Instructions, function, version); err != nil {
			return fmt.Errorf("function %d instructions: %w", index, err)
		}
	}

	return nil
}

func validateInstructions(bytecode *compiler.Bytecode, instructions code.Instructions, function *compiler.CompiledFunction, version uint16) error {
	boundaries := make([]bool, len(instructions)+1)
	boundaries[len(instructions)] = true
	references := make([]instructionReference, 0)

	for offset := 0; offset < len(instructions); {
		boundaries[offset] = true
		op := code.Opcode(instructions[offset])
		if version < VersionV4 && (op == code.OpIntToFloat || op == code.OpFloatToInt) {
			return fmt.Errorf("opcode %d requires Mochi v4 at offset %d", op, offset)
		}
		if version < VersionV5 && (op == code.OpArray || op == code.OpIndex) {
			return fmt.Errorf("opcode %d requires Mochi v5 at offset %d", op, offset)
		}
		if version < VersionV6 && (op == code.OpLen || op == code.OpSetIndex) {
			return fmt.Errorf("opcode %d requires Mochi v6 at offset %d", op, offset)
		}
		if version < VersionV7 && (op == code.OpIsNil || op == code.OpSetNullableIndex) {
			return fmt.Errorf("opcode %d requires Mochi v7 at offset %d", op, offset)
		}
		if version < VersionV8 && op >= code.OpStruct && op <= code.OpCallValue {
			return fmt.Errorf("opcode %d requires Mochi v8 at offset %d", op, offset)
		}
		if version < VersionV9 && op >= code.OpClosure && op <= code.OpDefineLocal {
			return fmt.Errorf("opcode %d requires Mochi v9 at offset %d", op, offset)
		}
		if version < VersionV10 && op >= code.OpIntToFloat32 && op <= code.OpMath {
			return fmt.Errorf("opcode %d requires Mochi v10 at offset %d", op, offset)
		}
		if version < Version && op == code.OpIntegerConvert {
			return fmt.Errorf("opcode %d requires Mochi v11 at offset %d", op, offset)
		}
		definition, err := code.Lookup(op)
		if err != nil {
			return fmt.Errorf("unknown opcode at offset %d: %w", offset, err)
		}
		operands, read, err := code.ReadOperands(definition, instructions[offset+1:])
		if err != nil {
			return fmt.Errorf("truncated operands at offset %d: %w", offset, err)
		}
		if offset+1+read > len(instructions) {
			return fmt.Errorf("invalid instruction width at offset %d", offset)
		}

		switch op {
		case code.OpConstant:
			if operands[0] >= len(bytecode.Constants) {
				return fmt.Errorf("constant index %d out of range at offset %d", operands[0], offset)
			}
		case code.OpMath:
			if operands[0] < code.MathSqrt || operands[0] > code.MathError {
				return fmt.Errorf("unknown math primitive %d at offset %d", operands[0], offset)
			}
			expected := 1
			if operands[0] == code.MathAddOverflows {
				expected = 2
			}
			if operands[1] != expected {
				return fmt.Errorf("math primitive %d expects %d arguments at offset %d", operands[0], expected, offset)
			}
		case code.OpIntegerConvert:
			if operands[0] < code.IntegerInt8 || operands[0] > code.IntegerUint32 {
				return fmt.Errorf("unknown integer conversion target %d at offset %d", operands[0], offset)
			}
		case code.OpSetGlobal, code.OpGetGlobal:
			if operands[0] >= bytecode.GlobalsCount {
				return fmt.Errorf("global slot index %d out of range at offset %d", operands[0], offset)
			}
		case code.OpSetLocal, code.OpGetLocal, code.OpDefineLocal:
			if function == nil {
				return fmt.Errorf("%s outside function at offset %d", definition.Name, offset)
			}
			if operands[0] >= function.LocalCount {
				return fmt.Errorf("local slot index %d out of range at offset %d", operands[0], offset)
			}
		case code.OpCall, code.OpFunction, code.OpClosure:
			if operands[0] >= len(bytecode.Functions) {
				return fmt.Errorf("FunctionID %d out of range at offset %d", operands[0], offset)
			}
			if (op == code.OpCall || op == code.OpFunction) && len(bytecode.Functions[operands[0]].Captures) != 0 {
				return fmt.Errorf("%s cannot reference capturing function %d at offset %d", definition.Name, operands[0], offset)
			}
			if op == code.OpClosure {
				for captureIndex, capture := range bytecode.Functions[operands[0]].Captures {
					if function == nil {
						return fmt.Errorf("closure function %d capture %d has no enclosing function at offset %d", operands[0], captureIndex, offset)
					}
					limit := len(function.Captures)
					if capture.Local {
						limit = function.LocalCount
					}
					if capture.Index >= limit {
						return fmt.Errorf("closure function %d capture %d index %d out of range at offset %d", operands[0], captureIndex, capture.Index, offset)
					}
				}
			}
		case code.OpGetUpvalue, code.OpSetUpvalue:
			if function == nil {
				return fmt.Errorf("%s outside function at offset %d", definition.Name, offset)
			}
			if operands[0] >= len(function.Captures) {
				return fmt.Errorf("upvalue index %d out of range at offset %d", operands[0], offset)
			}
		case code.OpCallValue:
			if operands[0] > MaxFunctionSlots {
				return fmt.Errorf("argument count %d out of range at offset %d", operands[0], offset)
			}
		case code.OpJump, code.OpJumpIfFalse:
			references = append(references, instructionReference{offset: offset, value: operands[0]})
		case code.OpReturn:
			if function == nil {
				return fmt.Errorf("OpReturn outside function at offset %d", offset)
			}
			if function.ReturnsValue {
				return fmt.Errorf("value-returning function uses OpReturn at offset %d", offset)
			}
		case code.OpReturnValue:
			if function == nil {
				return fmt.Errorf("OpReturnValue outside function at offset %d", offset)
			}
			if !function.ReturnsValue {
				return fmt.Errorf("unit function uses OpReturnValue at offset %d", offset)
			}
		}

		offset += 1 + read
	}

	for _, reference := range references {
		if reference.value > len(instructions) {
			return fmt.Errorf("jump target %d out of range at offset %d", reference.value, reference.offset)
		}
		if !boundaries[reference.value] {
			return fmt.Errorf("jump target %d is not an instruction boundary at offset %d", reference.value, reference.offset)
		}
	}
	return nil
}
