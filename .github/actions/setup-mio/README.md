# setup-mio

Installs an exact Mio release and puts `mio` on `PATH`.

```yaml
permissions:
  contents: read
steps:
  - uses: actions/checkout@<sha>
  - uses: mio-kobo/mio/.github/actions/setup-mio@<tag or sha>
    with:
      version: 2.1.2        # exact x.y.z; "latest" and ranges are rejected
      distribution: full    # full embeds Bunko; core needs MIO_BUNKO / --bunko-root
  - run: mio test ./...
```

Behaviour:

- Downloads `mio-<version>-<os>-<arch>-<distribution>` from the GitHub release and
  verifies it against the release `SHA256SUMS` before extracting. A mismatch fails the step.
- Supported platforms are exactly those with published release assets:
  linux-amd64, darwin-arm64, windows-amd64. Any other platform fails with a clear
  error; no other build is substituted. A missing release or asset also fails.
- Installs into the runner tool cache and marks it verified, so later steps and
  self-hosted runners reuse it (`cache-hit` output).
- Checks that the installed binary reports the requested version.
- Needs only `contents: read`; the token is used to read public release assets.

Limits: the checksum file comes from the same release as the archive, so this
detects corruption and partial uploads; it is not a signature check. Verifying the
release attestation (`release-verification.sigstore.json`) is a possible follow-up.
Pin the action by commit SHA in your workflows.
