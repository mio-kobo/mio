import hashlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'ci'))
import gf4_source
import trusted_seed
from seed_evidence import GF4_ASSET_NAME, REPOSITORY, WORKFLOW

SOURCE = 'a' * 40
VERSION = '2.1.0'
REF = f'refs/tags/v{VERSION}'
HOSTED = dict(GITHUB_ACTIONS='true', GITHUB_EVENT_NAME='workflow_dispatch', GITHUB_REF=REF,
              GITHUB_REPOSITORY=REPOSITORY, GITHUB_WORKFLOW_REF=f'{REPOSITORY}/{WORKFLOW}@{REF}',
              RUNNER_ENVIRONMENT='github-hosted', RUNNER_OS='macOS', RUNNER_ARCH='ARM64',
              GITHUB_SHA=SOURCE, GITHUB_RUN_ID='12', GITHUB_RUN_ATTEMPT='1')
PIN = dict(tag='gf4-fixture', release_id=42, asset_id=43)
BYTES = b'test bytes, never executed'
HASH = hashlib.sha256(BYTES).hexdigest()


class TrustedSeedTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.inputs = self.root / 'distribution-input.json'
        self.inputs.write_text(json.dumps(dict(source_sha=SOURCE, version=VERSION, embedded_bunko=False)))
        for target, value in [('native_platforms.ROOT', self.root), ('trusted_seed.GF4_SOURCE', None),
                              ('native_platforms.platform.system', 'Darwin'),
                              ('native_platforms.platform.machine', 'arm64')]:
            patcher = patch(target, return_value=value) if 'platform.' in target else patch(target, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        for target in ('trusted_seed.download_seed', 'trusted_seed.run'):
            patcher = patch(target)
            mocked = patcher.start()
            self.addCleanup(patcher.stop)
            setattr(self, target.split('.')[-1], mocked)

    def verify(self, env=HOSTED, local_seed=None):
        with tempfile.TemporaryDirectory(dir=self.root) as output, patch.dict('os.environ', env, clear=True):
            result = trusted_seed.verify(output, local_seed)
            return result, json.loads((Path(output) / 'report.json').read_text())

    def test_missing_approved_source_stays_not_run(self):
        result, report = self.verify()
        self.assertEqual(result, 2)
        self.assertEqual(report['status'], 'NOT_RUN')
        self.assertEqual(report['seed']['status'], 'NOT_RUN')
        self.download_seed.assert_not_called()
        self.run.assert_not_called()

    def test_invalid_context_never_retrieves_or_executes(self):
        for overrides in (dict(GITHUB_EVENT_NAME='pull_request'), dict(GITHUB_REF='refs/heads/Mio-2.1'),
                          dict(GITHUB_WORKFLOW_REF='unapproved'), dict(GITHUB_REPOSITORY='fork/mio'),
                          dict(RUNNER_ENVIRONMENT='self-hosted'), dict(GITHUB_SHA='b' * 40),
                          dict(GITHUB_RUN_ID=''), dict(GITHUB_RUN_ATTEMPT=''),
                          dict(RUNNER_ARCH='X64'), dict(RUNNER_OS='Linux')):
            with self.subTest(overrides=overrides):
                result, report = self.verify(dict(HOSTED, **overrides))
                self.assertEqual(result, 1)
                self.assertEqual(report['status'], 'FAIL')
        self.assertEqual(self.verify(local_seed=self.root / 'override')[0], 1)
        self.download_seed.assert_not_called()
        self.run.assert_not_called()

    def test_wrong_actual_os_or_architecture_fails_before_retrieval(self):
        for field, value in [('system', 'Linux'), ('machine', 'x86_64')]:
            with self.subTest(field=field), patch(f'native_platforms.platform.{field}', return_value=value):
                result, report = self.verify()
                self.assertEqual(result, 1)
                self.assertEqual(report['checks'][-1]['name'], 'runner-os-arch')
        self.download_seed.assert_not_called()
        self.run.assert_not_called()

    def test_missing_or_corrupt_seed_never_executes(self):
        for error, code in [(gf4_source.SeedUnavailable('missing'), 2), (ValueError('corrupt'), 1)]:
            with self.subTest(error=error), patch('trusted_seed.GF4_SOURCE', PIN):
                self.download_seed.side_effect = error
                result, report = self.verify()
                self.assertEqual(result, code)
                self.assertEqual(report['status'], 'NOT_RUN' if code == 2 else 'FAIL')
        self.run.assert_not_called()

    def test_pin_checked_before_codesign_and_execution(self):
        def retrieve(_, path):
            path.write_bytes(BYTES)

        self.download_seed.side_effect = retrieve
        self.run.side_effect = [(1, '', 'release attestation rejected', 0),
                                (0, 'release attestation verified', '', 0),
                                (1, '', 'signature rejected', 0)]
        with patch('trusted_seed.GF4_SOURCE', PIN):
            result, report = self.verify()
            self.assertEqual(result, 1)
            self.assertEqual(report['checks'][-1]['name'], 'seed-pin')
            self.run.assert_not_called()
            with patch('trusted_seed.GF4_SEED_SHA256', HASH):
                result, report = self.verify()
            self.assertEqual(result, 1)
            self.assertEqual(report['seed']['source'], PIN)
            self.assertEqual(report['checks'][-2]['status'], 'PASS')
            self.assertEqual(report['checks'][-1]['name'], 'seed-release-attestation')
            self.run.assert_called_once()
            self.assertEqual(self.run.call_args.args[2][:4], ['gh', 'release', 'verify-asset', PIN['tag']])
            with patch('trusted_seed.GF4_SEED_SHA256', HASH):
                result, report = self.verify()
            self.assertEqual(result, 1)
            self.assertEqual(report['checks'][-2]['status'], 'PASS')
            self.assertEqual(report['checks'][-1]['name'], 'seed-signature')
            self.assertEqual(self.run.call_count, 3)
            self.assertEqual(self.run.call_args.args[2][0], '/usr/bin/codesign')


class DownloadTests(unittest.TestCase):
    def release(self):
        return dict(id=42, tag_name=PIN['tag'], immutable=True, draft=False, assets=[
            dict(id=43, name=GF4_ASSET_NAME, state='uploaded', size=len(BYTES),
                 url=f'{gf4_source.API}/assets/43',
                 browser_download_url=f'https://github.com/{REPOSITORY}/releases/download/{PIN["tag"]}/{GF4_ASSET_NAME}',
                 digest='sha256:' + HASH)
        ])

    def download(self, release, payload):
        binary = io.BytesIO(payload)
        binary.status = 200
        with (tempfile.TemporaryDirectory() as output, patch('gf4_source.GF4_SEED_SHA256', HASH),
              patch('gf4_source.request', side_effect=[io.BytesIO(json.dumps(release).encode()), binary])):
            path = Path(output) / 'gf4'
            gf4_source.download_seed(PIN, path)
            return path.read_bytes(), path.stat().st_mode & 0o111

    def test_preserves_exact_bytes_without_execute_permission(self):
        self.assertEqual(self.download(self.release(), BYTES), (BYTES, 0))

    def test_incomplete_oversized_and_corrupt_downloads(self):
        for payload in [b'', BYTES[:-1], BYTES + b'!', b'x' + BYTES[1:]]:
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                self.download(self.release(), payload)

    def test_release_or_asset_identity_mismatch(self):
        for field, value in [('immutable', False), ('id', 99), ('tag_name', 'unapproved'), ('draft', True)]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.download(dict(self.release(), **{field: value}), BYTES)
        for field, value in [('name', 'wrong'), ('digest', 'sha256:' + 'f' * 64),
                             ('browser_download_url', 'https://example.org/file')]:
            release = self.release()
            release['assets'][0][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.download(release, BYTES)
        with self.assertRaises(gf4_source.SeedUnavailable):
            self.download(dict(self.release(), assets=[]), BYTES)

    @patch('gf4_source.build_opener')
    def test_only_one_approved_https_cdn_redirect(self, opener):
        origin = f'{gf4_source.API}/assets/43'
        good = f'https://{gf4_source.CDN}/github-production-release-asset/repo/asset?signature=test'
        wrong_hosts = [good.replace('https:', 'http:'), good.replace(gf4_source.CDN, 'example.org'),
                       good.replace(gf4_source.CDN, gf4_source.CDN + '@example.org')]

        def redirect(destination):
            return HTTPError(origin, 302, 'redirect', {'Location': destination}, None)

        body = io.BytesIO(BYTES)
        opener.return_value.open.side_effect = [redirect(good), body]
        self.assertIs(gf4_source.request(origin, 'application/octet-stream', allow_cdn=True), body)
        for destination in wrong_hosts:
            opener.return_value.open.side_effect = [redirect(destination)]
            with self.subTest(destination=destination), self.assertRaisesRegex(ValueError, 'unexpected'):
                gf4_source.request(origin, 'application/octet-stream', allow_cdn=True)
        opener.return_value.open.side_effect = [redirect(good), redirect(good)]
        with self.assertRaisesRegex(ValueError, 'unexpected'):
            gf4_source.request(origin, 'application/octet-stream', allow_cdn=True)
