package darwinarm64

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// M7 以只读字符串 ID 作为运行时值，字符串数据始终保存在 __TEXT,__const。
// 相同 UTF-8 字节通过编译时驻留表分配同一个 ID，比较不会读取堆内存。
const m7StringKind = 5

// stringImage 建立按 8 字节索引的静态字符串表。
// 每项为 (相对 __const 起点的 uint32 字节偏移, uint32 字节长度)，后面紧跟数据。
func stringImage(strings []string) ([]byte, error) {
	if len(strings) == 0 {
		return nil, nil
	}
	if uint64(len(strings)) > pageSize/8 {
		return nil, fmt.Errorf("AOT M7 too many static strings")
	}
	size := len(strings) * 8
	for _, s := range strings {
		if !utf8.ValidString(s) {
			return nil, fmt.Errorf("AOT M7 static string contains invalid UTF-8")
		}
		if uint64(len(s)) > pageSize || uint64(size)+uint64(len(s)) > pageSize {
			return nil, fmt.Errorf("AOT M7 static string data exceeds __const limit")
		}
		size += len(s)
	}
	data := make([]byte, size)
	cursor := len(strings) * 8
	for i, s := range strings {
		binary.LittleEndian.PutUint32(data[i*8:], uint32(cursor))
		binary.LittleEndian.PutUint32(data[i*8+4:], uint32(len(s)))
		copy(data[cursor:], s)
		cursor += len(s)
	}
	return data, nil
}

// ChantString 从操作数栈取出一个静态字符串 ID，转交原生 SYS_write 例程。
func (e *StackEmitter) ChantString() error {
	if err := e.ensurePop("OpChant", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 0)
	e.stringCalls = append(e.stringCalls, len(e.words))
	e.words = append(e.words, 0) // BL 由 FinishWithFunctionsAndStrings 回填
	e.depth--
	return nil
}

// PopString 保留顶层最后一次求得的静态字符串，函数内部 Pop 仅丢弃值。
func (e *StackEmitter) PopString() error {
	if err := e.ensurePop("OpPop", 1); err != nil {
		return err
	}
	if !e.functionMode {
		e.loadOperand(e.depth-1, 19)
		e.words = append(e.words, movzX(20, 1), movzX(21, m7StringKind))
		e.lastPopped = true
		e.lastPopString = true
	}
	e.depth--
	return nil
}

// StringLen 返回静态字符串的 UTF-8 字节长度，与 VM 的 len(string) 一致。
func (e *StackEmitter) StringLen() error {
	if err := e.ensurePop("OpLen", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 0)
	e.stringLenCalls = append(e.stringLenCalls, len(e.words))
	e.words = append(e.words, 0)
	e.storeOperand(e.depth-1, 0)
	return nil
}

