# Mio 2.2 native concurrency and zero-copy ownership transfer — Phase 0 specification

Status: **design proposal (Phase 0)**; D-1 and D-3 were decided by the project owner (see the box below). Nothing in this document is implemented yet; "exists" below always means a
statement about the Mio-2.2.0 tree at the time of writing (commit `4f347fd`). Language-design points that are not decided by
the project owner are marked **DECISION NEEDED** with a recommendation and alternatives; work that does not depend on them
proceeds.

> **Decisions recorded (project owner):**
> * **D-1 = Option A, prefix expression.** `yuzuru expr` is an explicit prefix ownership-transfer operator (`thread.send(ch, yuzuru graph);`,
>   `kaku mine = yuzuru graph;`). For the initial implementation: the operand must be an identifiable, statically tracked ownership variable (a local
>   variable or parameter, not an element path, global or call result); unsafe aliases, live borrows and escaping references are rejected; use after
>   transfer is rejected; there are **no implicit moves** through arguments or returns (`b = a`, `f(a)`, `kaesu a` are unchanged); a failed transfer
>   preserves ownership; fallible send operations are explicit ownership-state transitions with defined success and failure paths. **No `own x: T`
>   parameter annotation in this milestone**; function parameter semantics stay compatible and ownership effects come from the compiler's semantic
>   analysis. Any further annotation is proposed separately.
> * **D-3 = Option A, fully reserved keyword.** `yuzuru` is no longer an ordinary identifier anywhere (variables, functions, fields, parameters, imports);
>   there is no contextual-keyword behaviour. Lexer, parser, AST, semantic analysis, diagnostics, formatter/LSP keyword lists, docs and tests are updated;
>   the Tree-sitter grammar and highlighting live in ecosystem repositories and are listed as required follow-up in the PR (they are not edited from this tree).

Fixed by the project owner (not reopened here): ordinary assignment, initialisation, argument passing and `kaesu` keep their
current semantics; `yuzuru` is the only ownership transfer and there is no implicit move; arbitrary object graphs are
transferred without deep copying and keep identity and topology; native execution uses Mio's own runtime, never the Go
runtime; the six targets must eventually have identical observable semantics and no target may advertise concurrency
before it passes its safety requirements.

## 1. Audit of the current tree

### 1.1 Language facts that shape the design

* **Mutable heap state is narrow.** Structs are immutable (there is no field assignment), strings are immutable. The
  mutable things are array elements, map entries (`array.push`, `append_all`, map insert/overwrite), **captured cells**
  (a captured mutable variable is one shared heap cell) and **global variables** (runtime-ABI §5). Every cross-thread data
  race therefore goes through one of those four, and every one of them already has a single choke point in the lowering
  (it is the set of stores that carry the generational write barrier on Darwin).
* There is **no keyword or API named `yuzuru` anywhere in the tree** (whole-repo search) and no `kiso/thread`, channel or
  sync package. One name does exist: **`os.spawn(path, arguments)` starts a child *process*** (`bunko/kiso/os`), so a thread
  API must not reuse the bare name `spawn` unqualified (it would read as the process call); it lives in its own package
  (`thread.start`, §4). The only other concurrency vocabulary is the M19ef runtime protocol (§1.3), which is runtime-internal
  and "not a language builtin".
* Keywords are defined in two places that must stay identical: Go `internal/token/token.go` (`keywordsV01`..`V05`) and
  self-hosted `selfhost/lexer/token.mio`. Every consumer (lexer, formatter, LSP, incremental compiler) hard-codes
  `token.V05` as the current language version; there is no per-source version selection. Adding a keyword is therefore a
  **source-incompatible reservation** for any program that uses that word as an identifier (the repo itself has none for
  `yuzuru`; see §6 D-3).
* Front end exists twice: Go reference (`internal/{lexer,parser,ast,resolver,typecheck,compiler,vm,mochi}`) and self-hosted
  (`selfhost/{lexer,parser,resolver,typecheck,codegen,frontend}`). The Go VM is the semantic reference and the Stage-A bootstrap engine; native binaries never use it.
* Lowering goes through Mochi IR (`internal/mochi`, `bunko/kiso/aot/lower`). Per-target code generators:
  `arm64` (Darwin, Linux ARM64, Windows ARM64 via OS adapters), `x86_64` (Linux, Windows AMD64, Darwin AMD64 via adapters).

### 1.2 Runtime ABI status (RA-1)

`docs/runtime-abi.md` (RA-1) is in force: **every target is single-mutator**, object layouts are non-moving, roots are exact
(Darwin shadow frames; x86-64 inline frame descriptors after every call and poll site, scanned along the rbp chain),
safepoints are at allocation (Darwin) or function entry and loop back-edges (x86-64), and "enabling threads on any target
requires RA-2". Confirmed against the code:

| target family | managed threads | TLS | park/wake | precise roots for >1 mutator | `target.TargetInfo` |
|---|---|---|---|---|---|
| Darwin ARM64 | pthread bridge (`runtime/os/darwin/thread.mio`, `worker.mio`), **internal/opt-in only** | pthread keys | pthread cond/mutex | M19ef cooperative registry; `attached_roots.mio`/`worker_roots.mio` are internal, flags false | `threads: hai, concurrent_collector: iie, precise_roots: iie` |
| Linux AMD64 | none (no `clone`) | none (no `arch_prctl`) | **raw futex wait/wake only** (`platform_futex_*`, slots 204/205); the portable condition/mutex operations are deliberately *not defined* | none | `threads: iie, tls: iie, park_wake: iie` |
| Windows AMD64 | none | none | none | none | single mutator |
| Linux ARM64 / Darwin AMD64 / Windows ARM64 (new in 2.2) | none; Linux ARM64 and Darwin AMD64 reuse the Linux x86/Darwin AArch64 lowering through syscall adapters; the Windows ARM64 adapter has **in-process fork emulation for process spawning only**, not threads | none | none | none | single mutator |

The capability flags in `runtime/target` are already the **fail-closed gate**: portable code that needs an operation a target
does not define fails validation (`UNDEFINED_RUNTIME_FUNCTION` / `NOT_SUPPORTED_RUNTIME_OPERATION`) instead of silently
degrading. RA-2 keeps that mechanism and makes it the single place where a target "advertises" concurrency.

### 1.3 Reusable M19ef infrastructure (do not duplicate)

All of it is expressed in the neutral Runtime IR (`runtime/ir`) so it lowers identically per CPU:

* **Thread context / registry ABI v3** (`runtime/gc/threads.mio`, `thread_context_bytes=288`): state machine
  `new → starting → running → stopping → exited → joined | closed | failed | quarantined`, ownership flags
  (key/mutex/condition/handle), fault injection fields, registration counters, shadow-stack pointer, `thread_entry`/
  `thread_user`, acquire/release counters.
* **Mutator protocol** (`mutators.mio`): `gc_heap_enter`, `gc_mutator_poll`, `gc_block_enter/leave` (blocked-in-native
  threads park so the collector may proceed), `thread_wait_started/completion`, `memory_snapshot`.
* **Safepoint / STW handshake** (`safepoints.mio`): registry owner/epoch/heartbeat/phase words, `gc_poll`, `gc_park_locked`
  with a 30 s heartbeat deadline after which the process aborts rather than release live root frames.
* **Stopped-heap adapter** (`stw.mio`, `lower/stw_bridge.mio`): `gc_collect_stopped`, `gc_allocate_stopped` over the legacy
  heap with a coordinator-owned exact root snapshot.
* **Protocol model** (`protocol.mio`): pure phase/SATB/reclaim guards with tests.
* **Evidence**: `tests/m19ef/*` (thread-smoke with real pthreads, contended atomics, release/acquire publication, managed
  roots, lifecycle, worker platform, old-mark/sweep suites, E6 acceptance). These are **Darwin, seed-gated and NOT_RUN on
  hosted CI**; they are a design witness, not a Linux/Windows validation.
* **x86-64 atomics** (`runtime/cpu/amd64/atomic.mio`) and **IR operations** `acquire_load`, `release_store`, `tls_*`,
  `mutex_*`, `condition_*`.

The referenced `specs/002-m19ef-portable-pure-mio` directory is **not present in this repository**; the protocol
documentation that exists is `tests/m19ef/*.md` and the code comments above. Phase 0 treats those as the specification of
record and does not assume the missing directory.

### 1.4 What is missing (gap list)

1. Any user-visible concurrency surface (keyword, library, type rules).
2. Ownership/alias/effect analysis in the compiler (none exists; type checking is nominal/structural only).
3. Linux (and Windows) thread creation, TLS and the portable park/wake layer; Darwin's is pthread-based and internal.
4. A multi-mutator collector on the **x86-64 free-list mark-sweep** heap (the M19ef work targets the Darwin generational
   heap; x64 safepoints exist but assume one mutator and no stop request).
5. A managed object kind for queues/handles and a root rule for in-flight values.
6. Tests that run real compiled multi-threaded Mio programs on Linux; CI evidence on all six targets.

## 2. Ownership model

### 2.1 Vocabulary

* **Transfer set** of an expression `e` at a program point: the objects reachable from the value of `e` (the root and
  everything reachable from it through array elements, map keys/values, struct and enum payloads, closure environments and
  cells). Ownership is a property of the whole set, never of the root variable.
* **Isolated** (at a point): no object in the transfer set is reachable from any other root of the same thread (other
  locals, parameters, evaluation temporaries, captured cells of live closures, globals), and no object *outside* the set
  holds a reference into it.

### 2.2 States of a graph and legal transitions

```
 Private(T) --yuzuru--> Pending --publish--> InTransit(Q) --acquire--> Private(R)
     ^                     |                      |
     |                     +-- reject/fail -------+--(queue closed / cancelled / receiver gone)--> returned to the sender as a value
     +------------------------------------------------ unreachable --> Dead (ordinary GC)
```

1. `Private(T)`: reachable only from thread T's roots. All current programs live here forever.
2. `yuzuru e` (explicit): the compiler has proved the transfer set isolated (§3) and the source place is *moved*
   (statically unusable until re-assigned).
