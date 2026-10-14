package mochi

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
	"Mio/internal/vm"
)

// 测试混合常量、全局槽位与指令的往返编码
func TestRoundTripMixedConstantsGlobalsAndInstructions(t *testing.T) {
	instructions := joinInstructions(t,
		mustInstruction(t, code.OpConstant, 0),
		mustInstruction(t, code.OpSetGlobal, 0),
		mustInstruction(t, code.OpGetGlobal, 0),
		mustInstruction(t, code.OpChant),
		mustInstruction(t, code.OpNil),
		mustInstruction(t, code.OpPop),
	)
	want := &compiler.Bytecode{
		Constants: []value.Value{
			value.NewInt(-9223372036854775807),
			value.NewString("mochi"),
			value.NewString("こんにちは、世界 🐾"),
			value.NewInt(42),
		},
		GlobalsCount: 3,
		Instructions: instructions,
	}

	got, encoded := roundTrip(t, want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded bytecode:\n got %#v\nwant %#v", got, want)
	}

	// 解码结果不能与原始字节码或文件数据共享可变切片
	want.Instructions[0] = byte(code.OpTrue)
	want.Constants[0] = value.NewInt(0)
	encoded[len(encoded)-1] = byte(code.OpTrue)
	if got.Instructions[0] != byte(code.OpConstant) || got.Instructions[len(got.Instructions)-1] != byte(code.OpPop) {
		t.Fatal("decoded instructions share mutable storage")
	}
	if got.Constants[0] != value.NewInt(-9223372036854775807) {
		t.Fatal("decoded constants share mutable slice storage")
	}
}

// 测试 Mochi v1 的精确布局仍可解码
func TestDecodeExactV1Encoding(t *testing.T) {
	data := []byte{
		'M', 'O', 'C', 'H', 'I',
		0, 1,
		0, 0, 0, 2,
		constantTagInt,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe,
		constantTagString,
		0, 0, 0, 3,
		0xe7, 0x8c, 0xab,
		0, 0, 0, 1,
		0, 0, 0, 4,
		byte(code.OpConstant), 0, 0, byte(code.OpPop),
	}
	got, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got.GlobalsCount != 1 || len(got.Constants) != 2 || len(got.Functions) != 0 {
		t.Fatalf("decoded v1 bytecode: %#v", got)
	}
	if result := runResult(t, got); result != value.NewInt(-2) {
		t.Fatalf("v1 result = %v, want -2", result)
	}
}

// 测试条件跳转指令的往返编码
func TestRoundTripConditionalJumps(t *testing.T) {
	instructions := joinInstructions(t,
		mustInstruction(t, code.OpTrue),            // 字节偏移 0
		mustInstruction(t, code.OpJumpIfFalse, 11), // 字节偏移 1
		mustInstruction(t, code.OpConstant, 0),     // 字节偏移 4
		mustInstruction(t, code.OpChant),           // 字节偏移 7
		mustInstruction(t, code.OpJump, 15),        // 字节偏移 8
		mustInstruction(t, code.OpNil),             // 字节偏移 11
		mustInstruction(t, code.OpChant),           // 字节偏移 12
		mustInstruction(t, code.OpNil),             // 字节偏移 13
		mustInstruction(t, code.OpPop),             // 字节偏移 14
	)
	bytecode := &compiler.Bytecode{
		Constants:    []value.Value{value.NewString("hai")},
		Instructions: instructions,
	}

	decoded, _ := roundTrip(t, bytecode)
	if !bytes.Equal(decoded.Instructions, instructions) {
		t.Fatalf("instructions changed: got %v, want %v", decoded.Instructions, instructions)
	}
}

