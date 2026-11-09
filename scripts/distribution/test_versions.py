import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))

import bootstrap
import versions
from prepare import prepare
from stable_release import archives, docker_archive, installers, payloads

VERSION_HELPERS = ('parse_version', 'parse_tag', 'tag_for', 'branch_for', 'stable_version',
                   'is_stable', 'is_rc', 'is_build_version')


def git(root, *args):
    return subprocess.run(['git', *args], cwd=str(root), check=True, capture_output=True, text=True).stdout.strip()


def init_repo(root):
    subprocess.run(['git', 'init', '-q', str(root)], check=True)
    git(root, 'config', 'user.name', 'Mio Release Test')
    git(root, 'config', 'user.email', 'release-test@example.invalid')
    git(root, 'config', 'commit.gpgsign', 'false')
    (root / 'file.txt').write_text('base\n')
    git(root, 'add', '.')
    git(root, 'commit', '-qm', 'base')
    return git(root, 'rev-parse', 'HEAD')


def commit(root, text):
    (root / 'file.txt').write_text(text + '\n')
    git(root, 'add', 'file.txt')
    git(root, 'commit', '-qm', text)
    return git(root, 'rev-parse', 'HEAD')


def branch(root, name, sha):
    git(root, 'update-ref', 'refs/heads/' + name, sha)


def tag(root, name, sha):
    git(root, 'update-ref', 'refs/tags/' + name, sha)


class VersionParsingTests(unittest.TestCase):
    def test_stable_version_parsing(self):
        for value in ('2.1.0', '2.1.1', '2.1.2', '2.2.0', '2.99.4', '3.0.0', '10.4.2'):
            with self.subTest(value=value):
                parsed = versions.parse_version(value)
                self.assertTrue(parsed.is_stable)
                self.assertFalse(parsed.is_rc)
                self.assertEqual(str(parsed), value)
                self.assertTrue(versions.stable_version(value))
                self.assertFalse(versions.is_rc(value))
        for value in ('2.1', '2', '2.1.1.1', 'v2.1.1', '02.1.1', '2.01.1', '2.1.01',
                      '2.1.1-rc', '2.1.1-pr.0123456789ab', '../2.1.1', ''):
            with self.subTest(value=value), self.assertRaises(versions.VersionError):
                versions.parse_version(value)
        # v1.x and v2.0.x are historical and are not rebuilt by this pipeline.
        for value in ('1.0.0', '1.9.9', '2.0.0', '2.0.9'):
            with self.subTest(value=value), self.assertRaises(versions.VersionError):
                versions.parse_version(value)

    def test_release_candidate_parsing(self):
        for value, rc in (('2.1.1-rc.1', 1), ('2.1.2-rc.2', 2), ('3.0.0-rc.12', 12)):
            with self.subTest(value=value):
                parsed = versions.parse_version(value)
                self.assertTrue(parsed.is_rc)
                self.assertEqual(parsed.rc, rc)
                self.assertEqual(str(parsed), value)
                self.assertTrue(versions.is_rc(value))
                self.assertFalse(versions.stable_version(value))
        for value in ('2.1.1-rc.0', '2.1.1-rc.01', '2.1.1-rc', '2.1.1-rc.1.2',
                      '2.1.1-RC.1', '2.1.1-rc.-1', '2.0.0-rc.1'):
            with self.subTest(value=value), self.assertRaises(versions.VersionError):
                versions.parse_version(value)

    def test_future_major_versions(self):
        for value in ('3.0.0', '3.1.4', '4.0.0', '10.20.30', '3.0.0-rc.1'):
            with self.subTest(value=value):
                self.assertEqual(versions.tag_for(value), 'v' + value)
                self.assertEqual(str(versions.parse_tag('v' + value)), value)
        self.assertEqual(versions.branch_for('v3.0.0'), 'Mio-3.0.0')
        self.assertEqual(versions.branch_for('v3.0.0-rc.1'), 'Mio-3.0.0')
        self.assertEqual(versions.branch_for('v10.20.30'), 'Mio-10.20.30')

    def test_version_to_branch_mapping(self):
        expected = {
            'v2.1.0': 'Mio-2.1',
            'v2.1.0-rc.1': 'Mio-2.1',
            'v2.1.1': 'Mio-2.1.1',
            'v2.1.1-rc.1': 'Mio-2.1.1',
            'v2.1.2': 'Mio-2.1.2',
            'v2.1.2-rc.1': 'Mio-2.1.2',
            'v2.2.0': 'Mio-2.2.0',
            'v3.0.0': 'Mio-3.0.0',
            'v3.0.0-rc.1': 'Mio-3.0.0',
        }
        for value, expected_branch in expected.items():
            with self.subTest(value=value):
                self.assertEqual(versions.branch_for(value), expected_branch)
                self.assertEqual(versions.branch_for(value[1:]), expected_branch)
                self.assertEqual(versions.info(value)['branch'], expected_branch)
        self.assertTrue(versions.is_historical('v2.1.0'))
        self.assertFalse(versions.is_historical('v2.1.1'))


class GitReferenceTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.base = init_repo(self.root)

    def test_missing_version_branch_is_rejected(self):
        tag(self.root, 'v2.1.1', self.base)
        with self.assertRaisesRegex(versions.RefError, 'Mio-2.1.1 does not exist'):
            versions.verify_release(self.root, 'v2.1.1')
        with self.assertRaisesRegex(versions.RefError, 'does not exist'):
            versions.verify_release(self.root, 'v9.9.9')

    def test_tag_must_resolve_to_the_exact_branch_tip(self):
        other = commit(self.root, 'unrelated')
        tag(self.root, 'v2.1.2', other)
        branch(self.root, 'Mio-2.1.2', self.base)
        with self.assertRaisesRegex(versions.RefError, 'exact version branch tip'):
            versions.verify_release(self.root, 'v2.1.2')

    def test_moved_version_branch_is_rejected(self):
        tag(self.root, 'v2.1.2', self.base)
        branch(self.root, 'Mio-2.1.2', self.base)
        self.assertEqual(versions.verify_release(self.root, 'v2.1.2')['mode'], 'exact-branch-tip')
        moved = commit(self.root, 'branch advanced after the tag')
        branch(self.root, 'Mio-2.1.2', moved)
        with self.assertRaisesRegex(versions.RefError, 'exact version branch tip'):
            versions.verify_release(self.root, 'v2.1.2')

    def test_unexpected_source_revision_is_rejected(self):
        tag(self.root, 'v2.1.1', self.base)
        branch(self.root, 'Mio-2.1.1', self.base)
        with self.assertRaisesRegex(versions.RefError, 'does not match'):
            versions.verify_release(self.root, 'v2.1.1', source_sha='b' * 40)
        with self.assertRaisesRegex(versions.RefError, 'full 40-character source commit'):
            versions.verify_release(self.root, 'v2.1.1', source_sha='short')

    def test_remote_branch_is_preferred_when_present(self):
        tag(self.root, 'v2.1.2', self.base)
        branch(self.root, 'Mio-2.1.2', self.base)
        remote = commit(self.root, 'remote head')
        git(self.root, 'update-ref', 'refs/remotes/origin/Mio-2.1.2', remote)
        with self.assertRaisesRegex(versions.RefError, 'exact version branch tip'):
            versions.verify_release(self.root, 'v2.1.2')

    def test_historical_v2_1_0_may_remain_an_ancestor(self):
        tag(self.root, 'v2.1.0', self.base)
        advanced = commit(self.root, 'post v2.1.0 development')
        branch(self.root, 'Mio-2.1', advanced)
        result = versions.verify_release(self.root, 'v2.1.0')
        self.assertEqual(result['mode'], 'historical-ancestor')
        self.assertEqual(result['branch'], 'Mio-2.1')
        # A historical tag pointing at an unrelated commit is still rejected.
        git(self.root, 'checkout', '-q', '-b', 'side')
        unrelated = commit(self.root, 'unrelated history')
        git(self.root, 'checkout', '-q', '--detach', self.base)
        tag(self.root, 'v2.1.0', unrelated)
        with self.assertRaisesRegex(versions.RefError, 'not an ancestor'):
            versions.verify_release(self.root, 'v2.1.0')

    def test_rc_tag_and_stable_tag_share_the_version_branch(self):
        tip = commit(self.root, 'release content')
        branch(self.root, 'Mio-2.1.2', tip)
        tag(self.root, 'v2.1.2-rc.1', tip)
        tag(self.root, 'v2.1.2', tip)
        self.assertEqual(versions.verify_release(self.root, 'v2.1.2-rc.1')['branch'], 'Mio-2.1.2')
        self.assertEqual(versions.verify_release(self.root, 'v2.1.2')['branch'], 'Mio-2.1.2')
        self.assertEqual(versions.verify_release(self.root, 'v2.1.2')['kind'], 'stable')


