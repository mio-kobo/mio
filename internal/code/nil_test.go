package code

import "testing"

func TestOpcodeNumbersStable(t *testing.T) {
	ops := []Opcode{
		OpConstant, OpAdd, OpPop, OpSub, OpMul, OpDiv, OpNeg,
		OpTrue, OpFalse, OpNot, OpEqual, OpNotEqual, OpLess,
		OpLessEqual, OpGreater, OpGreaterEqual, OpJump, OpJumpIfFalse,
		OpSetGlobal, OpGetGlobal, OpNil, OpChant,
	}
	for i, op := range ops {
		if int(op) != i+1 {
			t.Fatalf("opcode at %d: got %d, want %d", i, op, i+1)
		}
	}
}

// 测试固定宽度整数 Opcode 只追加到末尾
func TestFixedIntegerOpcodeNumber(t *testing.T) {
	if OpIntegerConvert != 55 {
		t.Fatalf("OpIntegerConvert=%d, want 55", OpIntegerConvert)
	}
}

func TestNilInstruction(t *testing.T) {
	ins, err := Make(OpNil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 1 || ins[0] != 21 {
		t.Fatalf("unexpected encoding: %v", ins)
	}
	def, err := Lookup(OpNil)
	if err != nil {
		t.Fatal(err)
	}
	operands, read, err := ReadOperands(def, ins[1:])
	if err != nil || read != 0 || len(operands) != 0 {
		t.Fatalf("unexpected operands: %v, read=%d, err=%v", operands, read, err)
	}
	if got := ins.String(); got != "0000 OpNil\n" {
		t.Fatalf("unexpected disassembly: %q", got)
	}
	if _, err := Make(OpNil, 0); err == nil {
		t.Fatal("expected operand count error")
	}
}
