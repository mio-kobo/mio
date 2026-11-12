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

> **Status (`v2.2.0` in development):** Mio 2.2 is currently under active development and will succeed `v2.1.*` as the next stable release, expanding native ahead-of-time (AOT) compilation to **six platforms** alongside native concurrency and event-loop primitives.

```mio
yobu "kiso/math";

waza square(value: float64) -> float64 {
    kaesu value * value;
}

kaku area = square(12.0);
chant(area);
chant(math.sqrt(area));
```

## Language and toolchain

- **Static type system:** Fixed-width integers, floating-point numbers, generics, nullable types, structs, enums, and closures.
- **Native AOT across six platforms:** Mach-O (`darwin-arm64`, `darwin-amd64`), static ELF (`linux-amd64`, `linux-arm64`), and PE32+ (`windows-amd64`, `windows-arm64`).
- **Mochi VM & WebAssembly:** Compact **Mochi** bytecode runs in the reference VM and in the browser playground.
- **Precise GC:** Shadow Frame root tracking, a dedicated managed arena, and cross-page object allocation (generational GC on `darwin-arm64`; precise mark-and-sweep on other targets), conforming to [Runtime ABI contract RA-1](docs/runtime-abi.md).
- **Modular Bunko standard library:** Import `kiso/` modules on demand with `yobu` without paying for unused runtime features.
- **Localized CLI:** Supports 39 locales with BCP 47 fallback rules, adaptive help themes (`--lang`, `--theme`, `NO_COLOR`), and stable machine-readable output. See [docs/i18n.md](docs/i18n.md).

## Getting started

### Distributions: Full vs. Core

Mio ships in two variants, both containing integrated developer commands (`run`, `build`, `test`, `lsp`):

- **Full (`mio`):** A single self-contained executable (`mio.exe` on Windows) with the complete Bunko standard library embedded.
- **Core (`mio-core`):** A smaller compiler binary without embedded Bunko, using an external Bunko tree via `--bunko-root` or `MIO_BUNKO`.

### Installation

#### macOS / Linux (Homebrew)

```bash
# Install Full edition
brew install mio-kobo/mio/mio

# Or install Core edition
brew install mio-kobo/mio/mio-core
```

#### Debian / Ubuntu (APT)

1. Download the archive signing key and verify its fingerprint via an independent trusted channel:

```bash
curl -fsSLo mio-archive-keyring.gpg \
  https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg

gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
```

2. Add the repository and install Mio:

```bash
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg

printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] https://mio-kobo.github.io/mio-apt stable main' \
  | sudo tee /etc/apt/sources.list.d/mio.list

sudo apt update
sudo apt install mio       # Full edition (or `sudo apt install mio-core` for Core)
mio version
```

To upgrade later:

```bash
sudo apt update && sudo apt install --only-upgrade mio
```

#### Windows (Chocolatey)

```powershell
# Install Full edition
choco install mio

# Or install Core edition
choco install mio-core
```

#### Prebuilt Binaries & Source

Standalone archives for all six targets are also available on [GitHub Releases](https://github.com/mio-kobo/mio/releases) and run without Go, Python, or an external toolchain.

> **macOS binary note:** Standalone pre-release builds from GitHub Releases use ad-hoc signing and are not notarized; you may need to allow the binary in System Settings if Gatekeeper restricts it. For building from source, see [Testing](docs/testing.md).

### Quick run

```bash
mio run hello.mio
mio test
```

## Platform support and roadmap

Mio 2.2 validates native compiler builds, self-hosting fixed point, Runtime ABI, GC correctness, blocking & nonblocking TCP, and the Bunko standard library across all six native targets in CI.

| Target | Core AOT, GC & Bunko | Blocking & Nonblocking TCP | Reactor / Event Loop | Native Threads & Channels |
| :--- | :---: | :---: | :---: | :---: |
| `linux-amd64` | ✅ Validated | ✅ Validated | ✅ Validated | ✅ Validated |
| `darwin-arm64` | ✅ Validated | ✅ Validated | ✅ Validated | ✅ Validated |
| `linux-arm64` | ✅ Validated | ✅ Validated | ✅ Validated | 🚧 Planned |
| `darwin-amd64` | ✅ Validated | ✅ Validated | ✅ Validated | 🚧 Planned |
| `windows-amd64` | ✅ Validated | ✅ Validated | 🚧 Planned | 🚧 Planned |
| `windows-arm64` | ✅ Validated | ✅ Validated | 🚧 Planned | 🚧 Planned |

*Unsupported concurrency or reactor features fail closed at compile time or runtime without silent single-thread fallbacks. For the full 15-feature CI parity matrix and backlog, see the [Six-Platform Parity Matrix](docs/parity/README.md) and [Bunko Test Matrix](tests/bunko-matrix/bunko-test-matrix.md).*

### Additional targets & upcoming work

- **Web / browser playground:** Runs Mochi bytecode in the WebAssembly reference VM, verified in CI for bytecode compatibility with the native CLI.
- **Concurrency & Reactor parity:** Extending native multithreading, synchronization, and channels to `linux-arm64`, `darwin-amd64`, and Windows, and completing the Windows Reactor backend.
- **GC enhancements:** High / Balanced / Low GC modes and concurrent GC.

## Documentation & Mascots

The [official Mio documentation site](https://mio.kaelexvp.cc) hosts the language tutorial, Bunko API reference, CLI manual, and runtime specifications.

**Mio and Mochi** are also the project's mascots: **Mio** is the girl pictured at the top, and **Mochi** is the little companion in her arms—sharing a name with Mio's bytecode execution system.

## Contributing, security, and license

- See [Contributing](CONTRIBUTING.md) for the contribution workflow and [Testing](docs/testing.md) for the native-first test process.
- Please use [Security Policy](SECURITY.md) for private security reports and [Code of Conduct](CODE_OF_CONDUCT.md) for community expectations.

Mio source code is licensed under the [MIT License](LICENSE). The Mio and Mochi illustrations in [`assets/mascot/`](assets/mascot/) **are not covered by the code's MIT License**; their copyright and usage terms are separate.
