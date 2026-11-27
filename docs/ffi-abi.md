# Mio Native FFI ABI v1

For Darwin ARM64 shared libraries and runtime loading, see [Darwin FFI ABI v1](darwin-ffi-abi.md).

Native FFI v1 adds checked Windows AMD64 dynamic calls to the existing fixed-prototype C declarations. It is independent of **Runtime ABI Revision RA-1**. It does not revise Object ABI v1, either GC profile, root tracking, Mio's own calling convention, or the current Linux ELF implementation.

## Source audit and implementation boundary

| Area | Existing implementation | Change |
| --- | --- | --- |
| RA-1 | `kiso/runtime/abi`, platform-specific non-moving collectors, exact roots | None |
| OS adapters | `kiso/runtime/os/{darwin,linux}`, Windows operations emitted by `lowerwinrt` | None; loader resources live in a separate support DLL |
| Windows AMD64 | `lowerwinrt` is the production heap backend; `lowerwin` is the scalar backend | Both reuse the updated `win64abi.emit` |
| PE32+ | `kiso/aot/pe` writes IAT, imports, rel32 patches and RBP frame unwind records | Existing writer and IAT remapping reused |
| C external declarations | `$win64c` Mochi metadata carries complete scalar prototypes | Reserve `mio.dynamic.dll` as a compiler marker |
| Indirect calls | `x86_64.call_r` already exists for machine code; language closures use the Mio convention | Add a checked **native** indirect-call lowering; a Mio closure is not a C function pointer |
| Scalar ABI | Existing bridge handles positional GPR/XMM, stack args and scalar normalization | Same marshal code, skipping the leading function token |
| Darwin | Mach-O writer/OS adapter and upstream Darwin ARM64 fixed-prototype C ABI use their own conventions | Reuse linked dylib calls for the shared fixtures; checked runtime token loader remains Windows-only |
| Resource ownership | Existing `cptr` is an opaque non-GC pointer without dereference or arithmetic | Generation-checked library/symbol/buffer registries in the support DLL |
| Frontend | `copy_type_memo` mistakenly overwrote imported CPtr/Uint64 with TypeParameter | Restrict that fallback to TypeParameter; imported `Result.handle` now retains uint64 |

The Go VM still rejects execution of C stubs with `E_FFI_RUNTIME`. A successful VM compile or cross-build is not a native-call test.

## Deployment

Dynamic FFI applications require `mio_native_ffi.dll` beside the generated EXE in a trusted application directory. Build this small support DLL from `native/ffi/windows.c`; it is a production dependency for dynamic FFI only. The compiler and ordinary Mio executables remain independent of it. This is an explicit deployment change from the earlier static-import-only implementation.

The helper uses only the Windows API and the C standard library, with MSVC's static CRT. There is no libffi, CGo, runtime JIT, generated C application, or external linker for the Mio executable. A C compiler and linker are needed to build the helper and test DLLs. From a VS x64 developer prompt:

```text
cl /nologo /std:c11 /LD /O2 /MT native/ffi/windows.c /link /OUT:mio_native_ffi.dll
```

Deploy the helper as application code; the normal Windows loader loads this initial IAT dependency. The safe path rules below govern libraries subsequently opened with `ffi.open`.

## API and complete example

```mio
yobu "kiso/ffi";
yobu "kiso/os";
waza add(function: uint64, a: int32, b: int32) -> int32 = c "mio.dynamic.dll" "add_i32";
kaku library = ffi.open("./mio_ffi_math.dll");
moshi (library.error != uint32(0)) { chant(library.error); chant(library.os_error); os.exit(1); }
kaku function = ffi.symbol(library.handle, "mio_add_i32", "int32", ["int32", "int32"]);
moshi (function.error != uint32(0)) { ffi.close(library.handle); os.exit(2); }
chant(add(function.handle, int32(19), int32(23))); // 42
ffi.release(function.handle);
ffi.close(library.handle);
```

The marker DLL is never imported or loaded. Its export string identifies a local prototype, not a DLL export. `ffi.symbol` names the actual export at runtime. Calls use the token as a leading Mio-only parameter; it is omitted from the C arguments. A prototype can call different exports with identical signatures. Signature aliases with conflicting prototypes follow the existing declaration rules.

`ffi.open` and `ffi.symbol` return `Result { handle:uint64, error:uint32, os_error:uint32 }`. Zero handle means failure. `close`, `release`, `buffer_free` and `buffer_set` return uint32 1 on success, 0 on error. `ffi.error()` and `ffi.os_error()` report the last helper error for the calling OS thread. Read these immediately; subsequent helper operations can replace them. Result captures both before freeing temporary text buffers.

