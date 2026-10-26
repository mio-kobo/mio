package darwinarm64

// The 16-byte header and every precise payload layout retain their ABI.
// Allocated flags: mark=1, scanned=2, old=8, age=bits4..7, remembered=256.
// Free flags keep their historical meaning (type=0, bit2, relative bin link).
// Two native links per possible header live after telemetry in the mmap:
// side(header) = end + telemetryBytes + header - origin. This first version
// reserves capacity bytes of virtual side memory; it is not a LOS or budget.
// Normal allocation policy supplements every eighth minor with a major when
// old objects exist; explicit minor remains independent. This bounds deferred
// dead-old retention and avoids the 8 MiB NativeFS stress fragmentation case.
const (
	m19bOld               = 8
	m19bAgeStep           = 16
	m19bRemembered        = 256
	m19bYoungHead  uint32 = 128 + iota
	m19bRememberedHead
	m19bMode // 0 major, 1 minor
	m19bThreshold
	m19bMinorCount
	m19bMajorCount
	m19bMinorTicks
	m19bMajorTicks
	m19bYoungBytes
	m19bOldBytes
	m19bPromotedObjects
	m19bPromotedBytes
	m19bRememberedCount
	m19bHasYoung
	m19bYoungBefore
	m19bMatureBytes
	m19bAdaptDirection
	m19bAdaptStreak
	m19bScannedOld
	m19bMinorMax      uint32 = 152
	m19bMajorMax      uint32 = 153
	m19bAgeHistogram  uint32 = 160 // age 0..8, physical surviving young bytes
	m19bMinorBucket   uint32 = 192
	m19bMajorBucket   uint32 = 256
	m19bMajorEntry           = "m19b.major"
	m19bMinorEntry           = "m19b.minor"
	m19bRegisterEntry        = "m19b.register"
	m19bRebuildEntry         = "m19b.rebuild"
	m19bScanEntry            = "m19b.scan"
)

// An owner-dirty barrier. It never guesses a pointer from the written word;
// the collector scans only the owner's exact layout. A superset of owners is
// safe (e.g. scalar Cells); obsolete entries are pruned at the next minor.
// No safepoint, allocation, call, or clobbered register, including LR.
func m19bRememberWords(owner, heap uint32) []uint32 {
	b := newM8Assembler()
	b.emit(subImmX(31, 31, 32))
	for r := uint32(14); r <= 17; r++ {
		b.emit(strX(r, 31, r-14))
	}
	b.emit(subImmX(14, owner, 16), m9LdrW(15, 14, 4), movzX(17, m19bOld|m19bRemembered), m8And(15, 15, 17), cmpImmX(15, m19bOld))
	b.bc(1, "done")
	m19cSide(b, 17, 14, heap, 16)
	b.emit(ldrX(15, 16, m19bRememberedHead), strX(15, 17, 1), strX(14, 16, m19bRememberedHead))
	b.emit(m9LdrW(15, 14, 4), movzX(17, m19bRemembered), m12Or(15, 15, 17), m9StrW(15, 14, 4))
	b.emit(ldrX(15, 16, m19bRememberedCount), addImmX(15, 15, 1), strX(15, 16, m19bRememberedCount))
	b.label("done")
	for r := uint32(14); r <= 17; r++ {
		b.emit(ldrX(r, 31, r-14))
	}
	b.emit(addImmX(31, 31, 32))
	words, err := b.assemble()
	if err != nil {
		panic(err)
	}
	return words
}

func m19bSide(b *m8Assembler, dst, header, heap, scratch uint32) {
	m19cSide(b, dst, header, heap, scratch)
}