class DispatchTests(unittest.TestCase):
    ENV = dict(event='workflow_dispatch', repository='mio-kobo/mio')

    def test_dispatch_requires_the_exact_version_tag(self):
        result = versions.verify_dispatch('2.1.2', 'stable', tag='v2.1.2',
                                          ref='refs/tags/v2.1.2', **self.ENV)
        self.assertEqual(result['branch'], 'Mio-2.1.2')
        self.assertEqual(result['tag'], 'v2.1.2')
        for kwargs in (dict(tag='v2.1.1'), dict(ref='refs/heads/Mio-2.1.2'),
                       dict(event='pull_request'), dict(repository='fork/mio')):
            with self.subTest(kwargs=kwargs), self.assertRaises(versions.VersionError):
                arguments = dict(tag='v2.1.2', ref='refs/tags/v2.1.2', **self.ENV)
                arguments.update(kwargs)
                versions.verify_dispatch('2.1.2', 'stable', **arguments)

    def test_stable_and_rc_requests_must_not_be_mixed(self):
        with self.assertRaisesRegex(versions.VersionError, 'stable release requested with an RC'):
            versions.verify_dispatch('2.1.2-rc.1', 'stable', tag='v2.1.2-rc.1',
                                     ref='refs/tags/v2.1.2-rc.1', **self.ENV)
        with self.assertRaisesRegex(versions.VersionError, 'RC release requested with a stable'):
            versions.verify_dispatch('2.1.2', 'rc', tag='v2.1.2',
                                     ref='refs/tags/v2.1.2', **self.ENV)

    def test_future_major_dispatch_is_resolved_generically(self):
        result = versions.verify_dispatch('3.0.0-rc.1', 'rc', tag='v3.0.0-rc.1',
                                          ref='refs/tags/v3.0.0-rc.1', **self.ENV)
        self.assertEqual(result['branch'], 'Mio-3.0.0')


class SourceVersionTests(unittest.TestCase):
    def source(self, name, version):
        root = Path(tempfile.mkdtemp(dir=self.temp.name)) / name
        (root / 'selfhost/resources').mkdir(parents=True)
        (root / 'selfhost/cli').mkdir(parents=True)
        (root / 'selfhost/resources/bunko_data.mio').write_text(
            '// generated\nwaza data_available() -> bool { kaesu hai; }\nwaza data_files() -> []File { kaesu []; }\n')
        (root / 'selfhost/cli/main.mio').write_text(
            'mio v%s-dev (pure Mio Native AOT; maintenance candidate)' % version)
        return root

    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.temp = temp

    def test_source_version_mismatch_is_rejected(self):
        source = self.source('mismatch', '2.1.1')
        with self.assertRaisesRegex(ValueError, 'source declares Mio 2.1.1'):
            prepare(source, 'a' * 40, stable_version='2.1.2')
        with self.assertRaisesRegex(ValueError, 'source declares Mio 2.1.1'):
            prepare(source, 'a' * 40, rc_version='2.1.2-rc.1')

    def test_source_version_matches_stable_and_rc(self):
        stable = self.source('stable', '2.1.2')
        record = prepare(stable, 'a' * 40, stable_version='2.1.2')
        self.assertEqual(record['version'], '2.1.2')
        self.assertEqual(record['source_version'], '2.1.2')
        candidate = self.source('candidate', '2.2.0')
        record = prepare(candidate, 'a' * 40)
        self.assertEqual(record['version'], '2.2.0-pr.' + 'a' * 12)
        self.assertEqual(record['source_version'], '2.2.0')

    def test_missing_or_duplicate_version_template_is_rejected(self):
        source = self.source('missing', '2.1.1')
        (source / 'selfhost/cli/main.mio').write_text('mio version')
        with self.assertRaisesRegex(ValueError, 'unexpected CLI version template'):
            prepare(source, 'a' * 40)
        duplicate = self.source('duplicate', '2.1.1')
        (duplicate / 'selfhost/cli/main.mio').write_text(
            'mio v2.1.1-dev (pure Mio Native AOT; a)\nmio v2.1.1-dev (pure Mio Native AOT; b)\n')
        with self.assertRaisesRegex(ValueError, 'unexpected CLI version template'):
            prepare(duplicate, 'a' * 40)


