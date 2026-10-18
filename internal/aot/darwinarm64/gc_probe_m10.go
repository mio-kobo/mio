package darwinarm64

import "fmt"

// M10GCProbe 会真正执行 mmap、对象分配、精确 Mark、Sweep、内存复用和 munmap。
// 该原生自检与标准 Mochi Opcode 分开，仍不自动扫描普通 Mio 程序中的 Stack Frame。
func M10GCProbe(mode string) ([]byte, error) {
	switch mode {
	case "smoke", "cycles", "split", "coalesce", "reset", "badtype", "badsize", "badlink", "oom", "interior", "stress", "holes", "zerobyte", "stale", "largeindex":
	default:
		return nil, fmt.Errorf("unknown M10 probe %q", mode)
	}
	layouts := []M9TypeLayout{
		{TypeID: 11, PayloadBytes: 16, RefOffsets: []uint32{0}},
		{TypeID: 12, PayloadBytes: 32, RefOffsets: []uint32{0, 8}},
		{TypeID: 13, PayloadBytes: 16},
		{TypeID: 14, PayloadBytes: 80},
		{TypeID: 15, PayloadBytes: 0},
	}
	roots := M9RootMap{FrameBytes: 64, FrameRefs: []uint32{0, 16}, GlobalSlots: 2, GlobalRefs: []uint32{0}}
	metadata, err := M9EncodeMetadata(layouts, roots)
	if err != nil {
		return nil, err
	}

	b := newM8Assembler()
	type pointerFixup struct {
		at, extra int
		reg       uint32
		global    bool
	}
	var fixups []pointerFixup
	ptr := func(reg uint32, global bool, extra int) {
		fixups = append(fixups, pointerFixup{at: len(b.words), reg: reg, global: global, extra: extra})
		b.emit(0, 0)
	}
	checkEq := func(a, c uint32) {
		b.emit(m8Cmp(a, c))
		b.bc(1, "m10.probe.fail")
	}
	checkInt := func(reg uint32, n uint16) {
		b.emit(movzX(9, n))
		checkEq(reg, 9)
	}
	checkNonZero := func(reg uint32) { b.cbz(reg, "m10.probe.fail") }
	checkZero := func(reg uint32) { b.cbnz(reg, "m10.probe.fail") }
	alloc := func(dst uint32, typeID uint16, size uint16) {
		b.emit(movX(0, 19), movzX(1, typeID), movzX(2, size))
		b.bl(m10AllocEntry)
		checkNonZero(0)
		b.emit(movX(dst, 0))
	}
	collect := func() {
		b.emit(movX(0, 19))
		ptr(1, false, 0)
		b.emit(movzX(2, uint16(len(layouts))), addImmX(3, 31, 0))
		ptr(4, false, len(layouts)*M9TypeEntryBytes)
		b.emit(movX(5, 28))
		b.bl(m10CollectEntry)
	}
	root := func(slot uint32, reg uint32) {
		b.emit(strX(reg, 31, slot))
	}
	rootZero := func(slot uint32) { b.emit(movzX(9, 0), strX(9, 31, slot)) }
	checkCount := func(n uint16) {
		b.emit(ldrX(10, 19, 5))
		checkInt(10, n)
	}
	checkHeader := func(p uint32, typ uint16, payload uint16) {
		b.emit(subImmX(8, p, M8ObjectHeaderSize), m9LdrW(10, 8, 0))
		checkInt(10, typ)
		b.emit(ldrX(10, 8, 1))
		checkInt(10, payload)
	}

	b.emit(subImmX(31, 31, 64)) // 一整个 Safepoint frame，所有根槽必须显式赋值。
	b.emit(movzX(0, 4096))
	if mode == "oom" {
		b.emit(movzX(0, 64))
	} else if mode == "largeindex" {
		b.emit(m8MovBits(0, 2<<20)...)
	}
	b.bl(m8InitLabel)
	checkNonZero(0)
	b.emit(movX(19, 0), movzX(9, 0), strX(9, 31, 0), strX(9, 31, 1), strX(9, 31, 2))
	ptr(28, true, 0) // 两个全局槽位，global slot1 故意不声明为根。
	b.emit(movzX(9, 0), strX(9, 28, 0), strX(9, 28, 1))

	switch mode {
	case "largeindex":
		// A valid free block beyond the historical 1 MiB index boundary must
		// remain addressable when the configured heap is larger.
		b.emit(ldrX(20, 19, 6))
		b.emit(m8MovBits(9, 1<<20)...)
		b.emit(addX(21, 20, 9), movzW(9, 0), m9StrW(9, 21, 0), movzW(9, uint16(M10FreeBit)), m9StrW(9, 21, 4), movzX(9, 16), strX(9, 21, 1))
		b.emit(addImmX(9, 21, 32), strX(9, 19, 1))
		b.emit(m8MovBits(9, (1<<20)/16+1)...)
		b.emit(ldrX(10, 19, 2), strX(9, 10, m8TelFreeBin1))
		alloc(22, 13, 16)
		b.emit(addImmX(9, 21, 16))
		checkEq(22, 9)
		checkCount(1)

	case "smoke":
		alloc(20, 11, 16) // A->B
		alloc(21, 11, 16) // B
		alloc(22, 13, 16) // C，不可达
		b.emit(strX(21, 20, 0))
		root(0, 20)
		collect()
		checkInt(0, 1)
		checkCount(2)
		b.emit(ldrX(10, 20, 0))
		checkEq(10, 21)
		b.emit(ldrX(9, 19, 2), ldrX(10, 9, m8TelFreeBin1))
		checkNonZero(10)
		alloc(23, 13, 16)
		checkEq(23, 22)
		checkHeader(23, 13, 16)
		b.emit(ldrX(10, 23, 0))
		checkZero(10)
		checkCount(3)
		collect() // 旧 Free Bit 不得妨碍下一轮标记，D 又应被回收。
		checkInt(0, 1)
		checkCount(2)

	case "cycles":
		alloc(20, 11, 16) // A
		alloc(21, 11, 16) // B
		alloc(22, 11, 16) // C
		alloc(23, 11, 16) // D
		b.emit(strX(21, 20, 0), strX(20, 21, 0))
		b.emit(strX(23, 22, 0), strX(22, 23, 0))
		root(0, 20)
		root(1, 22)             // 故意在非 Root 槽保存真正的 C 指针。
		b.emit(strX(23, 28, 1)) // 另一个伪根：global slot1。
		collect()
		checkInt(0, 2)
		checkCount(2)
		b.emit(ldrX(10, 20, 0))
		checkEq(10, 21)
		b.emit(ldrX(10, 21, 0))
		checkEq(10, 20)
		// C、D 连续回收合并，再各分配一个 16 字节对象。
		alloc(24, 13, 16)
		checkEq(24, 22)
		alloc(25, 13, 16)
		checkEq(25, 23)

	case "split":
		alloc(20, 14, 80)
		collect()
		checkInt(0, 1)
		checkCount(0)
		alloc(21, 13, 16)
		checkEq(20, 21)
		alloc(22, 13, 16)
		b.emit(addImmX(9, 20, 32))
		checkEq(22, 9)
		alloc(23, 13, 16)
		b.emit(addImmX(9, 20, 64))
		checkEq(23, 9)
		checkCount(3)
		checkHeader(23, 13, 16)

	case "coalesce":
		for reg := uint32(20); reg < 24; reg++ {
			alloc(reg, 13, 16)
		}
		collect()
		checkInt(0, 4)
		checkCount(0)
		alloc(24, 14, 80) // 单个旧块容不下，必须依赖相邻空闲块合并。
		checkEq(24, 20)
		alloc(25, 13, 16)
		b.emit(addImmX(9, 20, 96))
		checkEq(25, 9)
		checkCount(2)

	case "reset":
		alloc(20, 11, 16)
		alloc(21, 11, 16)
		alloc(22, 13, 16)
		b.emit(strX(21, 20, 0))
		root(0, 20)
		collect()
		checkInt(0, 1) // C 不可达。
		alloc(23, 13, 16)
		checkEq(23, 22)
		rootZero(0)
		root(2, 23)
		collect()
		checkInt(0, 2) // 原来的 A/B 不再可达，D 必须保留。
		checkCount(1)
		checkHeader(23, 13, 16)
		alloc(24, 11, 16)
		checkEq(24, 20)

	case "badtype":
		alloc(20, 13, 16)
		alloc(21, 13, 16)
		b.emit(subImmX(8, 21, 16), movzW(9, 99), m9StrW(9, 8, 0))
		b.emit(ldrX(27, 19, 1)) // 保存 cursor
		collect()
		b.emit(m8Movn(9, 0))
		checkEq(0, 9)
		b.emit(ldrX(10, 19, 1))
		checkEq(10, 27)
		checkCount(2)
		b.emit(ldrX(10, 19, 7))
		checkZero(10)

	case "badsize":
		alloc(20, 13, 16)
		b.emit(subImmX(8, 20, 16), m8Movn(9, 0), strX(9, 8, 1))
		b.emit(ldrX(27, 19, 1))
		collect()
		b.emit(m8Movn(9, 0))
		checkEq(0, 9) // 必须拒绝畸形长度，不能产生越界读写或进入 Mark。
		b.emit(ldrX(10, 19, 1))
		checkEq(10, 27)
		checkCount(1)
		b.emit(ldrX(10, 19, 7))
		checkZero(10)

	case "badlink":
		alloc(20, 13, 16)
		collect() // 整块变为 free，且有可复用的链表头。
		checkInt(0, 1)
		b.emit(ldrX(27, 19, 1))
		b.emit(ldrX(10, 19, 2), movzX(9, 0), movkX(9, 2, 1), strX(9, 10, m8TelFreeBin1)) // 131072 > 1 MiB/16
		b.emit(movX(0, 19), movzX(1, 13), movzX(2, 16))
		b.bl(m10AllocEntry)
		checkZero(0)
		b.emit(ldrX(10, 19, 1))
		checkEq(10, 27)
		checkCount(0)

	case "holes":
		alloc(20, 14, 80) // A: 空闲大块
		alloc(21, 13, 16) // B: 保留
		alloc(22, 14, 80) // C: 空闲大块
		alloc(23, 13, 16) // D: 保留
		root(0, 21)
		root(2, 23)
		collect()
		checkInt(0, 2)
		checkCount(2)
		alloc(24, 13, 16) // 第一块来自高地址的 C；拆分残余块
		checkEq(24, 22)
		alloc(25, 14, 80) // C 的残余不够；必须沿链表找到 A
		checkEq(25, 20)
		checkCount(4)

	case "stale":
		alloc(20, 13, 16) // A
		alloc(21, 13, 16) // B
		collect()         // A/B 全部不可达，合并成 64 字节自由区。
		checkInt(0, 2)
		alloc(22, 12, 32) // 新对象跨越旧 A 和旧 B 的物理位置。
		checkEq(22, 20)
		root(0, 21) // B 的旧地址已不是一个有效对象的 payload 起点。
		collect()
		checkInt(0, 1)
		checkCount(0)

	case "zerobyte":
		alloc(20, 15, 0)
		checkHeader(20, 15, 0)
		collect()
		checkInt(0, 1)
		checkCount(0)
		alloc(21, 15, 0)
		checkEq(21, 20) // 16 字节的块也必须可被链表记录和复用。
		checkHeader(21, 15, 0)

	case "oom":
		alloc(20, 13, 16)
		alloc(21, 13, 16)
		b.emit(movX(0, 19), movzX(1, 13), movzX(2, 16))
		b.bl(m10AllocEntry)
		checkZero(0)
		checkCount(2)
		root(0, 20)
		collect()
		checkInt(0, 1)
		alloc(22, 13, 16)
		checkEq(22, 21)
		checkCount(2)

	case "interior":
		alloc(20, 13, 16)
		b.emit(addImmX(9, 20, 8))
		root(0, 9)  // 内部地址不是合法 Root。
		root(1, 20) // slot1 是普通整数，即使位模式为合法载荷地址也不扫描。
		collect()
		checkInt(0, 1)
		checkCount(0)
		alloc(21, 13, 16)
		checkEq(21, 20)

	case "stress":
		alloc(20, 11, 16)
		alloc(21, 11, 16)
		b.emit(strX(21, 20, 0))
		root(0, 20)
		b.emit(movzX(24, 0), movzX(25, 64))
		b.label("m10.probe.stress.loop")
		b.emit(movX(0, 19), movzX(1, 13), movzX(2, 16))
		b.bl(m10AllocEntry)
		checkNonZero(0)
		b.emit(movX(23, 0))
		collect()
		checkInt(0, 1)
		checkCount(2)
		b.emit(ldrX(10, 20, 0))
		checkEq(10, 21)
		b.emit(addImmX(24, 24, 1), m8Cmp(24, 25))
		b.bc(1, "m10.probe.stress.loop")
		checkCount(2)
	}

	b.emit(movX(0, 19))
	b.bl(m8DestroyLabel)
	checkInt(0, 1)
	b.emit(addImmX(31, 31, 64))
	b.label("m10.probe.pass")
	m8WriteLiteral(b, "M10 "+mode+" PASS\n")
	b.emit(movzX(0, 0), movzX(16, 1), svc(0x80))
	b.label("m10.probe.fail")
	m8WriteLiteral(b, "M10 "+mode+" FAIL\n")
	b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))

	appendM10Collect(b)
	appendM10Sweep(b)
	appendM10Alloc(b)
	appendM9Mark(b)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, err
	}
	constOffset := (entryOffset + uint64(len(words))*4 + 7) &^ 7
	for _, fix := range fixups {
		addr := textVMAddr + constOffset + uint64(fix.extra)
		if fix.global {
			addr = textVMAddr + pageSize
		}
		pc := textVMAddr + entryOffset + uint64(fix.at)*4
		first, err := adrpX(fix.reg, pc, addr)
		if err != nil {
			return nil, err
		}
		words[fix.at] = first
		words[fix.at+1] = addImmX(fix.reg, fix.reg, uint32(addr&0xfff))
	}
	return ExecutableWordsWithGlobalsAndConst(words, 2, metadata)
}
