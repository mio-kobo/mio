package darwinarm64

const (
	m12ArrayMakeLabel         = "m12.array.make"
	m12ArrayWithCapacityLabel = "m12.array.with_capacity"
	m12ArrayPushLabel         = "m12.array.push"
	m12ArrayReserveLabel      = "m12.array.reserve"
	m12ArrayCloneLabel        = "m12.array.clone"
	m12ArrayTruncateLabel     = "m12.array.truncate"
	m12ArrayAppendAllLabel    = "m12.array.append_all"
	m12StorageAllocLabel      = "m12.array.storage"
)

func m12Or(dst, left, right uint32) uint32 {
	return 0xAA000000 | right<<16 | left<<5 | dst
}

func appendM12ArrayRuntime(b *m8Assembler, allocLabel string) {
	appendM12StorageAlloc(b, allocLabel)
	appendM12ArrayWithCapacity(b, allocLabel)
	appendM12ArrayMake(b)
	appendM12ArrayReserve(b)
	appendM12ArrayPush(b)
	appendM12ArrayClone(b, allocLabel)
	appendM12ArrayTruncate(b)
	appendM12ArrayAppendAll(b)
}

// storage(heap, capacity, width, reference) 分配并清零完整 backing storage。
func appendM12StorageAlloc(b *m8Assembler, allocLabel string) {
	b.label(m12StorageAllocLabel)
	b.emit(subImmX(31, 31, 48), strX(30, 31, 0))
	for reg := uint32(0); reg <= 3; reg++ {
		b.emit(strX(reg, 31, reg+1))
	}
	b.emit(m8Cmp(1, 31))
	b.bc(11, "m12.storage.invalid") // B.LT
	b.emit(cmpImmX(2, 1))
	b.bc(0, "m12.storage.width_ok")
	b.emit(cmpImmX(2, 8))
	b.bc(1, "m12.storage.invalid")
	b.label("m12.storage.width_ok")
	// 先验证有符号乘法及 payload 加法，再应用固定 1 MiB Heap 上限。
	b.emit(m8MovBits(9, uint64(int64(^uint64(0)>>1)-m12PayloadBase))...)
	b.emit(udivX(9, 9, 2))
	b.emit(m8Cmp(1, 9))
	b.bc(8, "m12.storage.invalid") // B.HI
	b.emit(m8MovBits(9, uint64((M8MaxHeapCapacity - m12PayloadBase)))...)
	b.emit(udivX(9, 9, 2), m8Cmp(1, 9))
	b.bc(8, "m12.storage.oom")
	b.emit(mulX(10, 1, 2), addImmX(10, 10, m12PayloadBase))
	b.emit(movX(0, 0), movzX(1, M12StorageTypeID), movX(2, 10))
	b.bl(allocLabel)
	b.cbz(0, "m12.storage.oom")
	b.emit(movX(9, 0), ldrX(10, 31, 3), ldrX(11, 31, 4)) // width、reference
	b.emit(movzX(12, m12StorageKind), m10Lsl(13, 10, 8), m12Or(12, 12, 13))
	b.emit(m10Lsl(13, 11, 16), m12Or(12, 12, 13), strX(12, 9, 0))
	b.emit(ldrX(12, 31, 2), strX(12, 9, 1), movzX(13, 1), strX(13, 9, 2), strX(11, 9, 3))
	// 即便 mmap 初始为零，也显式初始化新容量，避免未来复用对象时泄漏旧引用。
	b.emit(mulX(12, 12, 10), addImmX(13, 9, m12PayloadBase), movzX(14, 0))
	b.label("m12.storage.zero")
	b.emit(m8Cmp(14, 12))
	b.bc(2, "m12.storage.success")
	b.emit(m8StrB(31, 13), addImmX(13, 13, 1), addImmX(14, 14, 1))
	b.b("m12.storage.zero")
	b.label("m12.storage.success")
	b.emit(movX(0, 9), movzX(1, 0), ldrX(30, 31, 0), addImmX(31, 31, 48), m8Ret())
	b.label("m12.storage.invalid")
	b.emit(movzX(0, 0), movzX(1, 1), ldrX(30, 31, 0), addImmX(31, 31, 48), m8Ret())
	b.label("m12.storage.oom")
	b.emit(movzX(0, 0), movzX(1, 2), ldrX(30, 31, 0), addImmX(31, 31, 48), m8Ret())
}

