package darwinarm64

import "fmt"

// IntegerEmitter 将少量 int64 栈指令直接翻译为 AArch64 机器码。
// M2 仍用 x9..x15 保存操作数栈，最多同时容纳 7 个 int64 值。
// x8 作为全局槽位寻址临时寄存器；x19 保存最后的顶层表达式结果。
type IntegerEmitter struct {
	words      []uint32
	depth      int
	lastPopped bool
	printCalls []int
	globals    int
}

func NewIntegerEmitter() *IntegerEmitter { return &IntegerEmitter{} }

// SetGlobalCount 设置静态 int64 全局槽位数量，当前最多占用一页 __DATA。
func (e *IntegerEmitter) SetGlobalCount(count int) error {
	if count < 0 || count > MaxGlobalSlots {
		return fmt.Errorf("AOT M2 global slot count %d exceeds limit %d", count, MaxGlobalSlots)
	}
	e.globals = count
	return nil
}

// SetGlobal 将操作数栈顶的 int64 写入可写 __DATA，并弹出这个值。
func (e *IntegerEmitter) SetGlobal(slot int) error {
	if err := e.checkGlobalSlot(slot); err != nil {
		return err
	}
	if e.depth < 1 {
		return fmt.Errorf("AOT M2 stack underflow at OpSetGlobal")
	}
	reg := uint32(9 + e.depth - 1)
	if err := e.emitGlobalAccess(reg, slot, true); err != nil {
		return err
	}
	e.depth--
	return nil
}

// GetGlobal 将 __DATA 中的 int64 读入操作数栈，而不是重新载入常量。
func (e *IntegerEmitter) GetGlobal(slot int) error {
	if err := e.checkGlobalSlot(slot); err != nil {
		return err
	}
	if e.depth == 7 {
		return fmt.Errorf("AOT M2 operand stack limit exceeded (max 7 int64 values)")
	}
	reg := uint32(9 + e.depth)
	if err := e.emitGlobalAccess(reg, slot, false); err != nil {
		return err
	}
	e.depth++
	return nil
}

func (e *IntegerEmitter) checkGlobalSlot(slot int) error {
	if slot < 0 || slot >= e.globals {
		return fmt.Errorf("AOT M2 global slot %d out of range (slots=%d)", slot, e.globals)
	}
	return nil
}

// emitGlobalAccess 使用 ADRP 计算 ASLR 下的页基址，再用 LDR/STR 访问槽位。
// ARM64 ADRP 始终以 4 KiB 为单位计算 PC 相对页，而 __DATA 是 16 KiB 的 macOS 页。
func (e *IntegerEmitter) emitGlobalAccess(reg uint32, slot int, store bool) error {
	const scratch = uint32(8)
	addr := textVMAddr + pageSize + uint64(slot)*8
	pc := textVMAddr + entryOffset + uint64(len(e.words))*4
	pageBase, err := adrpX(scratch, pc, addr)
	if err != nil {
		return err
	}
	imm12 := uint32((addr & 0xfff) / 8) // LDR/STR X 的偏移按 8 字节缩放。
	access := ldrX(reg, scratch, imm12)
	if store {
		access = strX(reg, scratch, imm12)
	}
	e.words = append(e.words, pageBase, access)
	return nil
}

func adrpX(rd uint32, pc, target uint64) (uint32, error) {
	const mask = uint64(0xfff)
	diffPages := (int64(target&^mask) - int64(pc&^mask)) / 4096
	if diffPages < -(1<<20) || diffPages >= 1<<20 {
		return 0, fmt.Errorf("AOT M2 ADRP target out of range")
	}
	imm := uint32(diffPages) & 0x1fffff
	return 0x90000000 | (imm&3)<<29 | ((imm>>2)&0x7ffff)<<5 | rd, nil
}

func ldrX(rt, rn, imm12 uint32) uint32 {
	return 0xF9400000 | imm12<<10 | rn<<5 | rt
}
func strX(rt, rn, imm12 uint32) uint32 {
	return 0xF9000000 | imm12<<10 | rn<<5 | rt
}

