# Six-platform parity matrix

Generated from `docs/parity/matrix.json` by `scripts/ci/parity_matrix.py render`; do not edit by hand.

Evidence: https://github.com/mio-kobo/mio/actions/runs/42969371443, head `d265bb0`. PR #72 head after merging Mio-2.2.0; Mio-2.2.0 at 94316df additionally contains the async session's reactor (#75): its CI evidence is its own PR run; darwin-arm64 groups I, J and K cite their own run 43166577034 (PR #92 head c5b3b54, macos-15)

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
| I native multithreading | VALIDATED | UNSUPPORTED | UNSUPPORTED | VALIDATED | UNSUPPORTED | UNSUPPORTED |
| J thread synchronisation | VALIDATED | UNSUPPORTED | UNSUPPORTED | VALIDATED | UNSUPPORTED | UNSUPPORTED |
| K channels | VALIDATED | UNSUPPORTED | UNSUPPORTED | VALIDATED | UNSUPPORTED | UNSUPPORTED |
| L nonblocking TCP | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| M Reactor / event loop | VALIDATED | VALIDATED | VALIDATED | VALIDATED | UNSUPPORTED | UNSUPPORTED |
| N error handling and resource lifecycle | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |
| O standard library behaviour | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED | VALIDATED |

## Parity backlog (14 entries)

* **linux-arm64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 2: polls, park protocol and thread natives in AArch64 (Stage B)
* **linux-arm64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 2: polls, park protocol and thread natives in AArch64 (Stage B)
* **linux-arm64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 2: polls, park protocol and thread natives in AArch64 (Stage B)
* **darwin-amd64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 3: after darwin-arm64
* **darwin-amd64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 3: after darwin-arm64
* **darwin-amd64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 3: after darwin-arm64
* **windows-amd64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 4: CreateThread/WaitOnAddress runtime
* **windows-amd64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 4: CreateThread/WaitOnAddress runtime
* **windows-amd64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 4: CreateThread/WaitOnAddress runtime
* **windows-amd64 / M Reactor / event loop** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
* **windows-arm64 / I native multithreading** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 5: after windows-amd64
* **windows-arm64 / J thread synchronisation** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 5: after windows-amd64
* **windows-arm64 / K channels** (UNSUPPORTED): Fails closed: the target rejects programs that use the thread natives (compile error or a loud run-time unsupported); there is no single-thread fallback. Next: Priority 5: after windows-amd64
* **windows-arm64 / M Reactor / event loop** (UNSUPPORTED): Fails closed; CI records NOT_RUN (never PASS). Owned by the async session: wait for its Phase D PR before building non-Linux backends here. Next: after the async session's backend PR for this target
