"""Unit tests for the native example driver; the Python fixtures only test the driver."""

import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import examples_native as examples


class DriverTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.work = Path(directory.name)
        self.stdout = self.work / "stdout"
        self.stderr = self.work / "stderr"

    def launch(self, code):
        return examples.child([sys.executable, "-c", code], self.work, os.environ, self.stdout, self.stderr)

    def test_pass(self):
        with self.launch("import sys; sys.stdout.buffer.write(b'Mio\\n')") as proc:
            code = examples.wait(proc, time.monotonic() + 10)
        examples.compare(code, self.stdout.read_bytes(), self.stderr.read_bytes(), examples.Expected(b"Mio\n"))

    def test_output_mismatch(self):
        for stdout, stderr, stream in ((b"Mio\r\n", b"", "stdout"), (b"Mio\n", b"warning\n", "stderr")):
            with self.subTest(stream=stream), self.assertRaisesRegex(ValueError, f"{stream} mismatch"):
                examples.compare(0, stdout, stderr, examples.Expected(b"Mio\n"))

    def test_nonzero_exit(self):
        with self.launch("raise SystemExit(7)") as proc:
            code = examples.wait(proc, time.monotonic() + 10)
        with self.assertRaisesRegex(ValueError, "exit 7, expected 0"):
            examples.compare(code, b"", b"", examples.Expected(b""))

    def test_missing_expected_output(self):
        with self.assertRaisesRegex(ValueError, "missing expected output"):
            examples.compare(0, b"", b"", None)

    def test_timeout(self):
        with self.assertRaisesRegex(TimeoutError, "timed out"):
            with self.launch("import time; time.sleep(60)") as proc:
                examples.wait(proc, time.monotonic() + 0.1)
        self.assertIsNotNone(proc.poll())

    def test_exe_suffix(self):
        self.assertEqual(examples.image_path(self.work, "windows-amd64"), self.work / "program.exe")
        self.assertEqual(examples.image_path(self.work, "linux-amd64"), self.work / "program")

    def test_tcp_server_cleaned_up_if_client_fails(self):
        # Real subprocess, with the same readiness protocol as the native listener.
        body = f"import sys, time; sys.stdout.buffer.write({examples.READY!r}); sys.stdout.flush(); time.sleep(60)"
        with self.assertRaisesRegex(ValueError, "client failed"):
            with self.launch(body) as proc:
                self.assertTrue(examples.wait_for_server(proc, self.stdout, time.monotonic() + 10))
                raise ValueError("client failed")
        self.assertIsNotNone(proc.poll())

    def test_tcp_does_not_treat_a_partial_ready_marker_as_ready(self):
        body = f"import sys; sys.stdout.buffer.write({examples.READY[:-1]!r})"
        with self.launch(body) as proc:
            self.assertFalse(examples.wait_for_server(proc, self.stdout, time.monotonic() + 10))
        self.assertEqual(proc.returncode, 0)

    def test_platform_exclusion_follows_bunko_matrix(self):
        with patch.object(examples, "tcp_exclusion", side_effect=lambda target: "no Winsock" if target == "windows-amd64" else None):
            self.assertEqual(examples.exclusions("windows-amd64"), dict.fromkeys(examples.TCP, "no Winsock"))
            for target in ("darwin-arm64", "linux-amd64"):
                self.assertEqual(set(examples.exclusions(target)), {examples.PE_EXECUTION})
        with patch.object(examples, "tcp_exclusion", return_value=None):
            self.assertEqual(examples.exclusions("windows-amd64"), {})

    def test_inventory_missing_or_unlisted_example(self):
        examples.check_inventory(examples.ROOT / "examples")
        with patch.object(examples, "PROGRAMS", {"demo.mio": examples.Expected(b"")}), patch.object(examples, "TESTS", {}):
            (self.work / "demo.mio").touch()
            examples.check_inventory(self.work)
            (self.work / "demo.mio").rename(self.work / "renamed.mio")
            with self.assertRaisesRegex(ValueError, "unlisted=.*renamed.mio.*missing=.*demo.mio"):
                examples.check_inventory(self.work)

    def test_no_cli_records_required_examples_not_run(self):
        with patch.object(examples, "host_target", return_value="darwin-arm64"):
            results = examples.run_suite(None, "darwin-arm64", self.work, time.monotonic() + 10)
        self.assertEqual({r["status"] for r in results if r["required"] and r["name"] != "inventory"}, {"NOT_RUN"})
        self.assertTrue((self.work / "results.json").is_file())


if __name__ == "__main__":
    unittest.main()