// PushInt64 使用 MOVZ/MOVK 装载完整的 64 位整数位模式，包括负数。
func (e *IntegerEmitter) PushInt64(n int64) error {
	if e.depth == 7 {
		return fmt.Errorf("AOT M1 operand stack limit exceeded (max 7 int64 values)")
	}
	reg := uint32(9 + e.depth)
	bits := uint64(n)
	e.words = append(e.words, movzX(reg, uint16(bits)))
	for shift := uint32(1); shift < 4; shift++ {
		e.words = append(e.words, movkX(reg, uint16(bits>>(16*shift)), shift))
	}
	e.depth++
	return nil
}

func (e *IntegerEmitter) Add() error { return e.binary("OpAdd", addX) }
func (e *IntegerEmitter) Sub() error { return e.binary("OpSub", subX) }
func (e *IntegerEmitter) Mul() error { return e.binary("OpMul", mulX) }

func (e *IntegerEmitter) binary(name string, encode func(uint32, uint32, uint32) uint32) error {
	if e.depth < 2 {
		return fmt.Errorf("AOT M1 stack underflow at %s", name)
	}
	left := uint32(9 + e.depth - 2)
	right := uint32(9 + e.depth - 1)
	e.words = append(e.words, encode(left, left, right))
	e.depth--
	return nil
}

func (e *IntegerEmitter) Neg() error {
	if e.depth < 1 {
		return fmt.Errorf("AOT M1 stack underflow at OpNeg")
	}
	reg := uint32(9 + e.depth - 1)
	e.words = append(e.words, subX(reg, 31, reg)) // NEG = SUB Xd, XZR, Xm
	return nil
}

// Chant 打印栈顶 int64，之后弹出该值。
func (e *IntegerEmitter) Chant() error {
	if e.depth < 1 {
		return fmt.Errorf("AOT M1 stack underflow at OpChant")
	}
	e.words = append(e.words, movX(0, uint32(9+e.depth-1)))
	e.printCalls = append(e.printCalls, len(e.words))
	e.words = append(e.words, 0) // BL: Finish 中回填到原生打印例程
	e.depth--
	return nil
}

// Pop 复现当前 Mochi VM 的顶层表达式语义：程序结束时打印最后一个值。
func (e *IntegerEmitter) Pop() error {
	if e.depth < 1 {
		return fmt.Errorf("AOT M1 stack underflow at OpPop")
	}
	e.words = append(e.words, movX(19, uint32(9+e.depth-1)))
	e.lastPopped = true
	e.depth--
	return nil
}

// Finish 拼接程序、退出系统调用及按需生成的 int64 打印例程。
func (e *IntegerEmitter) Finish() ([]byte, error) {
	if e.depth != 0 {
		return nil, fmt.Errorf("AOT M1 unbalanced operand stack: %d remaining", e.depth)
	}
	code := append([]uint32(nil), e.words...)
	calls := append([]int(nil), e.printCalls...)
	if e.lastPopped {
		code = append(code, movX(0, 19))
		calls = append(calls, len(code))
		code = append(code, 0)
	}
	// 正常的 Mio 原生可执行程序以 0 退出，计算结果不充当进程退出码。
	code = append(code, movzX(0, 0), movzX(16, 1), svc(0x80))

	if len(calls) > 0 {
		printStart := len(code)
		code = appendPrintInt64(code)
		for _, call := range calls {
			branch, err := branch26(0x94000000, call, printStart)
			if err != nil {
				return nil, err
			}
			code[call] = branch
		}
	}
	return ExecutableWordsWithGlobals(code, e.globals)
}

