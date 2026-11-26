# Linux AMD64 native shared libraries

Mio programs on Linux x86-64 can load ELF shared objects (`.so`) at run time, resolve functions by name and call them with the System V AMD64 ABI, then unload the library. This builds on the [Linux x86-64 C ABI](linux-amd64-c-ffi.md), which links C functions at build time; the new pieces are run-time loading (`dlopen`/`dlsym`/`dlclose`/`dlerror` from the system loader), calls through a function pointer, and `[]uint8` buffers as C arguments.

Scope: Linux, x86-64, ELF64, System V AMD64 ABI, glibc 2.41 verified (glibc 2.34 and newer have `dlopen` in libc; older versions need `-ldl`, which `mio build` accepts). musl is not verified.

## Architecture

```
Mio source -> Mio compiler -> Linux lowering -> dynamic ELF (cc/ld, PT_INTERP, libc)
    -> ld-linux-x86-64.so.2 -> dlopen/dlsym (libc) -> C ABI bridge (indirect call) -> native library
```

There are two execution modes. They are chosen by the program, not by a flag, and they never mix:

| | static mode | dynamic mode (`linux-amd64-dynamic`) |
| --- | --- | --- |
| Selected when | the program has no `= c "symbol"` declaration | the program (or an imported package) has at least one |
| ELF writer | Mio's own writer | Mio machine code placed into `.text` by the system C driver (`cc`, `gcc`, `clang`) |
| Program header `PT_INTERP` / `PT_DYNAMIC` | absent | present, `/lib64/ld-linux-x86-64.so.2` |
| `DT_NEEDED` | none | `libc.so.6` plus anything linked with `-l`/`--link` |
| libc | not used, raw syscalls | used; `exit` goes through libc so stdio and `atexit` run |
| OS threads | supported | rejected at build time (`E_CABI`) |
| Output | byte-identical to the previous compiler | ET_EXEC (non-PIE), runtime data at 0x10000000 |

Nothing about the static mode changed: `tests/ffi/linux-amd64-dynamic/run.py` checks that a program without C declarations still has no `PT_INTERP`, no `PT_DYNAMIC` and no `DT_NEEDED`, and `tests/ffi/linux-amd64/run.py --baseline` checks ordinary programs byte for byte against a baseline compiler. Mio does not implement a dynamic loader: loading is the system's `ld.so`, and the dynamic ELF itself is produced by the existing build path.

### What was added to the compiler

Two extensions of the existing fixed-prototype C declaration (`waza name(...) -> type = c "symbol";`). Both are checked by the Go Reference compiler and by the self-hosted compiler, which agree on the encoded prototype (`selfhost/codegen/ffi_diff_test.go`).

* **`[]uint8` parameter** (prototype code `m`). The bridge passes the address of the array's first byte in an integer register. The array stays owned by Mio. The call is not a GC safe point (C cannot call back into Mio), and the collector does not move objects, so the pointer is valid for exactly the duration of the call. A `nil` array traps with `runtime error: nil reference` before C runs. A zero-length array is valid. No other array element type is accepted, and none of GC objects, `string`, `Map` or closures can be passed.
* **Indirect call**, the reserved symbol `mio_ffi_indirect`. The first parameter must be a `cptr` holding the function address; the remaining parameters and the result are an ordinary C prototype. The bridge classifies the remaining parameters exactly like a direct call, loads the target into `r11` before aligning the stack, traps on a null target (`nil reference`, never a jump to address 0) and issues `call r11`. Each declaration is one fixed prototype, so the Mio type checker validates every call's argument types and the result type.

Both are Linux-only; Darwin ARM64 and Windows reject them at build time.

## Mio FFI ABI v1

| Mio FFI type | Mio type | C type | Signature letter |
| --- | --- | --- | --- |
| int32 | `int32` | `int32_t` | `i` |
| uint32 | `uint32` | `uint32_t` | `u` |
| int64 | `int64` | `int64_t` | `l` |
| uint64 / size | `uint64` | `uint64_t`, `size_t` | `z` |
| float32 | `float32` | `float` | `f` |
| float64 | `float64` | `double` | `d` |
| bool | `bool` | `uint8_t` 0 or 1 | `b` |
| pointer / handle | `cptr` | `void *` (opaque) | `p` |
| buffer | `[]uint8` + `uint64` length | `const uint8_t *`, `size_t` | `n` (followed by the length) |
| none | `unit` | `void` | `v` (result only) |

