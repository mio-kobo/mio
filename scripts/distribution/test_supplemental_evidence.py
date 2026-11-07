import copy
import gzip
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from package import digest, write_json
from seed_evidence import NATIVE_V21, REPOSITORY, SEED_LOGS
from stable_release import EVIDENCE_SCHEMAS, REPORT, evidence_files
from supplemental_evidence import ARCHIVE, EVIDENCE_TAG, LOG_DIR, RELEASE_PURL, STABLE_TAG, restore_logs

SOURCE = 'a' * 40


class SupplementalEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.assets = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.logs = {f'{LOG_DIR}/{name}': name.encode() for name in SEED_LOGS}
        self.payload = self.assets / ARCHIVE
        self.write_archive()
        hashes = {name: 'b' * 64 for name in evidence_files(STABLE_TAG[1:], NATIVE_V21)}
        hashes.update({name: hashlib.sha256(content).hexdigest() for name, content in self.logs.items()})
        self.report = dict(schema_version=EVIDENCE_SCHEMAS[NATIVE_V21], status='PASS',
                           version=STABLE_TAG[1:], source_sha=SOURCE, files=hashes)
        write_json(self.assets / REPORT, self.report)

        self.release = dict(id=42, tag_name=EVIDENCE_TAG, immutable=True, draft=False,
                            prerelease=True, published_at='2026-11-07T15:39:24Z', assets=[dict(
                                name=ARCHIVE, state='uploaded',
                                browser_download_url=f'https://github.com/{REPOSITORY}/releases/download/{EVIDENCE_TAG}/{ARCHIVE}',
                            )])
        self.commit = SOURCE
        self.statement = dict(
            predicateType='https://in-toto.io/attestation/release/v0.2',
            predicate=dict(databaseId='42', repository=REPOSITORY, tag=EVIDENCE_TAG, purl=RELEASE_PURL),
            subject=[dict(uri=RELEASE_PURL, digest=dict(sha1=SOURCE)), dict(name=ARCHIVE, digest={})],
        )
        self.sigstore = self.enterContext(patch('stable_release.subprocess.run'))
        self.github = self.enterContext(patch('supplemental_evidence.subprocess.check_output', side_effect=self.gh))

    def write_archive(self, extra=(), omitted=None, changed=None,
                      pax=b'52 LIBARCHIVE.xattr.com.apple.provenance=example123\n'):
        with tarfile.open(self.payload, 'w:gz', format=tarfile.USTAR_FORMAT) as archive:
            self.add_file(archive, f'PaxHeader/{LOG_DIR}', pax, tarfile.XHDTYPE)
            self.add_file(archive, LOG_DIR, b'', tarfile.DIRTYPE)
            for name, content in self.logs.items():
                if name != omitted:
                    self.add_file(archive, name, b'changed' if name == changed else content)
            for name, kind, link in extra:
                self.add_file(archive, name, b'', kind, link)

    def add_file(self, archive, name, content, kind=tarfile.REGTYPE, link=''):
        entry = tarfile.TarInfo(name)
        entry.type = kind
        entry.linkname = link
        entry.size = len(content)
        archive.addfile(entry, io.BytesIO(content))

    def gh(self, command, *, text):
        self.assertEqual(command[0], 'gh')
        self.assertTrue(text)
        if command[1] == 'api':
            self.assertEqual(command[2:4], ['--hostname', 'github.com'])
            if command[4] == f'repos/{REPOSITORY}/releases/tags/{EVIDENCE_TAG}':
                return json.dumps(self.release)
            self.assertEqual(command[4], f'repos/{REPOSITORY}/commits/refs/tags/{EVIDENCE_TAG}')
            return json.dumps(dict(sha=self.commit))
        self.assertEqual(command[1], 'release')
        self.assertEqual(command[3], EVIDENCE_TAG)
        self.assertEqual(command[command.index('--repo') + 1], f'github.com/{REPOSITORY}')
        if command[2] == 'download':
            self.assertEqual(command[command.index('--pattern') + 1], ARCHIVE)
            (Path(command[-1]) / ARCHIVE).write_bytes(self.payload.read_bytes())
            return ''
        self.assertEqual(command[2], 'verify-asset')
        self.assertEqual(digest(Path(command[4])), digest(self.payload))
        return json.dumps(dict(verificationResult=dict(statement=self.statement)))

    def bind_archive(self):
        sha = digest(self.payload)
        self.release['assets'][0].update(size=self.payload.stat().st_size, digest=f'sha256:{sha}')
        self.statement['subject'][1]['digest'] = {'sha256': sha}

    def restore(self):
        restore_logs(self.assets, STABLE_TAG, SOURCE)

    def assert_rejected(self, reason):
        with self.assertRaisesRegex(ValueError, reason):
            self.restore()
        self.assertFalse((self.assets / LOG_DIR).exists())

    def test_restores_exact_logs_using_both_attestations(self):
        self.bind_archive()
        self.restore()
        self.assertEqual(self.sigstore.call_count, 1)
        args = self.sigstore.call_args.args[0]
        self.assertEqual(args[:3], ['gh', 'attestation', 'verify'])
        self.assertEqual(args[args.index('--source-ref') + 1], f'refs/tags/{STABLE_TAG}')
        self.assertEqual(args[args.index('--source-digest') + 1], SOURCE)
        self.assertEqual(self.github.call_args.args[0][2], 'verify-asset')
        restored = {f'{LOG_DIR}/{path.name}': path.read_bytes() for path in (self.assets / LOG_DIR).iterdir()}
        self.assertEqual(restored, self.logs)
        self.assertEqual(json.loads((self.assets / REPORT).read_text()), self.report)

    def test_existing_logs_are_left_for_the_full_authentication_gate(self):
        destination = self.assets / LOG_DIR
        destination.mkdir()
        partial = destination / SEED_LOGS[0]
        partial.write_bytes(b'existing modified log')
        self.restore()
        self.assertEqual(list(destination.iterdir()), [partial])
        self.assertEqual(partial.read_bytes(), b'existing modified log')
        self.github.assert_not_called()

    def test_other_versions_do_not_retrieve_evidence(self):
        restore_logs(self.assets, 'v2.1.1', SOURCE)
        self.github.assert_not_called()
        self.sigstore.assert_not_called()

    def test_original_sigstore_failure_prevents_download(self):
        self.sigstore.side_effect = subprocess.CalledProcessError(1, 'gh')
        self.assert_rejected('stable release verification attestation')
        self.github.assert_not_called()

    def test_missing_release_or_asset_fails(self):
        self.github.side_effect = subprocess.CalledProcessError(1, 'gh')
        self.assert_rejected('could not verify or retrieve')
        self.github.side_effect = self.gh
        self.release['assets'] = []
        self.assert_rejected('only release asset')

    def test_wrong_release_metadata_fails(self):
        self.bind_archive()
        original = copy.deepcopy(self.release)
        for field, value in [('tag_name', STABLE_TAG), ('immutable', False), ('draft', True),
                             ('prerelease', False), ('published_at', None)]:
            with self.subTest(field=field):
                self.release = dict(original, **{field: value})
                self.assert_rejected('published immutable supplemental prerelease')
        self.release = original
        self.release['assets'][0]['browser_download_url'] = 'https://github.com/other/mio/asset'
        self.assert_rejected('asset metadata')

    def test_wrong_tag_commit_or_attested_commit_fails(self):
        self.bind_archive()
        self.commit = 'c' * 40
        self.assert_rejected('tag does not resolve')
        self.commit = SOURCE
        self.statement['subject'][0]['digest']['sha1'] = 'c' * 40
        self.assert_rejected('attestation does not bind the stable source commit')

    def test_wrong_attested_release_identity_or_verification_failure(self):
        self.bind_archive()
        predicate = self.statement['predicate']
        for field, value in [('repository', 'other/mio'), ('tag', STABLE_TAG), ('databaseId', '43'),
                             ('purl', f'pkg:github/{REPOSITORY}@{STABLE_TAG}')]:
            with self.subTest(field=field), patch.dict(predicate, {field: value}):
                self.assert_rejected('attestation identifies a different release')

        def fail_attestation(command, *, text):
            if command[1:3] == ['release', 'verify-asset']:
                raise subprocess.CalledProcessError(1, 'gh')
            return self.gh(command, text=text)
        self.github.side_effect = fail_attestation
        self.assert_rejected('could not verify or retrieve')

    def test_asset_digest_and_attested_digest_must_match(self):
        self.bind_archive()
        self.release['assets'][0]['digest'] = 'sha256:' + 'c' * 64
        self.assert_rejected('asset digest mismatch')
        self.bind_archive()
        self.statement['subject'][1]['digest']['sha256'] = 'c' * 64
        self.assert_rejected('attestation does not bind the evidence archive')

    def test_missing_or_modified_log_fails_after_archive_verification(self):
        name = next(iter(self.logs))
        for change, error in [('omitted', 'missing expected logs'), ('changed', 'does not match signed release evidence')]:
            with self.subTest(change=change):
                self.write_archive(**{change: name})
                self.bind_archive()
                self.assert_rejected(error)

    def test_rejects_malicious_archive_entries(self):
        for name, kind, link, error in [
            (f'{LOG_DIR}/../escape.log', tarfile.REGTYPE, '', 'unexpected'),
            ('/escape.log', tarfile.REGTYPE, '', 'unexpected'),
            (f'{LOG_DIR}/extra.log', tarfile.REGTYPE, '', 'unexpected'),
            (next(iter(self.logs)), tarfile.REGTYPE, '', 'duplicate'),
            (LOG_DIR, tarfile.DIRTYPE, '', 'duplicate'),
            (f'{LOG_DIR}/extra.log', tarfile.SYMTYPE, '../escape.log', 'unexpected'),
            (f'{LOG_DIR}/extra.log', tarfile.LNKTYPE, next(iter(self.logs)), 'unexpected'),
            ('global-pax', tarfile.XGLTYPE, '', 'unexpected'),
        ]:
            with self.subTest(name=name, kind=kind):
                self.write_archive(extra=[(name, kind, link)])
                self.bind_archive()
                self.assert_rejected(error)
        self.write_archive(pax=b'22 path=../escape.log\n')
        self.bind_archive()
        self.assert_rejected('invalid supplemental archive PAX record')
        name = next(iter(self.logs))
        self.write_archive(omitted=name, extra=[(name, tarfile.SYMTYPE, '../escape.log')])
        self.bind_archive()
        self.assert_rejected('unexpected supplemental archive member')

    def test_rejects_malformed_archives(self):
        data = self.payload.read_bytes()
        tar = gzip.decompress(data)
        for payload, reason in [
            (data[:-1], 'malformed supplemental gzip'),
            (gzip.compress(b'x' + tar[1:]), 'malformed supplemental tar header'),
            (gzip.compress(tar + bytes(511)), 'invalid supplemental tar archive size'),
            (gzip.compress(tar[:(2 * len(self.logs) + 4) * 512]), 'malformed supplemental tar archive ending'),
            (gzip.compress(tar + b'x' * 512), 'malformed supplemental tar archive ending'),
        ]:
            with self.subTest(reason=reason):
                self.payload.write_bytes(payload)
                self.bind_archive()
                self.assert_rejected(reason)


if __name__ == '__main__':
    unittest.main()
