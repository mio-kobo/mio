package darwinarm64

import "fmt"

// MaxM5OperandSlots 限制每个指令块的静态操作数槽位数。
// M5 根据控制流分析结果预留栈帧，不再把表达式深度限制在 7 个寄存器内。
const MaxM5OperandSlots = 512

// StackEmitter 将虚拟操作数栈映射到 x29 下方连续的 8 字节槽位。
// SP 只在函数入口和出口调整；各条 Mochi 指令不会临时改变 SP。
// x8 是地址暂存寄存器，x9/x10 用于计算；x19..x22 保留 M3/M4 的状态语义。
type m6TrapSite struct {
	word int
	cond uint32
}

type floatCallFixup struct {
	word int
	kind uint8
}

type StackEmitter struct {
	m11Enabled    bool
	m11InitCall   int
	m11InitFail   int
	floatCalls    []floatCallFixup
	stringCalls   []int
	lastPopString bool
	lastPopFloat  bool
	words         []uint32
	depth         int
	maxStack      int
	locals        int
	globals       int
	functionMode  bool
	lastPopped    bool
	printCalls    []int
	boolCalls     []int
	fixups        []branchFixup
	functionCalls []functionCallFixup
	overflowSites []int
	trapSites     []m6TrapSite
	returnSites   []int
}

// NewStackEmitter 创建固定大小的栈帧。主程序和函数使用同一套槽位布局。
func NewStackEmitter(globals, locals, params, maxStack int, function bool) (*StackEmitter, error) {
	if globals < 0 || globals > MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M5 global slot count %d exceeds limit %d", globals, MaxGlobalSlots)
	}
	if maxStack < 0 || maxStack > MaxM5OperandSlots {
		return nil, fmt.Errorf("AOT M5 operand stack depth %d exceeds limit %d", maxStack, MaxM5OperandSlots)
	}
	if function {
		if locals < params || locals > MaxM4Locals || params < 0 || params > MaxM4Parameters {
			return nil, fmt.Errorf("AOT M5 invalid native frame (locals=%d parameters=%d)", locals, params)
		}
	} else if locals != 0 || params != 0 {
		return nil, fmt.Errorf("AOT M5 main cannot have locals or parameters")
	}
	e := &StackEmitter{maxStack: maxStack, locals: locals, globals: globals, functionMode: function}
	// 先保存 FP/LR，再将当前帧设为 x29；始终保持 SP 的 16 字节对齐。
	e.words = append(e.words, 0xA9BF7BFD, addImmX(29, 31, 0))
	frameBytes := (8*(locals+maxStack) + 15) &^ 15
	for remaining := frameBytes; remaining > 0; {
		step := remaining
		if step > 4080 {
			step = 4080
		}
		e.words = append(e.words, subImmX(31, 31, uint32(step)))
		remaining -= step
	}
	if function {
		// 参数暂存在 x0..x6；进入函数时立即写入本帧局部槽位。
		for i := 0; i < params; i++ {
			e.localAddr(i)
			e.words = append(e.words, strX(uint32(i), 8, 0))
		}
	} else {
		// x20: 是否曾经执行 Pop；x22: 原生用户函数调用深度。
		e.words = append(e.words, movzX(20, 0), movzX(22, 0))
	}
	return e, nil
}

func (e *StackEmitter) CodeOffset() int { return len(e.words) }

func (e *StackEmitter) SetOperandDepth(depth int) error {
	if depth < 0 || depth > e.maxStack {
		return fmt.Errorf("AOT M5 operand depth %d out of frame range 0..%d", depth, e.maxStack)
	}
	e.depth = depth
	return nil
}

// frameAddr 计算 x29 下方偏移，可以覆盖 ARM64 单条 SUB immediate 的范围。
func (e *StackEmitter) frameAddr(offset int) {
	if offset <= 4095 {
		e.words = append(e.words, subImmX(8, 29, uint32(offset)))
	} else {
		e.words = append(e.words, movzX(8, uint16(offset)), subX(8, 29, 8))
	}
}

