package darwinarm64

import "fmt"

const (
	M15ClosureTypeID     = 103
	M15EnvironmentTypeID = 104
	M15CellTypeID        = 105
	m15ClosureKind       = 4
	m15EnvironmentKind   = 5
	m15CellKind          = 6
	m15ClosurePayload    = 48
)

// ClosureCapture describes one CompiledFunction.Captures entry after static
// type analysis has determined whether the shared Cell value is a GC pointer.
type ClosureCapture struct {
	Local     bool
	Index     int
	Reference bool
}

func blrX(rn uint32) uint32 { return 0xD63F0000 | rn<<5 }

func (e *StackEmitter) captureLocalCell(local int, reference bool) error {
	cellSlot, ok := e.cellSlots[local]
	if !ok {
		return fmt.Errorf("AOT M15b local %d has no Cell slot", local)
	}
	e.frameAddr(8 * (cellSlot + 1))
	e.words = append(e.words, ldrX(10, 8, 0))
	doneBranch := len(e.words)
	e.words = append(e.words, 0)
	if err := e.m12Alloc(M15CellTypeID, 16); err != nil {
		return err
	}
	flags := uint64(m15CellKind)
	if reference {
		flags |= 1 << 8
	}
	e.m12StoreImmediate(0, 0, flags)
	e.localAddr(local)
	e.words = append(e.words, ldrX(9, 8, 0), strX(9, 0, 1))
	e.frameAddr(8 * (cellSlot + 1))
	e.words = append(e.words, strX(0, 8, 0))
	if err := e.ProtectGCTemporary(cellSlot, 0); err != nil {
		return err
	}
	done := len(e.words)
	e.words[doneBranch] = cbnzX(10, doneBranch, done)
	return nil
}

// NewClosure allocates an immutable function identity plus a separately
// scanned Environment. Every captured binding is a shared mutable Cell.
func (e *StackEmitter) NewClosure(functionID, parameters int, returns bool, captures []ClosureCapture) error {
	if err := e.ensurePush(); err != nil {
		return err
	}
	if !e.m12Enabled {
		return fmt.Errorf("AOT M15b closure runtime is not enabled")
	}
	for _, capture := range captures {
		if capture.Local {
			if err := e.captureLocalCell(capture.Index, capture.Reference); err != nil {
				return err
			}
		} else if capture.Index < 0 || capture.Index >= e.upvalueCount {
			return fmt.Errorf("AOT M15b upvalue capture %d out of range", capture.Index)
		}
	}

	base := e.depth
	if len(captures) != 0 {
		if err := e.m12Alloc(M15EnvironmentTypeID, 16+8*len(captures)); err != nil {
			return err
		}
		e.words = append(e.words, movX(12, 0))
		e.m12StoreImmediate(12, 0, m15EnvironmentKind)
		e.m12StoreImmediate(12, 8, uint64(len(captures)))
		for index, capture := range captures {
			if capture.Local {
				cellSlot := e.cellSlots[capture.Index]
				e.frameAddr(8 * (cellSlot + 1))
				e.words = append(e.words, ldrX(9, 8, 0))
			} else {
				e.words = append(e.words, ldrX(9, 25, uint32(2+capture.Index)))
			}
			e.words = append(e.words, strX(9, 12, uint32(2+index)))
		}
		e.storeOperand(base, 12)
		if err := e.ProtectGCTemporary(e.locals+base, 12); err != nil {
			return err
		}
	} else {
		e.words = append(e.words, movzX(12, 0))
		e.storeOperand(base, 12)
	}

	if err := e.m12Alloc(M15ClosureTypeID, m15ClosurePayload); err != nil {
		return err
	}
	e.words = append(e.words, movX(11, 0))
	e.m12StoreImmediate(11, 0, m15ClosureKind)
	e.m12StoreImmediate(11, 8, uint64(functionID))
	e.m12StoreImmediate(11, 16, uint64(parameters))
	returnWord := uint64(0)
	if returns {
		returnWord = 1
	}
	e.m12StoreImmediate(11, 24, returnWord)
	e.closureTargets = append(e.closureTargets, closureTargetFixup{word: len(e.words), functionID: functionID})
	e.words = append(e.words, 0, 0, strX(9, 11, 4))
	e.loadOperand(base, 9)
	e.words = append(e.words, strX(9, 11, 5))
	e.storeOperand(base, 11)
	e.depth++
	return nil
}

