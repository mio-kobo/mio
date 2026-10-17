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

type StackEmitter struct {
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
	if e.functionMode || e.depth != 0 {
		return nil, fmt.Errorf("AOT M5 main must have empty operand stack")
	}
	code := append([]uint32(nil), e.words...)
	calls := append([]int(nil), e.printCalls...)
	boolCalls := append([]int(nil), e.boolCalls...)
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
	// 正常退出前也恢复入口 SP；不会把 main 的帧遗留在退出路径上。
	code = append(code, addImmX(31, 29, 0), 0xA8C17BFD, movzX(0, 0), movzX(16, 1), svc(0x80))
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
	return ExecutableWordsWithGlobals(code, e.globals)
}
