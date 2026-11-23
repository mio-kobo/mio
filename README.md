[English](README.md) | [简体中文](docs/README.zh-CN.md) | [日本語](docs/README.ja.md)

---

<p align="center">
  <a href="assets/mascot/mio-and-mochi.png">
    <img src="assets/mascot/mio-and-mochi.webp" alt="Mio and Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>A small core, a wide world of mathematics and programs.</strong></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">Releases</a>
  · <a href="https://github.com/mio-kobo/mio/actions">Build status</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">Documentation</a>
</p>

**Mio** is a statically typed language with romanized Japanese keywords. It focuses on explicit numeric semantics, a small core, and the **Bunko** standard library—imported only as needed.

> **`v2.2.0` (in development):** Succeeding `v2.1.*`, Mio 2.2 expands native AOT compilation to six platforms and adds native concurrency and event-loop primitives.

```mio
yobu "kiso/math";

waza square(value: float64) -> float64 {
    kaesu value * value;
}

kaku area = square(12.0);
chant(area);
chant(math.sqrt(area));
```

## Features

- **Type system:** Fixed-width integers, floats, generics, nullable types, structs, enums, and closures.
- **Native AOT (6 targets):** Mach-O (`darwin-arm64`, `darwin-amd64`), static ELF (`linux-amd64`, `linux-arm64`), and PE32+ (`windows-amd64`, `windows-arm64`).
- **Mochi VM & WebAssembly:** Compact bytecode for the reference VM and the browser playground.
- **Precise GC:** Shadow Frame root tracking, a dedicated managed arena, and cross-page allocation (generational GC on `darwin-arm64`; precise mark-and-sweep elsewhere), following [Runtime ABI contract RA-1](docs/runtime-abi.md).
- **Modular Bunko library:** Import `kiso/` modules on demand via `yobu` without pulling in unused runtime features.
- **Localized CLI:** 39 locales with BCP 47 fallback, adaptive help themes (`--lang`, `--theme`, `NO_COLOR`), and stable machine-readable output. See [docs/i18n.md](docs/i18n.md).

## Getting started

### Full vs. Core

Both distributions ship the `run`, `build`, `test`, and `lsp` commands:

- **Full (`mio`):** Self-contained binary (`mio.exe` on Windows) with the Bunko standard library embedded.
- **Core (`mio-core`):** Smaller compiler binary without embedded Bunko. Point it to an external Bunko tree via `--bunko-root` or `MIO_BUNKO`.

### Installation

#### macOS / Linux (Homebrew)

```bash
# Full edition
brew install mio-kobo/mio/mio

# Core edition
brew install mio-kobo/mio/mio-core
```

#### Debian / Ubuntu (APT)

1. Download the signing key and verify its fingerprint:

```bash
curl -fsSLo mio-archive-keyring.gpg \
  [https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg](https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg)

gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
```

2. Add the repository and install:

```bash
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg

printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] [https://mio-kobo.github.io/mio-apt](https://mio-kobo.github.io/mio-apt) stable main' \
  | sudo tee /etc/apt/sources.list.d/mio.list

sudo apt update
sudo apt install mio       # or `mio-core` for the Core edition
mio version
```

To upgrade later:

```bash
sudo apt update && sudo apt install --only-upgrade mio
```

#### Windows (Chocolatey)

```powershell
# Full edition
choco install mio

# Core edition
choco install mio-core
```

#### Prebuilt binaries & source

Standalone archives for all six targets are on [GitHub Releases](https://github.com/mio-kobo/mio/releases). They require no Go, Python, or external toolchain.

> **macOS note:** Pre-release builds on GitHub Releases are ad-hoc signed and not notarized. If Gatekeeper blocks the binary, allow it in System Settings. For building from source, see [Testing](docs/testing.md).

### Quick run

```bash
mio run hello.mio
mio test
```

### Formatting and imports

The native `mio fmt` command uses four spaces per indentation level and
preserves literal spelling and comments. It refuses malformed source.
By default it also organizes `yobu` declarations using local package
exports, adding unique matches and retaining imports whose use or side
effects cannot be ruled out.

```sh
mio fmt file.mio
mio fmt .
mio fmt --check .
mio fmt --diff .
mio fmt --stdin --stdin-filepath path/to/file.mio
```

`--check` returns 1 when changes are needed and 2 when formatting fails.
`--diff` prints a unified diff without writing files. Use
`--no-organize-imports` to format whitespace only and `--bunko-root`
to select an installed library tree. In VS Code the Mio language
server supplies import completion and the standard Organize Imports action.

## Platform support

Mio 2.2 validates compiler builds, self-hosting fixed points, Runtime ABI, GC, blocking/nonblocking TCP, and Bunko across all six native targets in CI.

| Target | Core AOT, GC & Bunko | Blocking & Nonblocking TCP | Reactor / Event Loop | Native Threads & Channels |
| :--- | :---: | :---: | :---: | :---: |
| `linux-amd64` | ✅ Validated | ✅ Validated | ✅ Validated | ✅ Validated |
| `darwin-arm64` | ✅ Validated | ✅ Validated | ✅ Validated | ✅ Validated |
| `linux-arm64` | ✅ Validated | ✅ Validated | ✅ Validated | 🚧 Planned |
| `darwin-amd64` | ✅ Validated | ✅ Validated | ✅ Validated | 🚧 Planned |
| `windows-amd64` | ✅ Validated | ✅ Validated | 🚧 Planned | 🚧 Planned |
| `windows-arm64` | ✅ Validated | ✅ Validated | 🚧 Planned | 🚧 Planned |

*Unsupported concurrency or reactor features fail closed at compile time or runtime rather than falling back to a single thread. See the [Six-Platform Parity Matrix](docs/parity/README.md) and [Bunko Test Matrix](tests/bunko-matrix/bunko-test-matrix.md) for the full 15-feature CI matrix.*

### Roadmap

- **Web playground:** Mochi bytecode runs in the WebAssembly reference VM and is checked in CI for compatibility with the native CLI.
- **Concurrency & Reactor parity:** Extending native threads, synchronization, and channels to `linux-arm64`, `darwin-amd64`, and Windows, plus completing the Windows Reactor backend.
- **GC improvements:** High / Balanced / Low GC modes and concurrent GC.

## Documentation & Mascots

Language tutorials, the Bunko API reference, the CLI manual, and runtime specs live on the [Mio documentation site](https://mio.kaelexvp.cc).

**Mio** (the girl at the top) and **Mochi** (in her arms, sharing a name with the bytecode VM) are the project's mascots.

## Contributing, security & license

- Asynchronous functions (`hidouki` / `matsu`, Linux AMD64 runtime): [docs/async-await.md](docs/async-await.md)
- Workflow and native-first testing: [Contributing](CONTRIBUTING.md), [Testing](docs/testing.md)
- Vulnerability reports: [Security Policy](SECURITY.md)
- Community guidelines: [Code of Conduct](CODE_OF_CONDUCT.md)

Mio's source code is under the [MIT License](LICENSE). The Mio and Mochi illustrations in [`assets/mascot/`](assets/mascot/) are **not covered by the MIT License** and have separate copyright and usage terms.
