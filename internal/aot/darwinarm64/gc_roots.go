package darwinarm64

import (
	"encoding/binary"
	"fmt"
)

// M9 是精确 GC 的标记与元数据原型，尚不释放单个对象。
// 类型描述符存放在 Mach-O __TEXT,__const 中，不包含运行时绝对地址。
const (
	M9MaxTypes       = 8
	M9MaxRefFields   = 4
	M9MaxFrameRoots  = 8
	M9MaxGlobalRoots = 8
	M9TypeEntryBytes = 32
	M9RootMapBytes   = 80
	M9MarkBit        = 1
	M9ScannedBit     = 2
)

// M9TypeLayout 明确指明类型的载荷大小与哪些 8 字节槽位保存 GC 引用。
// 非引用整数即使碰巧等于对象地址，也绝不会沿该字段继续追踪。
type M9TypeLayout struct {
	TypeID       uint32
	PayloadBytes uint32
	RefOffsets   []uint32
}

// M9RootMap 描述一个 Safepoint 的帧和全局根。
// FrameRefs 是相对于 Safepoint 时传入的 frameBase 的字节偏移；
// GlobalRefs 是全局变量的 8 字节槽位索引，不扫描未登记的槽位。
// 后续普通 AOT 应基于类型检查结果，在每个 Safepoint 构造对应的 Map。
type M9RootMap struct {
	FrameBytes  uint32
	FrameRefs   []uint32
	GlobalSlots uint32
	GlobalRefs  []uint32
}

// M9EncodeMetadata 返回可以直接装入只读区的数据，不使用绝对指针。
// 数据格式：N 个 32 字节类型描述符，后接一个 80 字节 Root Map。
func M9EncodeMetadata(layouts []M9TypeLayout, roots M9RootMap) ([]byte, error) {
	if len(layouts) == 0 || len(layouts) > M9MaxTypes {
		return nil, fmt.Errorf("M9 type count must be 1..%d", M9MaxTypes)
	}
	seen := make(map[uint32]bool, len(layouts))
	data := make([]byte, len(layouts)*M9TypeEntryBytes+M9RootMapBytes)
	for i, layout := range layouts {
		if layout.TypeID == 0 || layout.TypeID > M8MaxTypeID || seen[layout.TypeID] {
			return nil, fmt.Errorf("M9 invalid or duplicate type ID %d", layout.TypeID)
		}
		if layout.PayloadBytes > M8MaxHeapCapacity || len(layout.RefOffsets) > M9MaxRefFields {
			return nil, fmt.Errorf("M9 invalid type %d payload or reference count", layout.TypeID)
		}
		seen[layout.TypeID] = true
		base := i * M9TypeEntryBytes
		binary.LittleEndian.PutUint32(data[base:], layout.TypeID)
		binary.LittleEndian.PutUint32(data[base+4:], layout.PayloadBytes)
		binary.LittleEndian.PutUint32(data[base+8:], uint32(len(layout.RefOffsets)))
		for j, off := range layout.RefOffsets {
			if off%8 != 0 || uint64(off)+8 > uint64(layout.PayloadBytes) {
				return nil, fmt.Errorf("M9 type %d invalid reference offset %d", layout.TypeID, off)
			}
			for k := 0; k < j; k++ {
				if layout.RefOffsets[k] == off {
					return nil, fmt.Errorf("M9 type %d duplicate reference offset %d", layout.TypeID, off)
				}
			}
			binary.LittleEndian.PutUint32(data[base+16+j*4:], off)
		}
	}
	if roots.FrameBytes > 65536 || roots.FrameBytes%16 != 0 || roots.GlobalSlots > uint32(MaxGlobalSlots) || len(roots.FrameRefs) > M9MaxFrameRoots || len(roots.GlobalRefs) > M9MaxGlobalRoots {
		return nil, fmt.Errorf("M9 invalid frame or global root map bounds")
	}
	base := len(layouts) * M9TypeEntryBytes
	binary.LittleEndian.PutUint32(data[base:], roots.FrameBytes)
	binary.LittleEndian.PutUint32(data[base+4:], uint32(len(roots.FrameRefs)))
	binary.LittleEndian.PutUint32(data[base+8:], roots.GlobalSlots)
	binary.LittleEndian.PutUint32(data[base+12:], uint32(len(roots.GlobalRefs)))
	for i, off := range roots.FrameRefs {
		if off%8 != 0 || uint64(off)+8 > uint64(roots.FrameBytes) {
			return nil, fmt.Errorf("M9 frame root offset %d outside frame", off)
		}
		for j := 0; j < i; j++ {
			if roots.FrameRefs[j] == off {
				return nil, fmt.Errorf("M9 duplicate frame root offset %d", off)
			}
		}
		binary.LittleEndian.PutUint32(data[base+16+i*4:], off)
	}
	for i, slot := range roots.GlobalRefs {
		if slot >= roots.GlobalSlots {
			return nil, fmt.Errorf("M9 global root slot %d outside globals", slot)
		}
		for j := 0; j < i; j++ {
			if roots.GlobalRefs[j] == slot {
				return nil, fmt.Errorf("M9 duplicate global root slot %d", slot)
			}
		}
		binary.LittleEndian.PutUint32(data[base+48+i*4:], slot)
	}
	return data, nil
}

