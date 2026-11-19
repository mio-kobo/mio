# Darwin ARM64 threads: heap exclusion and stop-the-world collection

Status: implementation-ready design plus the one change that has landed with it (narrower Epoch classification, section 3). Nothing in sections 4-6 is implemented. The numbers in section 1 come from static analysis of emitted images and from the logs of one historical macOS run; no native macOS ARM64 measurement was taken for this document (the host was Linux x86-64).

## 1. What the cost is

The Darwin thread build ("worker bridge", `bunko/kiso/aot/lower/worker_roots.mio`, `lower.mio`) polls at every solved opcode boundary and, before every opcode classified as a heap step, calls `attached_heap_enter` -> `gc_heap_enter` -> `gc_begin`. Three different things hide behind that call and must not be confused:

| Term | What happens | Cost class |
| --- | --- | --- |
| Epoch entry (heap exclusion) | `gc_begin`: take the registry lock, bump the registry epoch, publish `owner`/`phase=handshake`, then loop over every registered thread (`trylock` of its mutex, read `progress`/`ack_epoch`) until all are acknowledged, then `phase=stopped`. The owner keeps the Epoch until its next poll (`gc_mutator_poll` -> `gc_end`: clear owner, wake every thread's condition variable, unlock registry). | per heap opcode, O(threads), kernel wake-ups |
| Roots snapshot | `gc_scan_roots` walks every acknowledged thread's published shadow frames into `ctx+wr_snapshot` (4000 entries). Done by `runtime_alloc` and `attached_worker_collect`, not by plain heap steps. | per allocation |
| Collection | `gc_collect_stopped` (minor/major) inside an already stopped Epoch. | per collection |

A low collection count therefore says nothing about Epoch cost: a program can enter millions of Epochs and collect once.

Historical evidence (macOS 15 runner, run 43438227070, 3 cores, mpmc_long, stack sample 25 s after start, symbolized against an image rebuilt from the identical tree with `--emit-symbols`): the top-of-stack distribution was 27.9 % `native_word_wait_spin`, 26 % `__psynch_cvwait`, 14.6 % `mach_absolute_time`, 7.9 % `__psynch_cvbroad`, then `attached_gc_poll`, `attached_platform_now`, `attached_registry_lock_retry`, `pthread_mutex_trylock`, `attached_gc_begin_member`. That is the signature of the Epoch protocol (condition-variable parking, broadcast on `gc_end`, clock reads in the owner's retry loop and the park deadline, registry lock retries), not of a stopped system. It does **not** prove a rate, a starvation pattern or the absence of a lost wake-up: no per-worker counters existed. The `mpmc_probe` phase (benchmarks/threads/efficiency.mio) was added to produce exactly those facts on a real Mac.

Static cost per message (before this change, `tests/threads/mpmc_long.mio` for darwin-arm64): 148 heap-entering call sites in 582 polled opcodes; `send` 29 of 103, `receive` 35 of 111, plus `lock`/`unlock`/`wake_*`/`sleep_on` on both sides. Linux x86-64 runs the same workload in 215 ms (Linux uses a heap lock, not per-opcode Epochs).

## 2. Current states and ownership (as implemented)

Per thread (`gc/threads.mio`): `thread_progress` in {`at_work`=0, `parked`=1 (inside a blocked region: native wait, join, ulock), `finished`=2, `gc_parked`=3 (acknowledged an Epoch at a poll)}; `ack_epoch`, `seen_epoch`; `thread_shadow` (published root frame chain); `thread_mutex`/`thread_condition`.

Registry (`gc/safepoints.mio`): `epoch`, `owner` (0 = none), `phase` (`handshake`=1, `stopped`=2), `heartbeat`, member list, registry lock.

Heap ownership by state:

| State | Who may touch the heap |
| --- | --- |
| `owner == 0` | Every thread, but only through opcodes that need no exclusion (they run between two polls) |
| `owner == T`, phase handshake | T waits for acknowledgements; other threads park at their next poll (`gc_poll`) or are already `parked`/`finished` |
| `owner == T`, phase stopped | T alone: allocation, initialization, publication, snapshot, collection. Everything else is parked. T keeps it until its next poll |

Lock order today: registry lock is taken only by the Epoch owner (and thread registration); the owner takes thread mutexes only with `trylock` (never blocks), a thread takes only its own mutex (`gc_poll`, `gc_block_enter/leave`). There is no cycle: nobody blocks on a second lock while holding one. An owner stuck behind an unscheduled thread retries for up to 120 s then aborts the process; a parked thread aborts after 600 s without an owner heartbeat.

Roots: published by `attached_roots_publish` (`emit_shadow_push`) in front of every opcode, before its poll, and valid until the next push. A parked or blocked thread's roots stay valid because its frame chain is not touched while it is acknowledged. The heap is non-moving.

Why an opcode body without its own Epoch is safe today: a thread acknowledges an Epoch only at the poll that precedes an opcode, and `gc_begin` returns only after every non-blocked thread has acknowledged. So an Epoch-free body never overlaps an Epoch owner, a collection, or any other Epoch-owning mutation; it can overlap only other Epoch-free bodies. ARM64 ordering: the acknowledgement is a release store (`stlr`) after the body's loads and stores in program order; the owner reads it with acquire; resumption goes through `gc_park_locked` under the thread mutex.

## 3. Landed: narrower Epoch classification (`heap_step_exempt`, lower.mio)

Exempted from the pre-opcode Epoch in every worker build (the Safepoint poll and shadow-root push stay for all opcodes):

* 19/23/49 with a non-nullable integer/float/bool target in a global slot or a non-captured local: one plain store into `__DATA` or the thread's own frame. Reference, nullable (boxing allocates) and captured-cell targets keep the Epoch. (In `old_sweep` builds 49 keeps its Epoch: that mode retains its previous scalar-store rule.)
* 32, 34, 37, 39, 40, 47, 60 and 31 (when the element is not boxed): loads only. (The old-sweep mode already used this list under its own assumptions; here it is justified by the argument in section 2, and by each lowering: `rt_get_field`, enum tag/payload, upvalue cell load, array length/capacity/element loads, all ending in `runtime_error` on failure.)
* 50-53, 55: register arithmetic. 66: emits no code.

Allocation is never exempted: `runtime_alloc` takes its own Epoch. Not changed: 24 (already not a heap step), 25 calls (already not), 30/33/35/36/38/41-46/48/54/56-65/67.

Effect (static call sites, cross-built darwin-arm64 images): mpmc_long 148 -> 22, pingpong_long 130 -> 16, channel_races 201 -> 19. Dynamic Epoch counts per message are NOT measured (native Darwin was not available). `tests/threads/heap_epochs.py` pins the classification (zero sites in a scalar loop and a read loop; sites kept for reference stores, captured cells, nullable boxing), `tests/threads/heap_epochs.mio` runs the same functions beside Epoch-owning mutators with a sequential cross-check.

**Constraint this places on section 4:** the argument "no Epoch-free body overlaps a mutation" holds only while every mutation is a full stop-the-world Epoch. The heap-lock protocol below breaks it for array reads.

## 4. Proposal: ordinary heap exclusion separate from collection

Goal: protected heap operations (allocation fast path, initialization, stores with barriers, push) take a cheap lock instead of a full-thread Epoch; a real collection still runs the full Epoch.

### 4.1 State

Add `heap_lock` (one word in the registry block; 0 free, otherwise the owner context pointer, bit 0 = waiters). Keep `owner/phase/epoch` exactly as they are: they now describe collection only.

Thread states: add no new `progress` values. A thread waiting for `heap_lock` is `parked` (blocked region), which `gc_begin` already counts as acknowledged.

### 4.2 Operations

`heap_enter` (replaces `gc_heap_enter` for plain heap steps):
1. Fast path: `ldaxr/stlxr` CAS `heap_lock` 0 -> self. Success: done (no registry lock, no epoch bump, no acknowledgement loop).
2. Slow path: shadow roots are already published by the preceding push; call `gc_block_enter` (progress = `parked`), `ulock_wait` on `heap_lock` until it can be taken (CAS), then `gc_block_leave`.
3. Re-check: after `gc_block_leave` returns no collection can be active, because collection requires `heap_lock` (4.3) and this thread holds it. Operands are on the shadow-rooted operand stack and the heap is non-moving, so nothing cached is stale; the opcode body then pops its operands as usual.

`heap_leave`: at the next poll (same release point as today): clear `heap_lock` with `stlr`; if the waiter bit is set, `ulock_wake` one waiter.

### 4.3 Allocation failure and collection

The allocator slow path runs with `heap_lock` held. It then calls `gc_begin` unchanged **while holding `heap_lock`** ("collector owns heap_lock"): other threads are either parked on `heap_lock` (blocked region, already acknowledged), blocked in a native wait, or running Epoch-free opcodes (acknowledge at their next poll). `gc_scan_roots`, `gc_collect_stopped`, `gc_end` are unchanged; `gc_end` does not release `heap_lock`; the next poll's `heap_leave` does.
A thread that wants an Epoch for another reason (`attached_worker_collect`, `old_*` control) first takes `heap_lock` the same way.

### 4.4 Lock order

`heap_lock` -> registry lock -> (thread mutex by `trylock` only). A thread holding a thread mutex never waits for `heap_lock` or the registry lock. `gc_block_enter/leave` take only the thread's own mutex and are called with no other lock held except in the case "waiting for `heap_lock`" where nothing is held. The `heap_lock` holder never blocks on anything that depends on a thread waiting for `heap_lock` (waiters are `parked`).

### 4.5 Initialization, publication, registration, exit

* Initialization/publication: unchanged rule: an object is initialized and published inside one `heap_lock` hold; collection needs `heap_lock`, so the collector never sees a partially initialized object.
* Registration (`thread_attach`/`thread_enter`): registers under the registry lock as today; a child that has not registered is not in the member list, so it needs no acknowledgement, and it cannot touch the heap before `thread_enter` returns (it takes `heap_lock` like anyone). The parent stays in a blocked region in `thread_wait_started` (60 s).
* Exit/failure/shutdown: a thread must release `heap_lock` before `gc_mutator_poll` in `thread_leave`; a faulting thread exits the whole process in public mode, so a lock held by a dead thread is not observable there. In test builds a failed thread must be treated as `finished` and `heap_lock` force-released by the cleanup path only after its owner is proven gone (not designed here; the first implementation should abort instead).

### 4.6 Modes

* old_mark_proto/old_sweep: keep full Epochs for every operation that calls `old_abort_before_mutation` or the sweep lock helpers until separately audited; select by the existing `old_*_enabled` predicates.
* GC stress: unchanged; a stress collection is an Epoch taken with `heap_lock` held.

### 4.7 What must be re-audited (unresolved invariants)

1. **Epoch-free reads (this is the blocker for keeping section 3 under the new protocol).** With `heap_lock`, a mutator runs concurrently with other threads' Epoch-free opcodes. Array length (+8) and payload pointer (+40) are two words that `push`/`reserve`/`truncate` replace: a concurrent index read can pair a new length with an old pointer. Reads 31/32/60 must either take `heap_lock` or use a validated read (re-read the length after the element). Struct fields are immutable and enum tag/payload are immutable after publication, upvalue cells are single words, so 34/37/39/40/47 stay Epoch-free. A published object must also be fully initialized before the store that publishes it (release store), which the present Epoch hides.
2. Free bins, young list, remembered set, large-object table and memory accounting: all are only touched under `heap_lock` or inside an Epoch; every non-opcode caller (`worker_heap_alloc`, `worker_sweep_*`, nativefs cleanup, channel `words_new/free`) must be audited for an access outside both.
3. `runtime_alloc_legacy` currently expects an Epoch-owned complete snapshot; the allocation fast path under `heap_lock` must either skip the snapshot (non-collecting path) or the slow path must escalate before taking it.
4. Fairness and the 120 s/600 s deadlines: `heap_lock` waiters sit in `parked`, so the owner deadline is not charged to them; add a bound on time spent waiting for `heap_lock` only if a hang diagnosis requires it.
5. Wake loss on `heap_lock`: the waiter bit / ulock compare must follow the same handshake as the channel waiter counts (announce with a full barrier, then compare-and-wait).

### 4.8 Regressions that detect violations

| Invariant | Detector |
| --- | --- |
| Epoch-free body never races a mutation | `tests/threads/heap_epochs.mio` (extend with concurrent array push + index readers), `channel_graphs`, `gc_stress`, normal and `--gc-stress` |
| Collector never sees a partial object | `gc_stress`, `alloc_cache`, `gc_lifecycle` under stress |
| Roots valid while waiting for `heap_lock` | `reclaim`, `captures`, `channel_lifecycle` |
| No lock cycle / lost wake | `creation_limit`, `channel_races`, `mpmc_long` (must complete), `pingpong_long` |
| Exit/failure while holding the lock | `shutdown_active`, `stack_overflow`, `invalid_handles` |

### 4.9 Limits, stated honestly

A single `heap_lock` still serializes every protected heap operation: the best case is the uncontended-CAS cost per heap opcode plus serialization of allocation-heavy threads. It removes the O(threads) acknowledgement loop, the registry lock and the broadcast wake-up from ordinary operations; it does not make heap access scale. Per-thread allocation caches (thread-local free lists, per-thread young lists, buffered remembered-set updates) are the next step, and are worth building only if, after the heap-lock change, a native profile still shows the allocator/`heap_lock` as the dominant cost with more than two allocating threads (`alloc_2`/`alloc_4` weak-scaling elapsed time still growing with workers).

### 4.10 Smallest next implementation step

Implement `heap_lock` with the section 4.2/4.3 protocol behind a build flag (default off), for scalar-free paths only: replace the `attached_heap_enter` call in `lower.mio` for opcodes 33/35/48/59/61/63/64 (stores/push) and keep full Epochs for allocation. Gate: darwin-thread-core/programs/faults green in normal and stress modes, then `mpmc_probe` and `alloc_*` phases compared on one Mac.

## 5. Experiment: single-registered-mutator fast path (not implemented)

Expected benefit: only single-mutator phases of thread-enabled programs (main before the first `start`, after the last `join`). It is not a multi-thread scaling fix. Required answers before any code:

* Count publication: `registrations - exits` is updated with `fetch_add` (acq_rel) under the registry lock; a fast path would read it with acquire. Reading 1 is not "one runnable thread": a created-but-unregistered child is invisible to the count. `thread_open_shared` marks the child `starting` and `thread_wait_started` blocks the parent until the child is `running`, so the fast path is safe only if it is switched off before `attached_worker_start` creates the child and stays off until the child has exited and been closed (`thread_close`).
* Nested starts: a child that starts a grandchild does the same; the count is 1 only for the thread that is the sole registered mutator, and a start by a registered thread is the transition 1 -> 2 and must run under the registry lock with the fast path disabled first.
* 1 -> n -> 1: disable by setting a `multi` word (release) before the child is created; re-enable only after `thread_close` of the last child; the transition back must observe the child's `exits` increment (acquire) and its published roots are irrelevant afterwards.
* Pending collection: the fast path must still observe `owner != 0` (another thread cannot exist, but `gc_end` of the thread's own previous Epoch must happen): keep the poll, skip only the acknowledgement loop and registry lock.
* Roots remain valid because the poll/push are unchanged; the real collection path still calls `gc_begin` and obtains ownership.
Not implemented here because it cannot be validated without native Darwin runs of the start/join, nested and shutdown suites.

## 7. Native measurements after the epoch classification, and the next step chosen

Run 43818508856 (head dc858dd), `mpmc_long` sampled 25 s in, symbolized: `native_word_wait_spin` 43.2 %, `__psynch_cvwait` 41.9 %, epoch frames about 10 %. Same-job phases (runs 43823333447, 43825301236): alloc_1 4.7-5.2 s, alloc_2 81.5-94 s (baseline over the 120 s budget), alloc_4 over the 120 s budget on both sides.

Two candidates remain; only one was taken, and why:

1. **Allocation epoch cost (alloc_2/alloc_4).** Every allocation is a full epoch (`runtime_alloc` -> `gc_heap_enter` -> roots snapshot) and with a second running thread each epoch costs a park/wake of that thread. Fixing it means changing heap ownership (sections 4.1-4.7). Sections 4.5/4.7 still list unresolved invariants (array reads under a heap lock, non-opcode heap callers `worker_heap_alloc`/`worker_sweep_*`/nativefs, the snapshot in a fast path, `heap_lock` wake loss) that cannot be bounded and validated by a one-commit change, and no profile of alloc_2 existed. Not taken. The phase driver now samples alloc_2/alloc_4 (`--sample-after`, `--symbols`) so the next decision rests on a measured profile.
2. **Channel word_wait spin.** Measured at 43 % of all samples under oversubscription (9 channel threads, 3 cores). The spin does not poll a safepoint, so a collector that needs this thread's acknowledgement also waits for it. Taken as a controlled experiment: the per-call budget drops from 4000 to 128 reads (`worker_roots.mio`). It changes no heap access, no ordering and no handshake: the waiter-count announce (`word_add` with `dmb ish`), the compare-and-wait in the kernel, the GC-safe blocked region (`attached_native_enter/leave`) and the predicate re-checks are untouched. The budget belongs to one call, never spans an epoch change, and nothing resembling the #113 collector-park spin is added. If CPU time or elapsed time does not improve in the same-job alternating comparison, the commit is reverted by a normal follow-up commit and the evidence is kept.

### 7.1 Result of the experiment (run 43837173035, head f86c362, same job, 2 alternating repetitions, baseline = 21cfc1a)

| workload | baseline wall / CPU (user+system) | candidate wall / CPU | state |
| --- | --- | --- | --- |
| mpmc_long (original, 120 s watchdog) | 83.9 s, 79.8 s / 119.7 s, 115.0 s | 78.7 s, 79.0 s / 98.2 s, 98.6 s | all COMPLETE, output 200000/200000 |
| mpmc_probe, messages in 10 s | 24.3k, 29.4k | 27.9k, 24.9k | within noise |
| pingpong | 1.22 s, 1.53 s | 0.99 s, 1.47 s | within noise |
| alloc_1 / alloc_s1 | 5.5 s, 5.2 s / 9.4 s, 10.0 s | 5.1 s, 5.2 s / 9.3 s, 9.8 s | unchanged |
| alloc_2 (= strong 2 threads) | 87.3 s, 83.9 s | 84.0 s, 108.1 s | noise; both COMPLETE |
| alloc_4 / alloc_s4 | PARTIAL 120 s (CPU 230 s) | PARTIAL 120 s (CPU 189-224 s) | unchanged, budget exceeded |

Reading: the shorter spin does not raise throughput on the 3-core runner (the epoch protocol, not the spin, bounds it); it lowers CPU time of mpmc_long by about 16 % for the same wall time. Two repetitions per cell: treat the CPU figure as indicative. Not measured: a many-core Mac (the M4 Pro), where a short spin could cost latency; run `pingpong` and `mpmc_probe` there before keeping the value.

### 7.2 What the allocation profile says (samples taken 25 s into alloc_2 and alloc_4, symbolized)

alloc_2: `__psynch_cvwait` 67.1 %, `mach_absolute_time` 9.9 %, `__psynch_cvbroad` 4.3 %, `attached_platform_trylock` 3.5 %, `attached_gc_begin_member` 3.1 %, `pthread_mutex_trylock` 2.7 %. alloc_4: 67.0 %, 9.5 %, 5.9 %, 2.4 %, 2.3 %. No allocator or collector frame appears: the time is the epoch handshake itself (threads parked in `gc_park_locked`, wake broadcast in `gc_end`, clock reads in the owner's acknowledgement loop and the park deadline). Strong scaling confirms it: 4000000 iterations take 9.3 s on one thread, 84 s on two and more than 120 s on four, so the cost per allocation grows with the number of threads.

Smallest next step that does not yet need the full heap-lock protocol: **epoch retention across consecutive heap steps of one thread.** Today a heap opcode's epoch is released by the poll in front of the next opcode (`gc_end`, broadcast) and the next heap opcode immediately re-acquires it (`gc_begin`, acknowledgement loop, every other thread wakes and parks again). Keeping the epoch across a straight-line run of heap opcodes removes those release/acquire pairs. It would need: a bounded run length and release at every back edge, call and block boundary (fairness and collector progress), release before any blocking native (`gc_block_enter` already starts with `gc_mutator_poll`), unchanged root publication per opcode, and a proof that no opcode between the two polls reads state that another thread could have changed (it holds the epoch, so none can). It removes at most the redundant pairs, not the O(threads) cost of the first acquisition; the heap-lock protocol of section 4 is still needed for scaling.

## 9. Audit of the four heap-lock invariants (exact paths), the Linux precedent and the regressions

### 9.0 A precedent that was missing from sections 4-5: the Linux x86-64 backend already separates a heap lock from collection and has per-thread allocation caches
`bunko/kiso/aot/lowerlinux/mutators.mio` (header comment lines 4-27, `mut_lock` line 34, `rec_cache` lines 62-66) and `docs/concurrency.md` (lines 470-495, measurements) describe a shipped design: a spinlock word (own cache line) serialises `rt.alloc` and registry changes; runtime routines **never collect** while holding it (a thread is never parked holding it); collection is requested at a poll when the allocation volume word `mut_want` says so, by the thread that wins the stop flag, which waits until no other mutator is running; allocation of 24-56 bytes pops from five per-thread free lists with no lock and no shared write, refilled with 64 blocks from one locked allocation; cache blocks are ordinary free blocks (valid header, mark word 0) so a sweep reclaims them, and the collector empties every thread's lists before resuming the world. The report of 2026-11-16 (Phase B/C "no reference") was wrong about that; the Darwin phases are a port with these differences:
* Darwin's allocator may collect **inside** the allocation routine: `emit_runtime_alloc` (`lower.mio:1923`), retry loop `lower.mio:1986-1991` (`runtime_collect`, `gen_major`, `memory_grow`). Linux never does. A heap lock on Darwin therefore needs either the section 4.3 escalation (the allocation slow path takes a full epoch while holding the heap lock) or the Linux rule (allocation only sets a `want` flag; collection at the next poll) and a policy for allocation that cannot wait.
* Darwin polls at every opcode and publishes shadow roots per opcode; Linux polls at function entry and loop back-edges and publishes `rbp` chains. Root publication for a thread blocked on the heap lock is therefore already in place on Darwin (`emit_shadow_push` before the poll, `lower.mio:1232`).
* Darwin has a generational collector with a write barrier and a young list (`generational.mio:29,64`); Linux's sweep is non-generational. The young-list head `[x23+1048]` and byte counter `[x23+1112]` (`generational.mio:64-80`) are written by every allocation and must be under the heap lock or per-thread.

### 9.1 Invariant 1: array reads against growth
Paths: length `[arr+8]` and payload `[arr+40]` read by `emit_array_element` (`lower.mio:259-266`), `emit_len` (`:229`), capacity (opcode 60, `lower.mio:1592`); written by `runtime_array_push` (`lower.mio:2168`), reserve/truncate/append_all (opcodes 61/63/64, `lower.mio:1583`).
Finding: the tear needs two threads on the **same** mutable array. By the language's own race model (`docs/concurrency.md:25-35`) that is user-level shared mutable state. The only library-level sharing is `Channel.ring` (`thread.mio:33`): fixed capacity, never grown, accessed under the channel lock (`thread.mio:96-119`). User-level sharing is not prevented by the compiler in the current tree: boundary rule D-5 (thread entries may only capture immutable data) is "recommended; DECISION NEEDED" (`docs/concurrency.md:211-214`) and a search of `internal/` and `selfhost/` finds no checker for it (`TouchesGlobal` does not occur). Linux x86-64 has exactly the same exposure and runs without read locks.
Status: **resolved for library and ownership-respecting programs, open as a decision for racy user programs.** Today Darwin is *accidentally safer* than Linux: a racy read cannot overlap a mutation because mutations are epochs. A heap lock would give that up. Options for the maintainers: (a) accept Linux parity and document it; (b) enforce D-5 in the checker (then (a) becomes a guarantee); (c) keep reads 31/32/60 inside the lock (costs every array read) or use a validated read. Recommendation: (a) now, (b) as a separate PR; (c) only if (b) is rejected. The heap-lock commit must not land before this decision is recorded.

### 9.2 Invariant 2: every heap access outside an opcode body
Enumerated (all current callers, Darwin worker build):
| caller | path | protection today | under a heap lock |
| --- | --- | --- | --- |
| opcode heap steps | `attached_heap_enter` (`lower.mio:1247`), classification `heap_step_exempt` (`:675`) | epoch | take the heap lock at the same sites (the exempt list contains no barrier, no allocation, no metadata write: section 3) |
| allocation | `runtime_alloc` (`worker_roots.mio` runtime_alloc wrapper) -> `runtime_alloc_legacy` (`lower.mio:1923`) | epoch kept until the next poll | heap lock, with escalation to an epoch on collection |
| collection/allocation callbacks | `worker_heap_collect`, `worker_heap_alloc` (`worker_roots.mio:301-302`), IR `gc_collect_stopped`/`gc_allocate_stopped` (`protocol.mio`) | require phase `stopped` (`gc_scan_roots` refuses otherwise, `safepoints.mio:85`) | unchanged: only callable from an epoch |
| write barrier / remembered set | `emit_gen_remember` (`generational.mio:29`), executed in opcode bodies of heap steps (33, 35, 48, cell stores) | epoch | same sites as heap steps |
| memory accounting | `memory_sample`, `memory_grow` (called from the allocator only) | epoch | inside the allocator lock |
| channel control words | `native_words_new` (`worker_roots.mio:77-79`, `ldxr/stxr` counter), word get/set/cas/add | off-heap, lock-free | none needed (not heap) |
| thread start/join | `attached_worker_start/join` (`worker_roots.mio:164,184`), claim words off-heap; entry closure rooted by the bootstrap root | off-heap + roots | none (regression: `captures`) |
| file/socket tables | `nativefs_cleanup` (`nativefs.mio:416-420`) | table in the OS-state area, not managed heap (comment `nativefs.mio:426-427`); closes descriptors | none for the heap; the table itself is not synchronized across threads today (a separate pre-existing question, not a heap-lock prerequisite) |
Status: **resolved on paper**: a heap-lock protocol that guards exactly the heap-step sites and the allocator covers every caller; the single obligation is that no new exempt opcode may write barrier/metadata state (enforced by the static check `heap_epochs.py`).

### 9.3 Invariant 3: the root snapshot in an allocation fast path
`runtime_alloc` takes a complete snapshot (`gc_scan_roots` into `ctx+wr_snapshot`, capacity 4000 entries, `worker_roots.mio` snapshot code and `gc_snapshot_root` overflow check) **before** calling the allocator because the legacy allocator may collect. A lock-only fast path must not take it. Resolution: split `runtime_alloc_legacy` at the point where it decides to collect (`lower.mio:1986`): the non-collecting part (free-list/bump, `gen_register`) runs under the heap lock without a snapshot; the collecting retry (`runtime_collect`/`gen_major`/`memory_grow`) is reached only after escalating to an epoch and taking the snapshot. This is a code change to a shared allocator routine (single-threaded images use the same routine): status **designed, not implemented**; needs its own byte-identity gate for non-thread images.

### 9.4 Invariant 4: wake loss on the heap lock
Waiters block in a GC-recognized state (`gc_block_enter`, which begins with a poll and publishes `thread_parked`) and wait on the lock word with the same pattern as the channel (announce, full barrier, compare-and-wait with the observed value, re-check): `thread.mio:67-72` documents the handshake and `native_word_wait` (`worker_roots.mio:71-80`) implements the kernel compare-and-wait. Reuse `native_word_wait`/`native_word_wake` unchanged on a new word; do not write a new primitive. The proof obligations are exactly those already proved for the channel waiter counts (sequence word + waiter count with `dmb ish`), and the lock hand-off must use a release store on unlock and an acquire on lock. Status: **resolved by reuse**; to be validated by `channel_races`/`creation_limit`-style oversubscription runs once implemented.

### 9.5 Regressions that exist now (they pass with epochs and must keep passing under any heap lock)
`tests/threads/heap_invariants.mio` (four workers plus main, normal and `--gc-stress`, sequential cross-check): A array growth to 4000 elements with interleaved reads and `len`, transfer of the grown array through a channel, growth and reads by the receiver after the transfer; B closure-captured variable replaced with fresh arrays (reference store into a cell, write barrier) between allocations; C nullable scalar (boxing allocation) and nullable struct reference assignment in a loop; D objects built from nested allocating expressions (collection while the outer object is incomplete). Existing: `channel_graphs` (publication of shared sub-objects, cycles, 2000-deep chain, 20000-node tree), `epoch_retention`, `heap_epochs`, `captures`, `gc_stress`, `gc_lifecycle`, `alloc_cache`.
Not constructible as a regression today: a racing read/growth of one shared array (it is a user-level data race with undefined results by the language's own model, 9.1); it becomes testable only after decision 9.1 is taken.

### 9.6 What is ready and what is not
Ready: 9.2 (callers), 9.4 (wake handshake by reuse), regressions 9.5, observation path (section 10). Blocked on a decision: 9.1. Blocked on a code change with its own gates: 9.3. Therefore the heap-lock protocol is **not** implemented; the smallest next prerequisite is the maintainers' decision on 9.1 (recommended: Linux parity now, D-5 enforcement as a separate PR), then the 9.3 allocator split behind a byte-identity check.

## 10. Observation path (`mio build --observe` for thread images)

Opt-in, diagnostic, never for timing. Normal images are byte-identical with and without this code (31 of 31 thread/benchmark programs compared, cross-built). `--observe` on a thread program now builds `compile_with_public_threads_observed` (`lower.mio`): the existing single-threaded heap observation record (collections, GC pause histogram in CNTVCT ticks, live/peak bytes, allocated objects; `mio-gc name=value` on stderr) plus, from `worker_roots.mio` (`wr_obs_begin/end`, `emit_worker_observation_dump`), `mio-thread name=value` lines printed once by the main thread after all threads were joined: `epochs_entered` (the registry epoch word, incremented by every `gc_begin`), `safepoint_polls`, `runtime_protocol_calls`, `blocking_native_entries`, `retained_polls_taken`, and for each of three calls (`heap_enter` = epoch acquisition, `poll` = release/park, `block_leave` = return from a blocking native) nine elapsed-time buckets (upper bounds 1us, 4us, 16us, 64us, 256us, 1ms, 4ms, 16ms, inf), the total and the maximum in ns. Percentiles are reported as the upper bound of the bucket that contains them (not interpolated).
Storage: 272 bytes in the **unused tail** of each thread context (bytes 41032..41303 of the 41600-byte stride; the snapshot ends at 41016: layout audit in the comment at `wr_obs` in `worker_roots.mio`). No context size, offset, ABI record or heap layout changes; counters are written only by their owning thread. `MIO_OBSERVE_NO_RETENTION=1` builds the same observed image without epoch retention, so epoch counts with and without retention come from one compiler. The driver (`scripts/ci/thread_bench.py --observe`, CI check `darwin-thread-observe`, non-required) also records per-run peak RSS and voluntary/involuntary context switches from `/usr/bin/time -l` (in all runs; instrumentation-free) and keeps the raw stderr of every run.
Limits (stated, not hidden): the three timers add two clock reads per call (instrumented runs only; timing is never taken from them); a bucket counts the whole wrapper call (a few hundred ns of its own); no allocation fast-path hit rate or refill count exists because there is no allocation cache yet; global lock contention is not measured because there is no heap lock yet.

## 6. Validation status

See DARWIN_THREADS_REPORT.md of the task (native Darwin: NOT_RUN). Commands to run on an Apple M4 Pro are listed there; the benchmark driver is `scripts/ci/thread_bench.py`.

## 8. Bounded epoch retention (implemented: lower.mio `epoch_breaker`/`jump_targets`/`retain_limit`, worker_roots.mio `attached_roots_retain`)

### 8.1 What changes
Before: every opcode starts with `attached_roots_poll` -> `gc_mutator_poll`, which ends the thread's own epoch when it owns one (`gc_end`: clear owner, broadcast to every thread, unlock the registry), and a heap opcode then re-enters one (`attached_heap_enter` -> `gc_begin`: registry lock, new epoch, acknowledgement loop). Two consecutive heap opcodes (for example the array allocation and the store of the new array into a local) therefore cost one release and one acquisition, each a full-thread handshake.
After: the compiler tracks a static state (`held`, `run`) through each straight-line run. An opcode that follows a heap step in the same run calls `attached_roots_retain` instead of `attached_roots_poll`. `attached_roots_retain` reads the registry owner word (`ldar`); if it is this thread it only republishes the shadow-root head (`stlr` to `thread_shadow`, the same release store `gc_mutator_poll` does first) and returns; otherwise it calls `gc_mutator_poll` unchanged. `attached_heap_enter` is still emitted for heap opcodes and is a no-op when the epoch is still owned (`gc_heap_enter` already tests `owner == self`).
The solved Safepoint poll point, the shadow-root push/pop and the heap-step classification are unchanged for every opcode.

### 8.2 Run boundaries (static)
A run ends, so that the next poll is the ordinary one that releases the epoch, at: any branch target (a `jump_targets` prepass over opcodes 17/18: loop heads and join points are never entered retained, so every loop iteration passes through at least one releasing poll); the opcodes 17, 18 (branch), 25, 45, 67 (calls: the callee runs arbitrary code, may block or run long), 26, 27 (returns), 22 (print: a `write` can block); and after `retain_limit` = 16 retained opcodes. The breaker opcode itself uses the ordinary poll, so it never executes while holding an epoch that this run retained. Function entry (offset 0) starts with `held = false`; a run never crosses a function boundary. Not applied in `old_mark_proto`/`old_sweep` builds (their control paths call `old_abort_before_mutation` and the sweep lock helpers per opcode).

### 8.3 States, ownership, lock order
Unchanged from section 2: owner word, phase, per-thread progress and acknowledgement. The retaining thread keeps `owner == self`, phase `stopped` and the registry lock (held by the owner from `gc_begin` to `gc_end`) for at most 16 additional opcodes; no new lock, no new state, no change to the lock order (registry lock, thread mutexes by `trylock` only). A thread that wants an epoch while another thread retains one parks in `gc_poll` exactly as it did while the owner was working, and resumes at the owner's releasing poll (bounded by the run limit plus the owner's own work); a thread leaving a blocked region waits in `gc_park_locked` the same way. Nothing waits for the retaining thread except through that existing path.

### 8.4 Roots, initialization, allocation failure
* Roots: every opcode still runs `emit_shadow_push` before its poll/retain and `emit_shadow_pop` after the enter; `attached_roots_retain` publishes the new head (`thread_shadow`) with a release store, and an allocation in a later opcode of the run scans the owner's frames through the same head, so the temporaries that the compiler pins for that opcode are exactly those it pinned before (`gc_scan_roots` requires phase `stopped`, which still holds).
* Initialization/publication: an object is initialized inside the opcode body that allocates it; it becomes visible to other threads only after the epoch is released, and the release (`gc_end`, release stores) is now later, never earlier. A collection triggered by a later opcode of the run sees only completed opcodes' objects (the operand stack of the allocating opcode is published as for any opcode).
* Allocation failure/collection: `runtime_alloc` already nests inside an owned epoch (the opcode's own `heap_enter` precedes it); retention only makes the epoch older. `gc_collect_stopped` runs inside the same stopped epoch.
* Registration/exit: a child registers before it executes Mio code and the parent is in a blocked region; a thread leaves through `thread_leave` after its last opcode. A run cannot span either (calls and returns end runs, function start resets).
* Blocking: no retained opcode blocks. If a native blocking path ran inside a retained opcode it would pass `gc_block_enter`, which begins with `gc_mutator_poll` and therefore releases the epoch; `attached_roots_retain` re-checks the owner word on every use, so the static state is only an optimization hint and a stale hint falls back to the ordinary poll.

### 8.5 Fairness and responsiveness
Hold time per acquisition grows from one opcode to at most 17 opcodes (the first plus 16 retained), all of them non-blocking, call-free straight-line code. A pending epoch request or collection waits at most that long longer than before and is answered at the next releasing poll; loops always contain a branch (a breaker), so a non-allocating loop still polls and releases every iteration.

### 8.6 What it does not fix
It removes the release/acquire pairs inside a run (static: in the epoch-retention test program 51 of the 86 polls of the loop function become retained; mpmc_long 38 of 582). It does not remove the first acquisition of each run, which is still an O(threads) handshake; alloc scaling beyond two threads needs section 4. Expected effect: fewer `gc_end` broadcasts and `gc_begin` acknowledgement rounds per allocation-heavy iteration (the allocation loops: two epochs per iteration to one). Measured effect: see the report; unproven until the native A/B run.

### 8.7 Regressions
`tests/threads/epoch_retention.mio` (four threads and the main thread running a 14-allocation straight-line chain whose body exceeds the run limit; compared with the sequential result; in `darwin-thread-programs`, normal and `--gc-stress`, required), `tests/threads/heap_epochs.py` (static: no retained polls without an epoch, retained polls present in an allocation+store run, polls never removed), the existing gc_stress/gc_lifecycle/channel_*/creation_limit/reclaim programs.
Rollback conditions: any required Darwin thread check fails with the retention commit and passes without it; a hang or timeout in `darwin-thread-programs`; no CPU/wall improvement of alloc_2/alloc_s2-type phases in the alternating A/B.

## 11. Allocation lock (implemented: `runtime_alloc_try` in `lower.mio`, the `runtime_alloc` wrapper in `worker_roots.mio`)

### 11.1 Decision: this needs no maintainer decision, and why sections 9.1/9.3 stop blocking it
Sections 4 and 9 assumed that a heap lock *replaces* the Epoch for ordinary heap operations, which would let array reads overlap array growth (9.1, a semantic question about racy user programs) and needed the allocator to be split before it could run without a root snapshot (9.3). The implementation below is a different, narrower protocol, so neither is an obstacle:
* **Only allocation changes.** Every mutation of an existing object (array push/reserve/truncate/append, element and field stores with a barrier, captured-cell stores, map operations, closure capture) keeps its full Epoch. An array read therefore still cannot overlap a growth: the language semantics, the ownership rules and the racy-program behaviour are exactly what they are today (and section 9.1's "accidentally safer than Linux" property is preserved). The maintainers' decision in 9.1 is only needed for a future protocol that replaces Epochs for mutations; it is not needed here.
* **No collection under the lock.** The allocator is run in *try mode* (the same code, `x21 = 256`): every edge that would collect, grow the heap, map a large object or report out-of-memory returns 0 without side effects, and the caller falls back to the existing Epoch + root-snapshot + `runtime_alloc_legacy` path. So 9.3's split is realised inside the existing routine as a flag, not as a second allocator, and the collecting path is byte-for-byte the previous one.

### 11.2 The original PR #115 protocol

Section 13 describes the experimental contention-aware retry.
`runtime_alloc(size, type)` (wrapper, `worker_roots.mio`):
1. If the request is larger than 4095 bytes, or the lock was not obtained within a spin budget of 256 attempts: Epoch path (unchanged).
2. Acquire the lock word (`ldaxr`/`stlxr`, word at `wr_alloc_lock` in the tail of context 0), call `runtime_alloc_try`, release with `stlr`.
3. If the result is 0 (the free bins and the bump region were exhausted, or the memory budget would be exceeded, or the heap would need to grow): Epoch path (unchanged).
The result is stored in the saved-register frame exactly like the Epoch path's, so every caller (`emit_object`, array/closure/box allocation, `runtime_array_grow`, maps) is unchanged.

### 11.3 Why it is safe (all five obligations)
* **Mutual exclusion with collection and with every Epoch-owning operation.** An Epoch owner returns from `gc_begin` only after every registered thread that is not blocked has acknowledged at a poll, and a thread acknowledges only at the poll in front of an opcode. A thread inside the lock region is inside an opcode body: it has not acknowledged, so no Epoch owner exists while it runs. The region contains no poll, no blocking call and no collection (try mode), so it always finishes by itself. This is the same argument as for the Epoch-free scalar stores and reads (section 3) and is the only reason the lock can be so small: the lock orders lock holders among themselves, the Epoch protocol orders everything else.
* **Mutual exclusion among allocators.** One lock word; acquire/release (`ldaxr`/`stlr`) also publish the allocator metadata (free-bin heads, bump pointer, young list head and byte counter, large-object side table entry, memory accounting) from one holder to the next.
* **Roots.** Try mode never collects, so no snapshot is needed. A request that must collect leaves the lock region first and takes the unchanged path (complete snapshot under an Epoch). Callers already pin their live operands across `runtime_alloc` because the allocator can collect (shadow push before the call), so nothing about root publication changed.
* **No deadlock, no wake loss.** The lock is never held across a poll, a block, a thread-mutex or the registry lock, and a waiter holds nothing: there is no lock order to violate. Waiters spin (bounded) and then use the Epoch path, which cannot wait on the lock. There is therefore no sleeping primitive and no wake-up to lose (invariant 4 of section 9 disappears rather than being solved). A holder preempted by the OS delays spinners by at most their 256-attempt budget before they leave the fast path; an Epoch owner waiting for that holder's acknowledgement waits for its remaining straight-line allocation, exactly as it already waits for any opcode body.
* **Initialization and publication.** The fresh object is initialized by the opcode body after the lock is released and is reachable only from this thread's operand stack and frame until it is stored; a collection starts only after this thread's next poll, so it never sees a half-initialized object (the existing nested-root protocol for two-allocation constructors is unchanged: the second allocation can only collect through the Epoch path, which roots the first).

### 11.4 Classification changes that make the lock useful (`heap_step_exempt`, only when the lock exists)
The allocation itself no longer needs a per-opcode Epoch, so the opcodes whose only heap effect is allocation plus initialization of the fresh object lose it: 30 and 56 (array literal), 57 (array of n copies), 36 (struct literal), 38 (enum value). Their bodies (`emit_object`, `emit_array_construct`, `emit_array_operand`) contain no barrier, no remembered-set write and no store into a published object. The store of a reference into a **non-captured local** (23, and 49, the declaration store) is one `str` into this thread's own frame (`emit_local`); the barrier and cell code run only for captured locals. It is exempted for every type except nullable scalars (boxing) and unknown types. Not exempted: global stores (shared `__DATA` roots), captured cells, array/field/map stores, array mutations, closure creation (44/46) and clone (62). `tests/threads/heap_epochs.py` checks statically that `alloc_only` (struct/array literals bound to locals) has no heap-entering site while `ref_store`, `captured` and `nullable` keep theirs.

### 11.5 Where it is off
Images that are not Darwin cooperative thread builds are byte-identical to the previous head (checked: 12 non-thread darwin programs, and thread/non-thread linux-amd64 and windows-amd64 programs). In thread builds the lock is compiled out under `--gc-stress` (every allocation collects, so a lock-only path has no meaning), for the old-heap prototype and concurrent-sweep modes, and for the observation variant built with `MIO_OBSERVE_NO_RETENTION=1` (the lock follows the retention switch so that variant is the pre-change protocol).

### 11.6 Observation (`mio build --observe`, thread images)
New `mio-thread` counters: `alloc_lock_fast` (allocations served under the lock), `alloc_epoch_path` (requests that took the Epoch path: large, lock budget exhausted, or collection needed; in a build without the lock this counts every allocation), `alloc_lock_gave_up` (lock budget exhausted). Together with `epochs_entered` and the existing GC pause record they give the hit rate of the fast path and the remaining epochs per iteration.

### 11.7 Regression and measurement
`tests/threads/alloc_lock.mio` (four workers plus main, 300000 iterations each of struct + array-literal allocation with a rolling survivor window, array growth and a 700-element array every 5000 iterations that leaves the fast path; every total compared with the sequential value; Linux-x86-64 runs it too, GC-stress is skipped because the lock is compiled out there) and the existing `epoch_retention`, `heap_invariants`, `heap_epochs`, `gc_stress`, `gc_lifecycle`, `channel_*`. Native: `darwin-thread-programs` (normal and stress), `darwin-thread-phases` (same-job alternating A/B against head ebff537) and `darwin-thread-observe`.

## 12. Lock-stress validation image (test/instrumentation only)

### 12.1 The gap
`--gc-stress` makes the allocator collect at every allocation, which is incompatible with try mode (no collection under the lock), so section 11 compiled the lock out of gc-stress images. The existing gc-stress checks therefore never ran the lock fast path, and natural-GC benchmarks only collect when the heap goal is crossed (117 collections in 8 million allocations in `alloc_2`). Neither is stress coverage of the lock path.

### 12.2 Mechanism (selected only by `MIO_ALLOC_LOCK_STRESS=1` together with `--observe`; never by default)
The image is built with `gc_stress` = true *and* the lock, so the fallback path (the unchanged Epoch + snapshot + `runtime_alloc_legacy`) collects at every allocation it serves, and the lock path serves the rest:
* **Forced try-failure.** Under the lock, every fourth attempt (a counter in context 0's tail, touched only while holding the lock) releases the lock and takes the fallback without touching the heap. So about a quarter of lock-eligible allocations end in a real collection, the other three quarters run the real `runtime_alloc_try` concurrently with the other threads' collections and mutations.
* **Try mode never collects.** The stress collect in the allocator body is skipped when `x21 = 256` (`runtime_stress_try_skip`); the fallback (`x21 = 0`) collects as in every gc-stress image. Without the lock label these instructions are not emitted.
* **In-lock invariants** (acquire loads from the registry, checked before the allocator runs and again before the release; a failed check jumps to `runtime_error`): (1) `gc_registry_phase != gc_stopped` unless this thread is itself the registry owner (an epoch-owning opcode legitimately allocates through the fast path while everyone else is parked; the first native run showed this: invariant 1 without the owner exception aborted every image at startup), (2) no collection ran while the lock was held (the heap's collection counter, heap base + 16, is unchanged; status 60 on failure) and the registry epoch advanced by at most one, by another thread (`gc_begin` stores the epoch, then the phase, then the owner, and then waits for every non-blocked thread, including this holder, to acknowledge at a poll; the owner keeps the registry lock for the whole epoch). Native run 44148604890 showed epoch +1 with an owner and phase `gc_handshake` while the lock was held, and run 44258309305 showed the same advance with phase and owner not yet published (the stores are ordered epoch, phase, owner): both are the handshake waiting for the holder. The first formulation of (2), "epoch unchanged", and a second one that required an owner and phase `gc_handshake`, were stricter than the exclusion claim and wrong about the publication order; they are replaced. Limit, stated plainly: an epoch that began and completed entirely inside one lock hold WITHOUT collecting would show as epoch +1 with idle phase, indistinguishable from the publication window by the three registry words, so the detector does not catch that case; it catches every collection (counter), a stopped world sampled at entry or exit (invariant 1), more than one epoch, and an owner-self epoch moving. For the non-collecting case the argument of 11.3 (the owner waits for this holder's acknowledgement) is the only protection, and the native evidence does not contradict it. This is an independent check of the exclusion argument of 11.3, not a restatement of it.
* **Counters** (observed image, printed at exit): `alloc_lock_fast`, `alloc_epoch_path`, `alloc_lock_forced_fail`, `alloc_lock_invariant_checked` (lock-served allocations whose invariants were verified), plus the existing `epochs_entered` and `gc_count`.
* **Detector sensitivity control** (`MIO_ALLOC_LOCK_STRESS=violate`): the same image with one deliberate protocol violation, a poll inside the locked region. The in-lock checks must trip (nonzero exit) in at least one of several runs; a control that never trips means the checks are untested.
* **Program.** `tests/threads/alloc_lock_stress.mio`: five mutators, structs with managed fields, enum values, linked lists, arrays of structs, element stores into older arrays and array growth (Epoch-owning mutations), results compared with the sequential run. It also runs unchanged in `tests/threads/run.py` (normal and `--gc-stress`) and as an ordinary and `--gc-stress` Darwin program, so the existing stress coverage is untouched and extended.

### 12.3 What it does and does not establish
* Shows: the lock path and real collections both ran, interleaved, in every run (counters), without a failed in-lock invariant and with exact program output. Repeated native runs and the control give the evidence of record.
* Does not show: absence of races by construction (stress is evidence, not proof; the argument is 11.3 plus the invariants); behaviour on many-core hardware; timing (the image is instrumented and collects constantly, its speed is meaningless).
* Memory ordering: the lock word is acquired with `ldaxr` and released with `stlr` as in 11.3; the stress words (epoch seen, attempt counter) are only accessed while holding the lock; the registry reads are `ldar`.

### 12.4 Where it is off and rollback
Absent from every image built without the variable (byte-identical to the previous head for all non-stress builds, checked over the tests/threads programs for all six targets, normal, `--gc-stress` and `--observe`). Rollback: remove the `darwin-thread-lockstress` check and the `lock_stress` argument; production images do not change.

### 12.5 Evidence versus assumptions (head a063dce; hosted macOS ARM64, 3 cores)
Established by execution (Native 44267066617 and Core 44267066670, each six stress runs, all exit 0 with exact program output; control tripped 4 of 4 in both):
* The lock fast path and real collections ran interleaved: about 114,260 lock-served allocations, about 38,110 Epoch-path allocations, about 38,087 forced try-failures (every 4th locked attempt falls back to a real collection), `gc_count` about 42,870, `epochs_entered` about 54,100-54,170 per run; `alloc_lock_invariant_checked` equals `alloc_lock_fast` in every run (every locked allocation was checked).
* In every checked hold no collection ran (heap collection counter unchanged), no stopped world was visible at entry or exit for a non-owner, the epoch advanced at most once and never for the owner itself.
* The control (a poll inside the lock) trips the same checks (exit 60/205) while the stress image passes in the same job, so a trip is attributable and the detector is sensitive.
* Existing coverage is unchanged and green: `alloc_lock`, `alloc_lock_stress` (normal and `--gc-stress`), `epoch_retention` (carried-over epoch), `heap_invariants`, `heap_epochs`, `gc_stress`, `gc_lifecycle`, channels; non-stress and non-Darwin images are byte-identical to the previous head.
Assumed or not provable by this mechanism:
* A non-collecting epoch that begins and completes entirely inside one hold is not distinguishable from `gc_begin`'s publication window (stores are ordered epoch, phase, owner); only the acknowledgement argument of 11.3 covers it (the owner proceeds only after every non-blocked thread, including the holder, acknowledges at a poll, and the holder never polls inside the lock). Not contradicted by any run; not proved.
* The checks sample at lock entry and release, not continuously.
* Exact roots: the try-mode allocator never collects, never scans roots and has no side effects when it fails; an allocation that needs a collection always falls back to the unchanged snapshot path. This is supported by the forced fallback counters and the unchanged `gc-stress` suites, but rests on the code argument.
* ARM64 memory ordering: acquire (`ldaxr`) / release (`stlr`) on the lock, `ldar` registry reads, release stores and acquire loads in the poll/acknowledge protocol. A weakly ordered 3-core hosted runner exercises it; many-core hardware (for example M4 Pro) was NOT measured and is not claimed.
* The instrumented stress image collects on every allocation: its timing says nothing about performance.

## 13. Bounded Darwin allocation-lock waits

A small-object allocator first tries the existing allocation lock for up to
256 iterations, including failed exclusive stores. On contention it can enter
the native blocked-mutator protocol, attempt a 64-bit compare-and-wait, leave
through the GC handshake, and retry. Each kernel wait requests at most 1 ms.
The wrapper falls back to an Epoch after 64 such rounds, on oversize requests,
or when the non-collecting allocator returns zero.

The allocation word encodes free (0), held (1), and held with waiter interest
(2). Only running mutators acquire it (0 → 1). A parked waiter can advertise
interest (1 → 2) but cannot enter the heap. Darwin ARM64's release exchange
clears the word and returns the earlier value. If that value was 2, wake all
waiters. A waiter that races with release either fails to set the flag or
compares against the new word in the kernel. Spurious wakes and timeouts retry
after the mutator resumes. The reset and wake use the same state transition in
ordinary execution and on a forced try-failure in section 12.

The wait begins with the same published operands that the legacy Epoch path
would scan. The holder runs through release with its mutator active, so another
coordinator cannot stop the world while it is mutating allocator metadata.
The wrapper restores the original size and layout arguments after each wait.
The new path reserves no managed memory. Thread lifecycle, object initialization
and remembered-set updates use their existing protocols.

In an observe build `alloc_lock_contended` counts requests that enter this wait
path and `alloc_lock_wait_rounds` counts their attempts. Some attempts see a free
word on recheck and return without a kernel wait. `alloc_lock_gave_up` counts
exhaustion of the full contention budget. `tests/threads/alloc_lock_wait.mio`
starts 28 workers together, reuses their slots, and validates small and
large-object results by exact checksums.

## 14. Allocation wait guard and Darwin syscall outcomes

A stopped Epoch owner excludes every other allocator lock holder. A contender
can still exhaust its spin budget on failed exclusive stores against a free
word. An exclusive store can lose its reservation without another mutator
acquiring the lock. The owned-Epoch exclusion argument alone therefore cannot
rule out reaching the earlier experiment's blocking branch.

The revised cold path uses `attached_alloc_enter`. It publishes the shadow
chain and reads the registry owner before calling `gc_block_enter`. If the
owner is this mutator, the allocation takes `runtime_alloc_slow` and
`gc_heap_enter` retains the existing Epoch. An unrelated mutator can publish
its own request during the check. The per-thread block-entry handshake
coordinates with that request. No other thread can make this mutator an owner.
The cold path also rechecks the lock before entering the blocked protocol.

A Darwin raw syscall sets carry for an errno returned in x0. Wait timeout and
interruption consume a round and recheck. Wake `ENOENT` is an empty queue. A
hard error on wait or wake disables further allocation waits for this process.
An affected waiter leaves the blocked region before falling back. In-flight
waiters have finite deadlines, so failure of a notification cannot strand them.
Unlock still atomically exchanges zero through SWPL and wakes all if its old
value was 2.

Run the Darwin witnesses with
`python3 tests/threads/alloc_wait_deterministic.py --mio /path/to/mio --output /path/to/logs`.
The independent kernel witness uses barriers and Mach thread states. The Mio
witness parks a real mutator, makes a stopped coordinator observe it entering
`gc_block_leave`, and checks that the collection serial has advanced while
the worker's roots survive. The `owner` and `errno` images deliberately force
the ownership guard and a rejected kernel wait. These hooks require both
`--observe` and an explicit test mode.
