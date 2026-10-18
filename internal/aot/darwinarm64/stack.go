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

type closureTargetFixup struct {
	word       int
	functionID int
}

type globalAccessFixup struct {
	word  int
	reg   uint32
	slot  int
	store bool
}

type m14NativeCall struct {
	word int
	op   string
}

type StackEmitter struct {
	m11Enabled         bool
	m11InitCall        int
	m11InitFail        int
	floatCalls         []floatCallFixup
	stringCalls        []int
	lastPopString      bool
	lastPopFloat       bool
	lastPopNullable    bool
	words              []uint32
	depth              int
	maxStack           int
	locals             int
	globals            int
	functionMode       bool
	lastPopped         bool
	printCalls         []int
	boolCalls          []int
	fixups             []branchFixup
	functionCalls      []functionCallFixup
	overflowSites      []int
	trapSites          []m6TrapSite
	returnSites        []int
	m12Enabled         bool
	m12InitCall        int
	m12InitFail        int
	m12Calls           []m12CallSite
	m12ZeroSites       []int
	m12BoundsSites     []int
	m12ArrayFailSites  []int
	stringLenCalls     []int
	stringCompareCalls []int
	stringConstSites   []int
	gcEnabled          bool
	gcStress           bool
	gcRootCapacity     int
	gcRecordOffset     int
	gcSafepoints       []M11StackMap
	gcFrameRoots       []int
	gcGlobalRoots      []int
	frameSlots         int
	environmentSlot    int
	cellSlots          map[int]int
	upvalueCount       int
	closureTargets     []closureTargetFixup
	closureZeroSites   []int
	nullableNilSites   []int
	globalAccesses     []globalAccessFixup
	nativeFSCalls      []m14NativeCall
	nativeFSEnabled    bool
	nativeConstSite    int
	heapCapacity       uint64
	gcTelemetry        bool
}

// EnableNativeFS reserves x28 as the base of __TEXT,__const. Native filesystem
// entries use it to decode tagged static strings without absolute addresses.
func (e *StackEmitter) EnableNativeFS() {
	if e.nativeFSEnabled {
		return
	}
	e.nativeFSEnabled = true
	e.nativeConstSite = len(e.words)
	e.words = append(e.words, 0, 0, movzX(27, 0))
}

// ConfigureRuntime applies an explicit heap policy independently of which
// native modules the program imports.
func (e *StackEmitter) ConfigureRuntime(heapCapacity uint64, telemetry bool) error {
	if heapCapacity == 0 || heapCapacity > M8MaxHeapCapacity || heapCapacity%M8ObjectAlignment != 0 {
		return fmt.Errorf("AOT invalid heap capacity %d", heapCapacity)
	}
	e.heapCapacity = heapCapacity
	e.gcTelemetry = telemetry
	return nil
}

// EmitNativeFSFunction emits the body of one body-less MMA stub. The ordinary
// M15 frame remains in force, so arguments are reloaded from stable locals.
func (e *StackEmitter) EmitNativeFSFunction(op string, params int) error {
	if !e.functionMode || params < 0 || params > M15RegisterParameters {
		return fmt.Errorf("AOT M14 invalid native function %q", op)
	}
	roots := []int(nil)
	switch op {
	case "open", "open_dir":
		roots = []int{0}
	case "stat", "mkdir":
		roots = []int{0, 1}
	case "file_status", "close", "dir_status", "dir_close":
		roots = []int{1}
	case "read", "write":
		roots = []int{1, 4}
	}
	if err := e.SetGCRoots(roots, nil); err != nil {
		return err
	}
	for i := 0; i < params; i++ {
		e.localAddr(i)
		e.words = append(e.words, ldrX(uint32(i), 8, 0))
	}
	e.nativeFSCalls = append(e.nativeFSCalls, m14NativeCall{word: len(e.words), op: op})
	e.words = append(e.words, 0)
	e.storeOperand(0, 0)
	e.depth = 1
	return e.ReturnValue()
}

// NewStackEmitter 创建固定大小的栈帧。主程序和函数使用同一套槽位布局。
func NewStackEmitter(globals, locals, params, maxStack int, function bool) (*StackEmitter, error) {
	return newStackEmitter(globals, locals, params, maxStack, function, false, false, nil, 0)
}