3. `Pending`: owned by the in-progress primitive call, still in the sender's frame temporaries, therefore still a GC root.
4. `publish`: one release-store of the root reference into the queue slot.
5. `InTransit(Q)`: owned by the queue object. The queue is an ordinary managed object, so the mark phase reaches the
   graph through it; no pinning table is needed (§5.2).
6. `acquire`: one acquire-load/CAS removes the reference from the slot and returns it to the receiver; `Private(R)`.
7. End: unreachable → ordinary collection.

**Failure and cancellation (decided in this spec, DECISION NEEDED only for the surface spelling):** a transfer that does
not complete must **not lose the graph**. Recommended: *ownership flows through result values.* A transfer primitive
returns a result whose error case carries the rejected graph back to the sender (`SendError{ value, reason }`), so the
moved variable is dead on **both** success and failure paths and the graph is reachable from exactly one place on every
path. This removes any "maybe-moved" static state. Alternative considered: restore the variable on the failure edge
(needs flow-sensitive "moved if result ok" and breaks as soon as the result is stored). Closed queue, cancelled thread
and receiver termination all use the same rule: items still in a closed queue are handed back to the closer (or, if
nobody drains it, become ordinary garbage when the queue is unreachable — never a leak with a dangling reference).

### 2.3 What `yuzuru` does and does not do

* `yuzuru` never changes the meaning of `a = b`, `f(b)` or `kaesu b`. Without it nothing in this document applies.
* At the language level `yuzuru e` evaluates `e` to a reference and *invalidates the source place*. It is not a copy and has
  no deep-copy fallback: if isolation cannot be proved the program is **rejected at compile time** (never copied, never
  silently accepted).
* Scalars (`int64`, `float64`, `bool`, enum tags) and strings/immutable scalar-only structs carry no mutable state;
  transferring them is permitted and cheap but they are not "ownership-bearing" (no isolation check beyond the move).
  A value is ownership-bearing iff its transfer set contains an array, map, captured cell or closure.

## 3. Alias, escape and borrow analysis (Phase 1 design)

### 3.1 Why a static analysis is sufficient here

Because mutable state is limited to arrays, maps, cells and globals (§1.1), a flow-sensitive, allocation-site-based
points-to analysis over Mochi IR is enough and does not need field sensitivity for structs. Abstract objects:
`Alloc(site)` per allocation expression (arrays, maps, struct/enum constructions, closure environments, cells),
`Global(name)`, `Param(function, index)` and `Unknown`. Points-to sets are per local and per abstract container
(`Contents(obj)` merges all elements of an array/map — element-insensitive, hence conservative).

### 3.2 Admission rule

`yuzuru x` at point *p* is **accepted** iff all of:

1. *Closure of the set:* let `S` = abstract objects reachable from `pts(x)` through `Contents` edges. `Unknown` in `S` rejects.
2. *No other local alias:* no other live local/parameter/temporary `y` (liveness: non-lexical, computed on the IR CFG) has
   `pts(y) ∩ S ≠ ∅`, including `y` derived from `x`'s sub-objects (`y = x.items[0]`).
3. *No global/escape:* `S` contains no `Global`, nothing in `S` is reachable from a global, and no closure with a live
   reference has an environment reaching `S` unless that closure is itself in `S`.
4. *No inbound heap alias:* no abstract object outside `S` has a `Contents` edge into `S`.
5. *Calls:* for every call between the object's creation and `p`, the callee's **effect summary** (§3.4) shows no
   `Escape`/`Capture`/`PublishToGlobal` of an argument aliasing `S`; calls to unknown/native functions (no summary) are
   treated as escaping every argument.
6. *Borrow:* no in-scope borrow of any object in `S` is still live (§3.3).

Rejection is conservative and carries a diagnostic naming the offending alias (variable and line), the escape path
(`stored into global g`, `captured by closure at line n`, `reachable through y.items`) and a hint (re-create the value,
or end the alias before the transfer). Loops: the analysis iterates to a fixed point on the CFG; a variable moved inside a
loop body must be re-assigned on every path to the next iteration. Cycles are handled by the closure computation (it is a
reachability set, not a tree walk). Recursive structures use allocation-site summarisation, so they are over-approximated,
never under-approximated.

### 3.3 Borrowing model (compatible with existing semantics)

Mio has no reference-qualified types and ordinary assignment aliases freely, so *every live alias of any object in the
set is a borrow for the purposes of §3.2*. The model therefore needs **no new surface syntax**:

* Multiple simultaneous read-only borrows and exclusive mutable access are not enforced in ordinary single-threaded code
  (that would break compatibility); they are enforced *only at transfer and thread boundaries*, where the rule is the
  strong one above: zero other live references.
* Non-lexical lifetimes: a borrow (alias) is live from definition to its last use on any path; no scope syntax required.
* Nested/subobject borrows: aliases into `S` count (rule 2). Escaping references: rules 3–4. Closure captures: rule 3.
* Returning a borrowed value / returning aliases: summarised as `ReturnsAlias(param i)` in the callee summary; the caller's
  points-to then contains the argument's objects.
* Across thread boundaries: **no borrow crosses a thread boundary.** The only values a spawned thread may use are those it
  receives by `yuzuru` (arguments and results) and immutable constants (§3.5).

