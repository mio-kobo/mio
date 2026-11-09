# Mio Core / Full distribution and release process

This document describes the version-independent release system used for Mio
v2.1.1 and every later release. The same workflow and scripts handle v2.1.2,
v2.2.0, v3.0.0 and future major versions without release-specific rewrites.

## Version archive branches

Mio keeps a permanent archive branch per release version. The branch contains
the exact source of that release and stays checked out at the release tag.

| Release tag | Version archive branch |
| --- | --- |
| `v2.1.1` | `Mio-2.1.1` |
| `v2.1.2` | `Mio-2.1.2` |
| `v2.2.0` | `Mio-2.2.0` |
| `v3.0.0` | `Mio-3.0.0` |
| `v2.1.2-rc.1` | `Mio-2.1.2` |
| `v3.0.0-rc.1` | `Mio-3.0.0` |

The only historical exception is the already published v2.1.0 line, whose
release and RC tags predate per-version branches and remain on `Mio-2.1`.
Previously published releases, tags and branches are never rewritten.

`scripts/distribution/versions.py` is the single source of truth for parsing and
branch resolution. It is used by the distribution workflow, the package-manager
workflow and the release scripts; no workflow duplicates a version regex.

A release tag must resolve to the exact tip of its version archive branch. The
branch does **not** need to be merged into the repository default branch. A tag
pointing at an unrelated commit, a branch that moved after the tag, a missing
branch, an invalid version, a mismatched stable/RC request, or a source revision
that differs from the tagged commit all fail before a build starts.

## Stable release tags and RC tags

* A stable tag is exactly `v<major>.<minor>.<patch>` and a stable build is
  requested with `stable_version=<major>.<minor>.<patch>`.
* An RC tag is exactly `v<major>.<minor>.<patch>-rc.<N>` with `N >= 1` and is
  requested with `rc_version=<major>.<minor>.<patch>-rc.<N>`.
* `stable_version` and `rc_version` are mutually exclusive and must match the
  exact dispatch tag.
* RC and PR candidates never use the trusted bootstrap seed and can never be
  promoted into a stable release. Only a stable build compiled from the exact
  tagged source revision can produce stable release assets.
* Versions before 2.1.0 (`v1.x`, `v2.0.x`) are historical and are not rebuilt by
  this pipeline.

## Trusted bootstrap profiles

Stable builds are rooted in a reviewed, immutable native bootstrap seed.
`scripts/distribution/bootstrap.py` maps a release version to a reviewed
profile, and `scripts/distribution/seed_evidence.py` pins the seed's release tag,
release ID, asset ID, asset name and SHA-256.

* `native-v21` is the reviewed seed for the v2.1.x maintenance line
  (`2.1.1 <= version < 2.2.0`).
* `gf4` is retained only for the historical v2.1.0 evidence format.
* A future minor or major line (for example 2.2.0 or 3.0.0) has no profile until
  a maintainer registers one. The workflow then fails closed with an actionable
  error instead of trusting an unverified binary. Registering a new profile
  requires adding the immutable seed pins and a reviewed version range; it is a
  source change subject to normal review.

The existing verification chain is unchanged: the pinned seed is retrieved from
its immutable GitHub Release, its SHA-256 and release attestation are checked,
its macOS code signature is verified, and it runs only inside the production
sandbox. The seed builds Darwin CLI A; A builds B, B builds C and C builds D.
Darwin B, C and D must match byte for byte. The verified Full compiler builds a
disposable Core source with an empty resource table, and Core B, C and D must
reach their own fixed point. Darwin B and C independently build the Linux and
Windows Stage A images and must agree for each target. Each packaging job binds
the seed-derived hashes to its own native B/C/D before writing an archive.

## Core and Full distribution

Every release target produces two native variants:

* **Core**: the native Mio compiler without embedded Bunko. `kiso/` imports need
  an external Bunko tree supplied with `--bunko-root`, `MIO_BUNKO` or an
  executable-relative `../bunko`.
* **Full**: the native Mio compiler with the complete Bunko standard library
  embedded. A single self-contained `bin/mio` works offline and needs no
  additional Bunko directory.

