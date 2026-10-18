package darwinarm64

import (
	"fmt"
)

// M8 的 Heap 是通过 Darwin mmap 获取的独立原生内存区域，不属于 Go 堆。
// 每个 Heap 的前 64 字节保存分配状态，后方保存 16 字节对齐的对象。
// 对象指针指向载荷，紧邻指针前方的 16 字节为对象头：
//
//	-16: uint32 typeID
//	-12: uint32 flags（M8 固定为 0）
//	 -8: uint64 payloadSize
//
// 未来 GC 可以根据 typeID 查询指针布局，但 M8 尚不追踪引用，也不回收单个对象。
const (
	M8HeapHeaderSize      = 64
	M8ObjectHeaderSize    = 16
	M8ObjectAlignment     = 16
	M8DefaultHeapCapacity = 1 << 20
	M8MaxHeapCapacity     = 1 << 30
	M8TelemetrySize       = 512
	M8MaxTypeID           = 65535
	m8DarwinSYSWrite      = 4
	m8DarwinSYSMunmap     = 73
	m8DarwinSYSMmap       = 197
	m8DarwinMapAnonPriv   = 0x1002
	m8HeapMagic           = uint64(0x38504145484F494D) // 小端内存字节顺序为 "MIOHEAP8"
)

// Telemetry lives immediately after the managed object area. Keeping it out
// of the 64-byte heap header preserves the established object origin ABI.
// All counters are uint64 and are written only by allocation/GC code.
const (
	m8TelAllocatedObjects = iota
	m8TelAllocatedBytes
	m8TelGCCount
	m8TelLastBeforeBytes
	m8TelLastAfterBytes
	m8TelLastReclaimedObjects
	m8TelLastReclaimedBytes
	m8TelLastFreeBytes
	m8TelLastLargestFree
	m8TelLastFreeBlocks
	m8TelLastBumpRemaining
	m8TelOOMRequestBytes
	m8TelShadowDepth
	m8TelRootCount
	m8TelLastGCTicks
	m8TelTotalGCTicks
	m8TelTimerFrequency
	m8TelFreeBin1 = 32 // bins 1..31 occupy telemetry words 32..62
)

const (
	m8InitLabel    = "heap.init"
	m8AllocLabel   = "heap.alloc"
	m8DestroyLabel = "heap.destroy"
)

type m8RelocKind uint8

const (
	m8B m8RelocKind = iota
	m8BL
	m8Cond
	m8CBZ
	m8CBNZ
)

type m8Reloc struct {
	at    int
	kind  m8RelocKind
	label string
	cond  uint32
	reg   uint32
}

// m8Assembler 在编译期进行标签回填，不要求系统链接器或 dyld 处理重定位。
// 运行时只使用 AArch64 指令，所有函数的返回值统一放在 x0。
type m8Assembler struct {
	words  []uint32
	labels map[string]int
	relocs []m8Reloc
	err    error
}

func newM8Assembler() *m8Assembler {
	return &m8Assembler{labels: make(map[string]int)}
}

func (b *m8Assembler) emit(words ...uint32) { b.words = append(b.words, words...) }

func (b *m8Assembler) label(name string) {
	if _, ok := b.labels[name]; ok {
		b.err = fmt.Errorf("duplicate M8 native label %q", name)
		return
	}
	b.labels[name] = len(b.words)
}

func (b *m8Assembler) jump(kind m8RelocKind, label string, cond, reg uint32) {
	b.relocs = append(b.relocs, m8Reloc{at: len(b.words), kind: kind, label: label, cond: cond, reg: reg})
	b.emit(0)
}
func (b *m8Assembler) b(label string)                { b.jump(m8B, label, 0, 0) }
func (b *m8Assembler) bl(label string)               { b.jump(m8BL, label, 0, 0) }
func (b *m8Assembler) bc(cond uint32, label string)  { b.jump(m8Cond, label, cond, 0) }
func (b *m8Assembler) cbz(reg uint32, label string)  { b.jump(m8CBZ, label, 0, reg) }
func (b *m8Assembler) cbnz(reg uint32, label string) { b.jump(m8CBNZ, label, 0, reg) }

