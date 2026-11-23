# Native dynamic library fixtures

The C sources build into five independent real DLLs on Windows AMD64 or five real dylibs on macOS. Binaries and reports go under ignored `build/`; never commit them. The production Windows support DLL is built alongside fixtures from `native/ffi/windows.c`.

```text
python native-tests/build.py --output build/native-ffi
python native-tests/run.py --mio <native-mio-cli.exe> --output build/native-ffi
```

Use an updated native CLI built with the new `win64abi` module, not an old installed compiler. The Windows driver discovers installed MSVC/clang-cl plus SDK, builds all DLLs with warnings as errors, compiles real Mio PE images and executes them (normal and GC stress). It checks byte-exact stdout and exit status, records commands/logs and SHA-256 of compiler/DLLs, and separately tests fail-stop stale/mistyped calls. Supplemental ctypes tests cover loader busy pins, malformed input, capacity and native resource cleanup; they do not substitute for Mio calls. `example.mio` is a complete executable example; put its EXE next to the fixture/helper DLLs.

For a local source-built CLI:

```text
<existing-native-cli.exe> build selfhost/cli --bunko-root bunko --target=windows-amd64 -o build/mio-new.exe
python native-tests/run.py --mio build/mio-new.exe --output build/native-ffi
```

The tests run the generated EXE with a different working directory, so relative-path tests prove executable-directory behavior. A bare fixture basename is deliberately rejected because it only searches System32.

On an actual Mac with Xcode Command Line Tools:

```text
python native-tests/darwin.py --output build/native-ffi-darwin
```

This builds `libmio_ffi_{math,string,buffer,state,version}.dylib` with clang and invokes their exports with a C ABI host. On Apple Silicon, add `--mio <current-native-cli>` to link all five dylibs through the upstream Darwin C ABI and execute `darwin.mio` in normal and GC-stress modes. The Darwin ARM64 native-platform CI invokes this path. The checked runtime open/symbol/token-call loader remains Windows-only and is explicitly recorded NOT_RUN on Darwin. On Windows the Darwin driver returns nonzero/NOT_RUN, without substituting a PE file.

## Optional real-library integration

Supply existing AMD64 DLLs; no installation or network operation occurs during testing:

```text
python native-tests/run.py --mio build/mio-new.exe --output build/native-ffi-third-party --sqlite C:/deps/sqlite3.dll --zlib C:/deps/zlib1.dll --openssl C:/deps/libcrypto-3-x64.dll
```

Provision dependencies beforehand with a pinned vcpkg manifest/baseline (ports `sqlite3`, `zlib`, `openssl`, triplet `x64-windows`) and pass the actual DLL paths under `installed/x64-windows/bin`. Naming differs by package/distributor. Alternatively use existing Python SQLite and Git for Windows zlib/OpenSSL installations, as in the local validation. An installation used for reproducible CI should pin package versions or archive checksums; the report records DLL SHA-256. Default CI keeps these dependencies optional and names each missing test NOT_RUN.

With an already installed vcpkg checkout pinned to your reviewed baseline, provision before running tests:

```text
C:/vcpkg/vcpkg.exe install sqlite3:x64-windows zlib:x64-windows openssl:x64-windows
```

| Function | Windows AMD64 C prototype |
| --- | --- |
| sqlite3_initialize / sqlite3_shutdown | int (void) |
| sqlite3_libversion | const char* (void) |
| sqlite3_open | int (const char*, sqlite3**) |
| sqlite3_close | int (sqlite3*) |
| compressBound | unsigned long (unsigned long) |
| compress / uncompress | int (unsigned char*, unsigned long*, const unsigned char*, unsigned long) |
| OpenSSL_version | const char* (int) |

Windows `unsigned long` is **32 bits**; macOS uses 64 bits. The zlib test checks a real compression/decompression round trip. SQLite opens/closes `:memory:` using native input and pointer-out buffers. Version functions return nonnull C pointers. Explicit SQLite initialization supports distributions with OMIT_AUTOINIT. All these calls execute in generated Mio PE applications, including GC stress.

System tests invoke `GetCurrentProcessId` from kernel32, `IsWindow(NULL)` from user32 and `BCryptGenRandom` from bcrypt. No interactive desktop is needed and random bytes are not compared to nondeterministic expected data.

Full ABI, lifecycle, path policy and limitations: [Native FFI ABI v1](../docs/ffi-abi.md).