`ffi.signature(result, parameters)` produces an exact descriptor. `ffi.check(token, signature)` permits recoverable validation. Every invocation checks again atomically with pinning. A stale, zero, invalid or mismatched token emits `E_FFI_CALL` to stderr and exits with code **125**, before entering native code. Arbitrary scalar return types cannot also return a Mio Result, so this first version deliberately uses a fail-stop call error. `ffi.address(token)` exposes an address for inspection as `cptr`; a closed token returns null. That address is not accepted as a callable token and does not own the library.

## Native type mapping

| Mio | C | Notes |
| --- | --- | --- |
| int32 / uint32 | int32_t / uint32_t | Also Windows long/unsigned long/BOOL/DWORD as appropriate |
| int64 / uint64 | int64_t / uint64_t | Full 64-bit transport; uint64 arithmetic is still limited by the language |
| float32 / float64 | float / double | XMM transport, exact scalar bit representation |
| cptr | void*, const void*, opaque handle | No GC ownership, integer-address construction, dereference or pointer arithmetic |
| uint64 | size_t / uintptr_t | Windows AMD64 is LLP64 |
| unit | void | Return position only |
| int8/uint8/int16/uint16 | Corresponding fixed-width scalar | Reuse existing normalization |

Bare `bool`, variadic prototypes, arrays, strings, aggregates, closures and other GC values are rejected as native parameters/returns. Dynamic declarations require a leading uint64 token and support **0–12 C arguments**; static imports retain their earlier 256 argument bound. C++ ABI, callbacks, foreign-thread callbacks, exceptions crossing the bridge and structs/unions by value are unsupported.

The descriptor is an exact encoding, not a hash: return type in bits 0–3, argument types in successive four-bit nibbles, and argument count in bits 56–59. Tags are unit=0, int64=1, uint64=2, cptr=3, float64=4, float32=5, int8=6, uint8=7, int16=8, uint16=9, int32=10, uint32=11. `symbol` rejects unsupported names and types. The compiler independently derives the expected descriptor at each call bridge. This checks agreement between registration and invocation; PE exports do not describe C types, so developers must still declare the true C header signature.

## Microsoft x64 calling convention

The bridge saves RBP, establishes its frame, aligns RSP down to 16, and reserves 32-byte shadow space plus eight-byte argument slots after the fourth argument. The leading token is excluded before assigning positions. Integer/pointer arguments 0–3 go in RCX/RDX/R8/R9; float arguments at those same positions go in XMM0–XMM3. For `(int32,double,uint64,float,...)`, this means RCX, XMM1, R8, XMM3. Argument 4 starts at caller `[RSP+32]`.

Two private eight-byte slots after the aligned outgoing area hold the acquired target and returned scalar. Acquire calls the helper through the existing PE IAT and pins the owning DLL. `call r11` invokes the validated target. RAX carries integer/pointer results; XMM0 results are copied to their Mio scalar bit representation **before** the helper finish call. The result is preserved in the private slot while finish unpins the DLL. Both helper and target calls have shadow space and aligned stacks. The bridge writes only volatile GPR/XMM registers and saves/restores RBP. It leaves nonvolatile GPRs and XMM6–XMM15 alone.

The existing PE RBP frame records cover the bridge's lifetime, including dynamically aligned outgoing/private space. No Runtime object fields, shadow root frames or live Mio locals are repurposed. Native code must return normally; callbacks, SEH/C++ exceptions unwinding into Mio and nonlocal exits have no cleanup protocol.