func (b *m8Assembler) assemble() ([]uint32, error) {
	if b.err != nil {
		return nil, b.err
	}
	words := append([]uint32(nil), b.words...)
	for _, fix := range b.relocs {
		target, found := b.labels[fix.label]
		if !found {
			return nil, fmt.Errorf("M8 missing ARM64 label %q", fix.label)
		}
		var word uint32
		var err error
		distance := target - fix.at // AArch64 的 B/BL/B.cond 按当前指令计算相对位移。
		switch fix.kind {
		case m8B:
			word, err = branch26(0x14000000, fix.at, target)
		case m8BL:
			word, err = branch26(0x94000000, fix.at, target)
		case m8Cond:
			if distance < -(1<<18) || distance >= 1<<18 {
				err = fmt.Errorf("M8 B.cond target %q out of range", fix.label)
			} else {
				word = 0x54000000 | uint32(int32(distance)&0x7ffff)<<5 | fix.cond
			}
		case m8CBZ, m8CBNZ:
			if distance < -(1<<18) || distance >= 1<<18 {
				err = fmt.Errorf("M8 CBZ/CBNZ target %q out of range", fix.label)
			} else {
				base := uint32(0xB4000000)
				if fix.kind == m8CBNZ {
					base = 0xB5000000
				}
				word = base | uint32(int32(distance)&0x7ffff)<<5 | fix.reg
			}
		default:
			err = fmt.Errorf("M8 invalid fixup kind %d", fix.kind)
		}
		if err != nil {
			return nil, err
		}
		words[fix.at] = word
	}
	return words, nil
}

func m8MovBits(reg uint32, bits uint64) []uint32 {
	return []uint32{
		movzX(reg, uint16(bits)),
		movkX(reg, uint16(bits>>16), 1),
		movkX(reg, uint16(bits>>32), 2),
		movkX(reg, uint16(bits>>48), 3),
	}
}

func m8Cmp(regA, regB uint32) uint32       { return 0xEB00001F | regB<<16 | regA<<5 }
func m8And(dst, a, c uint32) uint32        { return 0x8A000000 | c<<16 | a<<5 | dst }
func m8Movn(reg uint32, imm uint16) uint32 { return 0x92800000 | uint32(imm)<<5 | reg }
func m8LdrB(dst, addr uint32) uint32       { return 0x39400000 | addr<<5 | dst }
func m8StrB(src, addr uint32) uint32       { return 0x39000000 | addr<<5 | src }
func m8Ret() uint32                        { return 0xD65F03C0 }
func mrsCNTFRQ(rd uint32) uint32           { return 0xD53BE000 | rd }
func mrsCNTVCT(rd uint32) uint32           { return 0xD53BE040 | rd }