func appendM19bRuntime(b *m8Assembler) {
	// Allocation registration occurs only in the dynamic object allocator;
	// M8/M9/M10 probe ABIs and their fixed type tables remain separate.
	b.label(m19bRegisterEntry) // x0 payload, x1 heap; returns same payload
	b.emit(subImmX(9, 0, 16))
	m19bSide(b, 10, 9, 1, 11)
	b.emit(ldrX(12, 11, m19bYoungHead), strX(12, 10, 0), strX(31, 10, 1), strX(9, 11, m19bYoungHead))
	b.emit(ldrX(10, 9, 1), addImmX(10, 10, 31), m8Movn(12, 15), m8And(10, 10, 12), ldrX(12, 11, m19bYoungBytes), addX(12, 12, 10), strX(12, 11, m19bYoungBytes), m8Ret())

	b.label(m11ObjectCollectEntry) // bounded major cadence prevents dead-old buildup
	b.emit(subImmX(31, 31, 32), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2))
	b.bl(m19bMinorEntry)
	b.emit(m8Movn(9, 0), m8Cmp(0, 9))
	b.bc(0, "m19b.policy.done")
	b.emit(strX(0, 31, 3), ldrX(9, 31, 1), ldrX(9, 9, 2), ldrX(10, 9, m19bMinorCount), movzX(11, 7), m8And(10, 10, 11))
	b.cbnz(10, "m19b.policy.done")
	b.emit(ldrX(10, 9, m19bOldBytes))
	b.cbz(10, "m19b.policy.done")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2))
	b.bl(m19bMajorEntry)
	b.emit(m8Movn(9, 0), m8Cmp(0, 9))
	b.bc(0, "m19b.policy.done")
	b.emit(ldrX(9, 31, 3), addX(0, 0, 9))
	b.label("m19b.policy.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 32), m8Ret())
	appendM19bMinor(b)
	appendM19bRebuild(b)
	appendM19bAdapt(b)
}

