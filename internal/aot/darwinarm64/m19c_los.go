package darwinarm64

import "strconv"

func fmtIndex(n int) string { return strconv.Itoa(n) }

// LOS mappings have a 32-byte native prefix followed by the unchanged 16-byte
// object header. Prefix words: all-next, mapping length, young-next, remembered-next.
// Thus payload=base+48 and generational side(header)=header-16. Only exact payload
// starts are accepted by the classifier; no speculative loads outside the arena.
const (
	m19cLOSHead uint32 = 320 + iota
	m19cLOSBytes
	m19cLOSReclaimed
	m19cLOSCount
	m19cLOSThreshold
	m19cLOSAlloc = "m19c.los.alloc"
	m19cLOSPrune = "m19c.los.prune"
)

// Inline side lookup, preserving scratch's established telemetry-base result.
func m19cSide(b *m8Assembler, dst, header, heap, scratch uint32) {
	prefix := "m19c.side." + fmtIndex(len(b.words))
	b.emit(ldrX(dst, heap, 6), m8Cmp(header, dst))
	b.bc(3, prefix+".los")
	b.emit(ldrX(scratch, heap, 2), m8Cmp(header, scratch))
	b.bc(2, prefix+".los")
	b.emit(m10Sub(dst, header, dst), addX(dst, dst, scratch), addImmX(dst, dst, M8TelemetrySize))
	b.b(prefix + ".done")
	b.label(prefix + ".los")
	b.emit(subImmX(dst, header, 16), ldrX(scratch, heap, 2))
	b.label(prefix + ".done")
}

