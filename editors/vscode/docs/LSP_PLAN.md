# Mio Language Server plan

## Current status (0.3.0)

The Mio repository contains a built-in language server:

- Go implementation in `internal/lsp`, served through the `mio lsp` CLI
  subcommand over stdio JSON-RPC 2.0.
- Reuses `internal/lexer`, `internal/parser`, `internal/constant`,
  `internal/resolver`, `internal/typecheck`, and
  `internal/bunko.LoadProgramWithOverlay`.
- Supports `initialize`, `initialized`, `shutdown`, `exit`, `$/cancelRequest`,
  `workspace/didChangeConfiguration`, document open/change/save/close, and
  `textDocument/publishDiagnostics`.
- Implements `textDocument/hover`, `textDocument/definition`,
  `textDocument/references`, `textDocument/documentSymbol`,
  `textDocument/completion`, and `textDocument/signatureHelp` on top of one
  unified semantic index.
- Uses a 150 ms debounce, per-document generations, versioned diagnostics, and
  an in-memory snapshot so unsaved buffers are analyzed without temporary files.
- Converts Mio's UTF-8 byte offsets to LSP UTF-16 positions.

The VS Code extension connects to `mio lsp` with `vscode-languageclient@9`.
Every editor feature below is implemented by the server and reaches VS Code
through that client; the extension never re-implements the Mio TypeChecker.

### Semantic index

`internal/lsp/semantic.go` and `internal/lsp/semantic_walk.go` build a read-only
index for each analyzed document version:

- Symbols reuse `resolver.Resolver` bindings, so shadowing, parameters, local
  variables, closures, recursion, and mutually recursive functions keep their
  real identity. Struct fields and enum variants are keyed by their owning
  type's stable name because the Resolver does not model members.
- Types and signatures come from `typecheck.Checker`; struct and enum
  definitions are exposed through read-only query methods added for the LSP.
- Bunko keeps the original source range on every rewritten node, and
  `bunko.Program.StatementSources` maps each merged top-level statement to its
  real file. The LSP therefore reports source ranges and file URIs, never the
  rewritten `package::name` spelling.
- `analysis.go` caches one immutable snapshot per document version with
  single-flight construction, dependency mtime checks, reverse dependency
  invalidation, cancellation, and a bounded cache.

### Editor features

| LSP method | Source of truth |
| --- | --- |
| `textDocument/hover` | semantic index symbol + inferred type + doc comments |
| `textDocument/definition` | Resolver binding declaration site |
| `textDocument/references` | resolver symbol ID reverse index (+ open documents) |
| `textDocument/documentSymbol` | AST declarations with UTF-16 selection ranges |
| `textDocument/completion` | resolver scopes, resolved types, Bunko exports |
| `textDocument/signatureHelp` | parsed function/enum-constructor signatures |

Incomplete buffers are parsed with statement-level recovery
(`Parser.ParseProgramRecovering`) and resolved/type-checked in lenient mode, so a
broken statement does not discard the surrounding declarations. Diagnostics
still come from the strict compiler pipeline and therefore stay identical to
`mio check`.

### Deliberately not implemented

The server does **not** advertise rename, semantic tokens, formatting, code
actions, or workspace symbols. Advertising providers that return nothing would
make the editor claim features that do not exist.

## Next stage

1. **Workspace symbol indexing** — index every `.mio` file in the workspace, not
   only open documents and their imports, with an incremental dependency graph.
2. **Match-arm and type-annotation completion** — offer `baai` variants for the
   matched enum and type parameters in annotations.
3. **Signature help polish** — highlight the active parameter range inside the
   signature label and support overload-like enum constructors.
4. **Multiple diagnostics** — collect every compiler diagnostic instead of
   stopping at the first one (requires an error-collecting compiler pipeline).
5. **Cross-process performance** — persist the semantic index for large Bunko
   trees and measure hover latency on cold caches.

Each capability must keep the CLI fallback intact and must not regress
`mio check` behavior.