// StringToBytes clones dynamic string storage or copies a tagged static string
// from __const into a fresh compact byte array. Static IDs use (id<<1)|1;
// heap-backed strings are aligned pointers and therefore have low bit zero.
func (e *StackEmitter) StringToBytes() error {
	if err := e.ensurePop("OpDataConvert string-to-bytes", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, movzX(10, 1), m8And(10, 9, 10), cmpZeroX(10))
	dynamicBranch := len(e.words)
	e.words = append(e.words, 0)

	// Static path: preserve x26 and the tagged ID across the allocation helper.
	e.words = append(e.words, subImmX(31, 31, 16), strX(26, 31, 0), movX(26, 9))
	e.stringConstSites = append(e.stringConstSites, len(e.words))
	e.words = append(e.words, 0, 0) // ADRP/ADD x10,__const
	e.words = append(e.words, m10Lsr(9, 9, 1), m10Lsl(11, 9, 3), addX(11, 10, 11), ldrX(1, 11, 0))
	e.words = append(e.words, m10Lsr(1, 1, 32), movX(0, 23), movzX(3, 1), movzX(4, 0))
	e.m12Calls = append(e.m12Calls, m12CallSite{at: len(e.words), kind: m12ArrayWithCapacity})
	e.words = append(e.words, 0)
	e.words = append(e.words, cbnzX(0, len(e.words), len(e.words)+2))
	e.m12ArrayFailSites = append(e.m12ArrayFailSites, len(e.words))
	e.words = append(e.words, 0)
	e.words = append(e.words, movX(14, 0)) // new array descriptor

	// Recompute source after the runtime call (x9-x15 are caller-clobbered).
	e.stringConstSites = append(e.stringConstSites, len(e.words))
	e.words = append(e.words, 0, 0) // ADRP/ADD x10,__const
	e.words = append(e.words, m10Lsr(9, 26, 1), m10Lsl(11, 9, 3), addX(11, 10, 11))
	e.words = append(e.words, m9LdrW(12, 11, 0), m9LdrW(13, 11, 4), addX(12, 10, 12))
	e.words = append(e.words, ldrX(15, 14, 5), addImmX(15, 15, m12PayloadBase), movzX(9, 0))
	copyLoop := len(e.words)
	e.words = append(e.words, m8Cmp(9, 13))
	copyDone := len(e.words)
	e.words = append(e.words, 0)
	e.words = append(e.words, addX(10, 12, 9), m8LdrB(10, 10), addX(11, 15, 9), m8StrB(10, 11), addImmX(9, 9, 1))
	back, err := branch26(0x14000000, len(e.words), copyLoop)
	if err != nil {
		return err
	}
	e.words = append(e.words, back)
	copyDoneTarget := len(e.words)
	e.words[copyDone], err = branchCondChecked(copyDone, copyDoneTarget, 2) // HS
	if err != nil {
		return err
	}
	e.words = append(e.words, strX(13, 14, 1))
	e.storeOperand(e.depth-1, 14)
	e.words = append(e.words, ldrX(26, 31, 0), addImmX(31, 31, 16))
	staticDone := len(e.words)
	e.words = append(e.words, 0)

	dynamicTarget := len(e.words)
	if err := e.ArrayClone(1, false); err != nil {
		return err
	}
	doneTarget := len(e.words)
	e.words[dynamicBranch], err = branchCondChecked(dynamicBranch, dynamicTarget, 0) // low bit == 0
	if err != nil {
		return err
	}
	e.words[staticDone], err = branch26(0x14000000, staticDone, doneTarget)
	return err
}

func (e *StackEmitter) StringCompare(notEqual bool) error {
	if err := e.ensurePop("string comparison", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 0)
	e.loadOperand(e.depth-1, 1)
	if notEqual {
		e.words = append(e.words, movzX(2, 1))
	} else {
		e.words = append(e.words, movzX(2, 0))
	}
	e.stringCompareCalls = append(e.stringCompareCalls, len(e.words))
	e.words = append(e.words, 0)
	e.storeOperand(e.depth-2, 0)
	e.depth--
	return nil
}

