# Six-platform parity matrix

Generated from `docs/parity/matrix.json` by `scripts/ci/parity_matrix.py render`; do not edit by hand.

Evidence: https://github.com/mio-kobo/mio/actions/runs/42969371443, head `d265bb0`. PR #72 head after merging Mio-2.2.0; Mio-2.2.0 at 94316df additionally contains the async session's reactor (#75): its CI evidence is its own PR run

Only **VALIDATED_BY_CI** counts toward parity (native execution on that platform). UNSUPPORTED, NOT_RUN, IN_PROGRESS and BLOCKED never do.

| feature | linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64 | windows-amd64 | windows-arm64 |
|---|---|---|---|---|---|---|
| A native compiler build | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| B native CLI | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| C self-hosting fixed point | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| D Full and Core distributions | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| E runtime ABI | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| F GC correctness | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| G memory management and root tracking | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| H blocking TCP | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| I native multithreading | VALIDATED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED |
| J thread synchronisation | VALIDATED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED |
| K channels | VALIDATED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED | UNSUPPORTED |
| L nonblocking TCP | VALIDATED | VALIDATED | UNSUPPORTED | VALIDATED | UNSUPPORTED | UNSUPPORTED |
| M Reactor / event loop | VALIDATED | VALIDATED | UNSUPPORTED | VALIDATED | UNSUPPORTED | UNSUPPORTED |
| N error handling and resource lifecycle | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| O standard library behaviour | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |

## Parity backlog (21 entries)

* **linux-arm64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 2: polls, park protocol and thread natives in AArch64 (Stage B)
* **linux-arm64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 2: polls, park protocol and thread natives in AArch64 (Stage B)
* **linux-arm64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 2: polls, park protocol and thread natives in AArch64 (Stage B)
* **darwin-amd64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 3: after darwin-arm64
* **darwin-amd64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 3: after darwin-arm64
* **darwin-amd64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 3: after darwin-arm64
* **darwin-amd64 / L nonblocking TCP** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
* **darwin-amd64 / M Reactor / event loop** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
* **darwin-arm64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 1, planned slices (docs/threading-stages.md, Stage Three): route thread-native programs through the existing Darwin worker bridge, map start/join with the Stage One error contract, implement the word natives, then enable tests/threads on macos-15
* **darwin-arm64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 1, planned slices (docs/threading-stages.md, Stage Three): route thread-native programs through the existing Darwin worker bridge, map start/join with the Stage One error contract, implement the word natives, then enable tests/threads on macos-15
* **darwin-arm64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 1, planned slices (docs/threading-stages.md, Stage Three): route thread-native programs through the existing Darwin worker bridge, map start/join with the Stage One error contract, implement the word natives, then enable tests/threads on macos-15
* **windows-amd64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 4: CreateThread/WaitOnAddress runtime
* **windows-amd64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 4: CreateThread/WaitOnAddress runtime
* **windows-amd64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 4: CreateThread/WaitOnAddress runtime
* **windows-amd64 / L nonblocking TCP** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
* **windows-amd64 / M Reactor / event loop** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
* **windows-arm64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 5: after windows-amd64
* **windows-arm64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 5: after windows-amd64
* **windows-arm64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 5: after windows-amd64
* **windows-arm64 / L nonblocking TCP** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
* **windows-arm64 / M Reactor / event loop** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
