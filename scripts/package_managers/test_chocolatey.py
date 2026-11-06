import json
from pathlib import Path
import tempfile
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
                    self.assertIn('Get-FileHash $archive -Algorithm SHA256', install)
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
        with patch('sys.argv', ['chocolatey.py', '--tag', 'v2.1.0', '--assets', '/missing', '--release', '/release.json', '--output', '/out']), patch.object(Path, 'read_text', return_value='{}'), patch('chocolatey.validate', side_effect=ValueError('seed NOT_RUN')), patch('chocolatey.build') as generate:
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


if __name__ == '__main__':
    unittest.main()
