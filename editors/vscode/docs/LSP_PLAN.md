# Mio Language Server plan

## Current status

The Mio repository does **not** contain a Language Server Protocol server. The
VS Code extension therefore ships only:

- TextMate grammar and language configuration.
- Snippets and folding.
- CLI-backed diagnostics from `mio check --format=json`.

No completion, hover, definition, references, rename, symbols, signature help,
or semantic-token provider is registered. Registering regex-based approximations
would conflict with Mio's real Resolver and TypeChecker and is intentionally
avoided.

The client boundary is reserved in `src/lsp/client.ts` through
`MioLanguageClientHost`. `UnavailableMioLanguageClient` is a no-op placeholder.

## Why a real Mio server

Mio already has the required semantic pipeline:

- `internal/lexer` and `internal/parser` for syntax and source spans.
- `internal/resolver` for name binding and capture relationships.
- `internal/typecheck` for types, generics, nullability narrowing, and
  diagnostics.
- `internal/bunko` for package loading and `MIO_BUNKO` lookup.
- `internal/diagnostic` for structured error codes and UTF-8 byte spans.

A server should call these components directly rather than re-parse source with
regular expressions. This keeps completion, hover, and rename consistent with the
compiler.

## Proposed architecture

```text
VS Code extension
  └── vscode-languageclient
        └── JSON-RPC over stdio
              └── mio lsp (new CLI subcommand)
                    ├── bunko loader
                    ├── resolver
                    ├── typechecker
                    └── document store with incremental sync
```

### Transport and lifecycle

- Add `mio lsp` to `cmd/mio` (or a separate `cmd/mio-lsp` binary) and communicate
  over stdio.
- Advertise `textDocumentSync: Incremental`; keep an in-memory document store.
- Use `workspace/didChangeWatchedFiles` to invalidate package caches.
- Respect `MIO_BUNKO` and a future `mio.bunkoPath` setting.
- Support cancellation through `$/cancelRequest` and compile with the existing
  `context` cancellation support.

### Position mapping

Mio diagnostics use UTF-8 byte offsets and 1-based display columns. LSP uses
UTF-16 code units. The server should convert byte offsets to UTF-16 positions
using the document text, matching the extension's existing mapping logic. Never
guess a range when the compiler has no span.

### Capabilities, in delivery order

1. **Diagnostics** — publish all compiler diagnostics, not just the first one.
2. **Document symbols** — functions, structs, enums, constants, imports.
3. **Completion** — real symbols from the resolver, function parameters, enum
   variants, struct fields, and Bunko public members.
4. **Hover** — inferred types from the typechecker and signatures from the
   resolver.
5. **Go to definition** and **find references** — resolver bindings, excluding
   comments, strings, and shadowed names.
6. **Signature help** — function and generic signatures.
7. **Rename** — resolver-verified symbol references only.
8. **Workspace symbols** — package-aware symbol index.
9. **Semantic tokens** — optional; TextMate remains the fast baseline.

## Extension integration

When a server exists:

1. Add `vscode-languageclient` as a runtime dependency.
2. Replace `UnavailableMioLanguageClient` with a `LanguageClient` that starts the
   configured Mio executable with `lsp`.
3. Keep the CLI diagnostic manager as a fallback only when the server is absent.
4. Reuse the existing `mio.executablePath` resolution, `MIO_BUNKO` lookup, and
   output channel.
5. Add configuration such as `mio.languageServer.enabled` only when the server is
   real and tested.
6. Add tests that exercise the server through the client, not just the CLI.

## Out of scope for this MVP

- Implementing the server.
- Adding `vscode-languageclient`.
- Advertising semantic features without a server.
- Debugger, formatter, or test explorer integration.
