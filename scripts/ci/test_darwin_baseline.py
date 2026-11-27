from pathlib import Path
import stat
import subprocess
import sys
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import native_platforms


def make_repo(root: Path, include_kiso: bool = True) -> tuple[Path, str]:
    repo = root / "repo"
    repo.mkdir(parents=True, exist_ok=True)
    subprocess.run(["git", "init"], cwd=repo, check=True, capture_output=True)
    subprocess.run(["git", "config", "user.name", "Test"], cwd=repo, check=True)
    subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=repo, check=True)
    (repo / "selfhost" / "cli").mkdir(parents=True)
    (repo / "selfhost" / "cli" / "main.mio").write_text('chant("ok");\n', encoding="utf-8")
    if include_kiso:
        (repo / "bunko" / "kiso").mkdir(parents=True)
        (repo / "bunko" / "kiso" / "mod.mio").write_text("// kiso\n", encoding="utf-8")
    else:
        (repo / "README.md").write_text("no bunko\n", encoding="utf-8")
    subprocess.run(["git", "add", "."], cwd=repo, check=True)
    subprocess.run(["git", "commit", "-m", "init"], cwd=repo, check=True, capture_output=True)
    sha = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, check=True, capture_output=True, text=True).stdout.strip()
    subprocess.run(["git", "remote", "add", "origin", str(repo)], cwd=repo, check=True)
    return repo, sha


def make_fake_cli(path: Path, exit_code: int = 0) -> Path:
    py = sys.executable
    path.write_text(
        f"#!{py}\nimport sys\nfrom pathlib import Path\n"
        f"if {exit_code} != 0:\n    sys.exit({exit_code})\n"
        "out = Path(sys.argv[sys.argv.index('-o') + 1])\n"
        "out.write_bytes(b'baseline-binary')\n",
        encoding="utf-8",
    )
    path.chmod(path.stat().st_mode | stat.S_IXUSR)
    return path


class DarwinBaselineProvenanceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_valid_historical_baseline_from_disposable_cwd(self):
        repo, sha = make_repo(self.root, include_kiso=True)
        disposable_cwd = self.root / "disposable"
        disposable_cwd.mkdir()
        cli = make_fake_cli(self.root / "mio-cli", exit_code=0)
        report = native_platforms.Report("darwin-arm64", self.root / "out")
        work = self.root / "work"
        work.mkdir()
        old_cwd = Path.cwd()
        try:
            import os
            os.chdir(disposable_cwd)
            res = native_platforms.prepare_darwin_baseline(report, cli, work, baseline_sha=sha, repo_root=repo)
        finally:
            os.chdir(old_cwd)
        self.assertTrue(res["ok"], res)
        self.assertEqual(res["status"], "PASS")
        self.assertEqual(res["baseline_sha"], sha)
        self.assertTrue(res["archive_sha256"])
        self.assertTrue(res["cli_sha256"])

    def test_invalid_baseline_sha_fails_explicitly(self):
        repo, _ = make_repo(self.root, include_kiso=True)
        cli = make_fake_cli(self.root / "mio-cli")
        report = native_platforms.Report("darwin-arm64", self.root / "out")
        work = self.root / "work"
        work.mkdir()
        res = native_platforms.prepare_darwin_baseline(report, cli, work, baseline_sha="0" * 40, repo_root=repo)
        self.assertFalse(res["ok"])
        self.assertEqual(res["status"], "BASELINE_NOT_AVAILABLE")

    def test_missing_bunko_kiso_is_rejected(self):
        repo, sha = make_repo(self.root, include_kiso=False)
        cli = make_fake_cli(self.root / "mio-cli")
        report = native_platforms.Report("darwin-arm64", self.root / "out")
        work = self.root / "work"
        work.mkdir()
        res = native_platforms.prepare_darwin_baseline(report, cli, work, baseline_sha=sha, repo_root=repo)
        self.assertFalse(res["ok"])
        self.assertIn("bunko/kiso", res["reason"])

    def test_corrupt_source_archive_is_rejected(self):
        repo, sha = make_repo(self.root, include_kiso=True)
        cli = make_fake_cli(self.root / "mio-cli")
        report = native_platforms.Report("darwin-arm64", self.root / "out")
        work = self.root / "work"
        work.mkdir()
        orig_run = native_platforms.run

        def corrupt_archive(rep, label, argv, **kwargs):
            r = orig_run(rep, label, argv, **kwargs)
            if label == "darwin-thread-phases-baseline-archive":
                (work / "baseline-src.tar").write_bytes(b"not-a-tar-archive")
            return r

        with patch.object(native_platforms, "run", side_effect=corrupt_archive):
            res = native_platforms.prepare_darwin_baseline(report, cli, work, baseline_sha=sha, repo_root=repo)
        self.assertFalse(res["ok"])
        self.assertIn("corrupt", res["reason"])

    def test_missing_compiler_executable_is_rejected(self):
        repo, sha = make_repo(self.root, include_kiso=True)
        report = native_platforms.Report("darwin-arm64", self.root / "out")
        work = self.root / "work"
        work.mkdir()
        res = native_platforms.prepare_darwin_baseline(report, self.root / "nonexistent-cli", work, baseline_sha=sha, repo_root=repo)
        self.assertFalse(res["ok"])
        self.assertIn("missing compiler", res["reason"])

    def test_baseline_compilation_failure_records_baseline_not_available(self):
        repo, sha = make_repo(self.root, include_kiso=True)
        cli = make_fake_cli(self.root / "mio-cli", exit_code=2)
        report = native_platforms.Report("darwin-arm64", self.root / "out")
        work = self.root / "work"
        work.mkdir()
        res = native_platforms.prepare_darwin_baseline(report, cli, work, baseline_sha=sha, repo_root=repo)
        self.assertFalse(res["ok"])
        self.assertEqual(res["status"], "BASELINE_NOT_AVAILABLE")
        self.assertIn("compilation failed", res["reason"])


if __name__ == "__main__":
    unittest.main()
