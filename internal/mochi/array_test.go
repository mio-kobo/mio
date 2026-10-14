package mochi

import (
	"encoding/binary"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
)

// 测试数组指令要求 Mochi v5
func TestArrayInstructionsRequireMochiV5(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Instructions: joinInstructions(t,
			mustInstruction(t, code.OpArray, 0),
			mustInstruction(t, code.OpPop),
		),
	}
	if err := validateVersion(bytecode, VersionV4); err == nil || !strings.Contains(err.Error(), "requires Mochi v5") {
		t.Fatalf("error=%v", err)
	}
	if err := validateVersion(bytecode, VersionV5); err != nil {
		t.Fatal(err)
	}
	_, encoded := roundTrip(t, bytecode)
	if encoded[5] != 0 || encoded[6] != byte(VersionV5) {
		t.Fatalf("version bytes=%v", encoded[5:7])
	}
}

// 测试数组长度与赋值指令要求 Mochi v6
func TestArrayMutationInstructionsRequireMochiV6(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Instructions: append(
			mustInstruction(t, code.OpArray, 0),
			mustInstruction(t, code.OpLen)...,
		),
	}
	if err := validateVersion(bytecode, VersionV5); err == nil || !strings.Contains(err.Error(), "requires Mochi v6") {
		t.Fatalf("error=%v", err)
	}
	_, encoded := roundTrip(t, bytecode)
	if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV6 {
		t.Fatalf("version=%d, want %d", version, VersionV6)
	}
}