// appendM8Runtime 追加三个原生函数：
//
//	heap.init(x0=容量) -> x0=Heap* 或 0
//	heap.alloc(x0=Heap*, x1=类型ID, x2=载荷字节数) -> x0=载荷指针或 0
//	heap.destroy(x0=Heap*) -> x0=1 成功、0 失败
//
// x9..x15 是临时寄存器，x19..x29 不得修改，SP 始终保持 16 字节对齐。
func appendM8Runtime(b *m8Assembler) {
	b.label(m8InitLabel)
	// 先拒绝零容量和异常大容量，避免大小溢出或无限制申请虚拟内存。
	b.cbz(0, "init.fail")
	b.emit(m8MovBits(9, M8MaxHeapCapacity)...)
	b.emit(m8Cmp(0, 9))
	b.bc(8, "init.fail") // B.HI
	// syscall 不依赖用户寄存器保存，容量先存到对齐的栈空间。
	b.emit(subImmX(31, 31, 16), strX(0, 31, 0))
	b.emit(addImmX(1, 0, M8HeapHeaderSize+M8TelemetrySize))
	b.emit(movzX(0, 0), movzX(2, 3), movzX(3, m8DarwinMapAnonPriv), m8Movn(4, 0), movzX(5, 0), movzX(16, m8DarwinSYSMmap), svc(0x80))
	// Darwin 的 BSD syscall 以 C=1 表示 errno，绝不能将错误码当作指针。
	b.bc(2, "init.sysfail") // B.CS
	b.emit(ldrX(10, 31, 0), addImmX(31, 31, 16))
	b.emit(movX(9, 0))
	b.emit(m8MovBits(11, m8HeapMagic)...)
	b.emit(strX(11, 9, 0))
	b.emit(addImmX(11, 9, M8HeapHeaderSize), strX(11, 9, 6))                  // heap[48]: origin
	b.emit(strX(11, 9, 1))                                                    // heap[8]: cursor
	b.emit(addX(12, 11, 10), strX(12, 9, 2))                                  // heap[16]: end
	b.emit(strX(10, 9, 3))                                                    // heap[24]: requested capacity
	b.emit(addImmX(12, 10, M8HeapHeaderSize+M8TelemetrySize), strX(12, 9, 4)) // heap[32]: mmap length
	b.emit(movzX(12, 0), strX(12, 9, 5), strX(12, 9, 7))                      // count、reserved
	b.emit(movX(0, 9), m8Ret())
	b.label("init.sysfail")
	b.emit(addImmX(31, 31, 16))
	b.label("init.fail")
	b.emit(movzX(0, 0), m8Ret())

	b.label(m8AllocLabel)
	b.cbz(0, "alloc.fail")
	b.emit(ldrX(9, 0, 0))
	b.emit(m8MovBits(10, m8HeapMagic)...)
	b.emit(m8Cmp(9, 10))
	b.bc(1, "alloc.fail") // B.NE
	b.cbz(1, "alloc.fail")
	b.emit(movzX(10, M8MaxTypeID), m8Cmp(1, 10))
	b.bc(8, "alloc.fail")                // B.HI
	b.emit(ldrX(10, 0, 3), m8Cmp(2, 10)) // request <= capacity
	b.bc(8, "alloc.fail")
	b.emit(ldrX(9, 0, 1))  // cursor
	b.emit(ldrX(10, 0, 2)) // end
	// totalSize = align16(16 + payloadSize)，先验证 payloadSize<=capacity 避免溢出。
	b.emit(addImmX(11, 2, 31), m8Movn(12, 15), m8And(11, 11, 12))
	b.emit(addX(13, 9, 11), m8Cmp(13, 10))
	b.bc(8, "alloc.fail")                // 超出 end，不写对象头，不移动 cursor。
	b.emit(strX(1, 9, 0), strX(2, 9, 1)) // uint32 typeID、flags=0、uint64 size
	b.emit(strX(13, 0, 1))
	b.emit(ldrX(14, 0, 5), addImmX(14, 14, 1), strX(14, 0, 5))
	// Cumulative allocation counters use physical bytes (header + alignment),
	// not requested payload bytes.
	b.emit(ldrX(14, 0, 2), ldrX(15, 14, m8TelAllocatedObjects), addImmX(15, 15, 1), strX(15, 14, m8TelAllocatedObjects))
	b.emit(ldrX(15, 14, m8TelAllocatedBytes), addX(15, 15, 11), strX(15, 14, m8TelAllocatedBytes))
	b.emit(addImmX(0, 9, M8ObjectHeaderSize), m8Ret())
	b.label("alloc.fail")
	b.emit(movzX(0, 0), m8Ret())

	b.label(m8DestroyLabel)
	b.cbz(0, "destroy.fail")
	b.emit(ldrX(9, 0, 0))
	b.emit(m8MovBits(10, m8HeapMagic)...)
	b.emit(m8Cmp(9, 10))
	b.bc(1, "destroy.fail")
	b.emit(ldrX(1, 0, 4), movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.bc(2, "destroy.fail") // B.CS: munmap 失败
	b.emit(movzX(0, 1), m8Ret())
	b.label("destroy.fail")
	b.emit(movzX(0, 0), m8Ret())
}

// M8RuntimeEntries 是相对于返回机器码片段起始位置的指令索引。
// 后续 AOT 后端可以把片段追加到程序末尾，再用 branch26 回填 BL。
// 片段内部的跳转全是 PC 相对跳转，可直接整体搬移。
type M8RuntimeEntries struct {
	Init    int
	Alloc   int
	Destroy int
}

// M8AllocatorRuntimeWords 提供可嵌入普通 AOT 产物的原生分配器例程。
// M8 先完成 runtime 本身的实机验证，后续再引入堆分配的语言语义和 Opcode。
func M8AllocatorRuntimeWords() ([]uint32, M8RuntimeEntries, error) {
	b := newM8Assembler()
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, M8RuntimeEntries{}, err
	}
	return words, M8RuntimeEntries{
		Init: b.labels[m8InitLabel], Alloc: b.labels[m8AllocLabel], Destroy: b.labels[m8DestroyLabel],
	}, nil
}

// M8AllocatorProbe 将分配器例程和原生边界测试一起编译进一个 Mach-O。
// 该自检入口不修改 Mochi Opcode；未来由 AOT 的对象指令调用同一套原生函数。
func M8AllocatorProbe(mode string) ([]byte, error) {
	b := newM8Assembler()
	switch mode {
	case "smoke":
		m8SmokeProbe(b)
	case "limits":
		m8LimitsProbe(b)
	case "metadata":
		m8MetadataProbe(b)
	default:
		return nil, fmt.Errorf("unknown M8 probe %q (expected smoke, limits or metadata)", mode)
	}
	appendM8Runtime(b)
	words, err := b.assemble()
	if err != nil {
		return nil, err
	}
	return ExecutableWords(words)
}