func (e *StackEmitter) GetUpvalue(index int) error {
	if index < 0 || index >= e.upvalueCount || e.environmentSlot < 0 {
		return fmt.Errorf("AOT M15b upvalue %d out of range", index)
	}
	if err := e.ensurePush(); err != nil {
		return err
	}
	e.words = append(e.words, ldrX(9, 25, uint32(2+index)), ldrX(9, 9, 1))
	e.storeOperand(e.depth, 9)
	e.depth++
	return nil
}

func (e *StackEmitter) SetUpvalue(index int) error {
	if index < 0 || index >= e.upvalueCount || e.environmentSlot < 0 {
		return fmt.Errorf("AOT M15b upvalue %d out of range", index)
	}
	if err := e.ensurePop("OpSetUpvalue", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, ldrX(9, 25, uint32(2+index)), strX(10, 9, 1))
	e.depth--
	return nil
}

// CallClosure preserves x25 in a private aligned caller slot. Stack arguments
// remain at sp+0 exactly as in M15a, so the hidden environment consumes no
// ordinary parameter register or stack-argument position.
func (e *StackEmitter) CallClosure(parameters int, returns bool) error {
	if parameters < 0 || parameters > MaxM15Parameters || e.depth < parameters+1 {
		return fmt.Errorf("AOT M15b invalid closure call params=%d depth=%d", parameters, e.depth)
	}
	base := e.depth - parameters - 1
	e.loadOperand(base, 9)
	e.words = append(e.words, cbnzX(9, len(e.words), len(e.words)+2))
	e.closureZeroSites = append(e.closureZeroSites, len(e.words))
	e.words = append(e.words, 0, ldrX(10, 9, 0), cmpImmX(10, m15ClosureKind))
	e.m6TrapIf(1) // NE
	e.words = append(e.words, ldrX(10, 9, 2), cmpImmX(10, uint32(parameters)))
	e.m6TrapIf(1)
	e.words = append(e.words, addImmX(22, 22, 1), cmpImmX(22, MaxM4CallDepth))
	e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, 9)) // LS skips long branch
	e.overflowSites = append(e.overflowSites, len(e.words))
	e.words = append(e.words, 0)

	stackParameters := parameters - M15RegisterParameters
	if stackParameters < 0 {
		stackParameters = 0
	}
	stackBytes := (stackParameters*8 + 15) &^ 15
	totalBytes := stackBytes + 16
	e.words = append(e.words, subImmX(31, 31, uint32(totalBytes)), strX(25, 31, uint32(stackBytes/8)))
	for i := M15RegisterParameters; i < parameters; i++ {
		e.loadOperand(base+1+i, 10)
		e.words = append(e.words, strX(10, 31, uint32(i-M15RegisterParameters)))
	}
	for i := 0; i < parameters && i < M15RegisterParameters; i++ {
		e.loadOperand(base+1+i, uint32(i))
	}
	e.loadOperand(base, 9)
	e.words = append(e.words, ldrX(25, 9, 5), ldrX(16, 9, 4), blrX(16))
	e.words = append(e.words, ldrX(25, 31, uint32(stackBytes/8)), addImmX(31, 31, uint32(totalBytes)), subImmX(22, 22, 1))
	if returns {
		e.storeOperand(base, 0)
	}
	e.depth = base
	if returns {
		e.depth++
	}
	return nil
}