// NewStackEmitterWithGC creates an M11b frame.  x24 points at a linked shadow
// record containing only the references selected by the compiler at the most
// recent safepoint; integer-looking values are never inspected by the GC.
func NewStackEmitterWithGC(globals, locals, params, maxStack int, function, stress bool) (*StackEmitter, error) {
	return newStackEmitter(globals, locals, params, maxStack, function, true, stress, nil, 0)
}

// NewStackEmitterWithClosures reserves precise hidden frame slots for the
// incoming environment and for lazily-created Cells belonging to capturable
// locals. The hidden environment is passed in x25, outside the M15a value ABI.
func NewStackEmitterWithClosures(globals, locals, params, maxStack int, function, stress bool, capturedLocals []int, upvalues int) (*StackEmitter, error) {
	return newStackEmitter(globals, locals, params, maxStack, function, true, stress, capturedLocals, upvalues)
}

func newStackEmitter(globals, locals, params, maxStack int, function, gc, stress bool, capturedLocals []int, upvalues int) (*StackEmitter, error) {
	if globals < 0 || globals > MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M5 global slot count %d exceeds limit %d", globals, MaxGlobalSlots)
	}
	if maxStack < 0 || maxStack > MaxM5OperandSlots {
		return nil, fmt.Errorf("AOT M5 operand stack depth %d exceeds limit %d", maxStack, MaxM5OperandSlots)
	}
	if function {
		if locals < params || locals > MaxM4Locals || params < 0 || params > MaxM15Parameters {
			return nil, fmt.Errorf("AOT M5 invalid native frame (locals=%d parameters=%d)", locals, params)
		}
	} else if locals != 0 || params != 0 {
		return nil, fmt.Errorf("AOT M5 main cannot have locals or parameters")
	}
	e := &StackEmitter{maxStack: maxStack, locals: locals, globals: globals, functionMode: function, gcEnabled: gc, gcStress: stress, environmentSlot: -1, cellSlots: make(map[int]int), upvalueCount: upvalues, heapCapacity: M8DefaultHeapCapacity}
	e.frameSlots = locals + maxStack
	if function && (upvalues != 0 || len(capturedLocals) != 0) {
		e.environmentSlot = e.frameSlots
		e.frameSlots++
		for _, local := range capturedLocals {
			if local < 0 || local >= locals {
				return nil, fmt.Errorf("AOT M15b capturable local %d out of range", local)
			}
			if _, exists := e.cellSlots[local]; exists {
				continue
			}
			e.cellSlots[local] = e.frameSlots
			e.frameSlots++
		}
	}
	// 先保存 FP/LR，再将当前帧设为 x29；始终保持 SP 的 16 字节对齐。
	e.words = append(e.words, 0xA9BF7BFD, addImmX(29, 31, 0))
	slotBytes := 8 * e.frameSlots
	frameBytes := (slotBytes + 15) &^ 15
	if gc {
		e.gcRootCapacity = e.frameSlots + globals
		// prev + count + the maximum precise root vector, rounded for SP.
		gcBytes := (16 + 8*e.gcRootCapacity + 15) &^ 15
		frameBytes += gcBytes
		e.gcRecordOffset = frameBytes
	}
	for remaining := frameBytes; remaining > 0; {
		step := remaining
		if step > 4080 {
			step = 4080
		}
		e.words = append(e.words, subImmX(31, 31, uint32(step)))
		remaining -= step
	}
	if gc {
		if !function {
			e.words = append(e.words, movzX(24, 0))
		}
		e.frameAddr(e.gcRecordOffset)
		e.words = append(e.words, strX(24, 8, 0), strX(31, 8, 1), movX(24, 8))
	}
	if e.environmentSlot >= 0 {
		e.frameAddr(8 * (e.environmentSlot + 1))
		e.words = append(e.words, strX(25, 8, 0))
		for _, local := range capturedLocals {
			slot, exists := e.cellSlots[local]
			if !exists {
				continue
			}
			e.frameAddr(8 * (slot + 1))
			e.words = append(e.words, strX(31, 8, 0))
		}
	}
	if function {
		// M15a：前 8 个参数来自 x0..x7，其余参数位于入口 x29+16 起的
		// 调用方栈参数区。所有值均立即复制到 callee 独立 local slots，
		// 因而第一次可能触发 GC 的用户指令之前，引用已有稳定帧内位置。
		for i := 0; i < params; i++ {
			e.localAddr(i)
			if i < M15RegisterParameters {
				e.words = append(e.words, strX(uint32(i), 8, 0))
				continue
			}
			e.words = append(e.words,
				ldrX(9, 29, uint32(2+i-M15RegisterParameters)),
				strX(9, 8, 0),
			)
		}
	} else {
		// x20: 是否曾经执行 Pop；x22: 原生用户函数调用深度。
		e.words = append(e.words, movzX(20, 0), movzX(22, 0))
	}
	return e, nil
}