func appendM19bMinor(b *m8Assembler) {
	b.label(m19bMinorEntry) // heap, exact shadow frame
	b.emit(ldrX(9, 0, 2), movzX(10, 1), strX(10, 9, m19cGCActive))
	m10SaveCallee(b)
	b.emit(subImmX(31, 31, 64), movX(19, 0), movX(20, 1), ldrX(21, 19, 6), ldrX(22, 19, 1), movzX(23, 0))
	b.emit(mrsCNTVCT(9), strX(9, 31, 1), ldrX(9, 19, 2), movzX(10, 1), strX(10, 9, m19bMode))
	b.emit(ldrX(10, 9, m19bYoungBytes), strX(10, 9, m19bYoungBefore), strX(31, 9, m19bMatureBytes))
	for i := uint32(0); i < 9; i++ {
		b.emit(strX(31, 9, m19bAgeHistogram+i))
	}
	// All young mark bits were cleared by the preceding sweep; new allocations
	// start with flags=0. Old roots stop here; remembered owners supply edges.
	b.emit(movX(25, 20), movzX(27, 0))
	b.label("m19b.root.frame")
	b.cbz(25, "m19b.rem.begin")
	b.emit(addImmX(27, 27, 1), cmpImmX(27, M11MaxShadowFrames))
	b.bc(8, "m19b.minor.fail")
	b.emit(ldrX(26, 25, 1))
	b.emit(m8MovBits(9, uint64(M11MaxRootsPerFrame))...)
	b.emit(m8Cmp(26, 9))
	b.bc(8, "m19b.minor.fail")
	b.emit(movzX(28, 0))
	b.label("m19b.root.value")
	b.emit(m8Cmp(28, 26))
	b.bc(2, "m19b.root.next")
	b.emit(m10Lsl(9, 28, 3), addImmX(9, 9, 16), addX(9, 25, 9), ldrX(0, 9, 0))
	b.bl(m11TryMarkEntry)
	b.emit(addImmX(28, 28, 1))
	b.b("m19b.root.value")
	b.label("m19b.root.next")
	b.emit(ldrX(25, 25, 0))
	b.b("m19b.root.frame")
	// Drain and rebuild the remembered list with exact outgoing-edge scans.
	b.label("m19b.rem.begin")
	b.emit(ldrX(9, 19, 2), ldrX(25, 9, m19bRememberedHead), strX(31, 9, m19bRememberedHead), strX(31, 9, m19bRememberedCount))
	b.label("m19b.rem.loop")
	b.cbz(25, "m19b.young.begin")
	m19bSide(b, 9, 25, 19, 10)
	b.emit(ldrX(9, 9, 1), strX(9, 31, 0), strX(31, 10, m19bHasYoung))
	b.emit(ldrX(9, 10, m19bScannedOld), addImmX(9, 9, 1), strX(9, 10, m19bScannedOld))
	b.bl(m19bScanEntry)
	b.cbnz(0, "m19b.minor.fail")
	b.emit(m9LdrW(9, 25, 4), m8Movn(10, m19bRemembered|3), m8And(9, 9, 10), m9StrW(9, 25, 4), ldrX(9, 19, 2), ldrX(9, 9, m19bHasYoung))
	b.cbz(9, "m19b.rem.next")
	b.emit(addImmX(0, 25, 16))
	b.emit(m19bRememberWords(0, 19)...)
	b.label("m19b.rem.next")
	b.emit(ldrX(25, 31, 0))
	b.b("m19b.rem.loop")
	// Fixed point over the young list only. No old header walk or old payload
	// scan other than remembered owners. Exact scanner is shared with major.
	b.label("m19b.young.begin")
	b.emit(strX(23, 31, 2), ldrX(9, 19, 2), ldrX(25, 9, m19bYoungHead))
	b.label("m19b.young.loop")
	b.cbz(25, "m19b.young.end")
	b.emit(m9LdrW(9, 25, 4), movzX(10, 3), m8And(9, 9, 10), cmpImmX(9, 1))
	b.bc(1, "m19b.young.next")
	b.bl(m19bScanEntry)
	b.cbnz(0, "m19b.minor.fail")
	b.label("m19b.young.next")
	m19bSide(b, 9, 25, 19, 10)
	b.emit(ldrX(25, 9, 0))
	b.b("m19b.young.loop")
	b.label("m19b.young.end")
	b.emit(ldrX(9, 31, 2), m8Cmp(9, 23))
	b.bc(1, "m19b.young.begin")
	// Sweep just young. Dead blocks enter the existing segregated allocator;
	// major retains full coalescing/splitting. Old dead objects await major.
	b.emit(ldrX(9, 19, 2), ldrX(25, 9, m19bYoungHead), strX(31, 9, m19bYoungHead), strX(31, 9, m19bYoungBytes), movzX(20, 0), movzX(24, 0))
	b.label("m19b.sweep.loop")
	b.cbz(25, "m19b.sweep.done")
	m19bSide(b, 9, 25, 19, 10)
	b.emit(ldrX(9, 9, 0), strX(9, 31, 0))
	b.emit(ldrX(26, 25, 1), addImmX(26, 26, 31), m8Movn(9, 15), m8And(26, 26, 9), m9LdrW(27, 25, 4), movzX(9, 3), m8And(9, 27, 9))
	b.cbz(9, "m19b.sweep.dead")
	b.emit(m8Movn(9, 3), m8And(27, 27, 9), addImmX(27, 27, m19bAgeStep), m10Lsr(28, 27, 4))
	b.emit(ldrX(9, 19, 2), ldrX(10, 9, m19bThreshold), m8Cmp(28, 10))
	b.bc(2, "m19b.sweep.promote")
	b.emit(m9StrW(27, 25, 4))
	m19bSide(b, 10, 25, 19, 9)
	b.emit(ldrX(11, 9, m19bYoungHead), strX(11, 10, 0), strX(25, 9, m19bYoungHead), ldrX(10, 9, m19bYoungBytes), addX(10, 10, 26), strX(10, 9, m19bYoungBytes))
	b.emit(m10Lsl(10, 28, 3), addX(10, 9, 10), ldrX(11, 10, m19bAgeHistogram), addX(11, 11, 26), strX(11, 10, m19bAgeHistogram))
	b.emit(cmpImmX(28, 2))
	b.bc(3, "m19b.sweep.next")
	b.emit(ldrX(10, 9, m19bMatureBytes), addX(10, 10, 26), strX(10, 9, m19bMatureBytes))
	b.b("m19b.sweep.next")
	b.label("m19b.sweep.promote")
	b.emit(movzX(10, m19bOld), m12Or(27, 27, 10), m9StrW(27, 25, 4))
	b.emit(ldrX(10, 9, m19bOldBytes), addX(10, 10, 26), strX(10, 9, m19bOldBytes), ldrX(10, 9, m19bPromotedBytes), addX(10, 10, 26), strX(10, 9, m19bPromotedBytes), ldrX(10, 9, m19bPromotedObjects), addImmX(10, 10, 1), strX(10, 9, m19bPromotedObjects))
	// Promotion can create old->young without any mutator store. Queue the
	// owner for an exact scan at the next minor (safe superset until then).
	b.emit(addImmX(0, 25, 16))
	b.emit(m19bRememberWords(0, 19)...)
	b.b("m19b.sweep.next")
	b.label("m19b.sweep.dead")
	b.emit(m9StrW(31, 25, 0), addX(20, 20, 26), addImmX(24, 24, 1))
	b.emit(ldrX(9, 19, 6), m8Cmp(25, 9))
	b.bc(3, "m19c.minor.dead.los")
	b.emit(ldrX(9, 19, 2), m8Cmp(25, 9))
	b.bc(2, "m19c.minor.dead.los")
	appendM10SizeClass(b, 26, 13, 9, "m19b.dead.class")
	appendM10LoadBinHead(b, 7, 19, 13, 9, 12, "m19b.dead.load")
	b.emit(m10Lsl(10, 7, 3), addImmX(10, 10, M10FreeBit), m9StrW(10, 25, 4), m10Sub(7, 25, 21), m10Lsr(7, 7, 4), addImmX(7, 7, 1))
	appendM10StoreBinHead(b, 7, 19, 13, 9, 12, "m19b.dead.store")
	b.b("m19b.sweep.next")
	b.label("m19c.minor.dead.los")
	b.emit(ldrX(9, 19, 2), ldrX(10, 9, m19cLOSBytes), m10Sub(10, 10, 26), strX(10, 9, m19cLOSBytes), ldrX(10, 9, m19cLOSCount), subImmX(10, 10, 1), strX(10, 9, m19cLOSCount), ldrX(10, 9, m19cLOSReclaimed), addX(10, 10, 26), strX(10, 9, m19cLOSReclaimed))
	b.label("m19b.sweep.next")
	b.emit(ldrX(25, 31, 0))
	b.b("m19b.sweep.loop")
	b.label("m19b.sweep.done")
	b.emit(ldrX(9, 19, 5), m10Sub(9, 9, 24), strX(9, 19, 5), ldrX(12, 19, 2), ldrX(9, 12, m8TelOccupiedBytes), strX(9, 12, m8TelLastBeforeBytes), m10Sub(9, 9, 20), strX(9, 12, m8TelOccupiedBytes), strX(9, 12, m8TelLastAfterBytes), strX(20, 12, m8TelLastReclaimedBytes), strX(24, 12, m8TelLastReclaimedObjects))
	b.emit(movX(0, 19), movzX(1, 0))
	b.bl(m19cLOSPrune)
	b.emit(movX(0, 19))
	b.bl("m19b.adapt")
	b.emit(mrsCNTVCT(9), ldrX(10, 31, 1), m10Sub(9, 9, 10), ldrX(12, 19, 2))
	appendM19bTiming(b, m19bMinorCount, m19bMinorTicks, m19bMinorBucket, "minor")
	b.emit(strX(31, 12, m19bMode), movX(1, 24), movX(0, 19))
	b.bl("m19c.aftergc")
	b.b("m19b.minor.restore")
	b.label("m19b.minor.fail")
	b.emit(ldrX(9, 19, 2), strX(31, 9, m19bMode), m8Movn(0, 0))
	b.label("m19b.minor.restore")
	b.emit(addImmX(31, 31, 64))
	m10RestoreCallee(b)
}

