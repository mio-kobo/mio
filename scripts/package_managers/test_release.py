import json
import copy
import subprocess
from unittest.mock import patch
from pathlib import Path
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parent))
from release import stable, immutable, formula, winget, validate, windows_product_code
from package import digest, inventory, write_json
from seed_evidence import GF4_SEED_SHA256, TARGETS, seed_binding
from stable_release import BUNDLE, REPORT, SEED_REPORT, authenticate, docker_archive, evidence_files

SOURCE = 'a' * 40
VERSION = '2.1.0'
RUN = dict(id='123', attempt='1')
INPUTS = dict(version=VERSION, source_sha=SOURCE, embedded_bunko=False)


class ReleaseTests(unittest.TestCase):
    def test_candidates_and_unstable_tags_rejected(self):
        for tag in ['2.1.0', 'v2.1.0-pr.abc', 'v2.1.0-rc.1', 'v2.1.0-dev', 'v2.0.0', 'v2.01.0', 'v2.1.00', '../v2.1.0']:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                stable(tag)
        self.assertEqual(stable('v2.1.0'), '2.1.0')

    def test_immutable_idempotent_and_rejects_replacement(self):
        with tempfile.TemporaryDirectory() as temp:
            p = Path(temp) / 'version/file'
            immutable(p, b'one')
            immutable(p, b'one')
            with self.assertRaises(ValueError):
                immutable(p, b'two')
            self.assertEqual(p.read_bytes(), b'one')

    def test_draft_prerelease_unpublished_and_wrong_release_rejected(self):
        for patch in [dict(draft=True), dict(prerelease=True), dict(published_at=None), dict(tag_name='v2.1.0-rc.1')]:
            r = dict(tag_name='v2.1.0', draft=False, prerelease=False, published_at='date')
            r.update(patch)
            with self.assertRaises(ValueError):
                validate(Path('/no/assets'), 'v2.1.0', r, SOURCE)

    def fixture(self, base):
        import tarfile
        import zipfile
        release = dict(tag_name='v2.1.0', draft=False, prerelease=False, published_at='date', assets=[])
        compiler_sha = __import__('hashlib').sha256(b'native fixture').hexdigest()
        names = ['runner-os-arch', 'seed-pin', 'seed-build-A', 'cli-fixed-point',
                 *['cli-stage-' + stage for stage in 'BCD'],
                 *['cross-stage-a ' + target for target in TARGETS if target != 'darwin-arm64']]
        seed = dict(schema_version=1, suite='trusted-gf4-distribution', status='PASS', target='darwin-arm64',
                    source_sha=SOURCE, distribution_input=INPUTS, run=RUN,
                    seed=dict(status='PASS', sha256=GF4_SEED_SHA256),
                    checks=[dict(name=name, status='PASS', required=True) for name in names],
                    hashes={**{f'cli-{stage}': compiler_sha for stage in 'ABCD'},
                            **{f'stage-a-{target}': compiler_sha for target in TARGETS}})
        for check in seed['checks']:
            if check['name'].startswith('cross-stage-a '):
                check.update(B=compiler_sha, C=compiler_sha)
        seed_path = base / SEED_REPORT
        write_json(seed_path, seed)
        sums = []
        for target in ['darwin-arm64', 'linux-amd64', 'windows-amd64']:
            report = dict(target=target, status='PASS', source_sha='a'*40,
                          seed=dict(status='NOT_RUN', sha256=GF4_SEED_SHA256), distribution_input=INPUTS, run=RUN,
                          hashes={f'cli-{s}': compiler_sha for s in 'BCD'},
                          checks=[dict(name='cli-fixed-point', status='PASS', engine='native', required=True)])
            report_path = base / (target+'-native-report.json')
            write_json(report_path, report)
            archives = {}
            for flavor in ['core', 'full']:
                name = f'mio-2.1.0-{target}-{flavor}'
                root = base / name
                (root / 'bin').mkdir(parents=True)
                binary = root / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')
                binary.write_bytes(b'native fixture')
                (root / 'LICENSE').write_text('MIT fixture')
                (root / 'README.md').write_text('test fixture only')
                if flavor == 'full':
                    (root / 'bunko/kiso').mkdir(parents=True)
                    (root / 'bunko/kiso/math.mio').write_text('fixture')
                provenance = dict(version='2.1.0', target=target, flavor=flavor, source_sha='a'*40, embedded_bunko=False,
                                  compiler_sha256=compiler_sha, native_report_sha256=digest(report_path),
                                  trusted_seed=seed_binding(seed, report, INPUTS, target, compiler_sha, digest(seed_path)))
                write_json(root / 'provenance.json', provenance)
                write_json(root / 'manifest.json', dict(version='2.1.0', source_sha='a'*40, target=target, flavor=flavor, files=inventory(root)))
                archive = base / (name + ('.zip' if target == 'windows-amd64' else '.tar.gz'))
                files = [p for p in root.rglob('*') if p.is_file()]
                if target == 'windows-amd64':
                    with zipfile.ZipFile(archive, 'w') as z:
                        for p in files:
                            z.write(p, p.relative_to(base))
                else:
                    with tarfile.open(archive, 'w:gz') as t:
                        for p in files:
                            t.add(p, arcname=p.relative_to(base), recursive=False)
                sha = digest(archive)
                archives[archive.name] = sha
                sums.append(sha+'  '+archive.name+'\n')
                release['assets'].append(dict(name=archive.name, digest='sha256:'+sha,
                    browser_download_url=f'https://github.com/mio-kobo/mio/releases/download/v2.1.0/{archive.name}'))
            write_json(base / (target+'-verification.json'), dict(target=target, status='PASS', checks=[dict(status='PASS')], archives=archives))
        for flavor in ('core', 'full'):
            filename = f'mio-2.1.0-windows-amd64-{flavor}-setup.exe'
            (base / filename).write_bytes(b'unit fixture only; not executable')
            sha = digest(base / filename)
            sums.append(sha+'  '+filename+'\n')
            release['assets'].append(dict(name=filename, digest='sha256:'+sha, browser_download_url=f'https://github.com/mio-kobo/mio/releases/download/v2.1.0/{filename}'))
        image = docker_archive(VERSION)
        (base / image).write_bytes(b'Docker test fixture')
        sums.append(f'{digest(base / image)}  {image}\n')
        (base / 'SHA256SUMS').write_text(''.join(sums))
        linux_full = next(a['digest'][7:] for a in release['assets'] if a['name'].endswith('linux-amd64-full.tar.gz'))
        write_json(base / 'docker-verification.json', dict(status='PASS', source_sha='a'*40, full_archive_sha256=linux_full, archive_sha256=digest(base / image), checks=[dict(status='PASS')]))
        return release

    @patch('release.authenticate', return_value=RUN)
    def test_six_asset_gate_rejects_tampering_and_missing_evidence(self, _authenticate):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            release = self.fixture(base)
            version, records = validate(base, 'v2.1.0', release, SOURCE)
            self.assertEqual(version, '2.1.0')
            self.assertEqual(len(records), 8)
            bad = copy.deepcopy(release)
            bad['assets'][0]['digest'] = 'sha256:' + '0'*64
            with self.assertRaises(ValueError):
                validate(base, 'v2.1.0', bad, SOURCE)
            original_digest = digest
            report_path = base / 'darwin-arm64-native-report.json'
            original_report = report_path.read_bytes()
            for change in ['wrong-run', 'wrong-input', 'fixed-not-run', 'hash', 'source']:
                report = json.loads(original_report)
                if change == 'wrong-run': report['run']['attempt'] = '2'
                if change == 'wrong-input': report['distribution_input']['embedded_bunko'] = True
                if change == 'fixed-not-run': report['checks'][0]['status'] = 'NOT_RUN'
                if change == 'hash': report['hashes']['cli-C'] = '0'*64
                if change == 'source': report['source_sha'] = 'b'*40
                write_json(report_path, report)
                # Rebinding only the evidence file hash isolates deeper gates.
                def rebound(path):
                    return original_digest_bytes if path == report_path else original_digest(path)
                original_digest_bytes = __import__('hashlib').sha256(original_report).hexdigest()
                with patch('release.digest', side_effect=rebound), self.assertRaises(ValueError):
                    validate(base, 'v2.1.0', release, SOURCE)
            report_path.write_bytes(original_report)
            evidence = base / 'darwin-arm64-verification.json'
            original = evidence.read_bytes()
            data = json.loads(original)
            for status in ['FAIL', 'NOT_RUN', 'SKIP', 'NOT_APPLICABLE']:
                data['checks'][0]['status'] = status
                write_json(evidence, data)
                with self.assertRaises(ValueError):
                    validate(base, 'v2.1.0', release, SOURCE)
            evidence.write_bytes(original)
            archive = base / records['darwin-arm64', 'core']['filename']
            archive.write_bytes(archive.read_bytes()+b'tamper')
            with self.assertRaises(ValueError):
                validate(base, 'v2.1.0', release, SOURCE)

    def test_seed_binding_rejects_stale_or_unrelated_origin(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            self.fixture(base)
            seed = json.loads((base / SEED_REPORT).read_text())
            report = json.loads((base / 'linux-amd64-native-report.json').read_text())
            compiler = report['hashes']['cli-B']
            for field, value in [
                ('source_sha', 'b' * 40),
                ('target', 'linux-amd64'),
                ('distribution_input', dict(INPUTS, version='2.1.1')),
                ('seed', dict(status='NOT_RUN', sha256=GF4_SEED_SHA256)),
                ('seed', dict(status='PASS', sha256='b' * 64)),
                ('run', dict(RUN, attempt='2')),
                ('hashes', dict(seed['hashes'], **{'stage-a-linux-amd64': 'b' * 64})),
                ('hashes', dict(seed['hashes'], **{'cli-D': 'b' * 64})),
            ]:
                with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                    seed_binding(dict(seed, **{field: value}), report, INPUTS, 'linux-amd64', compiler, 'c' * 64)

    @patch('stable_release.subprocess.run')
    def test_authentication_checks_signer_source_and_all_evidence_hashes(self, gh):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            for name in evidence_files(VERSION):
                (base / name).write_bytes(b'test evidence')
            write_json(base / REPORT, dict(schema_version=1, status='PASS', source_sha=SOURCE,
                                           version=VERSION, run=RUN,
                                           files={name: digest(base / name) for name in evidence_files(VERSION)}))
            self.assertEqual(authenticate(base, 'v2.1.0', SOURCE), RUN)
            args = gh.call_args.args[0]
            self.assertIn(str(base / BUNDLE), args)
            self.assertEqual(args[args.index('--source-ref') + 1], 'refs/tags/v2.1.0')
            self.assertEqual(args[args.index('--signer-digest') + 1], SOURCE)
            self.assertEqual(args[args.index('--source-digest') + 1], SOURCE)
            self.assertIn('--deny-self-hosted-runners', args)
            (base / SEED_REPORT).write_bytes(b'fabricated evidence')
            with self.assertRaisesRegex(ValueError, 'does not match signed'):
                authenticate(base, 'v2.1.0', SOURCE)
            gh.side_effect = subprocess.CalledProcessError(1, 'gh')
            with self.assertRaisesRegex(ValueError, 'missing or invalid'):
                authenticate(base, 'v2.1.0', SOURCE)

    def test_layout_conflicts_and_distinct_windows_aliases(self):
        r = dict(url='https://github.com/mio-kobo/mio/releases/download/v2.1.0/a.zip', sha='a'*64, root='mio-2.1.0-windows-amd64-full')
        aliases = []
        for flavor in ['core', 'full']:
            f = formula('2.1.0', flavor, r)
            self.assertIn('libexec.install', f)
            self.assertIn('bin.install_symlink libexec/"bin/mio"', f)
            self.assertIn('conflicts_with', f)
            ident, files = winget('2.1.0', flavor, r)
            manifest = files['installer.yaml']
            self.assertEqual(manifest['InstallerType'], 'inno')
            from windows import installer_script
            product_code = manifest['ProductCode']
            self.assertEqual(product_code, windows_product_code(flavor))
            self.assertIn('AppId={' + product_code.removesuffix('_is1'), installer_script('payload', 'output', '2.1.0', flavor, 'setup'))
            self.assertNotIn('NestedInstallerFiles', manifest)
            installer = manifest['Installers'][0]
            self.assertEqual(installer['InstallerSha256'], 'A'*64)
            aliases.append(manifest['Commands'][0])
        self.assertEqual(aliases, ['mio-core', 'mio'])

if __name__ == '__main__':
    unittest.main()
