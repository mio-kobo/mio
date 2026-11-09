"""Validate the default-branch package-manager workflow as YAML.

The package-manager release event runs this workflow from the repository default
branch. It must resolve each published tag to its version archive branch without
moving publication logic into tag-controlled jobs.
"""
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = ROOT / '.github/workflows'


class PackageManagerWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.path = WORKFLOWS / 'package-managers.yml'
        self.text = self.path.read_text(encoding='utf-8')
        self.document = yaml.safe_load(self.text)

    def test_workflow_is_valid_yaml(self):
        self.assertIsInstance(self.document, dict)
        self.assertTrue('on' in self.document or True in self.document)
        self.assertNotIn('pull_request_target', self.text)

    def test_all_release_workflows_parse(self):
        for path in sorted(WORKFLOWS.glob('*.yml')):
            with self.subTest(workflow=path.name):
                self.assertIsInstance(yaml.safe_load(path.read_text(encoding='utf-8')), dict)

    def test_tag_resolution_uses_the_version_archive_convention(self):
        self.assertIn('scripts/distribution/versions.py info', self.text)
        self.assertIn('scripts/distribution/versions.py verify', self.text)
        self.assertNotIn('git merge-base --is-ancestor', self.text)
        self.assertNotIn('origin/Mio-2.1', self.text)
        self.assertIn('github.event.release.tag_name || inputs.tag', self.text)
        self.assertIn('--source-sha "$SOURCE_SHA"', self.text)

    def test_release_event_and_dispatch_are_supported(self):
        value = self.document.get('on', self.document.get(True))
        self.assertIn('release', value)
        self.assertEqual(value['release']['types'], ['published'])
        self.assertIn('workflow_dispatch', value)

    def test_publication_remains_behind_environment_approval(self):
        jobs = self.document['jobs']
        propose = jobs['propose']
        submit = jobs['chocolatey-submit']
        self.assertEqual(propose.get('environment'), 'package-managers')
        self.assertEqual(submit.get('environment'), 'chocolatey-community')
        self.assertIn("vars.MIO_PACKAGE_PUBLISH_ENABLED == 'true'", str(propose.get('if')))
        self.assertIn("vars.MIO_CHOCOLATEY_PUBLISH_ENABLED == 'true'", str(submit.get('if')))
        self.assertNotIn('secrets.', str(jobs['prepare']))
        self.assertNotIn('create-github-app-token', str(jobs['prepare']))

    def test_pr_workflows_do_not_receive_publication_credentials(self):
        for path in sorted(WORKFLOWS.glob('*.yml')):
            text = path.read_text(encoding='utf-8')
            value = yaml.safe_load(text).get('on', yaml.safe_load(text).get(True))
            if 'pull_request' in value:
                with self.subTest(workflow=path.name):
                    self.assertNotIn('secrets.', text)


if __name__ == '__main__':
    unittest.main()