* **Integers** go in `RDI, RSI, RDX, RCX, R8, R9`, **floating point** in `XMM0-XMM7`; the two groups run out independently and the rest goes on the stack in 8-byte slots. Narrow integers are sign- or zero-extended to 64 bits.
* **Stack alignment**: the bridge executes `and rsp, -16` and reserves a 16-byte-multiple argument area, so `RSP` is 16-byte aligned at every `call`, at any Mio frame depth. `RBX, RBP, R12-R15` are preserved by C; Mio keeps nothing live in the other registers across a call.
* **Return** values are read from `RAX` or `XMM0` and re-extended to the declared width; `bool` is normalised to 0 or 1.
* **Not supported**: variadic functions (rejected at build time; they need `AL` set), structs by value, callbacks (C calling Mio), `long double`, C++ and Rust ABIs.
* **GC objects never cross the boundary.** `string`, arrays other than `[]uint8`, `Map`, structs, enums and closures are rejected by the type checker. A string is passed as a NUL-terminated byte buffer made explicitly (`linux.cstring`), and results come back as scalars or are copied out of C memory into a Mio array.
* **RA-1** (Runtime ABI) is unchanged: no object layout, no header and no GC protocol was altered. The `m` bridge reads the existing array object's data-pointer field and does nothing else.

## Building native modules

The sources are in `tests/ffi/linux-amd64-dynamic/modules/`. Binaries are built into the output directory and are never committed.

| Library | Exports | Links |
| --- | --- | --- |
| `libmio_math.so` | `mio_add`, `mio_sub`, `mio_multiply` (plus `mio_mul64`, `mio_scale32`, `mio_is_even` for int64, float32, bool) | |
| `libmio_buffer.so` | `mio_buffer_length`, `mio_buffer_checksum` | |
| `libmio_state.so` | `mio_state_create`, `_increment`, `_get`, `_destroy` | |
| `libmio_zlib.so` | `mio_zlib_compress`, `_decompress`, `_crc32`, `_bound` | `-lz` |
| `libmio_sqlite.so` | `mio_sqlite_open_memory`, `_exec`, `_query_int`, `_query_text`, `_close` | `-lsqlite3` |
| `libmio_crypto.so` | `mio_sha256` (OpenSSL EVP) | `-lcrypto` |
| `libmio_dep_outer.so` -> `libmio_dep_inner.so` | transitive dependency through `$ORIGIN` | |

```sh
sudo apt-get install build-essential zlib1g-dev libsqlite3-dev libssl-dev
cc -fPIC -shared -O2 modules/mio_math.c -o libmio_math.so
cc -fPIC -shared -O2 modules/mio_zlib.c -o libmio_zlib.so -lz
readelf -h libmio_math.so      # ELF64, Advanced Micro Devices X86-64, DYN
readelf -d libmio_zlib.so      # NEEDED libz.so.1
nm -D --defined-only libmio_math.so
```

## Loading modules from Mio: `kiso/ffi/linux`

The run-time loader is the production Bunko package `kiso/ffi/linux`. It is part of the embedded standard library of the Full distribution, so `yobu "kiso/ffi/linux";` works without an external Bunko tree; the Core distribution resolves it from `--bunko-root`/`MIO_BUNKO` like every other package. It is plain Mio on top of the two compiler extensions above: no compiler change, no helper library, no libffi. The package name follows the per-platform layout of `kiso/ffi/darwin`; the Windows API `kiso/ffi` and the Darwin API are unchanged.

```mio
yobu "kiso/ffi/linux";

kaku lib = linux.open("./libmio_math.so");             // RTLD_NOW | RTLD_LOCAL
moshi (linux.failed(lib)) { chant(linux.library_error(lib)); }
kaku add = linux.symbol(lib, "mio_add", "i:ii");       // name and the C prototype you claim
chant(linux.call_i_ii(add, int32(10), int32(20)));     // 30
chant(linux.close(lib));                               // "" on success; add is now invalid
```

