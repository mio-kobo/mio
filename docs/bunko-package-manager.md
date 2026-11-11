# Bunko client implementation status

This change is a **partial implementation for the Mio-2.2.0 branch**, not a
production-ready native package manager. The seven commands work in the Go
reference CLI. Native distributions provide command discovery and localized
help, but every operational `mio bunko` invocation returns the nonzero
`BUNKO_E_NATIVE_CAPABILITY` diagnostic without changing files. Native external
package imports are not implemented. Do not advertise installation in Core or
Full until those gaps are closed.

## Reference CLI

```sh
mio bunko init
mio bunko add alice/json@1.0.0
mio bunko remove alice/json
mio bunko install
mio bunko list
mio bunko info alice/json
mio bunko search json
mio bunko add --help
```

These are examples of syntax, not assertions that these example packages exist.
Commands operate on `bunko.toml` in the current working directory. Compilation
finds the nearest ancestor manifest starting at the entry source file. A manifest
contains only exact direct dependencies:

```toml
[dependencies]
"alice/json" = "1.0.0"
```

There is no dependency lock file. The graph must contain one version of each
package; conflicting paths and cycles fail instead of choosing a version.
`init` preserves existing declarations. `add` publishes the changed manifest only
after the entire graph has resolved and installed successfully. A failed operation
may leave successfully installed dependencies in the shared cache, but it does
not change the manifest or replace existing cache entries. `remove` retains the
cache. Comments and formatting are normalized when a manifest is edited.

The Go reference version banner and `VERSION` remain the base branch's
`2.1.2-dev` / `2.1.2`. Offline loader compatibility currently uses
`internal/bunko.ReferenceCompilerVersion`, also `2.1.2`; update it together with
the version banner when the branch receives its 2.2.0 release bump. Tests use
explicit 2.2.0 clients and fixtures supporting 2.2.0, not the published
`exmikupro/miobench@0.1.1` record, whose upper bound excludes 2.2.0.

## Global cache and offline imports

The cache defaults to the OS user cache directory plus `mio/bunko/v1`:

- Linux: `$XDG_CACHE_HOME/mio/bunko/v1` or `$HOME/.cache/mio/bunko/v1`.
- macOS: `$HOME/Library/Caches/mio/bunko/v1`.
- Windows: `%LocalAppData%\mio\bunko\v1`.

Set `MIO_BUNKO_CACHE` to override this location. It is entirely separate from
`MIO_BUNKO`, which still selects the standard library. Cache entries retain the
exact metadata, checksummed archive and source tree. Compilation checks both the
archive and the extracted files; it never downloads or repairs packages. Missing
entries direct the user to `mio bunko install`.

For example, `yobu "alice/json";` resolves the declared package or a subdirectory
beneath it. Imports from a package can use its own subdirectories and declared
dependencies. Root source cannot directly import an undeclared transitive
package. Existing local imports, import cycles, deterministic file order,
closed in-memory source filesystems, and `kiso/` behavior are preserved. External
`kiso/*` dependencies are rejected.

Project mutations use a transient `.bunko-manifest-write` directory for writer
exclusion; it contains no dependency state. After a process is killed, check that
no installer is running before removing this directory. Cache installers use
unique staging directories and publish only complete entries. Stale `.install-*`
directories after a forced termination are unreferenced and may be removed while
no installer is active. A corrupt published entry is reported, never silently
replaced; remove that exact entry explicitly and reinstall after investigating.
The cache assumes a private, user-controlled filesystem, not a shared writable
network directory or an attacker with the same OS account.

Revocations and namespace/platform policy are refreshed on online commands and
saved in `policy.json`. Offline compilation uses the last successfully fetched
policy. It cannot discover a revocation while offline. Newly fetched installs
must pass the current policy, even if their archive was already cached.

## Official index contract

Audited index revision:
[`e6c89a1`](https://github.com/mio-kobo/bunko-index/tree/e6c89a1890a782ff759c698ea527df85bbf2e9c0).
The authoritative sources are its [package format](https://github.com/mio-kobo/bunko-index/blob/e6c89a1890a782ff759c698ea527df85bbf2e9c0/docs/package-format.md),
[archive validator](https://github.com/mio-kobo/bunko-index/blob/e6c89a1890a782ff759c698ea527df85bbf2e9c0/tools/bunko_archive.py),
[provenance verifier](https://github.com/mio-kobo/bunko-index/blob/e6c89a1890a782ff759c698ea527df85bbf2e9c0/tools/bunko_source.py),
reserved namespace, revocation and platform policies. The Mio documentation site's
November 9 Bunko review page is an older snapshot; current index code already
requires platforms and verifies immutable releases in CI.

The client resolves the official recursive Git tree once and reads policies and
records at that exact revision. Search uses the published tree and metadata,
not an invented search service. Truncated or oversized responses fail closed.
The graph is bounded to 2000 packages and 256 dependency edges per path.
No custom index URL or HTTP trust override is exposed. HTTPS redirects are
restricted to GitHub download hosts; credentials are not accepted or forwarded.
GitHub anonymous API rate limits can therefore prevent large installs/searches.

Every new archive requires a published immutable release, matching asset ID and
repository, uploaded `.tar.gz` asset, matching reported size/digest, and a release
tag resolving to `source_commit`. SHA-256 is checked before extraction. Archives
have no stripped wrapper directory. Links, devices, sparse entries, executables,
scripts, binary content, traversal, nonportable names and case collisions fail.
The index limits are enforced: 32 MiB compressed, 2000 files, 8 MiB per file,
64 MiB total, path length 200 and depth 12. Additional defensive transport bounds
are 80 MiB decompressed tar data and 10,000 total visible archive entries.
No package code is executed by installation.

## Native infrastructure blocker and follow-up

The native runtime has `kiso/crypto/sha256` and TCP. It lacks a supported TLS/HTTPS
client, gzip/tar parser, TOML parser, exclusive file creation and atomic rename
interfaces usable by the installer on all six platforms. The filesystem API's
`open_write` truncates; it cannot safely implement idempotent concurrent init or
atomic manifest replacement. Shelling out to curl, tar, Go or Python would
violate the native distribution contract and is not a fallback.

A follow-up needs pure Mio bounded TOML/archive implementations, a reviewed TLS
and certificate-validation strategy consistent with Core/Full, portable atomic
filesystem runtime bindings and their ABI tests, then native graph/cache/import
integration. Ordinary native module tests and independent security oracles must
accompany those additions. This PR does not change the runtime, ABI, distribution
payload, language syntax or release process to hide those missing capabilities.

The client recognizes all six canonical OS/architecture pairs, but accepts
records only within the fetched index platform policy. That policy currently
admits `linux/x86_64`, `macos/arm64`, `windows/x86_64`. A **separate index PR**
needs to add `linux/arm64`, `macos/x86_64`, `windows/arm64` with verified official
2.2.0 compiler assets, SHA-256 values, hosted runner names and executable paths;
update the existing targets' compiler pins to a compatible 2.2.0 release and
run the index's validator and package-tooling matrix. No index files are changed
here. Already published records cannot be amended: publishers must submit new
versions to expand their target/compiler support.

## Localization and verification

New messages use the existing 39 locale catalogs and generation script. English,
Japanese and Simplified Chinese text is supplied; other catalogs currently carry
English text for these new keys. Technical error details remain English. Native
smoke checks cover help and the explicit capability failure, not installation.
Reference tests use controlled HTTPS transports and source archives; none depend
on external network availability. Platform compatibility unit tests cover all
six pairs, which is not native end-to-end evidence on those platforms.