func m8CheckEqual(b *m8Assembler, regA, regB uint32) {
	b.emit(m8Cmp(regA, regB))
	b.bc(1, "probe.fail") // B.NE
}
func m8CheckAligned(b *m8Assembler, reg uint32) {
	b.emit(movzX(9, 15), m8And(9, reg, 9))
	b.cbnz(9, "probe.fail")
}
func m8CallAlloc(b *m8Assembler, typeID, size uint16) {
	b.emit(movX(0, 19), movzX(1, typeID), movzX(2, size))
	b.bl(m8AllocLabel)
}
func m8CheckMetadata(b *m8Assembler, typeID, size uint16) {
	// 分配的返回指针是 payload，头部紧邻其前 16 字节。
	b.emit(subImmX(9, 0, M8ObjectHeaderSize), ldrX(10, 9, 0), movzX(11, typeID))
	m8CheckEqual(b, 10, 11)
	b.emit(ldrX(10, 9, 1), movzX(11, size))
	m8CheckEqual(b, 10, 11)
}
func m8ProbeEnd(b *m8Assembler, message string) {
	// 到达这里才证明所有原生检查均通过；输出只是验收结果，不是预置执行结果。
	b.label("probe.pass")
	m8WriteLiteral(b, message+"\n")
	b.emit(movzX(0, 0), movzX(16, 1), svc(0x80))
	b.label("probe.fail")
	m8WriteLiteral(b, "M8 allocator FAIL\n")
	b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))
}
func m8WriteLiteral(b *m8Assembler, text string) {
	// 不引入 libc；消息先放在对齐的栈缓冲区再通过 Darwin write(1, ...) 写出。
	frame := (len(text) + 15) &^ 15
	b.emit(subImmX(31, 31, uint32(frame)))
	for index := range text {
		b.emit(movzW(9, uint16(text[index])))
		b.emit(0x39000000 | uint32(index)<<10 | 31<<5 | 9) // STRB W9,[SP,#index]
	}
	b.emit(movzX(0, 1), addImmX(1, 31, 0), movzX(2, uint16(len(text))), movzX(16, m8DarwinSYSWrite), svc(0x80))
	b.emit(addImmX(31, 31, uint32(frame)))
}

func m8SmokeProbe(b *m8Assembler) {
	b.emit(movzX(0, 1024))
	b.bl(m8InitLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(19, 0)) // x19 是 Heap*；Runtime 保证不修改被调用者保存寄存器。
	m8CheckAligned(b, 19)
	b.emit(ldrX(10, 19, 0))
	b.emit(m8MovBits(11, m8HeapMagic)...)
	m8CheckEqual(b, 10, 11)

	// 检查 0/1/15/16/17/127 字节分配的真实地址与 16 字节对齐布局。
	used := 0
	for index, size := range []uint16{0, 1, 15, 16, 17, 127} {
		id := uint16(index + 1)
		m8CallAlloc(b, id, size)
		b.cbz(0, "probe.fail")
		m8CheckAligned(b, 0)
		m8CheckMetadata(b, id, size)
		b.emit(ldrX(10, 19, 6), addImmX(10, 10, uint32(used+M8ObjectHeaderSize)))
		m8CheckEqual(b, 0, 10)
		if size == 1 {
			b.emit(movX(20, 0), movzW(9, 0x5A), m8StrB(9, 20))
		}
		if size == 15 {
			b.emit(movX(21, 0), movzW(9, 0x7E), m8StrB(9, 21))
		}
		used += (int(size) + 31) &^ 15
	}
	b.emit(ldrX(10, 19, 5), movzX(11, 6))
	m8CheckEqual(b, 10, 11)
	b.emit(ldrX(10, 19, 1), ldrX(11, 19, 6), addImmX(11, 11, uint32(used)))
	m8CheckEqual(b, 10, 11)
	b.emit(m8LdrB(10, 20), movzX(11, 0x5A))
	m8CheckEqual(b, 10, 11)
	b.emit(m8LdrB(10, 21), movzX(11, 0x7E))
	m8CheckEqual(b, 10, 11)

	// 失败分配必须保持对象计数、游标和原有载荷不变。
	for _, request := range []uint16{1025} {
		m8CallAlloc(b, 7, request)
		b.cbnz(0, "probe.fail")
	}
	b.emit(movX(0, 19), movzX(1, 7), m8Movn(2, 0))
	b.bl(m8AllocLabel)
	b.cbnz(0, "probe.fail")
	b.emit(ldrX(10, 19, 5), movzX(11, 6))
	m8CheckEqual(b, 10, 11)
	b.emit(ldrX(10, 19, 1), ldrX(11, 19, 6), addImmX(11, 11, uint32(used)))
	m8CheckEqual(b, 10, 11)
	b.emit(movX(0, 19))
	b.bl(m8DestroyLabel)
	b.emit(movzX(11, 1))
	m8CheckEqual(b, 0, 11)
	m8ProbeEnd(b, "M8 smoke PASS")
}