A complete program that loads libc and libm by name is [examples/linux-amd64/ffi_libc.mio](../examples/linux-amd64/ffi_libc.mio); the examples suite runs it on Linux AMD64 and records it as NOT_APPLICABLE elsewhere.

### Handles

`Library` and `Function` are opaque handles. Each one names a slot of a package-private table plus the unique id the slot held when the handle was issued. Ids are never reused, so a handle whose slot was freed and later reused by another library or function stays invalid. Handles cannot be forged: their fields are immutable and the ticket type is private to the package (`linux.Ticket { ... }` is a compile error); copying a handle copies the same reference.

| Function | Result |
| --- | --- |
| `open(path) -> Library` | `dlopen(path, RTLD_NOW \| RTLD_LOCAL)`. The path must be absolute or contain `/` (`./libx.so`, `../lib/libx.so`); a bare name is `E_FFI_PATH` instead of a silent search of `LD_LIBRARY_PATH` and the system directories. |
| `open_system(name) -> Library` | Explicit loader search by file name, for example `"libz.so.1"`; a name containing `/` is `E_FFI_PATH`. |
| `failed(library)`, `library_error(library)` | Whether open failed, and why (`library.error` is the same string). |
| `is_open(library)`, `path(library)` | Whether the library is loaded and not closed; the path or name it was opened with (`""` once closed). |
| `close(library) -> string` | Invalidates the library and every function resolved from it, then calls `dlclose`. `""` on success; closing twice or closing a failed library is an error and does nothing. |
| `symbol(library, name, signature) -> Function` | `dlsym` with `dlerror` cleared first. The signature is validated and recorded. |
| `error(function)`, `resolved(function)` | Why resolution failed, and whether it succeeded. |
| `is_live(function)`, `signature(function)` | Whether the function can still be called (resolved, not released, library open); its recorded signature. |
| `release(function) -> string` | Frees one function handle; its library stays open. A second release is `E_FFI_STALE`. |
| `check(function, signature) -> string` | `""` when a call of that signature would be accepted, otherwise the reason. |
| `call_<result>_<parameters>(function, ...)` | Typed calls (below). |
| `last_error() -> string` | `""` after a typed call that reached native code, otherwise why the most recent typed call or `address` was refused. |
| `address(function, signature) -> cptr` | The validated address for a hand-written `= c "mio_ffi_indirect"` declaration, or `cptr(0)` with `last_error()` set. |
| `valid_signature(signature)`, `shape_of(signature)` | Signature validation and its integer code (`-1` when invalid). |
| `cstring(text) -> []uint8` | A fresh NUL-terminated copy of the UTF-8 bytes; it does not alias the string. |
| `libraries_open()`, `functions_live()` | Live handle counts, for leak checks. |

### Typed calls

Each wrapper is one fixed System V AMD64 prototype and its name spells the signature: `call_i_`, `call_l_`, `call_p_`, `call_z_`, `call_v_`, `call_i_i`, `call_l_l`, `call_d_d`, `call_b_i`, `call_i_p`, `call_v_p`, `call_p_n`, `call_i_ii`, `call_l_ll`, `call_f_ff`, `call_d_dd`, `call_z_nz` and `call_u_nz` (buffer and its length), `call_l_pn`, `call_i_pn`, `call_i_nzn`, `call_l_pnnz`, `call_l_nznz` (two buffers with their lengths, for compress/decompress). Wrappers that take a buffer followed by `z` pass `len(data)` themselves.

Every call goes through one gate before the native call: the handle's slot must exist and still hold the handle's id, and the shape the function was resolved with must equal the wrapper's shape. A refused call makes no native call, returns zero (`iie`, `cptr(0)`, nothing for `v`) and sets `last_error()`. Zero can also be a legitimate result, so code that must distinguish the two checks `last_error()` or validates once with `check`.

For a prototype that has no wrapper (more than 16 signature letters, `int8`/`int16` parameters, other buffer layouts), declare the bridge yourself and pass the validated address:

```mio
waza raw_scale(f: cptr, a: int64, factor: float32) -> float64 = c "mio_ffi_indirect";
kaku scale = linux.symbol(lib, "my_scale", "p:");     // illustrative symbol; the declaration above is its real prototype
chant(raw_scale(linux.address(scale, "p:"), 3, float32(1.5)));
```

