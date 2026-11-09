[English](README.md) | [简体中文](docs/README.zh-CN.md) | [日本語](docs/README.ja.md)

---

<p align="center">
  <a href="assets/mascot/mio-and-mochi.png">
    <img src="assets/mascot/mio-and-mochi.webp" alt="Mio and Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>A small core, a wide world of mathematics and programs.</strong></p>
<p align="center"><em>Minimal Core · Modular Bunko · Pay for What You Use</em></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">Releases</a>
  · <a href="https://github.com/mio-kobo/mio/actions">Build status</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">Documentation</a>
</p>

**Mio** is a statically and strongly typed programming language with keywords written in romanized Japanese. It emphasizes explicit numeric semantics, a compact language core, and the **Bunko** standard library, which is imported as needed.

Mio 2.1 targets macOS ARM64, Linux AMD64 and Windows AMD64 for native ahead-of-time (AOT) compilation.

> [v2.0.0](https://github.com/mio-kobo/mio/releases/tag/v2.0.0) is the latest stable release and was verified on macOS ARM64. [v2.1.0-rc.1](https://github.com/mio-kobo/mio/releases/tag/v2.1.0-rc.1) is the latest published release candidate. The [`Mio-2.1.1` development branch](https://github.com/mio-kobo/mio/tree/Mio-2.1.1) is preparing v2.1.1, which is still unreleased.

See the [v2.1.0 release notes draft](docs/releases/v2.1.0.md) for changes and release verification status.

## Language and toolchain

- **Static type system:** Fixed-width integers, floating-point numbers, generics, nullable types, structs, enums, and closures.
- **Mochi** bytecode runs in the reference VM.
- **Native AOT:** Mach-O on macOS ARM64, static ELF on Linux AMD64 and PE32+ on Windows AMD64.
- **Precise GC:** Shadow Frame root tracking, a dedicated managed arena, and cross-page object allocation.
- **Bunko standard library:** Import `kiso/` modules as needed with `yobu`.

Mio and Mochi are also the project's mascots: Mio is the girl pictured above, and Mochi is the little companion in her arms. The name Mochi is also used for the bytecode execution system.

## Getting started

### Getting Mio

Check [GitHub Releases](https://github.com/mio-kobo/mio/releases) for downloads. For Mio 2.1.1, **Full** is a single self-contained `mio` executable (`mio.exe` on Windows) with the complete Bunko standard library embedded. **Core** ships a smaller compiler without embedded Bunko. Both provide integrated development commands such as `test` and `lsp`. Full needs no external Bunko tree; Core can use a separately installed Bunko tree through `--bunko-root` or `MIO_BUNKO`.

Precompiled distributions run without Go, Python or an external development toolchain. macOS release candidates use ad-hoc signing and are not notarized, so Gatekeeper may restrict them. See [Testing](docs/testing.md) for source bootstrap and native verification.

## Platforms and roadmap

The table describes the development branch. See the [three-platform native CI results](https://github.com/mio-kobo/mio/actions/runs/42372415752) for the checks that passed.

| Platform or feature | Current status |
| --- | --- |
| macOS ARM64 native AOT | Primary target. Passed M18 and release-candidate verification. `Mio-2.1` native CI on `macos-15`: CLI native fixed point, `mio run` and process protocol, `mio test` of every package, Runtime ABI contract and differential corpus. |
| Linux x86-64 native (Mio, static ELF) | Experimental: compiler/CLI native fixed point, `mio run` and process protocol, `mio test` of every package, Runtime ABI contract and differential corpus pass in CI. `chant` of a float value is not yet supported (compile-time error; `convert.format_float64/32` gives the same text on every platform). |
| Windows x86-64 native (Mio, PE32+) | Experimental. The CLI native fixed point, `mio run` and process protocol (CreateProcessW), `mio test` of every package, Runtime ABI contract and differential corpus pass in CI. Blocking IPv4 `kiso/net/tcp` uses Winsock, with native package tests and two-process echo required in CI. The published RC1 lacks Windows TCP support. Direct float `chant` is unsupported. |
| Web / browser playground | Runs Mochi bytecode in the WebAssembly reference VM. [Web CI](https://github.com/mio-kobo/mio/actions/runs/42372415737) checks native CLI bytecode compatibility and tests the playground in Chromium. |
| Bunko standard library | Every public library function has a native test (bunko-coverage check in PR CI); per-function, per-platform results: [Bunko test matrix](tests/bunko-matrix/bunko-test-matrix.md). |
| Linux ARM64 | Planned. |
| Generational GC | Implemented for darwin-arm64 (Object ABI v1); x86-64 uses non-generational precise mark-and-sweep. Both satisfy [Runtime ABI contract RA-1](docs/runtime-abi.md). |
| High / Balanced / Low GC modes and concurrent GC | Planned. |

The [frozen Darwin ARM64 GF4 seed](https://github.com/mio-kobo/mio/releases/tag/gf4-seed-v1-immutable) is published as an immutable GitHub Release. The production Trusted GF4 bootstrap for v2.1.0 has no confirmed GitHub Actions PASS.

The [official Mio documentation site](https://mio.kaelexvp.cc) is the home for the language tutorial, Bunko API, CLI reference, and runtime documentation.

## Contributing, security, and license

See [Contributing](CONTRIBUTING.md) for the contribution workflow and [Testing](docs/testing.md) for the native-first test process. Please use [Security Policy](SECURITY.md) for private security reports and [Code of Conduct](CODE_OF_CONDUCT.md) for community expectations.

Mio source code is licensed under the [MIT License](LICENSE). The Mio and Mochi illustrations in [`assets/mascot/`](assets/mascot/) **are not covered by the code's MIT License**; their copyright and usage terms are separate.
