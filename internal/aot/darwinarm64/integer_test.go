package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestIntegerEmitterAddsAndPrints(t *testing.T) {
	e := NewIntegerEmitter()
	for _, n := range []int64{40, 2} {
		if err := e.PushInt64(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Add(); err != nil {
		t.Fatal(err)
	}
	if err := e.Chant(); err != nil {
		t.Fatal(err)
	}
	image, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	section := m.Section("__text")
	if section == nil || section.Size <= 12 {
		t.Fatal("missing or empty M1 text section")
	}
	// 4 MOV* + 4 MOV* + ADD、MOV、BL，后跟 M0 原生退出指令。
	want := []uint32{
		0xD2800509, 0xF2A00009, 0xF2C00009, 0xF2E00009,
		0xD280004A, 0xF2A0000A, 0xF2C0000A, 0xF2E0000A,
		0x8B0A0129, 0xAA0903E0, 0x94000004,
		0xD2800000, 0xD2800030, 0xD4001001,
	}
	for i, expected := range want {
		offset := int(entryOffset) + i*4
		actual := binary.LittleEndian.Uint32(image[offset : offset+4])
		if actual != expected {
			t.Fatalf("word %d: got %#08x want %#08x", i, actual, expected)
		}
	}
	// 确认十进制转换是运行时执行，而非编译器预计算后写死文本。
	// 数字循环中必须包含 UDIV / MSUB / CBNZ，且输出后恢复栈指针。
	code := make([]uint32, section.Size/4)
	for i := range code {
		code[i] = binary.LittleEndian.Uint32(image[int(entryOffset)+4*i : int(entryOffset)+4*i+4])
	}
	for _, wantOpcode := range []uint32{0x9AC60803, 0x9B068064, 0xB5FFFF20, 0x910083FF, 0xD65F03C0} {
		found := false
		for _, opcode := range code {
			if opcode == wantOpcode {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing print-helper instruction %#08x", wantOpcode)
		}
	}
}

func TestIntegerEmitterNegativeAndMinInt64(t *testing.T) {
	for _, n := range []int64{0, -1, -42, math.MinInt64, math.MaxInt64} {
		e := NewIntegerEmitter()
		if err := e.PushInt64(n); err != nil {
			t.Fatal(err)
		}
		if err := e.Chant(); err != nil {
			t.Fatal(err)
		}
		image, err := e.Finish()
		if err != nil {
			t.Fatalf("value=%d: %v", n, err)
		}
		bits := uint64(n)
		for shift := uint(0); shift < 4; shift++ {
			off := int(entryOffset) + int(shift)*4
			got := binary.LittleEndian.Uint32(image[off : off+4])
			imm := uint16(bits >> (shift * 16))
			var want uint32
			if shift == 0 {
				want = movzX(9, imm)
			} else {
				want = movkX(9, imm, uint32(shift))
			}
			if got != want {
				t.Fatalf("value=%d shift=%d: got %#x, want %#x", n, shift, got, want)
			}
		}
	}
}

func TestIntegerEmitterTopLevelPop(t *testing.T) {
	e := NewIntegerEmitter()
	if err := e.PushInt64(1); err != nil {
		t.Fatal(err)
	}
	if err := e.Pop(); err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(42); err != nil {
		t.Fatal(err)
	}
	if err := e.Pop(); err != nil {
		t.Fatal(err)
	}
	image, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	// Pop 使用 x19 保存最后一次表达式结果，仅在主程序结束前打印一次。
	if !e.lastPopped || !bytes.Contains(image, []byte{0xF3, 0x03, 0x09, 0xAA}) {
		t.Fatal("last popped value must be saved in x19")
	}
}

func TestIntegerEmitterRejectsInvalidStack(t *testing.T) {
	e := NewIntegerEmitter()
	if err := e.Add(); err == nil || !strings.Contains(err.Error(), "underflow") {
		t.Fatalf("expected Add underflow, got %v", err)
	}
	if err := e.Chant(); err == nil || !strings.Contains(err.Error(), "underflow") {
		t.Fatalf("expected Chant underflow, got %v", err)
	}
	for i := 0; i < 7; i++ {
		if err := e.PushInt64(int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.PushInt64(8); err == nil || !strings.Contains(err.Error(), "stack limit") {
		t.Fatalf("expected stack limit, got %v", err)
	}
	if _, err := e.Finish(); err == nil || !strings.Contains(err.Error(), "unbalanced") {
		t.Fatalf("expected unbalanced stack, got %v", err)
	}
}

func TestIntegerEmitterLargeProgramRejected(t *testing.T) {
	// 仅 M1 的一页 __TEXT 空间；不能越界写入下一段文件。
	tooMany := make([]uint32, int((pageSize-entryOffset)/4)+1)
	if _, err := ExecutableWords(tooMany); err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("expected oversized Mach-O error, got %v", err)
	}
}
