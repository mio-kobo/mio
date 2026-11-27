"""Validate the release workflows as YAML and enforce the version-independent design.

These tests parse the real workflow files; they do not execute GitHub Actions.
The distribution and package-manager release workflows additionally run these
checks in CI before any native artifact is accepted.
"""
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

import yaml

from select_artifacts import select_artifacts

ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / '.github/workflows'
RELEASE_WORKFLOWS = (
    'distribution-pr.yml',
    'package-managers.yml',
    'release-candidate.yml',
    'ci-pr.yml',
    'native.yml',
    'web.yml',
)


def load(name):
    return yaml.safe_load((WORKFLOWS / name).read_text(encoding='utf-8'))


def triggers(document):
    value = document.get('on', document.get(True))
    if value is None:
        raise AssertionError('workflow has no on: trigger')
    return value


class WorkflowYamlTests(unittest.TestCase):
    def test_every_workflow_is_valid_yaml_with_triggers_and_jobs(self):
        paths = sorted(WORKFLOWS.glob('*.yml'))
        self.assertTrue(paths)
        for path in paths:
            with self.subTest(workflow=path.name):
                document = yaml.safe_load(path.read_text(encoding='utf-8'))
                self.assertIsInstance(document, dict)
                # PyYAML implements YAML 1.1, where the bare ``on`` key parses as True.
                self.assertTrue('on' in document or True in document,
                                'the YAML 1.1 on key may be parsed as True')
                self.assertIsInstance(triggers(document), (dict, str, list))
                self.assertIsInstance(document.get('jobs'), dict)
                self.assertTrue(document['jobs'])

    def test_modified_release_workflows_parse(self):
        for name in RELEASE_WORKFLOWS:
            with self.subTest(workflow=name):
                document = load(name)
                self.assertIsInstance(document['jobs'], dict)

    def test_no_workflow_uses_pull_request_target(self):
        for path in sorted(WORKFLOWS.glob('*.yml')):
            with self.subTest(workflow=path.name):
                self.assertNotIn('pull_request_target', path.read_text(encoding='utf-8'))


class ArtifactRerunTests(unittest.TestCase):
    def setUp(self):
        work = TemporaryDirectory()
        self.addCleanup(work.cleanup)
        self.downloads = Path(work.name) / "downloads"
        self.destination = Path(work.name) / "selected"

    def artifact(self, name, filename, text):
        artifact = self.downloads / name
        artifact.mkdir(parents=True, exist_ok=True)
        (artifact / filename).write_text(text, encoding="utf-8")

    def test_candidate_retry_reuses_its_compiler_from_the_same_run(self):
        name = "distribution-compiler-darwin-arm64"
        self.artifact(f"{name}-125-1", "mio-B", "the successful producer")
        self.artifact(f"{name}-126-2", "mio-B", "a different workflow run")
        self.artifact("distribution-compiler-windows-amd64-125-2", "mio-B.exe", "a different target")
        attempt = select_artifacts(self.downloads, self.destination, name, "125")
        self.assertEqual(attempt, 1)
        self.assertEqual((self.destination / "mio-B").read_text(encoding="utf-8"), "the successful producer")
        self.assertEqual(len(list(self.destination.iterdir())), 1)
        jobs = load("distribution-pr.yml")["jobs"]
        self.assertTrue(any(
            step.get("uses") == "./.github/actions/download-run-artifact"
            and step["with"]["artifact"] == "distribution-compiler-${{ matrix.target }}"
            for step in jobs["candidates"]["steps"]
        ))

    def test_summary_uses_latest_numeric_attempt_and_keeps_reused_reports(self):
        self.artifact("native-report-linux-amd64-125-2", "report.json", "old")
        self.artifact("native-report-linux-amd64-125-10", "report.json", "new")
        self.artifact("native-report-darwin-arm64-125-1", "report.json", "reused")
        self.artifact("native-report-windows-amd64-125-1", "report.json", "old")
        self.artifact("native-report-windows-amd64-125-10", "build.log", "no report")
        select_artifacts(self.downloads, self.destination, "native-report-*", "125", group=True)
        self.assertEqual({entry.name for entry in self.destination.iterdir()}, {
            "native-report-linux-amd64-125-10",
            "native-report-darwin-arm64-125-1",
            "native-report-windows-amd64-125-10",
        })
        self.assertFalse((self.destination / "native-report-windows-amd64-125-10" / "report.json").exists())

    def test_required_artifact_from_a_different_run_is_rejected(self):
        self.artifact("native-stage-a-126-2", "mio", "from another workflow run")
        with self.assertRaisesRegex(ValueError, "No artifacts matching"):
            select_artifacts(self.downloads, self.destination, "native-stage-a", "125")
        self.assertFalse(self.destination.exists())


