# Mio Language Support

<p align="center">
  <img src="icon.png" alt="Mio Language Support icon" width="96" height="96" />
</p>

<p align="center"><strong>Visual Studio Code support for the Mio programming language</strong></p>

<p align="center">
  <a href="https://github.com/ExMikuPro/Mio">Mio project</a> ·
  <a href="https://github.com/ExMikuPro/Mio/blob/main/docs/LANGUAGE.md">Language reference</a> ·
  <a href="https://github.com/ExMikuPro/Mio/releases">Releases</a>
</p>

Mio is a statically typed programming language with Japanese romaji keywords,
a Mochi bytecode toolchain, and the modular **Bunko** standard library. This
extension brings `.mio` files into VS Code with syntax highlighting, editor
conventions, snippets, compiler diagnostics, and Mio CLI commands.

The extension works without the Mio CLI for basic editing. CLI features are
enabled automatically when `mio` (or `mio.exe`) is available on `PATH`, or when
`mio.executablePath` points to a Mio executable.

## Syntax preview

Dark theme:

![Mio syntax highlighting on a dark theme](docs/images/syntax-preview-dark.png)

Light theme:

![Mio syntax highlighting on a light theme](docs/images/syntax-preview-light.png)

These preview images are generated from the extension's TextMate scope palette.
See [docs/images/README.md](docs/images/README.md) for how to replace them with
real editor screenshots.

## Features

- **TextMate syntax highlighting** for the current Mio language version (V0.5,
  as used by Mio `1.0.0-beta.1`):
  - keywords: `kaku`, `waza`, `moshi`, `sore`, `aida`, `kaesu`, `nukeru`,
    `tsuzuku`, `yobu`, `kata`, `sentaku`, `awase`, `baai`, `koukai`, `teisu`
  - `hai`, `iie`, and `mu`
  - built-in types: fixed-width integers, `float32`, `float64`, `rune`, `bool`,
    `string`, `unit`, `resource`, `Map`, and the numeric constraints
    `Number`, `Integer`, `Signed`, `Float`
  - function declarations and calls, closures, parameters, struct fields, enum
    variants, match arms, generic declarations and applications, imports,
    arrays, numbers, strings, runes, comments, and operators
  - keywords inside ordinary strings and comments are **not** highlighted
- **Language configuration**: auto-closing and surrounding pairs, bracket
  matching, `//` and `/* ... */` comments, indentation rules, region markers,
  a Mio-aware word pattern, and brace/comment folding.
- **Snippets** for variables, constants, functions, closures, generics,
  conditionals, `aida` loops, `nukeru`/`tsuzuku`, returns, imports, structs,
  enums, exhaustive `awase` matches, arrays, `Map`, nullable values, and runes.
- **Mio CLI commands**:
  - `Mio: Check Current File`
  - `Mio: Run Current File`
  - `Mio: Build Current File`
  - `Mio: Show Compiler Version`
  - `Mio: Open Output`
- **Compiler diagnostics** from `mio check --format=json` with UTF-8 byte offset
  to VS Code UTF-16 position mapping, save debouncing, stale-process
  cancellation, and cleanup on document close.
- **Safe process execution**: every CLI call uses an argument array with
  `shell: false`; paths containing spaces, Chinese, or Japanese are passed
  through unchanged.
- **Bilingual README** and a 128×128 Marketplace icon derived from the project's
  language logo.

## Installation

### From a VSIX (current MVP)

```bash
code --install-extension mio-language-support-0.1.0.vsix
```

Then reload VS Code and open any `.mio` file.

### From source

```bash
cd editors/vscode
npm ci
npm run compile
npx vsce package
code --install-extension mio-language-support-0.1.0.vsix
```

The extension is not published to the Visual Studio Marketplace yet. The
`publisher` field is a placeholder derived from the project's GitHub
organization; see [PUBLISHING.md](PUBLISHING.md) before publishing.

## Supported platforms

- macOS (Apple Silicon and Intel)
- Windows (x64 and ARM64)
- Linux (x64 and ARM64)

The extension itself is platform-neutral. The Mio CLI must match your platform.

## Mio CLI configuration

Build the CLI from the Mio repository:

```bash
go build -o mio ./cmd/mio
./mio version
```

Put `mio` on `PATH` (or `mio.exe` on Windows), or configure an absolute path.
The extension does not scan the workspace for a Mio binary; it only uses the
configured path or `PATH`.

| Setting | Default | Description |
| --- | --- | --- |
| `mio.executablePath` | `""` | Absolute path to the Mio CLI. Empty means search `PATH` for `mio` / `mio.exe`. `~` is expanded. |
| `mio.diagnostics.enabled` | `true` | Show diagnostics from `mio check --format=json`. |
| `mio.diagnostics.onSave` | `true` | Run `mio check` after saving a `.mio` file (350 ms debounce). |
| `mio.outputDirectory` | `""` | Output directory for `mio build`. Empty writes the `.mochi` file next to the source. Supports `${workspaceFolder}`. |

