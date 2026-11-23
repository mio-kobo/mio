# Mio FFI ABI v1 on Darwin ARM64

Mio can build Apple Silicon Mach-O shared libraries from scalar source functions. A library has an install name of `@rpath/<output filename>` and an ad-hoc signature. The native Mio compiler writes the image, the export trie and symbol table, and the signature. Its only load dependency is macOS libSystem.

## Build and link a library

```sh
mio build --kind dylib tests/ffi/dylib/math.mio \
  --export add=mio_math_add --export root=mio_math_sqrt -o libmio_math.dylib
```

Each `--export source=symbol` entry selects a concrete function in the entry module and assigns a C symbol beginning with `mio_`. This list controls exports independently of Mio's `koukai` visibility. The implementation adds Mach-O's leading underscore. C declarations and `ffi.symbol` use the names without that underscore.

Every library publishes `int64_t mio_module_abi_version(void)`. It returns `1`. The `mio_ffi_v1_` namespace is reserved for signature aliases. A name of the form `mio_ffi_v1_<result><parameters>_<symbol>` denotes a specific prototype and points at the same text entry as the corresponding C symbol. Codes use the fixed C ABI scalar table below in declaration order. These aliases are for runtime lookup.

| Mio type | C type | Code |
| --- | --- | --- |
| `int32` | `int32_t` | `e` |
| `uint32` | `uint32_t` | `f` |
| `int64` | `int64_t` | `g` |
| `float32` | `float` | `i` |
| `float64` | `double` | `j` |
| `bool` | `_Bool` (eight bits, canonical values 0 and 1) | `l` |

Exports support up to eight fixed parameters and one scalar result. C reaches a thunk that collects the separate Darwin GP and FP argument banks into Mio's machine-word convention, including the eighth GP argument in `x7`. It uses a 16-byte-aligned stack, preserves the ABI's callee-saved registers and returns in `x0` or `v0`. The library code uses PC-relative intra-image branches. The writer also supports bounds-checked ADRP/ADD patches to image text, constants, and zero-initialized data.

## Open, bind and call from Mio

Import `kiso/ffi`. Check the `error` field of every result. An empty string denotes success. `ffi.open(path)` takes one dyld reference with `RTLD_NOW | RTLD_LOCAL | RTLD_FIRST`, so lookup is confined to the requested image. Each successful open has an independent ticket. `ffi.close(library)` returns a diagnostic string and invalidates that ticket and its bound functions on success. Close all owned tickets explicitly.

Bind with `ffi.symbol(library, "mio_math_add", ffi.Prototype.Int64FromTwoInt64())`. The module copies `dlerror` messages, checks the signature alias and its address, and verifies that the address lies in the loaded image's ARM64 text section. A function ticket holds no public address. Calling a ticket with the wrong typed invocation function or after its library closes returns an error.

| Prototype | Invocation |
| --- | --- |
| `Int64` | `invoke_int64(function)` |
| `Int64FromInt64` | `invoke_int64_1(function, a)` |
| `Int64FromTwoInt64` | `invoke_int64_2(function, a, b)` |
| `Float64FromFloat64` | `invoke_float64(function, a)` |
| `Float64FromTwoInt64` | `invoke_float64_from_ints(function, a, b)` |
| `BoolFromInt64` | `invoke_bool(function, a)` |

Load libraries from trusted sources. dyld can execute initializers during `open`. The signature alias is a producer declaration checked for consistency by the loader.

## Ownership and current limits

The loader owns temporary C strings and frees them before returning. Mio pointers to managed objects stay inside their originating Mio runtime. Library bodies use scalar locals and calls and can run without a Mio heap, initializers, or runtime globals. Explicit errors reject managed values, nullable values, closures, indirect calls, recursive or over-deep call graphs, external calls from library bodies, and operations that can jump to a process-wide Mio runtime error, including integer division. The library entry has no GC work and preserves [Runtime ABI RA-1](runtime-abi.md) for its caller.

This slice supports single-mutator Mio hosts with the six invocation signatures above. `uint64_t`, pointers and opaque foreign values as export arguments or results, byte buffers, C aggregate values, variadic functions, callbacks, and arbitrary linker inputs for a loader program remain future extensions. A loader build uses the fixed libSystem imports through the native Darwin Mach-O writer. Programs with arbitrary C declarations continue to use the [Darwin C ABI build path](darwin-arm64-c-ffi.md).

## Integration

```sh
python3 tests/ffi/dylib/run.py --mio /path/to/current/mio --output build/dylib
```

The driver builds and inspects the math, convert, and JSON libraries and runs a Mio loader and a separately Clang-linked C oracle. The JSON export tests the tokenizer's digit predicate. The convert export tests the decimal parser's binary scaling helper and assumes parser-sized exponents. The test driver records SHA-256 hashes, symbols, load results, ordinary and GC stress runs, and timing samples.
