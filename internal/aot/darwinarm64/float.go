package darwinarm64

import (
	"fmt"
	"math"
)

// M6 的数字仍放在 M5 的 8 字节槽位里：int64 为原值，float 为 IEEE 754 位模式。
// 计算时使用 V0/V1；M15a 函数参数通过 X0..X7 及调用方栈参数区传递原始
// 位模式，返回值仍通过 X0。这里不采用 Darwin C ABI 的 FP 参数分类。
// 这些 FP/SIMD 寄存器属于易失寄存器，跨调用保留的值始终存放在本帧内。
const (
	fp32Kind   = 3
	fp64Kind   = 4
	m6OpAdd    = 2
	m6OpSub    = 4
	m6OpMul    = 5
	m6OpDiv    = 6
	m6OpEq     = 11
	m6OpNe     = 12
	m6OpLt     = 13
	m6OpLe     = 14
	m6OpGt     = 15
	m6OpGe     = 16
	m6IntToF64 = 28
	m6F64ToInt = 29
	m6IntToF32 = 50
	m6F32ToInt = 51
	m6F32ToF64 = 52
	m6F64ToF32 = 53
)

func (e *StackEmitter) m6PushBits(bits uint64) error {
	if err := e.ensurePush(); err != nil {
		return err
	}
	e.words = append(e.words, movzX(9, uint16(bits)))
	for shift := uint32(1); shift < 4; shift++ {
		e.words = append(e.words, movkX(9, uint16(bits>>(shift*16)), shift))
	}
	e.storeOperand(e.depth, 9)
	e.depth++
	return nil
}
func (e *StackEmitter) PushFloat32(n float32) error { return e.m6PushBits(uint64(math.Float32bits(n))) }
func (e *StackEmitter) PushFloat64(n float64) error { return e.m6PushBits(math.Float64bits(n)) }

func floatMoveFromX(kind uint8, freg, xreg uint32) uint32 {
	if kind == fp32Kind {
		return 0x1E270000 | xreg<<5 | freg // FMOV Sn, Wm
	}
	return 0x9E670000 | xreg<<5 | freg // FMOV Dn, Xm
}
func floatMoveToX(kind uint8, xreg, freg uint32) uint32 {
	if kind == fp32Kind {
		return 0x1E260000 | freg<<5 | xreg // FMOV Wd, Sn (零扩展)
	}
	return 0x9E660000 | freg<<5 | xreg // FMOV Xd, Dn
}
func (e *StackEmitter) FloatBinary(kind, opcode uint8) error {
	if kind != fp32Kind && kind != fp64Kind {
		return fmt.Errorf("AOT M6 unsupported float kind %d", kind)
	}
	if err := e.ensurePop("M6 float arithmetic", 2); err != nil {
		return err
	}
	base := uint32(0x1E600000)
	if kind == fp32Kind {
		base = 0x1E200000
	}
	op := uint32(0)
	switch opcode {
	case m6OpAdd:
		op = 0x2800
	case m6OpSub:
		op = 0x3800
	case m6OpMul:
		op = 0x0800
	case m6OpDiv:
		op = 0x1800
	default:
		return fmt.Errorf("AOT M6 unknown float opcode %d", opcode)
	}
	e.loadOperand(e.depth-2, 9)
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, floatMoveFromX(kind, 0, 9), floatMoveFromX(kind, 1, 10))
	e.words = append(e.words, base|op|1<<16) // FOP V0,V0,V1
	e.words = append(e.words, floatMoveToX(kind, 9, 0))
	e.storeOperand(e.depth-2, 9)
	e.depth--
	return nil
}
func (e *StackEmitter) FloatNeg(kind uint8) error {
	if kind != fp32Kind && kind != fp64Kind {
		return fmt.Errorf("AOT M6 invalid float kind %d", kind)
	}
	if err := e.ensurePop("M6 float negation", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, floatMoveFromX(kind, 0, 9))
	word := uint32(0x1E614000) // FNEG D0,D0
	if kind == fp32Kind {
		word = 0x1E214000 // FNEG S0,S0
	}
	e.words = append(e.words, word, floatMoveToX(kind, 9, 0))
	e.storeOperand(e.depth-1, 9)
	return nil
}
func (e *StackEmitter) FloatCompare(kind, opcode uint8) error {
	if kind != fp32Kind && kind != fp64Kind {
		return fmt.Errorf("AOT M6 invalid float comparison kind %d", kind)
	}
	if err := e.ensurePop("M6 float comparison", 2); err != nil {
		return err
	}
	// FCMP 在 unordered（NaN）时设置 C=1,V=1,N=Z=0。
	// 小于必须使用 MI，大于等于使用 GE，小于等于使用 LS，避免 NaN 被误判为 true。
	cond := uint32(0)
	switch opcode {
	case m6OpEq:
		cond = 0 // EQ
	case m6OpNe:
		cond = 1 // NE（NaN != x 为 true）
	case m6OpLt:
		cond = 4 // MI
	case m6OpLe:
		cond = 9 // LS
	case m6OpGt:
		cond = 12 // GT
	case m6OpGe:
		cond = 10 // GE
	default:
		return fmt.Errorf("AOT M6 invalid comparison opcode %d", opcode)
	}
	e.loadOperand(e.depth-2, 9)
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, floatMoveFromX(kind, 0, 9), floatMoveFromX(kind, 1, 10))
	word := uint32(0x1E612000) // FCMP D0,D1
	if kind == fp32Kind {
		word = 0x1E212000 // FCMP S0,S1
	}
	e.words = append(e.words, word, csetW(9, cond))
	e.storeOperand(e.depth-2, 9)
	e.depth--
	return nil
}

