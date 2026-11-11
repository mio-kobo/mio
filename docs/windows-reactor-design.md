# Windows reactor (IOCP): design options and recommendation

Status: **decision needed from the maintainer** before the windows-amd64 and windows-arm64 reactor (group M) is implemented. Nonblocking TCP (group L) on windows-amd64 does not depend on this (see the nonblocking-TCP PR).

## The constraint

`kiso/reactor` is a *readiness* contract: `submit(handle, interest)`, `poll(timeout)`, `cancel`, `wake`, `close`, level-triggered, with `Operation{id, slot, generation}` and stale-event rejection (docs/async-networking.md section 3). Linux (epoll) and macOS (kqueue) implement it directly. Windows IOCP is a *completion* mechanism: an overlapped operation (`WSARecv`, `WSASend`, `AcceptEx`, `ConnectEx`) is started with an `OVERLAPPED` and a buffer, and the kernel owns both until a completion packet is dequeued from the port. The directive forbids treating completion as readiness by renaming it, and forbids freeing an `OVERLAPPED` or buffer while the kernel may still use it (including after cancellation).

## Options

**A. `WSAPoll` readiness reactor (not IOCP).** Same contract, simplest code, no overlapped state. Costs: linear scan per poll, no completion port, the wake-up needs a loopback socket pair, and `WSAPoll` historically did not report a failed nonblocking connect until recent Windows 10 builds. It does not give an IOCP backend, which the platform plan names.

**B. Readiness over IOCP with zero-byte overlapped operations (recommended).** Same public API and semantics as the other backends. A read interest arms a zero-byte `WSARecv`, a write interest a zero-byte `WSASend`; their completion means "readable / writable" (a zero-byte receive completes when data or EOF is available, a zero-byte send when buffer space exists). Level-triggered behaviour is obtained by re-arming immediately after delivering an event, so a socket that is not drained completes again at once. The adapter owns one `OVERLAPPED` per (operation, direction) in memory the collector never moves or frees, and releases it only after its completion packet has been dequeued: `cancel` calls `CancelIoEx`, then waits for the cancelled completion (or marks the record "dying" and releases it on dequeue) before the slot is reused; the generation in the completion key rejects late packets. `wake` is `PostQueuedCompletionStatus`, the timeout is the `GetQueuedCompletionStatusEx` timeout, closing a socket cancels its pending operations first. Listeners use a zero-byte-style `AcceptEx` probe or a readiness check before `accept`. New imports: `CreateIoCompletionPort`, `GetQueuedCompletionStatusEx`, `PostQueuedCompletionStatus`, `CancelIoEx`, `WSARecv`, `WSASend`, `CloseHandle`.

**C. Completion API (true IOCP).** Add a second public layer, for example `kiso/net/tcp_completion`, where the caller submits `read(buffer)` / `write(buffer)` / `accept` / `connect` and receives completions; epoll and kqueue would implement it by readiness plus retry. This is the model a future task scheduler (phase C) could use directly and it avoids the re-arm traffic of B, but it is a new public API and the largest change: buffers must stay valid and unmoved for the whole operation (the collector is non-moving, so a pinned array works, but ownership of a buffer with a pending operation has to be specified together with `yuzuru` and cancellation).

## Recommendation

Implement **B** for group M on windows-amd64, then reuse it on windows-arm64 through the existing Windows adapter. It keeps one reactor contract on all six targets, uses a real completion port, and keeps the pending-operation lifetime rules local to the backend. Keep **C** as a separate proposal for the scheduler phase. Do not ship A as the Windows backend: it would be a different mechanism under the same name.

## Tests that B must pass (native, on the Windows runners)

The whole of `bunko/kiso/reactor/reactor_test.mio` and `tests/reactor/run.py` (the same suite as the other targets), plus Windows-specific cases: cancel of a pending zero-byte operation followed by immediate close and slot reuse (no late packet reaches the new owner, no use of a freed `OVERLAPPED`), a peer reset while an operation is armed, `wake` from another thread racing `poll`, and a thousand armed sockets idle without CPU.

## Question for the maintainer

Approve B (or choose A or C) for the Windows reactor. Until then windows-amd64 and windows-arm64 group M stay `UNSUPPORTED` (fail closed, explicit `Unsupported`), and the parity matrix says so.
