package darwinarm64

const m8TelemetryEntry = "heap.telemetry"

// appendM8TelemetryRuntime emits an allocation-free diagnostic path. It scans
// the physical heap and exact shadow chain, stores a consistent snapshot in
// the out-of-heap telemetry area, then writes decimal fields to stderr.
func appendM8TelemetryRuntime(b *m8Assembler) {
	b.label(m8TelemetryEntry)
	m10SaveCallee(b)
	b.emit(movX(19, 0), movX(20, 1))
	b.cbz(19, "telemetry.done")
	b.emit(ldrX(9, 19, 0))
	b.emit(m8MovBits(10, m8HeapMagic)...)
	b.emit(m8Cmp(9, 10))
	b.bc(1, "telemetry.done")
	b.emit(ldrX(21, 19, 6), ldrX(22, 19, 1), movzX(23, 0), movzX(24, 0), movzX(25, 0), movzX(26, 0))
	b.label("telemetry.heap.loop")
	b.emit(m8Cmp(21, 22))
	b.bc(2, "telemetry.heap.done")
	b.emit(ldrX(27, 21, 1), addImmX(27, 27, 31), m8Movn(9, 15), m8And(27, 27, 9), m9LdrW(10, 21, 0))
	b.cbnz(10, "telemetry.heap.live")
	b.emit(addX(24, 24, 27), addImmX(26, 26, 1), m8Cmp(27, 25))
	b.bc(9, "telemetry.heap.next") // LS
	b.emit(movX(25, 27))
	b.b("telemetry.heap.next")
	b.label("telemetry.heap.live")
	b.emit(addX(23, 23, 27))
	b.label("telemetry.heap.next")
	b.emit(addX(21, 21, 27))
	b.b("telemetry.heap.loop")
	b.label("telemetry.heap.done")
	b.emit(ldrX(21, 19, 2), ldrX(27, 19, 1), m10Sub(27, 21, 27))
	b.emit(strX(24, 21, m8TelLastFreeBytes), strX(25, 21, m8TelLastLargestFree), strX(26, 21, m8TelLastFreeBlocks), strX(27, 21, m8TelLastBumpRemaining))

	// Count frames and published exact roots. A malformed chain is bounded by
	// the same frame limit as the collector and cannot trap the OOM reporter.
	b.emit(movX(21, 20), movzX(24, 0), movzX(25, 0))
	b.label("telemetry.roots.loop")
	b.cbz(21, "telemetry.roots.done")
	b.emit(addImmX(24, 24, 1), cmpImmX(24, M11MaxShadowFrames))
	b.bc(8, "telemetry.roots.done")
	b.emit(ldrX(26, 21, 1), addX(25, 25, 26), ldrX(21, 21, 0))
	b.b("telemetry.roots.loop")
	b.label("telemetry.roots.done")
	b.emit(ldrX(21, 19, 2), strX(24, 21, m8TelShadowDepth), strX(25, 21, m8TelRootCount))

	appendM8TelemetryMetric(b, "heap_capacity", func() { b.emit(ldrX(0, 19, 3)) })
	appendM8TelemetryMetric(b, "heap_cursor", func() { b.emit(ldrX(9, 19, 1), ldrX(10, 19, 6), m10Sub(0, 9, 10)) })
	appendM8TelemetryMetric(b, "heap_live_bytes", func() { b.emit(movX(0, 23)) })
	metrics := []struct {
		name string
		slot uint32
	}{
		{"allocated_objects", m8TelAllocatedObjects}, {"allocated_bytes", m8TelAllocatedBytes},
		{"gc_count", m8TelGCCount}, {"gc_before_bytes", m8TelLastBeforeBytes}, {"gc_after_bytes", m8TelLastAfterBytes},
		{"gc_reclaimed_objects", m8TelLastReclaimedObjects}, {"gc_reclaimed_bytes", m8TelLastReclaimedBytes},
		{"free_bytes", m8TelLastFreeBytes}, {"largest_free_block", m8TelLastLargestFree}, {"free_blocks", m8TelLastFreeBlocks},
		{"bump_remaining", m8TelLastBumpRemaining}, {"oom_request_bytes", m8TelOOMRequestBytes},
		{"shadow_depth", m8TelShadowDepth}, {"precise_roots", m8TelRootCount},
		{"gc_last_ticks", m8TelLastGCTicks}, {"gc_total_ticks", m8TelTotalGCTicks}, {"timer_frequency_hz", m8TelTimerFrequency},
	}
	for _, metric := range metrics {
		slot := metric.slot
		appendM8TelemetryMetric(b, metric.name, func() { b.emit(ldrX(9, 19, 2), ldrX(0, 9, slot)) })
	}
	b.label("telemetry.done")
	m10RestoreCallee(b)

	b.label("telemetry.print.uint")
	b.emit(subImmX(31, 31, 32), addImmX(1, 31, 31), movzW(4, '\n'), strbW(4, 1), movzX(2, 1), movzX(6, 10))
	b.label("telemetry.print.digit")
	b.emit(udivX(3, 0, 6), msubX(4, 3, 6, 0), addImmW(4, 4, 48), subImmX(1, 1, 1), strbW(4, 1), addImmX(2, 2, 1), movX(0, 3))
	b.cbnz(0, "telemetry.print.digit")
	b.emit(movzX(0, 2), movzX(16, m8DarwinSYSWrite), svc(0x80), addImmX(31, 31, 32), m8Ret())
}

func appendM8TelemetryMetric(b *m8Assembler, name string, load func()) {
	prefix := "mio-gc " + name + "="
	frame := (len(prefix) + 15) &^ 15
	b.emit(subImmX(31, 31, uint32(frame)))
	for index, ch := range []byte(prefix) {
		b.emit(movzW(9, uint16(ch)), strbOffsetW(9, 31, uint32(index)))
	}
	b.emit(movzX(0, 2), addImmX(1, 31, 0), movzX(2, uint16(len(prefix))), movzX(16, m8DarwinSYSWrite), svc(0x80), addImmX(31, 31, uint32(frame)))
	load()
	b.bl("telemetry.print.uint")
}
