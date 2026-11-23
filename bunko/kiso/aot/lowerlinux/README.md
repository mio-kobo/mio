# kiso/aot/lowerlinux

Pure-Mio lowering of Mochi IR to a static Linux x86-64 ELF64 executable (no libc, no linker, no VM).
It sits next to `kiso/aot/lower` (the Darwin ARM64 backend) and reuses the target-neutral type analysis
`kiso/aot/lower/types`, the x86-64 encoder `kiso/aot/x86_64` and the ELF writer `kiso/aot/elf`.

Files: `lowerlinux.mio` (opcodes, functions, closures), `runtime_objects.mio` (arrays, strings, UTF-8),
`runtime_fs.mio` (NativeFS: files, directories, stat), `runtime_os.mio` (arguments, environment, exit, spawn),
`gc.mio` (allocator, precise mark & sweep collector, crash reporting), `runtime_testing.mio` (test report channel), `runtime_socket.mio` (IPv4 TCP sockets), `maps.mio` (Map).

Execution model: the Mochi operand stack is the native stack; every function has an rbp frame (arguments pushed by the
caller). Heap objects start with a 16-byte header (kind | size << 8, mark word). Nullable scalars are boxed.
The collector runs at function entry and loop back-edges; each call/poll site is followed by an inline frame descriptor
(`jmp over; count; rbp offsets`) that lists the reference slots of that frame, so roots are exact.

C interop (Mio → C, System V AMD64): `cabi_sysv.mio` classifies fixed-prototype scalar C declarations (`$cabi::` externals shared with the Darwin backend) into INTEGER/SSE/MEMORY placements and emits the call bridge; `compile_with_c` returns position-independent machine code plus the list of external `call` sites, and `selfhost/pipeline/cabi_sysv.mio` hands them to the system C driver (`call sym@PLT`, non-PIE, `.mio_bss` fixed at the ELF data address). Programs without C declarations compile exactly as before. See `docs/linux-amd64-c-ffi.md`.

Not supported (reported as errors or as run-time "unsupported operation" stubs, never skipped silently):
string ordering, `chant` of floats, threads, TLS and concurrent collection.
Tests: `scripts/ci/native_platforms.py` (native compiler and CLI fixed points, `tests/native-differential` and the Runtime ABI contract in the default and `--gc-stress` configurations, compared with the reference VM).

Shared libraries (Linux AMD64): `shared.mio` lowers a program in position-independent mode (the label `pic`: every
data-area address is a rip-relative `lea` recorded as a `DataRef`, no signal handlers are installed) and generates one
System V AMD64 entry point per `koukai waza ... export "symbol"` function (arguments converted to the Mio calling
convention, one-time runtime initialisation, callee-saved registers preserved, boxed scalars converted). The result goes
to `kiso/aot/elfrel` and the system linker through `selfhost/pipeline/shared_linux.mio`. See
`docs/linux-amd64-shared-libraries.md`.