func m8LimitsProbe(b *m8Assembler) {
	b.emit(movzX(0, 0))
	b.bl(m8InitLabel)
	b.cbnz(0, "probe.fail")
	b.emit(movzX(0, 0), movkX(0, 17, 1)) // 17 × 65536 > 1 MiB
	b.bl(m8InitLabel)
	b.cbnz(0, "probe.fail")
	b.emit(movzX(0, 48))
	b.bl(m8InitLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(19, 0))
	// 零类型、非法类型、超大载荷均不消耗空间。
	b.emit(movX(0, 19), movzX(1, 0), movzX(2, 1))
	b.bl(m8AllocLabel)
	b.cbnz(0, "probe.fail")
	b.emit(movX(0, 19), movzX(1, 1), movzX(2, 0), movkX(2, 1, 1)) // payload 65536
	b.bl(m8AllocLabel)
	b.cbnz(0, "probe.fail")
	b.emit(movX(0, 19), movzX(1, 0), movkX(1, 1, 1), movzX(2, 1)) // typeID 65536
	b.bl(m8AllocLabel)
	b.cbnz(0, "probe.fail")
	m8CallAlloc(b, 65535, 32) // 精确耗尽容量：16 字节头 +32 字节载荷
	b.cbz(0, "probe.fail")
	m8CheckMetadata(b, 65535, 32)
	m8CallAlloc(b, 1, 0)
	b.cbnz(0, "probe.fail")
	b.emit(ldrX(10, 19, 5), movzX(11, 1))
	m8CheckEqual(b, 10, 11)
	b.emit(ldrX(10, 19, 1), ldrX(11, 19, 2))
	m8CheckEqual(b, 10, 11)
	b.emit(movX(0, 19))
	b.bl(m8DestroyLabel)
	b.emit(movzX(11, 1))
	m8CheckEqual(b, 0, 11)
	m8ProbeEnd(b, "M8 limits PASS")
}

func m8MetadataProbe(b *m8Assembler) {
	b.emit(movzX(0, 2048))
	b.bl(m8InitLabel)
	b.cbz(0, "probe.fail")
	b.emit(movX(19, 0), movzX(20, 0), movzX(23, 0))
	// 真实的 ARM64 循环，每次为对象分配 8 字节载荷并检查地址单调性。
	b.label("metadata.loop")
	b.emit(movX(0, 19), addImmX(1, 23, 1), movzX(2, 8))
	b.bl(m8AllocLabel)
	b.cbz(0, "probe.fail")
	m8CheckAligned(b, 0)
	b.emit(subImmX(9, 0, 16), ldrX(10, 9, 0), addImmX(11, 23, 1))
	m8CheckEqual(b, 10, 11)
	b.emit(ldrX(10, 9, 1), movzX(11, 8))
	m8CheckEqual(b, 10, 11)
	b.cbz(20, "metadata.first")
	b.emit(addImmX(10, 20, 32))
	m8CheckEqual(b, 0, 10)
	b.label("metadata.first")
	b.emit(movX(20, 0), addImmX(23, 23, 1), cmpImmX(23, 32))
	b.bc(3, "metadata.loop") // B.LO：执行 32 次原生循环
	b.emit(ldrX(10, 19, 5), movzX(11, 32))
	m8CheckEqual(b, 10, 11)
	b.emit(ldrX(10, 19, 1), ldrX(11, 19, 6), addImmX(11, 11, 1024))
	m8CheckEqual(b, 10, 11)
	b.emit(movX(0, 19))
	b.bl(m8DestroyLabel)
	b.emit(movzX(11, 1))
	m8CheckEqual(b, 0, 11)
	m8ProbeEnd(b, "M8 metadata PASS")
}
