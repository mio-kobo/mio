# Linux AMD64 shared libraries

`mio build --shared` turns Mio modules into an ELF64 shared object (`.so`, `ET_DYN`, `EM_X86_64`, System V AMD64 ABI)
that a C program can `dlopen`, `dlsym` and call. It is the generation side of dynamic linking; loading an external
`.so` from a Mio program is a different feature (see [Linux x86-64 C ABI](linux-amd64-c-ffi.md)), though the two share
the declaration syntax and the `--link`, `-L`, `-l` and `--rpath` options.

Scope: Linux x86-64 host and target only. `--shared` with any other `--target` is refused. Static executables are
untouched: for programs without `export` declarations the compiler produces byte-for-byte the same images as before.

## Implemented and not implemented

| Feature | State |
| --- | --- |
| Position-independent code (rip-relative data, no load-address dependence, no TEXTREL) | implemented |
| `ET_REL` object writer (`kiso/aot/elfrel`), `--emit=object` | implemented |
| `ET_DYN` output through the system linker (ld, ld.lld, cc, gcc, clang), SONAME, hash, RELRO, BIND_NOW | implemented |
| Explicit exports (`koukai waza ... export "symbol"`), default-hidden runtime, duplicate detection | implemented |
| C ABI for `int8..int64`, `uint8..uint64`, `float32`, `float64`, `bool`, `cptr`, up to six integer and eight floating-point register arguments plus up to 64 stack slots | implemented |
| Opaque handles instead of GC objects | implemented (by convention, see below) |
| Several module directories into one library (`mio build a b c --shared`) | implemented |
| Declared C dependencies (`= c "sym"` with `--link/-L/-l/--rpath`, `DT_NEEDED`, PLT) | implemented |
| Post-link verification inside the compiler (`ET_DYN`, `EM_X86_64`, no W+X, non-executable stack, no TEXTREL, exact SONAME, RELRO and BIND_NOW) | implemented |
| Strings, arrays, structs or closures as parameters or results | **not supported** (rejected at compile time) |
| Pointer + length buffers filled by C | **not supported** (Mio has no operation that dereferences a `cptr`) |
| Callbacks from C into Mio, C threads calling concurrently, OS threads inside the library | **not supported** |
| GOT-relative (`GOTPCREL*`) addressing and `R_X86_64_64`/`RELATIVE` data pointers | **not generated** (the code needs none); the object writer rejects them |
| A Mio native linker | **reserved**: `--linker native` reports that it is not implemented |
| Shared libraries for linux-arm64, darwin, windows | **not supported** |
| Go reference compiler (`cmd/mio`) | parses `export` with the same rules and ignores it; it cannot produce shared libraries (the Linux native pipeline is Mio-hosted) |

## Build guide

```mio
// math.mio (module directory mio_math/)
koukai waza add(a: int32, b: int32) -> int32 export "mio_add" { kaesu a + b; }
```

```sh
mio build mio_math --shared -o libmio_math.so
mio build mio_math --shared --soname libmio_math.so.1 -o libmio_math.so.1
mio build mio_math --shared --emit=object -o math.o         # the ET_REL object only, no linker
mio build mio_math --shared --linker /usr/bin/ld.lld -o libmio_math.so
mio build mio_math --shared --verbose -o libmio_math.so      # prints the exact link command
```

