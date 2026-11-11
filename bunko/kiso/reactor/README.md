# `kiso/reactor`

Readiness reactor for Mio: level-triggered **epoll** on Linux AMD64, written in Mio over five OS primitives of `mio/sys/socket`
(`reactor_open`, `reactor_ctl`, `reactor_wait`, `reactor_signal`, `reactor_ack`). It tells you *when* a nonblocking operation of
[`kiso/net/tcp_async`](../net/tcp_async/README.md) can make progress; it never performs I/O itself.

Status: **linux-amd64 only** (Implemented, tested locally with native epoll over real loopback sockets). On other targets the primitives
fail closed (an error, never a silent success); CI reports NOT_RUN there. The Darwin (kqueue) and Windows (IOCP) backends are phase D.

| call | purpose |
|---|---|
| `open()` | epoll instance plus an eventfd for wakeups; `OpenResult{reactor, error}` |
| `register(r, handle, interest)` | watch a socket handle (`readable = 1`, `writable = 2`, or 3); returns a `Registration{slot, generation}` |
| `modify(r, registration, handle, interest)` | change the interest of a live registration |
| `deregister(r, registration, handle)` | stop watching; the slot is released even if the socket was already closed |
| `wait(r, timeout_ms, max)` | **the only blocking call**: -1 sleeps until an event or a wake, 0 polls, up to `max` (1..64) events; `WaitResult{events, woken, stale, error}` |
| `wake(r)` | make a pending or future `wait` return with `woken`; wakes coalesce |
| `close(r)` | release the kernel objects and invalidate every registration |

Guarantees (each covered by a native test in `reactor_test.mio`):

* **No lost wakeups, no busy polling.** Level-triggered: a descriptor stays ready until drained to EAGAIN. `wait(r, 60, ...)` really sleeps
  (the test measures >= 50 ms) and a wake returns a 5 s wait at once.
* **No stale delivery.** The kernel only holds `generation * 1048576 + slot + 1`. An event whose generation no longer matches a live registration
  (deregistered slot, reused descriptor, event still in flight) is dropped and counted in `stale`; a stale `Registration` can neither modify nor
  deregister the slot's new owner.
* **No double completion.** Each readiness is reported once per `wait` while it persists; the owner drains it before the next.
* **Resources.** `close` frees the epoll and eventfd descriptors; closing twice or waiting on a closed reactor reports `Closed`.

```mio
yobu "kiso/net/tcp";
yobu "kiso/net/tcp_async";
yobu "kiso/reactor";

waza watch(listener: tcp_async.AsyncListener?) -> unit {
    moshi (listener == mu) { kaesu; }
    kaku opened = reactor.open().reactor;
    moshi (opened == mu) { kaesu; }
    kaku r = opened;
    kaku registered = reactor.register(r, listener.handle, reactor.readable);
    chant(registered.error == mu);                 // hai
    chant(len(reactor.wait(r, 0, 8).events));       // 0: no connection yet, and the poll did not block
    reactor.close(r);
    tcp_async.close_listener(listener);
}

watch(tcp_async.listen(tcp.ipv4(uint8(127), uint8(0), uint8(0), uint8(1), 18082), 4).listener);
```

Tests: `mio test bunko/kiso/reactor`. The package uses `kiso/os` (monotonic clock), which the Go reference VM does not provide, so the
test package runs natively only; the primitives themselves were compared between the VM and a native build with an equivalent program
(identical output).