func appendM19cLOS(b *m8Assembler) {
	// Dynamic allocator wrapper. Legacy M8/M10 fixed-layout probes remain separate.
	b.label("m19c.alloc")
	b.emit(subImmX(31, 31, 48), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2), strX(2, 31, 3))
	b.bl("m19c.sample")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), ldrX(2, 31, 3), ldrX(30, 31, 0), addImmX(31, 31, 48))
	b.emit(ldrX(9, 0, 2), ldrX(10, 9, m8TelOccupiedBytes), addImmX(11, 2, 31), m8Movn(12, 15), m8And(11, 11, 12), addX(10, 10, 11), ldrX(12, 9, m19cGoal), m8Cmp(10, 12))
	b.bc(8, "m19c.alloc.fail")
	b.emit(ldrX(10, 9, m19cObjectLimit), m8Cmp(2, 10))
	b.bc(8, "m19c.alloc.fail")
	b.emit(ldrX(10, 9, m19cLOSThreshold))
	b.cbnz(10, "m19c.threshold.ready")
	b.emit(m8MovBits(10, 128<<10)...)
	b.label("m19c.threshold.ready")
	b.emit(addImmX(11, 2, 31), m8Movn(12, 15), m8And(11, 11, 12), m8Cmp(11, 10))
	b.bc(2, m19cLOSAlloc)
	// LOS and ordinary objects share the historical object-capacity limit.
	b.emit(ldrX(10, 9, m8TelOccupiedBytes), addX(10, 10, 11), ldrX(12, 9, m19cObjectLimit), m8Cmp(10, 12))
	b.bc(8, "m19c.alloc.fail")
	b.emit(subImmX(31, 31, 16), strX(30, 31, 0))
	b.bl("m19c.commit")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 16))
	b.cbz(9, "m19c.alloc.fail")
	b.b(m10AllocEntry)
	b.label("m19c.alloc.fail")
	b.emit(movzX(0, 0), m8Ret())
	b.label(m19cLOSAlloc)
	b.cbz(1, "m19c.alloc.fail")
	b.emit(m8MovBits(10, M8MaxTypeID)...)
	b.emit(m8Cmp(1, 10))
	b.bc(8, "m19c.alloc.fail")
	b.emit(ldrX(10, 0, 3), m8Cmp(2, 10))
	b.bc(8, "m19c.alloc.fail")
	b.emit(ldrX(10, 9, m8TelOccupiedBytes), addX(10, 10, 11), ldrX(12, 9, m19cObjectLimit), m8Cmp(10, 12))
	b.bc(8, "m19c.alloc.fail")
	b.emit(subImmX(31, 31, 48), strX(0, 31, 0), strX(1, 31, 1), strX(2, 31, 2), strX(11, 31, 3))
	// Page rounding is explicit (Darwin ARM64 16 KiB); all lengths bounded by 1 GiB.
	b.emit(addImmX(1, 11, 32))
	b.emit(m8MovBits(10, 16383)...)
	b.emit(addX(1, 1, 10), m8Movn(10, 16383), m8And(1, 1, 10), strX(1, 31, 4))
	b.emit(ldrX(9, 31, 0), ldrX(9, 9, 2), ldrX(10, 9, m19cCommitted), addX(10, 10, 1), ldrX(11, 9, m19cLimit), m8Cmp(10, 11))
	b.bc(8, "m19c.los.sysfail")
	b.emit(movzX(0, 0), movzX(2, 3), movzX(3, m8DarwinMapAnonPriv), m8Movn(4, 0), movzX(5, 0), movzX(16, m8DarwinSYSMmap), svc(0x80))
	b.bc(2, "m19c.los.sysfail")
	b.emit(ldrX(9, 31, 0), ldrX(9, 9, 2), ldrX(10, 9, m19cLOSHead), strX(10, 0, 0), strX(0, 9, m19cLOSHead), ldrX(10, 31, 4), strX(10, 0, 1), ldrX(11, 9, m19cLOSCommitted), addX(11, 11, 10), strX(11, 9, m19cLOSCommitted))
	b.emit(ldrX(10, 31, 1), strX(10, 0, 4), ldrX(10, 31, 2), strX(10, 0, 5))
	b.emit(ldrX(11, 31, 3), ldrX(10, 9, m19cLOSBytes), addX(10, 10, 11), strX(10, 9, m19cLOSBytes), ldrX(10, 9, m19cLOSCount), addImmX(10, 10, 1), strX(10, 9, m19cLOSCount))
	b.emit(ldrX(10, 9, m8TelAllocatedObjects), addImmX(10, 10, 1), strX(10, 9, m8TelAllocatedObjects), ldrX(10, 9, m8TelAllocatedBytes), addX(10, 10, 11), strX(10, 9, m8TelAllocatedBytes))
	appendM19OccupiedAllocation(b, 9, 11, 10, "m19c.los.peak")
	b.emit(ldrX(10, 31, 0), ldrX(11, 10, 5), addImmX(11, 11, 1), strX(11, 10, 5), addImmX(0, 0, 48), addImmX(31, 31, 48), m8Ret())
	b.label("m19c.los.sysfail")
	b.emit(addImmX(31, 31, 48), movzX(0, 0), m8Ret())

	b.label("m19c.los.major")
	m10SaveCallee(b)
	b.emit(movX(19, 0), ldrX(20, 19, 2), ldrX(21, 20, m19cLOSHead), movzX(22, 0), movzX(23, 0))
	b.label("m19c.major.loop")
	b.cbz(21, "m19c.major.done")
	b.emit(m9LdrW(9, 21, 32))
	b.cbz(9, "m19c.major.next")
	b.emit(m9LdrW(9, 21, 36), movzX(10, 1), m8And(10, 9, 10))
	b.cbz(10, "m19c.major.dead")
	b.emit(m8Movn(10, 3), m8And(9, 9, 10), m9StrW(9, 21, 36))
	b.b("m19c.major.next")
	b.label("m19c.major.dead")
	b.emit(m9StrW(31, 21, 32), ldrX(9, 21, 5), addImmX(9, 9, 31), m8Movn(10, 15), m8And(9, 9, 10), addX(23, 23, 9), addImmX(22, 22, 1))
	b.label("m19c.major.next")
	b.emit(ldrX(21, 21, 0))
	b.b("m19c.major.loop")
	b.label("m19c.major.done")
	for _, slot := range []uint32{m19cLOSBytes, m8TelOccupiedBytes, m8TelLastAfterBytes} {
		b.emit(ldrX(9, 20, slot), m10Sub(9, 9, 23), strX(9, 20, slot))
	}
	for _, slot := range []uint32{m19cLOSReclaimed, m8TelLastReclaimedBytes} {
		b.emit(ldrX(9, 20, slot), addX(9, 9, 23), strX(9, 20, slot))
	}
	b.emit(ldrX(9, 20, m19cLOSCount), m10Sub(9, 9, 22), strX(9, 20, m19cLOSCount), ldrX(9, 19, 5), m10Sub(9, 9, 22), strX(9, 19, 5), ldrX(9, 20, m8TelLastReclaimedObjects), addX(9, 9, 22), strX(9, 20, m8TelLastReclaimedObjects), movX(0, 22))
	m10RestoreCallee(b)

	// Sweep has already removed dead nodes from young/remembered lists. Unlink
	// before munmap; never dereference released memory. Single-threaded safe phase.
	b.label(m19cLOSPrune) // heap, x1=1 destroy-all; otherwise only zero-type nodes
	m10SaveCallee(b)
	b.emit(movX(19, 0), movX(22, 1), movzX(25, 0), ldrX(20, 19, 2), addImmX(21, 20, m19cLOSHead*8))
	b.label("m19c.prune.loop")
	b.emit(ldrX(23, 21, 0))
	b.cbz(23, "m19c.prune.done")
	b.cbnz(22, "m19c.prune.dead")
	b.emit(m9LdrW(9, 23, 32))
	b.cbz(9, "m19c.prune.dead")
	b.emit(movX(21, 23))
	b.b("m19c.prune.loop")
	b.label("m19c.prune.dead")
	b.cbnz(22, "m19c.prune.release")
	b.emit(cmpImmX(25, 8))
	b.bc(2, "m19c.prune.done")
	b.emit(addImmX(25, 25, 1))
	b.label("m19c.prune.release")
	b.emit(ldrX(9, 23, 0), strX(9, 21, 0), ldrX(1, 23, 1), movX(24, 1), movX(0, 23), movzX(16, m8DarwinSYSMunmap), svc(0x80))
	// Failed release retains ownership so destroy/retry can recover it.
	b.bc(2, "m19c.prune.failed")
	b.emit(ldrX(9, 20, m19cLOSCommitted), m10Sub(9, 9, 24), strX(9, 20, m19cLOSCommitted))
	b.b("m19c.prune.loop")
	b.label("m19c.prune.failed")
	b.emit(ldrX(9, 21, 0), strX(9, 23, 0), strX(23, 21, 0), movX(21, 23))
	b.b("m19c.prune.loop")
	b.label("m19c.prune.done")
	m10RestoreCallee(b)
}