func appendM19bTiming(b *m8Assembler, count, total, bucket uint32, prefix string) {
	b.emit(ldrX(10, 12, m19bMinorMax), m8Cmp(9, 10))
	b.bc(9, "m19b.minor.kindmax")
	b.emit(strX(9, 12, m19bMinorMax))
	b.label("m19b.minor.kindmax")
	for _, slot := range []uint32{count, m8TelGCCount} {
		b.emit(ldrX(10, 12, slot), addImmX(10, 10, 1), strX(10, 12, slot))
	}
	for _, slot := range []uint32{total, m8TelTotalGCTicks} {
		b.emit(ldrX(10, 12, slot), addX(10, 10, 9), strX(10, 12, slot))
	}
	b.emit(strX(9, 12, m8TelLastGCTicks), ldrX(10, 12, m8TelMaxGCTicks), m8Cmp(9, 10))
	b.bc(9, "m19b."+prefix+".max")
	b.emit(strX(9, 12, m8TelMaxGCTicks))
	b.label("m19b." + prefix + ".max")
	b.emit(movzX(13, 0))
	b.cbz(9, "m19b."+prefix+".bucket")
	b.emit(subImmX(14, 9, 1))
	b.label("m19b." + prefix + ".shift")
	b.cbz(14, "m19b."+prefix+".bucket")
	b.emit(cmpImmX(13, 63))
	b.bc(0, "m19b."+prefix+".bucket")
	b.emit(m10Lsr(14, 14, 1), addImmX(13, 13, 1))
	b.b("m19b." + prefix + ".shift")
	b.label("m19b." + prefix + ".bucket")
	b.emit(m10Lsl(13, 13, 3), addX(13, 12, 13))
	for _, slot := range []uint32{bucket, m19TelPauseBucket0} {
		b.emit(ldrX(14, 13, slot), addImmX(14, 14, 1), strX(14, 13, slot))
	}
}

