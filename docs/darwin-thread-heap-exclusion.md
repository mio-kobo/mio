# Darwin ARM64 threads: why heap access is slow, and the options

Status: analysis with measurements, for a maintainer decision. No runtime change is made by the PR that carries this file.

## Measurements (macOS ARM64 runner, hosted `macos-15`, 3 cores)

All numbers come from non-required diagnostics (the `darwin-thread-bench` check and temporary allocation probes) and are one runner's.

| 2,000,000 small allocations, one thread | time |
|---|---|
| ordinary program (no thread bridge) | 0.24 to 0.27 s (about 0.06 us per allocated object) |
| the same loop built as a thread build | 3.3 to 3.6 s |

| benchmark in a thread build | macOS ARM64 | Linux x86-64 |
|---|---|---|
| 1 thread, 2M allocations | 4.9 to 6.1 s | 0.06 s |
| 2 threads, 4M allocations | 150 to 200 s | 0.10 s |
| 4 threads, 8M allocations | 660 to 790 s | 0.20 s |
| 4 producers and 4 consumers, 400000 channel operations | 490 to 1270 s | 0.25 s |

Collections are not the cause. The runtime's own counters (`--observe`) for the plain loop report 18 minor collections for 4,000,000 allocated objects, a heap goal of 58 MB and 15 ms of total collection time.

## Cause

In a thread build the lowering (`bunko/kiso/aot/lower/lower.mio`, the "opt-in cooperative path") emits, at every solved opcode boundary, a safepoint poll (`attached_roots_poll`), and before every heap-touching opcode (`opcode >= 30`, 19 and 23) a call of `attached_heap_enter`, which is `gc_heap_enter`: a full collector epoch. `gc_begin` takes the registry lock, publishes an owner and an epoch and waits until every other thread has reached a poll and parked on its condition variable; the next poll of the owner ends the epoch (`gc_end`: unlock, broadcast to every thread's condition). So the Darwin thread bridge takes a global stop-the-world lock around every heap access, parking and waking all other threads each time.

* One thread: no thread to park, but the registry lock, the epoch bookkeeping, a root snapshot per allocation and a clock read per wait loop remain: 0.8 us per allocation instead of 0.06 us.
* Several threads: each epoch parks and wakes every other thread through the kernel: about 100 us per heap access, and the cost grows with the number of threads. A stack sample of four allocating threads shows about 60 percent of each worker's time in the condition wait, 15 percent in the broadcast and about 28 percent in clock reads inside those loops.

Smaller issues found on the way, already fixed: a 30 second ceiling on `join` (#108), one second limits in the handshake and the registry lock (#109), a sleep-polling channel wait with no wake-up (the 1.3 ms ping-pong round trip, #107).

## Why it is built this way

The cooperative bridge (M19ef) was written so that the old concurrent collector protocol and the generational runtime, both designed for one mutator, stay correct with several: the mutator is in an epoch, hence alone in the heap, whenever it touches the heap. The generational runtime shares mutable metadata (free bins, young-object list, remembered set, memory counters) that every allocation and every pointer store may update, so exclusion is real, not decorative.

## Options

A. Global heap lock with a fast uncontended path, stop-the-world only for collections. `heap_enter` acquires a heap lock (a futex mutex) instead of opening an epoch; the poll releases it when another thread is waiting; a thread blocked on the lock sits in the blocked region (as a blocked `write` does), so a collection, which still uses the existing epoch, does not wait for it. Expected cost per heap opcode: tens of nanoseconds uncontended. Risks: it relies on every heap access of every thread being inside an opcode the lowering classifies as heap-touching (the old design did not rely on that, because a full stop also froze the other threads' non-heap opcodes); the interaction with the old collector protocol (`old_mark_proto`) must stay on the epoch path; it changes the concurrency protocol of the GC and can only be validated on the macOS runner.

B. Make the shared runtime structures thread-safe and drop `heap_enter` in public thread builds. Allocation under a lock, an atomic remembered set, per-thread young lists. Larger change, touches the generational runtime, highest potential (parallel heap access), highest risk.

C. Skip the epoch when only one thread is registered. Safe (a thread can only be created by a running thread, and a child that registers late takes the epoch on its first heap access) and about three times faster for the single-thread phases of a thread program, but no help for programs with several live threads.

D. Keep the design and document the limit: thread programs on macOS ARM64 are correct but heap-bound work does not scale; use channels sparingly and prefer coarse work items.

Recommendation: C first (small, safe), then A with the safety checks above, validated by the existing required thread programs, the stress variants and a new multi-thread allocation test. B only if A is not enough.

## What this PR contains

The analysis only, plus a lighter variant of the allocation test (`alloc_cache_light.mio`, 20000 iterations instead of 400000) that fits the macOS allocation speed and joins the required macOS thread programs. The diagnostic programs and checks that produced the measurements were removed again so that they do not keep costing macOS runner time; the numbers above are the record.