// SetGCRoots materializes the exact root vector for a safepoint. Slots are
// frame-relative logical slots: locals first, then operand-stack slots.
func (e *StackEmitter) SetGCRoots(frameSlots, globalSlots []int) error {
	if !e.gcEnabled {
		return nil
	}
	hidden := make([]int, 0, 1+len(e.cellSlots))
	if e.environmentSlot >= 0 {
		hidden = append(hidden, e.environmentSlot)
	}
	for slot := e.locals + e.maxStack; slot < e.frameSlots; slot++ {
		if slot != e.environmentSlot {
			hidden = append(hidden, slot)
		}
	}
	frameSlots = append(append([]int(nil), frameSlots...), hidden...)
	if len(frameSlots)+len(globalSlots) > e.gcRootCapacity {
		return fmt.Errorf("AOT M11b root vector exceeds frame capacity")
	}
	seenFrame := make(map[int]bool, len(frameSlots))
	seenGlobal := make(map[int]bool, len(globalSlots))
	rootIndex := 0
	for _, slot := range frameSlots {
		if slot < 0 || slot >= e.frameSlots || seenFrame[slot] {
			return fmt.Errorf("AOT M11b invalid frame root slot %d", slot)
		}
		seenFrame[slot] = true
		e.frameAddr(8 * (slot + 1))
		e.words = append(e.words, ldrX(9, 8, 0), strX(9, 24, uint32(2+rootIndex)))
		rootIndex++
	}
	for _, slot := range globalSlots {
		if slot < 0 || slot >= e.globals || seenGlobal[slot] {
			return fmt.Errorf("AOT M11b invalid global root slot %d", slot)
		}
		seenGlobal[slot] = true
		if err := e.globalAccess(9, slot, false); err != nil {
			return err
		}
		e.words = append(e.words, strX(9, 24, uint32(2+rootIndex)))
		rootIndex++
	}
	e.words = append(e.words, m8MovBits(9, uint64(rootIndex))...)
	e.words = append(e.words, strX(9, 24, 1))
	e.gcSafepoints = append(e.gcSafepoints, M11StackMap{
		NativeWord: uint32(len(e.words)), FrameSlots: append([]int(nil), frameSlots...),
		GlobalSlots: append([]int(nil), globalSlots...),
	})
	e.gcFrameRoots = append(e.gcFrameRoots[:0], frameSlots...)
	e.gcGlobalRoots = append(e.gcGlobalRoots[:0], globalSlots...)
	return nil
}

// ProtectGCTemporary publishes a newly allocated value before a subsequent
// allocation can collect. If the logical frame slot was already a root its
// shadow value is replaced; otherwise one temporary root is appended.
func (e *StackEmitter) ProtectGCTemporary(frameSlot int, reg uint32) error {
	if !e.gcEnabled {
		return nil
	}
	for i, slot := range e.gcFrameRoots {
		if slot == frameSlot {
			e.words = append(e.words, strX(reg, 24, uint32(2+i)))
			e.gcSafepoints = append(e.gcSafepoints, M11StackMap{NativeWord: uint32(len(e.words)), FrameSlots: append([]int(nil), e.gcFrameRoots...), GlobalSlots: append([]int(nil), e.gcGlobalRoots...)})
			return nil
		}
	}
	count := len(e.gcFrameRoots) + len(e.gcGlobalRoots)
	if count >= e.gcRootCapacity {
		return fmt.Errorf("AOT M11b has no temporary root capacity")
	}
	e.words = append(e.words, strX(reg, 24, uint32(2+count)))
	e.words = append(e.words, m8MovBits(9, uint64(count+1))...)
	e.words = append(e.words, strX(9, 24, 1))
	frameRoots := append(append([]int(nil), e.gcFrameRoots...), frameSlot)
	e.gcSafepoints = append(e.gcSafepoints, M11StackMap{NativeWord: uint32(len(e.words)), FrameSlots: frameRoots, GlobalSlots: append([]int(nil), e.gcGlobalRoots...)})
	return nil
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
	// __DATA follows a page-rounded, variable-size __TEXT. Record both words
	// and patch them after code and constants have reached their final size.
	e.globalAccesses = append(e.globalAccesses, globalAccessFixup{
		word: len(e.words), reg: reg, slot: slot, store: store,
	})
	e.words = append(e.words, 0, 0)
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
	return e.setLocal(slot, false)
}

