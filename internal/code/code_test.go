package code

import (
	"bytes"
	"strings"
	"testing"
)

// 测试字节码指令编码
func TestMake(t *testing.T) {
	tests := []struct {
		name     string
		opcode   Opcode
		operands []int
		want     Instructions
	}{
		{
			name:     "常量索引 0",
			opcode:   OpConstant,
			operands: []int{0},
			want:     Instructions{1, 0, 0},
		},
		{
			name:     "常量索引 258",
			opcode:   OpConstant,
			operands: []int{258},
			want:     Instructions{1, 1, 2},
		},
		{
			name:   "加法指令",
			opcode: OpAdd,
			want:   Instructions{2},
		},
		{
			name:   "弹栈指令",
			opcode: OpPop,
			want:   Instructions{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Make(tt.opcode, tt.operands...)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got, tt.want) {
				t.Fatalf(
					"got %v, want %v",
					got,
					tt.want,
				)
			}
		})
	}
}

// 测试非法字节码参数
func TestMakeErrors(t *testing.T) {
	tests := []struct {
		name     string
		opcode   Opcode
		operands []int
		wantErr  string
	}{
		{
			name:    "未知操作码",
			opcode:  Opcode(255),
			wantErr: "unknown opcode",
		},
		{
			name:    "缺少操作数",
			opcode:  OpConstant,
			wantErr: "expects 1 operands",
		},
		{
			name:     "多余操作数",
			opcode:   OpAdd,
			operands: []int{0},
			wantErr:  "expects 0 operands",
		},
		{
			name:     "负数索引",
			opcode:   OpConstant,
			operands: []int{-1},
			wantErr:  "out of range",
		},
		{
			name:     "常量索引溢出",
			opcode:   OpConstant,
			operands: []int{65536},
			wantErr:  "out of range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Make(tt.opcode, tt.operands...)

			if err == nil ||
				!strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf(
					"expected %q, got %v",
					tt.wantErr,
					err,
				)
			}
		})
	}
}

// 测试字节码操作数解码
func TestReadOperands(t *testing.T) {
	tests := []struct {
		name     string
		opcode   Opcode
		operands []int
		wantRead int
	}{
		{
			name:     "常量索引 0",
			opcode:   OpConstant,
			operands: []int{0},
			wantRead: 2,
		},
		{
			name:     "常量索引 258",
			opcode:   OpConstant,
			operands: []int{258},
			wantRead: 2,
		},
		{
			name:     "常量索引最大值",
			opcode:   OpConstant,
			operands: []int{65535},
			wantRead: 2,
		},
		{
			name:     "加法指令",
			opcode:   OpAdd,
			wantRead: 0,
		},
		{
			name:     "弹栈指令",
			opcode:   OpPop,
			wantRead: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 先编码指令
			ins, err := Make(tt.opcode, tt.operands...)
			if err != nil {
				t.Fatal(err)
			}

			// 查询指令定义
			def, err := Lookup(tt.opcode)
			if err != nil {
				t.Fatal(err)
			}

			// 跳过 Opcode，只解码操作数
			got, n, err := ReadOperands(def, ins[1:])
			if err != nil {
				t.Fatal(err)
			}

			if n != tt.wantRead {
				t.Fatalf(
					"read %d bytes, want %d",
					n,
					tt.wantRead,
				)
			}

			if len(got) != len(tt.operands) {
				t.Fatalf(
					"got %d operands, want %d",
					len(got),
					len(tt.operands),
				)
			}

			for i, value := range got {
				if value != tt.operands[i] {
					t.Fatalf(
						"operand[%d]: got %d, want %d",
						i,
						value,
						tt.operands[i],
					)
				}
			}
		})
	}
}

// 测试操作数解码复用调用方缓冲区
func TestReadOperandsInto(t *testing.T) {
	def, err := Lookup(OpEnum)
	if err != nil {
		t.Fatal(err)
	}
	ins, err := Make(OpEnum, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	var operands [2]int
	read, err := ReadOperandsInto(def, ins[1:], operands[:])
	if err != nil {
		t.Fatal(err)
	}
	if read != 4 || operands != [2]int{7, 11} {
		t.Fatalf("read=%d operands=%v", read, operands)
	}
}

// 测试跳转指令的编码与解码
func TestJumpInstructions(t *testing.T) {
	tests := []struct {
		name   string
		op     Opcode
		target int
	}{
		{"无条件跳转", OpJump, 258},
		{"条件跳转", OpJumpIfFalse, 1024},
		{"零地址", OpJump, 0},
		{"最大地址", OpJumpIfFalse, 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 编码
			ins, err := Make(tt.op, tt.target)
			if err != nil {
				t.Fatal(err)
			}

			if len(ins) != 3 {
				t.Fatalf("got %d bytes, want 3", len(ins))
			}

			// 解码
			def, err := Lookup(tt.op)
			if err != nil {
				t.Fatal(err)
			}

			operands, read, err := ReadOperands(def, ins[1:])
			if err != nil {
				t.Fatal(err)
			}

			if read != 2 ||
				len(operands) != 1 ||
				operands[0] != tt.target {
				t.Fatalf(
					"incorrect jump operands: %v, read %d",
					operands,
					read,
				)
			}
		})
	}
}