// m6LiteralFloat64 在 FP 比较边界时用 X10 传递完整的 64 位位模式。
func (e *StackEmitter) m6LiteralFloat64(v float64) {
	bits := math.Float64bits(v)
	e.words = append(e.words, movzX(10, uint16(bits)))
	for shift := uint32(1); shift < 4; shift++ {
		e.words = append(e.words, movkX(10, uint16(bits>>(shift*16)), shift))
	}
	e.words = append(e.words, floatMoveFromX(fp64Kind, 1, 10))
}
func (e *StackEmitter) m6TrapIf(cond uint32) {
	e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, cond^1))
	e.trapSites = append(e.trapSites, m6TrapSite{word: len(e.words), cond: ^uint32(0)})
	e.words = append(e.words, 0)
}

func (e *StackEmitter) FloatConvert(op uint8) error {
	if err := e.ensurePop("M6 float conversion", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	switch op {
	case m6IntToF64:
		e.words = append(e.words, 0x9E620120, floatMoveToX(fp64Kind, 9, 0)) // SCVTF D0,X9
	case m6IntToF32:
		e.words = append(e.words, 0x9E220120, floatMoveToX(fp32Kind, 9, 0)) // SCVTF S0,X9
	case m6F32ToF64:
		e.words = append(e.words, floatMoveFromX(fp32Kind, 0, 9), 0x1E22C000, floatMoveToX(fp64Kind, 9, 0))
	case m6F64ToF32:
		e.words = append(e.words, floatMoveFromX(fp64Kind, 0, 9), 0x1E624000, floatMoveToX(fp32Kind, 9, 0))
	case m6F64ToInt, m6F32ToInt:
		kind := uint8(fp64Kind)
		if op == m6F32ToInt {
			kind = fp32Kind
		}
		e.words = append(e.words, floatMoveFromX(kind, 0, 9))
		if kind == fp32Kind {
			e.words = append(e.words, 0x1E22C000) // FCVT D0,S0
		}
		// VM 对 NaN、Infinity、< -2^63、>= 2^63 都返回转换错误。
		// 这些非法输入从原生程序以状态码 1 退出，不能交给 FCVTZS 饱和处理。
		e.words = append(e.words, 0x1E602000) // FCMP D0,D0
		e.m6TrapIf(6)                         // VS: unordered / NaN
		e.m6LiteralFloat64(0x1p63)
		e.words = append(e.words, 0x1E612000) // FCMP D0,D1
		e.m6TrapIf(10)                        // GE: >= +2^63
		e.m6LiteralFloat64(-0x1p63)
		e.words = append(e.words, 0x1E612000) // FCMP D0,D1
		e.m6TrapIf(11)                        // LT: < -2^63
		e.words = append(e.words, 0x9E780009) // FCVTZS X9,D0
	default:
		return fmt.Errorf("AOT M6 unsupported conversion opcode %d", op)
	}
	e.storeOperand(e.depth-1, 9)
	return nil
}
