"""Offline tests for release validation, including malformed and mismatched artifacts."""
import importlib.util
import fnmatch
import io
import json
import os
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("plugin-release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def test_history_gate_fails_closed(self):
        # A fake Git command exercises the shell gate without creating commits/tags.
        with tempfile.TemporaryDirectory() as directory:
            git = Path(directory) / "git"
            git.write_text('''#!/bin/sh
case "$1" in
  fetch) exit "${FETCH_RESULT:-0}" ;;
  rev-parse)
    if [ "$2" = HEAD ]; then echo "${HEAD_RESULT:-trusted-sha}"; else echo trusted-sha; fi ;;
  merge-base) exit "${ANCESTOR_RESULT:-0}" ;;
  *) exit 99 ;;
esac
''')
            git.chmod(0o755)
            environment = {**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"],
                           "GITHUB_EVENT_NAME": "push", "GITHUB_REF_TYPE": "tag",
                           "GITHUB_REF": "refs/tags/jetbrains-V0.6.2"}
            for overrides, expected in [({}, True), ({"FETCH_RESULT": "1"}, False),
                                        ({"HEAD_RESULT": "different-sha"}, False),
                                        ({"ANCESTOR_RESULT": "1"}, False),
                                        ({"GITHUB_EVENT_NAME": "pull_request"}, False),
                                        ({"GITHUB_REF_TYPE": "branch"}, False)]:
                with self.subTest(overrides=overrides):
                    result = subprocess.run(["bash", str(release.ROOT / ".github/scripts/check-release-history.sh")],
                                            env={**environment, **overrides}, capture_output=True)
                    self.assertEqual(result.returncode == 0, expected)

    def test_workflow_glob_routes_only_its_own_platform(self):
        for platform in ["jetbrains", "vscode"]:
            workflow = (release.ROOT / f".github/workflows/publish-{platform}.yml").read_text()
            glob = f"{platform}-V*"
            self.assertIn(f"      - '{glob}'", workflow)
            for tag, expected in [("jetbrains-V0.6.2", platform == "jetbrains"),
                                  ("vscode-V0.3.1", platform == "vscode"),
                                  (f"{platform}-v1.2.3", False), ("Mio-2", False)]:
                self.assertEqual(fnmatch.fnmatchcase(tag, glob), expected)
            # A Glob is deliberately broader than SemVer: the job must reject this.
            self.assertTrue(fnmatch.fnmatchcase(f"{platform}-Vgarbage", glob))
            with self.assertRaises(ValueError):
                release.tag_version(platform, f"{platform}-Vgarbage")

    def test_strict_semver_and_independent_tags(self):
        valid = ["0.0.0", "0.6.2", "10.20.30", "1.2.3-alpha.1", "1.2.3-0", "1.2.3-01a+build.007"]
        invalid = ["", "1", "1.2", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-alpha.01",
                   "1.2.3-", "1.2.3+", "1.2.3+a..b", "1.2.3\n", "1.2.3/evil", "１.2.3", "1.2.3 extra"]
        for platform in ["jetbrains", "vscode"]:
            for version in valid:
                with self.subTest(platform=platform, version=version):
                    self.assertEqual(release.tag_version(platform, f"{platform}-V{version}"), version)
            for version in invalid:
                with self.subTest(platform=platform, version=version), self.assertRaises(ValueError):
                    release.tag_version(platform, f"{platform}-V{version}")
            for tag in ["vscode-V0.3.1", "jetbrains-V0.6.2", f"{platform}-v1.2.3", f"{platform}-V1.2.3junk"]:
                if tag.startswith(platform + "-V") and tag in ["vscode-V0.3.1", "jetbrains-V0.6.2"]:
                    continue
                with self.subTest(platform=platform, tag=tag), self.assertRaises(ValueError):
                    release.tag_version(platform, tag)

    def test_source_versions(self):
        release.check_source("jetbrains", "0.6.2", "0.6.2")
        with self.assertRaises(ValueError):
            release.check_source("jetbrains", "0.6.2", "0.6.1")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            plugin = root / "editors/vscode"
            plugin.mkdir(parents=True)
            package = {"publisher": "exmikupro", "name": "mio-language-support", "version": "0.3.1"}
            (plugin / "package.json").write_text(json.dumps(package))
            lock = {"version": "0.3.1", "packages": {"": {"version": "0.3.1"}}}
            with patch.object(release, "ROOT", root):
                (plugin / "package-lock.json").write_text(json.dumps(lock))
                release.check_source("vscode", "0.3.1")
                with self.assertRaises(ValueError):
                    release.check_source("vscode", "0.3.2")
                lock["packages"][""]["version"] = "0.3.0"
                (plugin / "package-lock.json").write_text(json.dumps(lock))
                with self.assertRaises(ValueError):
                    release.check_source("vscode", "0.3.1")

    def test_final_vsix_manifest_and_packaged_json(self):
        for xml_version, package_version, publisher, expected in [
            ("0.3.1", "0.3.1", "exmikupro", True),
            ("0.3.0", "0.3.1", "exmikupro", False),
            ("0.3.1", "0.3.0", "exmikupro", False),
            ("0.3.1", "0.3.1", "someone-else", False),
        ]:
            with self.subTest(xml_version=xml_version, package_version=package_version, publisher=publisher):
                data = io.BytesIO()
                with zipfile.ZipFile(data, "w") as archive:
                    archive.writestr("extension.vsixmanifest", f'<PackageManifest xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011"><Metadata><Identity Id="mio-language-support" Publisher="{publisher}" Version="{xml_version}" /></Metadata></PackageManifest>')
                    archive.writestr("extension/package.json", json.dumps({"version": package_version, "publisher": publisher, "name": "mio-language-support"}))
                data.seek(0)
                if expected:
                    release.check_archive("vscode", "0.3.1", data)
                else:
                    with self.assertRaises(ValueError):
                        release.check_archive("vscode", "0.3.1", data)

    def test_jetbrains_archive_version_and_identity(self):
        for identity, version, expected in [("com.exmikupro.mio", "0.6.2", True), ("other.plugin", "0.6.2", False), ("com.exmikupro.mio", "0.6.1", False)]:
            jar = io.BytesIO()
            with zipfile.ZipFile(jar, "w") as archive:
                archive.writestr("META-INF/plugin.xml", f"<idea-plugin><id>{identity}</id><version>{version}</version></idea-plugin>")
            bundle = io.BytesIO()
            with zipfile.ZipFile(bundle, "w") as archive:
                archive.writestr("mio/lib/mio.jar", jar.getvalue())
            bundle.seek(0)
            if expected:
                release.check_archive("jetbrains", "0.6.2", bundle)
            else:
                with self.assertRaises(ValueError):
                    release.check_archive("jetbrains", "0.6.2", bundle)

    def test_duplicate_jetbrains_in_older_page(self):
        pages = [{"xmlId": "com.exmikupro.mio"}, [{"version": "0.6.3"}] * 100, [{"version": "0.6.2"}]]
        with patch.object(release, "request_json", side_effect=pages) as request, self.assertRaisesRegex(ValueError, "Already published"):
            release.check_unpublished("jetbrains", "0.6.2")
        self.assertIn("offset=100", request.call_args.args[0])

    def test_duplicate_vscode(self):
        data = {"results": [{"extensions": [{"publisher": {"publisherName": "exmikupro"}, "extensionName": "mio-language-support", "versions": [{"version": "0.3.1"}, {"version": "0.3.0"}]}]}]}
        with patch.object(release, "request_json", return_value=data), self.assertRaisesRegex(ValueError, "Already published"):
            release.check_unpublished("vscode", "0.3.0")
        with patch.object(release, "request_json", return_value=data):
            release.check_unpublished("vscode", "0.3.2")

    def test_marketplace_failure_is_not_ignored(self):
        for platform in ["jetbrains", "vscode"]:
            with patch.object(release, "request_json", side_effect=OSError("network down")), self.assertRaises(OSError):
                release.check_unpublished(platform, "1.2.3")


if __name__ == "__main__":
    unittest.main()