// 测试空程序的往返编码
func TestRoundTripEmptyProgram(t *testing.T) {
	want := &compiler.Bytecode{}
	got, encoded := roundTrip(t, want)
	if len(got.Constants) != 0 || len(got.Instructions) != 0 || got.GlobalsCount != 0 {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if len(encoded) != 23 {
		t.Fatalf("empty file length = %d, want 23", len(encoded))
	}
}

// 测试编码结果的确定性
func TestEncodingIsDeterministic(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Constants:    []value.Value{value.NewString("同じ"), value.NewInt(7)},
		GlobalsCount: 2,
		Instructions: joinInstructions(t, mustInstruction(t, code.OpConstant, 1), mustInstruction(t, code.OpPop)),
	}

	var first, second bytes.Buffer
	if err := Encode(&first, bytecode); err != nil {
		t.Fatal(err)
	}
	if err := Encode(&second, bytecode); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("identical bytecode produced different file bytes")
	}
}

// 测试解码前后的执行行为一致
func TestDecodedBehaviorMatchesOriginal(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(40), value.NewInt(2)},
		Instructions: joinInstructions(t,
			mustInstruction(t, code.OpConstant, 0),
			mustInstruction(t, code.OpConstant, 1),
			mustInstruction(t, code.OpAdd),
			mustInstruction(t, code.OpPop),
		),
	}
	decoded, _ := roundTrip(t, bytecode)

	originalResult := runResult(t, bytecode)
	decodedResult := runResult(t, decoded)
	if originalResult != decodedResult || decodedResult != value.NewInt(42) {
		t.Fatalf("original result %v, decoded result %v", originalResult, decodedResult)
	}
}

// 测试 Mochi v2 函数表往返编码
func TestRoundTripV2Functions(t *testing.T) {
	bytecode := &compiler.Bytecode{
		Constants: []value.Value{value.NewInt(42)},
		Instructions: joinInstructions(t,
			mustInstruction(t, code.OpCall, 0),
			mustInstruction(t, code.OpPop),
		),
		Functions: []compiler.CompiledFunction{{
			ID:             0,
			Name:           "answer",
			Instructions:   joinInstructions(t, mustInstruction(t, code.OpConstant, 0), mustInstruction(t, code.OpReturnValue)),
			ParameterCount: 0,
			LocalCount:     0,
			ReturnsValue:   true,
		}},
	}

	got, encoded := roundTrip(t, bytecode)
	if binary.BigEndian.Uint16(encoded[5:7]) != VersionV2 {
		t.Fatalf("encoded version = %d, want %d", binary.BigEndian.Uint16(encoded[5:7]), VersionV2)
	}
	if !reflect.DeepEqual(got, bytecode) {
		t.Fatalf("decoded bytecode:\n got %#v\nwant %#v", got, bytecode)
	}
	if result := runResult(t, got); result != value.NewInt(42) {
		t.Fatalf("result = %v, want 42", result)
	}
}

