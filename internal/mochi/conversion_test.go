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

// 测试转换指令使用 Mochi v4 往返编码
func TestConversionInstructionsUseMochiV4(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(9007199254740993)},
		Instructions: joinInstructions(t,
			mustInstruction(t, code.OpConstant, 0),
			mustInstruction(t, code.OpIntToFloat),
			mustInstruction(t, code.OpFloatToInt),
			mustInstruction(t, code.OpPop),
		),
	}
	decoded, encoded := roundTrip(t, bytecode)
	if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV4 {
		t.Fatalf("version=%d, want %d", version, VersionV4)
	}
	if result := runResult(t, decoded); result != value.NewInt(9007199254740992) {
		t.Fatalf("result=%v, want 9007199254740992", result)
	}
}

// 测试 Mochi v1、v2 和 v3 拒绝转换指令
func TestLegacyVersionsRejectConversionInstructions(t *testing.T) {
	for _, version := range []uint16{VersionV1, VersionV2, VersionV3} {
		data := legacyConversionFile(version)
		_, err := Decode(bytes.NewReader(data))
		if err == nil || !strings.Contains(err.Error(), "requires Mochi v4") {
			t.Fatalf("version %d error=%v", version, err)
		}
	}
}

func legacyConversionFile(version uint16) []byte {
	data := append([]byte(nil), fileSignature...)
	var versionBytes [2]byte
	binary.BigEndian.PutUint16(versionBytes[:], version)
	data = append(data, versionBytes[:]...)
	data = append(data, u32(1)...)
	data = append(data, constantTagInt)
	data = append(data, make([]byte, 8)...)
	data = append(data, u32(0)...)
	instructions := []byte{byte(code.OpConstant), 0, 0, byte(code.OpIntToFloat)}
	data = append(data, u32(uint32(len(instructions)))...)
	data = append(data, instructions...)
	if version >= VersionV2 {
		data = append(data, u32(0)...)
	}
	return data
}
