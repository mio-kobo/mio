package darwinarm64

// The policy block has identical byte offsets in both native runtimes. It is
// single-writer/single-threaded today. Sequence brackets publication, but is NOT
// a lock-free concurrent-reader protocol (future threads need atomics + ordering).
const (
	m19cGoal uint32 = 325 + iota
	m19cLimit
	m19cCommitted
	m19cReserved
	m19cRate
	m19cLevel
	m19cReason
	m19cSequence
	m19cGCActive
	m19cSystem
	m19cLastSample
	m19cLastAllocated
	m19cRecovery
	m19cStrategy // 0 High, 1 Balanced, 2 Low; correctness is identical
	m19cObjectLimit
	m19cLOSCommitted
	m19cLastBefore
	m19cLastReclaimed
	m19cAuto
	m19cSystemAvailable
	m19cSnapshotUsed
	m19cSnapshotAllocated
	m19cSampleFrequency
	m19cHighWater
	m19cCommitFrontier
	m19cAdapterReclaimed // pure Mio adapter scratch at 2800
	m19cPressureTransitions
)

// emitM19cMemoryPolicy is ABI-independent: metadata fields above are shared.
// update(meta, used, allocated, objectCapacity, frequency, before, reclaimed).
// No user callbacks, managed allocations, or logging occur here.
func emitM19cMemoryPolicy(b *m8Assembler) {
	b.label("memory.update")
	m10SaveCallee(b)
	b.emit(strX(31, 31, 11))
	b.emit(movX(19, 0), movX(20, 1), movX(21, 2), movX(22, 3), movX(23, 4))
	b.emit(strX(5, 19, m19cLastBefore), strX(6, 19, m19cLastReclaimed), strX(20, 19, m19cSnapshotUsed), strX(21, 19, m19cSnapshotAllocated), strX(23, 19, m19cSampleFrequency))
	b.emit(ldrX(9, 19, m19cSequence), addImmX(9, 9, 1), strX(9, 19, m19cSequence))
	b.emit(ldrX(9, 19, m19cObjectLimit))
	b.cbnz(9, "memory.initialized")
	b.emit(strX(22, 19, m19cObjectLimit))
	b.emit(m8MovBits(9, 16383)...)
	b.emit(addX(9, 22, 9), m8Movn(10, 16383), m8And(9, 9, 10), addX(10, 9, 9), addX(9, 9, 10))
	b.emit(m8MovBits(10, 65536)...)
	b.emit(addX(9, 9, 10), strX(9, 19, m19cLimit))
	b.emit(m8MovBits(9, 1<<20)...)
	b.emit(m8Cmp(9, 22))
	b.bc(9, "memory.initial.goal")
	b.emit(movX(9, 22))
	b.label("memory.initial.goal")
	b.emit(strX(9, 19, m19cGoal))
	b.label("memory.initialized")
	// A 10ms sampling interval avoids an OS query for every tiny allocation.
	b.emit(mrsCNTVCT(24), ldrX(9, 19, m19cLastSample), m10Sub(25, 24, 9), movzX(10, 100), udivX(10, 23, 10), m8Cmp(25, 10))
	b.bc(3, "memory.evaluate")
	b.emit(movzX(9, 1), strX(9, 31, 11))
	b.emit(strX(24, 19, m19cLastSample), ldrX(9, 19, m19cLastAllocated), m10Sub(9, 21, 9), strX(21, 19, m19cLastAllocated), m8Movn(10, 0), udivX(10, 10, 23), m8Cmp(9, 10))
	b.bc(8, "memory.rate.saturated")
	b.emit(mulX(9, 9, 23), udivX(9, 9, 25))
	b.b("memory.rate.store")
	b.label("memory.rate.saturated")
	b.emit(m8Movn(9, 0))
	b.label("memory.rate.store")
	b.emit(strX(9, 19, m19cRate))
	// Darwin sysctlbyname(name,namelen,oldp,oldlenp,newp,newlen).
	b.emit(ldrX(9, 19, m19cSystemAvailable), cmpImmX(9, 2))
	b.bc(0, "memory.evaluate")
	name := "kern.memorystatus_vm_pressure_level"
	b.emit(subImmX(31, 31, 80), strX(31, 31, 6), movzX(9, 4), strX(9, 31, 7))
	for i, c := range []byte(name + "\x00") {
		b.emit(movzX(9, uint16(c)), strbW(9, 31)|uint32(i)<<10)
	}
	b.emit(addImmX(0, 31, 0), movzX(1, uint16(len(name))), addImmX(2, 31, 48), addImmX(3, 31, 56), movzX(4, 0), movzX(5, 0), movzX(16, 274), svc(128))
	b.bc(2, "memory.system.unavailable")
	b.emit(ldrX(9, 31, 6), strX(9, 19, m19cSystem), movzX(9, 1), strX(9, 19, m19cSystemAvailable))
	b.b("memory.system.done")
	b.label("memory.system.unavailable")
	b.emit(strX(31, 19, m19cSystemAvailable))
	b.label("memory.system.done")
	b.emit(addImmX(31, 31, 80))
	b.label("memory.evaluate")
	b.emit(movzX(26, 0), movzX(27, 0), ldrX(28, 19, m19cObjectLimit), movzX(9, 100), mulX(10, 20, 9), udivX(10, 10, 28))
	for _, threshold := range []struct{ p, level uint16 }{{70, 1}, {85, 2}, {95, 3}} {
		label := "memory.used." + fmtIndex(int(threshold.p))
		b.emit(cmpImmX(10, uint32(threshold.p)))
		b.bc(3, label)
		b.emit(movzX(26, threshold.level), movzX(27, 1))
		b.label(label)
	}
	// Committed pressure uses the managed page-charge bound, never process RSS.
	b.emit(ldrX(9, 19, m19cCommitted), movzX(10, 100), mulX(9, 9, 10), ldrX(10, 19, m19cLimit), udivX(9, 9, 10), cmpImmX(9, 85))
	b.bc(3, "memory.predict")
	b.emit(movzX(9, 2), m12Or(27, 27, 9), cmpImmX(26, 2))
	b.bc(2, "memory.predict")
	b.emit(movzX(26, 2))
	b.label("memory.predict")
	b.emit(m10Sub(9, 28, 20), ldrX(10, 19, m19cRate), m10Lsr(10, 10, 2), m8Cmp(9, 10))
	b.bc(8, "memory.gc")
	b.emit(movzX(26, 3), movzX(10, 4), m12Or(27, 27, 10))
	b.label("memory.gc")
	b.emit(ldrX(9, 19, m19cLastBefore))
	b.cbz(9, "memory.system")
	b.emit(m10Lsr(9, 9, 4), ldrX(10, 19, m19cLastReclaimed), m8Cmp(10, 9))
	b.bc(2, "memory.system")
	b.emit(m10Lsr(9, 28, 1), m8Cmp(20, 9))
	b.bc(3, "memory.system")
	b.emit(movzX(9, 8), m12Or(27, 27, 9))
	b.cbnz(26, "memory.system")
	b.emit(movzX(26, 1))
	b.label("memory.system")
	b.emit(ldrX(9, 19, m19cSystemAvailable))
	b.cbz(9, "memory.hysteresis")
	b.emit(ldrX(9, 19, m19cSystem), cmpImmX(9, 1))
	b.bc(9, "memory.hysteresis")
	b.emit(movzX(10, 16), m12Or(27, 27, 10), cmpImmX(9, 4))
	b.bc(3, "memory.system.warning")
	b.emit(movzX(26, 3))
	b.b("memory.hysteresis")
	b.label("memory.system.warning")
	b.emit(cmpImmX(26, 2))
	b.bc(2, "memory.hysteresis")
	b.emit(movzX(26, 2))
	b.label("memory.hysteresis")
	b.emit(ldrX(9, 19, m19cLevel), m8Cmp(26, 9))
	b.bc(2, "memory.publish")
	// Recovery needs three distinct 10ms-or-longer samples, not three allocations.
	b.emit(ldrX(10, 31, 11))
	b.cbz(10, "memory.finish")
	b.emit(ldrX(10, 19, m19cRecovery), addImmX(10, 10, 1), strX(10, 19, m19cRecovery), cmpImmX(10, 3))
	b.bc(3, "memory.finish")
	b.label("memory.publish")
	b.emit(m8Cmp(26, 9))
	b.bc(0, "memory.publish.same")
	b.emit(ldrX(10, 19, m19cPressureTransitions), addImmX(10, 10, 1), strX(10, 19, m19cPressureTransitions))
	b.label("memory.publish.same")
	b.emit(strX(26, 19, m19cLevel), strX(27, 19, m19cReason), strX(31, 19, m19cRecovery))
	b.label("memory.finish")
	b.emit(ldrX(9, 19, m19cSequence), addImmX(9, 9, 1), strX(9, 19, m19cSequence))
	m10RestoreCallee(b)

	// Goal is a scheduling budget, distinct from capacity, mapping, and RSS.
	// After GC: live*2 + requested span, bounded by the shared object capacity.
	b.label("memory.goal") // metadata, live, request
	b.emit(ldrX(9, 0, m19cObjectLimit), addX(10, 1, 1), addX(10, 10, 2), ldrX(11, 0, m19cRate), movzX(12, 20), udivX(11, 11, 12), m10Lsr(12, 9, 2), m8Cmp(11, 12))
	b.bc(9, "memory.goal.rate")
	b.emit(movX(11, 12))
	b.label("memory.goal.rate")
	b.emit(addX(10, 10, 11))
	b.emit(m8MovBits(11, 1<<20)...)
	b.emit(m8Cmp(10, 11))
	b.bc(2, "memory.goal.minimum")
	b.emit(movX(10, 11))
	b.label("memory.goal.minimum")
	b.emit(m8Cmp(10, 9))
	b.bc(9, "memory.goal.store")
	b.emit(movX(10, 9))
	b.label("memory.goal.store")
	b.emit(strX(10, 0, m19cGoal), m8Ret())

	// Optional automatic object budget. Hardware memory and both DATA/AS soft
	// limits constrain it; 3x object capacity plus fixed-page overhead bounds all
	// arena side metadata and page-rounded LOS overhead. Query failure is explicit
	// in the configuration flag and uses a conservative 16 MiB object fallback.
	b.label("memory.autocap") // metadata, requested max object capacity
	m10SaveCallee(b)
	b.emit(movX(19, 0), movX(20, 1))
	b.emit(m8MovBits(21, 16<<20)...)
	b.emit(subImmX(31, 31, 64), movzX(9, 6), m9StrW(9, 31, 0), movzX(9, 24), m9StrW(9, 31, 4), strX(31, 31, 2), movzX(9, 8), strX(9, 31, 3))
	b.emit(addImmX(0, 31, 0), movzX(1, 2), addImmX(2, 31, 16), addImmX(3, 31, 24), movzX(4, 0), movzX(5, 0), movzX(16, 202), svc(128))
	b.bc(2, "memory.auto.limits")
	b.emit(ldrX(9, 31, 2))
	b.emit(m8MovBits(10, 256<<20)...)
	b.emit(m8Cmp(9, 10))
	b.bc(9, "memory.auto.fail")
	b.emit(m10Sub(9, 9, 10), movzX(10, 12), udivX(21, 9, 10))
	b.label("memory.auto.limits")
	for _, resource := range []uint16{2, 5} {
		label := "memory.auto.limit." + fmtIndex(int(resource))
		b.emit(movzX(0, resource), addImmX(1, 31, 32), movzX(16, 194), svc(128))
		b.bc(2, label)
		b.emit(ldrX(9, 31, 4))
		b.emit(m8MovBits(10, 65536)...)
		b.emit(m8Cmp(9, 10))
		b.bc(9, "memory.auto.fail")
		b.emit(m10Sub(9, 9, 10), movzX(10, 6), udivX(9, 9, 10), m8Cmp(21, 9))
		b.bc(9, label)
		b.emit(movX(21, 9))
		b.label(label)
	}
	b.emit(m8Cmp(21, 20))
	b.bc(9, "memory.auto.store")
	b.emit(movX(21, 20))
	b.label("memory.auto.store")
	b.emit(m8Movn(9, 16383), m8And(21, 21, 9), cmpImmX(21, 0))
	b.bc(0, "memory.auto.fail")
	b.cbz(19, "memory.auto.value")
	b.emit(strX(21, 19, m19cObjectLimit), addX(9, 21, 21), addX(9, 9, 21))
	b.emit(m8MovBits(10, 65536)...)
	b.emit(addX(9, 9, 10), strX(9, 19, m19cLimit), movzX(9, 1), strX(9, 19, m19cAuto), movX(0, 21))
	b.b("memory.auto.return")
	b.label("memory.auto.value")
	b.emit(movX(0, 21))
	b.b("memory.auto.return")
	b.label("memory.auto.fail")
	b.emit(movzX(0, 0))
	b.label("memory.auto.return")
	b.emit(addImmX(31, 31, 64))
	m10RestoreCallee(b)

	// Explicit page access for the reserved non-moving arena and sparse side
	// metadata. setup revokes unused suffixes; commit enables stable prefixes.
	// (metadata, origin, sideBase, capacity, desiredFrontier, setupFlag) -> bool.
	b.label("memory.commit")
	m10SaveCallee(b)
	b.emit(movX(19, 0), movX(20, 1), movX(21, 2), movX(22, 3), movX(23, 4), movX(24, 5))
	// Commit by OS pages, not by the exact next object end. Re-protecting
	// the same prefix for every 16-byte object would turn allocation into an
	// O(objects) stream of unnecessary mprotect syscalls.
	b.emit(m8MovBits(9, 16383)...)
	b.emit(addX(23, 23, 9), m8Movn(9, 16383), m8And(23, 23, 9))
	b.emit(m8Cmp(23, 22))
	b.bc(9, "memory.commit.bounded")
	b.emit(movX(23, 22))
	b.label("memory.commit.bounded")
	b.emit(m8MovBits(9, 16383)...)
	b.emit(addX(9, 23, 9), m8Movn(10, 16383), m8And(9, 9, 10), addX(9, 9, 9))
	b.emit(m8MovBits(10, 65536)...)
	b.emit(addX(9, 9, 10), ldrX(10, 19, m19cLOSCommitted), addX(9, 9, 10), ldrX(10, 19, m19cLimit), m8Cmp(9, 10))
	b.bc(8, "memory.commit.fail")
	b.cbnz(24, "memory.commit.setup")
	b.emit(ldrX(9, 19, m19cCommitFrontier), m8Cmp(23, 9))
	b.bc(9, "memory.commit.success")
	b.emit(ldrX(9, 19, m19cHighWater), m8Cmp(23, 9))
	b.bc(9, "memory.commit.charge")
	b.emit(strX(23, 19, m19cHighWater))
	b.label("memory.commit.charge")
	for i, reg := range []uint32{20, 21} {
		label := "memory.commit.prefix." + fmtIndex(i)
		b.emit(m8Movn(9, 16383), m8And(0, reg, 9), addX(1, reg, 23))
		b.emit(m8MovBits(10, 16383)...)
		b.emit(addX(1, 1, 10), m8And(1, 1, 9), m10Sub(1, 1, 0), movzX(2, 3), movzX(16, 74), svc(128))
		b.bc(2, "memory.commit.fail")
		b.label(label)
	}
	b.emit(strX(23, 19, m19cCommitFrontier))
	b.b("memory.commit.success")
	b.label("memory.commit.setup")
	// Object suffix stops before the fixed metadata/side boundary; those pages
	// must remain readable even when the logical goal is very small.
	for i, reg := range []uint32{20, 21} {
		label := "memory.commit.suffix." + fmtIndex(i)
		b.emit(addX(0, reg, 23))
		b.emit(m8MovBits(10, 16383)...)
		b.emit(addX(0, 0, 10), m8Movn(9, 16383), m8And(0, 0, 9), addX(1, reg, 22))
		if i == 1 {
			b.emit(addX(1, 1, 10))
		}
		b.emit(m8And(1, 1, 9), m8Cmp(0, 1))
		b.bc(2, label)
		b.emit(m10Sub(1, 1, 0), movzX(2, 0), movzX(16, 74), svc(128))
		b.bc(2, "memory.commit.fail")
		b.label(label)
	}
	b.emit(strX(23, 19, m19cHighWater), strX(23, 19, m19cCommitFrontier))
	b.label("memory.commit.success")
	b.emit(movzX(0, 1))
	b.b("memory.commit.return")
	b.label("memory.commit.fail")
	b.emit(movzX(0, 0))
	b.label("memory.commit.return")
	m10RestoreCallee(b)

	// Copy the immutable-by-contract snapshot into caller-owned native storage.
	// No returned pointer aliases mutable policy state. Not a multithread guarantee.
	b.label("memory.snapshot") // metadata, native destination
	for i, slot := range []uint32{m19cLevel, m19cReason, m19cSnapshotUsed, m19cCommitted, m19cLimit, m19cRate, m19cGCActive, m19cSequence} {
		b.emit(ldrX(9, 0, slot), strX(9, 1, uint32(i)))
	}
	b.emit(m8Ret())
}

// M19cPolicyTemplate returns symbolic assembly for the shared native policy.
// The generated Mio emitter performs all branch relocation itself. It neither
// calls Go nor consumes a Go compiler at runtime or during native self-compilation.
type M19cPolicyRelocation struct {
	At        int
	Kind      int
	Label     string
	Cond, Reg uint32
}
type M19cPolicyTemplateData struct {
	Words       []uint32
	Labels      map[string]int
	Relocations []M19cPolicyRelocation
}

func M19cPolicyTemplate() M19cPolicyTemplateData {
	b := newM8Assembler()
	emitM19cMemoryPolicy(b)
	out := M19cPolicyTemplateData{Words: b.words, Labels: b.labels}
	for _, r := range b.relocs {
		out.Relocations = append(out.Relocations, M19cPolicyRelocation{r.at, int(r.kind), r.label, r.cond, r.reg})
	}
	return out
}
