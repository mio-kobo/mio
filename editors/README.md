# Editor integrations

Ready-to-use configuration for editors other than VS Code and JetBrains. All of
them use the official language server (`mio lsp --stdio`), so hover,
definition, references, symbols, completion, signature help, document
highlight, rename and formatting come from the compiler and are identical
across editors. Nothing here re-implements Mio semantics.

| Editor | Files | LSP | Highlighting | Status |
| --- | --- | --- | --- | --- |
| Neovim 0.11+ | `neovim/mio.lua` | `vim.lsp.config` | Tree-sitter parser from `tools/tree-sitter-mio` (manual registration) | configuration only, not run in an editor here |
| Vim 9 | `vim/` | vim-lsp snippet in `vim/README.md` | regex syntax | configuration only, not run in an editor here |
| Helix | `helix/languages.toml` | yes | LSP only until a grammar rev is published | configuration only, not run in an editor here |
| Emacs 29+ | `emacs/mio-mode.el` | Eglot | font-lock | configuration only, not run in an editor here |
| Sublime Text | `sublime/` | LSP package | `.sublime-syntax` | configuration only, not run in an editor here |
| Zed | none | - | - | blocked: see below |

## Custom executable

Every configuration calls `mio` from `PATH`. To use another binary, set
`vim.g.mio_executable` (Neovim), `mio-executable` (Emacs) or write an absolute
path into `command` / `cmd` in the other files. The server only starts the
binary you configure; it does not run anything from the opened workspace.

## Verification

`python3 editors/verify.py [path/to/mio]` checks that the TOML/JSON-like files
parse, that keyword lists match the lexer keywords, and that a real `mio lsp`
process answers `initialize` with every capability these configurations
rely on. The editors themselves are not installed in the build environment, so
interactive behaviour is untested and should be confirmed by a user of each
editor.

## Zed (needs a maintainer decision)

A Zed extension must reference a Tree-sitter grammar by repository URL and
commit, and is installed from a published extension repository. That requires
either a new public repository or a published grammar, both outside this
change. Once `tools/tree-sitter-mio` is merged and a hosting decision is made,
the extension needs only `extension.toml`, `languages/mio/config.toml` and
`languages/mio/highlights.scm` (a copy of `tools/tree-sitter-mio/queries`), with
the language server command `mio lsp --stdio`.

## Syntax lists

Keywords follow `internal/token` (language version 0.5). If the lexer gains
keywords, update `vim/syntax/mio.vim`, `emacs/mio-mode.el`,
`sublime/Mio.sublime-syntax` and the tree-sitter grammar; `verify.py` fails
when they drift.