### 3.4 Function effect summaries

Per function (and per closure body), computed bottom-up over the call graph, SCCs by iteration to a fixed point, stored in
the module metadata so separate compilation (Bunko packages) sees them:

`Reads(param)`, `Mutates(param)`, `ReturnsAlias(param)` / `ReturnsFresh`, `Escapes(param, to: Global | Heap(param j) |
Closure)`, `Captures(param)`, `Transfers(param)` (callee contains `yuzuru` of it), `TouchesGlobal(read|write, name)`,
`Publishes` (callee performs a thread/queue publication), `Unknown` (native/external). Indirect calls (closures, function
values) use the join of all possible targets from the points-to set of the callee value, `Unknown` if the set is open.
Generic functions are summarised once per instantiation if they are specialised (existing monomorphising pass) and once
conservatively otherwise. Summaries are **conservative joins**: absent a summary the function is `Unknown`.

### 3.5 Boundary rules for threads (recommended; DECISION NEEDED D-5)

A function reachable from a thread entry may not read or write a **mutable** global (`TouchesGlobal`), and may only capture
immutable data (`teisu` constants) or values received by `yuzuru`. Violations are compile errors with the access path.

## 4. User-visible surface (recommendations)

Names are **proposals**; nothing is reserved until approved (D-1..D-4).

* **Transfer syntax (D-1, recommended):** prefix unary `yuzuru expr`, where `expr` must be a *place* (local variable or
  parameter; later also an element path) so the compiler knows which place to invalidate. Example (illustrative):
  `thread.send(channel, yuzuru graph)`. It is allowed as a call argument or on the right of `kaku`/assignment
  (`kaku mine = yuzuru graph;`). Alternatives: (B) statement form `yuzuru graph -> channel;` (reads well but needs a new
  statement production and an implicit queue), (C) parameter-mode annotation `waza f(own x: T)` (needs type-syntax
  changes and per-callee ownership contracts; larger incompatibility surface). Recommendation: A — smallest extension,
  call-site explicit, works with both library intrinsics and ordinary functions (`f(yuzuru g)` = caller loses `g`).
* **Library (D-2, recommended placement):** a new Bunko package `kiso/thread` (names `start`, `join`, `Channel`,
  `send`, `recv`, `close`, `cancel` as proposals) built on intrinsics that are the only transfer-capable primitives in v1.
  `thread.start(entry, yuzuru argument)` returns a `Thread` handle; `join` returns the entry's result by ownership transfer, or an
  error value carrying a panic/runtime-error summary. Error propagation is by value (Mio has no exceptions).
* **Cancellation / closure / shutdown:** cooperative. `close(channel)` wakes receivers (they observe `closed`); `cancel(thread)`
  sets a flag polled at blocking operations and safepoints; there is no asynchronous kill. Receiver termination with items
  still queued: the closer drains and receives the graphs back (§2.2).
* **Thread-local execution state:** each thread owns its shadow/inline-frame chain, TLS slot (current-thread context), and
  an allocation cursor; no user-visible TLS in v1.

## 5. Runtime and GC design

### 5.1 Strategy: staged, not a rewrite

Keep both existing collectors and object layouts. First milestone targets **Linux x86-64** (free-list mark-sweep, single
generation, no barriers, inline frame descriptors already exact, only `clone`/`futex`/`arch_prctl` are new OS surface).
Darwin keeps its M19ef pthread path and is brought under the same user API afterwards. Concurrent *marking* (SATB) is not
required for correctness of the first milestone: collections are **stop-the-world** with all mutators at safepoints.

### 5.2 In-flight rooting and publication

* A channel is a managed object (new kind, §5.4) holding a ring of reference slots plus counters; it is reachable from the
  creating thread and every thread holding it, and is traced like any array. A graph in a slot is reachable through the
  channel, so **no separate pin/root table**.
* Publication: sender does the allocation-free sequence *(1)* store root into slot (generational targets: via the write
  barrier), *(2)* release-store the tail index, *(3)* clear the moved source slot. A collection between any two steps sees
  the graph through the sender frame and/or the queue — never through neither.
* Acquisition: receiver acquire-loads head/tail, loads the slot, stores it to a described local (so it is a root), then
  advances head with release; the slot keeps the reference until head advances, again preventing a "neither" window.
* Blocking send/recv uses `park_wake` (futex on Linux, pthread cond on Darwin, `WaitOnAddress`/SRW on Windows). A thread
  blocked in native code is marked *parked-with-roots* via `gc_block_enter` so the collector does not wait for it.

### 5.3 Thread registration and stop-the-world on x86-64

* Registration: `thread.start` allocates the context (reusing `gc/threads.mio` ABI v3), registers it with the registry **before**
  the new thread runs managed code; the thread entry stub installs TLS then calls `gc_heap_enter`. A thread that cannot be
  registered is never started (fail closed → `thread.start` returns an error value).
* Safepoints: function-entry and loop back-edge polls test the registry owner word; if set, the thread publishes its
  `rbp` chain head and parks (existing `gc_poll`/`gc_park_locked`). Allocation takes a heap lock in the first milestone
  (correctness first); thread-local allocation buffers are a later optimisation.
