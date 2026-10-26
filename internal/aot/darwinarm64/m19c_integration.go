package darwinarm64

// All new configuration is internal; the historical explicit object capacity
// and CLI options retain their meaning. Bit32 is used only between the Go AOT
// options layer and this internal native initializer.
const M19cAdaptiveHeapFlag uint64 = 1 << 32

func appendM19cIntegration(b *m8Assembler) {
	emitM19cMemoryPolicy(b)
	b.label("m19c.init")
	b.emit(subImmX(31, 31, 32), strX(30, 31, 0), m10Lsr(9, 0, 32), strX(9, 31, 1))
	// Strip the private flag without changing low-word capacity.
	b.emit(0xD3407C00) // UBFM x0,x0,#0,#31
	b.cbz(9, "m19c.init.map")
	b.emit(movX(1, 0), movzX(0, 0))
	b.bl("memory.autocap")
	b.cbz(0, "m19c.init.done")
	b.label("m19c.init.map")
	b.bl(m8InitLabel)
	b.cbz(0, "m19c.init.done")
	b.emit(strX(0, 31, 2), ldrX(9, 31, 1))
	b.cbz(9, "m19c.init.sample")
	b.emit(ldrX(1, 0, 3), ldrX(0, 0, 2))
	b.bl("memory.autocap")
	b.cbnz(0, "m19c.init.sample")
	b.emit(ldrX(0, 31, 2))
	b.bl(m8DestroyLabel)
	b.emit(movzX(0, 0))
	b.b("m19c.init.done")
	b.label("m19c.init.sample")
	b.emit(ldrX(0, 31, 2))
	b.bl("m19c.sample")
	b.emit(ldrX(9, 31, 2), ldrX(0, 9, 2), ldrX(1, 9, 6), addImmX(2, 0, M8TelemetrySize), ldrX(3, 9, 3), ldrX(4, 0, m19cGoal), movzX(5, 1))
	b.bl("memory.commit")
	b.cbnz(0, "m19c.init.protected")
	b.emit(ldrX(0, 31, 2))
	b.bl(m8DestroyLabel)
	b.emit(movzX(0, 0))
	b.b("m19c.init.done")
	b.label("m19c.init.protected")
	b.emit(ldrX(0, 31, 2))
	b.bl("m19c.sample")
	b.emit(ldrX(0, 31, 2))
	b.label("m19c.init.done")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 32), m8Ret())

	b.label("m19c.sample") // heap, preserves x0 and every callee-saved register
	b.emit(subImmX(31, 31, 32), strX(30, 31, 0), strX(0, 31, 1))
	b.emit(ldrX(9, 0, 1), ldrX(10, 0, 6), m10Sub(9, 9, 10))
	b.emit(ldrX(10, 0, 2), ldrX(11, 10, m19cHighWater), m8Cmp(9, 11))
	b.bc(2, "m19c.highwater.store")
	b.emit(movX(9, 11))
	b.label("m19c.highwater.store")
	b.emit(strX(9, 10, m19cHighWater))
	b.emit(m8MovBits(10, 16383)...)
	b.emit(addX(9, 9, 10), m8Movn(10, 16383), m8And(9, 9, 10), addX(9, 9, 9))
	b.emit(m8MovBits(10, 65536)...)
	b.emit(addX(9, 9, 10), ldrX(10, 0, 2), ldrX(11, 10, m19cLOSCommitted), addX(9, 9, 11), strX(9, 10, m19cCommitted), ldrX(9, 0, 4), ldrX(11, 10, m19cLOSCommitted), addX(9, 9, 11), strX(9, 10, m19cReserved))
	// Round reserved mappings to OS pages and clamp the conservative working
	// charge to that exact reservation (page sharing can only reduce the charge).
	b.emit(ldrX(9, 10, m19cReserved))
	b.emit(m8MovBits(11, 16383)...)
	b.emit(addX(9, 9, 11), m8Movn(11, 16383), m8And(9, 9, 11), strX(9, 10, m19cReserved), ldrX(11, 10, m19cCommitted), m8Cmp(11, 9))
	b.bc(9, "m19c.charge.bounded")
	b.emit(strX(9, 10, m19cCommitted))
	b.label("m19c.charge.bounded")
	b.emit(ldrX(1, 10, m8TelOccupiedBytes), ldrX(2, 10, m8TelAllocatedBytes), ldrX(3, 0, 3), ldrX(4, 10, m8TelTimerFrequency), ldrX(5, 10, m8TelLastBeforeBytes), ldrX(6, 10, m8TelLastReclaimedBytes), movX(0, 10))
	b.bl("memory.update")
	// An explicit cap starts with a bounded soft goal, too. An automatic cap was
	// configured before this first sample and must not be replaced here.
	b.emit(ldrX(0, 31, 1), ldrX(9, 0, 2), ldrX(10, 9, m19cGoal))
	b.cbnz(10, "m19c.sample.done")
	b.emit(movX(0, 9), movzX(1, 0), movzX(2, 0))
	b.bl("memory.goal")
	b.label("m19c.sample.done")
	b.emit(ldrX(0, 31, 1), ldrX(30, 31, 0), addImmX(31, 31, 32), m8Ret())

	b.label("m19c.commit")
	b.emit(subImmX(31, 31, 48), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2), strX(2, 31, 3), ldrX(9, 0, 1), ldrX(10, 0, 6), m10Sub(4, 9, 10), addImmX(9, 2, 31), m8Movn(10, 15), m8And(9, 9, 10), addX(4, 4, 9), ldrX(1, 0, 6), ldrX(3, 0, 3), ldrX(0, 0, 2), addImmX(2, 0, M8TelemetrySize), movzX(5, 0))
	b.bl("memory.commit")
	b.emit(strX(0, 31, 4), ldrX(0, 31, 1), ldrX(1, 31, 2), ldrX(2, 31, 3), ldrX(9, 31, 4), ldrX(30, 31, 0), addImmX(31, 31, 48), m8Ret())

	b.label("m19c.grow") // heap, requested payload; preserve original args
	b.emit(subImmX(31, 31, 32), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2))
	b.emit(addImmX(2, 1, 31), m8Movn(9, 15), m8And(2, 2, 9), ldrX(0, 0, 2), ldrX(1, 0, m8TelOccupiedBytes))
	b.bl("memory.goal")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), ldrX(30, 31, 0), addImmX(31, 31, 32), m8Ret())

	b.label("m19c.aftergc") // heap, preserve collection result in x1
	b.emit(subImmX(31, 31, 32), strX(30, 31, 0), strX(0, 31, 1), strX(1, 31, 2), ldrX(9, 0, 2), strX(31, 9, m19cGCActive))
	b.bl("m19c.sample")
	b.emit(ldrX(0, 31, 1), ldrX(0, 0, 2), ldrX(1, 0, m8TelOccupiedBytes), movzX(2, 0))
	b.bl("memory.goal")
	b.emit(ldrX(0, 31, 2), ldrX(30, 31, 0), addImmX(31, 31, 32), m8Ret())

	b.label("m19c.destroy")
	b.emit(subImmX(31, 31, 16), strX(30, 31, 0), strX(0, 31, 1), movzX(1, 1))
	b.bl(m19cLOSPrune)
	b.emit(ldrX(0, 31, 1), ldrX(9, 0, 2), ldrX(9, 9, m19cLOSHead))
	b.cbnz(9, "m19c.destroy.failed")
	b.bl(m8DestroyLabel)
	b.b("m19c.destroy.return")
	b.label("m19c.destroy.failed")
	b.emit(movzX(0, 0))
	b.label("m19c.destroy.return")
	b.emit(ldrX(30, 31, 0), addImmX(31, 31, 16), m8Ret())
}