Both variants keep the integrated CLI tools. Supported targets are
`darwin-arm64`, `linux-amd64` and `windows-amd64`. Windows releases add the two
self-contained Inno Setup installers, and Linux Full is exercised in Docker with
`--network=none`, then saved, removed, loaded and exercised offline again.

PR candidates (`<version>-pr.<sha>`) and RC builds run the same three-platform
native, archive, manager and Docker pipelines with trusted seed verification
recorded as `NOT_RUN`. Stable builds additionally require the trusted seed chain.

## Release preparation

Release inputs are copied to a disposable checkout. `prepare.py` reads the
development version declared by `selfhost/cli/main.mio`, requires it to match the
requested release base version, and stamps the exact release version and full
source commit into the CLI and `distribution-input.json`. `package.py` then
requires the native report's `distribution_input` to equal that prepared input
record, so the source version, requested release version, tag and generated
compiler version cannot silently diverge.

Dispatch the distribution workflow on the existing tag, from the matching
version archive branch, after the branch exists at that exact commit:

```bash
gh workflow run distribution-pr.yml \
  --repo mio-kobo/mio \
  --ref v2.1.2 \
  -f stable_version=2.1.2
```

For an RC:

```bash
gh workflow run distribution-pr.yml \
  --repo mio-kobo/mio \
  --ref v2.1.2-rc.1 \
  -f rc_version=2.1.2-rc.1
```

The command works once the matching version branch and release tag exist and all
required release prerequisites (including a reviewed bootstrap profile for a
stable release) are satisfied. The workflow does not create tags or Releases.

## Release artifact validation

A stable run assembles, attests and uploads only complete evidence. The required
set is:

* six native Core/Full archives: `mio-<version>-<target>-{core,full}.{tar.gz,zip}`
* both Windows installers: `mio-<version>-windows-amd64-{core,full}-setup.exe`
* the Linux Full Docker archive and its offline verification report
* `SHA256SUMS`
* `<target>-native-report.json` for all three targets
* `<target>-verification.json` for all three targets
* `<target>-package-manager-verification.json` for all three targets
* the trusted seed report and all of its execution logs
* `docker-verification.json`, `chocolatey-verification.json` and the three APT
  container reports
* the signed `release-verification.json` inventory and its Sigstore bundle

Artifact names are derived from the resolved version, not from a hardcoded
release. The GitHub attestation binds the inventory to the official workflow, the
exact tag, the exact source commit and the Actions run attempt. Missing, failed
or mismatched evidence fails the run; incomplete artifacts are never published.

To verify downloaded assets independently:

```bash
tag=v2.1.2
source=$(git rev-parse "refs/tags/$tag^{commit}")
gh attestation verify release-verification.json \
  --bundle release-verification.sigstore.json \
  --repo mio-kobo/mio \
  --signer-workflow mio-kobo/mio/.github/workflows/distribution-pr.yml \
  --source-ref "refs/tags/$tag" --source-digest "$source" --signer-digest "$source" \
  --deny-self-hosted-runners
sha256sum -c SHA256SUMS
```

## GitHub Release publication

The distribution workflow does not create a GitHub Release. After the
`mio-stable-release-assets-<run>-<attempt>` artifact and its attestation have
been reviewed, a maintainer publishes the Release from the exact tag and attaches
the complete artifact. Publishing is a maintainer action, never an automatic
step in a PR or tag-controlled job.

## Package-manager publication

`package-managers.yml` validates an already published stable Release. It resolves
the tag to its version archive branch, requires the tag to be that branch's exact
tip (with the documented v2.1.0 ancestor exception), and checks the published
release's source commit. It then verifies the signed release evidence and every
asset digest before generating Homebrew formulae, WinGet manifests, Chocolatey
packages and APT packages.

The workflow supports a `release: published` event and a manual dry run. A dry
run never publishes. Publication jobs stay behind the `package-managers` and
`chocolatey-community` environments, required reviewers and the
`MIO_PACKAGE_PUBLISH_ENABLED` / `MIO_CHOCOLATEY_PUBLISH_ENABLED` repository
variables. Only those privileged jobs receive publication credentials; PR and
tag-controlled jobs never do. No package is submitted to an external repository
automatically.

See [package-manager operations](../../scripts/package_managers/README.md) for the
complete review and activation procedure.
