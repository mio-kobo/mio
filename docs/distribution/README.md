# Mio 2.1 Core / Full PR candidates

These archives are PR candidates (`2.1.0-pr.<source-sha-prefix>`), not v2.1.0 releases.
The `Mio Core / Full PR candidates` workflow builds all three targets through the
existing `scripts/ci/native_platforms.py` orchestration. Stage A has a Go-hosted
root; the shipped compiler must match native B, C and D byte for byte. This is
independent native verification, **not Trusted Seed verification**. The frozen
GF4 seed remains NOT_RUN on hosted runners. `tools/native-ci`, its pinned seed,
resource generator and source hashes have not been repinned or relaxed.

Build inputs are copied to a disposable checkout. `prepare.py` replaces only that
copy's generated Bunko resource table with an empty table and stamps the candidate
version and full source commit into the CLI. The original table's SHA256 and the
generated table's SHA256 are recorded. Internal Bunko implementations still
compile to machine code; neither archive contains an importable embedded library.
The legacy source checkout and frozen release entry points remain available.

For each of darwin-arm64 / linux-amd64 / windows-amd64:

```
mio-2.1.0-pr.<sha>-<target>-core/
  bin/mio                   # Windows: mio.exe
  LICENSE
  README.md
  manifest.json
  provenance.json
mio-2.1.0-pr.<sha>-<target>-full/
  bin/mio                   # identical SHA256 to Core
  bunko/kiso/**/*.mio        # production sources, no tests/patch snapshots
  examples/hello.mio
  examples/math.mio
  LICENSE
  README.md
  manifest.json
  provenance.json
```

Darwin/Linux use tar.gz; Windows uses zip. Each target artifact also contains
SHA256SUMS, the native report and extracted-candidate verification results.
`manifest.json` lists every other regular file's exact byte length and SHA256;
its own integrity is covered by the archive checksum. `provenance.json` records
target, source commit, candidate version, ABI, compiler hash, native report hash,
Actions run and the actual Trusted Seed status. It is a build record, not a
cryptographic GitHub attestation of untrusted PR code.

The tool whitelist is in `scripts/distribution/package.py`. Only integrated
`check/build/run/exec/test/lsp/disasm` are included today; no standalone helper is
approved. Windows process spawning, run/exec/test and LSP stdin polling are implemented
and are tested. Windows `kiso/net/tcp` has no Winsock runtime layer and remains
NOT_APPLICABLE. Platform statements follow the current native reports rather
than historical audits.

Bunko resolution for build/check/run/test/lsp is:

1. `--bunko-root <directory>` (nonempty, must contain kiso/).
2. `MIO_BUNKO` (an existing empty value is an error).
3. `../bunko` from the operating system's actual executable path.
4. No standard library. Hello World still compiles; `kiso/...` imports report
   `E_BUNKO_MISSING`.

Invalid explicit/environment roots fail without falling back. LSP clients should
pass the CLI option or environment; `initializationOptions.bunkoRoot` no longer
introduces a separate lookup path. The executable identity uses Linux
`/proc/self/exe`, Darwin `proc_info(PROC_PIDPATHINFO)`, and Windows
`GetModuleFileNameW`, via the empty-path request of the existing native `real_path`
binding (same signature and RA-1 calling convention). Executable-relative lookup
supports PATH launches and relocation. No lookup uses argv[0] or the working directory.

Unpack Full, put `bin/` on PATH, then build for your native target explicitly:

```sh
mio version
mio build examples/hello.mio --target=linux-amd64 -o hello
./hello
mio check examples/math.mio
```

Use `darwin-arm64` / `windows-amd64` on those targets. The CLI defaults to the native host target; an explicit `--target` can select
a cross-compilation target. `run` and `exec` require the host target.
There is no runtime download and no Go/Python requirement for installed packages.
The source preparation, verification and packaging scripts require Python only
on the build host. See [Docker offline environment](../../docker/README.md).

## Package manager infrastructure

See [installation instructions](../installation/README.md) (English, 简体中文,
日本語) and [release operations](../../scripts/package_managers/README.md).
The existing candidate verification remains required. Manager lifecycle tests run
only after it passes. Stable metadata is generated from actual stable Release
assets; PR versions can never activate publication. Publication remains disabled
pending maintainer configuration and approval.
