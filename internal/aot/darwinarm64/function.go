package darwinarm64

import "fmt"

// M4 的函数调用使用一套明确的内部 ABI：
// x0..x6 传递至多 7 个 int64 参数，x0 返回 int64；x9..x15 是表达式寄存器栈。
// 调用方保存 x9..x15，函数使用 x29 作为局部槽位的帧指针并保存 x30。
// x22 记录当前调用深度；超过 1024 层时，原生程序以状态码 1 退出。
const (
	MaxM4Parameters = 7
	MaxM4Locals     = 256
	MaxM4CallDepth  = 1024
)

type functionCallFixup struct {
	word       int
	functionID int
}

// EnableFunctionCalls 初始化原生调用深度。只在主程序入口使用。
func (e *IntegerEmitter) EnableFunctionCalls() error {
	if !e.branching || e.functionMode || e.depth != 0 || len(e.words) != 1 {
		return fmt.Errorf("AOT M4 function calls must be enabled at the main entry")
	}
	e.words = append(e.words, movzX(22, 0))
	return nil
}

// EnableFunction 创建独立的栈帧，不影响主程序的 OpPop 输出状态。
func (e *IntegerEmitter) EnableFunction(localCount, parameterCount int) error {
	if len(e.words) != 0 || e.depth != 0 || e.branching {
		return fmt.Errorf("AOT M4 function emitter must be empty")
	}
	if parameterCount < 0 || parameterCount > MaxM4Parameters || localCount < parameterCount || localCount > MaxM4Locals {
		return fmt.Errorf("AOT M4 invalid native frame: parameters=%d locals=%d", parameterCount, localCount)
	}
	e.branching = true
	e.functionMode = true
	e.frameBytes = (localCount*8 + 15) &^ 15
	// STP x29,x30,[sp,#-16]! / MOV x29,sp / SUB sp,sp,#frameBytes
	e.words = append(e.words, 0xA9BF7BFD, addImmX(29, 31, 0))
	if e.frameBytes > 0 {
		e.words = append(e.words, subImmX(31, 31, uint32(e.frameBytes)))
	}
	// 参数在进入函数时立即写进当前帧，避免随后的 BL 和 syscall 覆盖寄存器。
	for i := 0; i < parameterCount; i++ {
		e.words = append(e.words, subImmX(8, 29, uint32((i+1)*8)), strX(uint32(i), 8, 0))
	}
	return nil
}

func (e *IntegerEmitter) localAccess(slot int, store bool) error {
	if !e.functionMode || slot < 0 || (slot+1)*8 > e.frameBytes {
		return fmt.Errorf("AOT M4 local slot %d out of range", slot)
	}
	if store && e.depth == 0 {
		return fmt.Errorf("AOT M4 stack underflow at OpSetLocal")
	}
	if !store && e.depth == 7 {
		return fmt.Errorf("AOT M4 operand stack limit exceeded")
	}
	reg := uint32(9 + e.depth)
	if store {
		reg--
	}
	// x8 是专用的暂存地址寄存器；对 SP 的操作始终保持 16 字节对齐。
	e.words = append(e.words, subImmX(8, 29, uint32((slot+1)*8)))
	if store {
		e.words = append(e.words, strX(reg, 8, 0))
		e.depth--
	} else {
		e.words = append(e.words, ldrX(reg, 8, 0))
		e.depth++
	}
	return nil
}

func (e *IntegerEmitter) SetLocal(slot int) error { return e.localAccess(slot, true) }
func (e *IntegerEmitter) GetLocal(slot int) error { return e.localAccess(slot, false) }

// Discard 丢弃函数中的表达式结果，不更新主程序最后一次 OpPop 的状态。
func (e *IntegerEmitter) Discard() error {
	if !e.functionMode || e.depth < 1 {
		return fmt.Errorf("AOT M4 stack underflow at function OpPop")
	}
	e.depth--
	return nil
}