func appendM12ArrayWithCapacity(b *m8Assembler, allocLabel string) {
	b.label(m12ArrayWithCapacityLabel)
	b.emit(subImmX(31, 31, 96), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2), strX(3, 31, 3), strX(4, 31, 4))
	b.emit(movX(2, 3), movX(3, 4))
	b.bl(m12StorageAllocLabel)
	b.cbz(0, "m12.with_capacity.done")
	// The storage is a runtime temporary across the descriptor allocation. Add
	// a one-root shadow frame so stress collection cannot reclaim it.
	b.emit(strX(0, 31, 5), strX(24, 31, 6), movzX(9, 1), strX(9, 31, 7), strX(0, 31, 8), addImmX(24, 31, 48))
	b.emit(ldrX(0, 31, 1), movzX(1, M12ArrayTypeID), movzX(2, 48))
	b.bl(allocLabel)
	b.emit(ldrX(24, 24, 0))
	b.cbz(0, "m12.with_capacity.oom")
	b.emit(movX(9, 0), ldrX(10, 31, 3), ldrX(11, 31, 4))
	b.emit(movzX(12, m12ArrayKind), m10Lsl(13, 10, 8), m12Or(12, 12, 13))
	b.emit(m10Lsl(13, 11, 16), m12Or(12, 12, 13), strX(12, 9, 0))
	b.emit(movzX(12, 0), strX(12, 9, 1), ldrX(12, 31, 2), strX(12, 9, 2))
	b.emit(movzX(12, 1), strX(12, 9, 3), strX(12, 9, 4), ldrX(12, 31, 5), strX(12, 9, 5))
	b.emit(movX(0, 9), movzX(1, 0))
	b.b("m12.with_capacity.done")
	b.label("m12.with_capacity.oom")
	b.emit(movzX(0, 0), movzX(1, 2))
	b.label("m12.with_capacity.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 96), m8Ret())
}

func appendM12ArrayMake(b *m8Assembler) {
	b.label(m12ArrayMakeLabel)
	b.emit(subImmX(31, 31, 64), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2), strX(2, 31, 3), strX(3, 31, 4), strX(4, 31, 5))
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), ldrX(3, 31, 4), ldrX(4, 31, 5))
	b.bl(m12ArrayWithCapacityLabel)
	b.cbz(0, "m12.make.done")
	b.emit(movX(9, 0), ldrX(10, 31, 2), ldrX(11, 31, 3), ldrX(12, 31, 4), ldrX(13, 9, 5), addImmX(13, 13, m12PayloadBase), movzX(14, 0))
	b.emit(cmpImmX(12, 1))
	b.bc(0, "m12.make.byte_loop")
	b.label("m12.make.word_loop")
	b.emit(m8Cmp(14, 10))
	b.bc(2, "m12.make.commit")
	b.emit(strX(11, 13, 0), addImmX(13, 13, 8), addImmX(14, 14, 1))
	b.b("m12.make.word_loop")
	b.label("m12.make.byte_loop")
	b.emit(m8Cmp(14, 10))
	b.bc(2, "m12.make.commit")
	b.emit(m8StrB(11, 13), addImmX(13, 13, 1), addImmX(14, 14, 1))
	b.b("m12.make.byte_loop")
	b.label("m12.make.commit")
	b.emit(strX(10, 9, 1), movX(0, 9))
	b.label("m12.make.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 64), m8Ret())
}