`address` performs the same gate, so a closed, released or mismatched function gives `cptr(0)`, and the indirect bridge traps on `cptr(0)` with `runtime error: nil reference` instead of jumping to address 0 (`tests/ffi/linux-amd64-dynamic/cases/traps/stale_address.mio`). A hand-written declaration is only as correct as its match with the C prototype, and an address copied into a variable is not invalidated by a later `close`: call `address` at the call site.

## Memory ownership

* Mio owns every `[]uint8`. C may read or write it during the call; C must not keep the pointer. After the call it is an ordinary Mio array again.
* C owns everything it allocates (`mio_state_create`, `mio_sqlite_open_memory`). A `cptr` is an untracked 64-bit value: the collector does not scan it, does not keep anything alive for it and never frees it. The library's own destroy function (`mio_state_destroy`, `mio_sqlite_close`) is the only release, and the module's contract decides what a stale handle does (the sample state module detects a null handle and returns `-1`).
* Results move across the boundary by value: integers, floats and pointers as registers, buffers by filling a Mio array the caller sized (`mio_zlib_compress` reports the number of bytes written).
* A `Library` owns its `Function` handles: `close` frees them. The package owns the `dlopen` handle; it is never exposed.
* Loader error messages are copied out of `dlerror()` with `strnlen` bounded to 64 KiB, so a corrupt message cannot make the copy run away.

## Error handling

Every failure is a value; none of them aborts the program except the nil-reference traps described above. Messages start with a stable prefix:

| Prefix | Meaning |
| --- | --- |
| `E_FFI_PATH` | empty path, NUL in the path, a bare name passed to `open`, or a path passed to `open_system` |
| `E_FFI_LOAD` | `dlopen` failed; the message is the system's `dlerror()` text (missing file, not an ELF file, truncated, missing dependency) |
| `E_FFI_SYMBOL` | empty name, NUL in the name, or `dlsym` could not find it |
| `E_FFI_SIGNATURE` | malformed signature string, or the call does not match the resolved signature |
| `E_FFI_CLOSED` | the library was closed (use after unload, double close, a function of a closed library) |
| `E_FFI_STALE` | the function handle was released |
| `E_FFI_STATE` | the library or symbol never resolved |
| `E_FFI_CLOSE` | `dlclose` reported a failure (the handles were already invalidated) |

C functions report their own errors through return values (`-1` for a null handle, negative zlib codes, SQLite result codes); the Mio side reads them as ordinary integers.

## Lifecycle

* `open` calls `dlopen` once per `Library`. Opening the same file twice gives two `Library` values; the system loader reference-counts the mapping and `close` calls `dlclose` once per `Library`, so closing one leaves the other usable.
* `close` invalidates the library and its functions before calling `dlclose`. Every `Function` of that library then refuses calls with `E_FFI_CLOSED`: the stale function pointer is never executed. A reload gives a new `Library` and new `Function` values; the old ones stay dead even if the file is mapped at the same address again and even when the new handles reuse the old slots.
* `close` scans the function table only while the library still owns live functions, so closing a library is O(1) when its functions were released and does not depend on how many functions other libraries hold.
* Libraries are not unloaded when the `Library` value is collected: Mio has no finalizers. A library that is never closed stays loaded for the life of the process; a function that is never released stays in the table until its library is closed.
* Threads: a program that declares C functions cannot use OS threads on Linux AMD64 (the compiler rejects it), so the handle tables are single-threaded by construction. Lifting that restriction requires making them thread-safe first.

## Tests

```sh
python3 tests/ffi/linux-amd64-dynamic/run.py --mio /path/to/mio --output build/ffi-dynamic --bench
mio test bunko/kiso/ffi/linux --bunko-root bunko
```

`run.py` builds the modules, inspects them (`readelf -h`, `readelf -d`, `nm -D`, `ldd`), compiles and runs every case in `cases/` with the native Mio, once normally and once with `--gc-stress`, then checks the static and dynamic ELF layouts and runs the benchmark. The cases are `test_ffi_math`, `test_ffi_buffer`, `test_ffi_state`, `test_ffi_zlib`, `test_ffi_sqlite`, `test_ffi_crypto`, `test_ffi_missing_symbol`, `test_ffi_invalid_library`, `test_ffi_lifecycle` (1,000 load/unload cycles, 10,000 repeated resolutions, four libraries open at once, transitive dependencies, use after unload, double close) and `test_ffi_handles` (release, stale handles across slot reuse, close invalidating exactly its own functions, `open_system`, 2,000 leak-free cycles), plus null-function-pointer, empty-buffer and stale-`address` trap checks. The package's own tests (`bunko/kiso/ffi/linux/ffi_test.mio`) cover signatures, the wrapper shape constants, the path policy and handle states, and call libc and libm through most wrappers.

