package mochi

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试 Mochi v3 保留 float64 的 IEEE 754 位模式
func TestFloatConstantsUseMochiV3(t *testing.T) {
	values := []float64{0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.Float64frombits(0x7ff8000000000042)}
	constants := make([]value.Value, len(values))
	for i, number := range values {
		constants[i] = value.NewFloat(number)
	}
	decoded, encoded := roundTrip(t, &compiler.Bytecode{Constants: constants})
	if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV3 {
		t.Fatalf("version=%d, want %d", version, VersionV3)
	}
	for i, want := range values {
		got, ok := decoded.Constants[i].Float64()
		if !ok || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("constant %d bits=%016x, want %016x", i, math.Float64bits(got), math.Float64bits(want))
		}
	}
}

// 测试旧常量仍生成 Mochi v2
func TestLegacyConstantsStillUseMochiV2(t *testing.T) {
	encoded := encodeForTest(t, &compiler.Bytecode{Constants: []value.Value{value.NewInt(1), value.NewString("猫")}})
	if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV2 {
		t.Fatalf("version=%d, want %d", version, VersionV2)
	}
}

// 测试 Mochi v1 和 v2 拒绝 v3 浮点标签
func TestLegacyVersionsRejectFloatTag(t *testing.T) {
	for _, version := range []uint16{VersionV1, VersionV2} {
		data := append([]byte(nil), fileSignature...)
		var versionBytes [2]byte
		binary.BigEndian.PutUint16(versionBytes[:], version)
		data = append(data, versionBytes[:]...)
		data = append(data, u32(1)...)
		data = append(data, constantTagFloat)
		var bits [8]byte
		binary.BigEndian.PutUint64(bits[:], math.Float64bits(1.5))
		data = append(data, bits[:]...)
		data = append(data, u32(0)...)
		data = append(data, u32(0)...)
		if version == VersionV2 {
			data = append(data, u32(0)...)
		}
		_, err := Decode(bytes.NewReader(data))
		if err == nil || !strings.Contains(err.Error(), "invalid type tag") {
			t.Fatalf("version %d error=%v", version, err)
		}
	}
}
