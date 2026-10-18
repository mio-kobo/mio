# Changelog

## 0.1.0 - 2026-10-18

First MVP of the VS Code Mio Language Support extension.

### Added

- TextMate grammar for the Mio V0.5 keyword set, built-in types, functions,
  generics, structs, enums, match arms, arrays, strings, runes, comments, and
  operators.
- `language-configuration.json` with auto-closing and surrounding pairs, bracket
  matching, comment tokens, indentation rules, folding markers, and a Mio word
  pattern.
- A syntax-aware folding range provider for braces and block comments.
- Snippets for common Mio declarations and collection operations.
- `Mio: Check Current File`, `Mio: Run Current File`,
  `Mio: Build Current File`, `Mio: Show Compiler Version`, and
  `Mio: Open Output` commands.
- Safe `child_process.spawn` execution with argument arrays and no shell.
- Mio CLI discovery through `mio.executablePath` and `PATH`, with guidance when
  the CLI is missing.
- Structured diagnostics from `mio check --format=json`, UTF-8 byte offset to
  UTF-16 position mapping, save debouncing, stale-process cancellation, and
  cleanup on close.
- Temporary-package checking for unsaved buffers.
- `mio.outputDirectory` support for `mio build`, including
  `${workspaceFolder}` expansion.
- English and Chinese READMEs, a 128×128 icon derived from the Mio language
  mark, syntax preview images, and Marketplace metadata.
- Unit and VS Code Extension Host tests, including a real CLI diagnostic test
  with spaces and Chinese/Japanese path segments.

### Not included

- Language Server, semantic completion, hover types, go-to-definition, find
  references, rename, symbols, signature help, or semantic tokens.
- Debugger, test explorer, formatter, or Marketplace publication.
