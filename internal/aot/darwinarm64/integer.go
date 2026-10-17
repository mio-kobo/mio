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
	boolCalls  []int
	fixups     []branchFixup
	branching  bool
	globals    int
}

func NewIntegerEmitter() *IntegerEmitter { return &IntegerEmitter{} }

// EnableControlFlow 为分支执行建立独立的最近表达式输出状态。
// x20 表示是否曾经执行 OpPop；x21 记录最后一次 Pop 的值类型。
func (e *IntegerEmitter) EnableControlFlow() error {
	if len(e.words) != 0 || e.depth != 0 {
		return fmt.Errorf("AOT M3 control flow must be enabled before emission")
	}
	e.branching = true
	e.words = append(e.words, movzX(20, 0))
	return nil
}

// SetOperandDepth 在每条 Mochi 指令前恢复数据流分析的抽象栈深度。
// 分支汇合处只允许相同栈布局，不从此前线性代码继承栈深度。
func (e *IntegerEmitter) SetOperandDepth(depth int) error {
	if depth < 0 || depth > 7 {
		return fmt.Errorf("AOT M3 invalid operand depth %d", depth)
	}
	e.depth = depth
	return nil
}

// CodeOffset 返回当前 ARM64 指令序列的字索引，用于映射 Mochi 跳转目标。
func (e *IntegerEmitter) CodeOffset() int { return len(e.words) }

// PatchControlFlow 统一回填所有 PC 相对跳转，不修改源 Mochi 字节码。
func (e *IntegerEmitter) PatchControlFlow(offsetWords map[int]int) error {
	for _, fix := range e.fixups {
		target, ok := offsetWords[fix.targetByte]
		if !ok {
			return fmt.Errorf("AOT M3 missing native target for Mochi byte offset %d", fix.targetByte)
		}
		if fix.conditional {
			encoded, err := checkedCBZ(fix.register, fix.atWord, target)
			if err != nil {
				return err
			}
			e.words[fix.atWord] = encoded
		} else {
			encoded, err := branch26(0x14000000, fix.atWord, target)
			if err != nil {
				return err
			}
			e.words[fix.atWord] = encoded
		}
	}
	return nil
}

type branchFixup struct {
	atWord      int
	targetByte  int
	register    uint32
	conditional bool
}

// Jump / JumpIfFalse 接受的是当前 Mochi 指令块内的绝对字节偏移。
func (e *IntegerEmitter) Jump(target int) error {
	if !e.branching {
		return fmt.Errorf("AOT M3 branching mode not enabled")
	}
	e.fixups = append(e.fixups, branchFixup{atWord: len(e.words), targetByte: target})
	e.words = append(e.words, 0)
	return nil
}
func (e *IntegerEmitter) JumpIfFalse(target int) error {
	if !e.branching {
		return fmt.Errorf("AOT M3 branching mode not enabled")
	}
	if e.depth == 0 {
		return fmt.Errorf("AOT M3 stack underflow at OpJumpIfFalse")
	}
	reg := uint32(9 + e.depth - 1)
	e.fixups = append(e.fixups, branchFixup{atWord: len(e.words), targetByte: target, register: reg, conditional: true})
	e.words = append(e.words, 0)
	e.depth--
	return nil
}

func checkedCBZ(reg uint32, from, to int) (uint32, error) {
	diff := to - from
	if diff < -(1<<18) || diff >= 1<<18 {
		return 0, fmt.Errorf("AOT M3 conditional branch target out of range")
	}
	return 0xB4000000 | (uint32(int32(diff))&0x7ffff)<<5 | reg, nil
}

func (e *IntegerEmitter) PushBool(v bool) error {
	if e.depth == 7 {
		return fmt.Errorf("AOT M3 operand stack limit exceeded")
	}
	n := uint16(0)
	if v {
		n = 1
	}
	e.words = append(e.words, movzX(uint32(9+e.depth), n))
	e.depth++
	return nil
}
func (e *IntegerEmitter) NotBool() error {
	if e.depth < 1 {
		return fmt.Errorf("AOT M3 stack underflow at OpNot")
	}
	r := uint32(9 + e.depth - 1)
	e.words = append(e.words, cmpZeroX(r), csetW(r, 0))
	return nil
}

// Compare 根据符号比较条件生成 CMP + CSET，产出规范化的 0 或 1。
// 布尔相等比较也使用 CMP，顺序关系只允许 int64。
func (e *IntegerEmitter) Compare(cond uint32) error {
	if e.depth < 2 {
		return fmt.Errorf("AOT M3 stack underflow at comparison")
	}
	left := uint32(9 + e.depth - 2)
	right := uint32(9 + e.depth - 1)
	e.words = append(e.words, cmpX(left, right), csetW(left, cond))
	e.depth--
	return nil
}

// ChantBool 输出 Mio 布尔文本 hai 或 iie，且不产生表达式结果。
func (e *IntegerEmitter) ChantBool() error {
	if e.depth == 0 {
		return fmt.Errorf("AOT M3 stack underflow at OpChant")
	}
	e.words = append(e.words, movX(0, uint32(9+e.depth-1)))
	e.boolCalls = append(e.boolCalls, len(e.words))
	e.words = append(e.words, 0)
	e.depth--
	return nil
}

