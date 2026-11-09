# Mio 2.1.1 Core / Full PR candidates

These archives are PR candidates (`2.1.1-pr.<source-sha-prefix>`), not v2.1.1
releases. The `Mio Core / Full PR candidates` workflow builds all three targets
through the existing `scripts/ci/native_platforms.py` orchestration. Stage A has
a go-hosted root; the shipped Full compiler must match native B, C and D byte for
byte. A second Core chain is built from an empty resource table by the verified
Full compiler and must reach its own B=C=D fixed point. Trusted seed verification
is **NOT_RUN** for PR and RC candidates. The stable workflow uses the
[pinned immutable seed](https://github.com/mio-kobo/mio/releases/tag/bootstrap-v2.1.0-seed.1).
`tools/native-ci`, its pinned seed, resource generator and source hashes have not
been repinned or relaxed.

Build inputs are copied to a disposable checkout. `prepare.py` keeps the generated
Bunko resource table for the Full compiler and stamps the candidate version and
full source commit into the CLI. `native_platforms.py` then copies only
`selfhost/` into a disposable Core source, applies the empty resource table, and
builds Core with the verified Full compiler. Internal Bunko implementations still
compile to machine code. The resulting archive layout is:

```
mio-2.1.1-pr.<sha>-<target>-core/
  bin/mio                   # Windows: mio.exe
  LICENSE
  README.md
  manifest.json
  provenance.json
mio-2.1.1-pr.<sha>-<target>-full/
  bin/mio                   # different SHA256 from Core; embeds Bunko
  LICENSE
  README.md
  manifest.json
  provenance.json
```

Darwin/Linux use tar.gz; Windows uses zip. Each target artifact also contains
SHA256SUMS, the native report and extracted-candidate verification results.
`manifest.json` lists every other regular file's exact byte length and SHA256; its
own integrity is covered by the archive checksum. `provenance.json` records
target, source commit, candidate version, ABI, embedded-Bunko state, compiler
hash, native report hash, Actions run and the actual trusted seed status. It is a
build record, not a cryptographic GitHub attestation of untrusted PR code.

Full's executable contains the complete production `kiso/` source tree (excluding
tests and historical dotted snapshot directories). The table is generated at
build time by the Mio-native `selfhost/embed-bunko` / `selfhost/resourcegen`
path; no host-language resource generator is used. Imports resolve from that
embedded table without extraction. Core is compiled with an empty table, so it
cannot accidentally gain the embedded library.

Bunko resolution for build/check/run/test/lsp is:

1. `--bunko-root <directory>` (nonempty, must contain `kiso/`).
2. `MIO_BUNKO` (an existing empty value is an error).
3. Embedded Bunko, when present (Full only).
4. `../bunko` from the operating system's actual executable path (Core and
   legacy layouts).
5. No standard library. Hello World still compiles; `kiso/...` imports report
   `E_BUNKO_MISSING`.

Invalid explicit/environment roots fail without falling back. Full therefore
prefers its own library over a legacy adjacent tree, while explicit overrides
still select a different one. An outdated `MIO_BUNKO` pointing at a missing or incomplete tree is still an
error for Full; unset it or point it at a complete compatible tree to use the
embedded library. Core keeps the v2.1.0 external behavior. LSP clients
should pass the CLI option or environment; `initializationOptions.bunkoRoot` does
not introduce a separate lookup path. The executable identity uses Linux
`/proc/self/exe`, Darwin `proc_info(PROC_PIDPATHINFO)`, and Windows
`GetModuleFileNameW`, via the empty-path request of the existing native `real_path`
binding (same signature and RA-1 calling convention).

Unpack Full, put `bin/` on PATH, then build for your native target explicitly:

```sh
mio version
mio build hello.mio --target=linux-amd64 -o hello
./hello
mio check math.mio
```

Full's `bin/mio` can be copied alone to another directory or computer with a
compatible OS and CPU. Use `darwin-arm64` / `windows-amd64` on those targets. The
CLI defaults to the native host target; `--target` can select a cross-compilation
target. `run` and `exec` require the host target. There is no runtime download and
no Go/Python requirement for installed packages. Source preparation, verification
and packaging scripts require Python only on the build host. See
[Docker offline environment](../../docker/README.md).

## Tagged release candidates

Dispatch `distribution-pr.yml` on an existing `v2.1.1-rc.N` tag with
`rc_version=2.1.1-rc.N` and no `stable_version`. RC and stable inputs are mutually
exclusive and must match the exact dispatch tag. This stamps the disposable
sources before native compilation, then runs the full three-platform native,
archive, real-manager and Docker pipeline. RC versions cannot enter stable
package-manager publication.

## Package manager infrastructure

See [installation instructions](../installation/README.md) (English, 简体中文,
日本語) and [release operations](../../scripts/package_managers/README.md). The
existing candidate verification remains required. Manager lifecycle tests run
only after it passes. Stable metadata is generated from actual stable Release
assets; PR versions can never activate publication. Publication remains disabled
pending maintainer configuration and approval.
