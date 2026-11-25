# Native differential corpus

The VM / native differential programs formerly embedded in the Go harness `internal/aot/linux_purelower_test.go`
(`pureLowerPrograms`), now plain Mio files run on every native target by
`tests/runtime-abi/run.py --suite differential` (called from `scripts/ci/native_platforms.py`).

`<name>.expected` is the reference VM's stdout (`tests/windows-amd64/refvm`), `<name>.exit` its exit class (`0` or
`nonzero`). A native image must print exactly the same bytes and have the same exit class, in the default
configuration and built with `mio build --gc-stress`. `gc_garbage` and `gc_keeps_live_data` run in the default
configuration only (quadratic under per-allocation collection, as in the former Go harness); they are reported
`NOT_APPLICABLE` for `--gc-stress`, never PASS.

Cases in `cases/compile-errors` must fail before execution. Each `.diagnostic`
file records the expected diagnostic code, line, and column as a JSON array.
The runner compares the structured diagnostic from the native build and the
Go reference checker.
