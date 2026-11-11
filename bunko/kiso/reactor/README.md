# `kiso/reactor`

OS-independent readiness reactor for Mio with a real **Linux AMD64 epoll backend** (level-triggered epoll plus an eventfd for wake-ups),
written in Mio over five OS primitives of `mio/sys/socket` (`reactor_open`, `reactor_ctl`, `reactor_wait`, `reactor_signal`,
`reactor_ack`). It tells you *when* a nonblocking operation of [`kiso/net/tcp_async`](../net/tcp_async/README.md) can make progress; it never
performs I/O itself and it is separate from any task scheduler.

Status: **Linux AMD64 only** (Implemented, tested locally and in CI with native epoll over real loopback sockets). On every other
target `open()` returns `Unsupported` (the primitives fail closed); CI reports NOT_RUN there. kqueue (Darwin) and IOCP (Windows) are
planned (docs/async-networking.md, phase D), not implemented.

## Contract

| call | purpose |
|---|---|
| `open()` | epoll instance plus a wake-up eventfd; `OpenResult{reactor, error}` (`Unsupported` where there is no backend) |
| `submit(r, handle, interest)` | watch a socket handle for `readable` (1), `writable` (2) or 3; returns `SubmitResult{operation, error}` |
| `update(r, operation, handle, interest)` | change the interest of a live operation |
| `poll(r, timeout_ms, max)` | **the only blocking call**: `-1` sleeps until an event or a wake, `0` does not wait, up to `max` (1..64) events; `PollResult{events, woken, stale, error}` |
| `cancel(r, operation, handle)` | stop watching; the slot is released even if the socket was already closed |
| `current(r, event)` | is this event's operation still live? (an operation may be cancelled after `poll` returned its event) |
| `wake(r)` | make a pending or future `poll` return with `woken`; wakes coalesce; safe from another thread |
| `close(r)` | release both kernel descriptors and invalidate every operation |

`Operation{id, slot, generation}`: `id` is unique within a reactor and never reused; `slot` is recycled, `generation` is not. The kernel only holds the
token `generation * 1048576 + slot + 1` (0 is the wake-up descriptor).

## Guarantees (each has a native test)

* **No stale delivery.** An event whose generation or id no longer matches a live operation (cancelled, descriptor reused, still in flight in the kernel) is dropped and counted in `stale`. A cancelled or stale `Operation` can neither update nor cancel the slot's new owner.
* **Cancellation.** After `cancel` the operation is never delivered again; cancelling twice reports `Closed`.
* **Closing.** Cancel before closing the resource; the other order is also safe (the kernel drops the registration when the descriptor closes and `cancel` still releases the slot without touching whoever got the descriptor next).
* **No lost wakeups, no busy polling.** Level-triggered readiness persists until drained to EAGAIN. `poll(r, 60, ...)` sleeps (measured >= 50 ms); a wake from another OS thread ends `poll(r, -1, ...)`; 20 consecutive wakes are each observed.
* **EINTR** (signal interruption) returns zero events early and is never an error: poll again.
* **Resources.** No raw descriptors are exposed. The reactor keeps the watched socket handle reachable while its operation is pending (a managed reference), so the collector cannot reclaim it under a pending operation (tested under `--gc-stress`). `close` frees the epoll and eventfd descriptors; closing twice reports `Closed`.

```mio
yobu "kiso/net/tcp";
yobu "kiso/net/tcp_async";
yobu "kiso/reactor";

waza watch(listener: tcp_async.AsyncListener?) -> unit {
    moshi (listener == mu) { kaesu; }
    kaku opened = reactor.open().reactor;
    moshi (opened == mu) { kaesu; }
    kaku r = opened;
    kaku submitted = reactor.submit(r, listener.handle, reactor.readable);
    chant(submitted.error == mu);                  // hai
    chant(len(reactor.poll(r, 0, 8).events));       // 0: no connection yet, and the poll did not block
    reactor.close(r);
    tcp_async.close_listener(listener);
}

watch(tcp_async.listen(tcp.ipv4(uint8(127), uint8(0), uint8(0), uint8(1), 18082), 4).listener);
```

Tests: `mio test bunko/kiso/reactor` (13 native tests) and `python3 tests/reactor/run.py --mio <cli>` (repeated stress, `--gc-stress`,
a 1000-connection idle-efficiency load test and a SIGSTOP/SIGCONT EINTR test; prints the measured numbers). The test package uses `kiso/os` and OS threads, which
the Go reference VM does not provide, so it runs natively only.
