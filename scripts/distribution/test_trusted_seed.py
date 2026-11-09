from dataclasses import replace
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
import seed_source
import trusted_seed
from seed_evidence import (GF4, NATIVE_V21, REPOSITORY, SEED_KINDS, WORKFLOW,
                           seed_identity, validate_seed)

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


class SeedIdentityTests(unittest.TestCase):
    def test_frozen_and_native_pins(self):
        expected = {
            GF4: ('gf4-seed-v1-immutable', 437559652, 720633092, 'mio-gf4-darwin-arm64',
                  '4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da'),
            NATIVE_V21: ('bootstrap-v2.1.0-seed.1', 437632871, 720957113, 'mio-v2.1.0-darwin-arm64',
                         'be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79'),
        }
        for kind, pin in expected.items():
            with self.subTest(kind=kind):
                identity = seed_identity(kind)
                self.assertEqual((identity.tag, identity.release_id, identity.asset_id,
                                  identity.asset_name, identity.sha256), pin)
        binary = Path(__file__).resolve().parents[2] / 'bootstrap/seeds/gf4' / expected[GF4][3]
        self.assertEqual(hashlib.sha256(binary.read_bytes()).hexdigest(), expected[GF4][4])


class TrustedSeedTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.inputs = self.root / 'distribution-input.json'
        self.inputs.write_text(json.dumps(dict(source_sha=SOURCE, version=VERSION, embedded_bunko=True)))
        for target, value in [('native_platforms.ROOT', self.root), ('seed_evidence.GF4_SOURCE', None),
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

    def verify(self, env=HOSTED, local_seed=None, kind=GF4):
        with tempfile.TemporaryDirectory(dir=self.root) as output, patch.dict('os.environ', env, clear=True):
            result = trusted_seed.verify(output, local_seed, kind)
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
        for error, code in [(seed_source.SeedUnavailable('missing'), 2), (ValueError('corrupt'), 1)]:
            with self.subTest(error=error), patch('seed_evidence.GF4_SOURCE', PIN):
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
        with patch('seed_evidence.GF4_SOURCE', PIN):
            result, report = self.verify()
            self.assertEqual(result, 1)
            self.assertEqual(report['checks'][-1]['name'], 'seed-pin')
            self.run.assert_not_called()
            with patch('seed_evidence.GF4_SEED_SHA256', HASH):
                result, report = self.verify()
            self.assertEqual(result, 1)
            self.assertEqual(report['seed']['source'], PIN)
            self.assertEqual(report['checks'][-2]['status'], 'PASS')
            self.assertEqual(report['checks'][-1]['name'], 'seed-release-attestation')
            self.run.assert_called_once()
            self.assertEqual(self.run.call_args.args[2][:4], ['gh', 'release', 'verify-asset', PIN['tag']])
            with patch('seed_evidence.GF4_SEED_SHA256', HASH):
                result, report = self.verify()
            self.assertEqual(result, 1)
            self.assertEqual(report['checks'][-2]['status'], 'PASS')
            self.assertEqual(report['checks'][-1]['name'], 'seed-signature')
            self.assertEqual(self.run.call_count, 3)
            self.assertEqual(self.run.call_args.args[2][0], '/usr/bin/codesign')

    def test_native_requires_hosted_release_and_rejects_local_replay(self):
        self.assertEqual(self.verify(local_seed=self.root / 'override', kind=NATIVE_V21)[0], 1)
        self.assertEqual(self.verify(dict(HOSTED, GITHUB_EVENT_NAME='pull_request'), kind=NATIVE_V21)[0], 1)
        self.download_seed.assert_not_called()
        self.run.assert_not_called()

    def test_native_checks_pin_attestation_and_signature_before_execution(self):
        self.download_seed.side_effect = lambda _, path: path.write_bytes(BYTES)
        for rejected, checks in [('seed-pin', []),
                                 ('seed-release-attestation', ['seed-release-attestation']),
                                 ('seed-signature', ['seed-release-attestation', 'seed-signature'])]:
            self.run.reset_mock()
            self.run.side_effect = lambda report, label, argv, **kw: (int(label == rejected), '', '', 0)
            digest = seed_identity(NATIVE_V21).sha256 if rejected == 'seed-pin' else HASH
            with self.subTest(rejected=rejected), patch('seed_evidence.NATIVE_V21_SEED_SHA256', digest):
                result, report = self.verify(kind=NATIVE_V21)
                self.assertEqual(result, 1)
                self.assertEqual(report['status'], 'FAIL')
                self.assertEqual(report['checks'][-1]['name'], rejected)
                self.assertEqual([call.args[1] for call in self.run.call_args_list], checks)

    def test_native_requires_entire_chain_and_both_cross_comparisons(self):
        self.download_seed.side_effect = lambda _, path: path.write_bytes(BYTES)

        def run(report, label, argv, **kwargs):
            (report.logs / (label + '.log')).write_text(label)
            if label == 'seed-release-attestation':
                self.assertEqual(argv[:4], ['gh', 'release', 'verify-asset', seed_identity(NATIVE_V21).tag])
            if label.startswith(('seed-build-', 'cli-', 'cross-')):
                self.assertIn('/usr/bin/sandbox-exec', report.exec_prefix)
                self.assertIn('GH_TOKEN', report.exec_prefix)
                self.assertEqual(Path(argv[0]).name, expected_builders[label])
                Path(argv[argv.index('-o') + 1]).write_bytes(label.encode() if label == unequal else b'compiler')
            return 0, '', '', 0

        expected_builders = {'seed-build-A': seed_identity(NATIVE_V21).asset_name, 'cli-B': 'mio-A', 'cli-C': 'mio-B', 'cli-D': 'mio-C',
                             **{f'cross-{s}-{t}': f'mio-{s}' for s in 'BC'
                                for t in ('linux-amd64', 'windows-amd64')}}
        self.run.side_effect = run
        with (patch('seed_evidence.NATIVE_V21_SEED_SHA256', HASH), patch('native_platforms.run', self.run)):
            for unequal, failed in [('cli-D', 'cli-fixed-point'), ('cross-C-linux-amd64', 'cross-stage-a linux-amd64'),
                                    ('cross-C-windows-amd64', 'cross-stage-a windows-amd64'), (None, None)]:
                with self.subTest(unequal=unequal):
                    self.run.reset_mock()
                    result, report = self.verify(kind=NATIVE_V21)
                    self.assertEqual(result, int(failed is not None))
                    self.assertEqual(report['suite'], 'trusted-native-v21-distribution')
                    if failed:
                        self.assertEqual(report['checks'][-1]['name'], failed)
                        self.assertEqual(report['status'], 'FAIL')
                    else:
                        self.assertEqual([c.args[1] for c in self.run.call_args_list], [
                            'seed-release-attestation', 'seed-signature', 'seed-build-A', 'cli-B', 'cli-C', 'cli-D',
                            'cross-B-linux-amd64', 'cross-C-linux-amd64', 'cross-B-windows-amd64', 'cross-C-windows-amd64',
                        ])
                        validate_seed(report, SOURCE, VERSION, report['run'], NATIVE_V21)
                        with patch('seed_evidence.GF4_SOURCE', PIN), self.assertRaises(ValueError):
                            validate_seed(report, SOURCE, VERSION, report['run'], GF4)


class DownloadTests(unittest.TestCase):
    def release(self, identity, asset=None):
        asset = asset or identity
        return dict(id=identity.release_id, tag_name=identity.tag, immutable=True, draft=False, assets=[
            dict(id=asset.asset_id, name=asset.asset_name, state='uploaded', size=len(BYTES),
                 url=f'{seed_source.API}/assets/{asset.asset_id}',
                 browser_download_url=f'https://github.com/{REPOSITORY}/releases/download/{identity.tag}/{asset.asset_name}',
                 digest='sha256:' + asset.sha256)
        ])

    def download(self, identity, release, payload):
        binary = io.BytesIO(payload)
        binary.status = 200
        with (tempfile.TemporaryDirectory() as output,
              patch('seed_source.request', side_effect=[io.BytesIO(json.dumps(release).encode()), binary])):
            path = Path(output) / identity.asset_name
            seed_source.download_seed(identity, path)
            return path.read_bytes(), path.stat().st_mode & 0o111

    def test_preserves_exact_bytes_without_execute_permission(self):
        for kind in SEED_KINDS:
            identity = replace(seed_identity(kind), sha256=HASH)
            with self.subTest(kind=kind):
                self.assertEqual(self.download(identity, self.release(identity), BYTES), (BYTES, 0))

    def test_incomplete_oversized_and_corrupt_downloads(self):
        for kind in SEED_KINDS:
            identity = replace(seed_identity(kind), sha256=HASH)
            for payload in [b'', BYTES[:-1], BYTES + b'!', b'x' + BYTES[1:]]:
                with self.subTest(kind=kind, payload=payload), self.assertRaises(ValueError):
                    self.download(identity, self.release(identity), payload)

    def test_release_or_asset_identity_mismatch(self):
        for kind, other_kind in ((GF4, NATIVE_V21), (NATIVE_V21, GF4)):
            identity, other = seed_identity(kind), seed_identity(other_kind)
            for field, value in [('immutable', False), ('id', other.release_id),
                                 ('tag_name', other.tag), ('draft', True)]:
                with self.subTest(kind=kind, field=field), self.assertRaises(ValueError):
                    self.download(identity, dict(self.release(identity), **{field: value}), BYTES)
            for field, value in [('name', other.asset_name), ('digest', 'sha256:' + other.sha256),
                                 ('browser_download_url', 'https://example.org/file')]:
                release = self.release(identity)
                release['assets'][0][field] = value
                with self.subTest(kind=kind, field=field), self.assertRaises(ValueError):
                    self.download(identity, release, BYTES)
            for assets in ([], self.release(identity, asset=other)['assets']):
                with self.subTest(kind=kind, assets=assets), self.assertRaises(seed_source.SeedUnavailable):
                    self.download(identity, dict(self.release(identity), assets=assets), BYTES)
            # Matching metadata still cannot authorize substitute executable bytes.
            with self.assertRaisesRegex(ValueError, 'pinned SHA-256'):
                self.download(identity, self.release(identity), BYTES)

    @patch('seed_source.build_opener')
    def test_only_one_approved_https_cdn_redirect(self, opener):
        origin = f'{seed_source.API}/assets/43'
        good = f'https://{seed_source.CDN}/github-production-release-asset/repo/asset?signature=test'
        wrong_hosts = [good.replace('https:', 'http:'), good.replace(seed_source.CDN, 'example.org'),
                       good.replace(seed_source.CDN, seed_source.CDN + '@example.org')]

        def redirect(destination):
            return HTTPError(origin, 302, 'redirect', {'Location': destination}, None)

        body = io.BytesIO(BYTES)
        opener.return_value.open.side_effect = [redirect(good), body]
        self.assertIs(seed_source.request(origin, 'application/octet-stream', allow_cdn=True), body)
        for destination in wrong_hosts:
            opener.return_value.open.side_effect = [redirect(destination)]
            with self.subTest(destination=destination), self.assertRaisesRegex(ValueError, 'unexpected'):
                seed_source.request(origin, 'application/octet-stream', allow_cdn=True)
        opener.return_value.open.side_effect = [redirect(good), redirect(good)]
        with self.assertRaisesRegex(ValueError, 'unexpected'):
            seed_source.request(origin, 'application/octet-stream', allow_cdn=True)