CI (`scripts/ci/native_platforms.py`, Linux AMD64 native job): `mio-test bunko/kiso/ffi/linux`; `linux-dynamic-libraries` (the driver above, required) after installing `build-essential zlib1g-dev libsqlite3-dev libssl-dev`; and `linux-ffi-embedded`, which builds and runs a `kiso/ffi/linux` program with an isolated copy of the Full CLI and no Bunko tree, and checks that Core reports the package as missing.

## Performance

`benchmarks/ffi/linux_ffi_bench.mio` (run by `run.py --bench`) times each phase separately with `clock_gettime(CLOCK_MONOTONIC)`; load time is never part of a call time. A/B against the test-only package that preceded `kiso/ffi/linux` (base `6ac642c`): both binaries built from their own tree, the same `libmio_math.so`, 5 alternating runs each, median, Debian 13 / glibc 2.41, 8-CPU shared x86-64 host (treat as indicative; single-shot rows are noisy):

| Measurement | Before | `kiso/ffi/linux` |
| --- | --- | --- |
| First load (`open`, file not mapped before, single shot) | 39.1 us | 46.8 us |
| First symbol resolution (single shot) | 4.0 us | 3.5 us |
| Repeated symbol resolution (100,000 x, without release) | 0.86 us each | 1.08 us each |
| Warm load + unload (file already mapped, reference counted) | 1.20 us per cycle | 1.34 us per cycle |
| Cold load + unload (every `dlclose` really unmaps) | 23.1 us per cycle | 23.4 us per cycle |
| Hot call, Mio function `a + b` (10,000,000 x) | 4.4 ns | 4.2 ns |
| Hot call, C function linked at build time (PLT) | 4.2 ns | 4.0 ns |
| Hot call, indirect through a `dlsym` pointer, no gate | 4.8 ns | 4.4 ns |
| Hot call, `call_i_ii` with the handle and signature gate | 14.3 ns | 12.0 ns |

The gate now indexes the handle's slot directly instead of re-reading a shared state array and an error slot on every call, which is why the gated call got cheaper while checking more (slot, generation id, shape). Resolution and load/unload pay for the table bookkeeping that makes stale handles detectable (slot allocation, id, ownership count). The benchmark binary grew from 65,368 to 73,560 bytes; the Full CLI grew by 24,416 bytes of embedded package source; the Core CLI is byte-identical.

## Limitations

* x86-64 Linux with glibc only; musl, Linux ARM64 and the other targets are not covered. Darwin ARM64 and Windows reject indirect calls and `[]uint8` C parameters at build time.
* No variadic functions, no struct by value, no callbacks from C into Mio, no external threads calling Mio, no cross-thread GC roots, no C++ or Rust ABI.
* A program that loads libraries cannot also use OS threads (existing restriction of the dynamic mode); the handle tables rely on it.
* Function pointers are checked against the signature the caller claims, not against the library: C has no run-time prototype information, so a wrong claim for a symbol that exists is the caller's error. Data symbols are not distinguished from functions.
* The signature letters cover scalars, pointers and `[]uint8` buffers; other shapes need their own `mio_ffi_indirect` declaration and `address`. Signatures are at most 16 letters.
* A refused typed call returns zero; distinguishing it from a zero result needs `last_error()` or `check`.
* The Go reference frontend does not accept package-level mutable state, so it cannot load `kiso/ffi/linux` (the same holds for `kiso/ffi/darwin`); the Go VM cannot execute C declarations in any case (`E_FFI_RUNTIME`). The package is compiled and tested by the native compiler only.
* `chant` cannot print floating-point values in the native Linux backend (existing limitation); the tests compare floats instead.
* The collector does not move objects. A future moving collector must pin `[]uint8` arguments for the duration of the call.
* A library that is never closed is never unloaded.
