# Windows AMD64 native DLLs

Mio can build a stateless, integer C interface as a real AMD64 PE32+ DLL:

```powershell
mio build tests/windows-dll/fixtures/math --target windows-amd64 --emit=dll --bunko-root bunko -o mio_math.dll
```

`--emit=dll` is a build output mode, independent of the output filename. It requires
the existing `windows-amd64` target. It cannot be combined with `--emit=mochi`,
GC stress, observation or symbol diagnostics. Windows EXE generation retains its
existing path when the option is absent.

## Architecture audit

The implementation builds on these source contracts:

| Question | Source-based finding |
| --- | --- |
| Existing Windows EXE generation? | Yes: `selfhost/pipeline/windows.mio` uses `lowerwinrt` and the pure-Mio PE writer. `lowerwin` is the separate scalar backend. |
| COFF object writer? | No Windows `.obj` producer in this path. `kiso/aot/pe` lays out a complete image directly. |
| External symbols? | Internal functions use assembler labels and fixups. `pe.Patch` resolves section-relative, RIP-relative and absolute addresses; Windows C imports use `win64c`/`win64abi` and IAT entries. |
| Base relocations? | Existing `va64` patches produce sorted, padded `IMAGE_REL_BASED_DIR64` blocks in `.reloc`. |
| DLL exports before this change? | No: the PE writer previously emitted EXE images without an Export Directory. |
| Existing heap runtime usable in a DLL? | Not established. The EXE runtime initializes process state and GC at its executable entry. This implementation uses the scalar backend and includes no heap runtime. |
| Multiple modules and state? | The frontend already resolves modules and codegen can qualify function names. DLLs link those functions into one image; mutable module state and initialization are rejected. |
| What was missing? | DLL image flags and no-entry semantics, a sorted export directory, externally callable ABI bridges, export selection/type validation, and real loader tests. |

No LLD or other linker dependency is added. `pe.dynamic_library` shares the existing
section layout, import encoding, relocation engine and `.pdata`/`.xdata` writer.
It adds read-only `.edata`, the Export Address Table, sorted Name Pointer Table,
Ordinal Table (base 1), and `IMAGE_FILE_DLL`. Export RVAs point to generated code.
`AddressOfEntryPoint` is zero: no DllMain executes under the loader lock.

## Export interface

```mio
koukai waza mio_add(a: int64, b: int64) -> int64 { kaesu a+b; }
```

Public (`koukai`) functions in the entry package are exported under their exact
ASCII source name, without decoration. Private functions and imported public
functions are not automatically exported. To expose an imported function, write
a public entry-package wrapper. Existing module visibility remains enforced by
the frontend. Duplicate definitions fail source resolution; duplicate binary
exports fail PE validation.

The first version accepts explicit `int64` parameters and an explicit `int64`
return type only, mapping to C `int64_t`. Zero through sixteen parameters are
supported. It rejects generic exports, inferred signatures, floats, pointers,
strings, arrays, closures and managed object interfaces. Existing C **imports**
support more types; that does not imply DLL **exports** support them.

The wrapper receives RCX/RDX/R8/R9 and subsequent stack arguments, reserves stack
space, and converts to the scalar backend's private argument stack. It returns
in RAX. The first private call has 16-byte alignment; Windows API boundaries
realign and reserve 32-byte shadow space. Returning scalar code modifies no
nonvolatile register except RBP, which is saved/restored. Frame-pointer unwind
records cover wrappers and internal functions. DLL functions do not implement
language exception propagation across a C boundary.

## Modules, runtime and lifetime

Only imports and function declarations are allowed at package scope in the
entry package and all dependencies. Global state, initializers, heap operations,
printing and native external declarations are outside this bounded implementation.
Reachable functions must be supported by the scalar backend, have no captures,
at most 256 local slots, and at most 16 parameters. Unsupported instructions
fail compilation. There is no VM fallback.

