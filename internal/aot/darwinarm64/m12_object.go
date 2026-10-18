package darwinarm64

import "fmt"

// M12 复用 M8 对象头；载荷内保存可变长精确引用位图，供 M11b 扫描器接入。
const (
	M12StructTypeID  = 100
	M12ArrayTypeID   = 101
	M12StorageTypeID = 102
	M13EnumTypeID    = 107
	m12StructKind    = 1
	m12ArrayKind     = 2
	m12StorageKind   = 3
	m13EnumKind      = 8
	m12PayloadBase   = 32
)

type m12CallKind uint8

const (
	m12Init m12CallKind = iota
	m12Alloc
	m12Destroy
	m12ArrayMake
	m12ArrayWithCapacity
	m12ArrayPush
	m12ArrayReserve
	m12ArrayClone
	m12ArrayTruncate
	m12ArrayAppendAll
	m12UTF8Valid
	m12Telemetry
)

type m12CallSite struct {
	at   int
	kind m12CallKind
}

// BytesToString validates UTF-8, then clones the compact byte array. The
// cloned descriptor is the immutable dynamic-string backing object; invalid
// input yields the nullable nil representation (zero).
func (e *StackEmitter) BytesToString() error {
	if err := e.ensurePop("OpDataConvert bytes-to-string", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 0)
	e.m12Calls = append(e.m12Calls, m12CallSite{at: len(e.words), kind: m12UTF8Valid})
	e.words = append(e.words, 0, cmpZeroX(0))
	invalid := len(e.words)
	e.words = append(e.words, 0)
	if err := e.ArrayClone(1, false); err != nil {
		return err
	}
	done := len(e.words)
	e.words = append(e.words, 0)
	invalidTarget := len(e.words)
	e.words = append(e.words, movzX(9, 0))
	e.storeOperand(e.depth-1, 9)
	doneTarget := len(e.words)
	var err error
	e.words[invalid], err = branchCondChecked(invalid, invalidTarget, 0)
	if err != nil {
		return err
	}
	e.words[done], err = branch26(0x14000000, done, doneTarget)
	return err
}

func branchCondChecked(from, to int, cond uint32) (uint32, error) {
	distance := to - from
	if distance < -(1<<18) || distance >= 1<<18 {
		return 0, fmt.Errorf("conditional branch target out of range")
	}
	return branchCond(from, to, cond), nil
}

// EnableM12Runtime initializes the fixed-capacity heap. Ordinary object
// allocations use M11c collection and retry before reporting a real OOM.
func (e *StackEmitter) EnableM12Runtime(main bool) error {
	if e.m12Enabled {
		return nil
	}
	if main {
		if e.functionMode || e.depth != 0 {
			return fmt.Errorf("AOT M12 runtime must be enabled at main entry")
		}
		e.words = append(e.words, movzX(23, 0))
		e.words = append(e.words, m8MovBits(0, e.heapCapacity)...)
		e.m12InitCall = len(e.words)
		e.m12Calls = append(e.m12Calls, m12CallSite{at: e.m12InitCall, kind: m12Init})
		e.words = append(e.words, 0)
		e.words = append(e.words, cbnzX(0, len(e.words), len(e.words)+2))
		e.m12InitFail = len(e.words)
		e.words = append(e.words, 0)
		e.words = append(e.words, movX(23, 0))
	}
	e.m12Enabled = true
	return nil
}

func (e *StackEmitter) m12Alloc(typeID, payload int) error {
	if !e.m12Enabled || payload < 0 || payload > M8MaxHeapCapacity {
		return fmt.Errorf("AOT M12 invalid object allocation of %d bytes", payload)
	}
	e.words = append(e.words, movX(0, 23))
	e.words = append(e.words, m8MovBits(1, uint64(typeID))...)
	e.words = append(e.words, m8MovBits(2, uint64(payload))...)
	e.m12Calls = append(e.m12Calls, m12CallSite{at: len(e.words), kind: m12Alloc})
	e.words = append(e.words, 0)
	e.words = append(e.words, cbnzX(0, len(e.words), len(e.words)+2))
	e.m12ZeroSites = append(e.m12ZeroSites, len(e.words))
	e.words = append(e.words, 0)
	return nil
}

func (e *StackEmitter) m12StoreImmediate(base, offset uint32, value uint64) {
	e.words = append(e.words, m8MovBits(9, value)...)
	e.words = append(e.words, strX(9, base, offset/8))
}

