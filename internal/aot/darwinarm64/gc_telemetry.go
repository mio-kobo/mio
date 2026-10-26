package darwinarm64

import "fmt"

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

	b.emit(mrsCNTVCT(9), ldrX(10, 19, 2), ldrX(11, 10, m8TelStartTicks), m10Sub(9, 9, 11), strX(9, 10, m8TelRuntimeTicks))
	appendM8TelemetryMetric(b, "heap_capacity", func() { b.emit(ldrX(0, 19, 3)) })
	appendM8TelemetryMetric(b, "heap_cursor", func() { b.emit(ldrX(9, 19, 1), ldrX(10, 19, 6), m10Sub(0, 9, 10)) })
	appendM8TelemetryMetric(b, "heap_live_bytes", func() { b.emit(ldrX(9, 19, 2), ldrX(0, 9, m8TelOccupiedBytes)) })
	metrics := []struct {
		name string
		slot uint32
	}{
		{"pressure_transitions", m19cPressureTransitions}, {"heap_goal", m19cGoal}, {"heap_limit", m19cLimit}, {"heap_committed", m19cCommitted}, {"heap_reserved", m19cReserved}, {"pressure_level", m19cLevel}, {"pressure_reason", m19cReason}, {"pressure_sequence", m19cSequence}, {"allocation_rate", m19cRate}, {"system_pressure_available", m19cSystemAvailable},
		{"los_live_bytes", m19cLOSBytes}, {"los_objects", m19cLOSCount}, {"los_reclaimed_bytes", m19cLOSReclaimed},
		{"minor_gc_max_ticks", m19bMinorMax}, {"major_gc_max_ticks", m19bMajorMax},
		{"minor_gc_count", m19bMinorCount}, {"major_gc_count", m19bMajorCount},
		{"minor_gc_total_ticks", m19bMinorTicks}, {"major_gc_total_ticks", m19bMajorTicks},
		{"young_live_bytes", m19bYoungBytes}, {"old_live_bytes", m19bOldBytes},
		{"promoted_objects", m19bPromotedObjects}, {"promoted_bytes", m19bPromotedBytes},
		{"promotion_threshold", m19bThreshold}, {"remembered_set_entries", m19bRememberedCount},
		{"minor_scanned_old_objects", m19bScannedOld},
		{"allocated_objects", m8TelAllocatedObjects}, {"allocated_bytes", m8TelAllocatedBytes},
		{"gc_count", m8TelGCCount}, {"gc_before_bytes", m8TelLastBeforeBytes}, {"gc_after_bytes", m8TelLastAfterBytes},
		{"gc_reclaimed_objects", m8TelLastReclaimedObjects}, {"gc_reclaimed_bytes", m8TelLastReclaimedBytes},
		{"free_bytes", m8TelLastFreeBytes}, {"largest_free_block", m8TelLastLargestFree}, {"free_blocks", m8TelLastFreeBlocks},
		{"bump_remaining", m8TelLastBumpRemaining}, {"oom_request_bytes", m8TelOOMRequestBytes},
		{"shadow_depth", m8TelShadowDepth}, {"precise_roots", m8TelRootCount},
		{"heap_peak_live_bytes", m8TelPeakOccupiedBytes}, {"runtime_ticks", m8TelRuntimeTicks}, {"gc_max_ticks", m8TelMaxGCTicks},
		{"gc_last_ticks", m8TelLastGCTicks}, {"gc_total_ticks", m8TelTotalGCTicks}, {"timer_frequency_hz", m8TelTimerFrequency},
	}
	for _, metric := range metrics {
		slot := metric.slot
		appendM8TelemetryMetric(b, metric.name, func() { b.emit(ldrX(9, 19, 2), ldrX(0, 9, slot)) })
	}
	for i := uint32(0); i < 64; i++ {
		slot := m19TelPauseBucket0 + i
		appendM8TelemetryMetric(b, fmt.Sprintf("gc_pause_bucket_%d", i), func() { b.emit(ldrX(9, 19, 2), ldrX(0, 9, slot)) })
	}
	for _, kind := range []struct {
		name string
		base uint32
	}{{"minor", m19bMinorBucket}, {"major", m19bMajorBucket}} {
		for i := uint32(0); i < 64; i++ {
			slot := kind.base + i
			appendM8TelemetryMetric(b, fmt.Sprintf("%s_gc_pause_bucket_%d", kind.name, i), func() { b.emit(ldrX(9, 19, 2), ldrX(0, 9, slot)) })
		}
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

// Update physical occupied/peak bytes using only caller-provided temporaries.
func appendM19OccupiedAllocation(b *m8Assembler, telemetry, span, scratch uint32, done string) {
	b.emit(ldrX(scratch, telemetry, m8TelOccupiedBytes), addX(scratch, scratch, span), strX(scratch, telemetry, m8TelOccupiedBytes))
	// A second temporary is deliberately avoided: keep the current value in the
	// slot, compare after loading peak into scratch, then reload on a new peak.
	b.emit(ldrX(scratch, telemetry, m8TelPeakOccupiedBytes))
	// Use x17, a caller-saved platform-independent scratch in these leaf paths.
	b.emit(ldrX(17, telemetry, m8TelOccupiedBytes), m8Cmp(17, scratch))
	b.bc(9, done)
	b.emit(strX(17, telemetry, m8TelPeakOccupiedBytes))
	b.label(done)
}