| Option | Meaning |
| --- | --- |
| `--shared` | build a shared library; the input is a module directory (or a single source file) |
| `-o <path>` | output path |
| `--soname <name>` | `DT_SONAME`; default is the base name of the output |
| `--linker <path\|name>` | linker program; default is detected (`MIO_LD`, `ld.lld`, `ld`, then `MIO_CC`/`CC`/`cc`/`gcc`/`clang`) |
| `--emit=object` | write the relocatable object instead of linking (shared mode only) |
| `--link`, `-L`, `-l`, `--rpath` | declared dependencies, see [Linker dependencies](#linker-dependencies) |
| `--verbose` | log the reproducible link command on stderr even when it succeeds |
| `--gc-stress` | run the library's collector with the stress threshold (testing) |

Pipeline: frontend and type check, typed lowering in PIC mode (`kiso/aot/lowerlinux/shared.mio`), the `ET_REL` object
(`kiso/aot/elfrel`), one linker invocation with an argv vector (never a shell string), then verification of the linked
file (`selfhost/pipeline/shared_linux.mio`). The verifier checks the dynamic string table for the requested SONAME
and requires RELRO and immediate binding. A link failure reports `E_SHARED_LINK` and preserves the linker’s stderr
and command line. Verification failures report `E_SHARED_VERIFY`. Both failures leave the requested output path untouched.

## C ABI export guide

A function is exported when it is `koukai` (public) and carries `export "c_symbol"` after its return type:

```mio
koukai waza dot(a: int64, b: int64, x: float64) -> float64 export "mio_dot" { kaesu float64(a * b) * x; }
```

* `export` is a contextual word: before this change only `=` or `{` could follow a signature, so no existing program
  changes meaning, and there is no new keyword.
* The signature must be an explicit prototype of scalar types: `int8 int16 int32 int64 uint8 uint16 uint32 uint64
  float32 float64 bool cptr`, result a scalar or `unit`. No type parameters. The symbol must be a valid C identifier
  other than `main` or a name starting with `mio_ffi_v1_`. Violations are compile errors (`E_CABI`).
* Only exported functions become dynamic symbols (`GLOBAL FUNC`, default visibility). Everything else, including every
  runtime routine, is not in `.dynsym`. Local function symbols exist in `.symtab` for debuggers only.
* Duplicate symbols in one module or across aggregated modules are `E_SHARED_EXPORT` errors. A build without any
  export is an error.
* Entry points follow System V AMD64: integer/pointer arguments in `rdi rsi rdx rcx r8 r9`, floating arguments in
  `xmm0-7`, the rest on the stack (8-byte slots), results in `rax`/`xmm0`. Narrow integers are extended to 64 bits on
  entry (the caller only guarantees 32), `bool` is normalised, `float32` travels as 32 bits. `rbx rbp r12-r15` are
  saved and restored (tested with sentinel values from a C host).

**Ownership rule.** No GC object ever crosses the boundary. A library that needs state owns it and gives C an opaque
integer handle (see `tests/shared/linux-amd64/modules/`): `mio_bytes_new` returns a handle, later calls take it, and
`mio_bytes_free` releases it. Invalid handles return negative statuses; they never fault. Memory owned by C is not
readable from Mio.

**Runtime model.** Each `.so` carries a private Mio runtime in its zero-filled data area (`.bss`, not exported): heap,
collector state and the module globals. The first exported call reserves the heap (8 GiB address space, lazily
committed, plus a 512 MiB mark stack) and runs the top-level code of every module once. Two libraries loaded in the
same process therefore have independent heaps and independent globals; to share state, put the modules in one
aggregate library. The runtime installs **no** signal handlers and never touches libc. A runtime trap (index out of
range, division by zero, nil use) writes a message to stderr and terminates the process with status 1, exactly as an
executable would. `dlclose` unmaps the library; the heap reservation is released by the kernel at process exit. Calling
a library from several threads at once is not supported.

## Multi-module libraries

```sh
mio build mio_math mio_bytes mio_text mio_collections --shared --soname libmio_std.so -o libmio_std.so
```

Each argument is a module directory. The compiler writes a private facade package that imports every directory by
absolute path and compiles all of them together, so symbols resolve in the type checker and no source is copied. The
result exports the union of the modules' exports; internal functions stay internal. The package alias is the directory
name, so two inputs with the same directory name conflict. Standard-library modules (`yobu "kiso/math"` and so on) are
compiled into the library that imports them.

## Linker dependencies

A pure Mio module needs no libc: the link is `ld -shared -soname ... -z noexecstack -z relro -z now -z text
--no-undefined --hash-style=gnu`, with no CRT files and no `DT_NEEDED`. A module that calls C declares both halves:

```mio
waza c_sqrt(x: float64) -> float64 = c "sqrt";
koukai waza root(x: float64) -> float64 export "mio_root" { kaesu c_sqrt(x); }
```

```sh
mio build mod --shared -lm -o libroot.so          # DT_NEEDED libm.so.6, call through the PLT
mio build mod --shared -o libroot.so              # E_SHARED_LINK: undefined reference to `sqrt`
```

`--link <file.o|.a|.so>`, `-L<dir>`, `-l<name>` and `--rpath <dir>` are passed to the linker in the given order. With a
compiler driver (`cc`, `gcc`, `clang`) the link adds `-nostdlib`, so nothing is linked that was not declared.
`--no-undefined` and `-z text` turn missing libraries and non-PIC objects into link errors: a `-fno-pic` object passed to
`--link` is refused. A missing linker is `E_SHARED_LINKER` (explicit `--linker` not found, or none installed).
`MIO_LD` selects a linker without a flag. The linker's stderr is passed through unchanged. All of this goes through
one function (`link_shared`), the seam where a Mio native linker will replace the system linker.

## Supported relocations

The code generator produces exactly two relocation types, and the `ET_REL` writer accepts exactly these two:

| Type | Use |
| --- | --- |
| `R_X86_64_PC32` | rip-relative reference to the data area (`lea reg, [rip + disp32]`, addend `offset - 4`), resolved by the linker inside the object |
| `R_X86_64_PLT32` | `call symbol@PLT` to a declared C dependency, resolved to a PLT entry and `R_X86_64_JUMP_SLOT` |

`GOTPCREL`, `GOTPCRELX`, `R_X86_64_64` and `RELATIVE` are not needed because Mio code addresses its own data
rip-relatively and holds no absolute pointers in data. The writer rejects unknown relocation types with
`UNSUPPORTED_RELOCATION type N` and checks the rel32 spans and addends. A self-contained library has no `.rela.dyn` at all.

## Known limitations

* Scalar-only boundary; strings, arrays and objects need handle conventions (above). No `cptr` dereference.
* Traps terminate the host process; there is no recoverable error path across the boundary.
* One mutator per library; no OS threads (`kiso/thread`) inside a shared library, no concurrent first calls.
* Each library reserves 8.5 GiB of address space; hosts with strict `RLIMIT_AS` need a larger limit.
* Compiler diagnostics for a malformed `export` reuse the C-declaration message ("expected a C declaration suffix").
* No `-fvisibility`-style control beyond exporting exactly the declared names; no version scripts; no `DT_INIT`.
* Linux x86-64 hosts only; the Go reference compiler parses `export` but never emits a library.

## Tests

```sh
mio test bunko/kiso/aot/elfrel                                   # ET_REL writer unit tests
mio test selfhost/pipeline --run='shared|linker|rejects|accepts' # verifier and linker helpers
python3 tests/shared/linux-amd64/run.py --mio <mio> [--baseline <mio-from-base-commit>] --output build/shared
```

The acceptance script builds the four libraries (`libmio_math.so`, `libmio_bytes.so`, `libmio_text.so`,
`libmio_collections.so`) and the aggregate `libmio_std.so` from `tests/shared/linux-amd64/modules`, inspects every file
with `file` (when installed), `readelf -h -l -S -d -s -r`, `nm -D`, `objdump -T` and `ldd -r`, then runs
`tests/shared/linux-amd64/host/host.c` (compiled with gcc and clang when present): `dlopen(RTLD_NOW|RTLD_LOCAL)`,
`dlsym`, argument/result checks for every export, callee-saved register preservation, a collector stress, five
libraries loaded together and closed in another order, and repeated open/close. It also checks the negative
diagnostics, the declared-dependency link, `--emit=object`, reproducibility, and (with `--baseline`) that static
executables are byte-identical to the base compiler's. Statuses are PASS, FAIL and NOT_RUN; a missing tool is NOT_RUN,
never PASS. The Native workflow runs it on the linux-amd64 hub as the required check `linux-shared`.