// PopTyped 让最后一个表达式的输出类型在运行时确定。
// 条件分支未执行 Pop 时，不会意外打印此前留在寄存器中的值。
func (e *IntegerEmitter) PopTyped(isBool bool) error {
	if !e.branching {
		return fmt.Errorf("AOT M3 branching mode not enabled")
	}
	if e.depth == 0 {
		return fmt.Errorf("AOT M3 stack underflow at OpPop")
	}
	e.words = append(e.words, movX(19, uint32(9+e.depth-1)), movzX(20, 1))
	kind := uint16(0)
	if isBool {
		kind = 1
	}
	e.words = append(e.words, movzX(21, kind))
	e.lastPopped = true
	e.depth--
	return nil
}

// CMP 的条件码：EQ=0, NE=1, GE=10, LT=11, GT=12, LE=13。
func cmpX(left, right uint32) uint32 { return 0xEB00001F | right<<16 | left<<5 }
func cmpZeroX(reg uint32) uint32     { return 0xF100001F | reg<<5 }
func csetW(rd, condition uint32) uint32 {
	return 0x1A9F07E0 | (condition^1)<<12 | rd
}

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
		return nil, fmt.Errorf("AOT M3 unbalanced operand stack: %d remaining", e.depth)
	}
	code := append([]uint32(nil), e.words...)
	calls := append([]int(nil), e.printCalls...)
	boolCalls := append([]int(nil), e.boolCalls...)
	if e.branching {
		if e.lastPopped {
			jumpEmpty := len(code)
			code = append(code, 0) // CBZ x20 -> exit
			jumpInt := len(code)
			code = append(code, 0) // CBZ x21 -> print integer
			code = append(code, movX(0, 19))
			boolCalls = append(boolCalls, len(code))
			code = append(code, 0) // BL printBool
			jumpExit := len(code)
			code = append(code, 0)
			intStart := len(code)
			code = append(code, movX(0, 19))
			calls = append(calls, len(code))
			code = append(code, 0) // BL printInt
			exitStart := len(code)
			var err error
			code[jumpEmpty], err = checkedCBZ(20, jumpEmpty, exitStart)
			if err != nil {
				return nil, err
			}
			code[jumpInt], err = checkedCBZ(21, jumpInt, intStart)
			if err != nil {
				return nil, err
			}
			code[jumpExit], err = branch26(0x14000000, jumpExit, exitStart)
			if err != nil {
				return nil, err
			}
		}
	} else if e.lastPopped {
		code = append(code, movX(0, 19))
		calls = append(calls, len(code))
		code = append(code, 0)
	}
	// 正常结束时总是返回 0；程序打印值与退出状态互不混淆。
	code = append(code, movzX(0, 0), movzX(16, 1), svc(0x80))
	if len(calls) > 0 {
		start := len(code)
		code = appendPrintInt64(code)
		for _, call := range calls {
			branch, err := branch26(0x94000000, call, start)
			if err != nil {
				return nil, err
			}
			code[call] = branch
		}
	}
	if len(boolCalls) > 0 {
		start := len(code)
		code = appendPrintBool(code)
		for _, call := range boolCalls {
			branch, err := branch26(0x94000000, call, start)
			if err != nil {
				return nil, err
			}
			code[call] = branch
		}
	}
	return ExecutableWordsWithGlobals(code, e.globals)
}

// printBool 在栈上写入四个 ASCII 字节，通过原生 SYS_write 输出。
func appendPrintBool(dst []uint32) []uint32 {
	const sp = uint32(31)
	dst = append(dst, subImmX(sp, sp, 16), movzX(2, 4))
	jumpFalse := len(dst)
	dst = append(dst, 0) // CBZ x0 -> iie
	for i, c := range []byte{'h', 'a', 'i', '\n'} {
		dst = append(dst, movzW(3, uint16(c)), strbOffsetW(3, sp, uint32(i)))
	}
	jumpWrite := len(dst)
	dst = append(dst, 0)
	falseStart := len(dst)
	for i, c := range []byte{'i', 'i', 'e', '\n'} {
		dst = append(dst, movzW(3, uint16(c)), strbOffsetW(3, sp, uint32(i)))
	}
	writeStart := len(dst)
	dst[jumpFalse] = cbzX(0, jumpFalse, falseStart)
	var err error
	dst[jumpWrite], err = branch26(0x14000000, jumpWrite, writeStart)
	if err != nil {
		panic("unreachable bool print branch span")
	}
	dst = append(dst, addImmX(1, sp, 0), movzX(0, 1), movzX(16, 4), svc(0x80), addImmX(sp, sp, 16), 0xD65F03C0)
	return dst
}

func strbOffsetW(rt, rn, byteOffset uint32) uint32 {
	return 0x39000000 | byteOffset<<10 | rn<<5 | rt
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