func appendM12ArrayReserve(b *m8Assembler) {
	b.label(m12ArrayReserveLabel)
	b.emit(subImmX(31, 31, 80), strX(30, 31, 0))
	for reg := uint32(0); reg <= 4; reg++ {
		b.emit(strX(reg, 31, reg+1))
	}
	b.cbz(1, "m12.reserve.invalid")
	b.emit(m8Cmp(2, 31))
	b.bc(11, "m12.reserve.invalid")
	b.emit(ldrX(9, 1, 1), addX(10, 9, 2), m8Cmp(10, 31))
	b.bc(11, "m12.reserve.invalid")
	b.emit(m8Cmp(10, 9))
	b.bc(3, "m12.reserve.invalid") // B.LO
	b.emit(strX(10, 31, 6), ldrX(11, 1, 2), m8Cmp(11, 10))
	b.bc(2, "m12.reserve.success")
	b.emit(cmpImmX(11, 4))
	b.bc(2, "m12.reserve.grow")
	b.emit(movzX(11, 4))
	b.label("m12.reserve.grow")
	b.emit(m8Cmp(11, 10))
	b.bc(2, "m12.reserve.allocate")
	b.emit(cmpImmX(11, 1024))
	b.bc(3, "m12.reserve.double")
	b.emit(m10Lsr(12, 11, 1), addX(11, 11, 12))
	b.emit(m8Cmp(11, 31))
	b.bc(11, "m12.reserve.invalid")
	b.b("m12.reserve.grow")
	b.label("m12.reserve.double")
	b.emit(addX(11, 11, 11))
	b.b("m12.reserve.grow")
	b.label("m12.reserve.allocate")
	b.emit(strX(11, 31, 7), ldrX(0, 31, 1), movX(1, 11), ldrX(2, 31, 4), ldrX(3, 31, 5))
	b.bl(m12StorageAllocLabel)
	b.cbz(0, "m12.reserve.done")
	b.emit(strX(0, 31, 8), ldrX(9, 31, 2), ldrX(10, 9, 1), ldrX(11, 9, 5), addImmX(11, 11, m12PayloadBase), addImmX(12, 0, m12PayloadBase), ldrX(13, 31, 4), movzX(14, 0))
	b.emit(cmpImmX(13, 1))
	b.bc(0, "m12.reserve.copy_byte")
	b.label("m12.reserve.copy_word")
	b.emit(m8Cmp(14, 10))
	b.bc(2, "m12.reserve.commit")
	b.emit(ldrX(15, 11, 0), strX(15, 12, 0), addImmX(11, 11, 8), addImmX(12, 12, 8), addImmX(14, 14, 1))
	b.b("m12.reserve.copy_word")
	b.label("m12.reserve.copy_byte")
	b.emit(m8Cmp(14, 10))
	b.bc(2, "m12.reserve.commit")
	b.emit(m8LdrB(15, 11), m8StrB(15, 12), addImmX(11, 11, 1), addImmX(12, 12, 1), addImmX(14, 14, 1))
	b.b("m12.reserve.copy_byte")
	b.label("m12.reserve.commit")
	b.emit(ldrX(9, 31, 2), ldrX(10, 31, 8), ldrX(11, 31, 7), strX(10, 9, 5), strX(11, 9, 2))
	b.label("m12.reserve.success")
	b.emit(movzX(0, 1), movzX(1, 0))
	b.b("m12.reserve.done")
	b.label("m12.reserve.invalid")
	b.emit(movzX(0, 0), movzX(1, 1))
	b.label("m12.reserve.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 80), m8Ret())
}

func appendM12ArrayPush(b *m8Assembler) {
	b.label(m12ArrayPushLabel)
	b.emit(subImmX(31, 31, 64), strX(30, 31, 0))
	for reg := uint32(0); reg <= 4; reg++ {
		b.emit(strX(reg, 31, reg+1))
	}
	b.emit(movzX(2, 1))
	b.bl(m12ArrayReserveLabel)
	b.cbz(0, "m12.push.done")
	b.emit(ldrX(9, 31, 2), ldrX(10, 9, 1), ldrX(11, 9, 5), addImmX(11, 11, m12PayloadBase), ldrX(12, 31, 4), ldrX(13, 31, 3))
	b.emit(cmpImmX(12, 1))
	b.bc(0, "m12.push.byte")
	b.emit(m10Lsl(14, 10, 3), addX(11, 11, 14), strX(13, 11, 0))
	b.b("m12.push.commit")
	b.label("m12.push.byte")
	b.emit(addX(11, 11, 10), m8StrB(13, 11))
	b.label("m12.push.commit")
	b.emit(addImmX(10, 10, 1), strX(10, 9, 1), movzX(0, 1), movzX(1, 0))
	b.label("m12.push.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 64), m8Ret())
}

// clone(heap, source, _, width, reference) creates an independent descriptor
// and storage while retaining the VM's shallow-copy semantics for elements.
func appendM12ArrayClone(b *m8Assembler, allocLabel string) {
	b.label(m12ArrayCloneLabel)
	b.emit(subImmX(31, 31, 96), strX(30, 31, 0))
	for reg := uint32(0); reg <= 4; reg++ {
		b.emit(strX(reg, 31, reg+1))
	}
	b.cbz(1, "m12.clone.invalid")
	b.emit(ldrX(9, 1, 1), strX(9, 31, 6))
	b.emit(ldrX(0, 31, 1), movX(1, 9), ldrX(2, 31, 4), ldrX(3, 31, 5))
	b.bl(m12StorageAllocLabel)
	b.cbz(0, "m12.clone.done")
	b.emit(strX(0, 31, 7), ldrX(9, 31, 2), ldrX(10, 9, 5), addImmX(10, 10, m12PayloadBase), addImmX(11, 0, m12PayloadBase), ldrX(12, 31, 4), ldrX(13, 31, 6), movzX(14, 0))
	b.emit(cmpImmX(12, 1))
	b.bc(0, "m12.clone.copy_byte")
	b.label("m12.clone.copy_word")
	b.emit(m8Cmp(14, 13))
	b.bc(2, "m12.clone.allocate_descriptor")
	b.emit(ldrX(15, 10, 0), strX(15, 11, 0), addImmX(10, 10, 8), addImmX(11, 11, 8), addImmX(14, 14, 1))
	b.b("m12.clone.copy_word")
	b.label("m12.clone.copy_byte")
	b.emit(m8Cmp(14, 13))
	b.bc(2, "m12.clone.allocate_descriptor")
	b.emit(m8LdrB(15, 10), m8StrB(15, 11), addImmX(10, 10, 1), addImmX(11, 11, 1), addImmX(14, 14, 1))
	b.b("m12.clone.copy_byte")
	b.label("m12.clone.allocate_descriptor")
	// The source and not-yet-published storage must both survive stress GC at
	// the descriptor allocation safepoint.
	b.emit(strX(24, 31, 8), movzX(9, 2), strX(9, 31, 9), ldrX(9, 31, 2), strX(9, 31, 10), ldrX(9, 31, 7), strX(9, 31, 11), addImmX(24, 31, 64))
	b.emit(ldrX(0, 31, 1), movzX(1, M12ArrayTypeID), movzX(2, 48))
	b.bl(allocLabel)
	b.emit(ldrX(24, 24, 0))
	b.cbz(0, "m12.clone.oom")
	b.emit(movX(9, 0), ldrX(10, 31, 4), ldrX(11, 31, 5))
	b.emit(movzX(12, m12ArrayKind), m10Lsl(13, 10, 8), m12Or(12, 12, 13), strX(12, 9, 0))
	b.emit(ldrX(12, 31, 6), strX(12, 9, 1), strX(12, 9, 2))
	b.emit(movzX(12, 1), strX(12, 9, 3), strX(12, 9, 4), ldrX(12, 31, 7), strX(12, 9, 5))
	b.emit(movX(0, 9), movzX(1, 0))
	b.b("m12.clone.done")
	b.label("m12.clone.invalid")
	b.emit(movzX(0, 0), movzX(1, 1))
	b.b("m12.clone.done")
	b.label("m12.clone.oom")
	b.emit(movzX(0, 0), movzX(1, 2))
	b.label("m12.clone.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 96), m8Ret())
}

// truncate(_, array, newLength, width, _) clears removed storage before
// committing the shorter logical length. Capacity and storage stay unchanged.
func appendM12ArrayTruncate(b *m8Assembler) {
	b.label(m12ArrayTruncateLabel)
	b.cbz(1, "m12.truncate.invalid")
	b.emit(m8Cmp(2, 31))
	b.bc(11, "m12.truncate.invalid")
	b.emit(ldrX(9, 1, 1), m8Cmp(2, 9))
	b.bc(8, "m12.truncate.invalid")
	b.emit(ldrX(10, 1, 5), addImmX(10, 10, m12PayloadBase), movX(11, 2), cmpImmX(3, 1))
	b.bc(0, "m12.truncate.byte")
	b.emit(m10Lsl(12, 11, 3), addX(10, 10, 12))
	b.label("m12.truncate.word_loop")
	b.emit(m8Cmp(11, 9))
	b.bc(2, "m12.truncate.commit")
	b.emit(strX(31, 10, 0), addImmX(10, 10, 8), addImmX(11, 11, 1))
	b.b("m12.truncate.word_loop")
	b.label("m12.truncate.byte")
	b.emit(addX(10, 10, 11))
	b.label("m12.truncate.byte_loop")
	b.emit(m8Cmp(11, 9))
	b.bc(2, "m12.truncate.commit")
	b.emit(m8StrB(31, 10), addImmX(10, 10, 1), addImmX(11, 11, 1))
	b.b("m12.truncate.byte_loop")
	b.label("m12.truncate.commit")
	b.emit(strX(2, 1, 1), movzX(0, 1), movzX(1, 0), m8Ret())
	b.label("m12.truncate.invalid")
	b.emit(movzX(0, 0), movzX(1, 3), m8Ret())
}

// append_all(heap, destination, source, width, reference) freezes the source
// length before reserve. Reserve is transactional, so allocation failure leaves
// all destination descriptor fields and existing elements untouched.
func appendM12ArrayAppendAll(b *m8Assembler) {
	b.label(m12ArrayAppendAllLabel)
	b.emit(subImmX(31, 31, 80), strX(30, 31, 0))
	for reg := uint32(0); reg <= 4; reg++ {
		b.emit(strX(reg, 31, reg+1))
	}
	b.cbz(1, "m12.append_all.invalid")
	b.cbz(2, "m12.append_all.invalid")
	b.emit(ldrX(9, 2, 1), strX(9, 31, 6))
	b.cbz(9, "m12.append_all.success")
	b.emit(ldrX(10, 1, 1), strX(10, 31, 7), addX(11, 10, 9), m8Cmp(11, 10))
	b.bc(3, "m12.append_all.invalid")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), movX(2, 9), ldrX(3, 31, 4), ldrX(4, 31, 5))
	b.bl(m12ArrayReserveLabel)
	b.cbz(0, "m12.append_all.done")
	// Reload both storage pointers after reserve. In the self/alias case both
	// descriptors now intentionally name the new storage, never the stale one.
	b.emit(ldrX(9, 31, 2), ldrX(10, 31, 3), ldrX(11, 9, 5), ldrX(12, 10, 5), addImmX(11, 11, m12PayloadBase), addImmX(12, 12, m12PayloadBase), ldrX(13, 31, 7), ldrX(14, 31, 6), ldrX(15, 31, 4))
	b.emit(cmpImmX(15, 1))
	b.bc(0, "m12.append_all.copy_byte")
	b.emit(m10Lsl(13, 13, 3), addX(11, 11, 13), movzX(13, 0))
	b.label("m12.append_all.copy_word")
	b.emit(m8Cmp(13, 14))
	b.bc(2, "m12.append_all.commit")
	b.emit(ldrX(15, 12, 0), strX(15, 11, 0), addImmX(12, 12, 8), addImmX(11, 11, 8), addImmX(13, 13, 1))
	b.b("m12.append_all.copy_word")
	b.label("m12.append_all.copy_byte")
	b.emit(addX(11, 11, 13), movzX(13, 0))
	b.label("m12.append_all.copy_byte_loop")
	b.emit(m8Cmp(13, 14))
	b.bc(2, "m12.append_all.commit")
	b.emit(m8LdrB(15, 12), m8StrB(15, 11), addImmX(12, 12, 1), addImmX(11, 11, 1), addImmX(13, 13, 1))
	b.b("m12.append_all.copy_byte_loop")
	b.label("m12.append_all.commit")
	b.emit(ldrX(9, 31, 2), ldrX(10, 31, 7), ldrX(11, 31, 6), addX(10, 10, 11), strX(10, 9, 1))
	b.label("m12.append_all.success")
	b.emit(movzX(0, 1), movzX(1, 0))
	b.b("m12.append_all.done")
	b.label("m12.append_all.invalid")
	b.emit(movzX(0, 0), movzX(1, 1))
	b.label("m12.append_all.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 80), m8Ret())
}

type M12RuntimeEntries struct {
	M8RuntimeEntries
	ArrayMake         int
	ArrayWithCapacity int
	ArrayPush         int
	ArrayReserve      int
	ArrayClone        int
	ArrayTruncate     int
	ArrayAppendAll    int
}

// M12ArrayRuntimeWords 将数组 helper 与同一份 M8 Allocator 一次链接。
func M12ArrayRuntimeWords() ([]uint32, M12RuntimeEntries, error) {
	b := newM8Assembler()
	appendM12ArrayRuntime(b, m8AllocLabel)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, M12RuntimeEntries{}, err
	}
	return words, M12RuntimeEntries{
		M8RuntimeEntries: M8RuntimeEntries{Init: b.labels[m8InitLabel], Alloc: b.labels[m8AllocLabel], Destroy: b.labels[m8DestroyLabel]},
		ArrayMake:        b.labels[m12ArrayMakeLabel], ArrayWithCapacity: b.labels[m12ArrayWithCapacityLabel],
		ArrayPush: b.labels[m12ArrayPushLabel], ArrayReserve: b.labels[m12ArrayReserveLabel],
		ArrayClone: b.labels[m12ArrayCloneLabel], ArrayTruncate: b.labels[m12ArrayTruncateLabel], ArrayAppendAll: b.labels[m12ArrayAppendAllLabel],
	}, nil
}

// M12ArrayGCRuntimeWords links M12 helpers to the automatic M11 collector and
// M10 free-list allocator. The legacy helper above remains for isolated M12
// allocator probes.
func M12ArrayGCRuntimeWords(stress bool) ([]uint32, M12RuntimeEntries, error) {
	b := newM8Assembler()
	appendM12ArrayRuntime(b, m11AutoAllocEntry)
	appendM11ObjectRuntime(b, stress)
	appendM10Sweep(b)
	appendM10Alloc(b)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, M12RuntimeEntries{}, err
	}
	return words, M12RuntimeEntries{
		M8RuntimeEntries: M8RuntimeEntries{Init: b.labels[m8InitLabel], Alloc: b.labels[m11AutoAllocEntry], Destroy: b.labels[m8DestroyLabel]},
		ArrayMake:        b.labels[m12ArrayMakeLabel], ArrayWithCapacity: b.labels[m12ArrayWithCapacityLabel],
		ArrayPush: b.labels[m12ArrayPushLabel], ArrayReserve: b.labels[m12ArrayReserveLabel],
		ArrayClone: b.labels[m12ArrayCloneLabel], ArrayTruncate: b.labels[m12ArrayTruncateLabel], ArrayAppendAll: b.labels[m12ArrayAppendAllLabel],
	}, nil
}