class VersionBranchEligibilityTests(unittest.TestCase):
    def branch_filters(self, name):
        pull_request = triggers(load(name)).get('pull_request')
        if not pull_request:
            return []
        return pull_request.get('branches', []) if isinstance(pull_request, dict) else []

    def test_future_version_archive_branches_are_eligible_for_pull_requests(self):
        for name in ('ci-pr.yml', 'native.yml', 'web.yml', 'distribution-pr.yml'):
            with self.subTest(workflow=name):
                filters = self.branch_filters(name)
                self.assertIn('Mio-[0-9]*', filters)
                # A hardcoded list would silently exclude the next version branch.
                self.assertNotIn('Mio-2.1.1', filters)

    def test_release_scope_resolves_versions_in_one_place(self):
        text = (WORKFLOWS / 'distribution-pr.yml').read_text(encoding='utf-8')
        self.assertIn('import bootstrap, versions', text)
        self.assertIn('versions.verify_dispatch', text)
        self.assertIn('bootstrap.profile_for', text)
        self.assertIn('scripts/distribution/versions.py verify', text)
        self.assertIn('scripts/distribution/bootstrap.py --version', text)
        self.assertNotIn('merge-base --is-ancestor', text)
        self.assertNotIn(r'^\s*if \[\[ "\$MIO_DISTRIBUTION_STABLE_VERSION" =~', text)
        self.assertIn('needs.scope.outputs.kind', text)

    def test_release_scope_publishes_only_from_the_official_repository(self):
        document = load('distribution-pr.yml')
        text = (WORKFLOWS / 'distribution-pr.yml').read_text(encoding='utf-8')
        self.assertIn("github.repository == 'mio-kobo/mio'", text)
        self.assertEqual(document['permissions'], {'contents': 'read'})
        eligible = [job for job in document['jobs'].values()
                    if job.get('permissions', {}).get('attestations') == 'write']
        self.assertTrue(eligible)
        for job in eligible:
            self.assertEqual(job['permissions'].get('id-token'), 'write')
            self.assertEqual(job['permissions'].get('contents'), 'read')


class PackageManagerWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.text = (WORKFLOWS / 'package-managers.yml').read_text(encoding='utf-8')
        self.document = load('package-managers.yml')

    def test_package_publication_resolves_the_version_archive_branch(self):
        self.assertIn('scripts/distribution/versions.py info', self.text)
        self.assertIn('scripts/distribution/versions.py verify', self.text)
        self.assertNotIn('git merge-base --is-ancestor', self.text)
        self.assertNotIn("origin/Mio-2.1", self.text)
        self.assertIn("github.event.release.tag_name || inputs.tag", self.text)

    def test_release_event_and_dispatch_are_both_supported(self):
        value = triggers(self.document)
        self.assertIn('release', value)
        self.assertEqual(value['release']['types'], ['published'])
        self.assertIn('workflow_dispatch', value)

    def test_publication_stays_behind_environment_and_repository_flags(self):
        jobs = self.document['jobs']
        propose = jobs['propose']
        submit = jobs['chocolatey-submit']
        self.assertEqual(propose.get('environment'), 'package-managers')
        self.assertEqual(submit.get('environment'), 'chocolatey-community')
        self.assertIn("vars.MIO_PACKAGE_PUBLISH_ENABLED == 'true'", str(propose.get('if')))
        self.assertIn("vars.MIO_CHOCOLATEY_PUBLISH_ENABLED == 'true'", str(submit.get('if')))
        # Only the privileged jobs mint or consume publication credentials.
        prepare = str(jobs['prepare'])
        self.assertNotIn('secrets.', prepare)
        self.assertNotIn('create-github-app-token', str(jobs['prepare']))

    def test_pr_workflows_do_not_receive_package_credentials(self):
        for name in ('ci-pr.yml', 'native.yml', 'web.yml', 'distribution-pr.yml'):
            with self.subTest(workflow=name):
                document = load(name)
                value = triggers(document)
                self.assertIn('pull_request', value)
                self.assertNotIn('secrets.', yaml.safe_dump(document))


class ReleaseDocumentationTests(unittest.TestCase):
    def test_documented_release_command_matches_the_workflow_dispatch(self):
        text = (ROOT / 'docs/distribution/README.md').read_text(encoding='utf-8')
        self.assertIn('gh workflow run distribution-pr.yml', text)
        self.assertIn('--ref v2.1.2', text)
        self.assertIn('-f stable_version=2.1.2', text)
        for phrase in ('version archive branch', 'Trusted bootstrap', 'SHA256SUMS',
                       'Package-manager publication', 'GitHub Release'):
            with self.subTest(phrase=phrase):
                self.assertIn(phrase, text)


if __name__ == '__main__':
    unittest.main()
