package darwinarm64

import "fmt"

// M9GCProbe 创建独立的原生对象图自检程序。
// 测试对象的引用追踪由 ARM64 在运行时完成，并非编译期预计算标记结果。
//
// 其中 root map 只声明 frame 的 slot0/slot2、global 的 slot0 是引用；
// frame slot1、global slot1 包含真实对象指针形状的整数，却不得影响追踪结果。
func M9GCProbe(mode string) ([]byte, error) {
	switch mode {
	case "roots", "cycles", "reset", "interior", "badtype", "fanout":
	default:
		return nil, fmt.Errorf("unknown M9 probe %q (expected roots, cycles, reset, interior, badtype or fanout)", mode)
	}
	layouts := []M9TypeLayout{
		{TypeID: 11, PayloadBytes: 16, RefOffsets: []uint32{0}},
		{TypeID: 12, PayloadBytes: 16, RefOffsets: []uint32{0}},
		{TypeID: 13, PayloadBytes: 16}, // 故意保存看似指针的整数，绝不读取其引用
		{TypeID: 14, PayloadBytes: 16, RefOffsets: []uint32{0, 8}},
	}
	if mode == "fanout" {
		// 四个引用字段全部由 Type Layout 明确描述。
		layouts[3].PayloadBytes = 32
		layouts[3].RefOffsets = []uint32{0, 8, 16, 24}
	}
	roots := M9RootMap{FrameBytes: 64, FrameRefs: []uint32{0, 16}, GlobalSlots: 2, GlobalRefs: []uint32{0}}
	metadata, err := M9EncodeMetadata(layouts, roots)
	if err != nil {
		return nil, err
	}

	b := newM8Assembler()
	type pointerFixup struct {
		at     int
		reg    uint32
		global bool
		extra  int
	}
	var fixups []pointerFixup
	ptr := func(reg uint32, global bool, extra int) {
		fixups = append(fixups, pointerFixup{at: len(b.words), reg: reg, global: global, extra: extra})
		b.emit(0, 0) // ADRP reg, section page / ADD reg, reg, #pageOffset
	}
	check := func(reg uint32, expected uint16) {
		b.emit(movzX(9, expected), m8Cmp(reg, 9))
		b.bc(1, "probe.fail")
	}
	checkMarked := func(reg uint32, marked bool) {
		b.emit(subImmX(8, reg, M8ObjectHeaderSize), m9LdrW(10, 8, 4))
		want := uint16(0)
		if marked {
			want = M9MarkBit | M9ScannedBit
		}
		b.emit(movzX(9, want), m8Cmp(10, 9))
		b.bc(1, "probe.fail")
	}
	alloc := func(reg uint32, typeID uint16) {
		b.emit(movX(0, 19), movzX(1, typeID), movzX(2, 16))
		b.bl(m8AllocLabel)
		b.cbz(0, "probe.fail")
		b.emit(movX(reg, 0))
	}
	callMark := func() {
		b.emit(movX(0, 19))
		ptr(1, false, 0) // x1=Types base in __const
		b.emit(movzX(2, uint16(len(layouts))), addImmX(3, 31, 0))
		ptr(4, false, len(layouts)*M9TypeEntryBytes) // x4=RootMap in __const
		b.emit(movX(5, 28))                          // x5=全局槽位基址
		b.bl(m9MarkEntry)
	}

	// 64 字节模拟真实 Safepoint 时已经建立的原生 Stack Frame。
	b.emit(subImmX(31, 31, 64), movzX(0, 4096))
	b.bl(m8InitLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(19, 0))
	alloc(20, 11) // A -> B
	alloc(21, 11) // B -> C
	alloc(22, 11) // C -> B，形成环
	alloc(23, 13) // D：包含看似指针的整数，但没有引用字段
	alloc(24, 12) // E：被 H 的第二个引用字段指向
	alloc(25, 11) // F -> G
	alloc(26, 11) // G -> F，独立不可达环
	if mode == "fanout" {
		b.emit(movX(0, 19), movzX(1, 14), movzX(2, 32))
		b.bl(m8AllocLabel)
		b.cbz(0, "probe.fail")
		b.emit(movX(27, 0))
	} else {
		alloc(27, 14) // H -> A, E
	}

	b.emit(strX(21, 20, 0), strX(22, 21, 0), strX(21, 22, 0))
	b.emit(strX(25, 23, 0), strX(26, 25, 0), strX(25, 26, 0))
	b.emit(strX(20, 27, 0), strX(24, 27, 1))
	if mode == "fanout" {
		b.emit(strX(25, 27, 2), strX(23, 27, 3))
	}

	// 独立 __DATA 的 global slot0 是 GC Root，slot1 保存假指针。
	ptr(28, true, 0)
	b.emit(strX(25, 28, 1))
	// frame slot1 也是普通整数，值故意设成对象 G 的真实指针。
	b.emit(strX(26, 31, 1))

	switch mode {
	case "roots", "reset", "badtype", "fanout":
		b.emit(strX(20, 31, 0), strX(23, 31, 2), strX(27, 28, 0))
	case "cycles":
		b.emit(strX(21, 31, 0), movzX(9, 0), strX(9, 31, 2), strX(9, 28, 0))
	case "interior":
		b.emit(addImmX(9, 20, 8), strX(9, 31, 0), movzX(9, 0), strX(9, 31, 2), strX(9, 28, 0))
	}
	if mode == "badtype" {
		// 在已分配对象中注入不存在的 Type ID；mark 必须拒绝而不是猜测字段布局。
		b.emit(subImmX(8, 20, M8ObjectHeaderSize), movzW(9, 99), m9StrW(9, 8, 0))
	}
	callMark()
	switch mode {
	case "badtype":
		b.emit(m8Movn(9, 0), m8Cmp(0, 9))
		b.bc(1, "probe.fail")
	case "fanout":
		check(0, 8)
		for _, reg := range []uint32{20, 21, 22, 23, 24, 25, 26, 27} {
			checkMarked(reg, true)
		}
	case "roots", "reset":
		check(0, 6)
		for _, reg := range []uint32{20, 21, 22, 23, 24, 27} {
			checkMarked(reg, true)
		}
		for _, reg := range []uint32{25, 26} {
			checkMarked(reg, false)
		}
	case "cycles":
		check(0, 2)
		for _, reg := range []uint32{21, 22} {
			checkMarked(reg, true)
		}
		for _, reg := range []uint32{20, 23, 24, 25, 26, 27} {
			checkMarked(reg, false)
		}
	case "interior":
		check(0, 0)
		for _, reg := range []uint32{20, 21, 22, 23, 24, 25, 26, 27} {
			checkMarked(reg, false)
		}
	}

	if mode == "reset" {
		// 模拟下一个 Safepoint：移除旧 Roots，改为仅保留 F，必须清除前一轮标记。
		b.emit(strX(25, 31, 0), movzX(9, 0), strX(9, 31, 2), strX(9, 28, 0))
		callMark()
		check(0, 2)
		for _, reg := range []uint32{20, 21, 22, 23, 24, 27} {
			checkMarked(reg, false)
		}
		for _, reg := range []uint32{25, 26} {
			checkMarked(reg, true)
		}
	}

	// GC 不得写入任何对象的载荷，也不能修改 alloc cursor 或 count。
	b.emit(ldrX(9, 23, 0), m8Cmp(9, 25))
	b.bc(1, "probe.fail")
	b.emit(ldrX(9, 19, 5))
	check(9, 8)
	b.emit(movX(0, 19))
	b.bl(m8DestroyLabel)
	check(0, 1)
	b.emit(addImmX(31, 31, 64))
	b.label("probe.pass")
	m8WriteLiteral(b, "M9 "+mode+" PASS\n")
	b.emit(movzX(0, 0), movzX(16, 1), svc(0x80))
	b.label("probe.fail")
	m8WriteLiteral(b, "M9 roots FAIL\n")
	b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))

	appendM9Mark(b)
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, err
	}

	// 所有原生地址均经 ADRP+ADD 求得，映像本身不存在绝对指针重定位。
	constOffset := (entryOffset + uint64(len(words))*4 + 7) &^ 7
	for _, fix := range fixups {
		addr := textVMAddr + constOffset + uint64(fix.extra)
		if fix.global {
			addr = textVMAddr + pageSize
		}
		pc := textVMAddr + entryOffset + uint64(fix.at)*4
		page, err := adrpX(fix.reg, pc, addr)
		if err != nil {
			return nil, err
		}
		words[fix.at] = page
		words[fix.at+1] = addImmX(fix.reg, fix.reg, uint32(addr&0xfff))
	}
	return ExecutableWordsWithGlobalsAndConst(words, 2, metadata)
}
