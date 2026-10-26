"""Regression tests for release boundaries, using fake transport only."""
import json
import os
import subprocess
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

    def test_repository_release_notes_are_tracked_and_not_ignored(self):
        # Local-only notes can make metadata() pass while a CI checkout fails.
        info = release.metadata('v' + (release.ROOT / 'VERSION').read_text().strip())
        notes = info['notes']
        tracked = subprocess.run(
            ['git', 'ls-files', '--error-unmatch', '--', notes],
            cwd=release.ROOT, capture_output=True, text=True)
        self.assertEqual(tracked.returncode, 0, 'release notes must be tracked: ' + notes)
        ignored = subprocess.run(
            ['git', 'check-ignore', '--no-index', '--quiet', '--', notes],
            cwd=release.ROOT, capture_output=True, text=True)
        self.assertEqual(ignored.returncode, 1, 'release notes must not be ignored: ' + notes)

    def test_workflow_contract(self):
        release.workflow_policy()

    def test_existing_release_and_api_failure_stop(self):
        with patch.object(release, 'api', return_value={'draft': True}), self.assertRaisesRegex(RuntimeError, 'already exists'):
            release.absent('v2.0.0')
        with patch.object(release, 'api', side_effect=RuntimeError('HTTP 403')), self.assertRaisesRegex(RuntimeError, '403'):
            release.absent('v2.0.0')

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


