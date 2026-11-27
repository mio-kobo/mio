# Linux distribution and libc compatibility

`run.py` checks a native Mio CLI in the current Linux x86-64 environment: the CLI itself (static ELF), a static
executable using only the embedded Bunko, a dynamically linked executable (system C driver), `kiso/ffi/linux` run-time
loading and native memory against the environment's own C library (normally and with `--gc-stress`), the package
tests (glibc), shared libraries built with the native and the system linker and loaded by a C host compiled in the
environment, and binaries built on another distribution (`--prebuilt`).

```sh
python3 tests/linux-compat/run.py --prebuild-only --mio <mio> --output build/compat-prebuilt      # on the build host
docker run --rm --platform linux/amd64 -v "$PWD:/w" -w /w debian:12 sh -c \
  "sh tests/linux-compat/setup.sh && python3 tests/linux-compat/run.py --mio <mio> --prebuilt build/compat-prebuilt --output build/compat/debian-12"
```

Statuses: PASS, FAIL, NOT_RUN (prerequisite missing), NOT_AVAILABLE (feature not in this source tree), UNSUPPORTED
(documented incompatibility, for example a glibc-linked binary on musl). Only PASS counts as a pass; the driver refuses
to run on a non-x86-64 machine (emulation is not evidence). The Native workflow runs it as the non-required matrix
`Linux compatibility / <image>` for debian:12, debian:13, ubuntu:22.04, ubuntu:24.04 and alpine:3.20.