func (e *StackEmitter) DefineLocal(slot int) error {
	return e.setLocal(slot, true)
}

func (e *StackEmitter) setLocal(slot int, fresh bool) error {
	if !e.functionMode || slot < 0 || slot >= e.locals {
		return fmt.Errorf("AOT M5 local slot %d out of range", slot)
	}
	if err := e.ensurePop("OpSetLocal", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	if cellSlot, captured := e.cellSlots[slot]; captured {
		if fresh {
			e.frameAddr(8 * (cellSlot + 1))
			e.words = append(e.words, strX(31, 8, 0))
		} else {
			e.frameAddr(8 * (cellSlot + 1))
			e.words = append(e.words, ldrX(10, 8, 0))
			toRaw := len(e.words)
			e.words = append(e.words, 0, strX(9, 10, 1))
			done := len(e.words)
			e.words = append(e.words, 0)
			raw := len(e.words)
			var err error
			e.words[toRaw], err = checkedCBZ(10, toRaw, raw)
			if err != nil {
				return err
			}
			e.localAddr(slot)
			e.words = append(e.words, strX(9, 8, 0))
			after := len(e.words)
			e.words[done], err = branch26(0x14000000, done, after)
			if err != nil {
				return err
			}
			e.depth--
			return nil
		}
	}
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
	if cellSlot, captured := e.cellSlots[slot]; captured {
		e.frameAddr(8 * (cellSlot + 1))
		e.words = append(e.words, ldrX(10, 8, 0))
		toRaw := len(e.words)
		e.words = append(e.words, 0, ldrX(9, 10, 1))
		done := len(e.words)
		e.words = append(e.words, 0)
		raw := len(e.words)
		var err error
		e.words[toRaw], err = checkedCBZ(10, toRaw, raw)
		if err != nil {
			return err
		}
		e.localAddr(slot)
		e.words = append(e.words, ldrX(9, 8, 0))
		after := len(e.words)
		e.words[done], err = branch26(0x14000000, done, after)
		if err != nil {
			return err
		}
	} else {
		e.localAddr(slot)
		e.words = append(e.words, ldrX(9, 8, 0))
	}
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
	if params < 0 || params > MaxM15Parameters || e.depth < params {
		return fmt.Errorf("AOT M5 invalid native call params=%d depth=%d", params, e.depth)
	}
	base := e.depth - params
	if returns && base >= e.maxStack {
		return fmt.Errorf("AOT M5 return stack limit exceeded")
	}
	// 先检查调用深度；溢出路径直接退出，不需要构造参数区。
	e.words = append(e.words, addImmX(22, 22, 1), cmpImmX(22, MaxM4CallDepth))
	e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, 9)) // LS skips the long branch
	e.overflowSites = append(e.overflowSites, len(e.words))
	e.words = append(e.words, 0) // B has +/-128 MiB reach

	// 第 9 个及后续参数按从左到右的槽位顺序写入调用方临时区域。
	// 区域向 16 字节取整，BL 前后 SP 始终保持 16 字节对齐。
	stackParams := params - M15RegisterParameters
	if stackParams < 0 {
		stackParams = 0
	}
	stackBytes := (stackParams*8 + 15) &^ 15
	if stackBytes != 0 {
		e.words = append(e.words, subImmX(31, 31, uint32(stackBytes)))
		for i := M15RegisterParameters; i < params; i++ {
			e.loadOperand(base+i, 9)
			e.words = append(e.words, strX(9, 31, uint32(i-M15RegisterParameters)))
		}
	}
	for i := 0; i < params && i < M15RegisterParameters; i++ {
		e.loadOperand(base+i, uint32(i))
	}
	e.functionCalls = append(e.functionCalls, functionCallFixup{word: len(e.words), functionID: functionID})
	e.words = append(e.words, 0)
	if stackBytes != 0 {
		e.words = append(e.words, addImmX(31, 31, uint32(stackBytes)))
	}
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
	if e.gcEnabled {
		e.words = append(e.words, ldrX(24, 24, 0))
	}
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
	stringLenCalls := append([]int(nil), e.stringLenCalls...)
	stringCompareCalls := append([]int(nil), e.stringCompareCalls...)
	stringConstSites := append([]int(nil), e.stringConstSites...)
	m12Calls := append([]m12CallSite(nil), e.m12Calls...)
	m12ZeroSites := append([]int(nil), e.m12ZeroSites...)
	m12BoundsSites := append([]int(nil), e.m12BoundsSites...)
	m12ArrayFailSites := append([]int(nil), e.m12ArrayFailSites...)
	if e.lastPopped && (e.lastPopString || e.lastPopNullable) {
		// M7 的表达式结果可能来自不同分支，退出前按最后的实际运行时标签输出。
		jumpEmpty := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(21, M13NilOutputKind))
		jumpNil := len(code)
		code = append(code, 0)
		jumpString := -1
		if e.lastPopString {
			code = append(code, cmpImmX(21, m7StringKind))
			jumpString = len(code)
			code = append(code, 0)
		}
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

		stringStart, stringExit := -1, -1
		if e.lastPopString {
			stringStart = len(code)
			code = append(code, movX(0, 19))
			stringCalls = append(stringCalls, len(code))
			code = append(code, 0)
			stringExit = len(code)
			code = append(code, 0)
		}

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
		float64Exit := len(code)
		code = append(code, 0)

		nilStart := len(code)
		code = append(code, subImmX(31, 31, 16))
		for index, ch := range []byte("mu\n") {
			code = append(code, movzW(9, uint16(ch)), 0x39000000|uint32(index)<<10|31<<5|9)
		}
		code = append(code,
			movzX(0, 1), addImmX(1, 31, 0), movzX(2, 3),
			movzX(16, m8DarwinSYSWrite), svc(0x80), addImmX(31, 31, 16),
		)
		exitStart := len(code)

		var err error
		if code[jumpEmpty], err = checkedCBZ(20, jumpEmpty, exitStart); err != nil {
			return nil, err
		}
		code[jumpNil] = branchCond(jumpNil, nilStart, 0)
		if jumpString >= 0 {
			code[jumpString] = branchCond(jumpString, stringStart, 0)
		}
		code[jumpFloat32] = branchCond(jumpFloat32, float32Start, 0)
		code[jumpFloat64] = branchCond(jumpFloat64, float64Start, 0)
		code[jumpBool] = branchCond(jumpBool, boolStart, 0)
		exits := []int{intExit, boolExit, float32Exit, float64Exit}
		if stringExit >= 0 {
			exits = append(exits, stringExit)
		}
		for _, at := range exits {
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
	if e.m12Enabled {
		if e.nativeFSEnabled {
			e.nativeFSCalls = append(e.nativeFSCalls, m14NativeCall{word: len(code), op: "cleanup"})
			code = append(code, 0)
		}
		if e.gcTelemetry {
			code = append(code, movX(0, 23), movX(1, 24))
			m12Calls = append(m12Calls, m12CallSite{at: len(code), kind: m12Telemetry})
			code = append(code, 0)
		}
		code = append(code, movX(0, 23))
		m12Calls = append(m12Calls, m12CallSite{at: len(code), kind: m12Destroy})
		code = append(code, 0)
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
	stackMaps := make([]M11StackMap, 0, len(e.gcSafepoints))
	for _, sm := range e.gcSafepoints {
		sm.FrameID = 0
		stackMaps = append(stackMaps, sm)
	}
	allCalls := append([]functionCallFixup(nil), e.functionCalls...)
	closureTargets := append([]closureTargetFixup(nil), e.closureTargets...)
	closureZeroSites := append([]int(nil), e.closureZeroSites...)
	nullableNilSites := append([]int(nil), e.nullableNilSites...)
	globalAccesses := append([]globalAccessFixup(nil), e.globalAccesses...)
	nativeFSCalls := append([]m14NativeCall(nil), e.nativeFSCalls...)
	overflows := append([]int(nil), e.overflowSites...)
	traps := append([]m6TrapSite(nil), e.trapSites...)
	for i, fn := range functions {
		if fn == nil || !fn.functionMode {
			return nil, fmt.Errorf("AOT M5 invalid native function %d", i)
		}
		functionStarts[i] = len(code)
		code = append(code, fn.words...)
		for _, sm := range fn.gcSafepoints {
			sm.FrameID = uint32(i + 1)
			sm.NativeWord += uint32(functionStarts[i])
			stackMaps = append(stackMaps, sm)
		}
		for _, at := range fn.printCalls {
			calls = append(calls, functionStarts[i]+at)
		}
		for _, at := range fn.boolCalls {
			boolCalls = append(boolCalls, functionStarts[i]+at)
		}
		for _, at := range fn.stringCalls {
			stringCalls = append(stringCalls, functionStarts[i]+at)
		}
		for _, at := range fn.stringLenCalls {
			stringLenCalls = append(stringLenCalls, functionStarts[i]+at)
		}
		for _, at := range fn.stringCompareCalls {
			stringCompareCalls = append(stringCompareCalls, functionStarts[i]+at)
		}
		for _, at := range fn.stringConstSites {
			stringConstSites = append(stringConstSites, functionStarts[i]+at)
		}
		for _, call := range fn.m12Calls {
			call.at += functionStarts[i]
			m12Calls = append(m12Calls, call)
		}
		for _, at := range fn.m12ZeroSites {
			m12ZeroSites = append(m12ZeroSites, functionStarts[i]+at)
		}
		for _, at := range fn.m12BoundsSites {
			m12BoundsSites = append(m12BoundsSites, functionStarts[i]+at)
		}
		for _, at := range fn.m12ArrayFailSites {
			m12ArrayFailSites = append(m12ArrayFailSites, functionStarts[i]+at)
		}
		for _, fx := range fn.floatCalls {
			fx.word += functionStarts[i]
			floatCalls = append(floatCalls, fx)
		}
		for _, fx := range fn.functionCalls {
			fx.word += functionStarts[i]
			allCalls = append(allCalls, fx)
		}
		for _, fx := range fn.closureTargets {
			fx.word += functionStarts[i]
			closureTargets = append(closureTargets, fx)
		}
		for _, at := range fn.closureZeroSites {
			closureZeroSites = append(closureZeroSites, functionStarts[i]+at)
		}
		for _, at := range fn.nullableNilSites {
			nullableNilSites = append(nullableNilSites, functionStarts[i]+at)
		}
		for _, fx := range fn.globalAccesses {
			fx.word += functionStarts[i]
			globalAccesses = append(globalAccesses, fx)
		}
		for _, call := range fn.nativeFSCalls {
			call.word += functionStarts[i]
			nativeFSCalls = append(nativeFSCalls, call)
		}
		for _, at := range fn.overflowSites {
			overflows = append(overflows, functionStarts[i]+at)
		}
		for _, trap := range fn.trapSites {
			trap.word += functionStarts[i]
			traps = append(traps, trap)
		}
	}
	if len(overflows) > 0 || len(traps) > 0 || len(closureZeroSites) > 0 {
		errExit := len(code)
		code = append(code, movzX(0, 1), movzX(16, 1), svc(0x80))
		for _, at := range overflows {
			word, err := branch26(0x14000000, at, errExit)
			if err != nil {
				return nil, fmt.Errorf("AOT M5 call-depth guard target out of range: %w", err)
			}
			code[at] = word
		}
		for _, trap := range traps {
			if trap.cond == ^uint32(0) {
				word, err := branch26(0x14000000, trap.word, errExit)
				if err != nil {
					return nil, fmt.Errorf("AOT M6 trap branch target out of range: %w", err)
				}
				code[trap.word] = word
				continue
			}
			delta := errExit - trap.word
			if delta < -(1<<18) || delta >= 1<<18 {
				return nil, fmt.Errorf("AOT M6 trap branch target out of range")
			}
			code[trap.word] = branchCond(trap.word, errExit, trap.cond)
		}
		for _, at := range closureZeroSites {
			word, err := branch26(0x14000000, at, errExit)
			if err != nil {
				return nil, fmt.Errorf("AOT M15b invalid function trap target out of range: %w", err)
			}
			code[at] = word
		}
	}
	if e.m12Enabled {
		m12ZeroSites = append(m12ZeroSites, e.m12InitFail)
		allocationExit := len(code)
		code = appendM12Failure(code, "M12 allocation failed\n", &m12Calls, e.gcTelemetry)
		boundsExit := len(code)
		code = appendM12Failure(code, "M12 array index out of bounds\n", &m12Calls, e.gcTelemetry)
		capacityExit := len(code)
		code = appendM12Failure(code, "M12 invalid array capacity\n", &m12Calls, e.gcTelemetry)
		lengthExit := len(code)
		code = appendM12Failure(code, "M12 invalid array length\n", &m12Calls, e.gcTelemetry)
		nullableExit := len(code)
		code = appendM12Failure(code, "M13 nullable value is nil\n", &m12Calls, e.gcTelemetry)
		arrayFailureDispatch := len(code)
		code = append(code, cmpImmX(1, 2))
		allocationBranch := len(code)
		code = append(code, 0)
		code = append(code, cmpImmX(1, 3))
		lengthBranch := len(code)
		code = append(code, 0)
		capacityBranch := len(code)
		code = append(code, 0)
		for _, at := range m12ZeroSites {
			word, err := branch26(0x14000000, at, allocationExit)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
		for _, at := range m12BoundsSites {
			word, err := branch26(0x14000000, at, boundsExit)
			if err != nil {
				return nil, fmt.Errorf("AOT M12 bounds failure target out of range: %w", err)
			}
			code[at] = word
		}
		var err error
		code[allocationBranch] = branchCond(allocationBranch, allocationExit, 0)
		code[lengthBranch] = branchCond(lengthBranch, lengthExit, 0)
		if code[capacityBranch], err = branch26(0x14000000, capacityBranch, capacityExit); err != nil {
			return nil, err
		}
		for _, at := range m12ArrayFailSites {
			word, branchErr := branch26(0x14000000, at, arrayFailureDispatch)
			if branchErr != nil {
				return nil, branchErr
			}
			code[at] = word
		}
		for _, at := range nullableNilSites {
			word, err := branch26(0x14000000, at, nullableExit)
			if err != nil {
				return nil, fmt.Errorf("AOT M13 nil failure target out of range: %w", err)
			}
			code[at] = word
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
	for _, fx := range closureTargets {
		if fx.functionID < 0 || fx.functionID >= len(functionStarts) {
			return nil, fmt.Errorf("AOT M15b unknown closure function %d", fx.functionID)
		}
		pc := textVMAddr + entryOffset + uint64(fx.word)*4
		target := textVMAddr + entryOffset + uint64(functionStarts[fx.functionID])*4
		first, err := adrpX(9, pc, target)
		if err != nil {
			return nil, err
		}
		code[fx.word] = first
		code[fx.word+1] = addImmX(9, 9, uint32(target&0xfff))
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
	// M10 probe runtime uses PC-relative calls and remains isolated from M12.
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
	if e.m12Enabled {
		runtimeStart := len(code)
		words, entries, err := M12ArrayGCRuntimeWords(e.gcStress)
		if err != nil {
			return nil, err
		}
		code = append(code, words...)
		for _, call := range m12Calls {
			target := entries.Alloc
			switch call.kind {
			case m12Init:
				target = entries.Init
			case m12Destroy:
				target = entries.Destroy
			case m12Alloc:
			case m12ArrayMake:
				target = entries.ArrayMake
			case m12ArrayWithCapacity:
				target = entries.ArrayWithCapacity
			case m12ArrayPush:
				target = entries.ArrayPush
			case m12ArrayReserve:
				target = entries.ArrayReserve
			case m12ArrayClone:
				target = entries.ArrayClone
			case m12ArrayTruncate:
				target = entries.ArrayTruncate
			case m12ArrayAppendAll:
				target = entries.ArrayAppendAll
			case m12UTF8Valid:
				target = entries.UTF8Valid
			case m12Telemetry:
				target = entries.Telemetry
			default:
				return nil, fmt.Errorf("AOT M12 unknown runtime function")
			}
			word, err := branch26(0x94000000, call.at, runtimeStart+target)
			if err != nil {
				return nil, err
			}
			code[call.at] = word
		}
		if len(nativeFSCalls) != 0 {
			nativeStart := len(code)
			emptyID := 0
			for id, value := range strings {
				if value == "" {
					emptyID = id
					break
				}
			}
			nativeWords, nativeEntries, nativeExternal, err := appendM14NativeFSRuntime(uint64(emptyID<<1 | 1))
			if err != nil {
				return nil, err
			}
			code = append(code, nativeWords...)
			for _, call := range nativeFSCalls {
				target, ok := nativeEntries[call.op]
				if !ok {
					return nil, fmt.Errorf("AOT M14 unknown native operation %q", call.op)
				}
				word, branchErr := branch26(0x94000000, call.word, nativeStart+target)
				if branchErr != nil {
					return nil, branchErr
				}
				code[call.word] = word
			}
			for _, call := range nativeExternal {
				target := runtimeStart + entries.ArrayWithCapacity
				word, branchErr := branch26(0x94000000, nativeStart+call, target)
				if branchErr != nil {
					return nil, branchErr
				}
				code[nativeStart+call] = word
			}
		}
	}
	var constants []byte
	if len(strings) != 0 {
		var err error
		constants, err = stringImage(strings)
		if err != nil {
			return nil, err
		}
	}
	if len(stringCalls) != 0 || len(stringLenCalls) != 0 || len(stringCompareCalls) != 0 || len(stringConstSites) != 0 {
		printStart, lenStart, compareStart := -1, -1, -1
		if len(stringCalls) != 0 {
			printStart = len(code)
			code = appendPrintStaticString(code)
		}
		if len(stringLenCalls) != 0 {
			lenStart = len(code)
			code = appendStaticStringLen(code)
		}
		if len(stringCompareCalls) != 0 {
			compareStart = len(code)
			compareWords, compareErr := appendStringCompare()
			if compareErr != nil {
				return nil, compareErr
			}
			code = append(code, compareWords...)
		}
		// 先确定字符串表的最终文件偏移，避免将静态绝对地址写入机器码。
		constOff := (entryOffset + uint64(len(code))*4 + 7) &^ 7
		constVA := textVMAddr + constOff
		var err error
		for _, start := range []int{printStart, lenStart, compareStart} {
			if start < 0 {
				continue
			}
			pc := textVMAddr + entryOffset + uint64(start)*4
			if code[start], err = adrpX(9, pc, constVA); err != nil {
				return nil, err
			}
			code[start+1] = addImmX(9, 9, uint32(constVA&0xfff))
		}
		for _, start := range stringConstSites {
			pc := textVMAddr + entryOffset + uint64(start)*4
			if code[start], err = adrpX(10, pc, constVA); err != nil {
				return nil, err
			}
			code[start+1] = addImmX(10, 10, uint32(constVA&0xfff))
		}
		for _, at := range stringCalls {
			word, err := branch26(0x94000000, at, printStart)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
		for _, at := range stringLenCalls {
			word, err := branch26(0x94000000, at, lenStart)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
		for _, at := range stringCompareCalls {
			word, err := branch26(0x94000000, at, compareStart)
			if err != nil {
				return nil, err
			}
			code[at] = word
		}
	}
	if e.nativeFSEnabled {
		constOff := (entryOffset + uint64(len(code))*4 + 7) &^ 7
		constVA := textVMAddr + constOff
		pc := textVMAddr + entryOffset + uint64(e.nativeConstSite)*4
		var err error
		if code[e.nativeConstSite], err = adrpX(28, pc, constVA); err != nil {
			return nil, err
		}
		code[e.nativeConstSite+1] = addImmX(28, 28, uint32(constVA&0xfff))
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
	if len(stackMaps) != 0 {
		for i := range stackMaps {
			stackMaps[i].SafepointID = uint32(i + 1)
		}
		for len(constants)%8 != 0 {
			constants = append(constants, 0)
		}
		encoded, err := M11EncodeStackMaps(stackMaps)
		if err != nil {
			return nil, err
		}
		constants = append(constants, encoded...)
	}
	if len(globalAccesses) != 0 {
		dataVA := textVMAddr + textSegmentSize(uint64(len(code))*4, uint64(len(constants)))
		for _, fx := range globalAccesses {
			addr := dataVA + uint64(fx.slot)*8
			pc := textVMAddr + entryOffset + uint64(fx.word)*4
			page, err := adrpX(8, pc, addr)
			if err != nil {
				return nil, err
			}
			access := ldrX(fx.reg, 8, uint32((addr&0xfff)/8))
			if fx.store {
				access = strX(fx.reg, 8, uint32((addr&0xfff)/8))
			}
			code[fx.word], code[fx.word+1] = page, access
		}
	}
	return ExecutableWordsWithGlobalsAndConst(code, e.globals, constants)
}
