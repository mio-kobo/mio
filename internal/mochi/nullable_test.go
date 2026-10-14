package mochi

import (
	"encoding/binary"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
)

// 测试可空指令要求 Mochi v7
func TestNullableInstructionsRequireMochiV7(t *testing.T) {
	for _, op := range []code.Opcode{code.OpIsNil, code.OpSetNullableIndex} {
		bytecode := &compiler.Bytecode{Instructions: mustInstruction(t, op)}
		if err := validateVersion(bytecode, VersionV6); err == nil || !strings.Contains(err.Error(), "requires Mochi v7") {
			t.Fatalf("opcode=%s error=%v", mustOpcodeName(t, op), err)
		}
		_, encoded := roundTrip(t, bytecode)
		if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV10 {
			t.Fatalf("opcode=%s version=%d", mustOpcodeName(t, op), version)
		}
	}
}

func mustOpcodeName(t *testing.T, op code.Opcode) string {
	t.Helper()
	definition, err := code.Lookup(op)
	if err != nil {
		t.Fatal(err)
	}
	return definition.Name
}
