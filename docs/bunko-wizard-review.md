# Bunko wizard redesign review

Implemented on `EXP/bunko-scaffold` for [PR #83](https://github.com/mio-kobo/mio/pull/83).
This report supersedes the previous handoff's wizard behavior and verification
claims for this revision. No other development branch, release or index was changed.

## Behavior

The default wizard has three questions: full `namespace/name`, description and
explicit publishing platforms. Version `0.1.0`, MIT, no dependencies, no Git,
no GitHub and no workflow are defaults. Names are validated before later questions;
existing destinations are refused before description entry. Every create still
uses exclusive directory creation and preserves the prior name/version/platform
security rules. `--advanced` adds local configuration and final confirmation;
existing non-interactive flags and explicit integration permissions remain.

The macOS ARM64 native menu supports Up/Down, Space, Enter, Esc and Ctrl+C.
No platform starts selected. Only the official policy's three targets are shown;
the current device is labelled without being selected. A numeric fallback is used
when native byte input is unavailable or `TERM=dumb`. Non-TTY create rejects
immediately with non-interactive flag instructions, including when stdin stays open.

Question titles and the active menu row use the existing Theme `command` accent;
success and errors use its existing semantic colors. NO_COLOR, explicit color
settings and locale parsing/fallback retain the shared CLI behavior. There are no
fixed color escapes in the wizard and no terminal UI runtime dependency.

A minimal native terminal operation saves/restores termios on Darwin ARM64 and
provides a SIGINT mask around file publication. An interrupt during publication
is checked before the mask is restored, with owned generated files removed first.
Other native targets currently use numeric input; their new terminal binding
fails closed rather than invoking an external utility. Reference Go creation
also handles interruption and rolls back owned files.

The single shared README template is always English. Its guide covers development,
tests, safe root-relative archives, public upstream repositories, immutable draft
release publication, real release/asset IDs and SHA-256, placeholder index TOML,
validation, fork/PR submission, CI and review. README prose is not in the catalogs.
The metadata and selected platform/dependency tables are rendered from user values.
No repository URL, commit, ID, hash or published state is fabricated.

## Official policy checked

The upstream index was read and cloned at
`e6c89a1890a782ff759c698ea527df85bbf2e9c0`, matching the existing bundled policy.
The following official sources were checked; their links are in the generated guide:

- https://github.com/mio-kobo/bunko-index
- https://github.com/mio-kobo/bunko-index/blob/main/CONTRIBUTING.md
- https://github.com/mio-kobo/bunko-index/blob/main/docs/package-format.md
- https://github.com/mio-kobo/bunko-index/blob/main/docs/testing.md
- https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases

The generated README's exact archive recipe was executed. The resulting archive
passed the upstream `tools/bunko_archive.py:extract_package` validator with only
`LICENSE`, `README.md`, `package.mio` and `package_test.mio` at its root.
Both development TOML files, Git configuration and workflows were excluded.

## i18n keys

All 39 catalogs retain their complete key set. New CLI translations are natural
English, Japanese and Simplified Chinese; other new locale entries use English
according to the existing catalog convention. No new locale resolver or fallback
system was introduced. No translated README variants were generated.

Added:

- `scaffold.current_device`
- `scaffold.keys`
- `scaffold.location`
- `scaffold.next`
- `scaffold.non_tty`
- `scaffold.numbers`
- `scaffold.platform_linux_x86_64`
- `scaffold.platform_macos_arm64`
- `scaffold.platform_windows_x86_64`
- `scaffold.prompt_full_name`
- `scaffold.publish_guide`
- `scaffold.select_platforms`
- `scaffold.summary`
- `scaffold.summary_dependencies`
- `scaffold.welcome`

Modified:

- `scaffold.prompt_description`
- `scaffold.prompt_version`
- `scaffold.prompt_license`
- `scaffold.prompt_platforms`
- `scaffold.created`
- `scaffold.help_create`

## Actual native interaction output

The three UTF-8 transcripts below were captured from the final macOS ARM64 CLI
through OS PTYs, including invalid input/retry and multiple selections. Cursor
movement is removed from the readable logs, so successive menu redraws remain
visible as repeated rows. The test compares restored user termios settings;
Darwin's transient kernel `PENDIN` bit is excluded from that comparison.

### en-US

Full log: `build/bunko-wizard/verified/en-US.txt`. Selected actual lines:

```text
♡ Let's create a Mio package!
? Package name (namespace/name)
✓ Package created successfully!
♡ Want to share your package? See the Bunko publishing guide in README.md.
```

### ja-JP

Full log: `build/bunko-wizard/verified/ja-JP.txt`. Selected actual lines:

```text
♡ Mio パッケージを作りましょう！
? パッケージ名（namespace/name）
✓ パッケージを作成しました！
♡ パッケージをみんなに使ってもらいませんか？ README.md の Bunko 公開ガイドをご覧ください。
```

### zh-CN

Full log: `build/bunko-wizard/verified/zh-CN.txt`. Selected actual lines:

```text
♡ 让我们创建一个 Mio 软件包吧！
? 软件包名称（namespace/name）
✓ 软件包创建成功！
♡ 想让大家使用你的软件包？查看 README.md 中的 Bunko 发布指南。
```

## Verification

Actual host: macOS 15.8.2, ARM64. Product processes in the native regressions
ran with an empty PATH; Python and Go below are development/test tooling only.

| Check | Result |
| --- | --- |
| Native CLI compilation, file identification | PASS, Mach-O ARM64 |
| `python3.12 scripts/bunko/test_wizard.py ...` | PASS, 8 tests, 0 skips |
| Three native language interactions | PASS, en-US / ja-JP / zh-CN |
| All 39 CLI locales produce the same English README | PASS, byte-for-byte |
| Arrow/Space multi-selection and empty-selection refusal | PASS |
| Invalid name/description/platform retry | PASS (native and reference coverage) |
| Esc / Ctrl+C, restored terminal settings, no project after cancellation | PASS |
| Native SIGINT publication guard, pending until restoration | PASS |
| Theme accent/success/error, NO_COLOR and Unicode description | PASS |
| Numeric fallback and open non-TTY stdin | PASS |
| `--non-interactive` compatibility and existing security checks | PASS |
| Existing native scaffold oracle | PASS, 10 checks |
| Native `selfhost/bunkoscaffold` module | PASS, 6 tests |
| Native `bunko/kiso/os` module | PASS, 7 tests |
| Generated projects `mio check .` / `mio test .` | PASS, all three languages |
| README Markdown fences, dynamic TOML fields, archive recipe | PASS |
| Official archive validator | PASS |
| `go test -race ./internal/bunkoscaffold ./internal/bunkopm ./internal/i18n -count=1 -timeout 5m` | PASS |
| `go test ./cmd/mio -count=1 -timeout 10m` | PASS, 89.766s |
| Focused CLI Bunko / language / theme / help regressions | PASS |
| `python3 scripts/i18n/catalog.py check --check-generated` | PASS, 39 locales / 215 keys |
| `python3 scripts/i18n/test_catalog.py` | PASS, 16 tests |
| `python3 scripts/bunko/generate.py --check` | PASS |
| `bash scripts/ci/selftest.sh` | PASS, 65 tests |
| `python3 scripts/ci/bunko_matrix.py --check` | PASS |
| Embedded Bunko bytes, lengths and SHA-256 against current sources | PASS |
| Terminal ABI fixture cross-build to other five native targets | PASS (compilation only) |
| `go vet ./internal/bunkoscaffold ./cmd/mio` | PASS |
| `git diff --check` | PASS |

The five terminal ABI cross-builds target Linux x86-64/ARM64, macOS x86-64,
and Windows x86-64/ARM64. They were not executed on those hosts and are not
reported as native runtime passes. Arrow input and publication SIGINT protection
were exercised on macOS ARM64. Other native targets retain numeric input; the
new terminal operation is explicitly unavailable there. Native Git/gh integration,
installation and remote publishing retain their prior explicit limitations.

An exploratory full race run including `./cmd/mio` reached its 10-minute timeout
in the unrelated `TestCLIKisoConvertRandomFloatParsing` test. It is **not** counted
as PASS. The complete non-race CLI suite subsequently passed, and the focused
Bunko/security/i18n race suites passed separately. Early development builds found
unsupported string escapes and modulo syntax, and early PTY harness runs needed
Darwin teardown/PENDIN handling; those failed attempts are superseded by the
final successful build and the eight passing native wizard tests.

No fresh six-host remote CI result is claimed for this change. The existing
native CI driver now runs the PTY/README suite for Full and Core macOS ARM64
images, with independent artifact directories. Other hosts keep existing native
scaffold regressions. Owner review/experience remains the next step.

Evidence is under `build/bunko-wizard/`: `verified-tests.log`,
`verified/report.json`, three locale transcripts, sample `verified/README.md`,
`verified/source-archive.tar.gz`, `native-module.json`, `native-os-module.json`,
`reference-final.log`, `cli-full.log`, `cli-focused.log`, catalog/selftest logs,
`terminal-cross-builds.json`, `native-scaffold-final/report.json` and `reviewed-build.log`.

## Executable for owner experience

Path: `/Volumes/Sector0/AppData/Mio/mio/build/bunko-wizard/mio-darwin-arm64`

SHA-256: `34ae700e548ac88ae8807bc0b9ecfc469e518cd212dd7f22dcd279045c1a9a5a`

The executable retains the repository's existing development version banner;
no Mio version or release was published. From a fresh directory:

```sh
/Volumes/Sector0/AppData/Mio/mio/build/bunko-wizard/mio-darwin-arm64 --lang zh-CN bunko create
/Volumes/Sector0/AppData/Mio/mio/build/bunko-wizard/mio-darwin-arm64 --lang ja-JP bunko create
/Volumes/Sector0/AppData/Mio/mio/build/bunko-wizard/mio-darwin-arm64 --lang en-US bunko create
```

## Changed files

- `.gitignore`
- `bunko/kiso/aot/lower/nativefs/nativefs.mio`
- `bunko/kiso/aot/lower/nativefs/process.mio`
- `bunko/kiso/aot/lower/nativefs/terminal.mio`
- `bunko/kiso/aot/lowerlinux/runtime_os.mio`
- `bunko/kiso/aot/lowerwinrt/winos.mio`
- `bunko/kiso/aot/lowerwinrt/winprocess.mio`
- `bunko/kiso/os/os.mio`
- `bunko/kiso/os/platform_test.mio`
- `docs/bunko-scaffold-handoff.md`
- `docs/bunko-scaffold.md`
- `docs/bunko-wizard-review.md`
- `internal/bunkoscaffold/platforms.go`
- `internal/bunkoscaffold/scaffold_test.go`
- `internal/bunkoscaffold/spec.go`
- `internal/bunkoscaffold/templates.json`
- `internal/bunkoscaffold/terminal_darwin.go`
- `internal/bunkoscaffold/terminal_other.go`
- `internal/bunkoscaffold/wizard.go`
- `internal/i18n/messages_generated.go`
- `scripts/bunko/fixtures/interrupt_guard.mio`
- `scripts/bunko/native_checks.py`
- `scripts/bunko/test_wizard.py`
- `scripts/ci/native_platforms.py`
- `selfhost/bunkoscaffold/core.mio`
- `selfhost/bunkoscaffold/core_test.mio`
- `selfhost/bunkoscaffold/templates_generated.mio`
- `selfhost/bunkoscaffold/wizard.mio`
- `selfhost/darwin-amd64-compiler/main.mio`
- `selfhost/frontend/compiler.mio`
- `selfhost/i18n/locales/ar-SA.json`
- `selfhost/i18n/locales/bn-BD.json`
- `selfhost/i18n/locales/cs-CZ.json`
- `selfhost/i18n/locales/da-DK.json`
- `selfhost/i18n/locales/de-DE.json`
- `selfhost/i18n/locales/el-GR.json`
- `selfhost/i18n/locales/en-GB.json`
- `selfhost/i18n/locales/en-US.json`
- `selfhost/i18n/locales/es-ES.json`
- `selfhost/i18n/locales/es-MX.json`
- `selfhost/i18n/locales/fa-IR.json`
- `selfhost/i18n/locales/fi-FI.json`
- `selfhost/i18n/locales/fil-PH.json`
- `selfhost/i18n/locales/fr-FR.json`
- `selfhost/i18n/locales/he-IL.json`
- `selfhost/i18n/locales/hi-IN.json`
- `selfhost/i18n/locales/hu-HU.json`
- `selfhost/i18n/locales/id-ID.json`
- `selfhost/i18n/locales/it-IT.json`
- `selfhost/i18n/locales/ja-JP.json`
- `selfhost/i18n/locales/ko-KR.json`
- `selfhost/i18n/locales/ms-MY.json`
- `selfhost/i18n/locales/nb-NO.json`
- `selfhost/i18n/locales/nl-NL.json`
- `selfhost/i18n/locales/pl-PL.json`
- `selfhost/i18n/locales/pt-BR.json`
- `selfhost/i18n/locales/pt-PT.json`
- `selfhost/i18n/locales/ro-RO.json`
- `selfhost/i18n/locales/ru-RU.json`
- `selfhost/i18n/locales/sv-SE.json`
- `selfhost/i18n/locales/ta-IN.json`
- `selfhost/i18n/locales/te-IN.json`
- `selfhost/i18n/locales/th-TH.json`
- `selfhost/i18n/locales/tr-TR.json`
- `selfhost/i18n/locales/uk-UA.json`
- `selfhost/i18n/locales/vi-VN.json`
- `selfhost/i18n/locales/zh-CN.json`
- `selfhost/i18n/locales/zh-HK.json`
- `selfhost/i18n/locales/zh-TW.json`
- `selfhost/i18n/messages_generated.mio`
- `selfhost/linux-arm64-compiler/main.mio`
- `selfhost/linux-compiler/main.mio`
- `selfhost/resources/bunko_data.mio`
- `selfhost/typecheck/expression.mio`
- `selfhost/windows-arm64-compiler/main.mio`
- `selfhost/windows-compiler/main.mio`