// 测试解码拒绝非法文件
func TestDecodeRejectsMalformedFiles(t *testing.T) {
	valid := encodeForTest(t, &compiler.Bytecode{})

	withVersion := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(withVersion[5:7], Version+1)

	withTrailing := append(append([]byte(nil), valid...), 0)

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "bad signature", data: append([]byte("WRONG"), valid[5:]...), want: "signature"},
		{name: "unsupported version", data: withVersion, want: "unsupported format version"},
		{name: "invalid tag", data: rawFile(1, []byte{0xff}, 0, nil), want: "invalid type tag"},
		{name: "oversized constant count", data: headerWithCount(MaxConstants + 1), want: "constant count"},
		{name: "oversized string", data: rawFile(1, append([]byte{constantTagString}, u32(MaxStringBytes+1)...), 0, nil), want: "string length"},
		{name: "truncated string", data: truncatedStringFile(4, []byte{'a'}), want: "string data"},
		{name: "invalid UTF-8", data: rawFile(1, append(append([]byte{constantTagString}, u32(1)...), 0xff), 0, nil), want: "invalid UTF-8"},
		{name: "oversized globals", data: rawFile(0, nil, MaxGlobals+1, nil), want: "global slot count"},
		{name: "oversized instructions", data: rawFileWithInstructionLength(MaxInstructionBytes + 1), want: "instruction length"},
		{name: "unknown opcode", data: rawFile(0, nil, 0, []byte{0xff}), want: "unknown opcode"},
		{name: "truncated operand", data: rawFile(0, nil, 0, []byte{byte(code.OpConstant), 0}), want: "truncated operands"},
		{name: "constant index", data: rawFile(0, nil, 0, []byte{byte(code.OpConstant), 0, 0}), want: "constant index"},
		{name: "set global index", data: rawFile(0, nil, 0, []byte{byte(code.OpSetGlobal), 0, 0}), want: "global slot index"},
		{name: "get global index", data: rawFile(0, nil, 0, []byte{byte(code.OpGetGlobal), 0, 0}), want: "global slot index"},
		{name: "jump out of range", data: rawFile(0, nil, 0, []byte{byte(code.OpJump), 0, 4}), want: "jump target"},
		{name: "jump into operand", data: rawFile(0, nil, 0, []byte{byte(code.OpJump), 0, 1}), want: "instruction boundary"},
		{name: "oversized parameter count", data: rawV2Function(MaxFunctionSlots+1, MaxFunctionSlots+1), want: "parameter count"},
		{name: "locals below parameters", data: rawV2Function(1, 0), want: "local count"},
		{name: "oversized local count", data: rawV2Function(0, MaxFunctionSlots+1), want: "local count"},
		{name: "trailing data", data: withTrailing, want: "trailing file data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(bytes.NewReader(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Decode() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// 测试解码拒绝所有位置的文件截断
func TestDecodeRejectsEveryTruncation(t *testing.T) {
	complete := encodeForTest(t, &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(3), value.NewString("猫")},
		Instructions: mustInstruction(t, code.OpConstant, 0),
	})

	for length := 0; length < len(complete); length++ {
		if _, err := Decode(bytes.NewReader(complete[:length])); err == nil {
			t.Fatalf("Decode accepted truncation at byte %d of %d", length, len(complete))
		}
	}
}

// 测试编码拒绝不支持的常量与非法结构
func TestEncodeRejectsUnsupportedConstantsAndInvalidStructure(t *testing.T) {
	tests := []struct {
		name     string
		bytecode *compiler.Bytecode
		want     string
	}{
		{name: "nil bytecode", bytecode: nil, want: "nil bytecode"},
		{name: "bool constant", bytecode: &compiler.Bytecode{Constants: []value.Value{value.NewBool(true)}}, want: "unsupported kind"},
		{name: "nil constant", bytecode: &compiler.Bytecode{Constants: []value.Value{value.NewNil()}}, want: "unsupported kind"},
		{name: "negative globals", bytecode: &compiler.Bytecode{GlobalsCount: -1}, want: "global slot count"},
		{name: "invalid opcode", bytecode: &compiler.Bytecode{Instructions: []byte{0xff}}, want: "unknown opcode"},
		{name: "call function index", bytecode: &compiler.Bytecode{Instructions: mustInstruction(t, code.OpCall, 0)}, want: "FunctionID"},
		{name: "local outside function", bytecode: &compiler.Bytecode{Instructions: mustInstruction(t, code.OpGetLocal, 0)}, want: "outside function"},
		{name: "non-canonical function ID", bytecode: &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 1, Name: "bad"}}}, want: "FunctionID"},
		{name: "parameter count exceeds locals", bytecode: &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 0, Name: "bad", ParameterCount: 1}}}, want: "local count"},
		{name: "unit return value", bytecode: &compiler.Bytecode{Functions: []compiler.CompiledFunction{{ID: 0, Name: "bad", Instructions: mustInstruction(t, code.OpReturnValue)}}}, want: "unit function"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buffer bytes.Buffer
			err := Encode(&buffer, tt.bytecode)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Encode() error = %v, want containing %q", err, tt.want)
			}
			if buffer.Len() != 0 {
				t.Fatalf("Encode wrote %d bytes before validation failed", buffer.Len())
			}
		})
	}
}

