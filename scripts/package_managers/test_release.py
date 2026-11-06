import json
import copy
from unittest.mock import patch
from pathlib import Path
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parent))
from release import stable, immutable, formula, winget, validate, GF4_SEED_SHA256
from package import digest, inventory, write_json


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
                validate(Path('/no/assets'), 'v2.1.0', r)

    def fixture(self, base):
        import tarfile
        import zipfile
        release = dict(tag_name='v2.1.0', draft=False, prerelease=False, published_at='date', assets=[])
        sums = []
        for target in ['darwin-arm64', 'linux-amd64', 'windows-amd64']:
            hashes = []
            compiler_sha = __import__('hashlib').sha256(b'native fixture').hexdigest()
            report = dict(target=target, status='PASS', source_sha='a'*40,
                          seed=dict(status='PASS', sha256=GF4_SEED_SHA256), hashes={f'cli-{s}': compiler_sha for s in 'BCD'},
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
                                  compiler_sha256=compiler_sha, native_report_sha256=digest(report_path))
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
        (base / 'SHA256SUMS').write_text(''.join(sums))
        linux_full = next(a['digest'][7:] for a in release['assets'] if a['name'].endswith('linux-amd64-full.tar.gz'))
        write_json(base / 'docker-verification.json', dict(status='PASS', source_sha='a'*40, full_archive_sha256=linux_full, checks=[dict(status='PASS')]))
        return release

    def test_six_asset_gate_rejects_tampering_and_missing_evidence(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            release = self.fixture(base)
            version, records = validate(base, 'v2.1.0', release)
            self.assertEqual(version, '2.1.0')
            self.assertEqual(len(records), 6)
            bad = copy.deepcopy(release)
            bad['assets'][0]['digest'] = 'sha256:' + '0'*64
            with self.assertRaises(ValueError):
                validate(base, 'v2.1.0', bad)
            original_digest = digest
            report_path = base / 'darwin-arm64-native-report.json'
            original_report = report_path.read_bytes()
            for change in ['seed-not-run', 'seed-hash', 'fixed-not-run', 'hash', 'source']:
                report = json.loads(original_report)
                if change == 'seed-not-run': report['seed']['status'] = 'NOT_RUN'
                if change == 'seed-hash': report['seed']['sha256'] = '0'*64
                if change == 'fixed-not-run': report['checks'][0]['status'] = 'NOT_RUN'
                if change == 'hash': report['hashes']['cli-C'] = '0'*64
                if change == 'source': report['source_sha'] = 'b'*40
                write_json(report_path, report)
                # Rebinding only the evidence file hash isolates deeper gates.
                def rebound(path):
                    return original_digest_bytes if path == report_path else original_digest(path)
                original_digest_bytes = __import__('hashlib').sha256(original_report).hexdigest()
                with patch('release.digest', side_effect=rebound), self.assertRaises(ValueError):
                    validate(base, 'v2.1.0', release)
            report_path.write_bytes(original_report)
            evidence = base / 'darwin-arm64-verification.json'
            original = evidence.read_bytes()
            data = json.loads(original)
            for status in ['FAIL', 'NOT_RUN', 'SKIP', 'NOT_APPLICABLE']:
                data['checks'][0]['status'] = status
                write_json(evidence, data)
                with self.assertRaises(ValueError):
                    validate(base, 'v2.1.0', release)
            evidence.write_bytes(original)
            archive = base / records['darwin-arm64', 'core']['filename']
            archive.write_bytes(archive.read_bytes()+b'tamper')
            with self.assertRaises(ValueError):
                validate(base, 'v2.1.0', release)

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
            self.assertEqual(manifest['NestedInstallerType'], 'portable')
            installer = manifest['Installers'][0]
            self.assertEqual(installer['InstallerSha256'], 'A'*64)
            self.assertEqual(installer['NestedInstallerFiles'][0]['RelativeFilePath'], r['root']+'/bin/mio.exe')
            aliases.append(installer['NestedInstallerFiles'][0]['PortableCommandAlias'])
        self.assertEqual(aliases, ['mio-core', 'mio'])

if __name__ == '__main__':
    unittest.main()