References: [Microsoft x64 calling convention](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention), [LoadLibraryExW flags and search behavior](https://learn.microsoft.com/en-us/windows/win32/api/libloaderapi/nf-libloaderapi-loadlibraryexw).

## Loading, lookup and search rules

`ffi.open` copies the path's UTF-8 bytes to native storage, rejects embedded NUL/invalid UTF-8, strictly converts them using `MultiByteToWideChar`, and calls `LoadLibraryExW` with UTF-16. Spaces and non-ASCII paths are preserved.

* A bare filename such as `kernel32.dll` expands to an **absolute System32 path**, with `LOAD_LIBRARY_SEARCH_SYSTEM32`. It does not search PATH or the current directory, and does not rely on basename lookup of already-loaded application modules.
* An absolute drive or UNC path loads that file. Dependencies search its directory and System32 using `LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | LOAD_LIBRARY_SEARCH_SYSTEM32`.
* An explicit relative path such as `./plugins/math.dll` resolves against the **executable directory**, with the same dependency policy. It never uses the process working directory. Drive-relative (`C:foo`) and root-relative (`\foo`) paths are rejected. The caller controls explicit `..` traversal.

No process-wide search configuration is changed. `GetProcAddress` resolves exact ASCII export names. It returns a token only after checking that the address lies in committed executable memory; exported data is rejected. An absent export returns a symbol-phase error, distinct from a failed load. Windows validates the actual PE machine/format; `.dylib` is not accepted by renaming it to `.dll`.

The loader holds its registry lock during LoadLibrary/FreeLibrary. DLL initialization, detach hooks and native calls must not reenter the helper; native callbacks are outside this ABI.

## Library and symbol lifecycle

The helper has fixed process-local tables of 1024 libraries, 1024 functions and 1024 buffers. Each slot receives a globally monotonic generation ID, never reused even across resource kinds; exhaustion fails instead of wrapping. `resources()` exposes the live entry count for leak checks.

An open library owns one OS module reference. Repeated opens own separate references, even if Windows shares the module. Resolve does not unload or transfer that reference. Close atomically invalidates every function token from that open and frees the OS reference. Duplicate close/release/free and wrong-kind/stale tokens fail safely. Close refuses with BUSY while any invocation acquired from that library is running; release also refuses during active calls, so finish can always locate its owner. Completion decrements the pin. Calls through copied function tokens cannot survive close or revive after slot reuse.

Native resources require explicit cleanup: release unused function tokens or close their library (which releases all its tokens), free buffers, and destroy foreign objects **before** closing their library. This version does not infer ownership or install GC finalizers. A copied `cptr` to a foreign object/data buffer is subject to the native library's lifetime contract; token safety does not validate arbitrary native pointer contents.

## GC and native memory

`ffi.bytes([]uint8)` copies data into zero-initialized native storage with an extra trailing NUL. `buffer_new(size)` allocates size bytes plus the sentinel. `buffer_get/set` check logical bounds. `buffer_data` borrows a native pointer; it remains valid until buffer_free. `buffer_pointer` reads an eight-byte pointer out parameter (for APIs such as SQLite) without permitting integer-to-pointer casts. Use pointer+length declarations with the actual capacity. C code can still corrupt memory if given an incorrect length, free pointers with the wrong allocator, or retain a borrowed pointer after it is freed.

Mio String, Array, Map, Struct and Closure internals never cross the ABI. Helper code does not allocate Mio objects or trigger its GC; calls keep managed locals and roots in their existing caller slots. Foreign buffers and objects need explicit ownership and library-specific destruction. Asynchronous borrowed pointers and callbacks into Mio are unsupported.

## Errors

| code | meaning |
| --- | --- |
| 0 | success |
| 1 | invalid/stale/wrong-kind handle |
| 2 | load/unload failure; os_error captures GetLastError immediately |
| 3 | symbol lookup failure or non-executable export; os_error contains the Win32 cause |
| 4 | unsupported/mismatched signature |
| 5 | library/function busy during an active native invocation |
| 6 | invalid UTF-8/NUL/name/path/buffer index |
| 7 | registry or generation capacity exhausted |
| 8 | native allocation failed |

## Platform status and tests

Windows AMD64 has the checked runtime loader and token-call API. The latest Mio-2.2.0 base also has Darwin ARM64 fixed-prototype C imports: `native-tests/darwin.py --mio <native-cli>` links **the same five C fixture dylibs** into real Mio programs using that existing ABI, and executes normal/GC-stress images. Without --mio it runs only the explicitly labeled ctypes fixture-host checks. The Darwin runtime open/symbol/token-call API remains NOT_RUN/unsupported; Darwin AMD64 has no equivalent fixed C bridge. Existing upstream Linux ELF C ABI/linking is retained without adding a Linux runtime loader in this change. Windows ARM64 and the reference VM reject Windows declarations at execution/lowering.

See `native-tests/README.md` for builds, optional third-party dependency paths, C signatures and commands. Windows native-platform CI runs the new driver as a required check after creating the native CLI, on a real windows-2022 AMD64 runner. The standalone macOS job validates the C fixture host; the Darwin ARM64 native-platform job also invokes all five dylibs through Mio. CI uploads JSON/logs, not DLLs. Dependencies are never downloaded by a test; optional third-party tests report NOT_RUN unless locally provisioned paths are supplied.

Local evidence on 2026-11-23, Windows 10 AMD64, MSVC 14.51, native Mio candidate built from this source:

* Five modules, scalar/mixed/12-argument calls, buffers/UTF-8/opaque objects, System32 APIs, Unicode paths, errors, 1100 load/resolve/call/close iterations, stale IDs and zero live resources: PASS in normal and GC stress modes.
* SQLite (Python-distributed DLL), Git for Windows zlib and OpenSSL: real Mio function calls PASS. SQLite initialization is explicit for builds with SQLITE_OMIT_AUTOINIT.
* Existing Windows static C FFI suite: 98 checks PASS, including register, unwind and IAT tests. RA-1: 16 PASS. Native differential: 64 PASS, 2 pre-existing NOT_APPLICABLE stress exclusions.
* Targeted Go frontend/Mochi/selfhost typechecker/codegen suites: PASS outside the Windows filesystem sandbox. Darwin execution and remote CI require their actual runners; local results do not stand in for them. The local bootstrap candidate is not a trusted Native Seed attestation.
