from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import apt_retry

SH_WRAPPER = Path(__file__).resolve().parent / "apt_retry.sh"


class FakeCompleted:
    def __init__(self, returncode: int, stdout: str = "", stderr: str = ""):
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr


class AptRetryTests(unittest.TestCase):
    def run_sequence(self, responses: list[tuple[int, str]]):
        calls = []
        sleeps = []

        def fake_run(cmd, capture_output=True, text=True):
            idx = len(calls)
            calls.append(cmd)
            rc, err = responses[min(idx, len(responses) - 1)]
            return FakeCompleted(rc, "", err)

        with patch.object(apt_retry.subprocess, "run", side_effect=fake_run):
            rc = apt_retry.run_with_retry(["apt-get", "update"], max_attempts=3, base_delay=2.0, sleep_fn=sleeps.append)
        return rc, len(calls), sleeps

    def test_transient_index_size_mismatch_then_success(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "E: Failed to fetch Packages.gz  File has unexpected size (1234 != 5678). Mirror sync in progress?"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (0, 2, [2.0]))

    def test_temporary_http_failure_then_success(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "E: Failed to fetch foo.deb  503  Service Unavailable"),
            (100, "E: Failed to fetch foo.deb  429  Too Many Requests"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (0, 3, [2.0, 4.0]))

    def test_dns_failure_then_success(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "W: Failed to fetch InRelease  Temporary failure resolving 'archive.ubuntu.com'"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (0, 2, [2.0]))

    def test_persistent_index_size_mismatch_fails_after_max_attempts(self):
        msg = "E: File has unexpected size (1 != 2). Mirror sync in progress?"
        rc, attempts, sleeps = self.run_sequence([(100, msg), (100, msg), (100, msg)])
        self.assertEqual((rc, attempts, sleeps), (100, 3, [2.0, 4.0]))

    def test_invalid_signature_fails_immediately_without_retry(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "W: GPG error: The following signatures were invalid: BADSIG 1234"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (100, 1, []))

    def test_missing_public_key_fails_immediately_without_retry(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "W: GPG error: NO_PUBKEY 0123456789ABCDEF"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (100, 1, []))

    def test_persistent_checksum_failure_never_passes(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "E: Failed to fetch foo.deb  Hash Sum mismatch"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (100, 1, []))

    def test_unrelated_package_install_failure_fails_immediately(self):
        rc, attempts, sleeps = self.run_sequence([
            (100, "E: Unable to locate package nonexistent-package-xyz"),
            (0, ""),
        ])
        self.assertEqual((rc, attempts, sleeps), (100, 1, []))

    def test_shell_wrapper_retries_transient_and_rejects_security_failure(self):
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            state_ok = root / "ok.count"
            cmd_ok = f"c=$(wc -l < '{state_ok}' 2>/dev/null || echo 0); echo x >> '{state_ok}'; if [ \"$c\" -eq 0 ]; then echo 'File has unexpected size' >&2; exit 100; fi; exit 0"
            p_ok = subprocess.run(["sh", str(SH_WRAPPER), "sh", "-c", cmd_ok], capture_output=True, text=True)
            self.assertEqual(p_ok.returncode, 0, p_ok.stderr)
            self.assertEqual(len(state_ok.read_text(encoding="utf-8").splitlines()), 2)

            state_sec = root / "sec.count"
            cmd_sec = f"echo x >> '{state_sec}'; echo 'W: GPG error: NO_PUBKEY ABCDEF' >&2; exit 100"
            p_sec = subprocess.run(["sh", str(SH_WRAPPER), "sh", "-c", cmd_sec], capture_output=True, text=True)
            self.assertEqual(p_sec.returncode, 100, p_sec.stderr)
            self.assertEqual(len(state_sec.read_text(encoding="utf-8").splitlines()), 1)


if __name__ == "__main__":
    unittest.main()