`mio.languageVersion` is intentionally **not** exposed: the current Mio CLI has
no language-version flag. The grammar and snippets target the V0.5 keyword set
used by the current toolchain. See the version table below.

### Bunko standard library

`mio check` and `mio run` need the Bunko root when a file imports `kiso/...`.
The extension reuses the CLI's lookup rules:

1. `MIO_BUNKO` from the environment, if it points to a directory.
2. The nearest `bunko/` directory above the source file.

If you keep Bunko outside the project, set `MIO_BUNKO` in the environment that
launches VS Code.

## Commands

| Command | Palette title | Requires Mio CLI |
| --- | --- | --- |
| `mio.checkCurrentFile` | Mio: Check Current File | Yes |
| `mio.runCurrentFile` | Mio: Run Current File | Yes |
| `mio.buildCurrentFile` | Mio: Build Current File | Yes |
| `mio.showCompilerVersion` | Mio: Show Compiler Version | Yes |
| `mio.openOutput` | Mio: Open Output | No |

Run and build save a dirty `.mio` buffer first. Manual `Check Current File`
checks the in-memory text without forcing a save by copying the package's
`.mio` files to a temporary directory, matching the JetBrains plugin's
behavior.

If the CLI is missing, the extension shows a guidance message with **Open
Settings** and **Installation Guide** actions. Syntax highlighting and snippets
continue to work.

## Mio language versions

Keyword gates are copied from `internal/token/token.go`; the current Mio CLI
loader uses `token.V05` (`internal/bunko/program.go`).

| Version | Added keywords / syntax |
| --- | --- |
| V0.1 | `kaku`, `waza`, `moshi`, `sore`, `kaesu`, `hai`, `iie`, `mu` |
| V0.2 | `aida`, `nukeru`, `tsuzuku` |
| V0.3 | `yobu` |
| V0.4 | `kata`, `sentaku`, `awase`, `baai` |
| V0.5 | `koukai`, `teisu` |

The extension targets V0.5. It does not treat a V0.4-only keyword as valid in a
V0.1 program because the plugin has no per-file version switch; such a switch
would require a real CLI flag.

## Current scope

- Syntax highlighting, language configuration, snippets, folding, and the five
  CLI commands described above.
- Diagnostics are parsed from the CLI's structured JSON output. The current CLI
  returns the first compiler diagnostic and may omit a source position for
  package/import failures. When no range is available, the diagnostic is shown
  at the start of the file with an explicit note rather than a guessed range.
- No Language Server, semantic completion, hover types, go-to-definition, find
  references, rename, document/workspace symbols, signature help, or semantic
  tokens in this MVP. The repository has no Mio LSP server, so the extension
  does not register fake semantic providers. See
  [docs/LSP_PLAN.md](docs/LSP_PLAN.md) for the planned design.
- No debugger, test explorer, formatter, or Marketplace publication.

## Known limitations

- The Mio CLI reports only the first diagnostic per check; the Problems panel
  therefore shows at most that diagnostic.
- Import/package errors without a source position are shown at the start of the
  current file and include the reported file path in the message.
- Windows `.cmd` / `.bat` launchers are not executed. Configure the native
  `mio.exe` instead; this keeps process execution shell-free.
- `mio.outputDirectory` only affects `mio build`; `mio run` always runs the
  source file.
- The Marketplace publisher ID is not finalized. See
  [PUBLISHING.md](PUBLISHING.md).

## Development

```bash
npm ci
npm run compile
npm run lint
npm test
npx vsce package
npm run check:vsix
```

`npm test` first runs Node-based unit tests (manifest, grammar scopes,
diagnostics mapping, CLI resolution, process runner), then launches a VS Code
Extension Host through `@vscode/test-electron` for activation, language ID,
command registration, snippets, folding, diagnostics, missing-CLI, and
space/Chinese/Japanese path tests.

The `pretest` step builds `cmd/mio` into `editors/vscode/.test-tmp/` when Go is
available. If Go is not installed, real-CLI integration tests are skipped with
an explicit marker; the extension itself has no Go dependency.

## License and artwork

The plugin code is MIT-licensed; see [LICENSE](LICENSE). The icon is a raster
derivative of the Mio language logo in
`editors/jetbrains/src/main/resources/icons/mio.svg`. The Mio/Mochi character
illustrations are **not** bundled because their license is separate from the
MIT-licensed code. See [NOTICE.md](NOTICE.md).

## Links

- [Mio repository](https://github.com/ExMikuPro/Mio)
- [Mio language reference](https://github.com/ExMikuPro/Mio/blob/main/docs/LANGUAGE.md)
- [Bunko standard library](https://github.com/ExMikuPro/Mio/blob/main/docs/BUNKO.md)
- [JetBrains Mio Language Support](https://github.com/ExMikuPro/Mio/tree/main/editors/jetbrains)
- [Issue tracker](https://github.com/ExMikuPro/Mio/issues)
