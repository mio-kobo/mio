# kiso/aot/linuxarm64

Pure Mio Linux AArch64 static-image builder (first slice of the linux-arm64 backend).

`hello_image(message, status)` returns an ELF64 `ET_EXEC`/`EM_AARCH64` image (via `elf.executable_aarch64`) whose code is
`write(1, message, len)` followed by `exit_group(status)` using the Linux generic syscall table (`write`=64,
`exit_group`=94; these differ from x86-64). The message follows the code and is addressed with `ADR`, so no relocation is
needed. No libc, no loader.

This is not yet a compiler target: `mio build --target linux-arm64` is still rejected with `E_TARGET`.

Tests: `linuxarm64_test.mio` (hand-computed instruction words) and `check_linuxarm64.py` (struct parse, readelf,
objdump, native execution on Linux aarch64; `NOT_RUN` elsewhere, never emulated).
