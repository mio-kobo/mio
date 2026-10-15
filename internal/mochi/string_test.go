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

// 测试数据复制转换使用 Mochi v14
func TestDataConvertUsesMochiV14(t *testing.T) {
	constant, _ := code.Make(code.OpConstant, 0)
	convert, _ := code.Make(code.OpDataConvert, code.DataStringToBytes)
	pop, _ := code.Make(code.OpPop)
	instructions := append(constant, convert...)
	instructions = append(instructions, pop...)
	bytecode := &compiler.Bytecode{Instructions: instructions, Constants: []value.Value{value.NewString("Mio")}}
	if err := validateVersion(bytecode, VersionV13); err == nil || !strings.Contains(err.Error(), "requires Mochi v14") {
		t.Fatalf("error=%v", err)
	}
	if err := validateVersion(bytecode, VersionV14); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := Encode(&encoded, bytecode); err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(encoded.Bytes()[5:7]); version != VersionV14 {
		t.Fatalf("version=%d", version)
	}
}

// 测试数据复制转换操作数校验
func TestDataConvertRejectsUnknownOperation(t *testing.T) {
	convert, _ := code.Make(code.OpDataConvert, 255)
	bytecode := &compiler.Bytecode{Instructions: convert}
	if err := validateVersion(bytecode, VersionV14); err == nil || !strings.Contains(err.Error(), "unknown data conversion") {
		t.Fatalf("error=%v", err)
	}
}