func (e *StackEmitter) localAddr(slot int) {
	e.frameAddr(8 * (slot + 1))
}
func (e *StackEmitter) operandAddr(slot int) {
	e.frameAddr(8 * (e.locals + slot + 1))
}
func (e *StackEmitter) loadOperand(slot int, reg uint32) {
	e.operandAddr(slot)
	e.words = append(e.words, ldrX(reg, 8, 0))
}
func (e *StackEmitter) storeOperand(slot int, reg uint32) {
	e.operandAddr(slot)
	e.words = append(e.words, strX(reg, 8, 0))
}
func (e *StackEmitter) ensurePush() error {
	if e.depth >= e.maxStack {
		return fmt.Errorf("AOT M5 operand stack limit exceeded (max %d)", e.maxStack)
	}
	return nil
}
func (e *StackEmitter) ensurePop(name string, count int) error {
	if e.depth < count {
		return fmt.Errorf("AOT M5 stack underflow at %s", name)
	}
	return nil
}

func (e *StackEmitter) PushInt64(n int64) error {
	if err := e.ensurePush(); err != nil {
		return err
	}
	bits := uint64(n)
	e.words = append(e.words, movzX(9, uint16(bits)))
	for shift := uint32(1); shift < 4; shift++ {
		e.words = append(e.words, movkX(9, uint16(bits>>(shift*16)), shift))
	}
	e.storeOperand(e.depth, 9)
	e.depth++
	return nil
}
func (e *StackEmitter) PushBool(v bool) error {
	if err := e.ensurePush(); err != nil {
		return err
	}
	imm := uint16(0)
	if v {
		imm = 1
	}
	e.words = append(e.words, movzX(9, imm))
	e.storeOperand(e.depth, 9)
	e.depth++
	return nil
}
func (e *StackEmitter) NotBool() error {
	if err := e.ensurePop("OpNot", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, cmpZeroX(9), csetW(9, 0))
	e.storeOperand(e.depth-1, 9)
	return nil
}
func (e *StackEmitter) Compare(cond uint32) error {
	if err := e.ensurePop("OpCompare", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 9)
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, cmpX(9, 10), csetW(9, cond))
	e.storeOperand(e.depth-2, 9)
	e.depth--
	return nil
}
func (e *StackEmitter) binary(name string, encode func(uint32, uint32, uint32) uint32) error {
	if err := e.ensurePop(name, 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 9)
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, encode(9, 9, 10))
	e.storeOperand(e.depth-2, 9)
	e.depth--
	return nil
}
func (e *StackEmitter) Add() error { return e.binary("OpAdd", addX) }
func (e *StackEmitter) Sub() error { return e.binary("OpSub", subX) }
func (e *StackEmitter) Mul() error { return e.binary("OpMul", mulX) }
func (e *StackEmitter) Neg() error {
	if err := e.ensurePop("OpNeg", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, subX(9, 31, 9))
	e.storeOperand(e.depth-1, 9)
	return nil
}

