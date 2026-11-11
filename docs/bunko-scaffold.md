# Bunko package scaffolding

`mio bunko create` creates a local development package. Native Mio implements the
local wizard and templates without Go, Python, Git, gh or a shell at runtime.
The Go reference CLI additionally implements optional Git/GitHub integration.
Native Git/GitHub operations explicitly fail with
`BUNKO_E_SCAFFOLD_NATIVE_PROCESS`; they are not installation or publishing support.

## Local creation

```sh
mio bunko create
mio bunko create --help
mio --lang zh-CN bunko create
mio bunko create --non-interactive --namespace alice --name json \
  --description 'JSON utilities' --version 0.1.0 --license MIT \
  --platform linux/x86_64 --platform macos/arm64
```

Names use two lowercase ASCII segments, each 1–39 characters with single internal
hyphens. Versions are exactly `major.minor.patch`, each component 1–9 digits,
without leading zeroes except `0`. Version defaults to `0.1.0`; license defaults
to MIT, with Apache-2.0 also available. Descriptions are one nonblank UTF-8 line
of at most 240 characters, without control or bidirectional control characters.

Publishing platforms have **no default**. Explicit `os/arch` pairs must come from
the bundled official index snapshot
`e6c89a1890a782ff759c698ea527df85bbf2e9c0`:

| Target | Optional workflow runner |
| --- | --- |
| linux/x86_64 | ubuntu-24.04 |
| macos/arm64 | macos-15 |
| windows/x86_64 | windows-2025 |

These are publishing-policy selections, independent of the compiler's six native
targets. The index policy is not modified. Selecting the current host does not
implicitly select any other platform. Update the bundled policy only after
reviewing an upstream policy change.

The destination is a **new direct child** of the current directory, defaulting
to the package name. `--directory` accepts a portable lowercase name, never an
absolute path, separator, `..`, or Windows device name. Existing files,
directories and symlinks are refused. There is no overwrite mode. Creation uses
an exclusive directory and removes known generated files after a write failure;
it does not recursively delete unrelated files. The parent is canonicalized.
Concurrent hostile modifications by another process running as the same user
are outside this local project creation model.

Generated files:

| File | Purpose |
| --- | --- |
| package.mio | Public `hello()` example |
| package_test.mio | Executable `test_hello()` unit test |
| bunko.toml | Exact local dependency declarations only |
| bunko.package.toml | Local package metadata and selected publishing platforms |
| README.md | Development commands and archive exclusions |
| LICENSE | Full selected license text |
| .gitignore | Compiler/cache output exclusions |
| .github/workflows/mio-test.yml | Optional dependency-free test workflow |

Run `mio check .` and `mio test .` inside the generated directory. No dependencies
are downloaded or executed during creation. Add exact dependencies with repeated
`--dependency alice/other@1.0.0` or comma-separated values. Conflicting versions,
more than 256 dependencies, and `kiso/` overrides are rejected.

The existing `bunko.toml` dependency-only schema is unchanged. The separate local
metadata file is not an index record or a new archive format. A Bunko V1 upload
must place eligible `.mio`, README and license files directly at the archive root.
Exclude both TOML files, `.gitignore`, `.git/` and `.github/`; do not upload this
development directory wholesale. Publishing is a future, separate operation.

`--workflow` generates a read-only, explicitly selected target matrix using
the index-pinned Mio **2.1.0** through the existing official setup action pinned
at `94316df688eaf3072d4462f161cf538af64957a1`. It is not evidence of a 2.2.0
release. Workflow plus external dependencies is currently rejected because secure
native installation is unavailable. The repository's existing version banner
`2.1.2-dev` and `VERSION` remain unchanged.

## Options and confirmations

| Option | Meaning |
| --- | --- |
| --non-interactive | Read values and permissions from flags; never ask |
| --namespace, --name, --description | Required package fields |
| --version, --license | Initial exact version and MIT or Apache-2.0 |
| --platform | Explicit approved pair; repeat or comma-separate |
| --dependency | Exact `namespace/package@version`; repeat or comma-separate |
| --directory | New direct child of the current directory |
| --workflow | Generate the optional workflow |
| --git-init | Request local Git initialization |
| --github | Request optional GitHub integration |
| --repo | GitHub `owner/repository` to connect |
| --create-repo | Request a new empty repository |
| --visibility | `public` or `private`, required for new repositories |
| --allow-git-init | Non-interactive consent to initialize Git |
| --allow-git-config | Separate consent to add origin and record the repository |
| --allow-connect | Separate consent to connect the verified repository |
| --allow-repo-create | Separate consent to create a remote repository |
| --allow-auth-login | Separate consent to official gh browser authentication |
| --allow-account | Exact active login confirmation when multiple accounts exist |

Interactive final creation and every external/configuration permission default to
No. Declining keeps the local package. Consent to repository creation does not
authorize connection, Git initialization, configuration or any push. No scaffold
command creates commits, pushes, publishes releases, or submits index PRs.

## Optional reference GitHub connection

Run inside a generated package, using the **Go reference** CLI:

```sh
mio bunko connect --repo alice/json
mio bunko connect --repo alice/json --create-repo --visibility private
```

The default local operation discovers OS/CPU/compiler and local Git/gh versions,
but does not query authentication or make network requests. Native discovery
currently reports executable paths only, with an explicit version/process
capability diagnostic. Missing gh points to <https://cli.github.com/>; nothing
is installed automatically. If explicitly requested tools are missing or an
integration step fails, creation retains the local package and reports failure.

The reference runner uses executable discovery and structured argument arrays,
bounded captured output and timeouts (5 seconds for versions, 15 for auth,
20 for API, 10 for Git writes, 180 for explicitly authorized login). It never
constructs shell commands. Git subprocesses discard inherited `GIT_*` overrides
so writes remain scoped to the package.

Authentication uses `gh auth status --no-refresh --json hosts` with an in-gh jq
filter retaining login, active and state, then `gh api user` for verified login
and numeric ID. Raw tokens and scopes are never requested, read, printed or
stored by scaffolding. Older gh without `--no-refresh` fails closed. Fatal query
errors do not initiate login. When no active authenticated account exists, only
explicit permission can start `gh auth login --web`; there is no refresh or scope
expansion command. Multiple accounts show the active account and allow cancel.

Community namespaces must match the repository owner. `bunko` is reserved;
`mio`/`kiso` require the official `mio-kobo` owner and index-approved submitter
ID 72144156. Local-only creation never claims namespace ownership. New org
repositories require verified active org admin membership; ordinary membership
does not establish creation rights. Existing repositories require verified write
permission. The active identity is rechecked before remote creation/configuration.

New repositories are created through `gh api` with JSON input and `auto_init:false`.
Each operation displays account, repository, visibility and action. Existing
origin remotes and Git worktree `.git` files are conservatively refused; remote
URLs are never read because they may contain credentials. Connection writes a
credential-free HTTPS origin and local repository metadata only. A later failure
may leave an already approved empty remote repository or local Git change;
the error preserves source files and never attempts automatic remote deletion.

English, Japanese and Simplified Chinese use the existing CLI catalogs/themes.
New keys in the other 36 locales currently use English pending translation.

See [handoff and verified evidence](bunko-scaffold-handoff.md) for native blockers,
test commands and the next authorized development phase.
