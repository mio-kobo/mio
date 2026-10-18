package darwinarm64

import "fmt"

// M13 keeps the one-word M15 ABI. Nullable heap references use a null pointer
// directly; nullable scalar payloads use this M8 object so every scalar bit
// pattern, including zero and every IEEE-754 pattern, remains representable.
const (
	M13NullableTypeID = 106
	m13NullableKind   = 7
	M13NilOutputKind  = 7
)

// BoxNullableOperand replaces a raw scalar operand with a GC-managed box.
func (e *StackEmitter) BoxNullableOperand(slot int) error {
	if !e.m12Enabled || slot < 0 || slot >= e.depth {
		return fmt.Errorf("AOT M13 invalid nullable box slot %d", slot)
	}
	if err := e.m12Alloc(M13NullableTypeID, 16); err != nil {
		return err
	}
	e.words = append(e.words, movX(12, 0))
	e.m12StoreImmediate(12, 0, m13NullableKind)
	e.loadOperand(slot, 9)
	e.words = append(e.words, strX(9, 12, 1))
	e.storeOperand(slot, 12)
	return nil
}

// UnboxNullableOperand loads a non-nil scalar payload in place. A nil value on
// a path that promised flow narrowing is invalid bytecode and takes the common
// checked-operation failure path instead of being confused with scalar zero.
func (e *StackEmitter) UnboxNullableOperand(slot int) error {
	if slot < 0 || slot >= e.depth {
		return fmt.Errorf("AOT M13 invalid nullable unbox slot %d", slot)
	}
	e.loadOperand(slot, 9)
	e.words = append(e.words, cmpZeroX(9))
	e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, 1))
	e.nullableNilSites = append(e.nullableNilSites, len(e.words))
	e.words = append(e.words, 0, ldrX(10, 9, 0), cmpImmX(10, m13NullableKind))
	e.m6TrapIf(1) // NE
	e.words = append(e.words, ldrX(9, 9, 1))
	e.storeOperand(slot, 9)
	return nil
}

// UnboxScalarOperand tolerates a flow-narrowed scalar whose representation
// still comes from a nullable predecessor. It validates the candidate against
// the live heap and nullable header before dereferencing; ordinary integer bit
// patterns therefore remain ordinary integers.
func (e *StackEmitter) UnboxScalarOperand(slot int) error {
	if slot < 0 || slot >= e.depth {
		return fmt.Errorf("AOT M13 invalid scalar unbox slot %d", slot)
	}
	e.loadOperand(slot, 9)
	e.words = append(e.words, cmpZeroX(9))
	rawSites := []int{len(e.words)}
	e.words = append(e.words, 0)
	e.words = append(e.words, ldrX(10, 23, 6), addImmX(10, 10, M8ObjectHeaderSize), cmpX(9, 10))
	rawSites = append(rawSites, len(e.words))
	e.words = append(e.words, 0)
	e.words = append(e.words, ldrX(11, 23, 1), cmpX(9, 11))
	rawSites = append(rawSites, len(e.words))
	e.words = append(e.words, 0)
	e.words = append(e.words, subImmX(12, 9, M8ObjectHeaderSize), m9LdrW(13, 12, 0), cmpImmX(13, M13NullableTypeID))
	rawSites = append(rawSites, len(e.words))
	e.words = append(e.words, 0)
	e.words = append(e.words, ldrX(13, 12, 1), cmpImmX(13, 16))
	rawSites = append(rawSites, len(e.words))
	e.words = append(e.words, 0, ldrX(9, 9, 1))
	raw := len(e.words)
	conds := []uint32{0, 3, 2, 1, 1} // EQ, LO, HS, NE, NE
	for i, at := range rawSites {
		word, err := branchCondChecked(at, raw, conds[i])
		if err != nil {
			return err
		}
		e.words[at] = word
	}
	e.storeOperand(slot, 9)
	return nil
}

// IsNil preserves VM semantics for every raw bit pattern. Non-nullable values
// are always false; only representations known to be nil-capable inspect zero.
func (e *StackEmitter) IsNil(nilCapable bool) error {
	if err := e.ensurePop("OpIsNil", 1); err != nil {
		return err
	}
	if nilCapable {
		e.loadOperand(e.depth-1, 9)
		e.words = append(e.words, cmpZeroX(9), csetW(9, 0))
	} else {
		e.words = append(e.words, movzX(9, 0))
	}
	e.storeOperand(e.depth-1, 9)
	return nil
}