class DistributionNamingTests(unittest.TestCase):
    def test_archive_names_are_version_independent(self):
        for version in ('2.1.1', '2.1.2', '2.2.0', '3.0.0'):
            with self.subTest(version=version):
                self.assertEqual(archives(version, 'linux-amd64'), [
                    'mio-%s-linux-amd64-core.tar.gz' % version,
                    'mio-%s-linux-amd64-full.tar.gz' % version])
                self.assertEqual(archives(version, 'windows-amd64'), [
                    'mio-%s-windows-amd64-core.zip' % version,
                    'mio-%s-windows-amd64-full.zip' % version])
                self.assertEqual(installers(version), [
                    'mio-%s-windows-amd64-core-setup.exe' % version,
                    'mio-%s-windows-amd64-full-setup.exe' % version])
                self.assertEqual(docker_archive(version), 'mio-%s-linux-amd64-full.docker.tar' % version)
                self.assertEqual(len(payloads(version)), 9)
                for name in payloads(version):
                    self.assertIn(version, name)

    def test_build_versions_are_generic_but_invalid_labels_are_rejected(self):
        for value in ('2.1.1', '2.1.2-rc.1', '3.0.0', '3.0.0-rc.4', '3.0.0-pr.0123456789ab'):
            with self.subTest(value=value):
                self.assertTrue(versions.is_build_version(value))
        for value in ('2.1.2-dev', 'v3.0.0', '2.0.0', '2.1.2-pr.xyz', '2.1.2-pr.0123456789AB'):
            with self.subTest(value=value):
                self.assertFalse(versions.is_build_version(value))


class BootstrapProfileTests(unittest.TestCase):
    def test_reviewed_profiles_are_narrow_and_versioned(self):
        self.assertEqual(bootstrap.seed_kind_for('2.1.0'), 'gf4')
        self.assertEqual(bootstrap.seed_kind_for('2.1.1'), 'native-v21')
        self.assertEqual(bootstrap.seed_kind_for('2.1.2'), 'native-v21')
        self.assertEqual(bootstrap.profile_for('2.1.2').artifact, 'trusted-native-v21')
        for version in ('2.2.0', '2.2.1', '3.0.0', '10.0.0'):
            with self.subTest(version=version), self.assertRaises(bootstrap.BootstrapProfileMissing):
                bootstrap.seed_kind_for(version)

    def test_rc_and_unregistered_versions_fail_closed(self):
        with self.assertRaises(bootstrap.BootstrapProfileMissing):
            bootstrap.profile_for('2.1.2-rc.1')
        with self.assertRaises(versions.VersionError):
            bootstrap.profile_for('2.0.0')

    def test_profiles_list_is_json_serializable(self):
        data = bootstrap.profiles()
        self.assertTrue(any(item['kind'] == 'native-v21' for item in data))
        json.dumps(data)


class PackageManagerReleaseTests(unittest.TestCase):
    def test_package_manager_stable_tag_validation_is_version_generic(self):
        sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'package_managers'))
        from release import stable
        for tag, version in (('v2.1.0', '2.1.0'), ('v2.1.2', '2.1.2'),
                             ('v2.2.0', '2.2.0'), ('v3.0.0', '3.0.0')):
            with self.subTest(tag=tag):
                self.assertEqual(stable(tag), version)
        for tag in ('v2.1.2-rc.1', 'v3.0.0-rc.1', 'v2.0.0', 'v1.0.0', '2.1.2'):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                stable(tag)


if __name__ == '__main__':
    unittest.main()
