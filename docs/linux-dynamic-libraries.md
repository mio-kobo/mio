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
* **GC objects never cross the boundary.** `string`, arrays other than `[]uint8`, `Map`, structs, enums and closures are rejected by the type checker. A string is passed as a NUL-terminated byte buffer made explicitly (`ffi.cstring`), and results come back as scalars or are copied out of C memory into a Mio array.
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

## Loading modules from Mio

The Mio side is the package `tests/ffi/linux-amd64-dynamic/ffi` (`ffi.mio`), plain Mio on top of the two compiler extensions. It is a source package: import it with a relative path (`yobu "../ffi";`). It is not part of the embedded Bunko standard library yet (see Limitations).

```mio
yobu "../ffi";

kaku lib = ffi.open("./libmio_math.so");               // RTLD_NOW | RTLD_LOCAL
moshi (ffi.failed(lib)) { chant(lib.error); }
kaku add = ffi.symbol(lib, "mio_add", "i:ii");         // name and the signature you claim
chant(ffi.call_i_ii(add, int32(10), int32(20)));       // 30
chant(ffi.close(lib));                                 // "" on success
```

* `ffi.open(path)` accepts an absolute path or a path containing `/` (`./libx.so`, `../lib/libx.so`). A bare name is rejected with `E_FFI_PATH` instead of silently searching `LD_LIBRARY_PATH` and the system directories.
* `ffi.symbol(library, name, signature)` returns a `Symbol` that records the signature. `ffi.error(symbol)` holds the reason when resolution failed.
* `ffi.call_<result>_<params>(symbol, ...)` are typed wrappers, one per C prototype shape (for example `call_i_ii`, `call_d_dd`, `call_z_nz` for `(buffer, len) -> size_t`, `call_l_nznz` for compress/decompress, `call_i_p` for handle functions). Every call re-checks, before the native call, that the library is still open, that the symbol was resolved with exactly this signature and that the address is non-null. On refusal the wrapper makes no native call, returns zero and sets `ffi.error`.
* For a prototype shape that has no wrapper, declare it yourself: `waza my_call(f: cptr, a: int32) -> int32 = c "mio_ffi_indirect";` and pass `symbol.address`; check it with `ffi.ready(symbol, ffi.shape_of("i:i"))` first if you want the library-open and signature gate. A hand-written declaration is only as safe as its match with the C prototype. Signatures are at most 16 letters.

## Memory ownership

* Mio owns every `[]uint8`. C may read or write it during the call; C must not keep the pointer. After the call it is an ordinary Mio array again.
* C owns everything it allocates (`mio_state_create`, `mio_sqlite_open_memory`). A `cptr` is an untracked 64-bit value: the collector does not scan it, does not keep anything alive for it and never frees it. The library's own destroy function (`mio_state_destroy`, `mio_sqlite_close`) is the only release, and the module's contract decides what a stale handle does (the sample state module detects a null handle and returns `-1`).
* Results move across the boundary by value: integers, floats and pointers as registers, buffers by filling a Mio array the caller sized (`mio_zlib_compress` reports the number of bytes written).
* `ffi.cstring(text)` returns a fresh NUL-terminated `[]uint8`; it is a copy and does not alias the string.

## Error handling

Every failure is a value; none of them aborts the program except the two traps described above.

| Prefix | Meaning |
| --- | --- |
| `E_FFI_PATH` | empty path, NUL in the path, or a bare name |
| `E_FFI_LOAD` | `dlopen` failed; the message is the system's `dlerror()` text (missing file, not an ELF file, truncated, missing dependency) |
| `E_FFI_SYMBOL` | empty name, NUL in the name, or `dlsym` could not find it |
| `E_FFI_SIGNATURE` | malformed signature string, or the call does not match the resolved signature |
| `E_FFI_CLOSED` | the library was already closed (use after unload, double close) |
| `E_FFI_STATE` | the library or symbol never resolved |
| `E_FFI_CLOSE` | `dlclose` reported a failure |

C functions report their own errors through return values (`-1` for a null handle, negative zlib codes, SQLite result codes); the Mio side reads them as ordinary integers.

## Lifecycle

