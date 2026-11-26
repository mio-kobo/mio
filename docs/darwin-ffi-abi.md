# Mio FFI ABI v1 on Darwin ARM64

Mio can build Apple Silicon Mach-O shared libraries from scalar source functions. A library has an install name of `@rpath/<output filename>` and an ad-hoc signature. The native Mio compiler writes the image, the export trie and symbol table, and the signature. A self-contained dylib has one load dependency, macOS libSystem.

## Build and link a library

```sh
mio build --kind dylib tests/ffi/dylib/math.mio \
  --export add=mio_math_add --export root=mio_math_sqrt -o libmio_math.dylib
```

Each `--export source=symbol` entry selects a concrete function in the entry module and assigns a C symbol beginning with `mio_`. This list controls exports independently of Mio's `koukai` visibility. The implementation adds Mach-O's leading underscore. C declarations and `darwin.symbol` use the names without that underscore.

Every library publishes `int64_t mio_module_abi_version(void)`. It returns `1`. The `mio_ffi_v1_` namespace is reserved for signature aliases. A name of the form `mio_ffi_v1_<result><parameters>_<symbol>` denotes a specific prototype and points at the same text entry as the corresponding C symbol. Codes use the fixed C ABI scalar table below in declaration order. These aliases are for runtime lookup.

| Mio type | C type | Code |
| --- | --- | --- |
| `unit` | `void` (result only) | `v` |
| `int8` | `int8_t` | `a` |
| `uint8` | `uint8_t` | `b` |
| `int16` | `int16_t` | `c` |
| `uint16` | `uint16_t` | `d` |
| `int32` | `int32_t` | `e` |
| `uint32` | `uint32_t` | `f` |
| `int64` | `int64_t` | `g` |
| `uint64` | `uint64_t` | `h` |
| `float32` | `float` | `i` |
| `float64` | `double` | `j` |
| `cptr` | opaque C pointer | `k` |
| `bool` | `_Bool` (eight bits, canonical values 0 and 1) | `l` |

Exports support up to 256 fixed scalar parameters. The entry thunk reads the two Darwin register banks independently and unpacks stack arguments at their natural alignment. It repacks those values into Mio's sequential register and machine-word stack convention. The thunk preserves callee-saved registers and returns in `x0` or `v0`. Previously published ABI v1 aliases retain their names.

## Link native dependencies

A library may declare fixed scalar C imports and link with `--link`, `-L`, `-l`, `-F`, `-framework`, and `--rpath`. Reachable C imports or explicit linker arguments select Apple Clang and ld to link the Mio-emitted ARM64 code. The final image receives an ad-hoc signature and retains the same explicit export list and ABI aliases. Self-contained builds use Mio's Mach-O writer and signer without Xcode.

The install name remains `@rpath/<output filename>`. Add runpaths that match deployment. For example, `--rpath @loader_path/../lib` resolves dependencies relative to the generated dylib. `@executable_path` resolves relative to the program dyld launched. Transitive libraries declare their own install names and dependencies. An undefined import fails the build with an Apple linker diagnostic, and a missing load dependency surfaces through `darwin.open` as a dyld diagnostic.

## Private data and lifecycle

Scalar `teisu` values are compiled as constants. A private mutable global may start with a scalar literal. That literal becomes bytes in the library's writable data section. A library can explicitly export initialization and finalization functions. dyld does not run Mio top-level code when opening or closing the image.

References to the same loaded image share its globals. Separately loaded images can have independent private state. A stateful library requires the caller to serialize access to its globals. Call explicit initialization and finalization according to the library's API.

## Open, bind and call from Mio

Import `kiso/ffi/darwin`. Check the `error` field of every result. An empty string denotes success. `darwin.open(path)` takes one dyld reference with `RTLD_NOW | RTLD_LOCAL | RTLD_FIRST`, so lookup is confined to the requested image. Each successful open has an independent ticket. `darwin.close(library)` returns a diagnostic string and invalidates that ticket and its bound functions on success. Close all owned tickets explicitly.

Bind with `darwin.symbol(library, "mio_math_add", darwin.Prototype.Int64FromTwoInt64())`. The module copies `dlerror` messages, checks the matching signature alias, and verifies ABI version 1 before issuing a Mio function ticket. The symbol and its alias must resolve to the same ARM64 text address. A function ticket holds no public address. Calling a ticket with the wrong typed invocation function or after its library closes returns an error.

| Prototype | Invocation |
| --- | --- |
| `Int64` | `invoke_int64(function)` |
| `Int64FromInt64` | `invoke_int64_1(function, a)` |
| `Int64FromTwoInt64` | `invoke_int64_2(function, a, b)` |
| `Float64FromFloat64` | `invoke_float64(function, a)` |
| `Float64FromTwoInt64` | `invoke_float64_from_ints(function, a, b)` |
| `BoolFromInt64` | `invoke_bool(function, a)` |

For fixed scalar prototypes, use `bind_c` for a C library or `bind_mio` for a Mio producer. The two functions share the same `open` and `close` API. Declare the exact C calling convention when using `bind_c`. dyld returns a symbol address, and the C prototype comes entirely from the caller. Functions must resolve to aligned addresses in the loaded image's ARM64 text section. The function API rejects data exports in other sections. A deliberately mislabeled symbol in a trusted library can still violate the declared contract.

