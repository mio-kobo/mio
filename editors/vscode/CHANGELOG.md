# Changelog

## 0.3.0 - 2026-10-18

MioCore semantic navigation.

### Added

- A unified semantic index built from the real Mio Resolver bindings and
  TypeChecker results: symbol identity, declaration and reference locations,
  lexical scopes, inferred types, function signatures, generic parameters,
  struct fields, enum variants, Bunko module imports, and line-comment
  documentation. Source ranges always stay in the original file, even when
  Bunko rewrites names to `package::name`.
- **Hover** (`textDocument/hover`) with Markdown signatures, inferred types,
  declarations, documentation, and UTF-16 ranges. Unresolved symbols return
  `null` instead of a guessed type.
- **Go to definition** (`textDocument/definition`) for variables, parameters,
  functions, struct fields, enum variants, generic symbols, and imported Bunko
  symbols across files and modules.
- **Find references** (`textDocument/references`) keyed by real symbol identity,
  with `includeDeclaration`, shadowing safety, comment/string exclusion, closure
  captures, and cross-module results.
- **Document symbols** (`textDocument/documentSymbol`) as a real hierarchy with
  functions, variables, constants, structs, fields, enums, variants, and
  selection ranges that point at the symbol name.
- **Completion** (`textDocument/completion`) from resolver scopes and resolved
  types: keywords, locals, parameters, functions, types, constants, struct
  fields, enum variants, struct-literal fields, and exported Bunko symbols.
- **Signature help** (`textDocument/signatureHelp`) with real parameter names,
  types, return types, nested calls, generics, and incomplete call expressions.
- Per-document semantic snapshots with version association, single-flight
  analysis, dependency mtime invalidation, cancellation, and bounded caches.
- Parser, Resolver, and TypeChecker tolerance for incomplete buffers: a broken
  statement no longer discards the surrounding declarations.
- Go protocol tests for every new method (Unicode/emoji/CRLF, cross-file,
  generics, closures, version changes, cancellation, concurrency, incomplete
  sources) and Extension Host tests for the real `mio lsp` process.

### Changed

- The language server now advertises exactly the implemented capabilities:
  hover, definition, references, document symbols, completion, and signature
  help. Rename, semantic tokens, and formatting remain unimplemented and are
  not advertised.
- Diagnostics and semantic navigation share one versioned analysis snapshot, so
  hover/definition never observe a different document version than the
  published diagnostics.

## 0.2.0 - 2026-10-18

MioCore language server integration.

### Added

- `vscode-languageclient@9` connection to the Mio CLI `lsp` subcommand.
- Live compiler diagnostics for open buffers, including unsaved text, with
  document versioning, incremental edits, cancellation, and UTF-16 positions.
- `mio.languageServer.enabled` and diagnostics ownership coordination: exactly
  one diagnostic source (language server or CLI fallback) is active at a time.
- Automatic fallback to `mio check --format=json` when the server is disabled,
  unavailable, or stops.
- Language server launch-option unit tests and Extension Host tests for live
  diagnostics and fallback.

### Changed

- Renamed the Marketplace display name to **Mio — Programming Language Tools**;
  the extension ID `mio-language-support` is unchanged.
- `mio.diagnostics.enabled` and `mio.diagnostics.onSave` now describe the CLI
  fallback; the language server owns diagnostics while it runs.
- Manual `Mio: Check Current File` reports that diagnostics come from the
  language server instead of starting a duplicate CLI check.

## 0.1.0 - 2026-10-18

First MVP of the VS Code extension.

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
