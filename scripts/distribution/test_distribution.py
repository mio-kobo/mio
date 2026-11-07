import copy
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import tarfile
import unittest
import zipfile

from package import digest, extract, gate, inventory, package
from prepare import copy_committed_source, prepare, EMPTY


class DistributionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def source(self, name):
        source = self.root / name
        (source / 'selfhost/resources').mkdir(parents=True)
        (source / 'selfhost/cli').mkdir(parents=True)
        (source / 'selfhost/resources/bunko_data.mio').write_text('legacy importable source')
        (source / 'selfhost/cli/main.mio').write_text('mio v2.0.0-dev (pure Mio Native AOT; GF3 candidate)')
        return source

    def test_release_versions_are_built_into_source_before_compilation(self):
        for i, kwargs in enumerate(({}, dict(stable_version='2.1.0'), dict(rc_version='2.1.0-rc.1'))):
            source = self.source(str(i))
            result = prepare(source, 'a'*40, **kwargs)
            expected = kwargs.get('stable_version') or kwargs.get('rc_version') or '2.1.0-pr.'+'a'*12
            self.assertEqual(result['version'], expected)
            self.assertIn('mio v'+expected, (source/'selfhost/cli/main.mio').read_text())
            self.assertIn('a'*40, (source/'selfhost/cli/main.mio').read_text())
        for invalid in ('2.1.0', '2.1.0-rc.0', '2.1.0-rc.01', '2.1.0-rc.1/escape', '2.0.0-rc.1'):
            with self.subTest(version=invalid), self.assertRaises(ValueError):
                prepare(self.source(invalid.replace('/', '_')), 'a'*40, rc_version=invalid)
        with self.assertRaises(ValueError):
            prepare(self.source('both'), 'a'*40, stable_version='2.1.0', rc_version='2.1.0-rc.1')

    def test_stable_source_uses_canonical_bytes_even_in_a_crlf_checkout(self):
        source = self.source('source-checkout')
        resource = source / 'selfhost/resources/bunko_data.mio'
        resource.write_bytes(b'committed line\n')
        subprocess.run(['git', 'init', '-q', str(source)], check=True)
        subprocess.run(['git', 'config', 'core.autocrlf', 'true'], cwd=source, check=True)
        subprocess.run(['git', 'add', '.'], cwd=source, check=True, capture_output=True)
        subprocess.run(['git', '-c', 'user.name=Mio Test', '-c', 'user.email=test@example.invalid',
                        '-c', 'commit.gpgsign=false', 'commit', '-qm', 'fixture'], cwd=source, check=True)
        revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=source, text=True).strip()
        resource.write_bytes(b'dirty checkout\r\n')
        destination = self.root / 'prepared-stable'
        copy_committed_source(source, revision, destination)
        self.assertEqual((destination / resource.relative_to(source)).read_bytes(), b'committed line\n')

    def test_rc_dispatch_ref_is_bound_before_any_source_copy(self):
        for event, ref in (('pull_request', 'refs/tags/v2.1.0-rc.1'),
                           ('workflow_dispatch', 'refs/heads/Mio-2.1'),
                           ('workflow_dispatch', 'refs/tags/v2.1.0-rc.2')):
            destination = self.root/'must-not-exist'
            env = dict(os.environ, GITHUB_ACTIONS='true', GITHUB_EVENT_NAME=event, GITHUB_REF=ref,
                       MIO_DISTRIBUTION_STABLE_VERSION='', MIO_DISTRIBUTION_RC_VERSION='')
            result = subprocess.run([sys.executable, str(Path(__file__).with_name('prepare.py')),
                                     '--rc-version', '2.1.0-rc.1', '--disposable-checkout', str(destination)],
                                    env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('exact pre-existing tag', result.stderr)
            self.assertFalse(destination.exists())

    def test_rc_windows_installer_retains_version_and_safe_numeric_metadata(self):
        sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'package_managers'))
        from windows import installer_script
        result = installer_script('payload', 'out', '2.1.0-rc.1', 'full', 'setup')
        self.assertIn('AppVersion=2.1.0-rc.1', result)
        self.assertIn('VersionInfoVersion=2.1.0\n', result)
        with self.assertRaises(ValueError):
            installer_script('payload', 'out', '2.1.0-rc.1\n[Code]', 'full', 'setup')

    def report(self):
        import os
        self.compiler = self.root / 'mio'
        self.compiler.write_bytes(b'fixed point native compiler')
        sha = digest(self.compiler)
        self.inputs = dict(source_sha='a'*40, embedded_bunko=False)
        return dict(target='linux-amd64', status='PASS', source_sha='a'*40,
                    run=dict(id=os.getenv('GITHUB_RUN_ID', ''), attempt=os.getenv('GITHUB_RUN_ATTEMPT', '')),
                    checks=[dict(name='cli-fixed-point', status='PASS', engine='native', required=True)],
                    hashes={f'cli-{s}': sha for s in 'BCD'})

    def test_gate_rejects_failed_unrun_foreign_or_wrong_compiler(self):
        report = self.report()
        gate(report, self.compiler, self.inputs, 'linux-amd64')
        for change in ('failed', 'not-run', 'go-hosted', 'source', 'target', 'hash', 'run'):
            with self.subTest(change=change):
                bad = copy.deepcopy(report)
                if change == 'failed': bad['status'] = 'FAIL'
                if change == 'not-run': bad['checks'][0]['status'] = 'NOT_RUN'
                if change == 'go-hosted': bad['checks'][0]['engine'] = 'go-hosted'
                if change == 'source': bad['source_sha'] = 'b'*40
                if change == 'target': bad['target'] = 'windows-amd64'
                if change == 'hash': bad['hashes']['cli-D'] = 'b'*64
                if change == 'run': bad['run']['id'] = 'another-run'
                with self.assertRaises(ValueError):
                    gate(bad, self.compiler, self.inputs, 'linux-amd64')

    def test_unsafe_archives_never_write_outside_destination(self):
        for names in (['../escape'], ['/escape'], ['C:/escape'], ['x\\escape'], ['pkg/a', 'pkg/a'],
                      ['pkg/A', 'pkg/a'], ['pkg/a', 'pkg/a/b']):
            for kind in ('zip', 'tar.gz'):
                with self.subTest(names=names, kind=kind):
                    path = self.root / ('bad.'+kind)
                    if kind == 'zip':
                        with zipfile.ZipFile(path, 'w') as z:
                            for name in names: z.writestr(name, 'unsafe')
                    else:
                        with tarfile.open(path, 'w:gz') as t:
                            for name in names:
                                info = tarfile.TarInfo(name); info.size = 6
                                t.addfile(info, io.BytesIO(b'unsafe'))
                    with self.assertRaises(ValueError): extract(path, self.root / 'out')
                    self.assertFalse((self.root / 'out').exists())
                    self.assertFalse((self.root / 'escape').exists())

    def test_links_are_rejected(self):
        path = self.root / 'bad.tar.gz'
        with tarfile.open(path, 'w:gz') as t:
            info = tarfile.TarInfo('pkg/link'); info.type = tarfile.SYMTYPE; info.linkname = '/tmp'
            t.addfile(info)
        with self.assertRaises(ValueError): extract(path, self.root / 'out')

    def test_packaging_both_formats_has_exact_inventory_and_no_dev_files(self):
        report = self.report()
        source = self.root / 'source'
        (source / 'selfhost/resources').mkdir(parents=True)
        (source / 'selfhost/cli').mkdir(parents=True)
        (source / 'selfhost/resources/bunko_data.mio').write_text('legacy importable source')
        (source / 'selfhost/cli/main.mio').write_text('mio v2.0.0-dev (pure Mio Native AOT; GF3 candidate)')
        (source / 'LICENSE').write_text('license')
        for name in ('kiso/math/integer.mio', 'kiso/math/integer_test.mio', 'kiso/snap.before-patch/old.mio', 'kiso/math/test.sh'):
            p = source / 'bunko' / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text('source')
        prepare(source, self.inputs['source_sha'])
        self.assertEqual((source / 'selfhost/resources/bunko_data.mio').read_text(), EMPTY)
        for target in ('linux-amd64', 'windows-amd64'):
            report['target'] = target; report['seed'] = dict(status='NOT_RUN')
            rp = self.root / 'report.json'; rp.write_text(json.dumps(report))
            archives = package(source, self.compiler, rp, self.root / target, target)
            binaries = []
            for i, archive in enumerate(archives):
                dest = extract(archive, self.root / (target+str(i)))
                manifest = json.loads((dest / 'manifest.json').read_text())
                self.assertEqual(inventory(dest), manifest['files'])
                binaries.append(next((dest / 'bin').iterdir()))
                self.assertFalse(any('test' in n or 'before-patch' in n for n in manifest['files']))
                self.assertEqual((dest / 'bunko/kiso/math/integer.mio').exists(), i == 1)
            self.assertEqual(digest(binaries[0]), digest(binaries[1]))


if __name__ == '__main__': unittest.main()
