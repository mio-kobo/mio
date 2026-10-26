package darwinarm64

// GCStrategy reserves policy choices for M19f. None changes precise layout,
// roots, barrier insertion, or collector correctness in M19c.
type GCStrategy uint8

const (
	GCStrategyHigh GCStrategy = iota
	GCStrategyBalanced
	GCStrategyLow
)

// MemoryPressureSnapshot describes the native eight-word read-only copy ABI.
// Reason is a bitset: capacity=1, committed=2, exhaustion forecast=4,
// low reclamation=8, system=16. This declaration does not claim a concurrent
// native publication protocol; the current runtime has one mutator thread.
type MemoryPressureSnapshot struct {
	Level, Reason                      uint64
	HeapUsed, HeapCommitted, HeapLimit uint64
	AllocationRate, GCActive, Sequence uint64
}
