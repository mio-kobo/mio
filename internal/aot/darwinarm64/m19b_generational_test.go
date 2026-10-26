package darwinarm64

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Execute the real emitted collector, inspect headers/counters and dereference
// retained edges. No host-side GC model stands in for native GC semantics.
func TestM19bNativeGenerationalRuntime(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	for _, mode := range []string{"ages", "remembered", "promotion-edge", "major", "precision", "adaptive", "minor-scan", "cross-graph", "alternating", "precision-storage"} {
		t.Run(mode, func(t *testing.T) {
			b := newM8Assembler()
			fail := "probe.fail"
			eq := func(a uint32, n uint64) { b.emit(m8MovBits(11, n)...); b.emit(m8Cmp(a, 11)); b.bc(1, fail) }
			metric := func(r, slot uint32) { b.emit(ldrX(9, 19, 2), ldrX(r, 9, slot)) }
			flags := func(p, r uint32) { b.emit(subImmX(9, p, 16), m9LdrW(r, 9, 4)) }
			alloc := func(dst uint32, ref bool) {
				b.emit(movX(0, 19), movzX(1, M15CellTypeID), movzX(2, 16))
				b.bl(m11AutoAllocEntry)
				b.cbz(0, fail)
				b.emit(movX(dst, 0), movzX(9, m15CellKind))
				if ref {
					b.emit(movzX(9, m15CellKind|256))
				}
				b.emit(strX(9, dst, 0), strX(31, dst, 1))
			}
			collect := func(major bool) {
				b.emit(movX(0, 19), movX(1, 24))
				if major {
					b.bl(m19bMajorEntry)
				} else {
					b.bl(m19bMinorEntry)
				}
				b.emit(m8Movn(9, 0), m8Cmp(0, 9))
				b.bc(0, fail)
			}
			root := func(slot, p uint32) { b.emit(strX(p, 31, slot+2)) }
			barrier := func(p uint32) { b.emit(m19bRememberWords(p, 19)...) }
			b.emit(subImmX(31, 31, 64), strX(31, 31, 0), movzX(9, 4), strX(9, 31, 1))
			for i := uint32(2); i < 6; i++ {
				b.emit(strX(31, 31, i))
			}
			b.emit(addImmX(24, 31, 0), movzX(0, 4096))
			b.bl(m8InitLabel)
			b.cbz(0, fail)
			b.emit(movX(19, 0))
			switch mode {
			case "ages":
				alloc(20, false)
				root(0, 20)
				flags(20, 10)
				eq(10, 0)
				for i := uint64(1); i <= 3; i++ {
					collect(false)
					flags(20, 10)
					if i < 3 {
						eq(10, i*16)
					} else {
						eq(10, 48|m19bOld|m19bRemembered)
					}
				}
				b.emit(ldrX(10, 31, 2), m8Cmp(10, 20))
				b.bc(1, fail)
				metric(10, m19bPromotedObjects)
				eq(10, 1)
				metric(10, m19bOldBytes)
				eq(10, 32)
			case "remembered", "minor-scan":
				alloc(20, true)
				root(0, 20)
				if mode == "minor-scan" {
					for i := 0; i < 63; i++ {
						alloc(21, true)
						b.emit(strX(20, 21, 1), movX(20, 21))
						root(0, 20)
					}
				}
				for i := 0; i < 3; i++ {
					collect(false)
				}
				collect(false) // prune pointer-free promoted owner
				metric(10, m19bRememberedCount)
				eq(10, 0)
				alloc(21, false)
				b.emit(movzX(9, 77), strX(9, 21, 1), strX(21, 20, 1))
				barrier(20)
				barrier(20)
				metric(10, m19bRememberedCount)
				eq(10, 1)
				// No root to child: the remembered edge is its only protection.
				collect(false)
				b.emit(ldrX(9, 20, 1), ldrX(10, 9, 1))
				eq(10, 77)
				b.emit(ldrX(9, 20, 1), m8Cmp(9, 21))
				b.bc(1, fail)
				if mode == "minor-scan" {
					metric(10, m19bScannedOld)
					eq(10, 65)
				} // 64 owners pruned once; only one scanned after mutation
				b.emit(strX(31, 20, 1))
				barrier(20)
				collect(false)
				metric(10, m19bRememberedCount)
				eq(10, 0)
				metric(10, m19bYoungBytes)
				eq(10, 0)
			case "promotion-edge":
				alloc(20, true)
				root(0, 20)
				collect(false)
				collect(false)
				alloc(21, false)
				b.emit(movzX(9, 91), strX(9, 21, 1), strX(21, 20, 1))
				collect(false) // parent promoted, child age1
				flags(20, 10)
				eq(10, 48|m19bOld|m19bRemembered)
				collect(false)
				b.emit(ldrX(9, 20, 1), ldrX(10, 9, 1))
				eq(10, 91)
			case "major":
				alloc(20, true)
				root(0, 20)
				for i := 0; i < 3; i++ {
					collect(false)
				}
				root(0, 31)
				collect(false)
				metric(10, m19bOldBytes)
				eq(10, 32)
				collect(true)
				metric(10, m19bOldBytes)
				eq(10, 0)
				b.emit(ldrX(10, 19, 5))
				eq(10, 0)
				// Allocator may reuse the same span with new young state and no links.
				alloc(21, false)
				flags(21, 10)
				eq(10, 0)
				root(0, 21)
				collect(false)
				flags(21, 10)
				eq(10, 16)
			case "precision":
				alloc(20, false)
				root(0, 20)
				for i := 0; i < 3; i++ {
					collect(false)
				}
				alloc(21, false)
				b.emit(strX(21, 20, 1))
				barrier(20)
				collect(false)
				metric(10, m19bYoungBytes)
				eq(10, 0)
				// A scalar equal to a payload address is not an edge.
				b.emit(ldrX(10, 20, 1), m8Cmp(10, 21))
				b.bc(1, fail)
			case "cross-graph":
				alloc(20, true)
				root(0, 20)
				alloc(21, true)
				root(1, 21)
				b.emit(strX(21, 20, 1))
				for i := 0; i < 3; i++ {
					collect(false)
				}
				// Root->old->old->young, plus shared old->young.
				alloc(22, true)
				b.emit(strX(22, 20, 1), strX(22, 21, 1))
				barrier(20)
				barrier(21)
				alloc(23, false)
				b.emit(movzX(9, 123), strX(9, 23, 1), strX(23, 22, 1))
				collect(false)
				b.emit(ldrX(9, 20, 1), ldrX(9, 9, 1), ldrX(10, 9, 1))
				eq(10, 123)
				// Root->young->old->young, with no direct old root.
				alloc(22, true)
				root(0, 22)
				root(1, 31)
				b.emit(strX(21, 22, 1))
				collect(false)
				b.emit(ldrX(9, 22, 1), ldrX(9, 9, 1), ldrX(9, 9, 1), ldrX(10, 9, 1))
				eq(10, 123)
			case "alternating":
				alloc(20, true)
				root(0, 20)
				for i := 0; i < 40; i++ {
					alloc(21, false)
					b.emit(movzX(9, uint16(i)), strX(9, 21, 1), strX(21, 20, 1))
					barrier(20)
					collect(false)
					if i%4 == 0 {
						collect(true)
					}
					b.emit(ldrX(9, 20, 1), ldrX(10, 9, 1))
					eq(10, uint64(i))
				}
				root(0, 31)
				collect(true)
				metric(10, m19bOldBytes)
				eq(10, 0)
				metric(10, m19bYoungBytes)
				eq(10, 0)
			case "precision-storage":
				b.emit(movX(0, 19), movzX(1, M12StorageTypeID), movzX(2, 40))
				b.bl(m11AutoAllocEntry)
				b.cbz(0, fail)
				b.emit(movX(20, 0), movzX(9, 3|8<<8), strX(9, 20, 0), movzX(9, 1), strX(9, 20, 1), strX(9, 20, 2), strX(31, 20, 3), strX(31, 20, 4))
				root(0, 20)
				for i := 0; i < 3; i++ {
					collect(false)
				}
				alloc(21, false)
				b.emit(strX(21, 20, 4))
				barrier(20)
				collect(false)
				metric(10, m19bYoungBytes)
				eq(10, 0)
				metric(10, m19bOldBytes)
				eq(10, 64)
				b.emit(ldrX(10, 20, 4), m8Cmp(10, 21))
				b.bc(1, fail)
			case "adaptive":
				// Three low-survival votes increase T, with deterministic upper bound.
				for i := 0; i < 18; i++ {
					alloc(20, false)
					collect(false)
				}
				metric(10, m19bThreshold)
				eq(10, 8)
				metric(10, m19bMinorCount)
				eq(10, 18)
				// High survivor/mature-age distribution votes down after hysteresis.
				alloc(20, false)
				root(0, 20)
				for i := 0; i < 4; i++ {
					collect(false)
				}
				metric(10, m19bThreshold)
				eq(10, 7)
			}
			b.emit(movX(0, 19))
			b.bl(m8DestroyLabel)
			eq(0, 1)
			m8WriteLiteral(b, "M19b PASS\n")
			b.emit(movzX(0, 0), movzX(16, 1), svc(0x80))
			b.label(fail)
			b.emit(movzX(0, 1), movzX(16, 1), svc(0x80))
			appendM11ObjectRuntime(b, false)
			appendM10Sweep(b)
			appendM10Alloc(b)
			appendM8Runtime(b)
			words, err := b.assemble()
			if err != nil {
				t.Fatal(err)
			}
			image, err := ExecutableWords(words)
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(t.TempDir(), "probe")
			if err = os.WriteFile(p, image, 0700); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("/usr/bin/codesign", "-s", "-", p).CombinedOutput(); err != nil {
				t.Fatalf("sign %v %s", err, out)
			}
			if out, err := exec.Command(p).CombinedOutput(); err != nil || string(out) != "M19b PASS\n" {
				t.Fatalf("native GC %s: %v %s", mode, err, out)
			}
		})
	}
}
