# Mio 2.1 examples

Small, standalone programs for learning Mio. Start with `hello.mio`, then follow the topics that interest you. These examples use the Mio 2.1 syntax and Bunko APIs, not the historical AOT milestone generators.

Run commands from the **repository root** with the `mio` command on your `PATH`:

```sh
mio run examples/hello.mio
mio run examples/arrays.mio
mio run examples/math.mio
mio check examples/structs_and_enums.mio
mio test examples/tests
# Verify the library with Python 3.12 and an existing native Mio CLI:
python3 examples/check.py --mio mio
```

Full distributions include Bunko. A Core distribution can run the examples marked **Core**; the others need a separately supplied Bunko root (`--bunko-root <directory>` or `MIO_BUNKO`) when using Core. The same source files can be built into native programs with `mio build examples/hello.mio -o hello` (Windows outputs `hello.exe`).

## Pick an example

| File | Topic | Requirements |
| --- | --- | --- |
| [hello.mio](hello.mio) | Your first output | Core |
| [variables_and_conditionals.mio](variables_and_conditionals.mio) | Variables and `moshi`/`sore` | Core |
| [control_flow.mio](control_flow.mio) | `aida`, `tsuzuku`, `nukeru` | Core |
| [local_scope.mio](local_scope.mio) | Local scopes and shadowing | Core |
| [functions.mio](functions.mio) | Parameters, return values, `unit` | Core |
| [recursion.mio](recursion.mio) | Recursive functions | Core |
| [nullable.mio](nullable.mio) | Nullable values and `mu` | Core |
| [structs_and_enums.mio](structs_and_enums.mio) | `kata`, `sentaku`, `awase` | Core |
| [generics.mio](generics.mio) | Generic functions and types | Core |
| [closures.mio](closures.mio) | Captures and function values | Core |
| [maps.mio](maps.mio) | Maps and optional lookups | Core |
| [arrays.mio](arrays.mio) | Dynamic arrays and copying | Full (`kiso/array`) |
| [math.mio](math.mio) | Math and numeric conversion | Full (`kiso/math`, `kiso/convert`) |
| [strings_and_utf8.mio](strings_and_utf8.mio) | UTF-8, code points and builders | Full (`kiso/string`) |
| [path.mio](path.mio) | Logical path handling | Full (`kiso/path`) |
| [json.mio](json.mio) | Parsing a JSON object | Full (`kiso/encoding/json`, `kiso/string`) |
| [regexp.mio](regexp.mio) | Regular-expression matching | Full (`kiso/regexp`) |
| [sha256.mio](sha256.mio) | SHA-256 digest bytes | Full (`kiso/crypto/sha256`, `kiso/string`) |
| [filesystem_read.mio](filesystem_read.mio) | Read and decode a file | Full (`kiso/fs`, `kiso/string`) |
| [os_arguments.mio](os_arguments.mio) | Command-line arguments | Full (`kiso/os`) |
| [tests/basic_test.mio](tests/basic_test.mio) | A first `mio test` suite | Full (`kiso/testing`) |

Every program above is non-interactive. `filesystem_read.mio` only reads [data/greeting.txt](data/greeting.txt) and must be run from the repository root. `os_arguments.mio` prints the argument count, including `argv[0]`, followed by the supplied arguments.

## TCP echo (macOS, Linux and Windows)

The [server](tcp_echo_server.mio) and [client](tcp_echo_client.mio) use the same `kiso/net/tcp` API and loopback address `127.0.0.1:18080` on each target:

1. Run `mio run examples/tcp_echo_server.mio` in one terminal.
2. Run `mio run examples/tcp_echo_client.mio` in another terminal.

Pass the same port to both commands after `--` to use a different loopback port. The server accepts one client, reads exactly three bytes, echoes them, then exits. Both peers handle short reads and partial writes. Expected output after a successful exchange:

| Server terminal | Client terminal |
| --- | --- |
| `hai` | `hai` |
| `Mio TCP server waiting on 127.0.0.1` | `3` |
| `3` | `hai` |
| `hai` | `3`, `hai`, `77`, `105`, `111` (one per line) |

Full CI requires both TCP examples to pass on all three native targets.

The [Windows PE generator](windows-amd64/hello.mio) writes `hello-windows.exe` in its working directory. The suite runs the generator and validates its output on all three platforms. On Windows it also executes the generated PE and checks its stdout and exit status.

Historical AOT M0–M7, M11A and M17 compiler experiments have been removed from this user-facing directory. They remain recoverable through Git history. Current compiler correctness tests live under `tests/` and Bunko's package test suites.
