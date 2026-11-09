import copy
import hashlib
import io
import json
import os
from pathlib import Path
import re
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
        (source / 'selfhost/resources/bunko_data.mio').write_text(
            '// generated\nwaza data_available() -> bool { kaesu hai; }\nwaza data_files() -> []File { kaesu []; }\n')
        (source / 'selfhost/cli/main.mio').write_text('mio v2.1.1-dev (pure Mio Native AOT; maintenance candidate)')
        return source

    def test_release_versions_are_built_into_source_before_compilation(self):
        for i, kwargs in enumerate(({}, dict(stable_version='2.1.1'), dict(rc_version='2.1.1-rc.1'))):
            source = self.source(str(i))
            result = prepare(source, 'a'*40, **kwargs)
            expected = kwargs.get('stable_version') or kwargs.get('rc_version') or '2.1.1-pr.'+'a'*12
            self.assertEqual(result['version'], expected)
            self.assertIn('mio v'+expected, (source/'selfhost/cli/main.mio').read_text())
            self.assertIn('a'*40, (source/'selfhost/cli/main.mio').read_text())
            self.assertTrue(result['embedded_bunko'])
            self.assertEqual(result['source_version'], '2.1.1')
            self.assertIn('kaesu hai;', (source/'selfhost/resources/bunko_data.mio').read_text())
        for invalid in ('2.1.1-rc.0', '2.1.1-rc.01', '2.1.1-rc.1/escape', '2.0.0-rc.1', '2.0.0', '2.1.1-rc'):
            with self.subTest(version=invalid), self.assertRaises(ValueError):
                prepare(self.source(invalid.replace('/', '_')), 'a'*40, rc_version=invalid)
        with self.assertRaises(ValueError):
            prepare(self.source('both'), 'a'*40, stable_version='2.1.1', rc_version='2.1.1-rc.1')
        # The source version and the requested release version must agree.
        with self.assertRaisesRegex(ValueError, 'source declares Mio 2.1.1'):
            prepare(self.source('mismatch'), 'a'*40, stable_version='2.1.2')
        with self.assertRaisesRegex(ValueError, 'source declares Mio 2.1.1'):
            prepare(self.source('mismatch-rc'), 'a'*40, rc_version='2.1.2-rc.1')

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
        self.compiler = self.root / 'mio-full'
        self.compiler.write_bytes(b'fixed point native Full compiler')
        self.core_compiler = self.root / 'mio-core'
        self.core_compiler.write_bytes(b'fixed point native Core compiler')
        self.compilers = dict(full=self.compiler, core=self.core_compiler)
        full, core = digest(self.compiler), digest(self.core_compiler)
        self.inputs = dict(source_sha='a'*40, embedded_bunko=True)
        checks = [dict(name='cli-fixed-point', status='PASS', engine='native', required=True),
                  dict(name='core-cli-fixed-point', status='PASS', engine='native', required=True)]
        checks += [dict(name=name, status='PASS', engine='native', required=True) for name in
                   ('full-embedded-runtime', 'full-embedded-transitive', 'core-no-embedded',
                    'core-external-root', 'explicit-root-precedence', 'invalid-module-diagnostic')]
        return dict(target='linux-amd64', status='PASS', source_sha='a'*40,
                    distribution_input=self.inputs,
                    run=dict(id=os.getenv('GITHUB_RUN_ID', ''), attempt=os.getenv('GITHUB_RUN_ATTEMPT', '')),
                    checks=checks,
                    hashes=dict({f'cli-{s}': full for s in 'BCD'},
                                **{f'core-cli-{s}': core for s in 'BCD'}))

    def test_gate_rejects_failed_unrun_foreign_or_wrong_compiler(self):
        report = self.report()
        gate(report, self.compilers, self.inputs, 'linux-amd64')
        for change in ('failed', 'not-run', 'go-hosted', 'source', 'target', 'hash', 'core-hash', 'run',
                       'embedded-check', 'inputs'):
            with self.subTest(change=change):
                bad = copy.deepcopy(report)
                if change == 'failed': bad['status'] = 'FAIL'
                if change == 'not-run': bad['checks'][0]['status'] = 'NOT_RUN'
                if change == 'go-hosted': bad['checks'][0]['engine'] = 'go-hosted'
                if change == 'source': bad['source_sha'] = 'b'*40
                if change == 'target': bad['target'] = 'windows-amd64'
                if change == 'hash': bad['hashes']['cli-D'] = 'b'*64
                if change == 'core-hash': bad['hashes']['core-cli-C'] = 'b'*64
                if change == 'run': bad['run']['id'] = 'another-run'
                if change == 'embedded-check': bad['checks'][2]['status'] = 'FAIL'
                if change == 'inputs':
                    with self.assertRaises(ValueError):
                        gate(report, self.compilers, dict(self.inputs, embedded_bunko=False), 'linux-amd64')
                    continue
                with self.assertRaises(ValueError):
                    gate(bad, self.compilers, self.inputs, 'linux-amd64')

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

    def test_committed_embedded_bundle_is_complete_and_exact(self):
        root = Path(__file__).resolve().parents[2]
        text = (root / 'selfhost/resources/bunko_data.mio').read_text(encoding='utf-8')
        self.assertIn('waza data_available() -> bool { kaesu hai; }', text)
        pattern = re.compile(r'path: "((?:[^"\\]|\\.)*)", version: "([^"]*)", length: (\d+), '
                             r'sha256: "([0-9a-f]+)", source: "((?:[^"\\]|\\.)*)"')
        entries = {json.loads('"'+path+'"'): (version, int(length), sha, json.loads('"'+source+'"'))
                   for path, version, length, sha, source in pattern.findall(text)}
        expected = sorted(str(path.relative_to(root / 'bunko')) for path in (root / 'bunko/kiso').rglob('*.mio')
                          if not path.name.endswith('_test.mio')
                          and not any('.' in part for part in path.relative_to(root / 'bunko').parts[:-1]))
        self.assertEqual(sorted(entries), expected)
        for name in expected:
            data = (root / 'bunko' / name).read_bytes()
            version, length, sha, source = entries[name]
            with self.subTest(name=name):
                self.assertEqual(version, 'Bunko-v2.1.1')
                self.assertEqual(length, len(data))
                self.assertEqual(sha, hashlib.sha256(data).hexdigest())
                self.assertEqual(source.encode('utf-8'), data)

    def test_packaging_both_formats_has_exact_inventory_and_no_dev_files(self):
        report = self.report()
        source = self.root / 'source'
        (source / 'selfhost/resources').mkdir(parents=True)
        (source / 'selfhost/cli').mkdir(parents=True)
        (source / 'selfhost/resources/bunko_data.mio').write_text(
            '// generated\nwaza data_available() -> bool { kaesu hai; }\nwaza data_files() -> []File { kaesu []; }\n')
        (source / 'selfhost/cli/main.mio').write_text('mio v2.1.1-dev (pure Mio Native AOT; maintenance candidate)')
        (source / 'LICENSE').write_text('license')
        for name in ('kiso/math/integer.mio', 'kiso/math/integer_test.mio', 'kiso/snap.before-patch/old.mio', 'kiso/math/test.sh'):
            p = source / 'bunko' / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text('source')
        prepare(source, self.inputs['source_sha'])
        prepared = json.loads((source / 'distribution-input.json').read_text())
        self.assertIn('kaesu hai;', (source / 'selfhost/resources/bunko_data.mio').read_text())
        for target in ('linux-amd64', 'windows-amd64'):
            report['target'] = target; report['seed'] = dict(status='NOT_RUN'); report['distribution_input'] = prepared
            rp = self.root / 'report.json'; rp.write_text(json.dumps(report))
            archives = package(source, self.compilers, rp, self.root / target, target)
            binaries = []
            for i, archive in enumerate(archives):
                dest = extract(archive, self.root / (target+str(i)))
                manifest = json.loads((dest / 'manifest.json').read_text())
                self.assertEqual(inventory(dest), manifest['files'])
                self.assertEqual(set(manifest['files']), {'bin/mio.exe' if target == 'windows-amd64' else 'bin/mio',
                                                          'LICENSE', 'README.md', 'provenance.json'})
                binaries.append((dest / 'bin' / ('mio.exe' if target == 'windows-amd64' else 'mio')))
                self.assertFalse(any('test' in n or 'before-patch' in n or n.startswith('bunko/') for n in manifest['files']))
            self.assertNotEqual(digest(binaries[0]), digest(binaries[1]))
            self.assertEqual(binaries[0].read_bytes(), b'fixed point native Core compiler')
            self.assertEqual(binaries[1].read_bytes(), b'fixed point native Full compiler')


if __name__ == '__main__': unittest.main()
