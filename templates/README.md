# Project templates

Starter projects for Mio 2.1.x. Copy a directory and run the commands in its first comment. Every template is verified by `go test ./cmd/mio -run TestProjectTemplates` (reference CLI) and `python3 scripts/templates/verify.py --mio <mio>` (any CLI, including the native release; this is the only check for `cli`).

| Template | What it shows | Commands | Needs |
| --- | --- | --- | --- |
| `hello` | One-file program | `mio run main.mio` | Core |
| `cli` | Command-line arguments (`kiso/os`) | `mio run main.mio -- Mio Mochi` | Native CLI, Bunko |
| `multi-file` | A local package imported with `yobu "geometry"` | `mio run main.mio` | Core |
| `library` | An exported package with its own tests, and a consumer | `mio run main.mio`, `mio test mathx` | Core; tests need Bunko |
| `unit-test` | `*_test.mio` with `kiso/testing` | `mio test tests` | Bunko |
| `stdlib-app` | Several `kiso/*` packages | `mio run main.mio` | Bunko |

"Bunko" means the standard library: Full distributions embed it, Core and source-built CLIs need `--bunko-root` or `MIO_BUNKO`.

## Local packages

Mio 2.1.x has no package manager. A local package is a directory next to a `bunko/` directory: the `.gitkeep` file in `bunko/` keeps that marker in version control, and `yobu "<directory>"` imports the sibling directory. Tests for a package live in `*_test.mio` files inside it and call its functions unqualified. Bunko package management belongs to Mio 2.2.0 and is not used here.
