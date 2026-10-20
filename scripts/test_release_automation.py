"""Regression tests for release boundaries, using fake transport only."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import release_automation as release


class PolicyTests(unittest.TestCase):
    def test_versions(self):
        for tag in ['v2.0.0', 'v2.0.1', 'v2.1.0', 'v2.2.0', 'v2.23.14']:
            self.assertFalse(release.parse_tag(tag)['prerelease'])
            self.assertTrue(release.parse_tag(tag + '-rc.5')['prerelease'])
        self.assertEqual(release.parse_tag('v2.0.0-rc.5')['version'], '2.0.0-rc.5')

    def test_invalid_tags(self):
        for tag in ['v1.0.0', 'v3.0.0', '2.0.0', 'v2.0', 'v2.00.0', 'v2.0.00',
                    'v2.0.0-rc.0', 'v2.0.0-rc.05', 'v2.0.0-rc5', 'v2.0.0-beta.1',
                    'v2.0.0+build', 'v2.0.0\n', 'v2.0.0;echo bad', 'v2.０.0']:
            with self.subTest(tag=tag), self.assertRaises(RuntimeError):
                release.parse_tag(tag)

    def test_source_metadata_and_notes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'cmd/mio').mkdir(parents=True)
            (root / 'docs/releases').mkdir(parents=True)
            (root / 'VERSION').write_text('2.1.0\n')
            cli = root / 'cmd/mio/main.go'
            cli.write_text('var version = "2.1.0"\n')
            notes = root / 'docs/releases/v2.1.0.md'
            with self.assertRaisesRegex(RuntimeError, 'notes missing'):
                release.metadata('v2.1.0-rc.1', root)
            notes.write_text('# Release 2.1.0')
            self.assertEqual(release.metadata('v2.1.0-rc.1', root)['notes'], 'docs/releases/v2.1.0.md')
            with self.assertRaisesRegex(RuntimeError, 'VERSION differs'):
                release.metadata('v2.2.0', root)
            cli.write_text('var version = "2.0.0"\n')
            with self.assertRaisesRegex(RuntimeError, 'CLI source'):
                release.metadata('v2.1.0', root)

    def test_workflow_contract(self):
        release.workflow_policy()

    def test_existing_release_and_api_failure_stop(self):
        with patch.object(release, 'api', return_value={'draft': True}), self.assertRaisesRegex(RuntimeError, 'already exists'):
            release.absent('v2.0.0')
        with patch.object(release, 'api', side_effect=RuntimeError('HTTP 403')), self.assertRaisesRegex(RuntimeError, '403'):
            release.absent('v2.0.0')

    def test_workflow_revision_requires_default_branch_review(self):
        for sha, passes in [('same', True), ('different', False)]:
            with patch.object(release, 'api', side_effect=[{'default_branch': 'main'},
                    [{'name': 'release.yml', 'sha': 'same'}], [{'name': 'release.yml', 'sha': sha}]]):
                if passes:
                    release.workflow_baseline('commit')
                else:
                    with self.assertRaisesRegex(RuntimeError, 'default branch'):
                        release.workflow_baseline('commit')

    def test_existing_draft_stops_retry(self):
        with patch.object(release, 'api', side_effect=[None, [{'tag_name': 'v2.0.0', 'draft': True}]]):
            with self.assertRaisesRegex(RuntimeError, 'including drafts'):
                release.absent('v2.0.0')
        with patch.object(release, 'api', side_effect=[None, []]):
            release.absent('v2.0.0')

    def test_annotated_tag_and_moved_tag(self):
        with patch.object(release, 'api', side_effect=[{'object': {'type': 'tag', 'sha': 'tag-object'}},
                                                      {'object': {'type': 'commit', 'sha': 'commit'}}]):
            release.remote_commit('v2.0.0', 'commit')
        with patch.object(release, 'api', return_value={'object': {'type': 'commit', 'sha': 'other'}}):
            with self.assertRaisesRegex(RuntimeError, 'moved'):
                release.remote_commit('v2.0.0', 'commit')

    def test_checksums_and_run_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            name = 'mio-2.0.0-rc.5-darwin-arm64.tar.gz'
            (root / name).write_bytes(b'final-archive')
            checksum = release.sha(root / name)
            (root / 'SHA256SUMS').write_text(f'{checksum}  {name}\n')
            provenance = dict(tag='v2.0.0-rc.5', version='2.0.0-rc.5', commit='commit', run_id='run',
                              run_attempt=os.environ.get('GITHUB_RUN_ATTEMPT', 'local'), sha256=checksum,
                              m18={'status': 'BOOTSTRAP_PASS'}, smoke={'status': 'PASS'},
                              assets=[name, 'SHA256SUMS', 'release-provenance.json'])
            (root / 'release-provenance.json').write_text(json.dumps(provenance))
            release.verify_assets(root, 'v2.0.0-rc.5', 'commit', 'run')
            with self.assertRaisesRegex(RuntimeError, 'source differs'):
                release.verify_assets(root, 'v2.0.0-rc.5', 'commit', 'another-run')
            (root / name).write_bytes(b'tampered')
            with self.assertRaisesRegex(RuntimeError, 'checksum differs'):
                release.verify_assets(root, 'v2.0.0-rc.5', 'commit', 'run')

    def test_dry_run_cannot_publish(self):
        with patch.dict(os.environ, {'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REF': 'refs/heads/main'}), \
             patch.object(release, 'metadata', return_value=release.parse_tag('v2.0.0')), \
             patch.object(release, 'git', return_value=os.environ.get('GITHUB_SHA', 'commit')), \
             patch.object(release, 'verify_assets', return_value=({}, {})), \
             patch.object(release, 'api') as api, \
             patch('sys.argv', ['release_automation.py', 'publish', '--tag', 'v2.0.0']):
            with self.assertRaisesRegex(RuntimeError, 'only a tag push'):
                release.main()
            api.assert_not_called()

    def test_release_classification_and_upload_failure(self):
        for tag, rc in [('v2.0.0-rc.5', True), ('v2.0.0', False)]:
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                notes = root / 'docs/releases/v2.0.0.md'
                notes.parent.mkdir(parents=True)
                notes.write_text('Release notes')
                (root / 'archive').write_bytes(b'archive')
                events = []
                def api(path, method='GET', data=None, content_type=None):
                    events.append((method, data))
                    if method == 'POST' and path == '/releases':
                        return {'id': 1, 'upload_url': 'https://uploads.github.com/repos/test/repo/releases/1/assets{?name}'}
                    if method == 'POST':
                        return {'name': 'archive', 'size': len(data), 'state': 'uploaded'}
                    return {}
                with patch.dict(os.environ, {'GITHUB_EVENT_NAME': 'push', 'GITHUB_REF': 'refs/tags/' + tag}), \
                     patch.object(release, 'ROOT', root), \
                     patch.object(release, 'metadata', return_value=release.parse_tag(tag)), \
                     patch.object(release, 'git', return_value=os.environ.get('GITHUB_SHA', 'commit')), \
                     patch.object(release, 'verify_assets', return_value=(release.parse_tag(tag), {'assets': ['archive']})), \
                     patch.object(release, 'remote_commit'), patch.object(release, 'workflow_baseline'), patch.object(release, 'absent'), \
                     patch.object(release, 'api', side_effect=api), \
                     patch('sys.argv', ['release_automation.py', 'publish', '--tag', tag, '--directory', directory]):
                    release.main()
                    self.assertTrue(events[0][1]['draft'])
                    self.assertEqual(events[0][1]['prerelease'], rc)
                    self.assertEqual(events[-1][0], 'PATCH')
                    self.assertEqual(events[-1][1]['prerelease'], rc)
                    events.clear()
                    with patch.object(release, 'api', side_effect=[{'id': 1, 'upload_url': 'https://uploads.github.com/x{?name}'}, RuntimeError('upload failed')]) as failed:
                        with self.assertRaisesRegex(RuntimeError, 'upload failed'):
                            release.main()
                        self.assertEqual(failed.call_count, 2)  # Never PATCH draft=false.

    def test_workflow_failure_bypass_rejected(self):
        import shutil
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(release.ROOT / '.github/workflows', root / '.github/workflows')
            path = root / '.github/workflows/release.yml'
            original = path.read_text()
            path.write_text(original.replace('needs: [preflight, test, m18, package, verify]',
                                             'needs: [preflight, test, m18, package]'))
            with self.assertRaisesRegex(RuntimeError, 'publish gates'):
                release.workflow_policy(root)
            path.write_text(original.replace('  test:\n', '  test:\n    continue-on-error: true\n'))
            with self.assertRaisesRegex(RuntimeError, 'ignore failure'):
                release.workflow_policy(root)

    def test_archive_traversal_and_duplicate_members_rejected(self):
        import io
        import subprocess
        import tarfile
        for names in [['../escape'], ['same', 'same']]:
            with self.subTest(names=names), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                archive = root / 'unsafe.tar.gz'
                with tarfile.open(archive, 'w:gz') as tar:
                    for name in names:
                        item = tarfile.TarInfo(name)
                        item.size = 1
                        tar.addfile(item, io.BytesIO(b'x'))
                report = root / 'report.json'
                result = subprocess.run(['python3', str(release.ROOT / 'scripts/smoke-release.py'),
                                         str(archive), '--report', str(report)], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                value = json.loads(report.read_text())
                self.assertEqual(value['status'], 'FAIL')
                self.assertRegex(value['error'], 'unsafe archive|duplicate archive')

    def test_build_cli_rejects_unsafe_version(self):
        import subprocess
        result = subprocess.run(['python3', str(release.ROOT / 'scripts/prepare-release.py'),
                                 '--output', '/unused', '--version', '2.0.0;bad'], capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'expected v2.x.y', result.stderr)


if __name__ == '__main__':
    unittest.main()