func roundTrip(t *testing.T, bytecode *compiler.Bytecode) (*compiler.Bytecode, []byte) {
	t.Helper()
	encoded := encodeForTest(t, bytecode)
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	return decoded, encoded
}

func encodeForTest(t *testing.T, bytecode *compiler.Bytecode) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := Encode(&buffer, bytecode); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), buffer.Bytes()...)
}

func mustInstruction(t *testing.T, op code.Opcode, operands ...int) code.Instructions {
	t.Helper()
	instruction, err := code.Make(op, operands...)
	if err != nil {
		t.Fatal(err)
	}
	return instruction
}

func joinInstructions(t *testing.T, instructions ...code.Instructions) code.Instructions {
	t.Helper()
	var joined code.Instructions
	for _, instruction := range instructions {
		joined = append(joined, instruction...)
	}
	return joined
}

func runResult(t *testing.T, bytecode *compiler.Bytecode) value.Value {
	t.Helper()
	machine := vm.New(bytecode)
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	result, ok := machine.LastPoppedValue()
	if !ok {
		t.Fatal("VM produced no result")
	}
	return result
}

func rawFile(constantCount uint32, constantData []byte, globals uint32, instructions []byte) []byte {
	data := append([]byte(nil), fileSignature...)
	data = append(data, 0, byte(VersionV1))
	data = append(data, u32(constantCount)...)
	data = append(data, constantData...)
	data = append(data, u32(globals)...)
	data = append(data, u32(uint32(len(instructions)))...)
	data = append(data, instructions...)
	return data
}

func headerWithCount(count uint32) []byte {
	data := append([]byte(nil), fileSignature...)
	data = append(data, 0, byte(VersionV1))
	return append(data, u32(count)...)
}

func rawFileWithInstructionLength(length uint32) []byte {
	data := rawFile(0, nil, 0, nil)
	binary.BigEndian.PutUint32(data[len(data)-4:], length)
	return data
}

func truncatedStringFile(length uint32, partial []byte) []byte {
	data := append([]byte(nil), fileSignature...)
	data = append(data, 0, byte(VersionV1))
	data = append(data, u32(1)...)
	data = append(data, constantTagString)
	data = append(data, u32(length)...)
	return append(data, partial...)
}

func u32(value uint32) []byte {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], value)
	return data[:]
}

func rawV2Function(parameterCount, localCount uint32) []byte {
	data := append([]byte(nil), fileSignature...)
	data = append(data, 0, byte(Version))
	data = append(data, u32(0)...)
	data = append(data, u32(0)...)
	data = append(data, u32(0)...)
	data = append(data, u32(1)...)
	data = append(data, 0, 0)
	data = append(data, 0, 1, 'f')
	data = append(data, u32(parameterCount)...)
	data = append(data, u32(localCount)...)
	data = append(data, 0)
	data = append(data, u32(1)...)
	return append(data, byte(code.OpReturn))
}

// 模糊测试 Mochi 解码不会接受不一致结构或发生崩溃
func FuzzDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add(rawFile(0, nil, 0, nil))
	f.Add(rawV2Function(0, 0))
	var v8 bytes.Buffer
	if err := Encode(&v8, &compiler.Bytecode{Instructions: mustInstructionForFuzz(code.OpMap)}); err != nil {
		f.Fatal(err)
	}
	f.Add(v8.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		bytecode, err := Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		if err := Validate(bytecode); err != nil {
			t.Fatalf("Decode returned invalid bytecode: %v", err)
		}
	})
}

func mustInstructionForFuzz(op code.Opcode) code.Instructions {
	instructions, err := code.Make(op)
	if err != nil {
		panic(err)
	}
	return instructions
}
