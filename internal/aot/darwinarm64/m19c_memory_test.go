package darwinarm64

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestM19cNativeMemoryPolicy(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Darwin ARM64")
	}
	b := newM8Assembler()
	check := uint16(1)
	eq := func(r uint32, n uint64) {
		check++
		t.Logf("check %d r%d = %d", check, r, n)
		b.emit(movzX(28, check))
		b.emit(m8MovBits(11, n)...)
		b.emit(m8Cmp(r, 11))
		b.bc(1, "fail")
	}
	set := func(slot uint32, n uint64) {
		b.emit(ldrX(9, 19, 2))
		b.emit(m8MovBits(10, n)...)
		b.emit(strX(10, 9, slot))
	}
	metric := func(slot uint32, n uint64) { b.emit(ldrX(9, 19, 2), ldrX(10, 9, slot)); eq(10, n) }
	b.emit(subImmX(31, 31, 128), strX(31, 31, 0), movzX(9, 1), strX(9, 31, 1), strX(31, 31, 2), addImmX(24, 31, 0))
	b.emit(m8MovBits(0, (8<<20)|M19cAdaptiveHeapFlag)...)
	b.bl("m19c.init")
	b.cbz(0, "fail")
	b.emit(movX(19, 0))
	metric(m19cAuto, 1)
	metric(m19cGoal, 1<<20)
	metric(m19cObjectLimit, 8<<20)
	metric(m19cCommitFrontier, 1<<20)
	// An actual object and payload survive explicit page-prefix growth.
	b.emit(movX(0, 19), movzX(1, M15CellTypeID), movzX(2, 16))
	b.bl(m11AutoAllocEntry)
	b.cbz(0, "fail")
	b.emit(movX(20, 0), movzX(9, m15CellKind), strX(9, 20, 0), movzX(9, 728), strX(9, 20, 1), strX(20, 31, 2))
	b.emit(ldrX(0, 19, 2), ldrX(1, 19, 6), addImmX(2, 0, M8TelemetrySize), ldrX(3, 19, 3))
	b.emit(m8MovBits(4, 4<<20)...)
	b.emit(movzX(5, 0))
	b.bl("memory.commit")
	eq(0, 1)
	metric(m19cCommitFrontier, 4<<20)
	b.emit(ldrX(10, 20, 1))
	eq(10, 728)
	b.emit(movX(0, 19), movX(1, 24))
	b.bl(m19bMinorEntry)
	b.emit(ldrX(10, 20, 1))
	eq(10, 728)
	for _, strategy := range []uint64{uint64(GCStrategyHigh), uint64(GCStrategyBalanced), uint64(GCStrategyLow)} {
		set(m19cStrategy, strategy)
		b.emit(movX(0, 19), movX(1, 24))
		b.bl(m19bMinorEntry)
		b.emit(ldrX(10, 20, 1))
		eq(10, 728)
	}
	// Use controlled policy inputs to test every reason independent of host load.
	set(m19cObjectLimit, 1000000)
	set(m19cLimit, 3065536)
	set(m19cCommitted, 0)
	set(m19cSystemAvailable, 2)
	set(m19cSystem, 1)
	set(m19cRate, 0)
	set(m19cLevel, 0)
	sample := func(used uint64, fresh bool, before, reclaimed uint64) {
		b.emit(ldrX(0, 19, 2))
		if fresh {
			b.emit(strX(31, 0, m19cLastSample))
		} else {
			b.emit(mrsCNTVCT(9), strX(9, 0, m19cLastSample))
		}
		b.emit(m8MovBits(1, used)...)
		b.emit(movzX(2, 0))
		b.emit(m8MovBits(3, 1000000)...)
		b.emit(mrsCNTFRQ(4))
		b.emit(m8MovBits(5, before)...)
		b.emit(m8MovBits(6, reclaimed)...)
		b.bl("memory.update")
	}
	for i, u := range []uint64{0, 700000, 850000, 950000} {
		sample(u, false, 0, 0)
		metric(m19cLevel, uint64(i))
	}
	sample(0, false, 0, 0)
	metric(m19cLevel, 3)
	for i := 0; i < 3; i++ {
		sample(0, true, 0, 0)
		if i < 2 {
			metric(m19cLevel, 3)
		} else {
			metric(m19cLevel, 0)
		}
	}
	set(m19cCommitted, 2800000)
	sample(0, false, 0, 0)
	metric(m19cLevel, 2)
	metric(m19cReason, 2)
	set(m19cCommitted, 0)
	set(m19cLevel, 0)
	set(m19cRate, 10000000)
	sample(0, false, 0, 0)
	metric(m19cLevel, 3)
	metric(m19cReason, 4)
	set(m19cRate, 0)
	set(m19cLevel, 0)
	sample(600000, false, 900000, 1)
	metric(m19cLevel, 1)
	metric(m19cReason, 8)
	set(m19cLevel, 0)
	set(m19cSystem, 4)
	sample(0, false, 0, 0)
	metric(m19cLevel, 3)
	metric(m19cReason, 16)
	set(m19cGCActive, 1)
	b.emit(ldrX(0, 19, 2), addImmX(1, 31, 64))
	b.bl("memory.snapshot")
	b.emit(ldrX(10, 31, 8))
	eq(10, 3)
	b.emit(ldrX(10, 31, 9))
	eq(10, 16)
	b.emit(ldrX(10, 31, 14))
	eq(10, 1)
	b.emit(ldrX(10, 31, 15), movzX(11, 1), m8And(10, 10, 11))
	eq(10, 0)
	// Soft goal grows and shrinks independently of stable mappings.
	set(m19cRate, 0)
	b.emit(ldrX(0, 19, 2))
	b.emit(m8MovBits(1, 400000)...)
	b.emit(m8MovBits(2, 500000)...)
	b.bl("memory.goal")
	metric(m19cGoal, 1000000)
	set(m19cObjectLimit, 8<<20)
	b.emit(ldrX(0, 19, 2))
	b.emit(m8MovBits(1, 2<<20)...)
	b.emit(m8MovBits(2, 1<<20)...)
	b.bl("memory.goal")
	metric(m19cGoal, 5<<20)
	b.emit(ldrX(0, 19, 2), movzX(1, 0), movzX(2, 0))
	b.bl("memory.goal")
	metric(m19cGoal, 1<<20)
	// A request whose header pushes it over the object cap retries Major and
	// reports allocation failure while the existing rooted payload stays intact.
	set(m19cLimit, (24<<20)+65536)
	set(m19cSystem, 1)
	set(m19cRate, 0)
	b.emit(movX(0, 19), movzX(1, M12StorageTypeID))
	b.emit(m8MovBits(2, 8<<20)...)
	b.bl(m11AutoAllocEntry)
	eq(0, 0)
	b.emit(ldrX(10, 20, 1))
	eq(10, 728)
	b.emit(ldrX(9, 19, 2), ldrX(10, 9, m19bMajorCount))
	b.cbz(10, "fail")
	b.emit(movX(0, 19))
	b.bl("m19c.destroy")
	eq(0, 1)
	m8WriteLiteral(b, "M19c memory PASS\n")
	b.emit(movzX(0, 0), movzX(16, 1), svc(128))
	b.label("fail")
	b.emit(movX(0, 28), movzX(16, 1), svc(128))
	appendM11ObjectRuntime(b, false)
	appendM10Sweep(b)
	appendM10Alloc(b)
	appendM8Runtime(b)
	w, e := b.assemble()
	if e != nil {
		t.Fatal(e)
	}
	img, e := ExecutableWords(w)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "memory")
	if e = os.WriteFile(p, img, 0700); e != nil {
		t.Fatal(e)
	}
	if out, e := exec.Command("codesign", "-s", "-", p).CombinedOutput(); e != nil {
		t.Fatalf("sign %v %s", e, out)
	}
	if out, e := exec.Command(p).CombinedOutput(); e != nil || string(out) != "M19c memory PASS\n" {
		t.Fatalf("native policy %v %s", e, out)
	}
}