// NewStructObject 将栈顶字段浅复制进不可变对象。
func (e *StackEmitter) NewStructObject(count int, refWords []uint64) error {
	if count < 0 || count > e.depth || len(refWords) != (count+63)/64 {
		return fmt.Errorf("AOT M12 invalid struct layout")
	}
	payload := m12PayloadBase + len(refWords)*8 + count*8
	if err := e.m12Alloc(M12StructTypeID, payload); err != nil {
		return err
	}
	e.words = append(e.words, movX(12, 0))
	e.m12StoreImmediate(12, 0, m12StructKind)
	e.m12StoreImmediate(12, 8, uint64(count))
	e.m12StoreImmediate(12, 16, uint64(count))
	e.m12StoreImmediate(12, 24, uint64(len(refWords)))
	for index, bits := range refWords {
		e.m12StoreImmediate(12, uint32(m12PayloadBase+index*8), bits)
	}
	base := e.depth - count
	dataOffset := m12PayloadBase + len(refWords)*8
	for index := 0; index < count; index++ {
		e.loadOperand(base+index, 9)
		e.words = append(e.words, strX(9, 12, uint32(dataOffset/8+index)))
	}
	e.storeOperand(base, 12)
	e.depth = base + 1
	return nil
}

// NewEnumObject creates one immutable tagged value. The payload is
// self-describing so variants of the same source enum may have different
// arities and exact reference layouts without changing the one-word ABI.
func (e *StackEmitter) NewEnumObject(tag, count int, refWords []uint64) error {
	if tag < 0 || tag > 0xffff || count < 0 || count > e.depth || len(refWords) != (count+63)/64 {
		return fmt.Errorf("AOT M13b invalid enum layout")
	}
	if len(refWords) > (M8MaxHeapCapacity-m12PayloadBase-count*8)/8 {
		return fmt.Errorf("AOT M13b enum payload size overflow")
	}
	payload := m12PayloadBase + len(refWords)*8 + count*8
	if err := e.m12Alloc(M13EnumTypeID, payload); err != nil {
		return err
	}
	e.words = append(e.words, movX(12, 0))
	e.m12StoreImmediate(12, 0, m13EnumKind)
	e.m12StoreImmediate(12, 8, uint64(tag))
	e.m12StoreImmediate(12, 16, uint64(count))
	e.m12StoreImmediate(12, 24, uint64(len(refWords)))
	for index, bits := range refWords {
		e.m12StoreImmediate(12, uint32(m12PayloadBase+index*8), bits)
	}
	base := e.depth - count
	dataOffset := m12PayloadBase + len(refWords)*8
	for index := 0; index < count; index++ {
		e.loadOperand(base+index, 9)
		e.words = append(e.words, strX(9, 12, uint32(dataOffset/8+index)))
	}
	e.storeOperand(base, 12)
	e.depth = base + 1
	return nil
}

func (e *StackEmitter) EnumTag(nullable bool) error {
	if err := e.ensurePop("OpEnumTag", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	if nullable {
		e.words = append(e.words, cmpZeroX(9))
		e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, 1)) // NE skips long failure branch
		e.nullableNilSites = append(e.nullableNilSites, len(e.words))
		e.words = append(e.words, 0)
	}
	e.words = append(e.words, ldrX(9, 9, 1))
	e.storeOperand(e.depth-1, 9)
	return nil
}

func (e *StackEmitter) GetEnumValue(index, count int, nullable bool) error {
	if err := e.ensurePop("OpGetEnumValue", 1); err != nil {
		return err
	}
	if index < 0 || index >= count {
		return fmt.Errorf("AOT M13b enum value %d out of range", index)
	}
	e.loadOperand(e.depth-1, 9)
	if nullable {
		e.words = append(e.words, cmpZeroX(9))
		e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, 1)) // NE skips long failure branch
		e.nullableNilSites = append(e.nullableNilSites, len(e.words))
		e.words = append(e.words, 0)
	}
	e.words = append(e.words,
		ldrX(10, 9, 3), m10Lsl(10, 10, 3),
		addImmX(9, 9, m12PayloadBase), addX(9, 9, 10),
		ldrX(9, 9, uint32(index)),
	)
	e.storeOperand(e.depth-1, 9)
	return nil
}

