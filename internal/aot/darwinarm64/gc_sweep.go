package darwinarm64

// M10 沿用 M8 的 Heap 和对象头 ABI：
//
//	Heap[56]          uint64：空闲链表头，存储以 16 字节为单位的 1-based 相对偏移
//	对象头 TypeID=0    表示已经回收的空闲块
//	对象头 flags bit2  表示空闲块，flags >> 3 保存下一个空闲块的相对索引
//	对象头 payloadSize 保留当前块的物理跨度减去 16 字节对象头
//
// M10 的空闲链表是 Heap 内部的相对地址链表，不存放需要重定位的绝对指针。
// 对象最小物理跨度为 16 字节，因此 payloadSize=0 的空闲对象也能入链。
// 对外仍返回原始 16 字节对齐的 payload 指针，M8 的头部布局不变。
const (
	M10FreeBit       uint32 = 4
	m10AllocEntry           = "gc.alloc"
	m10SweepEntry           = "gc.sweep"
	m10ValidateEntry        = "gc.validate"
	m10CollectEntry         = "gc.collect"
)

type M10RuntimeEntries struct {
	Init     int
	Alloc    int
	Destroy  int
	Mark     int
	Sweep    int
	Validate int
	Collect  int
}

// M10RuntimeWords 返回能直接拼接到 AOT 程序末尾的原生运行时片段。
// gc.collect(x0=heap,x1=types,x2=typeCount,x3=frameBase,x4=rootMap,x5=globalsBase)
// 返回 x0=本次回收的对象数量；UINT64_MAX 表示元数据或 Heap 损坏。
// gc.alloc(x0=heap,x1=typeID,x2=payloadBytes) 返回 payload 指针或 0。
// 当前必须由调用方在已建立精确 Root Map 的安全点手动调用，尚不支持自动 GC。
func M10RuntimeWords() ([]uint32, M10RuntimeEntries, error) {
	b := newM8Assembler()
	appendM10Collect(b)
	appendM10Sweep(b)
	appendM10Alloc(b)
	appendM9Mark(b)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, M10RuntimeEntries{}, err
	}
	return words, M10RuntimeEntries{
		Init: b.labels[m8InitLabel], Alloc: b.labels[m10AllocEntry], Destroy: b.labels[m8DestroyLabel],
		Mark: b.labels[m9MarkEntry], Sweep: b.labels[m10SweepEntry], Validate: b.labels[m10ValidateEntry], Collect: b.labels[m10CollectEntry],
	}, nil
}

// AArch64 UBFM 的 LSL/LSR 别名，和其余机器码一起在 Go 编译期直接编码。
func m10Lsl(rd, rn, shift uint32) uint32 {
	return 0xD3400000 | (64-shift)<<16 | (63-shift)<<10 | rn<<5 | rd
}
func m10Lsr(rd, rn, shift uint32) uint32 {
	return 0xD3400000 | shift<<16 | 63<<10 | rn<<5 | rd
}
func m10Sub(rd, rn, rm uint32) uint32 { return 0xCB000000 | rm<<16 | rn<<5 | rd }

// Free blocks are segregated by physical span. Heap[56] remains the 16-byte
// bin for the historical ABI; larger bin heads live in telemetry words 32..62.
// This prevents every small allocation from repeatedly walking millions of
// undersized holes while retaining exact first-fit behavior within a class.
func appendM10SizeClass(b *m8Assembler, size, class, scratch uint32, prefix string) {
	thresholds := make([]uint64, 0, 31)
	for limit := uint64(16); limit <= 256; limit += 16 {
		thresholds = append(thresholds, limit)
	}
	thresholds = append(thresholds, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1<<20, 4<<20, 16<<20, 64<<20)
	for i, limit := range thresholds {
		b.emit(movzX(class, uint16(i)))
		if limit <= 4095 {
			b.emit(cmpImmX(size, uint32(limit)))
		} else {
			b.emit(m8MovBits(scratch, limit)...)
			b.emit(m8Cmp(size, scratch))
		}
		b.bc(9, prefix+".done")
	}
	b.emit(movzX(class, 31))
	b.label(prefix + ".done")
}

