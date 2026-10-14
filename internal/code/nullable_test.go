package code

import "testing"

// 测试可空 Opcode 从旧编号之后追加
func TestNullableOpcodeNumbers(t *testing.T) {
	if OpIsNil != 34 || OpSetNullableIndex != 35 {
		t.Fatalf("nullable opcodes=%d,%d", OpIsNil, OpSetNullableIndex)
	}
	for _, op := range []Opcode{OpIsNil, OpSetNullableIndex} {
		instruction, err := Make(op)
		if err != nil {
			t.Fatal(err)
		}
		if len(instruction) != 1 || instruction[0] != byte(op) {
			t.Fatalf("opcode=%d instruction=%v", op, instruction)
		}
	}
}
