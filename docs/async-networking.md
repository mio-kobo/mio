# Mio native asynchronous networking and M-task runtime — audit, contracts and phased plan

Status: **Phase A (nonblocking TCP) and Phase B (epoll reactor) implemented on Linux AMD64; Phases C–E are plans.**
Base: `Mio-2.2.0` at `4236836` (PRs #59 yuzuru ownership and #63 Linux thread primitives are merged in this base).
Terminology used below: *Implemented* (in the tree), *Tested locally* (run on the stated host), *Validated by CI* (a green run of the
named workflow), *Available on target X* (native execution evidence on X), *Experimental*, *Planned*.

## 1. Audit

### 1.1 What exists

| area | state | where |
|---|---|---|
| Blocking IPv4 TCP | Implemented, all three release targets; public API `connect/listen/accept/read/write/write_all/close`, `kiso/io` adapters; `TcpError` already has `WouldBlock` | `bunko/kiso/net/tcp/tcp.mio` |
| Socket primitives | Body-less `mio/sys/socket` functions: `open status bind_ipv4 connect_ipv4 listen accept read write close`. One socket = one entry of the NativeFS generation-checked handle table (stale handles report `Closed`, never another socket) | Go VM: `internal/nativesocket`; typechecker: `selfhost/typecheck/expression.mio`; per-compiler layout lists in `selfhost/*compiler/main.mio`, `selfhost/frontend/compiler.mio`, `selfhost/bootstrap/bootstrap.mio`; Linux x86-64 assembler routines `bunko/kiso/aot/lowerlinux/runtime_socket.mio`; Darwin AArch64 `bunko/kiso/aot/lower/nativefs/socket.mio`; Windows Winsock `bunko/kiso/aot/lowerwinrt/runtime_socket.mio` |
| Error translation | One status code byte shared by every backend: 0 ok, 1 invalid, 2 closed, 3 timed out, 4 refused, 5 reset, 6 address in use, 7 permission denied, 8 would block, 9 other (**10 in progress added by Phase A**). Linux maps errno by table (`rt.sock_classify`); EINTR is retried inside every primitive | same files |
| Partial I/O reporting | Existing `read`/`write` already return the exact transferred byte count plus an `eof` flag in a 10-byte status array; `write` uses `MSG_NOSIGNAL` (no SIGPIPE) | `runtime_socket.mio` (`emit_socket_transfer`) |
| GC | x86-64: single-mutator free-list mark-sweep, exact roots via inline frame descriptors after every call/poll site, safepoints at function entry and loop back-edges. Darwin: generational, shadow frames. Closures and captured cells are ordinary heap objects traced like any object | `docs/runtime-abi.md` (RA-1) |
| Threads | PR #63: Linux AMD64 OS-level `clone`/futex-join/FS-base TLS primitives and a native witness (`check_threads.py`). **Not** a managed-thread runtime: no mutator registration, no multi-mutator collector on the x86-64 heap. Capability flags (`runtime/target`: `threads`, `tls`, `park_wake`, `concurrent_collector`, `precise_roots`) stay fail-closed | `bunko/kiso/runtime/os/linux`, `bunko/kiso/runtime/target` |
| Ownership | `yuzuru` (PR #59): reserved keyword, explicit prefix transfer of a tracked local/parameter, no implicit moves, no closure-capture transfer, no parameter ownership effects | `docs/concurrency.md` §2–3 |
| Clock | `os.monotonic_ms()`, `os.stdin_wait(timeout_ms)` — the only time sources today; no timer queue | `bunko/kiso/os` |
| Polling / reactor | **Nothing.** No `epoll`, `kqueue`, `IOCP`, `poll` or `select` anywhere in the tree except the Phase A `poll(2)` probe inside `finish_connect` | (grep of the whole tree) |
| Coroutines | **Nothing.** No task type, scheduler, waker, timer queue, state-machine lowering or suspended-frame root rule | — |

### 1.2 What is incomplete

* TCP is IPv4-literal only; no DNS, IPv6, timeouts, `getsockname` (a listener on port 0 cannot report its port), `shutdown`, socket options
  beyond `SO_REUSEADDR`.
* Sockets are blocking. Before Phase A there was no way to request nonblocking mode, to finish a nonblocking `connect`, or to
  distinguish "would block" from a failure on `connect`.
* No readiness primitive exists, so nothing can wait for many sockets without blocking the thread.
* No coroutine/task representation and no way for a suspended computation to keep managed objects alive other than being an
  ordinary heap object (closure environment / struct), which *is* precise and safe for the single-mutator collector.

### 1.3 Safe to reuse (do not duplicate)

* The handle table (generation-checked tokens) and `rt.sock_fd`/`rt.sock_wrap` plumbing: every new socket primitive reuses it, which is what makes
  closing "safe against stale events" at the descriptor level.
* The status-code/array calling convention and the existing `read`/`write`/`accept` primitives: they already behave correctly on a
  nonblocking descriptor (EAGAIN → code 8 with count 0), so Phase A needed only two new primitives.
* Binding keys: the key is `name::sha256(signature)`. `set_nonblocking(resource,int64,bytes)` has the same signature hash as `listen`, and
  `finish_connect(resource,bytes)` the same as `status`/`close`; no new hashes were needed.
* Closures, structs and enums as heap-allocated task frames (precise roots already exist for them).
* `kiso/runtime/gc` thread registry, mutator and safepoint protocol (Darwin witness) as the design base for Phase E.

### 1.4 What requires compiler changes

* Phase A: **none beyond declaring the two new primitives** (typechecker signature table, native-function layout list in each compiler driver,
  Linux lowering routine, Go VM). Other targets deliberately fail closed (§1.6). Every body-less function a program declares must be lowerable on every target, even when unreferenced, so each backend needs at least a fail-closed stub.
* Phase C: a library-level task (closure/struct state machine) needs **no** compiler change. Automatic stackless lowering (`async waza`/`await`)
  would need a new keyword, a state-machine transform in the Mochi lowering, and a frame-liveness root description: this is a
  **language proposal that needs project-owner approval** and is *not* started.
* Phase E task migration: compiler analysis of captured state (the ownership work of PR #59 does not cover closure captures or parameter effects).

### 1.5 What requires runtime changes

* Phase B: `epoll_create1/epoll_ctl/epoll_wait` (+ `eventfd` for wakeups) routines in the Linux assembler runtime; later `kqueue` (Darwin AArch64 and
  AMD64) and IOCP (Winsock + `CreateIoCompletionPort`, OVERLAPPED/buffer lifetime) routines.
* Phase C: a monotonic-clock wait with timeout (the reactor wait doubles as the sleep).
* Phase E: managed thread registration, multi-mutator collector for the x86-64 heap, TLS roots (this belongs to the concurrency work stream, not this one).

### 1.6 Platform limitations (as of this PR)

| target | `tcp_async` | evidence |
|---|---|---|
| linux-amd64 | **Available**: nonblocking accept/connect/read/write, SO_ERROR completion; `kiso/reactor` (epoll) | native `mio test` of `bunko/kiso/net/tcp_async` (5 tests, real loopback sockets) run locally on linux-amd64 with a compiler built from this tree; CI result recorded in the PR |
| darwin-arm64 / linux-arm64 / windows-arm64 | not available: the AArch64 lowering declares the two primitives as fail-closed stubs (`native_socket_set_nonblocking`/`native_socket_finish_connect`) that report an `Other` (ENOSYS) status, so existing programs (including `kiso/net/tcp`) build and run unchanged while `tcp_async` operations fail with an error — never silently succeed | **NOT_RUN** |
| darwin-amd64 | not available: reuses the Linux lowering through the `darwin_syscall` adapter; the unmapped syscalls return `ENOSYS`, so operations report an `Other` error — fail closed | **NOT_RUN** |
| windows-amd64 | not available: unknown socket functions lower to `rt.os_unsupported` | **NOT_RUN** |

CI reports the package as `NOT_RUN` (not a required check, never PASS) on every target except linux-amd64
(`TARGET_ONLY_PACKAGES` in `scripts/ci/native_platforms.py`), and the Bunko matrix records `UNSUPPORTED` with a reason for darwin-arm64 and
windows-amd64 (`scripts/ci/bunko_matrix_coverage.json`).

### 1.7 Dependency order and critical risks

```
A nonblocking sockets ──> B reactor (epoll) ──> C single-thread tasks/scheduler/timers ──> D kqueue + IOCP backends
                                                         │
PR #59/#63 + concurrency stream (managed threads, multi-mutator GC) ──> E M:N scheduler
```

Risks, in the order they bite:

1. **Lost wakeups / busy polling** (B): level-triggered epoll, tasks re-armed only after `EAGAIN`; the reactor wait is the single blocking point.
2. **Descriptor reuse** (B/C): the handle-table generation protects the *API*; the reactor registration must additionally be keyed by
   `(token generation)` and removed on close before the descriptor is released, otherwise a recycled fd could wake the wrong task.
3. **GC roots of suspended tasks** (C): a task frame must be reachable from a root (scheduler ready/wait queues are heap objects reachable from
   the scheduler, which is reachable from a global or a live frame). Tests must run GC repeatedly while tasks are suspended (`--gc-stress`).
4. **IOCP lifetime** (D): OVERLAPPED and buffers must outlive the kernel's use including cancellation (`CancelIoEx` then wait for the
   completion packet) — a different protocol from readiness, implemented behind the shared contract, not forced into epoll's shape.
5. **Ownership** (C/E): `await` must not move anything; cross-worker migration is gated by compiler proof that does not exist yet.
6. **Fixed-point coupling**: every new `mio/sys/socket` primitive touches seven compiler-driver layout lists, the typechecker, the
   embedded Bunko bundle (`selfhost/resources/bunko_data.mio`, regenerated with `selfhost/embed-bunko`) and the Go VM. Keep primitives few.

## 2. Phase A — nonblocking TCP foundation (implemented on linux-amd64)

### 2.1 New primitives (`mio/sys/socket`)

| function | meaning |
|---|---|
| `set_nonblocking(handle, enabled, status) -> string` | `enabled` 0 or 1 (anything else → code 1). Linux: `ioctl(FIONBIO)`. VM: `syscall.SetNonblock` |
| `finish_connect(handle, status) -> string` | Completes a nonblocking `connect`: `poll(POLLOUT, 0ms)`; not writable yet → code **10 (in progress)** and the call may be repeated; writable → `getsockopt(SO_ERROR)` is the result (0 = connected, else the translated error). Precondition: the socket's `connect` returned code 10; on a socket that never called connect the result is not meaningful |

`connect_ipv4` on a nonblocking socket returns code 10 for `EINPROGRESS`; `accept`/`read`/`write` return code 8 (would block) for
`EAGAIN`/`EWOULDBLOCK` (existing behaviour, now exercised). No primitive waits; none can block the scheduler thread when the descriptor is nonblocking.

### 2.2 Public API: `kiso/net/tcp_async` (additive; `kiso/net/tcp` is unchanged)

```
listen(address, backlog) -> ListenResult{listener, error}
accept(listener)         -> AcceptResult{connection, pending, error}
connect(address)         -> ConnectResult{connection, pending, error}
finish_connect(conn)     -> UnitResult{pending, error}
read(conn, buf, off, n)  -> ReadResult{count, eof, pending, error}
write(conn, buf, off, n) -> WriteResult{count, pending, error}
close(conn) / close_listener(l) -> UnitResult{pending, error}
```

Contracts (also in the module header comment):

* **No waiting.** Every call returns immediately. `pending = hai` means "no progress possible now": nothing was consumed, nothing is lost, call again
  when the reactor (Phase B) reports readiness.
* **Exact partial progress.** `count` is the number of bytes actually moved, `0 <= count <= length`. `write` may return fewer bytes than asked; the caller
  resumes at `offset + count`. There is deliberately no `write_all`: looping would hide the suspension point a task must have.
* **Outcome exclusivity.** `pending`, `eof` and `error` are mutually exclusive; EOF is `eof = hai, count = 0`; a zero-length read/write is a success, not EOF/pending.
* **Resource ownership.** A handle inside a result belongs to the caller. A handle that is *not* returned has already been closed on every error path
  (`listen` failure after `open`, `accept` failure after the kernel accepted, `connect` failure, `finish_connect` failure). `close` twice, or use after close,
  reports `Closed` and never touches a recycled descriptor.
* **Error translation is deterministic**: the shared status byte → `AsyncError` (`InvalidArgument, Closed, TimedOut, ConnectionRefused, ConnectionReset, AddressInUse,
  PermissionDenied, Other(string)`). `WouldBlock`/`InProgress` never surface as errors; they are `pending`.
* **Ownership/yuzuru:** nothing here moves anything implicitly; connections are ordinary values.

### 2.3 Tests (all native, real loopback sockets, one thread)

`bunko/kiso/net/tcp_async/tcp_async_test.mio` (5 tests): accept/read report pending without blocking; echo round-trip with offsets, zero-length I/O, range
rejection, EOF on peer close, closed-handle errors; partial writes (an unread peer accepts N bytes in several short writes until `pending`, then every accepted byte
arrives in order and writing resumes after the drain); refused connect through `finish_connect` with the socket already released, malformed address/port/backlog,
`AddressInUse` without leaks, stale listener; 2000 connect/accept/close cycles without a failed handle. `selfhost/typecheck/native_socket_test.mio` covers the new
signatures. The Go reference VM runs the same package (`go run ./cmd/mio test bunko/kiso/net/tcp_async`) with identical results.

Not yet covered (Phase B/C needs a scheduler to be meaningful): hundreds/1000/10000 concurrent connections, slow readers, connection reset, timers, cancellation,
fairness, long-duration stability, GC-under-suspension.

## 3. Phase B — portable reactor (implemented on linux-amd64)

Status legend used here: **Implemented** (in the tree), **Tested** (native execution on the stated target), **Unsupported** (fails closed with an explicit error), **Planned**.

### 3.1 Preconditions (checked when this phase started)

PR #65 (`801bb02`, nonblocking TCP) and PR #66 (`8348adb`, OS threads and channels) are merged in `Mio-2.2.0`. Their native-function IDs coexist in `NativeFS`: blocking sockets 32..40, threads and channels
41..50, nonblocking and reactor socket primitives 60..66 (the socket module owns 60 and up; `is_socket_operation` in `nativefs.mio` is the only place that lists the ranges).

### 3.2 Public contract: `kiso/reactor` (pure Mio)

```
open()                                  -> OpenResult{reactor, error}      error Unsupported where there is no backend
submit(reactor, handle, interest)       -> SubmitResult{operation, error}  interest: readable 1, writable 2, both 3
update(reactor, operation, handle, i)   -> UnitResult
poll(reactor, timeout_ms, max)          -> PollResult{events, woken, stale, error}   the only blocking call
cancel(reactor, operation, handle)      -> UnitResult
current(reactor, event)                 -> bool    is the event's operation still live?
wake(reactor)                           -> UnitResult
close(reactor)                          -> UnitResult
```

`Operation{id, slot, generation}` and `Event{operation, slot, generation, readable, writable, error, hangup}`. The portable operation set is submit / poll / cancel / wake / close; `update` exists because epoll can modify
interest in place (an IOCP adapter would cancel and resubmit behind the same call).

**Operation identity.** `id` is unique within a reactor and never reused. `slot` is recycled, `generation` is not: a slot's generation increases every time it is released. The kernel is only given the opaque token
`generation * 1048576 + slot + 1`; token 0 is the internal wake-up descriptor. **Stale rejection.** `poll` drops (and counts in `stale`) every event whose token no longer matches a live operation: a cancelled operation, a
slot that was released and reused, a kernel event still in flight. **Cancellation.** After `cancel` the operation is never delivered; an event already returned by an earlier `poll` can be re-validated with `current`.
A cancelled or stale `Operation` cannot update or cancel the slot's new owner (it reports `Closed`). **Closing resources.** Cancel before closing the socket. The other order is memory-safe too: the kernel drops the registration when the
descriptor closes, a later `cancel` releases the slot and ignores the already-closed handle, and it cannot touch whoever received the descriptor next.

**Resource ownership.** Raw file descriptors are never exposed: the epoll instance, the wake-up eventfd and every socket are generation-checked handles of the runtime's handle table. While an operation is pending the reactor stores the
socket handle in `handles` (a managed reference), so the collector keeps it reachable; a freed slot holds the reactor's own poll handle as a placeholder. `close` frees both descriptors and invalidates all operations; double close reports `Closed`.
**Waiting.** Level-triggered: readiness persists until the owner drains the socket to EAGAIN, so nothing is lost between polls. `poll(-1)` sleeps in the kernel until an event or a wake, `poll(n)` for at most n ms, `poll(0)` does not wait.
EINTR makes `poll` return zero events early (never an error). Wakes coalesce into one `woken`; `wake` only adds to an eventfd counter and may be called from another OS thread.

### 3.3 OS primitives (`mio/sys/socket`, Linux AMD64 assembler runtime; Go VM implements the same contract)

| primitive | meaning |
|---|---|
| `reactor_open(kind) -> resource` | kind 0: `epoll_create1(EPOLL_CLOEXEC)`; kind 1: `eventfd2(EFD_NONBLOCK\|EFD_CLOEXEC)`; any other kind is "invalid argument" (this is also how `open` detects that a target has no backend) |
| `reactor_ctl(reactor, target, packed, status)` | `epoll_ctl` add 1 / delete 2 / modify 3; `packed = op \| interest << 8 \| token << 16`; the 64-bit token travels in the kernel's data word (12-byte packed `epoll_event`) |
| `reactor_wait(reactor, events, max, timeout_ms, status)` | `epoll_wait`, 1..64 events as 16-byte `(token, mask)` records, mask 1 readable, 2 writable, 4 error, 8 hangup; EINTR reports zero events |
| `reactor_signal` / `reactor_ack` | add to / reset the eventfd counter; EAGAIN is success (already signalled / nothing pending) |

### 3.4 Platform matrix

| target | reactor | evidence |
|---|---|---|
| linux-amd64 | **Implemented, Tested** | native `mio test bunko/kiso/reactor` (13 tests) and `tests/reactor/run.py` in CI (`linux-reactor` check) |
| darwin-arm64 | **Implemented (kqueue)**; validation is the `darwin-reactor` CI check on the macOS ARM64 runner (see `docs/async-parity-ledger.json`) | see ledger |
| darwin-amd64 | Unsupported (fail closed: `open` returns `Unsupported`); kqueue through the `darwin_syscall` adapter **Planned** | NOT_RUN |
| windows-amd64, windows-arm64 | Unsupported (fail closed); IOCP **Planned** | NOT_RUN |
| linux-arm64 | Unsupported (the AArch64 kqueue routines reach the Linux adapter and fail with ENOSYS, reported as `Unsupported`); epoll on ARM64 **Planned** | NOT_RUN |

Planned backends (not part of this phase): **kqueue** has the same readiness shape (`EVFILT_READ`/`EVFILT_WRITE`, `EVFILT_USER` for the wake-up) and maps onto submit/poll/cancel/wake directly. **IOCP** is completion based and must not be forced into
the epoll shape: `submit` would start an overlapped operation whose `OVERLAPPED` and buffer are owned by the adapter until the completion packet has been dequeued, including after `CancelIoEx`; `poll` is `GetQueuedCompletionStatusEx`; `wake` is `PostQueuedCompletionStatus`.

### 3.5 Tests and measured evidence (linux-amd64)

`bunko/kiso/reactor/reactor_test.mio`, 13 native tests over real loopback sockets: lifecycle and argument validation; wake is prompt and coalesced; timeout really sleeps (>= 50 ms for a 60 ms timeout); read readiness, level-triggered
persistence, writable interest, peer close as readable then EOF; **pending connect** completing as a writable event followed by `finish_connect`; **refused connect** reported through the reactor; **cancel before readiness**
(later data is not delivered; double cancel reports `Closed`; `current` rejects the event of a cancelled operation); **closing a socket with a pending registration** and descriptor/slot reuse (cancelling the old operation does not disturb the new owner); unique ids and
new generations; **backpressure** (a full send buffer produces no writable event until the reader drains, then writing resumes); generation rejection of an in-flight kernel event; 200 simultaneous connections each reported exactly once;
**a wake from another OS thread ends `poll(-1)`**, and 20 consecutive wakes are all observed.

`tests/reactor/run.py` (real epoll, measured, not estimated): repeated stress rounds with garbage allocation while operations are pending, normal and `--gc-stress`; a load test with 1000 loopback connections registered with one reactor
whose timed idle phase must cost almost no CPU (extra CPU for the idle period < 10% of its length, at most 3 polls, zero idle events) followed by one readiness per connection that must be reported exactly once; an EINTR test using SIGSTOP/SIGCONT.
The numbers printed by the run (connections, wall and CPU seconds, poll counts) are recorded in the PR; see "Limitations".

### 3.6 Limitations of this phase

* linux-amd64 only; nothing is claimed for other targets. The Go VM runs the primitives (probe-equivalent output) but not the reactor test package (no `kiso/os`, no threads there).
* Level-triggered only (edge-triggered and `EPOLLEXCLUSIVE` are not used). `poll` returns at most 64 events per call.
* The largest measured load is 1000 simultaneous connections (2000 handles, one process); the 10000-connection stress test needs more handles than the 4096-slot Linux table and a raised `ulimit -n` and is not claimed.
* The reactor is not thread-safe except `wake`; a reactor is meant to be owned by one OS thread (the future per-worker reactor of phase E).
* Long-duration (hours) stability is not claimed: the repetition tests run seconds to minutes.

### 3.7 Future integration points for Phase C (single-threaded coroutine scheduler)

A task waiting on I/O is a heap object holding its `Operation`; the scheduler calls `submit` when the task suspends and `cancel` when it resumes or is cancelled; its main loop is `run ready tasks -> poll(timeout until the next timer, or -1) -> mark tasks ready for each current event -> repeat`.
`wake` is the cross-thread entry for phase E. Timers need no new primitive: the timer queue's next deadline is the `poll` timeout. A task's registrations must be cancelled when the task completes or is cancelled (structured scopes own that), and an event for a
cancelled task is filtered with `current`. None of this is implemented in this phase.

### 3.8 Runtime change needed for scale: Linux handle table 64 -> 4096

The Linux runtime's NativeFS handle table (shared by files, directories and sockets) had 64 slots and an 8-bit slot field in the resource token, so a program could hold at most 64 open sockets. A 200-connection reactor test failed against it
and the goal is >= 1000 concurrent connections. Changed (Linux x86-64 only, internal layout, no language-visible change): the token is `generation << 16 | (slot + 1)`; the table has 4096 x 24 bytes, placed after the directory buffers; the
Mochi globals and the image's `bss` shift by 98304 bytes; directory handles are allocated only in slots 0..63 (the directory table and buffers keep their 64 entries) and `rt.fs_dir_slot` reports "not a directory" for any handle in a higher slot.
Darwin and Windows keep their own tables; their limits are unchanged and will be measured in phase D.

## 4. Phase C — single-threaded tasks (planned; no new keyword)

Library-level `kiso/task`: heap task frames (struct/closure state machine with an explicit `resume` state field), ready queue, waker, timer heap (monotonic clock),
cancellation token, structured scope. States: `Created → Ready → Running → Waiting → {Completed | Failed | Cancelled}` with a total transition table and tests for
every invalid transition. Fairness: a task runs until it returns `Yield`/`Wait`; the scheduler round-robins the ready queue and runs the reactor poll at least every K tasks.
GC: frames and queues are ordinary reachable heap objects; tested under `mio build --gc-stress` with tasks suspended across collections. `async waza`/`await` syntax is
**not** part of this work: it is a separate proposal that needs owner approval (keyword reservation, state-machine lowering).

## 5. Phases D and E (planned)

D: the Darwin (kqueue; AArch64 + AMD64) and Windows (IOCP; AMD64 + ARM64) backends, plus the two Phase A primitives on those targets, each with native execution on that target; a
target is "available" only with native execution evidence, never because cross-compilation succeeds.
E: only after the concurrency stream delivers managed threads (mutator registration, multi-mutator collector on the x86-64 heap, TLS roots): N workers, per-worker
ready queue and reactor, thread-safe cross-worker wake via the reactor's `wake()`, network-I/O tasks pinned to their worker, migration only for tasks whose captured
state the compiler proves transferable, stealing only for such tasks.

## 6. Decisions needed from the project owner

None blocks Phase A–C. Recorded for later: (1) whether to propose `async waza`/`await` (new keywords, D-3 style reservation); (2) the task-entry/transfer contract that
cross-worker migration will require (depends on closure-capture ownership effects that PR #59 explicitly left out).
