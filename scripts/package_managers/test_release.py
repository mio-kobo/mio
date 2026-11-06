import json
from pathlib import Path
import sys
import tempfile
import unittest
sys.path.insert(0, str(Path(__file__).resolve().parent))
from release import stable, immutable, formula, winget, validate


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