// NewArrayObject 创建稳定描述符和独立 backing storage，后续扩容不会破坏共享别名。
func (e *StackEmitter) NewArrayObject(count, elementBytes int, reference bool) error {
	if count < 0 || count > e.depth {
		return fmt.Errorf("AOT M12 invalid array layout")
	}
	if elementBytes != 1 && elementBytes != 8 {
		return fmt.Errorf("AOT M12 invalid array element width %d", elementBytes)
	}
	base := e.depth - count
	storageBytes := m12PayloadBase + count*elementBytes
	if err := e.m12Alloc(M12StorageTypeID, storageBytes); err != nil {
		return err
	}
	e.words = append(e.words, movX(12, 0))
	flags := uint64(m12StorageKind) | uint64(elementBytes)<<8
	if reference {
		flags |= 1 << 16
	}
	e.m12StoreImmediate(12, 0, flags)
	e.m12StoreImmediate(12, 8, uint64(count))
	e.m12StoreImmediate(12, 16, 1)
	ref := uint64(0)
	if reference {
		ref = 1
	}
	e.m12StoreImmediate(12, 24, ref)
	dataOffset := uint32(m12PayloadBase)
	for index := 0; index < count; index++ {
		e.loadOperand(base+index, 9)
		if elementBytes == 1 {
			e.words = append(e.words, m8StrB(9, 12)|uint32(dataOffset+uint32(index))<<10)
		} else {
			e.words = append(e.words, strX(9, 12, dataOffset/8+uint32(index)))
		}
	}
	// 在第二次 BL 期间通过当前帧保存 storage 指针。
	e.storeOperand(base, 12)
	if err := e.ProtectGCTemporary(e.locals+base, 12); err != nil {
		return err
	}
	if err := e.m12Alloc(M12ArrayTypeID, 48); err != nil {
		return err
	}
	e.words = append(e.words, movX(12, 0))
	e.m12StoreImmediate(12, 0, m12ArrayKind|uint64(elementBytes)<<8)
	e.m12StoreImmediate(12, 8, uint64(count))
	e.m12StoreImmediate(12, 16, uint64(count))
	e.m12StoreImmediate(12, 24, 1)
	e.m12StoreImmediate(12, 32, 1) // 描述符的 storage 字段是精确引用。
	e.loadOperand(base, 9)
	e.words = append(e.words, strX(9, 12, 5))
	e.storeOperand(base, 12)
	e.depth = base + 1
	return nil
}

func (e *StackEmitter) GetStructField(index, fields int) error {
	if err := e.ensurePop("OpGetField", 1); err != nil {
		return err
	}
	if index < 0 || index >= fields {
		return fmt.Errorf("AOT M12 struct field %d out of range", index)
	}
	bitmapWords := (fields + 63) / 64
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, ldrX(9, 9, uint32((m12PayloadBase+bitmapWords*8)/8+index)))
	e.storeOperand(e.depth-1, 9)
	return nil
}

func (e *StackEmitter) ArrayLen() error {
	if err := e.ensurePop("OpLen", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, ldrX(9, 9, 1))
	e.storeOperand(e.depth-1, 9)
	return nil
}

func (e *StackEmitter) m12ArrayCall(kind m12CallKind, elementBytes int, reference bool) {
	e.words = append(e.words, movX(0, 23))
	e.words = append(e.words, movzX(3, uint16(elementBytes)))
	if reference {
		e.words = append(e.words, movzX(4, 1))
	} else {
		e.words = append(e.words, movzX(4, 0))
	}
	e.m12Calls = append(e.m12Calls, m12CallSite{at: len(e.words), kind: kind})
	e.words = append(e.words, 0)
	e.words = append(e.words, cbnzX(0, len(e.words), len(e.words)+2))
	e.m12ArrayFailSites = append(e.m12ArrayFailSites, len(e.words))
	e.words = append(e.words, 0)
}

// ArrayMake 创建指定长度并填充初值的共享数组。
func (e *StackEmitter) ArrayMake(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayMake", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 1)
	e.loadOperand(e.depth-1, 2)
	e.m12ArrayCall(m12ArrayMake, elementBytes, reference)
	e.storeOperand(e.depth-2, 0)
	e.depth--
	return nil
}

// ArrayWithCapacity 创建长度为零、容量固定的共享数组。
func (e *StackEmitter) ArrayWithCapacity(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayWithCapacity", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 1)
	e.m12ArrayCall(m12ArrayWithCapacity, elementBytes, reference)
	e.storeOperand(e.depth-1, 0)
	return nil
}

// ArrayPush 在必要时事务式扩容，再追加一个元素。
func (e *StackEmitter) ArrayPush(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayPush", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 1)
	e.loadOperand(e.depth-1, 2)
	e.m12ArrayCall(m12ArrayPush, elementBytes, reference)
	e.depth -= 2
	return nil
}

func (e *StackEmitter) ArrayCapacity() error {
	if err := e.ensurePop("OpArrayCapacity", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 9)
	e.words = append(e.words, ldrX(9, 9, 2))
	e.storeOperand(e.depth-1, 9)
	return nil
}

// ArrayReserve 的参数是相对当前长度的额外容量。
func (e *StackEmitter) ArrayReserve(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayReserve", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 1)
	e.loadOperand(e.depth-1, 2)
	e.m12ArrayCall(m12ArrayReserve, elementBytes, reference)
	e.depth -= 2
	return nil
}