func appendM10LoadBinHead(b *m8Assembler, dst, heap, class, addr, scratch uint32, prefix string) {
	b.cbnz(class, prefix+".telemetry")
	b.emit(ldrX(dst, heap, 7))
	b.b(prefix + ".done")
	b.label(prefix + ".telemetry")
	b.emit(ldrX(addr, heap, 2), subImmX(scratch, class, 1), m10Lsl(scratch, scratch, 3), addX(addr, addr, scratch), ldrX(dst, addr, m8TelFreeBin1))
	b.label(prefix + ".done")
}

func appendM10StoreBinHead(b *m8Assembler, value, heap, class, addr, scratch uint32, prefix string) {
	b.cbnz(class, prefix+".telemetry")
	b.emit(strX(value, heap, 7))
	b.b(prefix + ".done")
	b.label(prefix + ".telemetry")
	b.emit(ldrX(addr, heap, 2), subImmX(scratch, class, 1), m10Lsl(scratch, scratch, 3), addX(addr, addr, scratch), strX(value, addr, m8TelFreeBin1))
	b.label(prefix + ".done")
}

func appendM10Collect(b *m8Assembler) {
	b.label(m10CollectEntry)
	// 64 字节栈帧保留 LR 与所有入参，验证后才能开始改写 Mark Flags。
	// Root Map 必须包含全部活跃对象引用；不会保守扫描整数寄存器。
	b.emit(subImmX(31, 31, 64), strX(30, 31, 0))
	for reg := uint32(0); reg <= 5; reg++ {
		b.emit(strX(reg, 31, reg+1))
	}
	b.bl(m10ValidateEntry)
	b.cbnz(0, "m10.collect.done")
	for reg := uint32(0); reg <= 5; reg++ {
		b.emit(ldrX(reg, 31, reg+1))
	}
	b.bl(m9MarkEntry)
	b.emit(m8Movn(9, 0), m8Cmp(0, 9))
	b.bc(0, "m10.collect.done")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), ldrX(2, 31, 3))
	b.bl(m10SweepEntry)
	b.label("m10.collect.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 64), m8Ret())
}

func m10SaveCallee(b *m8Assembler) {
	b.emit(subImmX(31, 31, 96))
	for i := uint32(19); i <= 28; i++ {
		b.emit(strX(i, 31, i-19))
	}
	b.emit(strX(30, 31, 10))
}
func m10RestoreCallee(b *m8Assembler) {
	b.emit(ldrX(30, 31, 10))
	for i := uint32(19); i <= 28; i++ {
		b.emit(ldrX(i, 31, i-19))
	}
	b.emit(addImmX(31, 31, 96), m8Ret())
}

