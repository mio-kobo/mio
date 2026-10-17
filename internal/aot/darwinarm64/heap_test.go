package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"
)

// TestM8NativeProbeImages 校验三个探针都有完整的 Mach-O 结构和原生 syscall 代码。
// 此测试只检查编译产物；实际 mmap 行为仍需要 macOS ARM64 实机验证。
func TestM8NativeProbeImages(t *testing.T) {
	for _, mode := range []string{"smoke", "limits", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			image, err := M8AllocatorProbe(mode)
			if err != nil {
				t.Fatal(err)
			}
			if len(image) != int(pageSize) {
				t.Fatalf("image has %d bytes, expected %d", len(image), pageSize)
			}
			f, err := macho.NewFile(bytes.NewReader(image))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if f.Cpu != macho.CpuArm64 || f.Type != macho.TypeExec {
				t.Fatalf("invalid target: cpu=%s type=%s", f.Cpu, f.Type)
			}
			code := f.Section("__text")
			if code == nil || code.Size <= 200 || code.Size >= pageSize-entryOffset {
				t.Fatalf("invalid native code section: %+v", code)
			}
			for _, instruction := range []uint32{
				movzX(16, m8DarwinSYSMmap),
				movzX(16, m8DarwinSYSMunmap),
				svc(0x80),
				m8Movn(4, 0), // 匿名映射 fd=-1
			} {
				var encoded [4]byte
				binary.LittleEndian.PutUint32(encoded[:], instruction)
				data, err := code.Data()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(data, encoded[:]) {
					t.Fatalf("missing AArch64 instruction %#08x", instruction)
				}
			}
			// 自检信息在验证成功后才通过 SYS_write 写出，不预先写入数据段。
			if f.Section("__data") != nil {
				t.Fatal("heap must use native mmap, not a fixed Mach-O __DATA region")
			}
		})
	}
}

func TestM8AllocatorABIAndObjectFootprints(t *testing.T) {
	if M8HeapHeaderSize != 64 || M8ObjectHeaderSize != 16 || M8ObjectAlignment != 16 {
		t.Fatal("M8 heap/object ABI changed unexpectedly")
	}
	if m8HeapMagic != 0x38504145484F494D {
		t.Fatalf("invalid heap magic %#x", m8HeapMagic)
	}
	var magic [8]byte
	binary.LittleEndian.PutUint64(magic[:], m8HeapMagic)
	if string(magic[:]) != "MIOHEAP8" {
		t.Fatalf("heap magic byte order mismatch: %q", magic[:])
	}

	// 静态基准检查与原生探针相同的布局边界。
	cases := []struct{ payload, total int }{
		{0, 16}, {1, 32}, {15, 32}, {16, 32}, {17, 48}, {127, 144}, {32, 48},
	}
	for _, tt := range cases {
		got := (tt.payload + M8ObjectHeaderSize + M8ObjectAlignment - 1) &^ (M8ObjectAlignment - 1)
		if got != tt.total || got%M8ObjectAlignment != 0 {
			t.Fatalf("payload=%d footprint=%d expected=%d", tt.payload, got, tt.total)
		}
	}
}

func TestM8AssemblerRelocationErrors(t *testing.T) {
	b := newM8Assembler()
	b.bl("missing")
	if _, err := b.assemble(); err == nil || !strings.Contains(err.Error(), "missing ARM64 label") {
		t.Fatalf("missing fixup target should fail: %v", err)
	}

	b = newM8Assembler()
	b.label("dupe")
	b.label("dupe")
	if _, err := b.assemble(); err == nil || !strings.Contains(err.Error(), "duplicate M8") {
		t.Fatalf("duplicate native label should fail: %v", err)
	}

	b = newM8Assembler()
	b.bc(0, "far")
	b.emit(make([]uint32, 1<<18)...)
	b.label("far")
	if _, err := b.assemble(); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range B.cond should fail: %v", err)
	}
}

func TestM8RuntimeWordsReusable(t *testing.T) {
	words, entries, err := M8AllocatorRuntimeWords()
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < 75 || entries.Init != 0 || entries.Alloc <= 0 || entries.Destroy <= entries.Alloc || entries.Destroy >= len(words) {
		t.Fatalf("invalid relocatable runtime entries: %+v; words=%d", entries, len(words))
	}
	// 机器码整体追加到主程序后，内部 PC 相对位移不会发生变化。
	for _, entry := range []int{entries.Init, entries.Alloc, entries.Destroy} {
		if _, err := branch26(0x94000000, 100, 100+entry); err != nil {
			t.Fatalf("entry %d cannot be called by native main: %v", entry, err)
		}
	}
}

func TestM8UnknownProbeRejected(t *testing.T) {
	if _, err := M8AllocatorProbe("other"); err == nil || !strings.Contains(err.Error(), "unknown M8 probe") {
		t.Fatalf("invalid probe must be rejected: %v", err)
	}
}