func appendM19bRebuild(b *m8Assembler) {
	b.label(m19bRebuildEntry) // after successful major, no ages advance
	m10SaveCallee(b)
	b.emit(movX(19, 0), ldrX(21, 19, 6), ldrX(22, 19, 1), ldrX(9, 19, 2), strX(31, 9, m19bMode), strX(31, 9, m19bYoungHead), strX(31, 9, m19bRememberedHead), strX(31, 9, m19bRememberedCount), strX(31, 9, m19bYoungBytes), strX(31, 9, m19bOldBytes), movX(25, 21), movzX(28, 0))
	b.label("m19b.rebuild.loop")
	b.emit(m8Cmp(25, 22))
	b.bc(2, "m19c.rebuild.los.begin")
	b.label("m19c.rebuild.body")
	b.emit(ldrX(26, 25, 1), addImmX(26, 26, 31), m8Movn(9, 15), m8And(26, 26, 9), m9LdrW(9, 25, 0))
	b.cbz(9, "m19b.rebuild.next")
	b.emit(m9LdrW(9, 25, 4), m8Movn(10, m19bRemembered|3), m8And(9, 9, 10), m9StrW(9, 25, 4), movzX(10, m19bOld), m8And(10, 9, 10))
	b.cbnz(10, "m19b.rebuild.old")
	b.emit(addImmX(0, 25, 16), movX(1, 19))
	b.bl(m19bRegisterEntry)
	b.b("m19b.rebuild.next")
	b.label("m19b.rebuild.old")
	b.emit(ldrX(9, 19, 2), ldrX(10, 9, m19bOldBytes), addX(10, 10, 26), strX(10, 9, m19bOldBytes), addImmX(0, 25, 16))
	b.emit(m19bRememberWords(0, 19)...)
	b.label("m19b.rebuild.next")
	b.cbnz(28, "m19c.rebuild.los.next")
	b.emit(addX(25, 25, 26))
	b.b("m19b.rebuild.loop")
	b.label("m19c.rebuild.los.begin")
	b.emit(movzX(28, 1), ldrX(9, 19, 2), ldrX(25, 9, m19cLOSHead))
	b.b("m19c.rebuild.los.test")
	b.label("m19c.rebuild.los.next")
	b.emit(ldrX(25, 31, 11))
	b.label("m19c.rebuild.los.test")
	b.cbz(25, "m19b.rebuild.done")
	b.emit(ldrX(9, 25, 0), strX(9, 31, 11), addImmX(25, 25, 32))
	b.b("m19c.rebuild.body")
	b.label("m19b.rebuild.done")
	b.emit(movX(0, 19), movzX(1, 0))
	b.bl(m19cLOSPrune)
	b.emit(movzX(0, 0))
	m10RestoreCallee(b)
}