func appendStringCompare() ([]uint32, error) {
	b := newM8Assembler()
	b.emit(0, 0) // ADRP/ADD x9,__const; patched by the final linker.
	b.emit(movX(10, 0), movX(11, 1), cmpZeroX(10))
	b.bc(0, "string.compare.null")
	b.emit(cmpZeroX(11))
	b.bc(0, "string.compare.null")
	b.emit(movzX(16, 1), m8And(16, 10, 16), cmpZeroX(16))
	b.bc(0, "string.compare.left.dynamic")
	b.emit(m10Lsr(16, 10, 1), m10Lsl(16, 16, 3), addX(16, 9, 16), m9LdrW(12, 16, 0), m9LdrW(13, 16, 4), addX(12, 9, 12))
	b.b("string.compare.right")
	b.label("string.compare.left.dynamic")
	b.emit(ldrX(13, 10, 1), ldrX(12, 10, 5), addImmX(12, 12, m12PayloadBase))
	b.label("string.compare.right")
	b.emit(movzX(16, 1), m8And(16, 11, 16), cmpZeroX(16))
	b.bc(0, "string.compare.right.dynamic")
	b.emit(m10Lsr(16, 11, 1), m10Lsl(16, 16, 3), addX(16, 9, 16), m9LdrW(14, 16, 0), m9LdrW(15, 16, 4), addX(14, 9, 14))
	b.b("string.compare.length")
	b.label("string.compare.right.dynamic")
	b.emit(ldrX(15, 11, 1), ldrX(14, 11, 5), addImmX(14, 14, m12PayloadBase))
	b.label("string.compare.length")
	b.emit(m8Cmp(13, 15))
	b.bc(1, "string.compare.false")
	b.emit(movzX(16, 0))
	b.label("string.compare.loop")
	b.emit(m8Cmp(16, 13))
	b.bc(2, "string.compare.true")
	b.emit(addX(17, 12, 16), m8LdrB(17, 17), addX(10, 14, 16), m8LdrB(10, 10), m8Cmp(17, 10))
	b.bc(1, "string.compare.false")
	b.emit(addImmX(16, 16, 1))
	b.b("string.compare.loop")
	b.label("string.compare.true")
	b.emit(movzX(0, 1))
	b.b("string.compare.invert")
	b.label("string.compare.false")
	b.emit(movzX(0, 0))
	b.b("string.compare.invert")
	b.label("string.compare.null")
	b.emit(m8Cmp(10, 11), csetW(0, 0))
	b.label("string.compare.invert")
	b.emit(cmpZeroX(2))
	b.bc(0, "string.compare.return")
	b.emit(cmpZeroX(0), csetW(0, 0))
	b.label("string.compare.return")
	b.emit(m8Ret())
	return b.assemble()
}

func appendStaticStringLen(dst []uint32) []uint32 {
	start := len(dst)
	dst = append(dst,
		0, // ADRP x9, __const
		0, // ADD x9, x9, lo12(__const)
		movzX(10, 1),
		m8And(10, 0, 10),
		0, // CBZ x10, dynamic
		m10Lsr(0, 0, 1),
		0x8B000D2A, // ADD x10, x9, x0, LSL #3
		0xB9400540, // LDR w0, [x10, #4]
		0xD65F03C0,
	)
	dynamic := len(dst)
	dst = append(dst, ldrX(0, 0, 1), 0xD65F03C0)
	dst[start+4] = cbzX(10, start+4, dynamic)
	return dst
}

// appendPrintStaticString 接收 x0 中的 ID，从 __const 查找字节地址与长度。
// SYS_write 后再写入一个换行符，不调用 strlen，因此能正确处理内嵌 NUL。
// 第 0、1 条 ADRP/ADD 指令由最终链接阶段按真实 __const 地址回填。
func appendPrintStaticString(dst []uint32) []uint32 {
	start := len(dst)
	dst = append(dst,
		0, // ADRP x9, __const
		0, // ADD x9, x9, lo12(__const)
		movzX(10, 1),
		m8And(10, 0, 10),
		0, // CBZ x10, dynamic
		m10Lsr(0, 0, 1),
		0x8B000D2A,     // ADD x10, x9, x0, LSL #3
		0xB940014B,     // LDR w11, [x10]
		0xB9400542,     // LDR w2, [x10, #4]
		addX(1, 9, 11), // x1 = string address
		0,              // B write
	)
	dynamic := len(dst)
	dst = append(dst,
		ldrX(2, 0, 1),
		ldrX(1, 0, 5),
		addImmX(1, 1, m12PayloadBase),
	)
	write := len(dst)
	dst = append(dst,
		movzX(0, 1),  // stdout
		movzX(16, 4), // Darwin SYS_write
		svc(0x80),
		subImmX(31, 31, 16), // 16 字节对齐的临时换行缓冲
		0x52800149,          // MOVZ w9, #10
		strbW(9, 31),        // STRB w9, [sp]
		addImmX(1, 31, 0),   // x1 = sp
		movzX(2, 1),
		movzX(0, 1),
		movzX(16, 4),
		svc(0x80),
		addImmX(31, 31, 16),
		0xD65F03C0, // RET
	)
	dst[start+4] = cbzX(10, start+4, dynamic)
	var err error
	dst[start+10], err = branch26(0x14000000, start+10, write)
	if err != nil {
		panic(err)
	}
	return dst
}