func appendM10Sweep(b *m8Assembler) {
	b.label(m10ValidateEntry)
	b.emit(movzX(3, 1)) // 只验证；成功时不回收也不修改 Heap。
	b.b("m10.sweep.start")
	b.label(m10SweepEntry)
	b.emit(movzX(3, 0)) // Sweep 仍独立验证一次，不能信任调用者。
	b.label("m10.sweep.start")
	m10SaveCallee(b)
	b.emit(movX(19, 0), movX(20, 1), movX(21, 2))
	b.cbz(19, "m10.sweep.fail")
	// x1=x2=0 selects the M11/M12 dynamic object layouts. M10 probe callers
	// continue to provide the original fixed type table.
	b.cbnz(20, "m10.sweep.static.count")
	b.cbnz(21, "m10.sweep.fail")
	b.b("m10.sweep.count.ok")
	b.label("m10.sweep.static.count")
	b.cbz(21, "m10.sweep.fail")
	b.emit(cmpImmX(21, M9MaxTypes))
	b.bc(8, "m10.sweep.fail") // typeCount > 8
	b.label("m10.sweep.count.ok")
	b.emit(ldrX(9, 19, 0))
	b.emit(m8MovBits(10, m8HeapMagic)...)
	b.emit(m8Cmp(9, 10))
	b.bc(1, "m10.sweep.fail")
	b.emit(ldrX(22, 19, 6), ldrX(23, 19, 1)) // origin, cursor
	b.emit(m8Cmp(22, 23))
	b.bc(8, "m10.sweep.fail")
	b.emit(ldrX(9, 19, 2), m8Cmp(23, 9))
	b.bc(8, "m10.sweep.fail")

	// 第一遍：只验证、不改写。回收前必须保证每一个物理块都能被安全遍历。
	// 同时验证计数、类型与载荷大小，防止用错误的长度跨越 Heap 边界。
	b.emit(movX(25, 22), movzX(26, 0), movzX(27, 0))
	b.label("m10.preflight.loop")
	b.emit(m8Cmp(25, 23))
	b.bc(0, "m10.preflight.end")
	b.bc(8, "m10.sweep.fail")
	b.emit(ldrX(10, 25, 1))
	b.emit(ldrX(14, 19, 3), m8Cmp(10, 14)) // payloadSize <= configured heap capacity
	b.bc(8, "m10.sweep.fail")
	b.emit(addImmX(11, 10, 31), m8Movn(12, 15), m8And(11, 11, 12))
	b.emit(addX(12, 25, 11), m8Cmp(12, 23))
	b.bc(8, "m10.sweep.fail")
	b.emit(m9LdrW(9, 25, 0), m9LdrW(13, 25, 4))
	b.cbz(9, "m10.preflight.free")
	b.emit(cmpImmX(13, 0))
	b.bc(0, "m10.preflight.type")
	b.emit(cmpImmX(13, 3))
	b.bc(1, "m10.sweep.fail")
	b.label("m10.preflight.type")
	// Dynamic M12 objects have self-describing variable payloads. Deep layout
	// validation is performed by m11.object.mark before sweep mutates anything.
	b.cbz(20, "m10.preflight.dynamic")
	b.emit(movX(14, 20), movzX(15, 0))
	b.label("m10.preflight.type.loop")
	b.emit(m8Cmp(15, 21))
	b.bc(2, "m10.sweep.fail")
	b.emit(m9LdrW(16, 14, 0), m8Cmp(9, 16))
	b.bc(0, "m10.preflight.found")
	b.emit(addImmX(14, 14, M9TypeEntryBytes), addImmX(15, 15, 1))
	b.b("m10.preflight.type.loop")
	b.label("m10.preflight.found")
	b.emit(m9LdrW(16, 14, 4), m8Cmp(10, 16))
	b.bc(1, "m10.sweep.fail")
	b.emit(addImmX(26, 26, 1))
	b.emit(addX(27, 27, 11))
	b.b("m10.preflight.next")
	b.label("m10.preflight.dynamic")
	b.emit(cmpImmX(9, M12StructTypeID))
	b.bc(0, "m10.preflight.dynamic.variable")
	b.emit(cmpImmX(9, M13EnumTypeID))
	b.bc(0, "m10.preflight.dynamic.variable")
	b.emit(cmpImmX(9, M12ArrayTypeID))
	b.bc(0, "m10.preflight.dynamic.array")
	b.emit(cmpImmX(9, M12StorageTypeID))
	b.bc(0, "m10.preflight.dynamic.variable")
	b.emit(cmpImmX(9, M15EnvironmentTypeID))
	b.bc(0, "m10.preflight.dynamic.environment")
	b.emit(cmpImmX(9, M15ClosureTypeID))
	b.bc(0, "m10.preflight.dynamic.array")
	b.emit(cmpImmX(9, M15CellTypeID))
	b.bc(0, "m10.preflight.dynamic.word")
	b.emit(cmpImmX(9, M13NullableTypeID))
	b.bc(1, "m10.sweep.fail")
	b.label("m10.preflight.dynamic.word")
	b.emit(cmpImmX(10, 16))
	b.bc(1, "m10.sweep.fail")
	b.emit(addImmX(26, 26, 1))
	b.emit(addX(27, 27, 11))
	b.b("m10.preflight.next")
	b.label("m10.preflight.dynamic.variable")
	b.emit(cmpImmX(10, m12PayloadBase))
	b.bc(3, "m10.sweep.fail")
	b.emit(addImmX(26, 26, 1))
	b.emit(addX(27, 27, 11))
	b.b("m10.preflight.next")
	b.label("m10.preflight.dynamic.array")
	b.emit(cmpImmX(10, 48))
	b.bc(1, "m10.sweep.fail")
	b.emit(addImmX(26, 26, 1))
	b.emit(addX(27, 27, 11))
	b.b("m10.preflight.next")
	b.label("m10.preflight.dynamic.environment")
	b.emit(cmpImmX(10, 16))
	b.bc(3, "m10.sweep.fail")
	b.emit(addImmX(26, 26, 1))
	b.emit(addX(27, 27, 11))
	b.b("m10.preflight.next")
	b.label("m10.preflight.free")
	b.emit(movzX(14, 7), m8And(14, 13, 14), cmpImmX(14, M10FreeBit))
	b.bc(1, "m10.sweep.fail")
	b.label("m10.preflight.next")
	b.emit(movX(25, 12))
	b.b("m10.preflight.loop")
	b.label("m10.preflight.end")
	b.emit(ldrX(9, 19, 5), m8Cmp(26, 9))
	b.bc(1, "m10.sweep.fail")
	// Validation 的返回值为 0，所有对象头与 Free Block 均未被修改。
	b.cbz(3, "m10.sweep.mutate")
	b.emit(movzX(0, 0))
	b.b("m10.sweep.restore")
	b.label("m10.sweep.mutate")

	// 第二遍：已标记对象保留；其余已分配对象转成 Free Block。
	b.emit(movX(25, 22), movzX(24, 0), movzX(20, 0))
	b.label("m10.free.loop")
	b.emit(m8Cmp(25, 23))
	b.bc(0, "m10.free.end")
	b.emit(m9LdrW(9, 25, 0))
	b.cbz(9, "m10.free.next")
	b.emit(m9LdrW(10, 25, 4), cmpImmX(10, M9MarkBit|M9ScannedBit))
	b.bc(0, "m10.free.keep")
	b.emit(movzW(11, 0), m9StrW(11, 25, 0), movzW(11, uint16(M10FreeBit)), m9StrW(11, 25, 4))
	b.emit(ldrX(10, 25, 1), addImmX(10, 10, 31), m8Movn(11, 15), m8And(10, 10, 11), addX(20, 20, 10), addImmX(24, 24, 1))
	b.b("m10.free.next")
	b.label("m10.free.keep")
	b.emit(movzW(11, 0), m9StrW(11, 25, 4))
	b.label("m10.free.next")
	b.emit(ldrX(10, 25, 1), addImmX(10, 10, 31), m8Movn(11, 15), m8And(10, 10, 11), addX(25, 25, 10))
	b.b("m10.free.loop")
	b.label("m10.free.end")
	b.emit(ldrX(9, 19, 5), m10Sub(9, 9, 24), strX(9, 19, 5))
	b.emit(ldrX(9, 19, 2), strX(27, 9, m8TelLastBeforeBytes), m10Sub(10, 27, 20), strX(10, 9, m8TelLastAfterBytes))
	b.emit(strX(24, 9, m8TelLastReclaimedObjects), strX(20, 9, m8TelLastReclaimedBytes))

	// 第三遍：合并相邻空闲块，并按物理跨度重建分级链表。
	// 不借助操作系统额外内存，甚至 0 字节空闲对象也可以合并和复用。
	b.emit(movzX(28, 0), strX(28, 19, 7), ldrX(17, 19, 2))
	for slot := uint32(m8TelFreeBin1); slot < m8TelFreeBin1+31; slot++ {
		b.emit(strX(28, 17, slot))
	}
	b.emit(movX(25, 22))
	b.label("m10.coalesce.loop")
	b.emit(m8Cmp(25, 23))
	b.bc(0, "m10.coalesce.end")
	b.emit(ldrX(11, 25, 1), addImmX(11, 11, 31), m8Movn(12, 15), m8And(11, 11, 12))
	b.emit(addX(13, 25, 11), m9LdrW(9, 25, 0))
	b.cbnz(9, "m10.coalesce.next")
	b.emit(movX(27, 25), movX(14, 11)) // 自由区起点，总跨度
	b.label("m10.coalesce.merge")
	b.emit(m8Cmp(13, 23))
	b.bc(0, "m10.coalesce.store")
	b.emit(m9LdrW(9, 13, 0))
	b.cbnz(9, "m10.coalesce.store")
	b.emit(ldrX(10, 13, 1), addImmX(10, 10, 31), m8Movn(11, 15), m8And(10, 10, 11))
	b.emit(addX(14, 14, 10), addX(13, 13, 10))
	b.b("m10.coalesce.merge")
	b.label("m10.coalesce.store")
	b.emit(subImmX(15, 14, 16), strX(15, 27, 1))
	appendM10SizeClass(b, 14, 16, 10, "m10.coalesce.class")
	appendM10LoadBinHead(b, 17, 19, 16, 18, 10, "m10.coalesce.loadbin")
	b.emit(m10Lsl(10, 17, 3), addImmX(10, 10, M10FreeBit), m9StrW(10, 27, 4))
	b.emit(m10Sub(18, 27, 22), m10Lsr(18, 18, 4), addImmX(18, 18, 1))
	appendM10StoreBinHead(b, 18, 19, 16, 17, 10, "m10.coalesce.storebin")
	b.label("m10.coalesce.next")
	b.emit(movX(25, 13))
	b.b("m10.coalesce.loop")
	b.label("m10.coalesce.end")
	b.emit(movX(0, 24))
	b.b("m10.sweep.restore")
	b.label("m10.sweep.fail")
	b.emit(m8Movn(0, 0))
	b.label("m10.sweep.restore")
	m10RestoreCallee(b)
}