// M12ArrayProbe 在原生进程中验证扩容提交、引用位图和失败原子性。
func M12ArrayProbe() ([]byte, error) {
	b := newM8Assembler()
	b.emit(m8MovBits(0, M8MaxHeapCapacity)...)
	b.bl(m8InitLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(19, 0))
	// 创建 capacity=2 的引用数组。
	b.emit(movX(0, 19), movzX(1, 2), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayWithCapacityLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(20, 0), ldrX(9, 20, 2), movzX(10, 2))
	m8CheckEqual(b, 9, 10)
	b.emit(ldrX(9, 20, 4), movzX(10, 1))
	m8CheckEqual(b, 9, 10)
	b.emit(ldrX(21, 20, 5), ldrX(9, 21, 3), movzX(10, 1))
	m8CheckEqual(b, 9, 10)
	// Push 后 Reserve 到 8，descriptor 地址与已有元素必须保持。
	b.emit(movX(0, 19), movX(1, 20), movzX(2, 0x1234), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayPushLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(0, 19), movX(1, 20), movzX(2, 4), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayReserveLabel)
	b.cbz(0, "probe.fail")
	b.emit(ldrX(9, 20, 1), movzX(10, 1))
	m8CheckEqual(b, 9, 10)
	b.emit(ldrX(9, 20, 2), movzX(10, 8))
	m8CheckEqual(b, 9, 10)
	b.emit(ldrX(21, 20, 5), ldrX(9, 21, 4), movzX(10, 0x1234))
	m8CheckEqual(b, 9, 10)
	b.emit(ldrX(9, 21, 5), movzX(10, 0))
	m8CheckEqual(b, 9, 10)
	// OOM 不得改写 descriptor、元素或 Heap cursor。
	b.emit(ldrX(22, 20, 2), ldrX(23, 20, 5), ldrX(24, 19, 1))
	b.emit(movX(0, 19), movX(1, 20))
	b.emit(m8MovBits(2, 200000)...)
	b.emit(movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayReserveLabel)
	b.cbnz(0, "probe.fail")
	b.emit(cmpImmX(1, 2))
	b.bc(1, "probe.fail")
	b.emit(ldrX(9, 20, 2))
	m8CheckEqual(b, 9, 22)
	b.emit(ldrX(9, 20, 5))
	m8CheckEqual(b, 9, 23)
	b.emit(ldrX(9, 19, 1))
	m8CheckEqual(b, 9, 24)
	b.emit(ldrX(9, 23, 4), movzX(10, 0x1234))
	m8CheckEqual(b, 9, 10)
	// Build a large source, then prove Clone and AppendAll OOM leave both
	// pre-existing descriptors and destination storage untouched.
	b.emit(movX(0, 19))
	b.emit(m8MovBits(1, 90000)...)
	b.emit(movzX(2, 0), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayMakeLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(25, 0), ldrX(22, 20, 1), ldrX(23, 20, 2), ldrX(24, 20, 5), ldrX(26, 25, 1), ldrX(27, 25, 5))
	b.emit(movX(0, 19), movX(1, 25), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayCloneLabel)
	b.cbnz(0, "probe.fail")
	b.emit(cmpImmX(1, 2))
	b.bc(1, "probe.fail")
	b.emit(ldrX(9, 25, 1))
	m8CheckEqual(b, 9, 26)
	b.emit(ldrX(9, 25, 5))
	m8CheckEqual(b, 9, 27)
	b.emit(movX(0, 19), movX(1, 20), movX(2, 25), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayAppendAllLabel)
	b.cbnz(0, "probe.fail")
	b.emit(cmpImmX(1, 2))
	b.bc(1, "probe.fail")
	b.emit(ldrX(9, 20, 1))
	m8CheckEqual(b, 9, 22)
	b.emit(ldrX(9, 20, 2))
	m8CheckEqual(b, 9, 23)
	b.emit(ldrX(9, 20, 5))
	m8CheckEqual(b, 9, 24)
	b.emit(ldrX(9, 24, 4), movzX(10, 0x1234))
	m8CheckEqual(b, 9, 10)
	// Truncate clears the removed reference word but preserves capacity/storage.
	b.emit(movX(1, 20), movzX(2, 0), movzX(3, 8), movzX(4, 1))
	b.bl(m12ArrayTruncateLabel)
	b.cbz(0, "probe.fail")
	b.emit(ldrX(9, 20, 1), movzX(10, 0))
	m8CheckEqual(b, 9, 10)
	b.emit(ldrX(9, 20, 2))
	m8CheckEqual(b, 9, 23)
	b.emit(ldrX(9, 20, 5))
	m8CheckEqual(b, 9, 24)
	b.emit(ldrX(9, 24, 4), movzX(10, 0))
	m8CheckEqual(b, 9, 10)
	// capacity*width 溢出必须在任何分配前失败。
	b.emit(ldrX(28, 19, 1))
	b.emit(movX(0, 19))
	b.emit(m8MovBits(1, uint64(^uint64(0)>>1))...)
	b.emit(movzX(3, 8), movzX(4, 0))
	b.bl(m12ArrayWithCapacityLabel)
	b.cbnz(0, "probe.fail")
	b.emit(cmpImmX(1, 1))
	b.bc(1, "probe.fail")
	b.emit(ldrX(9, 19, 1))
	m8CheckEqual(b, 9, 28)
	b.emit(movX(0, 19))
	b.bl(m8DestroyLabel)
	b.cbz(0, "probe.fail")
	m8WriteLiteral(b, "M12 array PASS\n")
	b.emit(movzX(0, 0), movzX(16, 1), svc(0x80))
	b.label("probe.fail")
	m8WriteLiteral(b, "M12 array FAIL\n")
	b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))
	appendM12ArrayRuntime(b, m8AllocLabel)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, err
	}
	return ExecutableWords(words)
}