This avoids changing RA-1: the GC runtime is neither initialized nor exposed.
Each DLL contains stateless scalar code and private fatal-error helpers; there is
no shared Mio heap, no host-provided runtime, no GC allocation to free, and no
runtime teardown. Fatal arithmetic traps retain the scalar backend's behavior
of terminating the process rather than returning a managed exception. The private
helpers import `GetStdHandle`, `WriteFile`, and `ExitProcess` from KERNEL32.dll.

The host owns the DLL lifetime. Keep it loaded until all calls have returned;
discard function pointers before FreeLibrary. Independent DLLs do not exchange
Mio objects or runtime state. Inter-DLL function linking, shared GC/runtime state,
callbacks, TLS, delay/ordinal imports and general native-library linking are not
implemented by DLL output mode.

## Executable tests

The repository contains four independent minimal **fixtures**, rather than
claiming to export the complete standard string/array libraries:

| DLL | Undecorated exports |
| --- | --- |
| `mio_math.dll` | `mio_add`, `mio_sum6` |
| `mio_utility.dll` | `mio_abs` |
| `mio_bytes.dll` | `mio_low_byte` (scalar arithmetic, no buffer ABI) |
| `mio_encoding.dll` | `mio_decimal` (one ASCII digit) |
| `mio_standard.dll` | `mio_standard`, `mio_zero` |

`standard` imports all four fixture modules and calls across their boundaries.
Math and utility deliberately use different private functions with the same name;
qualified internal symbols keep their implementations distinct. Imports are
statically combined into the final DLL, not loaded from the independent DLLs.

With an AMD64 Windows host, Python 3.12+ and MSVC/Windows SDK installed:

```powershell
mio test bunko/kiso/aot/pe --bunko-root bunko
python tests/windows-dll/run.py --mio path/to/mio.exe --output build/windows-dll/results
dumpbin /headers /exports /imports /relocations /unwindinfo build/windows-dll/results/mio_math.dll
```

The driver writes `report.json`, command exit codes, image hashes and tool logs.
It checks PE32+/AMD64/DLL flags, exact sorted exports and ordinals, section/RVA
bounds, imports, DIR64 blocks, sorted unwind ranges, deterministic output, and
deliberately corrupted PE rejection. It compiles `host.c` with `/W4 /WX /O2`.
The C host loads five DLLs simultaneously, calls 64-bit/negative/zero/six-argument
and combined functions, verifies hidden exports, exercises nonpreferred image
bases, invokes `RtlVirtualUnwind`, and unloads/reloads all images three times.
It also checks the three DIR64 operands in each loaded image against that image's
actual IAT range, so relocation execution is checked even on unused trap paths.
Negative source tests reject unsupported ABI types, duplicate symbols and module
state; a normal Windows EXE must still execute and print 42.

The `Native / Windows AMD64 DLL exports` job in `native.yml` runs on
`windows-latest`, verifies the cross-built Stage A hash, builds this checkout's
native compiler, runs the PE unit suite and the C-host suite, and uploads logs.
The required summary fails if DLL acceptance does not succeed. Existing Linux,
macOS and Windows EXE jobs remain in the platform matrix. Local Windows results
are not a claim that those other platforms have executed locally.

## Recorded local validation

Validated on Windows AMD64, 2026-11-23, using MSVC 14.51 and Python 3.14:

- Native PE writer tests: 21 passed, zero failed.
- Existing Windows scalar backend tests: 4 passed, zero failed.
- DLL driver: five deterministic images; dumpbin and independent structural
  checks passed; six corrupted image variants rejected; nine invalid source
  fixtures and an invalid target rejected; C host build and execution passed.
- C host: three simultaneous load/call/unload cycles, with nonpreferred bases,
  rebased IAT operands, OS unwinding, private export checks and exact results.
- Existing `tests/pe-writer/run.py --mode native --execute`: PE/console structure,
  Windows execution, read-only stdout failure and corruption controls passed.
  Its optional reference-VM tests were not run; the native unit suites above
  were executed separately.
- Native CLI B and C self-builds produced identical SHA-256 values, including
  a self-build using embedded Bunko rather than an external library directory.

The CI result belongs to the PR check run and must be reported separately from
these local results. Generated DLLs, executables and per-command logs remain
under `build/windows-dll/` and are not committed.
