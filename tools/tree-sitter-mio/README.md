# tree-sitter-mio

Tree-sitter grammar for Mio (2.1.x syntax) for editors that use Tree-sitter
(Neovim, Helix, Zed and others). It is a syntax aid for highlighting, folding
and structural selection. It is not a type checker; `mio check` and `mio lsp`
remain the source of truth.

## Develop

```sh
npm ci
npx tree-sitter generate   # regenerates src/ (committed)
npx tree-sitter test       # corpus tests in test/corpus
./check.sh                 # compare accept/reject with the official parser
```

`check.sh` runs every `.mio` file in the repository through the official
parser (`validate/`) and through this grammar and fails on any disagreement.
The grammar is pinned to tree-sitter-cli 0.26.8.

## Known differences

- Index versus type arguments (`f[x]` / `f[T](...)`) is decided by the
  official parser with a naming heuristic. The grammar mirrors it with token
  priority. It accepts a few forms the official parser rejects, for example
  `a[N]`, `a[(1+2)]`, `a[int64(1)]` and `a[Foo(1)]`. Being more permissive is
  intentional for an editor grammar.
- Error recovery follows Tree-sitter defaults: a broken statement yields an
  `ERROR` node and later declarations still parse (see `test/corpus/errors.txt`).

## Queries

`queries/highlights.scm` provides highlight captures. Editor-specific
integration files are tracked separately.
