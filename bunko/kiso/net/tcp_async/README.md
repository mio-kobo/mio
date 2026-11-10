# `kiso/net/tcp_async`

Nonblocking IPv4 TCP for Mio, additive to the blocking [`kiso/net/tcp`](../tcp/README.md) (which is unchanged).
Every operation returns immediately; an operation that cannot make progress reports `pending = hai` and may be
called again later. Nothing in this package waits, sleeps or spins: deciding *when* to retry is the job of a reactor
(see [docs/async-networking.md](../../../../docs/async-networking.md), phase B).

Status: **linux-amd64 only** (Implemented, tested locally with native loopback sockets). On other targets a program that
imports this package is rejected at build time (AArch64 targets) or reports an error at run time; CI reports NOT_RUN there.

```mio
yobu "kiso/array";
yobu "kiso/net/tcp";
yobu "kiso/net/tcp_async";

waza demo(listener: tcp_async.AsyncListener?) -> unit {
    moshi (listener == mu) { kaesu; }
    kaku attempt = tcp_async.accept(listener);          // pending = hai: nobody connected yet, nothing blocked
    chant(attempt.pending);
    tcp_async.close_listener(listener);
}

demo(tcp_async.listen(tcp.ipv4(uint8(127), uint8(0), uint8(0), uint8(1), 18081), 16).listener);
```

| call | result fields | notes |
|---|---|---|
| `listen(address, backlog)` | `listener`, `error` | bound and listening, nonblocking |
| `accept(listener)` | `connection`, `pending`, `error` | accepted socket is nonblocking |
| `connect(address)` | `connection`, `pending`, `error` | `pending` → call `finish_connect` until it stops reporting pending |
| `finish_connect(connection)` | `pending`, `error` | only after `connect` reported pending; on failure the socket is already closed |
| `read(c, buffer, offset, length)` | `count`, `eof`, `pending`, `error` | `count` is exact; `eof` = peer closed |
| `write(c, buffer, offset, length)` | `count`, `pending`, `error` | may be partial; resume at `offset + count` |
| `close(c)`, `close_listener(l)` | `pending`, `error` | closing twice reports `Closed` |

Rules: partial progress is exact; `pending`, `eof` and `error` are mutually exclusive; a handle that is not returned has
already been closed on every error path; closed or stale handles report `Closed` and never affect a recycled descriptor.
`AsyncError` is `InvalidArgument | Closed | TimedOut | ConnectionRefused | ConnectionReset | AddressInUse | PermissionDenied | Other(string)`;
would-block and in-progress are never errors.

The executable examples are the tests in `tcp_async_test.mio` (echo, partial writes, refusal, leak check):
`mio test bunko/kiso/net/tcp_async`.
