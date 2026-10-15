package mochi

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试动态数组指令使用 Mochi v12
func TestDynamicArrayInstructionsRequireMochiV12(t *testing.T) {
	instruction, err := code.Make(code.OpArrayWithCapacity, int(value.KindUint8))
	if err != nil {
		t.Fatal(err)
	}
	bytecode := &compiler.Bytecode{Instructions: instruction}
	if err := validateVersion(bytecode, VersionV11); err == nil || !strings.Contains(err.Error(), "requires Mochi v12") {
		t.Fatalf("unexpected error: %v", err)
	}
	var encoded bytes.Buffer
	if err := Encode(&encoded, bytecode); err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(encoded.Bytes()[5:7]); version != VersionV12 {
		t.Fatalf("version=%d", version)
	}
}

// 测试动态数组类型描述符校验
func TestDynamicArrayDescriptorValidation(t *testing.T) {
	instruction, err := code.Make(code.OpArrayMake, 127)
	if err != nil {
		t.Fatal(err)
	}
	err = Validate(&compiler.Bytecode{Instructions: instruction})
	if err == nil || !strings.Contains(err.Error(), "invalid array element descriptor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 测试数组批量指令使用 Mochi v13
func TestArrayBulkInstructionsRequireMochiV13(t *testing.T) {
	for _, opcode := range []code.Opcode{code.OpArrayTruncate, code.OpArrayAppendAll} {
		instruction, err := code.Make(opcode)
		if err != nil {
			t.Fatal(err)
		}
		bytecode := &compiler.Bytecode{Instructions: instruction}
		if err := validateVersion(bytecode, VersionV12); err == nil || !strings.Contains(err.Error(), "requires Mochi v13") {
			t.Fatalf("opcode=%d error=%v", opcode, err)
		}
		var encoded bytes.Buffer
		if err := Encode(&encoded, bytecode); err != nil {
			t.Fatal(err)
		}
		if version := binary.BigEndian.Uint16(encoded.Bytes()[5:7]); version != VersionV13 {
			t.Fatalf("opcode=%d version=%d", opcode, version)
		}
	}
}