// ChantNil emits the VM spelling of KindNil without introducing a static
// string ID (zero is a valid empty-string ID).
func (e *StackEmitter) ChantNil() error {
	if err := e.ensurePop("OpChant", 1); err != nil {
		return err
	}
	e.words = append(e.words, subImmX(31, 31, 16))
	for index, ch := range []byte("mu\n") {
		e.words = append(e.words, movzW(9, uint16(ch)), 0x39000000|uint32(index)<<10|31<<5|9)
	}
	e.words = append(e.words,
		movzX(0, 1), addImmX(1, 31, 0), movzX(2, 3),
		movzX(16, m8DarwinSYSWrite), svc(0x80), addImmX(31, 31, 16),
	)
	e.depth--
	return nil
}

func (e *StackEmitter) PopNil() error {
	if err := e.ensurePop("OpPop", 1); err != nil {
		return err
	}
	if !e.functionMode {
		e.words = append(e.words, movzX(19, 0), movzX(20, 1), movzX(21, M13NilOutputKind))
		e.lastPopped = true
		e.lastPopNullable = true
	}
	e.depth--
	return nil
}

// PopNullable records either nil or the unboxed scalar with its ordinary
// output tag. It is used for top-level expression results; chant itself does
// not accept a nullable static type.
func (e *StackEmitter) PopNullable(outputKind uint8) error {
	if err := e.ensurePop("OpPop", 1); err != nil {
		return err
	}
	if !e.functionMode {
		e.loadOperand(e.depth-1, 9)
		toNil := len(e.words)
		e.words = append(e.words, 0, ldrX(19, 9, 1), movzX(21, uint16(outputKind)))
		doneJump := len(e.words)
		e.words = append(e.words, 0)
		nilStart := len(e.words)
		e.words = append(e.words, movzX(19, 0), movzX(21, M13NilOutputKind))
		done := len(e.words)
		var err error
		if e.words[toNil], err = checkedCBZ(9, toNil, nilStart); err != nil {
			return err
		}
		if e.words[doneJump], err = branch26(0x14000000, doneJump, done); err != nil {
			return err
		}
		e.words = append(e.words, movzX(20, 1))
		e.lastPopped = true
		e.lastPopNullable = true
		e.lastPopString = e.lastPopString || outputKind == 5
	}
	e.depth--
	return nil
}

// M13NullableGCProbe validates the box header and precise-root scanner without
// involving bytecode analysis or generated frame maps.
func M13NullableGCProbe() ([]byte, error) {
	b := newM8Assembler()
	b.emit(m8MovBits(0, M8MaxHeapCapacity)...)
	b.bl(m8InitLabel)
	b.cbz(0, "m13.probe.fail")
	b.emit(movX(19, 0), movX(0, 19), movzX(1, M13NullableTypeID), movzX(2, 16))
	b.bl(m11AutoAllocEntry)
	b.cbz(0, "m13.probe.fail")
	b.emit(movX(20, 0), movzX(9, m13NullableKind), strX(9, 20, 0))
	b.emit(subImmX(10, 20, M8ObjectHeaderSize), m9LdrW(11, 10, 0), cmpImmX(11, M13NullableTypeID))
	b.bc(1, "m13.probe.header")
	b.emit(ldrX(11, 10, 1), cmpImmX(11, 16))
	b.bc(1, "m13.probe.header")
	b.emit(subImmX(31, 31, 32), strX(31, 31, 0), movzX(9, 1), strX(9, 31, 1), strX(20, 31, 2))
	b.emit(movX(0, 19), movX(1, 31))
	b.bl(m11ObjectCollectEntry)
	b.emit(m8Movn(9, 0), m8Cmp(0, 9))
	b.bc(0, "m13.probe.fail.frame")
	b.emit(addImmX(31, 31, 32))
	m8WriteLiteral(b, "M13 nullable PASS\n")
	b.emit(movzX(0, 0), movzX(16, 1), svc(0x80))
	b.label("m13.probe.fail.frame")
	b.emit(addImmX(31, 31, 32))
	b.label("m13.probe.fail")
	m8WriteLiteral(b, "M13 nullable FAIL\n")
	b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))
	b.label("m13.probe.header")
	m8WriteLiteral(b, "M13 nullable HEADER\n")
	b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))
	appendM11ObjectRuntime(b, false)
	appendM10Sweep(b)
	appendM10Alloc(b)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, err
	}
	return ExecutableWords(words)
}
