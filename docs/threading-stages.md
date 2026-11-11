# Native threading: four stages (architecture audit, status matrix, Stage One report)

Companion to [docs/concurrency.md](concurrency.md). Statuses use the owner's vocabulary: **IMPLEMENTED**, **TESTED LOCALLY**, **VALIDATED BY CI**,
**UNSUPPORTED**, **NOT_RUN**, **PLANNED**. Nothing here is advertised for a target until its native validation passes.

## 1. Architecture audit (Mio-2.2.0 after #59, #63, #64, #66)

| concern | source of truth (Linux x86-64) | notes |
|---|---|---|
| native op IDs | `bunko/kiso/aot/lower/nativefs/nativefs.mio` `operation` | 32-40 sockets, 41-51 thread/words (this PR adds 51 `join_status`). #65 (async TCP, open) uses 60-61. **No collisions**; a test asserts uniqueness. |
| thread create / destroy / TLS | `lowerlinux/mutators.mio` (`rt.thread_start`, `rt.thread_child`, `rt.thread_join`) | raw `clone`, own mmap stack with guard page, FS base = the thread record |
| registration / mutator tracking | the same file: record per thread, registry list, state word, handle table (Stage One) | registered under the heap lock before the thread runs managed code |
| safepoints / stop-the-world | `lowerlinux/gc.mio` `emit_poll` (stop flag + threshold), `mutators.mio` `emit_collect_mutators`, `emit_park` | Dekker-style publish/re-check handshake with `xchg` |
| precise roots | inline frame descriptors after every call/poll site; a parked or joining thread publishes `(rbp, call-site id)` | the collector scans every registered thread's chain and every entry closure |
| allocation sync | spinlock around `rt.alloc.locked` (spin, then `sched_yield`) | no thread-local buffers |
| blocking ops that are GC-safe | `join`, `word_wait` only | every other blocking OS call still runs in the *running* state and makes a collection wait: never treated as GC-safe |
| channel send/receive | `bunko/kiso/thread/thread.mio` (generic Mio over off-heap control words) | payload ring is an ordinary heap array, traced by the collector |
| ownership checking | `internal/typecheck/ownership*.go`, `selfhost/typecheck/ownership.mio` | E0701-E0704, effect summaries (#70) |
| futex or equivalent | raw `futex` syscall (private) | |
| capability reporting | the fail-closed rule: other lowerings reject the thread natives (CI `thread-natives-fail-closed`); the neutral-IR `target.TargetInfo` describes the M19ef stack and is unchanged | |

Reuse decision: the neutral-IR M19ef registry (`runtime/gc/threads.mio`) is a different runtime stack from the compiler's real Linux backend; the
Linux x86-64 implementation lives in the backend (`lowerlinux`), reusing the *protocol* (registry, epoch-free stop flag, parked/native states)
and none of the code.

## 2. Platform matrix

| platform | threads and channels | evidence |
|---|---|---|
| linux-amd64 | IMPLEMENTED, hardened in Stage One (this PR) | TESTED LOCALLY; VALIDATED BY CI once the PR's checks are green |
| linux-arm64 | UNSUPPORTED (fails closed) | Stage Two PLANNED: see section 4 |
| darwin-arm64 / darwin-amd64 | UNSUPPORTED (fails closed) | Stage Three PLANNED |
| windows-amd64 / windows-arm64 | UNSUPPORTED (fails closed) | Stage Four PLANNED |

## 3. Stage One (Linux x86-64 hardening), what changed

Defects found by reading my own first implementation, then proved with tests:

1. **Handles were raw pointers into the thread's own stack mapping.** A second `join`, a stale handle or a made-up number dereferenced
   unmapped memory. Now a handle is `(generation << 16) | (slot + 1)` into a 4096-slot table; a slot is free, active or joining, and its generation
   grows on every release. `join` returns **-22 (EINVAL)** for anything that is not a live, not yet joined handle and records a join status;
   `thread.join_checked` returns `JoinResult{ok, value}` so a thread that itself returns a negative number is distinguishable from that error.
2. **Creation failure was only half defined.** `start` now reserves a slot first (-11 when all 4096 are live, -12 without a table), then maps the
   stack, registers and clones; every failure path unlinks the record, unmaps the stack and releases the slot. The kernel refusing more threads
   surfaces as its own negative errno.
3. **A performance defect found by the stress test:** the heap lock and the registry lock were pure spinlocks. With 4000 blocked threads
   (more than the cores) the descheduled holder left the others spinning: 8 cores busy for 4 CPU-minutes (31 s wall). Spin, then `sched_yield`:
   0.77 s wall and 5 CPU-seconds for the same program.
4. **`destroy` unmapped the control words** while other threads could still be inside `send`/`receive` (use after free) and a second `destroy`
   was a double free. Control words are now carved from never-unmapped 64 KiB chunks (64 bytes per channel, a deliberate bounded leak);
   `destroy` is idempotent, closes the channel, drops queued messages, wakes everyone; a destroyed channel behaves exactly like a closed one
   (sends are rejected and hand their graph back, receives report closed).
5. **The channel lock spun forever.** It is now a three-state futex mutex (0 free, 1 locked, 2 contended); waiters sleep in `word_wait`, which is a
   safepoint, so a waiter never blocks a collection.

Owner decisions recorded (not made silently): the package name `kiso/thread` and every function name are still **provisional (D-2)**;
`join` keeps its `int64` shape for compatibility and `join_checked` is added beside it; the 4096-thread cap and the 1 MiB stack are
implementation limits, not language guarantees; a stack overflow in a thread is a process-killing fault (SIGSEGV at the guard page), not an
error value; the process ends with the main thread (active workers are killed).

### Evidence (Linux x86-64; `tests/threads/run.py`, every program built normally and with `--gc-stress`, repeated)
`lifecycle_cycles` (6000 sequential and 3000 batched lifetimes through a 4096-slot table), `invalid_handles` (0, negative, made-up, double and stale joins,
negative result), `creation_limit` (start until the error, release, join all, create again), `shutdown_active` (exit with blocked and busy workers),
`stack_overflow` (dies by SIGSEGV at the guard page, never silently), `channel_races` (40 seeds x close and destroy rounds with randomised jitter: every
attempted send accepted or returned, delivered exactly once under close), `gc_lifecycle` (200 rounds of 8 threads allocating under stress),
`channel_lifecycle` (2000 create/use/destroy cycles, double destroy, operations after destroy, destroy against a blocked receiver), plus all earlier
programs. 360 additional `--gc-stress` runs of the three most race-prone programs showed no mismatch. **Stress tests do not prove the absence of
races**: they raise the chance of finding them. The safety argument is the publish/re-check handshake described in docs/concurrency.md section 10.

## 4. Stages Two to Four: plan and what each needs (none started in this PR)

* **Stage Two, linux-arm64 (audit result; implementation NOT started).** What the AArch64 backend actually looks like, from reading it:
  * It shares `lower/lower.mio` with Darwin and Windows ARM64 (Linux and Windows reach the OS through the `linux_abi` / `windows_abi` syscall shims).
    The collector is the **allocation-triggered generational** one (`generational.mio`, young list, remembered set, large-object space): a collection
    can only start inside `runtime_alloc`, and the exact roots are the **shadow frames** pushed around each allocation site (`emit_shadow_push`).
  * **There are no polls.** The only polls (`attached_roots_poll`) belong to the experimental M19ef worker mode and call Darwin pthread/libSystem
    routines (`runtime/os/darwin`, `worker_roots.mio`, `attached_roots.mio`, `stw_bridge.mio`) that do not exist on Linux. A thread in a loop that does not
    allocate would never reach a safepoint, so a stop-the-world request could wait forever. Stage Two therefore needs **new polls** (function entry and
    loop back-edges, emitted only for programs that use the thread natives, as on x86-64) and an AArch64 park/resume protocol.
  * **Per-thread state lives in registers** (`x20`/`x21` Mochi operand stack, `x23` collector metadata, `x24` shadow-stack top, `x25`, x18 reserved on
    Darwin). Each thread needs its own operand stack, its own shadow stack, and its register set saved/restored at park; `x23` metadata is shared and
    every mutation of the young list, remembered set and counters must happen under the heap lock.
  * **New pieces required:** `clone` with `CLONE_SETTLS` (TPIDR_EL0) through `linux_abi` (AAPCS64, 16-byte stack alignment, a guard page); a thread record
    and registry; the handle table, slab and futex natives of Stage One re-written in AArch64 (the native op IDs 41-51 already exist in `nativefs.mio`;
    `emit_native_body` currently rejects them on purpose); the Dekker-style publish/re-check handshake with `ldaxr/stlxr` or LSE atomics and `dmb`;
    a heap lock around `runtime_alloc`; scanning every parked thread's shadow chain and entry closure; and a write-barrier review (the remembered-set
    barrier must be safe against concurrent mutators, which it is not today).
  * **Verification:** only on the real `ubuntu-24.04-arm` runner (no ARM64 hardware is available locally; cross-compilation is not evidence). A local
    AArch64 user-mode emulator exists as a development aid only and cannot run multi-threaded programs.
  * **Honest estimate:** this is a re-implementation of the Stage One runtime on a different collector, in assembly that can only be exercised through CI
    iterations of about 10-15 minutes each; it should be done as its own series of small PRs (polls + park protocol behind a gate; thread/handle/futex natives;
    allocation lock and barrier; channel natives; tests), not as one change. No owner decision blocks it; the effort does.
* **Stage Three, macOS.** pthread based (`pthread_create`, `pthread_key`, `pthread_cond`); the existing M19ef Darwin infrastructure (`runtime/os/darwin`,
  `lower/worker_roots.mio`) is experimental and seed-gated and must be audited before reuse. Each architecture is verified on its own runner
  (`macos-15` ARM64, `macos-15-intel`).
* **Stage Four, Windows.** `CreateThread`, `WaitForSingleObject`, `TlsAlloc`/TEB, `WaitOnAddress`/`WakeByAddress*`; x18 is the TEB on ARM64 and must
  never be written; Windows AMD64 and ARM64 are separate validations (`windows-2025`, `windows-11-arm`).

Each stage is a separate PR (B Linux ARM64, C macOS ARM64, D macOS AMD64, E Windows AMD64, F Windows ARM64) with the same shared test programs and the
same `thread-natives-fail-closed` to `linux-thread-gc`-style check flipped for that target only when its native evidence passes.
