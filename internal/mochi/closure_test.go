package mochi

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
)

// 测试 Mochi v9 往返保存闭包捕获元数据
func TestMochiV9ClosureMetadataRoundTrip(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Instructions: mustInstruction(t, code.OpCall, 0),
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "outer", LocalCount: 1, Instructions: joinInstructions(t, mustInstruction(t, code.OpClosure, 1), mustInstruction(t, code.OpPop), mustInstruction(t, code.OpReturn))},
			{ID: 1, Name: "closure", Captures: []compiler.Capture{{Local: true, Index: 0}}, Instructions: joinInstructions(t, mustInstruction(t, code.OpGetUpvalue, 0), mustInstruction(t, code.OpPop), mustInstruction(t, code.OpReturn))},
		},
	}
	decoded, encoded := roundTrip(t, bytecode)
	if version := binary.BigEndian.Uint16(encoded[5:7]); version != VersionV10 {
		t.Fatalf("version=%d, want %d", version, VersionV10)
	}
	if len(decoded.Functions) != 2 || len(decoded.Functions[1].Captures) != 1 || !decoded.Functions[1].Captures[0].Local || decoded.Functions[1].Captures[0].Index != 0 {
		t.Fatalf("decoded captures=%#v", decoded.Functions)
	}
}

// 测试 Mochi v8 旧格式仍可解码
func TestDecodeMochiV8(t *testing.T) {
	data := append([]byte(nil), fileSignature...)
	data = append(data, 0, byte(VersionV8))
	data = append(data, u32(0)...)
	data = append(data, u32(0)...)
	data = append(data, u32(1)...)
	data = append(data, byte(code.OpMap))
	data = append(data, u32(0)...)
	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Instructions) != 1 || decoded.Instructions[0] != byte(code.OpMap) {
		t.Fatalf("instructions=%v", decoded.Instructions)
	}
}

// 测试闭包元数据和捕获索引经过严格验证
func TestRejectInvalidClosureMetadata(t *testing.T) {
	tests := []struct {
		name     string
		bytecode *compiler.Bytecode
		want     string
	}{
		{
			name: "局部捕获越界",
			bytecode: &compiler.Bytecode{Instructions: mustInstruction(t, code.OpCall, 0), Functions: []compiler.CompiledFunction{
				{ID: 0, Name: "outer", Instructions: joinInstructions(t, mustInstruction(t, code.OpClosure, 1), mustInstruction(t, code.OpPop), mustInstruction(t, code.OpReturn))},
				{ID: 1, Name: "closure", Captures: []compiler.Capture{{Local: true, Index: 0}}, Instructions: mustInstruction(t, code.OpReturn)},
			}},
			want: "capture 0 index 0 out of range",
		},
		{
			name: "Upvalue 指令越界",
			bytecode: &compiler.Bytecode{Instructions: mustInstruction(t, code.OpCall, 0), Functions: []compiler.CompiledFunction{
				{ID: 0, Name: "bad", Instructions: joinInstructions(t, mustInstruction(t, code.OpGetUpvalue, 0), mustInstruction(t, code.OpReturn))},
			}},
			want: "upvalue index 0 out of range",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.bytecode)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want containing %q", err, tt.want)
			}
		})
	}
}

// 测试解码拒绝截断和非法的捕获描述
func TestDecodeRejectsMalformedCaptureMetadata(t *testing.T) {
	base := append([]byte(nil), fileSignature...)
	base = append(base, 0, byte(Version))
	base = append(base, u32(0)...)
	base = append(base, u32(0)...)
	base = append(base, u32(0)...)
	base = append(base, u32(1)...)
	base = append(base, 0, 0, 0, 1, 'f')
	base = append(base, u32(0)...)
	base = append(base, u32(0)...)
	base = append(base, 0)
	base = append(base, u32(1)...)

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "截断捕获来源", data: append([]byte(nil), base...), want: "capture 0 source"},
		{name: "非法捕获来源", data: append(append(append([]byte(nil), base...), 2), u32(0)...), want: "invalid source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(bytes.NewReader(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want containing %q", err, tt.want)
			}
		})
	}
}
