import json
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch
import xml.etree.ElementTree as ET
import zipfile

from chocolatey import build, package_files
from chocolatey_publish import publish


class ChocolateyTests(unittest.TestCase):
    def record(self, flavor='full'):
        root = f'mio-2.1.0-windows-amd64-{flavor}'
        return dict(root=root, url=f'https://github.com/mio-kobo/mio/releases/download/v2.1.0/{root}.zip', sha='a' * 64)

    def test_packages_and_inventory(self):
        with tempfile.TemporaryDirectory() as temp:
            for flavor in ('core', 'full'):
                package = build('2.1.0', flavor, self.record(flavor), Path(temp))
                original = package.read_bytes()
                self.assertEqual(build('2.1.0', flavor, self.record(flavor), Path(temp)).read_bytes(), original)
                with zipfile.ZipFile(package) as z:
                    self.assertEqual(len(z.namelist()), 5)
                    name = 'mio' if flavor == 'full' else 'mio-core'
                    ns = {'n': 'http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd'}
                    meta = ET.fromstring(z.read(name + '.nuspec')).find('n:metadata', ns)
                    self.assertEqual(meta.find('n:id', ns).text, name)
                    self.assertEqual(meta.find('n:version', ns).text, '2.1.0')
                    self.assertIsNone(meta.find('n:dependencies', ns))
                    record = json.loads(z.read('tools/release.json'))
                    self.assertEqual(record['command'], name)
                    self.assertEqual(record['sha'], 'a' * 64)
                    self.assertEqual(record['version'], '2.1.0')
                    install = z.read('tools/chocolateyInstall.ps1').decode()
                    self.assertIn('(Get-MioSha256 $archive) -ne $record.sha', install)
                    self.assertIn('[System.Security.Cryptography.SHA256]::Create()', install)
                    self.assertIn('Install-BinFile -Name $record.command -Path $exe', install)
                    self.assertIn("'.ignore'", install)
                    self.assertNotIn('LOCALAPPDATA', install)
                    self.assertIn('Uninstall-BinFile', z.read('tools/chocolateyUninstall.ps1').decode())
                with self.assertRaises(ValueError):
                    build('2.1.0', flavor, dict(self.record(flavor), sha='b' * 64), Path(temp))

    def test_reject_unstable_or_untrusted_input(self):
        for version in ('2.1.0-pr.abc123456789', '0.0.1', '2.1.0-rc.1'):
            with self.assertRaises(ValueError):
                package_files(version, 'full', self.record())
        for delta in (dict(url='https://example.com/mio.zip'), dict(sha='placeholder'), dict(root='../mio'), dict(url=self.record()['url'].replace('v2.1.0', 'latest'))):
            with self.assertRaises(ValueError):
                package_files('2.1.0', 'full', dict(self.record(), **delta))
        with self.assertRaises(ValueError):
            package_files('0.0.1', 'full', self.record(), fixture=True)

    def test_stable_entry_uses_existing_release_gate(self):
        import chocolatey
        with patch('sys.argv', ['chocolatey.py', '--tag', 'v2.1.0', '--assets', '/missing', '--release', '/release.json', '--source-sha', 'a' * 40, '--output', '/out']), patch.object(Path, 'read_text', return_value='{}'), patch('chocolatey.validate', side_effect=ValueError('seed NOT_RUN')), patch('chocolatey.build') as generate:
            with self.assertRaisesRegex(ValueError, 'NOT_RUN'):
                chocolatey.main()
            generate.assert_not_called()

    def test_publish_dry_run_duplicate_collision_and_no_secret_output(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            for name in ('mio-core', 'mio'):
                (folder / f'{name}.2.1.0.nupkg').write_bytes(name.encode())
            with patch('chocolatey_publish.subprocess.run') as run:
                publish(folder, '2.1.0', fetch=lambda n, v: None)
                run.assert_not_called()
                publish(folder, '2.1.0', push=True, fetch=lambda n, v: n.encode())
                run.assert_not_called()
                with self.assertRaises(ValueError):
                    publish(folder, '2.1.0', push=True, fetch=lambda n, v: b'different' if n == 'mio' else None)
                run.assert_not_called()
            with patch.dict('os.environ', CHOCOLATEY_API_KEY='secret-test-value'), patch('chocolatey_publish.subprocess.run') as run:
                run.return_value.returncode = 1
                with self.assertRaises(RuntimeError) as error:
                    publish(folder, '2.1.0', push=True, fetch=lambda n, v: None)
                self.assertNotIn('secret-test-value', str(error.exception))
                run.side_effect = subprocess.TimeoutExpired(['choco', '--api-key=secret-test-value'], 300)
                with self.assertRaises(RuntimeError) as error:
                    publish(folder, '2.1.0', push=True, fetch=lambda n, v: None)
                self.assertNotIn('secret-test-value', str(error.exception))
                self.assertTrue(error.exception.__suppress_context__)

    def test_fixture_matching_cannot_reuse_changed_native_inputs(self):
        from fixture import compatible, wrapper, select
        self.assertTrue(wrapper('scripts/package_managers/chocolatey/tools/chocolateyInstall.ps1'))
        for path in ('selfhost/cli/main.mio', 'scripts/distribution/package.py', 'scripts/ci/native_platforms.py', 'VERSION', '.github/workflows/native.yml', 'scripts/package_managers/windows.py', 'scripts/package_managers/release.py', 'scripts/package_managers/test_install.py'):
            self.assertFalse(wrapper(path), path)
            with patch('fixture.subprocess.check_output', return_value=path + '\n'):
                self.assertFalse(compatible('fixture-sha'))
        with patch('fixture.subprocess.check_output', return_value='scripts/package_managers/chocolatey.py\n'):
            self.assertTrue(compatible('fixture-sha'))
        valid = dict(id=1, run_attempt=2, head_sha='a' * 40, conclusion='success', path='.github/workflows/distribution-pr.yml', repository=dict(full_name='mio-kobo/mio'))
        for delta in (dict(conclusion='failure'), dict(path='.github/workflows/ci-pr.yml'), dict(repository=dict(full_name='other/fork'))):
            with patch('fixture.api', return_value=dict(valid, **delta)), self.assertRaises(RuntimeError):
                select('1')
        for expired in (True, False):
            artifacts = dict(artifacts=[dict(name='mio-candidates-windows-amd64-1-2', expired=expired)])
            with patch('fixture.api', side_effect=[valid, artifacts]), patch('fixture.subprocess.run'), patch('fixture.compatible', return_value=True):
                if expired:
                    with self.assertRaises(RuntimeError):
                        select('1')
                else:
                    self.assertEqual(select('1')['run_id'], 1)

    def test_actual_yaml_gate_compiles_and_requires_executed_checks(self):
        import yaml
        workflow = Path(__file__).resolve().parents[2] / '.github/workflows/distribution-pr.yml'
        step = yaml.safe_load(workflow.read_text())['jobs']['required']['steps'][0]['run']
        script = step.split("python - <<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]
        code = compile(script, 'distribution-required', 'exec')
        results = {name: dict(result='skipped') for name in ('summary', 'candidates', 'docker-full-offline', 'candidate-linux', 'apt-distributions')}
        results.update({name: dict(result='success') for name in ('scope', 'packaging-tests', 'wrapper-regression')})
        def gate(data):
            with patch.dict('os.environ', RESULTS=json.dumps(data)):
                exec(code, {})
        gate(results)
        for status in ('failure', 'skipped', 'cancelled', 'NOT_RUN'):
            with self.assertRaises(AssertionError):
                gate(dict(results, **{'wrapper-regression': dict(result=status)}))
        native = {name: dict(result='success') for name in results}
        native['wrapper-regression'] = dict(result='skipped')
        gate(native)
        for status in ('failure', 'skipped', 'NOT_RUN'):
            with self.assertRaises(AssertionError):
                gate(dict(native, summary=dict(result=status)))


if __name__ == '__main__':
    unittest.main()