* `ffi.open` calls `dlopen` once per `Library`. Opening the same file twice gives two `Library` values; the system loader reference-counts the mapping and `ffi.close` calls `dlclose` once per `Library`, so closing one leaves the other usable.
* `ffi.close` marks the `Library` closed before calling `dlclose`. Every `Symbol` of that library then refuses calls with `E_FFI_CLOSED`: the stale function pointer is never executed. A reload gives a new `Library` and new `Symbol` values; the old ones stay dead even if the file is mapped at the same address again.
* Libraries are not unloaded when the `Library` value is collected: Mio has no finalizers. A library that is never closed stays loaded for the life of the process.
* Limit of the guarantee: the check protects the wrappers. A `cptr` copied out of `symbol.address`, or a data pointer a C module kept, is raw and is not protected.

## Tests

```sh
python3 tests/ffi/linux-amd64-dynamic/run.py --mio /path/to/mio --output build/ffi-dynamic --bench
```

Builds the modules, inspects them (`readelf -h`, `readelf -d`, `nm -D`, `ldd`), compiles and runs every case in `cases/` with the native Mio, once normally and once with `--gc-stress`, then checks the static and dynamic ELF layouts and runs the benchmark. The cases are `test_ffi_math`, `test_ffi_buffer`, `test_ffi_state`, `test_ffi_zlib`, `test_ffi_sqlite`, `test_ffi_crypto`, `test_ffi_missing_symbol`, `test_ffi_invalid_library` and `test_ffi_lifecycle` (1,000 load/unload cycles, 10,000 repeated resolutions, four libraries open at once, transitive dependencies, use after unload, double close), plus null-function-pointer and empty-buffer checks. CI runs the same driver in the Linux native job after installing `build-essential zlib1g-dev libsqlite3-dev libssl-dev`.

## Performance

`benchmarks/ffi/linux_ffi_bench.mio` (run by `run.py --bench`) times each phase separately with `clock_gettime(CLOCK_MONOTONIC)`; load time is never part of a call time. Median of 5 runs, Debian 13 / glibc 2.41, x86-64 shared host (other jobs were running on the machine, so treat the numbers as indicative):

| Measurement | Median |
| --- | --- |
| First load (`ffi.open`, file not mapped before) | 41.5 us |
| First symbol resolution (`ffi.symbol`) | 3.8 us |
| Repeated symbol resolution (100,000 x, includes building the C string and the `Symbol`) | 0.91 us each |
| Warm load + unload (file already mapped, reference counted) | 1.55 us per cycle |
| Cold load + unload (every `dlclose` really unmaps) | 26.3 us per cycle |
| Hot call, Mio function `a + b` (10,000,000 x) | 4.5 ns |
| Hot call, C function linked at build time (PLT) | 4.2 ns |
| Hot call, indirect through a `dlsym` pointer, no gate | 4.9 ns |
| Hot call, `ffi.call_i_ii` with the lifecycle and signature gate | 14.9 ns |

The bridge itself costs about the same as any Mio call (about 0.4 ns more than a PLT call). The gate (`ready`: address, library state, signature shape, error slot) adds roughly 10 ns per call because it is two extra Mio calls and a few field loads; that is the price of refusing a call to an unloaded library and it was kept. Code that cannot tolerate it can call a `mio_ffi_indirect` declaration directly with an address it validated once.

## Limitations

* x86-64 Linux with glibc only; musl, Linux ARM64 and the other targets are not covered. Darwin ARM64 and Windows reject indirect calls and `[]uint8` C parameters at build time.
* No variadic functions, no struct by value, no callbacks from C into Mio, no external threads calling Mio, no cross-thread GC roots, no C++ or Rust ABI.
* A program that loads libraries cannot also use OS threads (existing restriction of the dynamic mode).
* Function pointers are checked against the signature the caller claims, not against the library: C has no run-time prototype information, so a wrong claim for a symbol that exists is the caller's error. Declare each module's shapes once in a wrapper package.
* The signature letters cover scalars, pointers and one buffer-plus-length shape; other shapes need their own `mio_ffi_indirect` declaration (one line each). The `ffi` package is not yet in the embedded Bunko and has to be imported by relative path.
* `chant` cannot print floating-point values in the native Linux backend (existing limitation); the tests compare floats instead.
* The collector does not move objects. A future moving collector must pin `[]uint8` arguments for the duration of the call.
* A library that is never closed is never unloaded.
