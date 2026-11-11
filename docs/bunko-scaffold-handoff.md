# Bunko scaffolding handoff

## Scope and branch dependency

Repository: <https://github.com/mio-kobo/mio>. Destination development branch:
`Mio-2.2.0`. Scaffolding is isolated on `EXP/bunko-scaffold`, based on the open
draft [PR #77](https://github.com/mio-kobo/mio/pull/77) head
`8a32159b3d33579a083865df9f7de6bced7297a1`. The focused scaffold PR targets
`feat/v2.2-bunko-package-manager` while #77 remains open. Retarget to `Mio-2.2.0`
after #77 merges, preserving only scaffold commits. Do not modify another active
branch, merge either PR, create release tags, or publish a release.

Scaffold PR and CI links will be recorded below after remote verification.

## Implemented functionality and files

- `internal/bunkoscaffold/{spec,wizard,process,github}.go`: reference local wizard,
  exact validation/templates, process timeouts, gh identity/repository verification,
  separate permissions and safe failure behavior.
- `internal/bunkoscaffold/templates.json`: canonical shared templates, license text,
  explicit official policy targets. `scaffold_test.go`: isolated fake-gh coverage.
- `selfhost/bunkoscaffold/core.mio`: native local wizard/flags, exclusive directory,
  cleanup, bounded input, validation and environment reporting.
  `templates_generated.mio`, `core_test.mio`, `ci-module.json`, `test.sh`: shared
  assets, six native unit cases and module registration.
- `cmd/mio/bunko_scaffold*.go`, `bunko_command.go`, `cli_help.go`,
  `selfhost/cli/main.mio`, `selfhost/usage/usage.mio`: CLI dispatch/help/adapters,
  global option-value protection and generated-package reference tests.
- All 39 locale JSON catalogs and generated Go/Mio tables: existing i18n system,
  60 new keys, accurate English/Japanese/Simplified Chinese; other new translations
  currently English. Existing theme painting is reused.
- `scripts/bunko/generate.py`: development-time shared asset generation/check.
  `native_checks.py`: independent native CLI/create/check/test oracle.
- `scripts/ci/native_platforms.py`, `internal/test.sh`,
  `.github/workflows/{bunko-reference,bunko-scaffold}.yml`: six actual-host reference
  jobs, native end-to-end evidence and regression/catalog/CI harness validation.
- `.gitignore`, `docs/bunko-scaffold.md`, this file and a scoped correction to
  `docs/bunko-package-manager.md`: documentation tracked and native status precise.

Generated projects contain root source/test, dependency-only `bunko.toml`, separate
local `bunko.package.toml`, README, full MIT or Apache-2.0 license, `.gitignore`,
optional selected-platform test workflow. No installing, committing, pushing,
releasing or index submission occurs. Existing destinations are never overwritten.

## Architecture and policy decisions

1. Preserve #77's strict dependency-only manifest; package metadata is local and
   separate. Both TOML files and development configuration must be excluded from
   Bunko V1 source archives. No publishing format or index policy was changed.
2. Validate names/exact versions against official index rules. Require explicit
   platforms from index revision `e6c89a1890a782ff759c698ea527df85bbf2e9c0`:
   linux/x86_64, macos/arm64, windows/x86_64. Six compiler targets do not imply six
   approved publishing targets.
3. Create a new portable direct child using exclusive mkdir and private permissions.
   Reject existing files/directories/symlinks, traversal and Windows device names.
   Roll back known files only. No overwrite or recursive unrelated deletion.
4. Default operation is local. Reference Git/gh version discovery makes no auth/API
   query. Optional integration uses official gh, structured arguments, deadlines,
   filtered output and no raw credentials. Git environment overrides are removed.
5. Separate default-No permissions for Git init, Git configuration, connection,
   repository creation and official browser login. No push capability exists.
   Recheck account/owner/permissions. Conservative org-admin-only remote creation;
   existing origin/worktree `.git` files refused.
6. Shared JSON assets prevent native/reference template drift. Generation Python
   is developer/test tooling only; the native product does not invoke it.
7. Optional workflow pins official setup action and policy compiler 2.1.0, uses
   only explicitly selected targets, read-only permissions and no dependencies.
   Workflow plus external dependencies fails explicitly until native install exists.
   Existing Mio 2.1.2-dev banner is preserved; do not invent a 2.2.0 release.

## Native and reference status

| Capability | Native Mio | Go reference |
| --- | --- | --- |
| Local interactive/non-interactive creation | Implemented | Implemented |
| Shared source/test/license/workflow templates | Implemented | Implemented |
| Validation, destination refusal, rollback | Implemented | Implemented |
| Compiler/OS/CPU discovery | Implemented | Implemented |
| Git/gh executable discovery | Paths only | Paths and timed versions |
| Git/gh versions/auth/account/connection | BLOCKED / unsupported | Implemented, fake-gh tested |
| Secure installation | BLOCKED / unimplemented | #77 reference only |
| External package imports | BLOCKED / unimplemented | #77 offline reference only |
| Publishing/releases/index submissions | Out of scope | Out of scope |

Native `connect` returns `BUNKO_E_SCAFFOLD_NATIVE_PROCESS` without external action.
Native `create --git-init`/`--github` retains the created local package then returns
that diagnostic. Native help explains the boundary. These cases are explicit
unsupported behavior, not successful integration tests.

## Test commands and local evidence

Run from the repository root. Tests use temporary directories and mocked gh;
they do not create real repositories or require personal credentials.

```sh
go test -race ./internal/bunkoscaffold -count=1 -timeout 5m
go test ./cmd/mio ./internal/bunko ./internal/lsp ./internal/i18n ./editors -count=1 -timeout 10m
go test ./cmd/mio -run TestBunko -count=1 -timeout 5m
python3 scripts/bunko/generate.py --check
python3 scripts/i18n/catalog.py check --check-generated
python3 scripts/i18n/test_catalog.py
bash scripts/ci/selftest.sh
go vet ./internal/bunkoscaffold ./cmd/mio
```

Native local module verification on macOS ARM64 uses the SHA-256-verified GF4
seed at `build/gofree-evidence/GF4/bootstrap-r2/chain/D/mio`, hash
`4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da`:

```sh
build/gofree-evidence/GF4/bootstrap-r2/chain/D/mio test selfhost/bunkoscaffold \
  --bunko-root "$PWD/bunko" --lang=en-US --format=json
```

The existing Go CLI cannot check the native `kiso/os` import (its `mio/os/runtime`
registry is unavailable); a failed exploratory Go check is not native evidence.
The native seed/module tests and six-platform native workflow are separate.

Local Go CLI/loader/LSP/i18n/editor regression suite passed. Scaffold race tests
passed. All 16 catalog tests passed after replacing dynamic prompt/error lookups
with an explicit statically auditable key map. The final local native module run
passed all six cases (71.818 seconds). All 65 CI harness self-tests passed
(9.292 seconds), including Python 3.9 driver inspection; native CI uses 3.12.
`go vet`, shared asset/catalog checks, shell syntax and `git diff --check` passed.

Coverage includes interactive approve/reject, noninteractive creation, exact names
and versions, explicit targets, file/directory collisions, traversal/symlinks,
partial writes, missing tools, unauthenticated/multiple accounts, per-action
approve/reject, failed Git/login/repository creation, org authority, concurrent
metadata edits, localized diagnostics, bounded input/process timeouts, generated
package compilation and unit execution. Symlink fixtures report actual platform
skips/NOT_RUN separately if the host cannot create them.

## Remote verification

Pending the focused PR. Record current code SHA, run URLs, actual per-platform
results, and skips here before declaring the implementation phase finished.
The six native checks create with an empty PATH and then run native `check` and
`test` on the generated package. Separate six-host Go reference jobs must never
be presented as native execution.

## Known limitations and exact native requirements

- Native production `os.spawn`/exec is blocking and cannot safely capture bounded
  stdout/stderr with deadlines. `os.spawn_test` is test-only and must not be
  repurposed. A portable structured process API with cwd/environment control,
  bounded separate streams, exit status, timeouts/termination and stdin/interactive
  support is required across all six backends. This is major runtime work and
  was deliberately not introduced in scaffolding.
- Native Git/gh version and authentication queries, repo creation/connection and
  their permissions remain unsupported. Native connect also needs bounded strict
  parsing of local metadata and atomic, symlink-safe config publication.
- Parent #77 native installer needs supported HTTPS/TLS and redirect policy,
  bounded gzip/tar extraction, strict TOML, exclusive files and atomic rename/
  complete-cache publication. SHA-256 exists; do not substitute hidden Go/Python,
  curl, tar or shell dependencies.
- Native external imports still need secure declared-cache resolution preserving
  local/kiso imports, package identity and no implicit download.
- New repo creation can leave an approved empty remote if a later permission is
  declined or local step fails. Source files remain; no destructive remote rollback.
- Local metadata is not a publishing schema. Package creation does not establish
  namespace ownership. Existing origin/worktree configurations are conservative
  refusals. Concurrent hostile same-user filesystem tampering is not a supported
  transactional filesystem model.
- Only MIT/Apache-2.0 templates; only currently approved publishing targets;
  no workflow for packages with external dependencies; other new locale texts
  remain English. No installation/publishing end-to-end success is claimed.

## Next tasks for Claude

Review this focused PR and #77 first. Complete native process/filesystem/network/
archive/TOML capabilities with portable tests, then native scaffolding integration,
secure installation and external imports. Implement separately authorized
publishing with immutable GitHub releases and official index submissions only
after auditing the current policy. Validate create → compile/test → install →
import across all six native targets; policy-rejected publishing targets remain
rejected until a separately reviewed upstream change. Preserve existing tests,
commands, i18n/themes, cache/manifest/provenance defenses. Do not merge or release.

## Copy-ready English Claude prompt

```text
Claude, continue the Mio Bunko work in https://github.com/mio-kobo/mio for the
Mio-2.2.0 development line. Read docs/bunko-scaffold-handoff.md and
docs/bunko-scaffold.md first, then inspect the current repository, scaffold PR,
PR #77, their latest CI and the official mio-kobo/bunko-index specification.
Fetch current state; do not assume either PR remains open. Use an independent
branch based on the appropriate latest head and document dependencies. Do not
modify another active development branch without the owner's authorization.

Review the completed native local create wizard, shared templates, exact
validation, separate local metadata, i18n and reference Git/gh permissions.
Preserve existing functionality and tests. Native local creation is implemented;
native Git/gh execution, installation and external imports remain unsupported.
Do not count Go reference tests or explicit capability errors as native success.

Your next tasks are:
1. Audit scaffolding and fix demonstrated defects without changing index policy.
2. Complete missing portable native runtime capabilities: structured bounded
   subprocess execution with deadlines, safe filesystem publication, HTTPS/TLS,
   strict bounded TOML and gzip/tar. Do not repurpose test-only spawn APIs or add
   hidden Go, Python, curl, tar or shell runtime dependencies. Keep incomplete
   operations explicitly unsupported and document major changes for review.
3. Finish native Git/gh discovery, versions, auth/account checks and optional
   connect/create behavior. Use official gh interfaces, no raw token access,
   automatic token refresh/scope expansion or tool installation. Show exact
   previews and require separate default-No consent for login, Git init/config,
   connecting, repository creation and any future push. Verify namespace identity,
   org authority and repo permissions. Mock GitHub interactions in tests.
4. Implement secure native package installation matching #77's exact graph,
   provenance, immutable release/asset/tag identity, revocation, compiler/platform,
   SHA-256, archive limits, no links/traversal/binaries, complete cache staging,
   concurrent publication and failure-safe manifest rules.
5. Complete native external package imports from verified declared offline cache,
   preserving local and kiso imports and avoiding implicit downloads.
6. Implement the official package publishing workflow as a separate explicit
   operation: validate namespace/version/platform/license, compile/test, construct
   a root-relative V1 source-only archive excluding development TOML/Git/workflow
   files, calculate hashes, integrate immutable GitHub releases/assets and generate
   official bunko-index submissions. Re-read policy; never invent an endpoint,
   mutate published versions or expand approved targets automatically. Remote
   writes/pushes/releases/index submissions require the owner's explicit approval
   of the concrete operation; scaffold consent does not authorize publishing.
7. Validate creation, compilation, unit tests, installation and imports end-to-end
   on actual linux/x86_64, linux/arm64, macos/x86_64, macos/arm64,
   windows/x86_64 and windows/arm64 native hosts. Keep Go/reference/native results
   distinct; report skips, unsupported cases and failures accurately. Do not
   confuse six compiler targets with the index's three currently approved
   publishing targets. Preserve English/Japanese/Simplified Chinese and themes.

Use focused reviewable commits/PRs with English type: description titles and no
AI attribution. Run appropriate regressions, monitor CI, fix failures, and update
handoff documentation with exact SHAs, commands, results and URLs. Do not
automatically merge PRs, create release tags, publish releases, or change index
policies. Stop at a reviewable handoff and await the repository owner's next
instruction before any further phase or unapproved remote publishing action.
```
