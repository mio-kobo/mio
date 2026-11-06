# Installing Mio 2.1

Package manager publication is **not enabled yet**. The commands below apply only
after maintainers publish a verified stable Release and approve the distribution
repository PRs. `2.1.0-pr.<sha>` archives are test candidates, not stable packages.

| Package | Contents |
| --- | --- |
| Full (`mio`, `MioKobo.Mio`) | Native compiler, supported Bunko sources and integrated CLI tools |
| Core (`mio-core`, `MioKobo.Mio.Core`) | The identical compiler, license and package records; no importable Bunko or standalone tools |

Neither installation needs a runtime download. Full supports offline development.
Core builds programs without standard library imports. No standalone development
tool is currently approved for either package.

## macOS ARM64 / Homebrew

```sh
brew tap mio-kobo/mio
brew install mio                 # or: brew install mio-core
mio version
brew upgrade mio                # Core: mio-core
brew uninstall mio
```

Full and Core conflict because both provide `mio`; uninstall one before switching.
Homebrew installs the complete tree under `libexec` and links its executable into
PATH. Bunko stays next to `bin`, so upgrades and relocation retain normal lookup.

## Windows AMD64 / WinGet

```powershell
winget install --id MioKobo.Mio --exact
mio version
winget upgrade --id MioKobo.Mio --exact
winget uninstall --id MioKobo.Mio --exact
# Core instead:
winget install --id MioKobo.Mio.Core --exact
mio-core version
```

ZIP Portable retains the full archive tree, including `bunko/kiso`. Full uses the
`mio` alias; Core uses `mio-core`. They have separate install directories and can
coexist without overwriting files. Use the same package ID for upgrades/removal.
The Windows runner must verify the complete installed inventory before manifests
are submitted. These manifests are prepared for human review; automation does not
submit to `microsoft/winget-pkgs`.

## Linux AMD64 / APT

After repository activation, download the dedicated key and verify its fingerprint
against the maintainer-published fingerprint through a separate trusted channel:

```sh
curl -fsSLo mio-archive-keyring.gpg https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg
gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg
printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] https://mio-kobo.github.io/mio-apt stable main' | sudo tee /etc/apt/sources.list.d/mio.list
sudo apt-get update
sudo apt-get install mio         # or mio-core
mio version
sudo apt-get update && sudo apt-get install --only-upgrade mio
sudo apt-get remove mio
```

Only Linux AMD64 is supported. Full/Core declare `Conflicts` and `Replaces`; review
APT's proposed removal when switching. Packages contain no build-host dependencies.
A symlink from `/usr/bin/mio` points into the package's private directory, preserving
executable-relative Bunko lookup. Do not use `apt-key` or disable signature checks.

## Offline use and platform limits

```sh
mio build hello.mio --target=linux-amd64 -o hello
./hello
mio check math.mio
```

Use `darwin-arm64` or `windows-amd64` on those systems; the historical default target
is Darwin, so specify your target. Windows can compile and directly execute native
images, but Mio `run` / `exec` / `test` process spawning and LSP stdin polling are
NOT_APPLICABLE. macOS/Linux support the validated integrated CLI tools.

Bunko lookup remains: `--bunko-root`, then `MIO_BUNKO`, then `../bunko` relative to
the real executable. Invalid explicit/environment roots are errors, including an
empty `MIO_BUNKO`. Remove that variable to use bundled Full sources. Copy the whole
installation tree when relocating; moving only the executable loses Full's library.

See [简体中文](README.zh-CN.md), [日本語](README.ja.md) and
[release operations](../../scripts/package_managers/README.md).
