# Mio 2.2.0 stabilization ledger

Base commit for every entry: `dcae4c111101f6af95d48c08d1510433475ae762` (origin/Mio-2.2.0, 2026-11-12).
Evidence classes: **executed** = observed by running the code on linux-amd64 (native compiler built from CI artifact run 43171006972, or the Go
reference frontend); **static** = read from source only, not run. Statuses: CONFIRMED, ALREADY_FIXED, NOT_REPRODUCED, DESIGN_DECISION.
Platforms other than linux-amd64 were not available locally; their cells say NOT_RUN unless CI evidence is cited in the PR.

| # | Finding | Backend / platform | Status | Evidence | Fix PR | Regression test |
|---|---|---|---|---|---|---|
| 1a | Self-hosted checker does not narrow in `aida` bodies | self-hosted frontend (all targets) | CONFIRMED | executed (differential test) | #95 | `TestNullableLoopNarrowing`, `test_nullable_loop_narrowing` |
| 1b | Both frontends keep a pre-loop non-null fact across the loop backedge (new finding) | Go and self-hosted frontends | CONFIRMED | executed | #95 | same |
| 1c | Equality accepted on arrays, `mu == mu`, `unit` by self-hosted checker; Go rejects (tests in `cmd/mio` assert it) | self-hosted frontend | CONFIRMED | executed | #96 | `TestEqualityOperandContract` |
| 1d | Struct (also enum, Map, function, same-type nullable) equality: Go rejects by an undocumented scalar/string whitelist, self-hosted accepts | both frontends | **DESIGN_DECISION** | executed difference; native lowering of an accepted struct `==` not inspected | none | none |
| 2 | Nullable scalar Map values: storing a nullable variable holding `mu`, or overwriting with `mu`, traps `nil reference`; a literal `mu` was stored as raw 0 and read back as non-null zero | `lowerlinux`, `lowerwinrt` (shared code) | CONFIRMED (linux-amd64 executed; windows-amd64 NOT_RUN locally) | executed | #97 | `maps_nullable`, `gc_maps_nullable` differential cases |
| 2b | Same concern in `lower/map.mio` (arm64: darwin-arm64, linux-arm64, windows-arm64) | arm64 lowering | NOT_REPRODUCED locally (no runner); the new differential cases run there in CI | static only | #97 CI | same cases |
| 3a | `channel_new` stores an invalid `words_new` result; first send faults at `0xffffffffffffffea` | linux-amd64 | CONFIRMED | executed | #98 | `tests/threads/channel_alloc_failure` |
| 3b | `rt.words_new(2^61+1)` wraps `count*8` and returns a valid pointer | linux-amd64 | CONFIRMED | executed | #98 | same |
| 3c | Darwin ARM64 control-word pool: 16384 blocks, never reused, 16385th `words_new` returns -12 (worker slots, 128, are a separate limit) | darwin-arm64 | CONFIRMED by source and its own comment; exhaustion NOT executed | static | #98 makes the failure safe; reclamation deliberately not added (stale-handle hazard) | `channel_alloc_failure` is not wired into the darwin suites |
| 4 | Blocking socket read/write/accept/connect and reactor poll keep the thread in the running state, so a collection started by another thread does not complete | linux-amd64 multi-mutator | CONFIRMED (each of the four stalls past a 15 s watchdog; not proof of an infinite wait) | executed | #99 | `tests/threads/socket_gc_progress` |
| 4b | Other safepoint-unaware blocking calls: fs read/write natives, os poll/wait4 | linux-amd64 | CONFIRMED by source only | static | none (follow-up) | none |
| 5a | `thread.send` of a mutable graph accepted when the program has no `yuzuru` | self-hosted frontend | CONFIRMED | executed (`mio check`) | #100 | `reject_send_*`, `channel_send_boundary` |
| 5b | Closure captures of `thread.start` are not checked | self-hosted frontend | CONFIRMED (accepted by `mio check`) / **DESIGN_DECISION** (capture rule needed) | executed | none | none |
| 6 | Native Map lookup is a linear walk of one list | linux-amd64, windows-amd64 (`lower*/maps.mio`) proposed fix; arm64 (`lower/map.mio`) unchanged | CONFIRMED; #102 proposes a hash table on the x86-64 lowerings | executed on linux-amd64 (scaling table in #102); windows-amd64 and darwin-arm64 CI only for the new case; arm64 `lower/map.mio` still linear (static) | #102 | `maps_hashing` differential case |
| 7 | Runtime traps print only the message (`runtime error: index out of range`), no file/line/column | native runtimes | CONFIRMED | executed | none (proposal below) | |
| 8 | `mio fmt` exists in the Go reference CLI (`cmd/mio/fmt_command.go`, `internal/format`); the native CLI `help` lists no `fmt` | native CLI | CONFIRMED gap | executed (`mio help`) | none | |

## Design decisions requested from the maintainer

1. **Struct equality** (#96 leaves it unchanged): non-comparable (reject in the self-hosted checker: smallest change, matches the Go frontend), or a defined equality?
2. **Channel creation failure API** (#98): `channel_new` keeps its signature and returns a dead channel on failure; additive `channel_try_new` / `channel_valid` report it. Confirm the names (they sit under the provisional D-2 names) or prefer another shape.
3. **Capture rule for `thread.start`** (#100): which closure captures are allowed (only scalars/strings/channels, or captured graphs count as transferred)?
4. Whether control-block reclamation should be pursued (needs generation-checked handles; nothing reclaims today by design).

## Proposals recorded, not implemented (compatibility consequences)

* **Runtime source locations.** Traps carry no location. A narrow route: the x86-64 inline frame descriptors already identify every call site; add a (file index, line, column) triple per descriptor behind a flag, and have `rt.trap.*` print the innermost managed frame's location. Costs image size, changes the descriptor layout (RA-1 ABI gate), so it needs its own PR and ABI review. No DWARF/DAP.
* **Formatter in the native CLI.** The reference formatter exists; porting or wrapping it for the native CLI is a tooling task, separate from runtime stabilization. Semantic rename stays a separate tooling task.
* **Language ergonomics** (mutable struct fields, `Result`/`Option`, changing `FileResult`, new ownership syntax): not changed. `m[key]` and missing-key behavior are unchanged by #97.

## CI evidence at 2026-11-13

This section records observations, not merge approval. A passing run on an older PR merge ref does not validate the PR combined with the current target.

* At the initial audit, the latest submitted commits of #95, #96, #97, #98, #99, #100 and #102 had successful six-platform Native runs. The tested merge refs used older target revisions. Any subsequent fix or branch synchronization needs new checks.
* #101 latest commit `80b54eaa` failed Native run `43357433562`: Darwin `channel_races/normal` exceeded the 180-second watchdog. Distribution run `43357433631` also failed on Darwin. These are actual execution failures, not a Windows package-manager infrastructure result.
* Similar Darwin timeouts occurred on other PRs, including #104. Documentation-only changes make a shared runtime issue plausible, but neither a rerun nor a documentation diff establishes the cause. Keep the failure visible until investigated and validated on the current target; do not classify it as harmless flakiness.
* Integration review found that #99 pins at record offsets 88/96 overlap the allocation cache at 88..127 introduced by #103. The PR branch requires a non-overlapping layout and combined allocation/blocking/GC regression checks before merging.
* A cancelled, skipped or unacquired job is NOT_RUN and is never counted as a pass. All design proposals above remain unapproved until the maintainer decides; merging this ledger does not implement or approve them.
