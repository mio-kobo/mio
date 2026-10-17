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

// appendPrintStaticString 接收 x0 中的 ID，从 __const 查找字节地址与长度。
// SYS_write 后再写入一个换行符，不调用 strlen，因此能正确处理内嵌 NUL。
// 第 0、1 条 ADRP/ADD 指令由最终链接阶段按真实 __const 地址回填。
func appendPrintStaticString(dst []uint32) []uint32 {
	return append(dst,
		0,              // ADRP x9, __const
		0,              // ADD x9, x9, lo12(__const)
		0x8B000D2A,     // ADD x10, x9, x0, LSL #3
		0xB940014B,     // LDR w11, [x10]
		0xB9400542,     // LDR w2, [x10, #4]
		addX(1, 9, 11), // x1 = string address
		movzX(0, 1),    // stdout
		movzX(16, 4),   // Darwin SYS_write
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
}
