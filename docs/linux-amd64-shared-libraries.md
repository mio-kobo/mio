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
| Mio native linker for self-contained modules (`--linker native`, `kiso/aot/elfdyn`): no cc, ld or lld | implemented (milestone 1, see [Native linker](#native-linker)) |
| Native linking of declared C shared-library dependencies (`-l`, `-L`, `--link lib.so`, `--rpath`; `DT_NEEDED`, `DT_RUNPATH`, PLT/GOT, `R_X86_64_JUMP_SLOT`) | implemented (milestone 2, see [Native linker](#native-linker)); object files and archives are refused |
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
mio build mio_math --shared --linker native -o libmio_math.so # Mio links it; no external tool is run
```

| Option | Meaning |
| --- | --- |
| `--shared` | build a shared library; the input is a module directory (or a single source file) |
| `-o <path>` | output path |
| `--soname <name>` | `DT_SONAME`; default is the base name of the output |
| `--linker <path\|name>` | linker program; honors `MIO_LD`, then prefers a C driver for declared dependencies or a raw linker for a self-contained module. `--linker native` selects the Mio linker and never falls back |
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
The linker keeps globals from linked objects and archives local unless Mio declares them as exports.
`--no-undefined` and `-z text` turn missing libraries and non-PIC objects into link errors: a `-fno-pic` object passed to
`--link` is refused. A missing linker is `E_SHARED_LINKER` (explicit `--linker` not found, or none installed).
`MIO_LD` selects a linker without a flag. An explicitly selected raw `ld.lld` may need a `-L` directory for system
libraries. The default C driver supplies those search paths and keeps any user-provided `-L` entries. The linker’s
stderr is passed through unchanged. All of this goes through
one function (`link_shared`); `--linker native` goes through `link_native` instead (next section).

## Native linker

`mio build --shared --linker native` links the lowered module with `kiso/aot/elfdyn`, a pure Mio ELF64 `ET_DYN`
writer. It consumes the same linker input as the `ET_REL` writer (`elfrel.ObjectSpec`: code, data-area size, exports,
local symbols, relocations) and runs no external program: not `cc`, `ld`, `ld.lld` or any other tool. The CLI then runs
the same `verify_shared` check as for the system linker.

Milestone 1 covers **self-contained modules**, whose only relocations are `R_X86_64_PC32` references from `.text` to the
module's own data area; milestone 2 adds declared shared-library dependencies (below). The distance between the two is fixed at link time, so the linker resolves every reference
itself and the output needs no dynamic relocation, no GOT, no PLT and no `DT_NEEDED`:

| Part | Content |
| --- | --- |
| Segments | `PT_LOAD` R (headers, `.gnu.hash`, `.dynsym`, `.dynstr`), `PT_LOAD` R+X (`.text`), `PT_LOAD` R+W (`.dynamic`, then `.bss` from the next page), `PT_DYNAMIC`, `PT_GNU_RELRO` (the `.dynamic` page), `PT_GNU_STACK` without X |
| Dynamic tags | `DT_SONAME`, `DT_GNU_HASH`, `DT_STRTAB`, `DT_SYMTAB`, `DT_STRSZ`, `DT_SYMENT`, `DT_FLAGS=BIND_NOW`, `DT_FLAGS_1=NOW` |
| Dynamic symbols | exactly the exports, `GLOBAL FUNC` default visibility, grouped by GNU hash bucket; local functions only in `.symtab` |
| Determinism | the output is a pure function of the input; the acceptance test rebuilds and compares bytes |

### Declared shared-library dependencies

```sh
mio build mod --shared --linker native -lm -o libroot.so
mio build mod --shared --linker native -L /opt/x/lib -l example --rpath '$ORIGIN' -o libmodule.so
mio build mod --shared --linker native --link /usr/lib/x86_64-linux-gnu/libz.so.1 -o libmodule.so
```

The dependencies are read by `kiso/aot/elfdso`, which treats every file as untrusted input (all offsets, sizes, counts
and string indexes are bounds-checked):

* `-l name` is searched as `libname.so` in the `-L` directories, then `/lib/x86_64-linux-gnu`, `/usr/lib/x86_64-linux-gnu`,
  `/lib64`, `/usr/lib64`, `/lib`, `/usr/lib`, `/usr/local/lib`. `--link` takes an absolute path to a `.so`.
* A dependency is an ELF64 x86-64 `ET_DYN` with a section header table and `.dynsym`, or a GNU ld script limited to
  `OUTPUT_FORMAT`, `GROUP`/`INPUT` with absolute paths and `AS_NEEDED` (glibc's `libm.so` and `libc.so` are such scripts).
  Archives inside a script (`libc_nonshared.a`) are skipped; any other script command, a relative path or `-l` inside a
  script, nested scripts, other architectures, truncated or malformed files are errors.
* Every external C symbol the module calls must be a function (`STT_FUNC`, `STT_GNU_IFUNC` or `STT_NOTYPE`) defined by
  one of the declared libraries; the first library in declaration order wins. Otherwise: `E_SHARED_LINKER undefined
  symbol <name>: not defined by the declared libraries (...)`. Transitive dependencies are loaded by `ld.so` at run time
  but are not searched at link time, like GNU ld's default `--no-copy-dt-needed-entries`.
* `DT_NEEDED` lists the declared libraries (by `DT_SONAME`, or file name when a library has none) in declaration order;
  `AS_NEEDED` members only when they define a used symbol. `--rpath` values become one colon-joined `DT_RUNPATH`.
* Each import is an undefined `GLOBAL FUNC` dynamic symbol (placed before the GNU hash symbol offset), a 16-byte PLT stub
  `jmp *GOT(%rip)` in the executable segment and a `.got.plt` slot with an `R_X86_64_JUMP_SLOT` relocation (`DT_JMPREL`,
  `DT_PLTRELSZ`, `DT_PLTREL=RELA`, `DT_PLTGOT`). With `DF_BIND_NOW` every slot is bound before the library runs, so the
  GOT lies in `PT_GNU_RELRO` and no lazy-binding stub (PLT0) is needed. `R_X86_64_RELATIVE` and `R_X86_64_GLOB_DAT` are
  not generated: Mio code holds no absolute data pointers and references external symbols only through calls.
* No symbol versions are recorded; the loader binds each import to the library's default version of the symbol.

Not supported (refused): object files and archives as inputs, linker options other than `-l`/`-L`/`--link`/`--rpath`,
calls to the module's own exports through the PLT, external data references, TLS, symbol versioning, COMDAT and
linker scripts beyond the subset above. Unknown relocation types (`UNSUPPORTED_RELOCATION type N`), out-of-range or overlapping rel32 fields, duplicate exports
and invalid SONAMEs are link errors. None of these falls back to a system linker. `DT_HASH` (SysV hash) is not emitted,
which matches the system-linker path (`--hash-style=gnu`); loaders without `DT_GNU_HASH` support are not targeted.

The native linker is not a general-purpose ELF linker and is not GNU ld compatible: it links exactly what the Mio code
generator produces plus declared shared libraries.

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
* No `-fvisibility`-style control beyond exporting exactly the declared names; no user-provided version scripts or `DT_INIT` support.
* Linux x86-64 hosts only; the Go reference compiler parses `export` but never emits a library.

## Tests

```sh
mio test bunko/kiso/aot/elfrel                                   # ET_REL writer unit tests
mio test bunko/kiso/aot/elfdyn                                   # native ET_DYN linker unit tests
mio test bunko/kiso/aot/elfdso                                   # dependency reader (shared objects, ld scripts)
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
executables are byte-identical to the base compiler's. The same libraries are linked again with `--linker native`
while `PATH`, `MIO_LD` and `MIO_CC` point only to trap programs that record any execution (and under `strace -f -e
execve` when strace is installed), inspected with the same tools and loaded by the same C host suites. Statuses are PASS, FAIL and NOT_RUN; a missing tool is NOT_RUN,
never PASS. The Native workflow runs it on the linux-amd64 hub as the required check `linux-shared`.