func m9LdrW(rd, rn, offset uint32) uint32 {
	return 0xB9400000 | (offset/4)<<10 | rn<<5 | rd
}
func m9StrW(rd, rn, offset uint32) uint32 {
	return 0xB9000000 | (offset/4)<<10 | rn<<5 | rd
}

// M9MarkRuntimeWords 生成可搬移的原生 mark 函数。
//
// x0 Heap*；x1 类型描述符；x2 类型数量；x3 frameBase；x4 RootMap*；x5 globalsBase。
// 返回 x0=可达对象数量，若遇到损坏的对象头或找不到类型布局则返回 UINT64_MAX。
// 会清除上次标记后重新扫描，写入对象头 flags：0=未发现，1=灰色，3=已扫描。
// M9 采用无额外堆分配的固定点遍历，空间 O(1)，时间最坏 O(n²)，仅用于原型。
// 不修改任何 x19..x29，不改变对象载荷，不调用 Go Runtime。
func M9MarkRuntimeWords() ([]uint32, error) {
	b := newM8Assembler()
	appendM9Mark(b)
	return b.assemble()
}

const m9MarkEntry = "gc.mark"
const m9TryMark = "gc.trymark"

func appendM9Mark(b *m8Assembler) {
	b.label(m9MarkEntry)
	// 112 字节栈帧：保存全部使用的 callee-saved 寄存器和返回地址。
	b.emit(subImmX(31, 31, 112))
	for i := uint32(19); i <= 28; i++ {
		b.emit(strX(i, 31, i-19))
	}
	b.emit(strX(30, 31, 10))
	b.emit(movX(19, 0), movX(20, 1), movX(21, 2), movX(22, 3), movX(23, 4), movX(24, 5))
	b.emit(movzX(28, 0))
	b.cbz(19, "gc.fail")
	b.emit(ldrX(9, 19, 0))
	b.emit(m8MovBits(10, m8HeapMagic)...)
	b.emit(m8Cmp(9, 10))
	b.bc(1, "gc.fail")
	b.emit(ldrX(26, 19, 6), ldrX(27, 19, 1)) // origin, cursor
	b.emit(m8Cmp(26, 27))
	b.bc(8, "gc.fail")                   // cursor 不应在 origin 之前；相等表示空堆，但 origin 自身有效
	b.emit(ldrX(9, 19, 2), m8Cmp(27, 9)) // cursor <= end
	b.bc(8, "gc.fail")

	// 每一次 mark 都重新清空 M9 flags，保证 Root 变化后无旧标记残留。
	b.emit(movX(9, 26))
	b.label("gc.clear.loop")
	b.emit(m8Cmp(9, 27))
	b.bc(2, "gc.frame.begin")
	b.emit(movzW(10, 0), m9StrW(10, 9, 4), ldrX(10, 9, 1))
	b.emit(addImmX(10, 10, 31), m8Movn(11, 15), m8And(10, 10, 11))
	b.emit(addX(9, 9, 10), m8Cmp(9, 27))
	b.bc(8, "gc.fail")
	b.b("gc.clear.loop")

	// FrameRefs 仅包含指针槽位，普通 int64/float64 不会作为 GC Root。
	b.label("gc.frame.begin")
	b.emit(movzX(16, 0)) // 数组字节偏移，步长 4
	b.label("gc.frame.loop")
	b.emit(m9LdrW(9, 23, 4), addX(9, 9, 9), addX(9, 9, 9), m8Cmp(16, 9))
	b.bc(2, "gc.global.begin")
	b.emit(addImmX(9, 23, 16), addX(9, 9, 16), m9LdrW(10, 9, 0))
	b.emit(addX(11, 22, 10), ldrX(0, 11, 0))
	b.bl(m9TryMark)
	b.emit(addImmX(16, 16, 4))
	b.b("gc.frame.loop")

	// GlobalRefs 也必须由静态元数据指定，不能把每个全局整数都当指针。
	b.label("gc.global.begin")
	b.emit(movzX(16, 0))
	b.label("gc.global.loop")
	b.emit(m9LdrW(9, 23, 12), addX(9, 9, 9), addX(9, 9, 9), m8Cmp(16, 9))
	b.bc(2, "gc.scan.begin")
	b.emit(addImmX(9, 23, 48), addX(9, 9, 16), m9LdrW(10, 9, 0))
	b.emit(addX(10, 10, 10), addX(10, 10, 10), addX(10, 10, 10))
	b.emit(addX(11, 24, 10), ldrX(0, 11, 0))
	b.bl(m9TryMark)
	b.emit(addImmX(16, 16, 4))
	b.b("gc.global.loop")

	// 固定点遍历：每次找到一个灰色对象，通过类型表只扫描指定引用字段。
	b.label("gc.scan.begin")
	b.emit(movX(16, 26))
	b.label("gc.scan.loop")
	b.emit(m8Cmp(16, 27))
	b.bc(2, "gc.success")
	b.emit(m9LdrW(9, 16, 4), cmpImmX(9, 1))
	b.bc(1, "gc.scan.next")
	b.emit(movzW(9, 3), m9StrW(9, 16, 4))
	// 线性查找类型 ID；M9 类型上限为 8，后续由 GC Runtime 优化为直接索引。
	b.emit(movX(17, 20), movzX(9, 0))
	b.label("gc.type.loop")
	b.emit(m8Cmp(9, 21))
	b.bc(2, "gc.fail")
	b.emit(m9LdrW(10, 16, 0), m9LdrW(11, 17, 0), m8Cmp(10, 11))
	b.bc(0, "gc.type.found")
	b.emit(addImmX(17, 17, M9TypeEntryBytes), addImmX(9, 9, 1))
	b.b("gc.type.loop")
	b.label("gc.type.found")
	b.emit(ldrX(10, 16, 1), m9LdrW(11, 17, 4), m8Cmp(10, 11))
	b.bc(1, "gc.fail") // 对象载荷长度必须与 Type Layout 一致

	// 每个类型暂时最多 4 个引用偏移，循环展开以便不保存临时索引。
	for idx := 0; idx < M9MaxRefFields; idx++ {
		next := fmt.Sprintf("gc.ref.%d.end", idx)
		b.emit(m9LdrW(9, 17, 8), cmpImmX(9, uint32(idx+1)))
		b.bc(3, next) // count < (idx+1)
		b.emit(m9LdrW(10, 17, uint32(16+idx*4)), addImmX(11, 16, M8ObjectHeaderSize))
		b.emit(addX(11, 11, 10), ldrX(0, 11, 0))
		b.bl(m9TryMark) // 保留 x16 对象头、x17 当前 Type Layout
		b.label(next)
	}
	// 可能新标记的对象位于当前对象之前，因此每次从堆起点继续。
	b.b("gc.scan.begin")

	b.label("gc.scan.next")
	b.emit(ldrX(9, 16, 1), addImmX(9, 9, 31), m8Movn(10, 15), m8And(9, 9, 10))
	b.emit(addX(16, 16, 9), m8Cmp(16, 27))
	b.bc(8, "gc.fail")
	b.b("gc.scan.loop")

	b.label("gc.success")
	b.emit(movX(0, 28))
	b.b("gc.restore")
	b.label("gc.fail")
	b.emit(m8Movn(0, 0)) // UINT64_MAX 表示失败，不是有效的 count
	b.label("gc.restore")
	b.emit(ldrX(30, 31, 10))
	for i := uint32(19); i <= 28; i++ {
		b.emit(ldrX(i, 31, i-19))
	}
	b.emit(addImmX(31, 31, 112), m8Ret())

	// helper 接收 x0=候选载荷指针，只与合法的对象起始地址比较后才修改 flags。
	// 内部临时寄存器限定为 x9..x15；x16/x17 必须保存主标记循环的状态。
	b.label(m9TryMark)
	b.cbz(0, "gc.try.end")
	b.emit(movX(9, 26))
	b.label("gc.try.loop")
	b.emit(m8Cmp(9, 27))
	b.bc(2, "gc.try.end")
	b.emit(addImmX(10, 9, M8ObjectHeaderSize), m8Cmp(10, 0))
	b.bc(0, "gc.try.hit")
	b.emit(ldrX(11, 9, 1), addImmX(11, 11, 31), m8Movn(12, 15), m8And(11, 11, 12), addX(9, 9, 11))
	b.b("gc.try.loop")
	b.label("gc.try.hit")
	b.emit(m9LdrW(10, 9, 4))
	b.cbnz(10, "gc.try.end")
	b.emit(movzW(10, M9MarkBit), m9StrW(10, 9, 4), addImmX(28, 28, 1))
	b.label("gc.try.end")
	b.emit(m8Ret())
}
