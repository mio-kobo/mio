[English](CHANGELOG.md) | [简体中文](docs/CHANGELOG.zh-CN.md) | [日本語](docs/CHANGELOG.ja.md)

---

# Changelog

## 2.1.2 — Unreleased

- **Localized CLI:** every help page, option description, friendly error, warning and human-readable test result is available in 39 locales. Catalogs use BCP 47 tags, normalize POSIX forms such as `zh_CN.UTF-8`, fall back from region to language and finally to `en-US`, and never contact a network service at runtime.
- **Global language selection:** `--lang <locale>` and `--lang=<locale>` work before or after the command (`mio --lang ja-JP help`, `mio help build --lang fr-FR`). The priority is explicit `--lang`, `MIO_LANG`, `LANGUAGE`/`LC_ALL`/`LC_MESSAGES`/`LANG`, the operating-system preference, then `en-US`. The native CLI reads the macOS `AppleLanguages` preference and the Windows user locale on a best-effort basis; failure always falls back instead of blocking startup.
- **Adaptive help:** Help is grouped by command family (General, Compilation, Execution, Diagnostics, Testing, Development tools), documents every real subcommand, follows terminal width via `COLUMNS`, and uses semantic roles instead of ad-hoc colors. `--theme=auto|dark|light` selects a palette; `auto` uses `MIO_THEME`/`COLORFGBG` when available and otherwise keeps high-contrast ANSI 16-color semantics instead of guessing the background.
- **Color compatibility:** `--color=auto|always|never` keeps its meaning. Explicit CLI values win over `NO_COLOR`, `TERM=dumb`, `CLICOLOR` and `CLICOLOR_FORCE`; non-TTY output is plain. JSON, LSP frames and error codes remain machine-stable and never receive ANSI escapes or localized protocol fields.
- **Single catalog for both CLIs:** `selfhost/i18n/locales/*.json` are the source of truth for the native CLI and the Go reference CLI. `scripts/i18n/catalog.py` validates completeness, placeholders, UTF-8, technical tokens and RTL metadata, and regenerates both lookup tables; `scripts/i18n/test_catalog.py` runs in the quality gate so a new CLI string without translations cannot merge unnoticed.
- No language syntax, Mochi format, Native AOT output, exit-code meaning or compiler architecture was changed.

## 2.1.1 — Unreleased

- The **Full** distribution is now a single self-contained native compiler with the complete Bunko standard library embedded. It works without an external `bunko/` directory, supports all `kiso/...` imports immediately, and can be copied alone to another directory or compatible computer.
- **Core** remains a smaller compiler without embedded Bunko. Existing Core behavior and explicit external Bunko support are unchanged.
- Bunko resolution order is now `--bunko-root`, `MIO_BUNKO`, embedded Full Bunko, then executable-relative `../bunko` for Core and legacy layouts. Invalid explicit roots still fail without fallback.
- The Mio-native resource generator includes every production `kiso/` source (excluding tests and historical dotted snapshots). Full and Core are built through separate native fixed-point chains; their binaries intentionally have different SHA-256 hashes.
- Release archives, Docker images and package-manager validation now expect embedded Full Bunko and no external library tree. Automated checks compile and execute programs importing `kiso/math`, `string`, `array`, `bytes`, `io`, `fs`, `encoding/json`, `regexp`, `crypto/sha256`, `os` and `net/tcp` from a relocated Full executable.
- No language syntax, Mochi format, Native AOT output or user-program packaging behavior was changed.

## 2.1.0 — Unreleased

- Windows AMD64 supports blocking IPv4 TCP through Winsock. Native CI requires the TCP package and two-process echo tests.
- Stable Core and Full builds require tagged source and signed evidence linking the distributed compiler hashes to the Trusted GF4 bootstrap and native fixed points. Package-manager generation verifies that evidence. The production Trusted GF4 run is still pending.
- See the [v2.1.0 release notes draft](docs/releases/v2.1.0.md) for verification status.

## 2.1.0-rc.1 — 2026-11-07

### Distribution and release status

- First 2.1 release candidate; not a stable release. macOS ARM64 is the primary target; Linux and Windows AMD64 remain experimental.
- Core and Full archives for all three targets, plus self-contained Windows installers. Full includes external Bunko sources; Core requires an explicit external Bunko root for library imports. Installed packages do not require Go or Python.
- Executable-relative Bunko discovery, explicit override precedence, relocated installs, integrated tools and package-manager lifecycle checks.
- RC builds stamp the exact version and source commit before native compilation and run the full candidate pipeline. Stable package-manager publication continues to reject pre-releases and requires genuine Trusted Seed PASS evidence.
- Known limits: Windows TCP is unavailable; direct floating-point `chant` is unsupported on Linux/Windows (use `convert.format_float64/32`); macOS builds are ad-hoc signed and not notarized. Concurrent Old-GC is not enabled by default.

### Runtime ABI and native toolchain

- Runtime ABI contract **RA-1** ([docs/runtime-abi.md](docs/runtime-abi.md), `bunko/kiso/runtime/abi`): one set of
  guarantees (exact roots, object lifetime, memory safety, every reference store path) for darwin-arm64, linux-amd64 and
  windows-amd64, with the two existing object layouts named as profiles (Object ABI v1 / generational on Darwin,
  x64-marksweep-v1 on Linux and Windows). No object layout was changed.
- `mio build --gc-stress` selects each target's collection stress mode.
- Contract tests (`tests/runtime-abi`) and the VM/native differential corpus (`tests/native-differential`) run natively
  on all three targets, in the default and `--gc-stress` configurations.
- Fixed in the shared native type analysis: a self-referential nullable struct lost its nullability through recursive
  type joins (nil tests were folded to "not nil"), and a global first seen as `mu` failed analysis at its first field
  read. Both affected every native backend.
- Fixed truncated (`nil reference`, `index out of range`) and over-long (`invalid call`) runtime trap messages on Linux
  and Windows.

- Behaviour change (darwin-arm64): native programs no longer print the value of the last top-level expression
  statement at exit; on every target a compiled program writes only its `chant` output. The REPL-style display
  remains a feature of the reference `mio run`.
- Darwin runtime: the OS memory-pressure level is re-queried at most once per 256 allocations and after every
  collection instead of on every allocation (one `sysctlbyname` per allocation made allocation-heavy programs spend
  about a third of their time in the kernel). Uses a previously unused metadata word (offset 2880).

### CI

- `.github/workflows/native.yml`, also run by Full: native compiler and CLI fixed points, native `mio test`, Runtime ABI
  contract and differential corpus on `ubuntu-24.04`, `macos-15` and `windows-2025`, with the runner OS/architecture,
  Stage A SHA-256 and stage hashes recorded. Stage A is go-hosted (Go reference VM running the pure-Mio Linux lowering);
  the trusted GF4 Native Seed is reported NOT_RUN.

### Removed

- The Go AOT backends and tools (`internal/aot` including the Go+C Linux runtime blob, `cmd/mochi-aot`,
  `cmd/mochi-aot-audit`, `examples/aot_m8..m10` generators), the Go M18/BCD scripts and archived Go release workflows,
  and the CI modules `go-aot-reference`, `legacy-bootstrap`, `go-aot-linux-amd64`, `go-linux-native` and
  `linux-runtime-reference`. Their native coverage moved to the pure-Mio native workflow. The Go reference frontend and
  VM remain as the go-hosted Stage A host and VM oracle.