```mio
yobu "kiso/ffi/darwin";

kaku opened = darwin.open("/usr/lib/libSystem.B.dylib");
kaku library = opened.library;
moshi (library != mu) {
    kaku binding = darwin.bind_c(library, "getpid", darwin.CPrototype {
        result: darwin.Scalar.I32(), parameters: []
    });
    kaku function = binding.function;
    moshi (function != mu) {
        kaku result = darwin.invoke(function, []);
        moshi (result.error != "") { chant(result.error); }
        kaku released = darwin.release(function);
        moshi (released != "") { chant(released); }
    } sore { chant(binding.error); }
    kaku closed = darwin.close(library);
    moshi (closed != "") { chant(closed); }
} sore { chant(opened.error); }
```

`CPrototype` takes up to 256 parameter types. `Scalar` supports `I8`, `U8`, `I16`, `U16`, `I32`, `U32`, `I64`, `U64`, `F32`, `F64`, `Ptr`, and `Bool`. Use `Void` for a function without a C result. The matching `Value` constructors accept Mio's corresponding scalar types. `invoke` checks the argument count and tags before entering C and returns a tagged `Value`. GP and FP register banks are independent and further arguments use the Darwin stack layout.

`release(function)` invalidates that binding immediately. A successful `close` invalidates all bindings issued from that library ticket. A failed `dlclose` leaves that ticket available for a retry. Opening the same image twice gives independent dyld references. A released ticket or closed library ticket always returns an error when reused.

Load libraries from trusted sources. dyld can execute initializers during `open`. The signature alias is a producer declaration checked for consistency by the loader.

## Ownership and current limits

The loader owns temporary C strings and frees them before returning. Mio pointers to managed objects stay inside their originating Mio runtime. Library bodies use scalar locals, scalar globals and direct calls without a Mio heap. Explicit errors reject managed values, nullable values, closures, indirect calls, recursive or over-deep call graphs, and operations that can jump to a process-wide Mio runtime error, including integer division. The library entry has no GC work and preserves [Runtime ABI RA-1](runtime-abi.md) for its caller.

The loader supports Mio workers. Library and function registries are synchronized, and a checked lookup or call pins its library until completion. Close returns `library is busy` while the ticket is pinned or already closing. Releasing a binding blocks future calls. An admitted call keeps its pin and can finish. No registry lock is held while dyld initializes or finalizes an image or while an arbitrary C function runs. Errors are returned to the calling thread, using dyld's thread-local diagnostics where applicable.

The worker path publishes the existing GC roots and uses the direct C ABI's blocking transition. Native results are preserved across mutator reentry. Programs that use workers, extra C declarations, or linker flags use Apple ld to link the same Mio-generated bridge. A standalone loader with fixed libSystem imports uses the native Mach-O writer.

Stateless Mio exports can run concurrently in Mio and C hosts. Mutable module state requires caller-side serialization, and any native dependency's own concurrency contract applies. Callbacks, C aggregate values and variadic functions remain outside the supported loader contract. Programs with arbitrary C declarations continue to use the [Darwin C ABI build path](darwin-arm64-c-ffi.md).

## Native buffers

`alloc`, `alloc_zeroed`, `alloc_aligned`, and `c_string_buffer` return owned native buffers. Release them with `free`. Aligned allocations require a power of two of at least eight bytes. `resize` preserves the original alignment and the common byte prefix. On success it returns a new handle and invalidates the old handle and all of its views. Allocation failure preserves the original handle.

`slice` creates a bounded view, and `read_only` creates a view that rejects writes and mutable pointer extraction. Use `release_buffer` to drop a view or a borrowed handle. Views validate their own tickets and their root tickets before every access. Checked buffer operations serialize against concurrent release and resize. A slice of a read-only buffer remains read-only. Releasing an intermediate view leaves its sibling views valid while the common root lives.

`read` and `write` copy between Mio byte arrays and native memory. `copy` supports overlapping native regions. `read_scalar` and `write_scalar` use little-endian numeric layouts. Pointer loads use `unsafe_read_pointer` and require an aligned field initialized by C or a valid pointer stored with `write_scalar`. The pointee follows the producer's lifetime. `c_string_buffer` rejects embedded NUL bytes. `read_utf8` takes an exact byte length, and `read_c_string` requires a NUL within the specified range.

`pointer` returns a native address for a writable buffer. Keep the root alive while native code uses the address, including any time it retains the address after a call. Resizing or freeing the root during that lifetime makes the address invalid. `unsafe_borrow` creates a checked view over a caller-asserted foreign range. Release the borrow separately and let the original owner or its documented allocator release the underlying storage.

## Integration

```sh
python3 tests/ffi/dylib/run.py --mio /path/to/current/mio --output build/dylib
```

The driver builds and inspects the math, convert, and JSON libraries and runs a Mio loader and a separately Clang-linked C oracle. The JSON export tests the tokenizer's digit predicate. The convert export tests the decimal parser's binary scaling helper and assumes parser-sized exponents. The test driver records SHA-256 hashes, symbols, load results, ordinary and GC stress runs, and timing samples.
