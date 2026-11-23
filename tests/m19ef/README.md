# M19ef native development witnesses

Production additions and these tests are Mio. Shell/Python may record evidence,
but never generate compiler/runtime/native machine code. Use the verified native v2.1 seed (bootstrap-v2.1.0-seed.1, mio-v2.1.0-darwin-arm64) with
SHA256 `be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79`.
The `--bunko-root` argument is mandatory until new resources are regenerated;
otherwise the seed will read its frozen embedded older library.

From this worktree, substitute the absolute verified seed path for `$SEED`:

```sh
"$SEED" test bunko/kiso/runtime/gc --bunko-root "$PWD/bunko" --format=json
"$SEED" test bunko/kiso/runtime/target --bunko-root "$PWD/bunko" --format=json
"$SEED" test bunko/kiso/runtime/cpu/aarch64 --bunko-root "$PWD/bunko" --format=json
"$SEED" test bunko/kiso/runtime/os/darwin --bunko-root "$PWD/bunko" --format=json
"$SEED" test bunko/kiso/aot/arm64 --bunko-root "$PWD/bunko" --format=json
"$SEED" test bunko/kiso/aot/macho --bunko-root "$PWD/bunko" --format=json
"$SEED" test bunko/kiso/aot/lower --bunko-root "$PWD/bunko" --format=json
"$SEED" build tests/m19ef/thread-smoke/main.mio -o /new/path/builder --bunko-root "$PWD/bunko"
/new/path/builder /new/path/smoke
codesign --verify --strict /new/path/smoke
/new/path/smoke
/new/path/builder /new/path/timeout timeout
/new/path/timeout
```

Use new output paths. Ordinary smoke must print exactly `thread-smoke-pass` and
exit0; intentionally missing wake must exit71 within the external test deadline.
The builder's signatures are generated entirely in Mio. `codesign` verifies only.

Each ordinary smoke creates/joins a genuinely distinct public pthread, verifies
thread identity and independent TLS values, a condition-variable park/wake,
200,000 contended base-ISA atomic increments, 10,000 bidirectional release/acquire
message publications with non-atomic payload protected by that publication,
strong CAS mismatch/success, monotonic clock/page-size query and resource cleanup.
Wait predicates are rechecked and relative timeout or bounded polling fails closed.
There is no managed allocation in the generated smoke. It is not a GC witness.

The GF4 fixture for the 11 native metadata tests is immutable historical test
input: `build/gofree-evidence/GF4/audit-fixtures/runtime_metadata.mio` in the
original repository. Its SHA256 is
`93d0965dd5698a248d663964ee86da2951c6148a9c5a04d6fe7613c9ba351d9a`.
To rerun it, copy current `bunko/kiso/aot/lower/*.mio` (excluding `_test.mio`)
into a new evidence directory, add that fixture, build this temporary package
with the seed and explicit current Bunko root, run its generated builder with a
new output directory, then execute all eleven images. This tests actual current
emitters with package-private access, never a replacement collector.

Protocol tests exercise semantic guards only. They do not implement native
per-mutator safepoints, concurrent tracing, SATB queues, sweep ownership or atomic
snapshots. Those capabilities must remain disabled until integrated and accepted.

## E3 real managed-heap runtime witness (2026-10-29)

`managed-roots/main.mio` calls the actual production heap initializer/allocator,
Minor/Major, exact marker, LOS and remembered barrier through the stopped heap
adapter. Both pthread mutators allocate; objects are shared and retained through
B alone, promoted without moving, and reclaimed after roots disappear. It also
checks blocked waits, exit/re-registration, coordinator-only roots, snapshot
capacity failure, stale epochs and controlled reuse without reading dead payload.

Build and run using fresh output paths:

```sh
"$SEED" build tests/m19ef/managed-roots/main.mio -o /new/path/managed-builder --bunko-root "$PWD/bunko"
/new/path/managed-builder /new/path/managed-native
codesign --verify --strict /new/path/managed-native
/new/path/managed-native > /new/path/managed-counters.bin
```

Success exits 0 and emits exactly 32 bytes: four little-endian uint64 values
`1000, 1002, 4, 131088` (retained rounds, Minor count, Major count, reclaimed LOS
physical bytes). An external deadline is mandatory; timeout is failure.
`build/m19ef-evidence/E3-managed-20261029-05/run.py` freezes sources and serially
runs 64 managed repetitions, native package tests, prior handshake/lifecycle and
11 historical production metadata witnesses. Copy this audit controller into a
new evidence directory to rerun; keep its outputs and failed cases.

**Scope:** runtime-IR fixture with production layouts and collector; ordinary
language-codegen threading/TLS/safepoint integration is still outstanding.
E3 remains IN_PROGRESS. No SATB or concurrent Mark/Sweep acceptance is implied.
See `specs/002-m19ef-portable-pure-mio/E3_REPORT.md`.

## Automatic ordinary main-thread roots (2026-10-30)

`automatic-roots/builder.mio` uses the real source frontend and the internal
`lower.compile_with_attached_roots` path, public Darwin imports and Mio signing.
The ordinary source samples have no manual Root/TLS calls. Main attaches to the
v3 registry; compiler-generated exact frames publish to actual TLS and poll at
solved opcode boundaries. Diagnostic stderr is eight uint64 counters, documented
in the checkpoint report. This does not yet enable compiled managed user workers.

Run all same-source scoped tests serially from this worktree:

```sh
python3 tests/m19ef/automatic-roots/run.py build/m19ef-evidence/E3-auto-roots-NEW
```

Use a nonexistent output directory. The audit-only controller preserves a source
snapshot, SHA256 Manifest, commands/exits/deadlines, stdout/stderr, CPU/wall/RSS,
actual native binaries/signature checks, all failures and source-unchanged proof.
It also runs `main-attach/main.mio` (five failure points, real cleanup and reuse),
32 package tests, 64 prior shared-heap fixtures and 11 historical GC witnesses.

Completed scoped evidence: `E3-auto-roots-20261030-08/`; separate attached-path
2048-global/2049-rejection evidence: `E3-auto-globals-20261030-01/`.
Full boundaries and continuation:
`specs/002-m19ef-portable-pure-mio/autonomous-e3-e6/E3_CHECKPOINT_20261030.md`.
E3 IN_PROGRESS, E4/E5/E6 NOT_RUN, M19e NOT_ACCEPTED; no new fixed point claimed.

## Ordinary compiled Worker integration (2026-10-30)

`user-workers/builder.mio` compiles real ordinary Mio source callbacks through
`compile_with_worker_bridge`; callbacks never publish manual roots. The four
internal bridge stubs supply start/join/wait/collect. Run the serial frozen
regression with a fresh output directory:

```sh
python3 tests/m19ef/user-workers/run.py build/m19ef-evidence/E3-workers-NEW
```

Exact scope, failure reproductions, trace45-word format implementation and
remaining limits: `specs/002-m19ef-portable-pure-mio/autonomous-e3-e6/E3_WORKER_PROGRESS_20261030.md`.
E3 IN_PROGRESS; E4/E5/E6 NOT_RUN; M19e NOT_ACCEPTED.

E3 candidate16 now passes G3.1–G3.7 for the bounded internal native user-function
bridge. See `specs/002-m19ef-portable-pure-mio/autonomous-e3-e6/E3_FINAL_REPORT.md`.
E4–E6 and M19e are not accepted by that E3 result.