// CallNative 传递实参、保存调用者表达式寄存器、检查递归深度并执行 BL。
func (e *IntegerEmitter) CallNative(id, params int, returns bool) error {
	if !e.branching {
		return fmt.Errorf("AOT M4 native calls need control flow")
	}
	if params < 0 || params > MaxM4Parameters || e.depth < params {
		return fmt.Errorf("AOT M4 invalid call: arguments=%d stack=%d", params, e.depth)
	}
	base := e.depth - params
	if returns && base >= 7 {
		return fmt.Errorf("AOT M4 operand stack limit exceeded by return value")
	}
	// 保存全部表达式寄存器。M4 调用链会重用 x9..x15，不能只保存参数之外的寄存器。
	e.words = append(e.words,
		subImmX(31, 31, 64),
		stpOffsetX(9, 10, 31, 0), stpOffsetX(11, 12, 31, 16),
		stpOffsetX(13, 14, 31, 32), strX(15, 31, 6),
	)
	for i := 0; i < params; i++ {
		e.words = append(e.words, movX(uint32(i), uint32(9+base+i)))
	}
	// 每次递归都经过这里，避免函数递归造成无控制的栈溢出。
	e.words = append(e.words, addImmX(22, 22, 1), cmpImmX(22, MaxM4CallDepth))
	e.overflowSites = append(e.overflowSites, len(e.words))
	e.words = append(e.words, 0) // B.HI -> 程序退出码 1
	e.functionCalls = append(e.functionCalls, functionCallFixup{word: len(e.words), functionID: id})
	e.words = append(e.words, 0) // BL -> 目标函数入口
	e.words = append(e.words, subImmX(22, 22, 1))
	e.words = append(e.words,
		ldpOffsetX(9, 10, 31, 0), ldpOffsetX(11, 12, 31, 16),
		ldpOffsetX(13, 14, 31, 32), ldrX(15, 31, 6), addImmX(31, 31, 64),
	)
	if returns {
		e.words = append(e.words, movX(uint32(9+base), 0))
	}
	e.depth = base
	if returns {
		e.depth++
	}
	return nil
}

// ReturnValue / ReturnUnit 统一跳至函数结尾的恢复帧代码。
func (e *IntegerEmitter) ReturnValue() error {
	if !e.functionMode || e.depth != 1 {
		return fmt.Errorf("AOT M4 OpReturnValue needs exactly one result")
	}
	e.words = append(e.words, movX(0, uint32(9+e.depth-1)))
	e.depth = 0
	e.returnSites = append(e.returnSites, len(e.words))
	e.words = append(e.words, 0)
	return nil
}
func (e *IntegerEmitter) ReturnUnit() error {
	if !e.functionMode || e.depth != 0 {
		return fmt.Errorf("AOT M4 OpReturn needs empty operand stack")
	}
	e.returnSites = append(e.returnSites, len(e.words))
	e.words = append(e.words, 0)
	return nil
}

func (e *IntegerEmitter) FinishFunction() error {
	if !e.functionMode || e.depth != 0 {
		return fmt.Errorf("AOT M4 invalid function ending")
	}
	epilogue := len(e.words)
	if e.frameBytes > 0 {
		e.words = append(e.words, addImmX(31, 31, uint32(e.frameBytes)))
	}
	// LDP x29,x30,[sp],#16 / RET
	e.words = append(e.words, 0xA8C17BFD, 0xD65F03C0)
	for _, pos := range e.returnSites {
		b, err := branch26(0x14000000, pos, epilogue)
		if err != nil {
			return err
		}
		e.words[pos] = b
	}
	return nil
}

func stpOffsetX(a, b, rn uint32, offset int) uint32 {
	return 0xA9000000 | uint32(offset/8)<<15 | b<<10 | rn<<5 | a
}
func ldpOffsetX(a, b, rn uint32, offset int) uint32 {
	return 0xA9400000 | uint32(offset/8)<<15 | b<<10 | rn<<5 | a
}