// printInt64 接收 x0 中的 int64，使用栈上 32 字节缓冲区构造十进制文本。
// 通过 Darwin SYS_write(1, buf, len) 输出后恢复 SP，并通过 RET 返回。
// 处理 0、负数和 MinInt64；绝对值始终使用无符号除法，避免溢出。
func appendPrintInt64(dst []uint32) []uint32 {
	const (
		sp = uint32(31)
		x0 = uint32(0)
		x1 = uint32(1)
		x2 = uint32(2)
		x3 = uint32(3)
		x4 = uint32(4)
		x5 = uint32(5)
		x6 = uint32(6)
	)
	// 在栈顶从后往前写数字，末尾先放换行符。
	dst = append(dst,
		subImmX(sp, sp, 32),
		addImmX(x1, sp, 31),
		movzW(x4, 10),
		strbW(x4, x1),
		movzX(x2, 1),
		movzX(x5, 0),
		cmpImmX(x0, 0),
	)
	jumpPositive := len(dst)
	dst = append(dst, 0) // B.GE
	dst = append(dst,
		movzX(x5, 1),
		subX(x0, sp, x0), // NEG X0 = SUB X0, XZR, X0（Rn=31）
	)
	positive := len(dst)
	dst[jumpPositive] = branchCond(jumpPositive, positive, 0xA) // GE

	dst = append(dst, movzX(x6, 10))
	digit := len(dst)
	dst = append(dst,
		udivX(x3, x0, x6),
		msubX(x4, x3, x6, x0),
		addImmW(x4, x4, 48),
		subImmX(x1, x1, 1),
		strbW(x4, x1),
		addImmX(x2, x2, 1),
		movX(x0, x3),
	)
	jumpDigit := len(dst)
	dst = append(dst, cbnzX(x0, jumpDigit, digit))

	jumpNoSign := len(dst)
	dst = append(dst, 0) // CBZ x5
	dst = append(dst,
		subImmX(x1, x1, 1),
		movzW(x4, '-'),
		strbW(x4, x1),
		addImmX(x2, x2, 1),
	)
	noSign := len(dst)
	dst[jumpNoSign] = cbzX(x5, jumpNoSign, noSign)
	dst = append(dst,
		movzX(x0, 1), // stdout
		movzX(16, 4), // Darwin SYS_write
		svc(0x80),
		addImmX(sp, sp, 32),
		0xD65F03C0, // RET
	)
	return dst
}

func movkX(rd uint32, imm uint16, hw uint32) uint32 {
	return 0xF2800000 | hw<<21 | uint32(imm)<<5 | rd
}
func movzW(rd uint32, imm uint16) uint32 { return 0x52800000 | uint32(imm)<<5 | rd }
func movX(rd, rm uint32) uint32          { return 0xAA0003E0 | rm<<16 | rd }
func addX(rd, rn, rm uint32) uint32      { return 0x8B000000 | rm<<16 | rn<<5 | rd }
func subX(rd, rn, rm uint32) uint32      { return 0xCB000000 | rm<<16 | rn<<5 | rd }
func mulX(rd, rn, rm uint32) uint32      { return 0x9B007C00 | rm<<16 | rn<<5 | rd }
func udivX(rd, rn, rm uint32) uint32     { return 0x9AC00800 | rm<<16 | rn<<5 | rd }
func msubX(rd, rn, rm, ra uint32) uint32 {
	return 0x9B008000 | rm<<16 | ra<<10 | rn<<5 | rd
}
func addImmX(rd, rn, imm uint32) uint32 { return 0x91000000 | imm<<10 | rn<<5 | rd }
func subImmX(rd, rn, imm uint32) uint32 { return 0xD1000000 | imm<<10 | rn<<5 | rd }
func addImmW(rd, rn, imm uint32) uint32 { return 0x11000000 | imm<<10 | rn<<5 | rd }
func cmpImmX(rn, imm uint32) uint32     { return 0xF100001F | imm<<10 | rn<<5 }
func strbW(rt, rn uint32) uint32        { return 0x39000000 | rn<<5 | rt }
func branchCond(from, to int, cond uint32) uint32 {
	return 0x54000000 | uint32(int32(to-from)&0x7ffff)<<5 | cond
}
func cbnzX(rt uint32, from, to int) uint32 {
	return 0xB5000000 | uint32(int32(to-from)&0x7ffff)<<5 | rt
}
func cbzX(rt uint32, from, to int) uint32 {
	return 0xB4000000 | uint32(int32(to-from)&0x7ffff)<<5 | rt
}
func branch26(base uint32, from, to int) (uint32, error) {
	delta := to - from
	if delta < -(1<<25) || delta >= 1<<25 {
		return 0, fmt.Errorf("AOT M1 branch target is out of range")
	}
	return base | uint32(int32(delta)&0x03ffffff), nil
}
