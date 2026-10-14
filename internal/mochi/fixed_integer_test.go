package mochi

import (
	"bytes"
	"encoding/binary"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试固定宽度整数使用 Mochi v11 往返
func TestFixedIntegerRoundTrip(t *testing.T) {
	constants := []value.Value{
		value.NewInt8(-128), value.NewInt16(-32768), value.NewInt32(-2147483648),
		value.NewUint8(255), value.NewUint16(65535), value.NewUint32(4294967295),
	}
	instructions, err := code.Make(code.OpIntegerConvert, code.IntegerUint8)
	if err != nil {
		t.Fatal(err)
	}
	bytecode := &compiler.Bytecode{Constants: constants, Instructions: instructions}
	var encoded bytes.Buffer
	if err := Encode(&encoded, bytecode); err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(encoded.Bytes()[5:7]); version != Version {
		t.Fatalf("version=%d, want %d", version, Version)
	}
	decoded, err := Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Constants) != len(constants) {
		t.Fatalf("constant count=%d", len(decoded.Constants))
	}
	for i := range constants {
		want, _ := constants[i].IntegerBits()
		got, ok := decoded.Constants[i].IntegerBits()
		if !ok || got != want || decoded.Constants[i].Kind() != constants[i].Kind() {
			t.Fatalf("constant[%d]=%s/%d, want %s/%d", i, decoded.Constants[i].Kind(), got, constants[i].Kind(), want)
		}
	}
}
