package darwinarm64

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestM19cNativeLOS(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	for _, size := range []uint64{16 << 10, (128 << 10) - 64, (128 << 10) - 48, 128 << 10, 1 << 20, 2 << 20} {
		for _, refs := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/refs=%t", size, refs), func(t *testing.T) {
				b := newM8Assembler()
				check := uint16(10)
				eq := func(r uint32, n uint64) {
					check++
					b.emit(movzX(25, check))
					t.Logf("check %d r%d = %d", check, r, n)
					b.emit(m8MovBits(11, n)...)
					b.emit(m8Cmp(r, 11))
					b.bc(1, "fail")
				}
				metric := func(slot uint32, n uint64) { b.emit(ldrX(9, 19, 2), ldrX(10, 9, slot)); eq(10, n) }
				collect := func(major bool) {
					b.emit(movX(0, 19), movX(1, 24))
					if major {
						b.bl(m19bMajorEntry)
					} else {
						b.bl(m19bMinorEntry)
					}
					b.emit(m8Movn(9, 0), m8Cmp(0, 9))
					b.bc(0, "fail")
				}
				b.emit(subImmX(31, 31, 64), strX(31, 31, 0), movzX(9, 2), strX(9, 31, 1), strX(31, 31, 2), strX(31, 31, 3), addImmX(24, 31, 0))
				b.emit(m8MovBits(0, 8<<20)...)
				b.bl(m8InitLabel)
				b.cbz(0, "fail")
				b.emit(movX(19, 0))
				b.emit(movX(0, 19), movzX(1, M12StorageTypeID))
				b.emit(m8MovBits(2, size+32)...)
				b.bl(m11AutoAllocEntry)
				b.cbz(0, "fail")
				b.emit(movX(20, 0), strX(20, 31, 2))
				kind := uint64(m12StorageKind | 8<<8)
				if refs {
					kind |= 1 << 16
				}
				b.emit(m8MovBits(9, kind)...)
				b.emit(strX(9, 20, 0))
				b.emit(m8MovBits(9, size/8)...)
				b.emit(strX(9, 20, 1), movzX(9, 1), strX(9, 20, 2), strX(31, 20, 3))
				if refs {
					b.emit(strX(9, 20, 3))
				}
				// Write and verify the last word across page boundaries throughout collections.
				b.emit(m8MovBits(9, size+24)...)
				b.emit(addX(22, 20, 9), movzX(9, 713), strX(9, 22, 0))
				if refs {
					b.emit(strX(31, 22, 0))
				}
				los := size+48 >= 128<<10
				if los {
					metric(m19cLOSCount, 1)
				} else {
					metric(m19cLOSCount, 0)
				}
				for i := 0; i < 4; i++ {
					collect(false)
				}
				b.emit(subImmX(9, 20, 16), m9LdrW(10, 9, 4), movzX(11, m19bOld), m8And(10, 10, 11))
				eq(10, m19bOld)
				// Old LOS -> Young ordinary; scalar storage must NOT retain address-shaped data.
				b.emit(movX(0, 19), movzX(1, M15CellTypeID), movzX(2, 16))
				b.bl(m11AutoAllocEntry)
				b.cbz(0, "fail")
				b.emit(movX(21, 0), movzX(9, m15CellKind), strX(9, 21, 0), movzX(9, 927), strX(9, 21, 1), strX(21, 20, 4))
				b.emit(m19bRememberWords(20, 19)...)
				collect(false)
				b.emit(ldrX(10, 20, 4), m8Cmp(10, 21))
				b.bc(1, "fail")
				if refs {
					b.emit(ldrX(10, 21, 1))
					eq(10, 927)
				} else {
					metric(m19bYoungBytes, 0)
				}
				collect(true)
				b.emit(ldrX(10, 31, 2), m8Cmp(10, 20))
				b.bc(1, "fail")
				b.emit(ldrX(10, 22, 0))
				if refs {
					eq(10, 0)
				} else {
					eq(10, 713)
				}
				b.emit(strX(31, 31, 2))
				collect(true)
				metric(m19cLOSCount, 0)
				metric(m8TelOccupiedBytes, 0)
				// New unrooted LOS dies at the first independent Minor.
				b.emit(movX(0, 19), movzX(1, M12StorageTypeID))
				b.emit(m8MovBits(2, size+32)...)
				b.bl(m11AutoAllocEntry)
				b.cbz(0, "fail")
				collect(false)
				metric(m19cLOSCount, 0)
				metric(m8TelOccupiedBytes, 0)
				b.emit(movX(0, 19))
				b.bl(m8DestroyLabel)
				eq(0, 1)
				m8WriteLiteral(b, "M19c LOS PASS\n")
				b.emit(movzX(0, 0), movzX(16, 1), svc(128))
				b.label("fail")
				b.emit(movX(0, 19), movX(1, 24))
				b.bl(m8TelemetryEntry)
				b.emit(movX(0, 25), movzX(16, 1), svc(128))
				appendM8TelemetryRuntime(b)
				appendM11ObjectRuntime(b, false)
				appendM10Sweep(b)
				appendM10Alloc(b)
				appendM8Runtime(b)
				words, err := b.assemble()
				if err != nil {
					t.Fatal(err)
				}
				img, err := ExecutableWords(words)
				if err != nil {
					t.Fatal(err)
				}
				p := filepath.Join(t.TempDir(), "los")
				if err = os.WriteFile(p, img, 0700); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command("codesign", "-s", "-", p).CombinedOutput(); err != nil {
					t.Fatalf("sign %v %s", err, out)
				}
				if out, err := exec.Command(p).CombinedOutput(); err != nil || string(out) != "M19c LOS PASS\n" {
					t.Fatalf("native %v %s", err, out)
				}
			})
		}
	}
}