func (e *StackEmitter) globalAccess(reg uint32, slot int, store bool) error {
	if slot < 0 || slot >= e.globals {
		return fmt.Errorf("AOT M5 global slot %d out of range", slot)
	}
	// M5 延续 M2 的单页 __TEXT + 单页 __DATA 布局，地址仍采用 ADRP。
	addr := textVMAddr + pageSize + uint64(slot)*8
	pc := textVMAddr + entryOffset + uint64(len(e.words))*4
	ins, err := adrpX(8, pc, addr)
	if err != nil {
		return err
	}
	access := ldrX(reg, 8, uint32((addr&0xfff)/8))
	if store {
		access = strX(reg, 8, uint32((addr&0xfff)/8))
	}
	e.words = append(e.words, ins, access)
	return nil
}
func (e *StackEmitter) SetGlobal(slot int) error {
	if err := e.ensurePop("OpSetGlobal", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	if err := e.globalAccess(9, slot, true); err != nil {
		return err
	}
	e.depth--
	return nil
}
func (e *StackEmitter) GetGlobal(slot int) error {
	if err := e.ensurePush(); err != nil {
		return err
	}
	if err := e.globalAccess(9, slot, false); err != nil {
		return err
	}
	e.storeOperand(e.depth, 9)
	e.depth++
	return nil
}
func (e *StackEmitter) SetLocal(slot int) error {
	if !e.functionMode || slot < 0 || slot >= e.locals {
		return fmt.Errorf("AOT M5 local slot %d out of range", slot)
	}
	if err := e.ensurePop("OpSetLocal", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.localAddr(slot)
	e.words = append(e.words, strX(9, 8, 0))
	e.depth--
	return nil
}
func (e *StackEmitter) GetLocal(slot int) error {
	if !e.functionMode || slot < 0 || slot >= e.locals {
		return fmt.Errorf("AOT M5 local slot %d out of range", slot)
	}
	if err := e.ensurePush(); err != nil {
		return err
	}
	e.localAddr(slot)
	e.words = append(e.words, ldrX(9, 8, 0))
	e.storeOperand(e.depth, 9)
	e.depth++
	return nil
}

func (e *StackEmitter) Jump(target int) error {
	e.fixups = append(e.fixups, branchFixup{atWord: len(e.words), targetByte: target})
	e.words = append(e.words, 0)
	return nil
}
func (e *StackEmitter) JumpIfFalse(target int) error {
	if err := e.ensurePop("OpJumpIfFalse", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.fixups = append(e.fixups, branchFixup{atWord: len(e.words), targetByte: target, register: 9, conditional: true})
	e.words = append(e.words, 0)
	e.depth--
	return nil
}
func (e *StackEmitter) PatchControlFlow(offsetWords map[int]int) error {
	for _, fix := range e.fixups {
		target, ok := offsetWords[fix.targetByte]
		if !ok {
			return fmt.Errorf("AOT M5 missing native target for byte offset %d", fix.targetByte)
		}
		var word uint32
		var err error
		if fix.conditional {
			word, err = checkedCBZ(fix.register, fix.atWord, target)
		} else {
			word, err = branch26(0x14000000, fix.atWord, target)
		}
		if err != nil {
			return err
		}
		e.words[fix.atWord] = word
	}
	return nil
}

func (e *StackEmitter) Chant(isBool bool) error {
	if err := e.ensurePop("OpChant", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 0)
	if isBool {
		e.boolCalls = append(e.boolCalls, len(e.words))
	} else {
		e.printCalls = append(e.printCalls, len(e.words))
	}
	e.words = append(e.words, 0)
	e.depth--
	return nil
}
func (e *StackEmitter) Pop(isBool bool) error {
	if err := e.ensurePop("OpPop", 1); err != nil {
		return err
	}
	if !e.functionMode {
		e.loadOperand(e.depth-1, 19)
		e.words = append(e.words, movzX(20, 1))
		kind := uint16(0)
		if isBool {
			kind = 1
		}
		e.words = append(e.words, movzX(21, kind))
		e.lastPopped = true
	}
	e.depth--
	return nil
}

// ChantFloat 直接调用纯 ARM64 浮点十进制例程，运行时检查 IEEE754 舍入边界。
func (e *StackEmitter) ChantFloat(kind uint8) error {
	if kind != fp32Kind && kind != fp64Kind {
		return fmt.Errorf("AOT M6b invalid float output type %d", kind)
	}
	if err := e.ensurePop("OpChant", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 0)
	e.words = append(e.words, movzX(1, uint16(kind)))
	e.floatCalls = append(e.floatCalls, floatCallFixup{word: len(e.words), kind: kind})
	e.words = append(e.words, 0)
	e.depth--
	return nil
}

// PopFloat 保留顶层最后一个 float，只有真实执行过 Pop 才在结束时输出。
func (e *StackEmitter) PopFloat(kind uint8) error {
	if kind != fp32Kind && kind != fp64Kind {
		return fmt.Errorf("AOT M6b invalid float Pop type %d", kind)
	}
	if e.functionMode {
		if err := e.ensurePop("OpPop", 1); err != nil {
			return err
		}
		e.depth--
		return nil
	}
	if err := e.ensurePop("OpPop", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 19)
	e.words = append(e.words, movzX(20, 1), movzX(21, uint16(kind)))
	e.lastPopped = true
	e.lastPopFloat = true
	e.depth--
	return nil
}

// CallNative 不依赖 x9..x15 保留调用者操作数；所有临时值已经存进当前帧。
func (e *StackEmitter) CallNative(functionID, params int, returns bool) error {
	if params < 0 || params > MaxM4Parameters || e.depth < params {
		return fmt.Errorf("AOT M5 invalid native call params=%d depth=%d", params, e.depth)
	}
	base := e.depth - params
	if returns && base >= e.maxStack {
		return fmt.Errorf("AOT M5 return stack limit exceeded")
	}
	for i := 0; i < params; i++ {
		e.loadOperand(base+i, uint32(i))
	}
	e.words = append(e.words, addImmX(22, 22, 1), cmpImmX(22, MaxM4CallDepth))
	e.overflowSites = append(e.overflowSites, len(e.words))
	e.words = append(e.words, 0)
	e.functionCalls = append(e.functionCalls, functionCallFixup{word: len(e.words), functionID: functionID})
	e.words = append(e.words, 0)
	e.words = append(e.words, subImmX(22, 22, 1))
	if returns {
		e.storeOperand(base, 0)
	}
	e.depth = base
	if returns {
		e.depth++
	}
	return nil
}
func (e *StackEmitter) ReturnValue() error {
	if !e.functionMode || e.depth != 1 {
		return fmt.Errorf("AOT M5 OpReturnValue needs one int64 result")
	}
	e.loadOperand(0, 0)
	e.depth = 0
	e.returnSites = append(e.returnSites, len(e.words))
	e.words = append(e.words, 0)
	return nil
}
func (e *StackEmitter) ReturnUnit() error {
	if !e.functionMode || e.depth != 0 {
		return fmt.Errorf("AOT M5 OpReturn needs empty stack")
	}
	e.returnSites = append(e.returnSites, len(e.words))
	e.words = append(e.words, 0)
	return nil
}
func (e *StackEmitter) FinishFunction() error {
	if !e.functionMode || e.depth != 0 {
		return fmt.Errorf("AOT M5 invalid function ending")
	}
	ep := len(e.words)
	// 直接将 SP 恢复到 FP，避免大栈帧的多条 ADD，随后恢复 x29/x30。
	e.words = append(e.words, addImmX(31, 29, 0), 0xA8C17BFD, 0xD65F03C0)
	for _, at := range e.returnSites {
		word, err := branch26(0x14000000, at, ep)
		if err != nil {
			return err
		}
		e.words[at] = word
	}
	return nil
}

// FinishWithFunctions 拼接主程序、函数和原生输出例程，并回填所有 BL / B.HI。
func (e *StackEmitter) FinishWithFunctions(functions []*StackEmitter) ([]byte, error) {
	return e.FinishWithFunctionsAndStrings(functions, nil)
}

// FinishWithFunctionsAndStrings 链接程序、字符串输出例程和只读 UTF-8 常量表。
func (e *StackEmitter) FinishWithFunctionsAndStrings(functions []*StackEmitter, strings []string) ([]byte, error) {
	return e.finishWithM11(functions, strings)
}

func (e *StackEmitter) finishWithM11(functions []*StackEmitter, strings []string) ([]byte, error) {
	if e.functionMode || e.depth != 0 {
		return nil, fmt.Errorf("AOT M5 main must have empty operand stack")
	}
	code := append([]uint32(nil), e.words...)
	calls := append([]int(nil), e.printCalls...)
	boolCalls := append([]int(nil), e.boolCalls...)
	floatCalls := append([]floatCallFixup(nil), e.floatCalls...)
	stringCalls := append([]int(nil), e.stringCalls...)
	if e.lastPopped && e.lastPopString {
		// M7 的表达式结果可能来自不同分支，退出前按最后的实际运行时标签输出。
		jumpEmpty := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, m7StringKind))
		jumpString := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, uint32(fp32Kind)))
		jumpFloat32 := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, uint32(fp64Kind)))
		jumpFloat64 := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, 1))
		jumpBool := len(code)
		code = append(code, 0)
		code = append(code, movX(0, 19))
		calls = append(calls, len(code))
		code = append(code, 0)
		intExit := len(code)
		code = append(code, 0)

		stringStart := len(code)
		code = append(code, movX(0, 19))
		stringCalls = append(stringCalls, len(code))
		code = append(code, 0)
		stringExit := len(code)
		code = append(code, 0)

		boolStart := len(code)
		code = append(code, movX(0, 19))
		boolCalls = append(boolCalls, len(code))
		code = append(code, 0)
		boolExit := len(code)
		code = append(code, 0)

		float32Start := len(code)
		code = append(code, movX(0, 19), movzX(1, uint16(fp32Kind)))
		floatCalls = append(floatCalls, floatCallFixup{word: len(code), kind: fp32Kind})
		code = append(code, 0)
		float32Exit := len(code)
		code = append(code, 0)

		float64Start := len(code)
		code = append(code, movX(0, 19), movzX(1, uint16(fp64Kind)))
		floatCalls = append(floatCalls, floatCallFixup{word: len(code), kind: fp64Kind})
		code = append(code, 0)
		exitStart := len(code)

		var err error
		if code[jumpEmpty], err = checkedCBZ(20, jumpEmpty, exitStart); err != nil {
			return nil, err
		}
		code[jumpString] = branchCond(jumpString, stringStart, 0)
		code[jumpFloat32] = branchCond(jumpFloat32, float32Start, 0)
		code[jumpFloat64] = branchCond(jumpFloat64, float64Start, 0)
		code[jumpBool] = branchCond(jumpBool, boolStart, 0)
		for _, at := range []int{intExit, stringExit, boolExit, float32Exit} {
			if code[at], err = branch26(0x14000000, at, exitStart); err != nil {
				return nil, err
			}
		}
	} else if e.lastPopped && e.lastPopFloat {
		// 对浮点/整数/布尔 Pop 使用运行时标签分派，保留条件分支语义。
		jumpEmpty := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, uint32(fp32Kind)))
		float32Jump := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, uint32(fp64Kind)))
		float64Jump := len(code)
		code = append(code, 0)
		boolJump := len(code)
		code = append(code, 0)
		code = append(code, movX(0, 19))
		calls = append(calls, len(code))
		code = append(code, 0)
		intExitJump := len(code)
		code = append(code, 0)
		boolStart := len(code)
		code = append(code, movX(0, 19))
		boolCalls = append(boolCalls, len(code))
		code = append(code, 0)
		boolExitJump := len(code)
		code = append(code, 0)
		float32Start := len(code)
		code = append(code, movX(0, 19), movzX(1, uint16(fp32Kind)))
		e.floatCalls = append(e.floatCalls, floatCallFixup{word: len(code), kind: fp32Kind})
		code = append(code, 0)
		float32ExitJump := len(code)
		code = append(code, 0)
		float64Start := len(code)
		code = append(code, movX(0, 19), movzX(1, uint16(fp64Kind)))
		e.floatCalls = append(e.floatCalls, floatCallFixup{word: len(code), kind: fp64Kind})
		code = append(code, 0)
		exitStart := len(code)
		var err error
		code[jumpEmpty], err = checkedCBZ(20, jumpEmpty, exitStart)
		if err != nil {
			return nil, err
		}
		code[float32Jump] = branchCond(float32Jump, float32Start, 0) // EQ
		code[float64Jump] = branchCond(float64Jump, float64Start, 0) // EQ
		code[boolJump] = cbnzX(21, boolJump, boolStart)              // x21=1 时输出布尔值
		for _, at := range []int{intExitJump, boolExitJump, float32ExitJump} {
			code[at], err = branch26(0x14000000, at, exitStart)
			if err != nil {
				return nil, err
			}
		}
	} else {
		if e.lastPopped {
			jumpEmpty := len(code)
			code = append(code, 0)
			jumpInt := len(code)
			code = append(code, 0)
			code = append(code, movX(0, 19))
			boolCalls = append(boolCalls, len(code))
			code = append(code, 0)
			jumpExit := len(code)
			code = append(code, 0)
			intStart := len(code)
			code = append(code, movX(0, 19))
			calls = append(calls, len(code))
			code = append(code, 0)
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
	}
	// 最后表达式 Pop 可能在本函数中增添新的 float BL 占位符。
	if !e.lastPopString {
		floatCalls = append([]floatCallFixup(nil), e.floatCalls...)
	}
	// M11a 收尾阶段才分配自检对象，避免在没有真实 Stack Map 的 Mio 主程序中触发 GC。
	var m11 m11LinkState
	if e.m11Enabled {
		m11.calls = append(m11.calls, m11CallSite{at: e.m11InitCall, kind: m11Init})
		code = appendM11Lifecycle(code, &m11)
	}
	// 正常退出前也恢复入口 SP；不会把 main 的帧遗留在退出路径上。
	code = append(code, addImmX(31, 29, 0), 0xA8C17BFD, movzX(0, 0), movzX(16, 1), svc(0x80))
	if e.m11Enabled {
		var err error
		code, err = appendM11Failure(code, &m11, e.m11InitFail)
		if err != nil {
			return nil, err
		}
	}
	functionStarts := make([]int, len(functions))
	allCalls := append([]functionCallFixup(nil), e.functionCalls...)
	overflows := append([]int(nil), e.overflowSites...)
	traps := append([]m6TrapSite(nil), e.trapSites...)
	for i, fn := range functions {
		if fn == nil || !fn.functionMode {
			return nil, fmt.Errorf("AOT M5 invalid native function %d", i)
		}
		functionStarts[i] = len(code)
		code = append(code, fn.words...)
		for _, at := range fn.printCalls {
			calls = append(calls, functionStarts[i]+at)
		}
		for _, at := range fn.boolCalls {
			boolCalls = append(boolCalls, functionStarts[i]+at)
		}
		for _, at := range fn.stringCalls {
			stringCalls = append(stringCalls, functionStarts[i]+at)
		}
		for _, fx := range fn.floatCalls {
			fx.word += functionStarts[i]
			floatCalls = append(floatCalls, fx)
		}
		for _, fx := range fn.functionCalls {
			fx.word += functionStarts[i]
			allCalls = append(allCalls, fx)
		}
		for _, at := range fn.overflowSites {
			overflows = append(overflows, functionStarts[i]+at)
		}
		for _, trap := range fn.trapSites {
			trap.word += functionStarts[i]
			traps = append(traps, trap)
		}
	}
	if len(overflows) > 0 || len(traps) > 0 {
		errExit := len(code)
		code = append(code, movzX(0, 1), movzX(16, 1), svc(0x80))
		for _, at := range overflows {
			delta := errExit - at
			if delta < -(1<<18) || delta >= (1<<18) {
				return nil, fmt.Errorf("AOT M5 call-depth guard target out of range")
			}
			code[at] = branchCond(at, errExit, 8) // HI
		}
		for _, trap := range traps {
			delta := errExit - trap.word
			if delta < -(1<<18) || delta >= 1<<18 {
				return nil, fmt.Errorf("AOT M6 float trap branch target out of range")
			}
			code[trap.word] = branchCond(trap.word, errExit, trap.cond)
		}
	}
	for _, fx := range allCalls {
		if fx.functionID < 0 || fx.functionID >= len(functionStarts) {
			return nil, fmt.Errorf("AOT M5 unknown function %d", fx.functionID)
		}
		word, err := branch26(0x94000000, fx.word, functionStarts[fx.functionID])
		if err != nil {
			return nil, err
		}
		code[fx.word] = word
	}
	if len(calls) != 0 {
		start := len(code)
		code = appendPrintInt64(code)
		for _, at := range calls {
			word, err := branch26(0x94000000, at, start)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
	}
	if len(boolCalls) != 0 {
		start := len(code)
		code = appendPrintBool(code)
		for _, at := range boolCalls {
			word, err := branch26(0x94000000, at, start)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
	}
	if len(floatCalls) != 0 {
		start := len(code)
		code = append(code, floatRuntimeWords[:]...)
		for _, at := range floatCalls {
			word, err := branch26(0x94000000, at.word, start)
			if err != nil {
				return nil, err
			}
			code[at.word] = word
		}
	}
	// M10 的机器码全部采用 PC 相对 BL，原封不动链接到主程序后方。
	if e.m11Enabled {
		runtimeStart := len(code)
		words, entries, err := M10RuntimeWords()
		if err != nil {
			return nil, err
		}
		code = append(code, words...)
		for _, call := range m11.calls {
			target := -1
			switch call.kind {
			case m11Init:
				target = entries.Init
			case m11Alloc:
				target = entries.Alloc
			case m11Collect:
				target = entries.Collect
			case m11Destroy:
				target = entries.Destroy
			default:
				return nil, fmt.Errorf("AOT M11a unknown runtime function")
			}
			ins, err := branch26(0x94000000, call.at, runtimeStart+target)
			if err != nil {
				return nil, err
			}
			code[call.at] = ins
		}
	}
	if len(stringCalls) != 0 && len(strings) == 0 {
		return nil, fmt.Errorf("AOT M7 missing static string table")
	}
	var constants []byte
	if len(strings) != 0 {
		var err error
		constants, err = stringImage(strings)
		if err != nil {
			return nil, err
		}
	}
	if len(stringCalls) != 0 {
		start := len(code)
		code = appendPrintStaticString(code)
		// 先确定字符串表的最终文件偏移，避免将静态绝对地址写入机器码。
		constOff := (entryOffset + uint64(len(code))*4 + 7) &^ 7
		constVA := textVMAddr + constOff
		pc := textVMAddr + entryOffset + uint64(start)*4
		var err error
		if code[start], err = adrpX(9, pc, constVA); err != nil {
			return nil, err
		}
		code[start+1] = addImmX(9, 9, uint32(constVA&0xfff))
		for _, at := range stringCalls {
			word, err := branch26(0x94000000, at, start)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
	}
	if e.m11Enabled {
		// 元数据按 8 字节对齐追加到 __TEXT,__const，字符串表偏移不变。
		for len(constants)%8 != 0 {
			constants = append(constants, 0)
		}
		metaPos := len(constants)
		meta, err := M9EncodeMetadata(
			[]M9TypeLayout{{TypeID: 1, PayloadBytes: 8}},
			M9RootMap{FrameBytes: 16, FrameRefs: []uint32{0}},
		)
		if err != nil {
			return nil, err
		}
		constants = append(constants, meta...)
		constOff := (entryOffset + uint64(len(code))*4 + 7) &^ 7
		if err := m11PatchMetaAddress(code, m11.metadataADRP, constOff+uint64(metaPos)); err != nil {
			return nil, err
		}
	}
	return ExecutableWordsWithGlobalsAndConst(code, e.globals, constants)
}