class BaselineTests(unittest.TestCase):
    DEFAULT = '1' * 40
    TARGET = '2' * 40
    BLOB = '3' * 40
    OTHER = '4' * 40

    def transport(self, default=None, target=None):
        # Build real Git trees in an isolated repository, independently of the verifier.
        import subprocess
        objects = {}

        def tree(entries):
            payload = ''.join(f'{mode} {kind} {sha}\t{name}\n'
                              for name, mode, kind, sha in entries)
            oid = subprocess.check_output(['git', 'mktree', '--missing'],
                                          input=payload, text=True, cwd=directory).strip()
            objects['/git/trees/' + oid] = dict(sha=oid, truncated=False, tree=[
                dict(path=name, mode=mode, type=kind, sha=sha)
                for name, mode, kind, sha in entries])
            return oid

        with tempfile.TemporaryDirectory() as directory:
            subprocess.run(['git', 'init', '--bare', '--quiet', directory], check=True)
            for commit, files in [(self.DEFAULT, default), (self.TARGET, target)]:
                if files is None:
                    files = [('release.yml', '100644', 'blob', self.BLOB)]
                workflow_tree = tree(files)
                github_tree = tree([('workflows', '040000', 'tree', workflow_tree)])
                root_tree = tree([('.github', '040000', 'tree', github_tree)])
                objects['/git/commits/' + commit] = dict(sha=commit, tree={'sha': root_tree})
        objects[''] = {'default_branch': 'release/default'}
        objects['/git/ref/heads/release%2Fdefault'] = {
            'ref': 'refs/heads/release/default',
            'object': {'type': 'commit', 'sha': self.DEFAULT}}
        objects['/git/ref/tags/v2.0.0-rc.1'] = {'object': {'type': 'commit', 'sha': self.TARGET}}
        objects['/releases/tags/v2.0.0-rc.1'] = None
        objects['/releases?per_page=100&page=1'] = []
        return objects

    def preflight(self, objects, local_tag=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / 'output'
            (root / 'cmd/mio').mkdir(parents=True)
            (root / 'docs/releases').mkdir(parents=True)
            (root / 'VERSION').write_text('2.0.0\n')
            (root / 'cmd/mio/main.go').write_text('var version = "2.0.0"\n')
            (root / 'docs/releases/v2.0.0.md').write_text('RC fixture notes')
            metadata = release.metadata
            def git(*args):
                if args == ('rev-parse', 'HEAD'):
                    return self.TARGET
                if args == ('rev-parse', 'refs/tags/v2.0.0-rc.1^{commit}'):
                    return local_tag or self.TARGET
                if args == ('status', '--porcelain', '--untracked-files=normal'):
                    return ''
                self.fail('unexpected git call: ' + repr(args))
            with patch.dict(os.environ, {'GITHUB_EVENT_NAME': 'push',
                            'GITHUB_SHA': self.TARGET, 'GITHUB_REF': 'refs/tags/v2.0.0-rc.1',
                            'GITHUB_OUTPUT': str(output)}, clear=True), \
                 patch('sys.argv', ['release_automation.py', 'preflight', '--tag', 'v2.0.0-rc.1']), \
                 patch.object(release, 'git', side_effect=git), \
                 patch.object(release, 'metadata', side_effect=lambda tag: metadata(tag, root)), \
                 patch.object(release, 'api', side_effect=lambda path: objects[path]):
                # Real metadata validation (fixture sources) and repository workflow policy.
                release.main()
            self.assertIn('version=2.0.0-rc.1', output.read_text())

    def test_matching_reviewed_default_preflight(self):
        self.preflight(self.transport())

    def test_changed_unreviewed_target_rejected(self):
        objects = self.transport(target=[('release.yml', '100644', 'blob', self.OTHER)])
        with self.assertRaisesRegex(RuntimeError, 'workflow baseline mismatch') as caught:
            self.preflight(objects)
        for text in ['release/default', self.DEFAULT, self.TARGET, self.BLOB,
                     self.OTHER, '.github/workflows/release.yml']:
            self.assertIn(text, str(caught.exception))

    def test_added_deleted_renamed_and_mode_changed(self):
        base = [('release.yml', '100644', 'blob', self.BLOB)]
        for files in [base + [('ci.yml', '100644', 'blob', self.OTHER)],
                      [('ci.yml', '100644', 'blob', self.OTHER)],
                      [('renamed.yml', '100644', 'blob', self.BLOB)],
                      [('release.yml', '100755', 'blob', self.BLOB)]]:
            with self.subTest(files=files), self.assertRaisesRegex(RuntimeError, 'baseline mismatch'):
                self.preflight(self.transport(target=files))
        with self.assertRaisesRegex(RuntimeError, '.github/workflows/ci.yml') as caught:
            self.preflight(self.transport(
                default=base + [('ci.yml', '100644', 'blob', self.OTHER)], target=base))
        self.assertIn('target=MISSING', str(caught.exception))
        # Adding/deleting/renaming after review on BOTH sides passes.
        for files in [base, base + [('ci.yml', '100644', 'blob', self.OTHER)],
                      [('renamed.yml', '100644', 'blob', self.BLOB)]]:
            with self.subTest(reviewed=files):
                self.preflight(self.transport(default=files, target=files))

    def test_missing_workflow_and_directory_rejected(self):
        with self.assertRaisesRegex(RuntimeError, 'empty workflow directory'):
            self.preflight(self.transport(target=[]))
        objects = self.transport()
        root = objects['/git/commits/' + self.TARGET]['tree']['sha']
        objects['/git/trees/' + root]['tree'] = []
        with self.assertRaisesRegex(RuntimeError, 'incomplete or inconsistent'):
            self.preflight(objects)
        # A genuinely absent directory in a valid (empty) root tree.
        empty = '4b825dc642cb6eb9a060e54bf8d69288fbee4904'
        objects = self.transport()
        objects['/git/commits/' + self.TARGET]['tree']['sha'] = empty
        objects['/git/trees/' + empty] = dict(sha=empty, truncated=False, tree=[])
        with self.assertRaisesRegex(RuntimeError, 'missing workflow directory'):
            self.preflight(objects)

    def test_large_complete_listing_has_no_contents_api_limit(self):
        files = [(f'workflow-{i}.yml', '100644', 'blob', self.BLOB) for i in range(1001)]
        self.preflight(self.transport(default=files, target=files))
        with self.assertRaisesRegex(RuntimeError, 'baseline mismatch'):
            self.preflight(self.transport(default=files, target=files[:-1]))

    def test_symlink_workflow_rejected(self):
        with self.assertRaisesRegex(RuntimeError, 'unsupported workflow entry'):
            self.preflight(self.transport(target=[('release.yml', '120000', 'blob', self.BLOB)]))

    def test_wrong_local_and_remote_tag_diagnosed(self):
        objects = self.transport()
        with self.assertRaisesRegex(RuntimeError, 'tag differs from checkout') as caught:
            self.preflight(objects, local_tag=self.OTHER)
        self.assertIn(self.TARGET, str(caught.exception))
        self.assertIn(self.OTHER, str(caught.exception))
        objects['/git/ref/tags/v2.0.0-rc.1']['object']['sha'] = self.OTHER
        with self.assertRaisesRegex(RuntimeError, 'remote tag moved') as caught:
            self.preflight(objects)
        self.assertIn('v2.0.0-rc.1', str(caught.exception))
        self.assertIn(self.OTHER, str(caught.exception))

    def test_api_errors_missing_fields_wrong_refs_fail_closed(self):
        for path, value in [('', None), ('', {}),
                            ('/git/ref/heads/release%2Fdefault', None),
                            ('/git/ref/heads/release%2Fdefault', {'ref': 'refs/heads/wrong'}),
                            ('/git/commits/' + self.TARGET, {}),
                            ('/git/commits/' + self.TARGET, {'sha': self.OTHER})]:
            with self.subTest(path=path, value=value):
                objects = self.transport()
                objects[path] = value
                with self.assertRaises(RuntimeError):
                    self.preflight(objects)
        for error in [RuntimeError('HTTP 403'), TimeoutError('network unavailable')]:
            with patch.object(release, 'api', side_effect=error), self.assertRaises(type(error)):
                release.workflow_baseline(self.TARGET)

    def test_malformed_truncated_incomplete_duplicate_trees_rejected(self):
        import copy
        template = self.transport()
        tree_path = next(path for path, doc in template.items()
                         if path.startswith('/git/trees/') and doc['tree'][0]['path'] == 'release.yml')
        good = template[tree_path]
        bad_documents = [None, [], {}, {**good, 'truncated': True},
                         {**good, 'truncated': None}, {**good, 'tree': []},
                         {**good, 'tree': [*good['tree'], *good['tree']]},
                         {**good, 'tree': [{}]}, {**good, 'sha': self.OTHER}]
        for field, value in [('path', '../release.yml'), ('sha', None),
                             ('sha', 'bad'), ('type', 'tree'), ('mode', 'invalid')]:
            item = {**good['tree'][0], field: value}
            bad_documents.append({**good, 'tree': [item]})
        for doc in bad_documents:
            objects = copy.deepcopy(template)
            objects[tree_path] = doc
            with self.subTest(doc=doc), self.assertRaises(RuntimeError):
                self.preflight(objects)

    def test_same_yaml_display_name_is_not_a_path_collision(self):
        # Blob identities do not depend on workflow `name:`; two files may share it.
        files = [('one.yml', '100644', 'blob', self.BLOB),
                 ('two.yml', '100644', 'blob', self.BLOB)]
        self.preflight(self.transport(default=files, target=list(reversed(files))))



if __name__ == '__main__':
    unittest.main()