// Deterministic hysteresis: three consecutive votes before a one-step change.
// High old pressure or <25% survival votes up; >50% survival with >=25% of
// the pre-GC young bytes in age>=2 votes down. Bounds are 2..8, initial 3.
func appendM19bAdapt(b *m8Assembler) {
	b.label("m19b.adapt")
	b.emit(ldrX(9, 0, 2), ldrX(10, 9, m19bYoungBefore))
	b.cbz(10, "m19b.adapt.done")
	b.emit(ldrX(11, 9, m19bYoungBytes), ldrX(12, 9, m19bOldBytes), ldrX(13, 0, 3), m10Lsr(13, 13, 1), m8Cmp(12, 13))
	b.bc(2, "m19b.adapt.up")
	// Include promoted bytes in survival via before - reclaimed.
	b.emit(ldrX(12, 9, m8TelLastReclaimedBytes), m10Sub(11, 10, 12), m10Lsr(12, 10, 2), m8Cmp(11, 12))
	b.bc(3, "m19b.adapt.up")
	b.emit(m10Lsr(13, 10, 1), m8Cmp(11, 13))
	b.bc(9, "m19b.adapt.reset")
	b.emit(ldrX(11, 9, m19bMatureBytes), m8Cmp(11, 12))
	b.bc(3, "m19b.adapt.reset")
	b.emit(movzX(10, 2))
	b.b("m19b.adapt.vote")
	b.label("m19b.adapt.up")
	b.emit(movzX(10, 1))
	b.label("m19b.adapt.vote")
	b.emit(ldrX(11, 9, m19bAdaptDirection), m8Cmp(10, 11))
	b.bc(0, "m19b.adapt.same")
	b.emit(strX(10, 9, m19bAdaptDirection), strX(31, 9, m19bAdaptStreak))
	b.label("m19b.adapt.same")
	b.emit(ldrX(11, 9, m19bAdaptStreak), addImmX(11, 11, 1), strX(11, 9, m19bAdaptStreak), cmpImmX(11, 3))
	b.bc(3, "m19b.adapt.done")
	b.emit(strX(31, 9, m19bAdaptStreak), ldrX(11, 9, m19bThreshold), cmpImmX(10, 1))
	b.bc(1, "m19b.adapt.down")
	b.emit(cmpImmX(11, 8))
	b.bc(2, "m19b.adapt.done")
	b.emit(addImmX(11, 11, 1))
	b.b("m19b.adapt.store")
	b.label("m19b.adapt.down")
	b.emit(cmpImmX(11, 2))
	b.bc(9, "m19b.adapt.done")
	b.emit(subImmX(11, 11, 1))
	b.label("m19b.adapt.store")
	b.emit(strX(11, 9, m19bThreshold))
	b.b("m19b.adapt.done")
	b.label("m19b.adapt.reset")
	b.emit(strX(31, 9, m19bAdaptDirection), strX(31, 9, m19bAdaptStreak))
	b.label("m19b.adapt.done")
	b.emit(m8Ret())
}