* Roots: each registered thread's frames are scanned from its saved `rbp`/return address with the existing inline
  descriptors; the thread context and queue objects are reached from globals/handles.

### 5.4 RA-2 (proposed ABI transition)

RA-2 is **additive to RA-1** and is gated per target by `target.TargetInfo`:

1. **Mutators:** more than one mutator may exist iff `threads && tls && park_wake && precise_roots && atomic64` are true for
   the target; otherwise `thread.start` fails closed.
2. **Memory model:** release/acquire on publication and acquisition; sequentially consistent for the queue indices and
   join; data-race-free programs (as enforced by §3) behave sequentially consistently. Plain stores/loads of ownership-bearing
   data are only legal between the acquire that gave the thread ownership and the release that gives it up.
3. **Roots:** every registered thread has an exact root chain; the collector enumerates all of them at a stop-the-world
   request; a thread between safepoints is never scanned.
4. **Allocation:** thread-safe (heap lock or per-thread buffers).
5. **Layout:** header unchanged (16 B). New kinds appended: channel (ring + counters, traced as an array of references),
   thread handle (context pointer, result slot, state). x64 profile becomes `x64-marksweep-v2` (kinds 12/13 added; v1 programs
   are byte-for-byte unaffected — migration test: the whole `tests/runtime-abi` corpus plus a diff of generated images for
   programs that do not use `kiso/thread`). Generational profile adds the same two type ids.
6. **Unsupported operations fail closed** exactly as in RA-1 (capability missing ⇒ validation error or `thread.start` error value,
   never a silent single-threaded fallback).

### 5.5 Per-OS thread primitives (adapters, not language semantics)

| target | create | TLS | park/wake | join |
|---|---|---|---|---|
| linux-amd64 | raw `clone(CLONE_VM\|FS\|FILES\|SIGHAND\|THREAD\|SYSVSEM\|SETTLS\|PARENT_SETTID\|CHILD_CLEARTID)` on an `mmap`ed stack with guard page, no libc | `arch_prctl(ARCH_SET_FS)` | `futex` (exists) | futex on the CLEARTID word |
| linux-arm64 | same, `TPIDR_EL0` for TLS | `msr tpidr_el0` | futex | CLEARTID futex |
| darwin-* | pthread (exists for ARM64); x86-64 Darwin via the same libSystem imports | pthread keys | pthread cond | `pthread_join` |
| windows-* | `CreateThread` | `TlsAlloc` (ARM64: TEB via x18, never written) | `WaitOnAddress`/`WakeByAddress*` | `WaitForSingleObject` |

## 6. Decisions, risks, open questions

| id | question | recommendation | alternatives |
|---|---|---|---|
| D-1 | exact `yuzuru` grammar | **DECIDED: prefix expression, operand is a tracked variable** | statement form; parameter modes (not this milestone) |
| D-2 | library names/package | `kiso/thread` with `start/join/Channel/send/recv/close/cancel` | split `kiso/channel`; `go`-style statement |
| D-3 | keyword reservation | **DECIDED: fully reserved keyword** (new language version table V06 is the current one); repo scan found no existing use | contextual keyword (rejected) |
| D-4 | failure semantics | ownership returns inside the error value (§2.2) | restore variable on failure edge |
| D-5 | globals in threads | forbid mutable globals reachable from thread entries | allow atomics/mutex-protected globals later |
| D-6 | isolation proof | static (§3) only; optional debug audit pass under `--gc-stress` that recomputes reachability and aborts on violation | runtime check always on (O(heap) per transfer) |
| R-1 | element-insensitive container summaries reject legal programs | acceptable first cut; document patterns; refine later | per-index tracking |
| R-2 | x64 stack scanning of a thread blocked in a raw syscall | publish frame head before the syscall (`gc_block_enter`) | signal-based stop |

## 7. Compatibility and acceptance criteria

* **Compatibility:** with `yuzuru`/`kiso/thread` unused, every existing program's behaviour, output and exit status is
  unchanged on the Go VM and on every native target; generated images for such programs are byte-identical to the
  pre-change compiler (checked by building the existing corpus before/after); `tests/runtime-abi` and the differential
  corpus pass unchanged and unweakened. The only incompatibility is the new reserved word `yuzuru` (D-3).
* **Phase 1 accepted when:** parser/AST/typecheck (Go and self-hosted agree on the same fixtures) accept the transfer form;
  positive/negative analysis tests cover: valid transfer, use-after-move, double transfer, root alias, nested-subobject alias,
  global escape, closure-captured escape, failed-transfer ownership, identity preserved (VM), cyclic and shared-subobject
  graphs; ordinary-assignment programs unchanged (golden diff).
* **Phase 2/3 accepted when:** a real compiled Linux x86-64 program creates threads, transfers graphs through a channel
  with the sender provably unable to use them, runs under `--gc-stress` with concurrent allocation, shuts down cleanly,
  and the native chain (compiler fixed point, `mio test`, runtime-ABI contract and differential) still passes on all six
  targets with `target` flags untouched elsewhere (other targets must still *reject* `thread.start`).
