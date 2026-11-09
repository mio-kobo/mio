# Package manager release operations

This directory extends `scripts/distribution`; it does not build a new compiler.
The normal candidate workflow now runs real manager lifecycle tests after the
existing native fixed points and archive validation. No publication credentials
are available to PR jobs. Candidate metadata uses synthetic `0.0.1` → `0.0.2`
versions solely to exercise manager upgrades; shipped compiler bytes are unchanged.

## Stable inputs and review boundary

`package-managers.yml` reads an **already published** stable `v2.1+` GitHub Release.
Manual dispatch is always dry run. The gate requires all six native archives and both self-contained Windows installers,
`SHA256SUMS`, `<target>-native-report.json`, `<target>-verification.json`,
`docker-verification.json` bound to the Linux Full archive, exact
inventories, identical Core/Full compiler hashes, native B=C=D and every required
native check PASS. GitHub's asset SHA-256 digest must also match. All source SHAs
must resolve to the stable tag, which must belong to `Mio-2.1` history.

For stable builds, follow the [trusted GF4 and release verification instructions](../../docs/testing.md#stable-distribution-and-trusted-gf4).
Use the complete `mio-stable-release-assets-<run>-<attempt>` artifact from the
tagged run. The package-manager gate verifies the GitHub Actions attestation
against the exact tag and source commit, then compares the frozen GF4 origin
report with the natively tested B/C/D compiler in each package.

Generated output includes both Formulae with real archive hashes, both WinGet
Inno multi-file manifests, and both AMD64 Debian packages. No fake stable Formula or
placeholder SHA is committed. Actual manager install tests must pass for each
platform before cross-repository proposal jobs run. All generated files and real
manager reports are downloadable Actions artifacts.

## Activation (maintainer actions, not performed by this PR)

The `mio-kobo/homebrew-mio` and `mio-kobo/mio-apt` repositories already exist.
The implementation account has organization admin and repository admin access;
no new repository or permission workaround is needed.

1. Review/merge this infrastructure; prepare a stable release through the existing
   native pipeline with all required real evidence. Do not publish v2.1.0 early.
2. Create a GitHub App installed **only** on the two distribution repositories,
   with Contents write and Pull requests write. Configure environment
   `package-managers` with required reviewers and protected branch/tag policies.
   Set `MIO_DISTRIBUTION_APP_ID` and secret `MIO_DISTRIBUTION_APP_PRIVATE_KEY`.
3. Store a dedicated APT signing key as environment secret `MIO_APT_PRIVATE_KEY`;
   set its full uppercase fingerprint in `MIO_APT_SIGNING_FINGERPRINT`. Use a
   dedicated signing subkey exported for CI; keep the primary key offline. The
   batch GPG agent must be able to sign with this exported subkey. Private material
   is imported only into a temporary mode-0700 directory and removed on exit.
4. Run a dry run for the existing stable tag and inspect all artifacts/results.
5. Set repository variable `MIO_PACKAGE_PUBLISH_ENABLED=true`. Future published
   Release events can request environment approval and open distribution PRs.
   They never merge them. Enable GitHub Pages on the APT repository only after
   reviewing its signed snapshot; publish the key fingerprint independently.
6. Submit the generated `winget/manifests/m/MioKobo/...` tree for review manually
   to `microsoft/winget-pkgs`. No Microsoft submission credentials are used.

Repeated proposals compare an immutable per-version receipt before rebuilding
signed indexes or updating formulae. An identical version is a no-op; changed
bytes for that version fail. APT retains old pool files for upgrades and merges
only through a review PR. Run `apt-index.sh <snapshot>` with a dedicated GPG home
to generate Packages, Packages.gz, Release, Release.gpg, InRelease and the binary
public keyring. Pool collisions fail rather than overwrite content.

## Verification and sources

`test_install.py` checks installation, upgrades and removal using the actual manager,
checks every installed payload hash, runs `mio version`, compiles/executes a library
free program and checks Core's missing-Bunko diagnostic. Full checks/imports,
compiles and executes Bunko math. Linux uses a signed local APT repository and
`unshare --net` for offline execution. Relocated payloads are checked again.
Homebrew runs `brew audit` and `brew test`; installed LICENSE/README are
verified in Homebrew’s standard prefix location after its document normalization; Windows runs `winget validate` and
installs and upgrades the actual self-contained Inno installer, retaining the
full directory tree and checking the real command on user PATH. Core's separate
`mio-core` alias avoids collision; Homebrew/APT declare explicit conflicts. Clean Debian 12/13 and Ubuntu 22.04
containers also exercise signed APT upgrades. Automatic tests consume fresh artifacts from the same distribution run.
The optional manual regression workflow takes a successful native distribution
run ID and verifies its status/path before using its unexpired artifacts. Such
fixtures are never stable release inputs; PR CI has no expiring fixture pin.
Windows run/exec/test/LSP functionality is tested, matching the current native
protocol evidence. Windows TCP is covered by the required native CI checks.

The install tests require the native candidates produced by the distribution
workflow. Without them, lifecycle tests are NOT_RUN; unit tests do not claim real
manager validation. Homebrew's candidate test uses a temporary local HTTP server;
production formulae always reference fixed stable GitHub Release HTTPS URLs.

Official references:

- [Homebrew Formula Cookbook](https://docs.brew.sh/Formula-Cookbook)
- [WinGet 1.12 manifest specification](https://github.com/microsoft/winget-pkgs/blob/master/doc/manifest/schema/1.12.0/installer.md)
- [WinGet ZIP design](https://github.com/microsoft/winget-cli/blob/master/doc/specs/%23140%20-%20ZIP%20Support.md)
- [WinGet portable directory installation implementation](https://github.com/microsoft/winget-cli/blob/master/src/AppInstallerCLICore/Workflows/PortableFlow.cpp)
- [Debian repository format](https://wiki.debian.org/DebianRepository/Format)
- [Debian third-party repository key guidance](https://wiki.debian.org/DebianRepository/UseThirdParty)

Package ID check: GitHub Contents API returned 404 for
`microsoft/winget-pkgs/manifests/m/MioKobo` on 2026-11-06. No existing publisher
path was found; recheck immediately before the first upstream submission.

## Windows ZIP Portable finding

A real Windows Actions install confirmed ZIP Portable retained every Bunko file,
but invoking the WinGet-created `mio.exe` link made GetModuleFileNameW identify
the Links directory. Full then failed with E_BUNKO_MISSING. Direct invocation of
the installed compiler passed, so checking only the extracted ZIP was insufficient.

`windows.py` instead wraps the verified native ZIP in an Inno Setup installer. It
never recompiles Mio. Full preserves bin/mio.exe and bunko; Core installs the same
compiler bytes as bin/mio-core.exe and updates its installed file inventory. Both
use separate stable AppIds and per-user install directories. Their actual bin
directories are appended to user PATH; uninstall removes only its own entry. The
installer owns its normal uninstall executable/data files in addition to the Mio
payload. Inno Setup and Python are build-host tools, never runtime dependencies.

The lifecycle test installs synthetic metadata versions 0.0.1 and 0.0.2, then
upgrades to the actual candidate/Release installer bytes. It validates each manifest
with the current official WinGet client, tests direct and PATH invocation including
Bunko, and uninstalls using the local manifest before the ID has a community source
entry. Production users use the documented package ID after upstream publication.

The isolated Windows test runner trusts **only its loopback fixture host** during
local-manifest installation, restoring prior zone values afterwards. WinGet
otherwise leaves local manifests' EXEs marked as Internet downloads and can block
inside ShellExecute awaiting an unavailable security dialog. The real community
source promotes verified downloads to the trusted zone itself. No installed Mio
package changes users' Internet settings, and neither hashing nor malware scanning
is disabled. See [the Microsoft diagnosis](https://github.com/microsoft/winget-cli/issues/4046)
and [the trust decision in DownloadFlow](https://github.com/microsoft/winget-cli/blob/master/src/AppInstallerCLICore/Workflows/DownloadFlow.cpp).

## Chocolatey Community Repository

`chocolatey.py` reuses `release.validate` unchanged: all six stable archives,
Inno installers, source/tag binding, Trusted Seed PASS, native B=C=D, exact
inventories and Docker evidence remain mandatory. It emits deterministic,
metadata-only `mio.<version>.nupkg` (Full) and `mio-core.<version>.nupkg`.
Both include the OPC manifest and core properties relationships used by legacy
NuGet readers. Fixed ZIP metadata, stored entries and normalized script line
endings keep the nupkg hashes reproducible across platforms.
No binaries are embedded or rebuilt. Fixed official version URLs and SHA-256
come from the verified Windows ZIPs. The generator writes both as literal
arguments to `Get-ChocolateyWebFile` for CCR script validation. The installer
also checks the downloaded ZIP against the same pinned SHA-256 before extraction.
Candidate generation is a separate loopback
fixture API with only synthetic 0.0.1/0.0.2 package versions.

The existing Inno installer uses `PrivilegesRequired=lowest`, HKCU PATH and
`{localappdata}`. Elevated Chocolatey may run under another administrator's
identity; using that installer would install for the wrong user. Chocolatey
instead owns a ZIP payload under its package directory. `Install-BinFile` creates
process shims for `mio` and `mio-core`. Core renames only its executable to
`mio-core.exe` and updates the installed manifest key, preserving every compiler
byte (the same layout used by Inno). This is necessary because Chocolatey
automatically removes shims by executable basename on uninstall even with an
`.ignore` file. Distinct basenames protect Full when Core is removed; `.exe.ignore`
prevents automatic alias generation. Unlike the rejected WinGet ZIP link, a process shim launches the
real image and preserves executable-relative Bunko lookup. No user PATH registry
entries, user profiles or language/compiler bytes are changed. User projects
must live outside the replaceable package directory. Both uninstall scripts
remove only their own shim; Chocolatey removes its managed files.

`test_chocolatey_install.py` tests real Windows install/upgrade/reinstall,
Core/Full coexistence, PATH and ShimGen target, all file hashes, native execution,
Bunko imports, integrated tools and uninstall. Firewall rules block outbound
compiler traffic during offline checks and are removed afterwards. Stable jobs
also upgrade to the exact generated stable nupkg using its official HTTPS URL
and compare a Windows rebuild's SHA-256 to the Linux-generated package.
The Windows gate reads the packages through the pinned official NuGet 2 library,
validates the standard nuspec fields against its XSD and checks the Chocolatey
metadata extensions separately. It checks rejection without the manifest
relationship and compares the OPC layout to a local `choco pack` reference.
Both flavors must reject an incorrect ZIP checksum with the default Chocolatey
settings and with Chocolatey's checksum feature disabled.
Reports start FAIL and become PASS only after all checks execute.

Wrapper-only PRs reuse the existing Windows regression job in Chocolatey-only
mode (no unrelated PSGallery/WinGet download) and a successful,
unexpired `distribution-pr.yml` fixture. `fixture.py` checks repository, workflow,
conclusion and matching non-wrapper Git inputs before downloading. Missing or
mismatching fixtures fail (run the existing native workflow manually, then retry).
Compiler/source changes still run the full existing native pipeline, including
WinGet/Inno and Chocolatey lifecycles. Manual manager regression keeps all managers
enabled by default. Reused
fixtures are never stable release inputs. `.nupkg` files and JSON evidence are
Actions artifacts. Unit tests cover package metadata, immutable generation,
untrusted inputs, release gating, dry runs, and moderation-aware submission.

Maintainer submission:

1. Confirm ownership of both CCR IDs and inspect both exact versions in the
   signed-in moderation queue. For a first submission, confirm that neither
   version is already present. Public CCR endpoints may miss private entries.
   For the v2.1.0 repair, check both entries and confirm that any package whose
   content needs updating is Waiting, Pending, Responded, or Updated.
2. Dry-run `package-managers.yml` on `Mio-2.1` with the existing published tag.
   Inspect the generated nupkg files, hashes and Windows lifecycle evidence.
   Dry runs verify locally generated packages without contacting CCR.
3. Configure the `chocolatey-community` environment with required maintainer
   reviewers and protected branch policies for `Mio-2.1`. Store
   `CHOCOLATEY_API_KEY` as that environment's secret. Enable
   `MIO_CHOCOLATEY_PUBLISH_ENABLED` after maintainer review.
4. Dispatch `package-managers.yml` from `Mio-2.1` with the tag and
   `submit_chocolatey` enabled, then approve the protected environment. Enable
   `resubmit_chocolatey_v2_1_0` only to authorize the repair of `mio` and
   `mio-core` v2.1.0.
   Each submission needs a fresh dispatch after checking moderation. The job
   uploads to `https://push.chocolatey.org/` for normal community review.

`chocolatey_publish.py` checks the exact CCR records for both packages before
uploading either. It skips matching SHA-512 digests, blocks changes to approved
versions, and requires a recognized review status and explicit authorization
for any repair. A missing CCR record stops a repair. If a push fails or times
out, check both entries in the signed-in queue before a fresh dispatch. A
package already uploaded with the expected digest is skipped on that dispatch.
Logs show bounded HTTP diagnostics without printing the key, command arguments
or raw process output.

References: [Chocolatey setup](https://docs.chocolatey.org/en-us/choco/setup/),
[Install-BinFile](https://docs.chocolatey.org/en-us/create/functions/install-binfile/),
[Uninstall-BinFile](https://docs.chocolatey.org/en-us/create/functions/uninstall-binfile/),
[shim behavior](https://docs.chocolatey.org/en-us/features/shim/).