// ArrayClone replaces the source operand with an independent shallow copy.
func (e *StackEmitter) ArrayClone(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayClone", 1); err != nil {
		return err
	}
	e.loadOperand(e.depth-1, 1)
	e.m12ArrayCall(m12ArrayClone, elementBytes, reference)
	e.storeOperand(e.depth-1, 0)
	return nil
}

// ArrayTruncate shortens the shared descriptor and consumes both operands.
func (e *StackEmitter) ArrayTruncate(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayTruncate", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 1)
	e.loadOperand(e.depth-1, 2)
	e.m12ArrayCall(m12ArrayTruncate, elementBytes, reference)
	e.depth -= 2
	return nil
}

// ArrayAppendAll appends a shallow copy of the source's pre-operation prefix.
func (e *StackEmitter) ArrayAppendAll(elementBytes int, reference bool) error {
	if err := e.ensurePop("OpArrayAppendAll", 2); err != nil {
		return err
	}
	e.loadOperand(e.depth-2, 1)
	e.loadOperand(e.depth-1, 2)
	e.m12ArrayCall(m12ArrayAppendAll, elementBytes, reference)
	e.depth -= 2
	return nil
}

func (e *StackEmitter) m12ArrayAddress(elementBytes int) error {
	if elementBytes != 1 && elementBytes != 8 {
		return fmt.Errorf("AOT M12 invalid array element width %d", elementBytes)
	}
	e.loadOperand(e.depth-2, 9)
	e.loadOperand(e.depth-1, 10)
	e.words = append(e.words, ldrX(11, 9, 1), m8Cmp(10, 11))
	e.words = append(e.words, branchCond(len(e.words), len(e.words)+2, 3)) // LO skips long branch
	e.m12BoundsSites = append(e.m12BoundsSites, len(e.words))              // HS 同时捕获负数和上界。
	e.words = append(e.words, 0, ldrX(9, 9, 5), addImmX(9, 9, m12PayloadBase))
	if elementBytes == 8 {
		e.words = append(e.words, addX(10, 10, 10), addX(10, 10, 10), addX(10, 10, 10))
	}
	e.words = append(e.words, addX(9, 9, 10))
	return nil
}

func appendM12Failure(code []uint32, message string, calls *[]m12CallSite, telemetry bool) []uint32 {
	frame := (len(message) + 15) &^ 15
	code = append(code, subImmX(31, 31, uint32(frame)))
	for index := range message {
		code = append(code, movzW(9, uint16(message[index])))
		code = append(code, 0x39000000|uint32(index)<<10|31<<5|9)
	}
	code = append(code,
		movzX(0, 2), addImmX(1, 31, 0), movzX(2, uint16(len(message))),
		movzX(16, m8DarwinSYSWrite), svc(0x80))
	if telemetry {
		code = append(code, movX(0, 23), movX(1, 24))
		*calls = append(*calls, m12CallSite{at: len(code), kind: m12Telemetry})
		code = append(code, 0)
	}
	skipDestroy := len(code)
	code = append(code, 0, movX(0, 23))
	*calls = append(*calls, m12CallSite{at: len(code), kind: m12Destroy})
	code = append(code, 0)
	pastDestroy := len(code)
	code[skipDestroy], _ = checkedCBZ(23, skipDestroy, pastDestroy)
	return append(code, movzX(0, 1), movzX(16, 1), svc(0x80))
}

func (e *StackEmitter) ArrayIndex(elementBytes int) error {
	if err := e.ensurePop("OpIndex", 2); err != nil {
		return err
	}
	if err := e.m12ArrayAddress(elementBytes); err != nil {
		return err
	}
	if elementBytes == 1 {
		e.words = append(e.words, m8LdrB(9, 9))
	} else {
		e.words = append(e.words, ldrX(9, 9, 0))
	}
	e.storeOperand(e.depth-2, 9)
	e.depth--
	return nil
}

func (e *StackEmitter) ArraySetIndex(elementBytes int) error {
	if err := e.ensurePop("OpSetIndex", 3); err != nil {
		return err
	}
	// 地址助手读取数组和索引，因此临时把深度视为不含待写元素。
	valueSlot := e.depth - 1
	e.depth--
	if err := e.m12ArrayAddress(elementBytes); err != nil {
		e.depth++
		return err
	}
	e.loadOperand(valueSlot, 10)
	if elementBytes == 1 {
		e.words = append(e.words, m8StrB(10, 9))
	} else {
		e.words = append(e.words, strX(10, 9, 0))
	}
	e.depth -= 2
	return nil
}
