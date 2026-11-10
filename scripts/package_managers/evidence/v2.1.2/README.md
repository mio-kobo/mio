# Pinned v2.1.2 verification reports

Mio v2.1.2 was published as an immutable GitHub Release before two Windows-only
verification reports were attached. Immutable Releases cannot gain assets, so
the reports live here as exact, byte-identical copies of the attested outputs.

| File | SHA-256 |
| --- | --- |
| `windows-amd64-verification.json` | `a055d022a98bbcb1e80dafbf7267ca8be138ff52732a8d793289c3e1de14207f` |
| `windows-amd64-package-manager-verification.json` | `619dfe6605693bd90a04a11c476d81da3b58550aaa028e99749ed4d7fdc8dbb0` |

Both digests match the entries in the signed `release-verification.json`
inventory of the v2.1.2 Release. The files came from the attestation-matching
build: GitHub Actions run `42777698615`, attempt `1`, artifact
`mio-stable-release-assets-42777698615-1`.

`restore_pinned()` in `scripts/package_managers/release.py` copies one of these
reports into the downloaded Release assets only when the signed inventory names
it, the Release omits it, and its SHA-256 matches the signed digest. Nothing
else is restored from this directory; a missing payload, seed report, log or
signed inventory still fails the gate.
