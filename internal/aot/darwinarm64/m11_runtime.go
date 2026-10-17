package darwinarm64

import "fmt"

// M11a 仅用于显式启用的原生 Runtime 集成测试。
// 真实 Mio 字节码尚无堆对象指令，不能把这里的固定自检称为自动 GC。
// x23 在主程序中保存 Heap*；GC 标记时只使用明确登记的 Root 槽位。
const M11HeapCapacity = 4096

type m11CallKind uint8

const (
	m11Init m11CallKind = iota
	m11Alloc
	m11Collect
	m11Destroy
)

type m11CallSite struct {
	at   int
	kind m11CallKind
}

type m11MetadataSite struct {
	at int
}

type m11FailureSite struct {
	at      int
	zeroReg int // -1 表示 B.NE；非负表示 CBZ Xn
}

type m11LinkState struct {
	calls        []m11CallSite
	failed       []m11FailureSite
	metadataADRP []m11MetadataSite
}

func (l *m11LinkState) call(code *[]uint32, kind m11CallKind) {
	l.calls = append(l.calls, m11CallSite{at: len(*code), kind: kind})
	*code = append(*code, 0)
}
func (l *m11LinkState) failIfZero(code *[]uint32, reg uint32) {
	l.failed = append(l.failed, m11FailureSite{at: len(*code), zeroReg: int(reg)})
	*code = append(*code, 0) // CBZ -> 公共失败出口
}
func (l *m11LinkState) failIfNotEqual(code *[]uint32) {
	l.failed = append(l.failed, m11FailureSite{at: len(*code), zeroReg: -1})
	*code = append(*code, 0) // B.NE -> 公共失败出口
}

// EnableM11Runtime 必须在主程序首条 Mochi 指令生成前调用。
// 初始化并不触发 GC：堆内的对象还没有任何真实的 Mio 用户引用。
func (e *StackEmitter) EnableM11Runtime() error {
	if e.functionMode || e.depth != 0 || len(e.words) < 4 || e.m11Enabled {
		return fmt.Errorf("AOT M11a runtime must be enabled before main instructions")
	}
	e.m11Enabled = true
	e.words = append(e.words, movzX(23, 0), movzX(0, M11HeapCapacity))
	e.m11InitCall = len(e.words)
	e.words = append(e.words, 0) // BL heap.init
	e.m11InitFail = len(e.words)
	e.words = append(e.words, 0) // CBZ x0 -> 失败出口
	e.words = append(e.words, movX(23, 0))
	return nil
}

// appendM11Lifecycle 在 Mio 程序的普通输出结束后运行，不影响原有 stdout。
// 独立的 16 字节栈空间保存一个真实 Root 和一个故意不参与 Root Map 的旧地址。
// 整个过程只在单线程、安全的程序收尾阶段执行。
func appendM11Lifecycle(code []uint32, l *m11LinkState) []uint32 {
	code = append(code, subImmX(31, 31, 16), strX(31, 31, 0), strX(31, 31, 1))
	// 第一对象保留在 Root 中；第二对象只保存于未登记的普通整数槽位。
	for i := 0; i < 2; i++ {
		code = append(code, movX(0, 23), movzX(1, 1), movzX(2, 8))
		l.call(&code, m11Alloc)
		l.failIfZero(&code, 0)
		code = append(code, strX(0, 31, uint32(i)))
	}
	// 第一次 GC：必须保留 Root 中的对象，只回收未登记的第二对象。
	code = appendM11CollectCall(code, l)
	code = append(code, cmpImmX(0, 1))
	l.failIfNotEqual(&code)
	// gc.alloc 必须复用刚刚回收的第二块，而不是继续移动 Bump Cursor。
	code = append(code, movX(0, 23), movzX(1, 1), movzX(2, 8))
	l.call(&code, m11Alloc)
	l.failIfZero(&code, 0)
	code = append(code, ldrX(9, 31, 1), m8Cmp(0, 9))
	l.failIfNotEqual(&code)
	// 清掉唯一 Root，下一轮应回收两个对象（原有 Root 与刚复用的对象）。
	code = append(code, strX(31, 31, 0))
	code = appendM11CollectCall(code, l)
	code = append(code, cmpImmX(0, 2))
	l.failIfNotEqual(&code)
	code = append(code, addImmX(31, 31, 16), movX(0, 23))
	l.call(&code, m11Destroy)
	code = append(code, cmpImmX(0, 1))
	l.failIfNotEqual(&code)
	return code
}

// gc.collect(Heap*, types, 1, frameBase, RootMap*, 0)。
// ADRP/ADD 的目标要等代码与只读区全部排布完成后才能确定。
func appendM11CollectCall(code []uint32, l *m11LinkState) []uint32 {
	code = append(code, movX(0, 23))
	l.metadataADRP = append(l.metadataADRP, m11MetadataSite{at: len(code)})
	code = append(code, 0, 0) // ADRP x1 + ADD x1, x1, #lo12
	code = append(code,
		movzX(2, 1),
		addImmX(3, 31, 0),
		addImmX(4, 1, M9TypeEntryBytes),
		movzX(5, 0))
	l.call(&code, m11Collect)
	return code
}

// appendM11Failure 统一处理初始化及 GC 失败，恢复主程序 SP 并 exit(1)。
// 在 Heap 初始化之前 x23 固定为 0，因此不会误调用 heap.destroy。
func appendM11Failure(code []uint32, l *m11LinkState, initFail int) ([]uint32, error) {
	fail := len(code)
	jumpPastDestroy := len(code)
	code = append(code, 0) // CBZ x23
	code = append(code, movX(0, 23))
	l.call(&code, m11Destroy)
	pastDestroy := len(code)
	code = append(code, addImmX(31, 29, 0), 0xA8C17BFD, movzX(0, 1), movzX(16, 1), svc(0x80))
	var err error
	if code[jumpPastDestroy], err = checkedCBZ(23, jumpPastDestroy, pastDestroy); err != nil {
		return nil, err
	}
	if code[initFail], err = checkedCBZ(0, initFail, fail); err != nil {
		return nil, err
	}
	for _, site := range l.failed {
		if site.at < 0 || site.at >= len(code) || code[site.at] != 0 {
			return nil, fmt.Errorf("AOT M11a invalid failure site")
		}
		if site.zeroReg >= 0 {
			code[site.at], err = checkedCBZ(uint32(site.zeroReg), site.at, fail)
		} else {
			if distance := fail - site.at; distance < -(1<<18) || distance >= 1<<18 {
				return nil, fmt.Errorf("AOT M11a failure branch out of range")
			}
			code[site.at] = branchCond(site.at, fail, 1) // B.NE
		}
		if err != nil {
			return nil, err
		}
	}
	return code, nil
}

func m11PatchMetaAddress(code []uint32, sites []m11MetadataSite, constFileOffset uint64) error {
	constVA := textVMAddr + constFileOffset
	for _, site := range sites {
		if site.at < 0 || site.at+1 >= len(code) {
			return fmt.Errorf("AOT M11a metadata relocation out of range")
		}
		pc := textVMAddr + entryOffset + uint64(site.at)*4
		ins, err := adrpX(1, pc, constVA)
		if err != nil {
			return err
		}
		code[site.at] = ins
		code[site.at+1] = addImmX(1, 1, uint32(constVA&0xfff))
	}
	return nil
}