func appendM10Alloc(b *m8Assembler) {
	b.label(m10AllocEntry)
	b.cbz(0, "m10.alloc.fail")
	b.emit(ldrX(9, 0, 0))
	b.emit(m8MovBits(10, m8HeapMagic)...)
	b.emit(m8Cmp(9, 10))
	b.bc(1, "m10.alloc.fail")
	b.cbz(1, "m10.alloc.fail")
	b.emit(movzX(9, M8MaxTypeID), m8Cmp(1, 9))
	b.bc(8, "m10.alloc.fail")
	b.emit(ldrX(9, 0, 3), m8Cmp(2, 9))
	b.bc(8, "m10.alloc.fail")
	b.emit(addImmX(3, 2, 31), m8Movn(9, 15), m8And(3, 3, 9)) // 最小占用16字节
	b.emit(ldrX(10, 0, 6), ldrX(11, 0, 1))
	// The relative index is valid across the configured heap, not merely the
	// historical 1 MiB default. The same derived bound limits corrupt cycles.
	b.emit(ldrX(14, 0, 3), m10Lsr(14, 14, 4), addImmX(14, 14, 1), movzX(15, 0))
	appendM10SizeClass(b, 3, 13, 9, "m10.alloc.class")
	b.label("m10.alloc.bin")
	appendM10LoadBinHead(b, 4, 0, 13, 9, 12, "m10.alloc.loadbin")
	b.emit(movzX(6, 0))
	b.label("m10.alloc.walk")
	b.cbnz(4, "m10.alloc.block")
	b.emit(addImmX(13, 13, 1), cmpImmX(13, 31))
	b.bc(9, "m10.alloc.bin")
	b.b("m10.alloc.bump")
	b.label("m10.alloc.block")
	b.emit(addImmX(15, 15, 1), m8Cmp(15, 14))
	b.bc(8, "m10.alloc.fail")
	b.emit(m8Cmp(4, 14)) // index <= configured capacity/16 + 1
	b.bc(8, "m10.alloc.fail")
	b.emit(subImmX(5, 4, 1), m10Lsl(5, 5, 4), addX(5, 10, 5))
	b.emit(m8Cmp(5, 10)) // 相对地址计算不允许 64 位溢出
	b.bc(3, "m10.alloc.fail")
	b.emit(m8Cmp(5, 11))
	b.bc(2, "m10.alloc.fail")
	b.emit(m9LdrW(9, 5, 0))
	b.cbnz(9, "m10.alloc.fail")
	b.emit(m9LdrW(12, 5, 4), movzX(9, 7), m8And(9, 12, 9), cmpImmX(9, M10FreeBit))
	b.bc(1, "m10.alloc.fail")
	b.emit(m10Lsr(7, 12, 3))
	b.emit(ldrX(8, 5, 1), addImmX(8, 8, 31), m8Movn(9, 15), m8And(8, 8, 9))
	b.emit(addX(9, 5, 8), m8Cmp(9, 11))
	b.bc(8, "m10.alloc.fail")
	b.emit(m8Cmp(8, 3))
	b.bc(2, "m10.alloc.found")
	b.emit(movX(6, 5), movX(4, 7))
	b.b("m10.alloc.walk")
	b.label("m10.alloc.found")
	// Unlink from the source bin before a split potentially changes class.
	b.emit(movX(16, 13))
	b.cbz(6, "m10.alloc.unlink.head")
	b.emit(m10Lsl(9, 7, 3), addImmX(9, 9, M10FreeBit), m9StrW(9, 6, 4))
	b.b("m10.alloc.unlink.done")
	b.label("m10.alloc.unlink.head")
	appendM10StoreBinHead(b, 7, 0, 16, 9, 12, "m10.alloc.unlink.storebin")
	b.label("m10.alloc.unlink.done")
	b.emit(m10Sub(12, 8, 3)) // aligned remainder span
	b.cbz(12, "m10.alloc.commit")
	b.emit(addX(14, 5, 3), subImmX(9, 12, 16), strX(9, 14, 1), movzW(9, 0), m9StrW(9, 14, 0))
	appendM10SizeClass(b, 12, 13, 9, "m10.alloc.remainder.class")
	appendM10LoadBinHead(b, 7, 0, 13, 9, 16, "m10.alloc.remainder.loadbin")
	b.emit(m10Lsl(9, 7, 3), addImmX(9, 9, M10FreeBit), m9StrW(9, 14, 4))
	b.emit(m10Sub(7, 14, 10), m10Lsr(7, 7, 4), addImmX(7, 7, 1))
	appendM10StoreBinHead(b, 7, 0, 13, 9, 16, "m10.alloc.remainder.storebin")
	b.label("m10.alloc.commit")
	b.emit(strX(1, 5, 0), strX(2, 5, 1))
	b.emit(ldrX(9, 0, 5), addImmX(9, 9, 1), strX(9, 0, 5))
	b.emit(ldrX(9, 0, 2), ldrX(12, 9, m8TelAllocatedObjects), addImmX(12, 12, 1), strX(12, 9, m8TelAllocatedObjects))
	b.emit(ldrX(12, 9, m8TelAllocatedBytes), addX(12, 12, 3), strX(12, 9, m8TelAllocatedBytes))
	b.emit(addImmX(0, 5, M8ObjectHeaderSize), subImmX(8, 3, 16))
	// 重新使用旧块前清零全部载荷及填充字节，防止旧引用残留。
	b.emit(movX(9, 0))
	b.label("m10.alloc.zero.loop")
	b.cbz(8, "m10.alloc.return")
	b.emit(strX(31, 9, 0), addImmX(9, 9, 8), subImmX(8, 8, 8))
	b.b("m10.alloc.zero.loop")
	b.label("m10.alloc.return")
	b.emit(m8Ret())
	b.label("m10.alloc.bump")
	b.b(m8AllocLabel) // 直接尾调用原 M8 allocator，保持原始 LR。
	b.label("m10.alloc.fail")
	b.emit(movzX(0, 0), m8Ret())
}