* **Phase 4:** each target flips its capability flags only in the PR that adds its passing native evidence.
* **Phase 5:** benchmarks (construction, transfer, deep-copy baseline, small/large throughput, GC stress, contention) are
  reported with measured numbers; the transfer is called "zero-copy" only if the measured cost is independent of payload
  size apart from the isolation analysis.

## 8. Implementation status of the D-1/D-3 slice

Implemented (Go reference and self-hosted compiler, so every native target gets it): `yuzuru` is a fully reserved keyword
(language version V06, `token.CurrentVersion`; V05 sources still lex it as an identifier); `yuzuru name` is a prefix expression
whose operand is exactly one variable name (parse error E0103 otherwise); the checker reports **E0701** invalid operand
(parameter, function, constant, closure-captured variable, top-level variable that any function references, variable not
declared in the transferring function), **E0702** use after transfer (flow sensitive: branches, loops, the next iteration),
**E0703** double transfer, **E0704** use of a variable that still aliases the transferred graph. Aliasing is the coarse,
sound-by-over-approximation grouping described in `internal/typecheck/ownership.go`. At run time `yuzuru x` is a plain read
of `x`: it never copies, so identity, sharing and cycles are untouched. Programs without `yuzuru` are not analysed at all.

Not implemented yet: the points-to/effect-summary engine (PR-2; today's grouping rejects more than necessary), element and
field operands, parameter transfer, any runtime/threads/channels, and every target's concurrency capability (all flags unchanged).

Further decisions that surfaced and need the owner (the current behaviour is the conservative choice and can only be relaxed
later, never the reverse):

* **Parameters.** `yuzuru p` on a parameter is rejected, because the caller still holds the argument. Allowing it needs either
  the callee's effect summary to move the caller's variable at `f(a)` (an implicit move, forbidden by D-1) or a call-site
  `f(yuzuru a)` contract; both need your call.
* **Top-level variables.** Transferable only while no function body mentions them (script-style code); a mention anywhere in a
  function makes the variable "global" and untransferable.
* **Failure of a fallible send.** Recommendation unchanged (D-4): the rejected graph comes back inside the error value, so the
  moved variable is dead on both paths. This must be confirmed when the `kiso/thread` API is specified.
* **Closures.** A variable captured by any closure, before or after, cannot be transferred.

## 9. Editor and ecosystem changes required outside this repository

Tree-sitter (`tools/tree-sitter-mio`) is updated here. Still needed elsewhere (not edited from this tree):
* `mio-vscode`: add `yuzuru` to the TextMate keyword scopes (operator keyword) and to completion snippets.
* `mio-jetbrains`: add `yuzuru` to the lexer keyword set, the highlighter and completion.
* any other grammar consumer that vendors the keyword list (language version V06).

## 10. Implementation status: Linux x86-64 mutators (RA-2 draft, first slice)

Implemented in `bunko/kiso/aot/lowerlinux/mutators.mio` (and the gated hooks in `gc.mio` / `lowerlinux.mio`). A program that references the
thread natives gets the multi-mutator runtime; **every other program compiles to byte-identical images** (23 of 23 examples and the
compiler itself were compared against the previous compiler).

* **Registration.** Every mutator has a 128-byte record (in its own stack mapping; the main thread's lives in the collector state
  area). It is also the TLS block: word 0 points to itself and the FS base is the record. Records form a registry; thread start
  registers the record under the heap lock *before* the thread runs managed code, and the new thread enters the running state
  through the same park protocol, so a collection already in progress is honoured. A thread that cannot be registered is never started
  (`start` returns a negative errno).
* **Safepoints and stop-the-world.** Polls (function entry, loop back-edges) test a stop flag as well as the allocation threshold. The
  first thread to cross the threshold wins the flag with `lock cmpxchg`, waits until no other registered mutator is running, marks
  the globals, every mutator's published frame chain and every mutator's entry closure, sweeps, clears the flag and wakes the parked
  threads (futex). A thread publishes its chain (rbp and the call-site id of the frame's descriptor) before it leaves the running
  state and re-checks the flag after it re-enters it; the collector sets the flag before it reads any state. With `xchg` on both sides
  either the collector sees the thread as not running or the thread sees the request: a running thread is never scanned and never missed.
* **Heap lock.** A spinlock serialises `rt.alloc` and registry changes. Runtime routines never collect, so a thread is never parked
  while holding it.
* **join.** A thread waiting in `join` is in a *native* state with its chain published, so it never blocks a collection; the joiner
  unmaps the stack after the kernel cleared the tid word.
* **Entry points (provisional names, decision D-2 still open).** The pseudo-package `mio/thread/runtime` provides
  `start(entry: () -> int64) -> int64` (a handle, or a negative errno) and `join(handle) -> int64` (the entry's result). They are native
  operations of the Linux x86-64 lowering only; the arm64 lowerings reject them at compile time, macOS x86-64 reports
  `UNSUPPORTED_THREADS_TARGET`, and the Windows runtime reports them unsupported when called. A user-facing `kiso/thread` package, channels
  and `yuzuru` transfer through them are the next milestones.

Evidence (all native, Linux x86-64): `tests/threads/run.py` builds six real Mio programs (start/join, five mutators under
`--gc-stress` with a sequential cross-check, captured closures that outlive the creator's references, nested threads, 32 thread lifetimes,
and about 19 GiB of garbage through an 8 GiB heap) normally and with `--gc-stress` and runs them repeatedly; 600 local runs had no
wrong result, hang or crash. CI records it as the required check `linux-thread-gc`; every other target records
`thread-natives-fail-closed`.

Known limits: blocking OS calls other than `join` are not safepoint-aware (a collection waits until such a thread returns); the process
ends when the main thread ends; results are `int64` only (graphs travel through channels in the next milestone); one 1 MiB stack per thread;
no thread-local allocation buffers (the heap lock is the only allocation synchronisation); the `target.TargetInfo` capability flags
describe the neutral-IR runtime (M19ef path), which is unchanged, so they still report no threads for Linux.

### Channels and ownership transfer (second slice)

`bunko/kiso/thread` (Linux x86-64 only; **names provisional, decision D-2 open**) adds `start`, `join` and a bounded multi-producer
multi-consumer `Channel<T>` with `channel_new`, `send`, `receive`, `close`, `destroy`. The control words (lock, head, tail, count, closed,
two futex sequence words) live in an off-heap block reached through internal `int64` natives (`words_new/free`, `word_get/set/cas/add`,
`word_wait/wake`); the payload ring is an ordinary Mio array, so a graph in flight is an ordinary heap reference that the collector
traces: no pinning table, nothing copied. `word_wait` is a safepoint-aware native wait like `join`.

* `thread.send[T](channel, yuzuru graph)` stores the reference in a ring slot. With D-1/D-3 the compiler rejects every later use of `graph`
  (E0702), a second send (E0703) and a live alias (E0704), so the receiver is the only owner. A failed send (closed channel) returns
  `SendResult{ sent: iie, back: value }`: the graph comes back to the sender, which therefore never loses it merely because a transfer was
  attempted (decision D-4 recommendation, now implemented).
* `receive` blocks while empty and open; after `close` receivers drain what was queued and then get `closed`. `close` releases blocked
  senders (their graphs come back) and blocked receivers.
* Generic enums cannot yet be constructed inside generic functions by the checker, so the results are generic structs with a flag.
* The Bunko matrix records the package as covered by the `linux-thread-gc` check on linux-amd64 and unsupported (with the reason) on the
  other five targets; the package has no `_test.mio` because package tests run on every target.

Evidence (native, Linux x86-64): 7 new programs in `tests/threads` (basic producer/consumer; close and failed-send hand-back; shared
sub-objects, cycles, a 2000-deep chain and a 20000-node tree arriving intact with mutation visible through the other path; 3 producers x 3
consumers; three programs that must be rejected at compile time), each normally and with `--gc-stress`; 450 additional stress runs
of the three most concurrent programs had no mismatch.

### Measurements (benchmarks/threads/transfer.mio)

Linux x86-64 (8 hardware threads, kernel 6.12), two OS threads, a 1-slot channel, wall-clock milliseconds on the sender's thread, three
consecutive runs shown as min-max. "construct" builds a fresh graph per message with no channel traffic; "construct + transfer" builds it and
hands it over with `yuzuru`; "copy + transfer" deep-copies a prebuilt graph and sends the copy (what a copying design pays when the sender
keeps its graph).

| nodes per graph | messages | construct | construct + transfer | copy + transfer |
|---|---|---|---|---|
| 10 | 20000 | 59-68 | 152-161 | 129-154 |
| 1000 | 2000 | 441-505 | 445-545 | 459-486 |
| 100000 | 40 | 1249-1620 | 1250-1421 | 1254-1325 |

Reading: the transfer step itself (construct + transfer minus construct) is about 4.5 us per message for tiny graphs (the lock and futex
handoff between two threads dominates) and is within run-to-run noise at 1000 and 100000 nodes, i.e. it does not grow with the graph, while a deep copy costs about
as much as constructing the graph again (about 31 ms per 100000-node message). That is what "zero-copy" means here: no object is copied and the
cost of the handoff is independent of the payload size in the measured range. The numbers are time only: there is no allocation or memory counter
in the runtime yet, the variance between runs is large for the big graphs (the collector runs concurrently), and the figures are one machine's, not a
cross-platform result. No claim is made for other targets.

### Effect summaries and escapes (third slice)

The ownership analysis (Go `internal/typecheck/ownership*.go`, self-hosted `selfhost/typecheck/ownership.mio`, identical diagnostics) now has
interprocedural summaries. A direct call of a user function whose behaviour is fully visible (no top-level variable, no closure, no indirect
call, no unsummarisable callee, no function-typed argument, not recursive) is no longer a black box: its summary records which parameters the
result may alias (none: the result is a fresh graph) and which parameters the call may link to each other, and only those relations are applied.
Scalar-typed values never carry aliases. Everything else keeps the conservative rule, so a summary can only remove rejections.

A soundness hole that predates this slice is closed: a callee that stored its argument in a global (or an unsummarisable callee in general)
could publish a graph the caller then transferred. A graph that reaches a global, or that was passed to code the analysis cannot see
(unsummarised user functions, indirect calls, any call given a function value), is now **not transferable** (E0701). Package and builtin
functions that receive no function value cannot reach the program's globals and only merge their arguments.

Tests: 45 programs run identically through the Go checker and the self-hosted checker (`TestMioTypecheckerOwnershipDifferential`); native
programs `summary_copy` (copy transferred while the original stays in use) and `reject_escape_global` in `tests/threads`; the benchmark now
uses the natural `deep_copy(source)` form. Still open: parameter transfer (owner decision), recursion (a fixpoint would allow summaries for
recursive builders), field/element operands.

## 11. PR plan

1. **PR-0 (this document)**: audit + specification, no behaviour change.
2. **PR-1**: language version table + `yuzuru` token/parse/AST (Go and self-hosted), unresolved-grammar choice behind
   the D-1 recommendation, diagnostics for unsupported use; no runtime meaning yet.
3. **PR-2**: points-to/liveness/effect-summary engine and the admission checker with positive/negative tests.
4. **PR-3**: Linux x86-64 thread/futex adapter, thread registry integration, multi-mutator safepoints on the x64 heap.
5. **PR-4**: `kiso/thread` channel and end-to-end Linux x86-64 native tests, GC stress.
6. **PR-5+**: remaining targets one at a time, benchmarks, documentation.

### Efficiency: allocation scaling on Linux x86-64 (benchmarks/threads/efficiency.mio)

The microbenchmarks time fixed amounts of work (wall-clock milliseconds, median of three runs, 8 hardware threads, kernel 6.12, one machine). The
first measurement of the thread runtime showed that allocation did not scale at all: pinned to one core the times were exactly serial (213, 423 and
841 ms for 1, 2 and 4 threads), but on several cores two threads took 4.4 times and four threads 15 times the single-thread time, i.e. worse than
running the threads one after the other.

Two causes, both cache-line contention rather than algorithm: (1) the heap lock shared a 64-byte line with the stop flag that every thread reads at
every poll, and the poll also read the allocation volume word that every allocation writes, so each allocation invalidated the polls of all other
cores; (2) every allocation, however small, took the one global lock and wrote the shared free lists and counters.

Changes (`bunko/kiso/aot/lowerlinux/mutators.mio`, `gc.mio`): the heap lock waits with a read-only test before the atomic exchange and has a cache line
of its own; the allocator raises a single `want` word when the volume reaches the threshold and polls read that word instead of the volume and
threshold; small allocations (24 to 56 bytes) pop from a per-thread list of exactly sized free blocks with no lock and no shared write, an empty list
being refilled with 64 blocks carved from one locked allocation. The blocks in a cache are ordinary free blocks (valid header, mark word 0), so a sweep
reclaims them like any dead block, and the collector empties every thread's lists after the sweep before it resumes the world. Single-threaded
programs are untouched (they compile to the same bytes).

| benchmark | before | after |
|---|---|---|
| 1 thread, 2M small allocations | 207 | 85 |
| 2 threads, 4M allocations | 900 | 148 |
| 4 threads, 8M allocations | 3150 | 290 |
| 4 producers + 4 consumers, 200000 messages through a 16-slot channel | 335 | 250 |
| 20000 threads started and joined one after the other | 640 | 650 (unchanged) |
| 200 rounds of 32 threads started together and joined | 135 | 135 (unchanged) |
| 20000 round trips between two threads | 200 | 185 |

Not claimed: the figures are one machine's; thread creation and join (about 32 microseconds each) and the channel handoff (about 5 microseconds per
wake-up) are unchanged and remain the next candidates; allocations above 56 bytes still take the global lock. The regression test is
`tests/threads/alloc_cache.mio` (five threads, every cached size class, rings of live objects verified after collections; a build that forgets to empty
the caches after a sweep hangs or corrupts it, which was checked by mutation).

### Efficiency: thread start and join on Linux x86-64

Starting and joining a thread cost about 32 microseconds. Each start mapped a fresh 1 MiB stack (mmap), protected its guard page (mprotect) and cloned
the thread; each join unmapped the stack (munmap, which also has to shoot down the other cores' TLB entries of the shared address space).

A joined thread's stack mapping, guard page included, is now kept in a pool of at most 64 mappings and the next start reuses one instead of mapping a
new one (`rt.stack_pool_take` / `rt.stack_pool_put`, protected by the heap lock; a full pool unmaps as before). The mutator record lives in the stack
mapping and holds the thread's allocation cache, so a recycled record is reset to zero before the new thread starts, exactly like a fresh mapping.

| benchmark (ms, median of 3, 8 hardware threads) | before | after |
|---|---|---|
| 20000 threads started and joined one after the other | 650 | 365 |
| 200 rounds of 32 threads started together and joined | 135 | 60 |

Regression test: `tests/threads/stack_reuse.mio` (200 rounds of three threads that recurse, allocate every cached size class and verify the objects
while collections run, on stacks that were used by earlier threads). A build that does not reset the recycled record faults on it, which was checked
by mutation. Not claimed: one machine's numbers; the remaining cost is the clone itself and the kernel's thread teardown.

