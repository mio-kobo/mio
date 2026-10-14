package mochi

import (
	"encoding/binary"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
)

// 测试核心类型指令要求 Mochi v8
func TestCoreTypeInstructionsRequireMochiV8(t *testing.T) {
	ops := []struct {
		op       code.Opcode
		operands []int
	}{
		{code.OpStruct, []int{0}},
		{code.OpEnum, []int{0, 0}},
		{code.OpMap, nil},
	}
	for _, item := range ops {
		instructions, err := code.Make(item.op, item.operands...)
		if err != nil {
			t.Fatal(err)
		}
		bytecode := &compiler.Bytecode{Instructions: instructions}
		if err := validateVersion(bytecode, VersionV7); err == nil {
			t.Fatalf("%v accepted by Mochi v7", item.op)
		}
		_, encoded := roundTrip(t, bytecode)
		if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV10 {
			t.Fatalf("version=%d", version)
		}
	}
}
