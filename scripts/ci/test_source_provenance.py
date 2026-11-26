from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify_source_provenance as vsp


class SourceProvenanceTests(unittest.TestCase):
    def make_repo(self, root: Path) -> tuple[Path, str]:
        repo = root / "repo"
        repo.mkdir()
        subprocess.run(["git", "init"], cwd=repo, check=True, capture_output=True)
        subprocess.run(["git", "config", "user.name", "Test"], cwd=repo, check=True)
        subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=repo, check=True)
        (repo / "README.md").write_text("hi\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=repo, check=True)
        subprocess.run(["git", "commit", "-m", "init"], cwd=repo, check=True, capture_output=True)
        sha = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, check=True, capture_output=True, text=True).stdout.strip()
        return repo, sha

    def test_matching_sha_succeeds_and_records_metadata(self):
        with TemporaryDirectory() as tmp:
            repo, sha = self.make_repo(Path(tmp))
            rec = vsp.verify_provenance(sha, repo=str(repo), workflow_ref="mio-kobo/mio/.github/workflows/ci-full.yml@refs/heads/Mio-2.1", run_id="100", attempt_id="1")
            self.assertEqual(rec["requested_sha"], sha)
            self.assertEqual(rec["actual_sha"], sha)
            self.assertEqual(rec["run_id"], "100")
            self.assertEqual(rec["attempt_id"], "1")

    def test_mismatched_sha_raises(self):
        with TemporaryDirectory() as tmp:
            repo, _ = self.make_repo(Path(tmp))
            with self.assertRaisesRegex(ValueError, "source provenance mismatch"):
                vsp.verify_provenance("0" * 40, repo=str(repo))


if __name__ == "__main__":
    unittest.main()
