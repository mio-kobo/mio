#!/usr/bin/env python3
"""Negative contract tests; no Go or target-native runtime required."""
import importlib.util
import json
import os
import io
import shutil
import tarfile
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import shlex
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


runner = load("ci_runner", HERE / "runner.py")
verify = load("ci_verify", HERE / "verify.py")
isolation = load("release_isolation", HERE / "release_isolation.py")
plan = load("ci_plan", HERE / "plan.py")
native = load("ci_native_platforms", HERE / "native_platforms.py")


class ContractTests(unittest.TestCase):
    def test_discovery_rejects_malformed_contract(self):
        for extra in (dict(module="../escape"), dict(engine="unknown"), dict(required_checks="native"),
                      dict(suite_checks={"quick": "native"}), dict(targets=["typo"]),
                      dict(covered_by="example"), dict(covered_by="missing")):
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as directory:
                module = Path(directory) / "internal/example"
                module.mkdir(parents=True)
                data = dict(module="example", targets=["linux-amd64"], suites=["quick"])
                data.update(extra)
                (module / "ci-module.json").write_text(json.dumps(data))
                (module / "test.sh").write_text("exit 1\n")
                with patch.object(runner, "ROOT", Path(directory)), self.assertRaises(ValueError):
                    runner.discover()

    def dispatch(self, modules, selected="all", execute=None, **environment):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, MIO_CI_RESULTS=directory, **environment), \
             patch.object(runner, "discover", return_value=modules), \
             patch.object(runner.platform, "system", return_value="Linux"), \
             patch.object(runner.platform, "machine", return_value="x86_64"), \
             patch.object(runner.shutil, "which", return_value="tool"):
            if execute is None:
                code = runner.run(["linux-amd64", selected, "quick"])
            else:
                with patch.object(runner, "execute", side_effect=execute):
                    code = runner.run(["linux-amd64", selected, "quick"])
            report = json.loads(next(Path(directory).glob("*/report.json")).read_text())
            return code, report

    def test_all_skipped_is_not_success(self):
        modules = {"fake": dict(module="fake", engine="infrastructure", targets=["macos-arm64"], suites=["quick"])}
        code, report = self.dispatch(modules)
        self.assertEqual((code, report["status"], report["checks"][0]["status"]), (2, "NOT_RUN", "SKIP"))
        code, report = self.dispatch({})
        self.assertEqual((code, report["status"]), (2, "NOT_RUN"))

    def test_invalid_module(self):
        code, report = self.dispatch({}, selected="missing")
        self.assertEqual((code, report["status"]), (2, "NOT_RUN"))
        self.assertIn("unknown module", report["reason"])

    def test_invalid_timeout(self):
        for value in ("nan", "inf", "0", "-1", "wrong"):
            with self.subTest(value=value):
                code, report = self.dispatch({}, MIO_CI_MODULE_TIMEOUT=value)
                self.assertEqual((code, report["status"]), (2, "NOT_RUN"))

    def test_interrupted_module_cannot_publish_success(self):
        modules = {"fake": dict(module="fake", engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script="fake.sh")}
        def execute(argv, timeout, env):
            raise KeyboardInterrupt()
        code, report = self.dispatch(modules, execute=execute)
        self.assertEqual((code, report["status"]), (2, "NOT_RUN"))
        self.assertIn("interrupted", report["reason"])

    def test_malformed_module_report_does_not_hide_later_failure(self):
        modules = {name: dict(module=name, engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script="fake.sh") for name in ("broken", "failure")}
        def execute(argv, timeout, env):
            Path(env["MIO_CI_CHECKS"]).write_text('{"name":"bad","status":"UNKNOWN"}\n' if "broken" in env["MIO_CI_MODULE_DIR"] else '{"name":"failed","status":"FAIL"}\n')
            return 0, b"", b"", "PASS", "", 1
        code, report = self.dispatch(modules, execute=execute)
        self.assertEqual((code, report["status"]), (2, "FAIL"))
        self.assertEqual([m["status"] for m in report["checks"]], ["NOT_RUN", "FAIL"])

    def test_native_current_source_build_is_reused(self):
        modules = {name: dict(module=name, targets=["linux-amd64"], suites=["quick"], script="fake.sh") for name in ("one", "two")}
        builds = []
        def execute(argv, timeout, env):
            if argv[1] == "build":
                builds.append(argv)
                Path(argv[-1]).write_bytes(b"current compiler")
            else:
                self.assertEqual(Path(env["MIO_CI_MIO"]).read_bytes(), b"current compiler")
                Path(env["MIO_CI_CHECKS"]).write_text('{"name":"native","kind":"mio","layer":"native","status":"PASS"}\n')
            return 0, b"", b"", "PASS", "", 1
        with tempfile.TemporaryDirectory() as directory:
            seed = Path(directory) / "seed"
            seed.write_bytes(b"seed")
            with patch.object(runner, "SEED_SHA256", runner.hashlib.sha256(b"seed").hexdigest()):
                code, report = self.dispatch(modules, execute=execute, MIO_NATIVE_SEED=str(seed))
        self.assertEqual((code, report["status"], len(builds)), (0, "PASS", 1))

    def test_failed_source_build_cannot_pass_or_rebuild_per_module(self):
        modules = {name: dict(module=name, targets=["linux-amd64"], suites=["quick"], script="fake.sh", required_checks=["native", "independent"]) for name in ("one", "two")}
        calls = []
        def execute(argv, timeout, env):
            calls.append(argv)
            return 1, b"", b"old_sweep_enabled undefined", "FAIL", "compile failed", 1
        with tempfile.TemporaryDirectory() as directory:
            seed = Path(directory) / "seed"
            seed.write_bytes(b"seed")
            with patch.object(runner, "SEED_SHA256", runner.hashlib.sha256(b"seed").hexdigest()):
                code, report = self.dispatch(modules, execute=execute, MIO_NATIVE_SEED=str(seed))
        self.assertEqual((code, report["status"], len(calls)), (1, "FAIL", 1))
        self.assertTrue(all(m["status"] == "NOT_RUN" for m in report["checks"]))
        self.assertTrue(all(c["status"] == "NOT_RUN" for m in report["checks"] for c in m["checks"]))

    def test_mio_skips_and_nonzero_failure_details(self):
        data = dict(packages=[dict(tests=[dict(name="skipped", status="skipped")])], summary=dict(passed=0, failed=0))
        self.assertEqual(runner.assess("mio", 0, json.dumps(data).encode(), "PASS", "")[0], "NOT_RUN")
        data["packages"][0]["tests"][0]["status"] = "failed"
        data["summary"]["failed"] = 1
        status, _, tests = runner.assess("mio", 1, json.dumps(data).encode(), "FAIL", "exit 1")
        self.assertEqual((status, tests[0]["status"]), ("FAIL", "FAIL"))

    def test_timeout_kills_descendants(self):
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "orphan"
            child = "import time; from pathlib import Path; time.sleep(2); Path(" + repr(str(marker)) + ").touch()"
            parent = "import subprocess,sys,time; subprocess.Popen([sys.executable,'-c'," + repr(child) + "]); time.sleep(20)"
            result = self.command(parent, 0.4)
            self.assertEqual((result[3], result[4]), ("FAIL", "timeout"))
            time.sleep(2.2)
            self.assertFalse(marker.exists(), "timed out descendant survived")

    @unittest.skipUnless(shutil.which("bash") and shutil.which("git"), "Bash/Git unavailable")
    def test_parallel_children_and_redirection(self):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "test.sh"
            script.write_text('''set -euo pipefail
"$PYTHON" "$TEST_CI_RUNNER" check command child-fail "$PYTHON" -c 'raise SystemExit(9)' > "$MIO_CI_MODULE_DIR/redirect.log" 2>&1 &
failed=$!
"$PYTHON" "$TEST_CI_RUNNER" check command child-pass "$PYTHON" -c 'print(1)' &
passed=$!
result=0
wait "$failed" || result=$?
wait "$passed" || result=$?
exit "$result"
''', encoding="utf-8")
            modules = {"parallel": dict(module="parallel", engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script=str(script))}
            code, report = self.dispatch(modules, TEST_CI_RUNNER=str(HERE / "runner.py"))
            self.assertEqual((code, report["status"]), (1, "FAIL"))
            self.assertEqual({r["status"] for r in report["checks"][0]["checks"]}, {"PASS", "FAIL"})

    def command(self, source, timeout=5):
        return runner.execute([sys.executable, "-c", source], timeout)

    def test_success(self):
        self.assertEqual(self.command("print('ok')")[3], "PASS")

    def test_false_assertion(self):
        self.assertEqual(self.command("assert 1 == 2")[3], "FAIL")

    def test_output_mismatch(self):
        result = subprocess.CompletedProcess([], 0, b"wrong\n", b"")
        with self.assertRaises(RuntimeError):
            verify.require_output(result, b"expected\n")

    def test_abnormal_exit(self):
        self.assertEqual(self.command("import os; os._exit(17)")[0], 17)

    def test_signal_death(self):
        result = self.command("import os,signal; os.kill(os.getpid(), signal.SIGTERM)")
        self.assertNotEqual(result[0], 0)
        self.assertEqual(result[3], "FAIL")

    @unittest.skipUnless(shutil.which("bash"), "Bash unavailable")
    def test_native_failure_still_runs_independent(self):
        source = shlex.quote((HERE / "common.sh").as_posix())
        result = runner.execute(["bash", "-c", "source " + source + "; ci_native() { return 1; }; ci_check() { echo independent-executed; }; ci_native_and_crosscheck native package independent echo oracle"], 5)
        self.assertEqual((result[0], result[3]), (1, "FAIL"))
        self.assertIn(b"independent-executed", result[1])

    def test_missing_tool(self):
        result = runner.execute(["mio-ci-nonexistent-tool-82ac"], 1)
        self.assertEqual((result[0], result[3]), (2, "NOT_RUN"))

    def test_timeout(self):
        result = self.command("import time; time.sleep(10)", 0.05)
        self.assertEqual((result[3], result[4]), ("FAIL", "timeout"))

    def test_empty_go_report(self):
        self.assertEqual(runner.assess("go", 0, b"", "PASS", "")[0], "NOT_RUN")

    def test_skipped_go_report(self):
        event = dict(Package="p", Test="TestSkipped", Action="skip")
        self.assertEqual(runner.assess("go", 0, json.dumps(event).encode(), "PASS", "")[0], "NOT_RUN")

    def test_empty_mio_report(self):
        data = dict(packages=[], summary=dict(passed=0, failed=0))
        self.assertEqual(runner.assess("mio", 0, json.dumps(data).encode(), "PASS", "")[0], "NOT_RUN")

    def test_failed_go_package_cannot_be_hidden(self):
        events = [dict(Package="p", Test="TestOK", Action="pass"), dict(Package="p", Action="fail")]
        self.assertEqual(runner.assess("go", 0, b"\n".join(json.dumps(e).encode() for e in events), "PASS", "")[0], "FAIL")

    def test_missing_native_seed_records_both_layers(self):
        host = runner.TARGETS["macos-arm64"]
        modules = {"fake": dict(module="fake", targets=["macos-arm64"], suites=["quick"], script="fake.sh", required_checks=["native", "independent"])}
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, MIO_CI_RESULTS=directory, MIO_NATIVE_SEED=""), \
             patch.object(runner, "discover", return_value=modules), \
             patch.object(runner.platform, "system", return_value=host[0]), \
             patch.object(runner.platform, "machine", return_value=host[1]), \
             patch.object(runner.shutil, "which", return_value="fake-tool"):
            self.assertEqual(runner.run(["macos-arm64", "fake", "quick"]), 2)
            report = json.loads(next(Path(directory).glob("*/report.json")).read_text())
            self.assertEqual([r["status"] for r in report["checks"][0]["checks"]], ["NOT_RUN", "NOT_RUN"])

    def test_lying_framework(self):
        # A framework's zero exit code cannot override independently wrong output.
        with self.assertRaises(RuntimeError):
            verify.require_output(subprocess.CompletedProcess([], 0, b"2\n", b""), b"1\n")

    def test_bad_arguments_and_platform(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, MIO_CI_RESULTS=directory):
            self.assertEqual(runner.run([]), 2)
            self.assertEqual(runner.run(["windows-amd64", "core", "quick"]), 2)
            reports = list(Path(directory).glob("*/report.json"))
            self.assertEqual(len(reports), 2)
            self.assertTrue(all(json.loads(p.read_text())["status"] == "NOT_RUN" for p in reports))

    def test_aggregate_failure_and_empty_module(self):
        host = next(iter(runner.TARGETS.values()))
        for child_code, records, expected in ((1, [], 1), (0, [], 2),
                (0, [dict(name="failed-child", status="FAIL")], 1),
                (0, [dict(name="ok-child", status="PASS")], 0)):
            with self.subTest(child_code=child_code, records=records), tempfile.TemporaryDirectory() as directory:
                def fake_execute(argv, timeout, env=None):
                    if env and "MIO_CI_CHECKS" in env:
                        Path(env["MIO_CI_CHECKS"]).write_text("".join(json.dumps(r) + "\n" for r in records))
                        return child_code, b"", b"", "PASS" if child_code == 0 else "FAIL", "", 1
                    return 0, b"", b"", "PASS", "", 1
                modules = {"fake": dict(module="fake", engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script="fake.sh")}
                with patch.dict(os.environ, MIO_CI_RESULTS=directory), patch.object(runner, "discover", return_value=modules), \
                     patch.object(runner.platform, "system", return_value=host[0]), \
                     patch.object(runner.platform, "machine", return_value=host[1]), \
                     patch.object(runner.shutil, "which", return_value="fake-tool"), \
                     patch.object(runner, "execute", side_effect=fake_execute):
                    self.assertEqual(runner.run(["linux-amd64", "fake", "quick"]), expected)

    def test_unsupported_module_suite_and_missing_dependency(self):
        host = runner.TARGETS["linux-amd64"]
        modules = {"fake": dict(module="fake", engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script="fake.sh")}
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, MIO_CI_RESULTS=directory), \
             patch.object(runner, "discover", return_value=modules), \
             patch.object(runner.platform, "system", return_value=host[0]), \
             patch.object(runner.platform, "machine", return_value=host[1]):
            self.assertEqual(runner.run(["linux-amd64", "fake", "impossible"]), 2)
            with patch.object(runner.shutil, "which", return_value=None):
                self.assertEqual(runner.run(["linux-amd64", "fake", "quick"]), 2)

    def test_packaging_rejects_test_tools(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "polluted.tar.gz"
            with tarfile.open(path, "w:gz") as archive:
                for name in ("mio", "LICENSE", "README.txt", "runner.py"):
                    info = tarfile.TarInfo(name)
                    info.size = 4
                    archive.addfile(info, io.BytesIO(b"fake"))
            with self.assertRaisesRegex(RuntimeError, "unexpected or duplicate"):
                isolation.archive_audit(path)

    @unittest.skipUnless(shutil.which("bash") and shutil.which("git"), "Bash/Git unavailable")
    def test_real_dispatch_and_logging(self):
        host = runner.TARGETS["linux-amd64"]
        for source, expected in (("print('executed')", 0), ("assert False", 1), (None, 2)):
            with self.subTest(source=source), tempfile.TemporaryDirectory() as directory:
                script = Path(directory) / "test.sh"
                body = '#!/usr/bin/env bash\nset -euo pipefail\n'
                if source is not None:
                    body += '"$PYTHON" "$TEST_CI_RUNNER" check command independent "$PYTHON" -c ' + "'" + source.replace("'", '"') + "'\n"
                script.write_text(body, encoding="utf-8")
                modules = {"fake": dict(module="fake", engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script=str(script))}
                with patch.dict(os.environ, MIO_CI_RESULTS=directory, TEST_CI_RUNNER=str(HERE / "runner.py")), \
                     patch.object(runner, "discover", return_value=modules), \
                     patch.object(runner.platform, "system", return_value=host[0]), \
                     patch.object(runner.platform, "machine", return_value=host[1]):
                    self.assertEqual(runner.run(["linux-amd64", "fake", "quick"]), expected)
                report_path = next(Path(directory).glob("*/report.json"))
                report = json.loads(report_path.read_text())
                self.assertEqual(report["status"], {0: "PASS", 1: "FAIL", 2: "NOT_RUN"}[expected])
                if source is not None:
                    record = report["checks"][0]["checks"][0]
                    self.assertEqual(record["exit_code"], expected)
                    self.assertTrue(all(Path(p).exists() for p in record["artifacts"]))

    @unittest.skipUnless(shutil.which("bash"), "Bash unavailable; POSIX pipeline NOT_RUN")
    def test_pipefail(self):
        result = runner.execute(["bash", "-c", "set -euo pipefail; (exit 7) | tee /dev/null"], 5)
        self.assertEqual(result[0], 7)


    def test_unregistered_bunko_tests_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            package = Path(directory) / "bunko/kiso/new"
            package.mkdir(parents=True)
            (package / "new_test.mio").write_text("waza test_new() -> unit {}")
            with patch.object(runner, "ROOT", Path(directory)), self.assertRaisesRegex(ValueError, "unregistered"):
                runner.discover()

    def test_wrong_seed_cannot_start_compiler_or_tests(self):
        modules = {"fake": dict(module="fake", targets=["linux-amd64"], suites=["quick"], script="fake.sh",
                                required_checks=["native"], suite_checks={"quick": ["independent"]})}
        with tempfile.TemporaryDirectory() as directory:
            seed = Path(directory) / "untrusted"
            seed.write_bytes(b"not the trusted GF4 seed")
            def forbidden(*args):
                self.fail("untrusted seed or dependent tests executed")
            code, report = self.dispatch(modules, execute=forbidden, MIO_NATIVE_SEED=str(seed))
        self.assertEqual((code, report["status"]), (2, "NOT_RUN"))
        self.assertEqual([r["name"] for r in report["checks"][0]["checks"]], ["native", "independent"])

    def test_inventory_matches_platform_and_preserves_child_oracles(self):
        items = {m["module"]: m for m in runner.inventory()}
        self.assertEqual(items["kiso/aot/arm64"]["schedule"]["macos-arm64"]["quick"], "RUN")
        self.assertEqual(items["kiso/aot/macho"]["schedule"]["macos-arm64"]["full"], "RUN")
        self.assertTrue(all(m["schedule"]["linux-amd64"]["quick"] == "SKIP"
                            for m in items.values() if m["engine"] == "native"))

    def test_recursive_delegation_preserves_independent_checks(self):
        parent = dict(targets=["macos-arm64"], suites=["quick", "full"])
        child = dict(covered_by="aot", required_checks=["native"])
        self.assertTrue(runner.delegated(child, {"aot": parent}, "macos-arm64", "quick"))
        child["required_checks"].append("independent")
        self.assertFalse(runner.delegated(child, {"aot": parent}, "macos-arm64", "quick"))
        self.assertFalse(runner.delegated(child, {"aot": parent}, "linux-amd64", "quick"))

    def test_reference_check_cannot_satisfy_native_module(self):
        modules = {"fake": dict(module="fake", targets=["linux-amd64"], suites=["quick"], script="fake.sh")}
        def execute(argv, timeout, env):
            if argv[1] == "build":
                Path(argv[-1]).write_bytes(b"compiler")
            else:
                Path(env["MIO_CI_CHECKS"]).write_text('{"name":"native","kind":"go","layer":"supplementary","status":"PASS"}\n')
            return 0, b"", b"", "PASS", "", 1
        with tempfile.TemporaryDirectory() as directory:
            seed = Path(directory) / "seed"
            seed.write_bytes(b"seed")
            with patch.object(runner, "SEED_SHA256", runner.hashlib.sha256(b"seed").hexdigest()):
                code, report = self.dispatch(modules, execute=execute, MIO_NATIVE_SEED=str(seed))
        self.assertEqual((code, report["status"]), (2, "NOT_RUN"))

    def test_required_go_skip_cannot_hide_behind_passing_test(self):
        events = [dict(Package="p", Test="TestOK", Action="pass"), dict(Package="p", Test="TestRequired", Action="skip")]
        data = b"\n".join(json.dumps(e).encode() for e in events)
        self.assertEqual(runner.assess("go", 0, data, "PASS", "")[0], "PASS")
        self.assertEqual(runner.assess("go-required", 0, data, "PASS", "")[0], "NOT_RUN")
        package_skip = json.dumps(dict(Package="other", Action="skip")).encode()
        self.assertEqual(runner.assess("go-required", 0, data.splitlines()[0] + b"\n" + package_skip, "PASS", "")[0], "NOT_RUN")

    def test_missing_required_check_and_skip_are_not_success(self):
        modules = {"fake": dict(module="fake", engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script="fake.sh", required_checks=["required"])}
        for records in ([dict(name="other", status="PASS")], [dict(name="required", status="SKIP"), dict(name="other", status="PASS")]):
            def execute(argv, timeout, env):
                Path(env["MIO_CI_CHECKS"]).write_text("".join(json.dumps(r) + "\n" for r in records))
                return 0, b"", b"", "PASS", "", 1
            code, report = self.dispatch(modules, execute=execute)
            self.assertEqual((code, report["status"]), (2, "NOT_RUN"))

    def test_quick_documentation_scope_is_explicit_skip(self):
        result = plan.select(["docs/testing.md", "CONTRIBUTING.md"], runner.discover())
        self.assertEqual(result["mode"], "documentation")
        self.assertEqual((result["native_modules"], result["reference_modules"]), ([], []))
        gate = plan.gate(result, "success", "skipped", "skipped")
        self.assertEqual(gate["status"], "PASS")
        self.assertEqual([c["status"] for c in gate["checks"]], ["PASS", "SKIP", "SKIP"])

    def test_quick_test_only_scope_keeps_core_and_affected_package(self):
        result = plan.select(["bunko/kiso/math/math_test.mio"], runner.discover())
        self.assertEqual(result["mode"], "module-tests")
        self.assertEqual(set(result["native_modules"]), {"core", "kiso/math"})
        self.assertEqual(result["reference_modules"], ["go-reference"])
        self.assertFalse(result["native_needs_go"])

    def test_quick_critical_unknown_and_source_changes_fall_back(self):
        modules = runner.discover()
        complete = set(plan.select([], modules)["native_modules"])
        for path in ("selfhost/pipeline/compiler.mio", "bunko/kiso/runtime/gc/mock_test.mio",
                     "bunko/kiso/aot/arm64/arm64_test.mio", "bunko/kiso/math/math.mio",
                     "bunko/kiso/math/ci-module.json", "go.mod", ".github/workflows/ci-pr.yml",
                     "bunko/kiso/new/removed_test.mio", "unknown.file"):
            with self.subTest(path=path):
                result = plan.select([path], modules)
                self.assertEqual(result["mode"], "full-quick")
                self.assertEqual(set(result["native_modules"]), complete)
        result = plan.select(["docs/testing.md"], modules, "diff unavailable")
        self.assertEqual(result["mode"], "full-quick")

    def test_new_modules_are_included_without_central_registration(self):
        modules = runner.discover()
        modules["new-native"] = dict(module="new-native", targets=["macos-arm64"], suites=["quick"], script="new/test.sh")
        modules["new-reference"] = dict(module="new-reference", engine="reference", targets=["linux-amd64"], suites=["quick"], script="new/test.sh")
        result = plan.select(["new/source.file"], modules)
        self.assertIn("new-native", result["native_modules"])
        self.assertIn("new-reference", result["reference_modules"])

    def test_diff_failure_never_becomes_docs_only(self):
        def git(*args):
            if args[0] == "diff":
                raise subprocess.CalledProcessError(1, args)
            return b"a" * 40
        with patch.object(plan, "git", side_effect=git):
            result = plan.make_plan("base", "head")
        self.assertEqual(result["mode"], "full-quick")
        self.assertIn("unreliable", result["reason"])

    def test_required_quick_job_cannot_skip_cancel_or_fail_to_green(self):
        result = plan.select(["go.mod"], runner.discover())
        for job_result in ("skipped", "cancelled", "failure", "", "pending"):
            with self.subTest(result=job_result):
                self.assertNotEqual(plan.gate(result, "success", job_result, "success")["status"], "PASS")
                self.assertNotEqual(plan.gate(result, job_result, "success", "success")["status"], "PASS")
                self.assertNotEqual(plan.gate(result, "success", "success", job_result)["status"], "PASS")
        self.assertEqual(plan.gate(result, "success", "skipped", "success")["status"], "NOT_RUN")
        self.assertEqual(plan.gate(result, "success", "success", "success")["status"], "PASS")
        with self.assertRaises(ValueError):
            plan.gate({}, "success", "success", "success")

    def test_multi_module_failure_does_not_hide_other_module(self):
        modules = {name: dict(module=name, engine="infrastructure", targets=["linux-amd64"], suites=["quick"], script="fake.sh") for name in ("one", "two")}
        def execute(argv, timeout, env):
            state = "FAIL" if env["MIO_CI_MODULE_DIR"].endswith("one") else "PASS"
            Path(env["MIO_CI_CHECKS"]).write_text(json.dumps(dict(name="check", status=state)) + "\n")
            return 1 if state == "FAIL" else 0, b"", b"", state, "", 1
        code, report = self.dispatch(modules, selected="one,two", execute=execute)
        self.assertEqual((code, report["status"]), (1, "FAIL"))
        self.assertEqual([m["status"] for m in report["checks"]], ["FAIL", "PASS"])
        code, report = self.dispatch(modules, selected="one,missing", execute=execute)
        self.assertEqual((code, report["status"]), (2, "NOT_RUN"))

    def test_successful_job_still_requires_scope_and_source_evidence(self):
        report = dict(status="PASS", exit_code=0, suite="quick", target="macos-arm64",
                      source_revision="current", selected_modules=["core"], checks=[dict(module="core", status="PASS")])
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            self.assertEqual(plan.evidence(directory, ["core"], "macos-arm64", "current")["status"], "NOT_RUN")
            (directory / "report.json").write_text(json.dumps(report))
            self.assertEqual(plan.evidence(directory, ["core"], "macos-arm64", "current")["status"], "PASS")
            self.assertEqual(plan.evidence(directory, ["core", "gc"], "macos-arm64", "current")["status"], "NOT_RUN")
            self.assertEqual(plan.evidence(directory, ["core"], "macos-arm64", "stale")["status"], "NOT_RUN")
            report["checks"][0]["status"] = "SKIP"
            (directory / "report.json").write_text(json.dumps(report))
            self.assertEqual(plan.evidence(directory, ["core"], "macos-arm64", "current")["status"], "NOT_RUN")

    def test_reference_tools_build_lazily_once_per_run(self):
        modules = {name: dict(module=name, engine="reference", targets=["linux-amd64"], suites=["quick"],
                             script="fake.sh", reference_tools=tools) for name, tools in
                   (("one", ["mio"]), ("two", ["mio"]), ("three", ["mio"]))}
        built = []
        def execute(argv, timeout, env):
            if argv[0] == "go":
                built.append(argv[-1])
            else:
                Path(env["MIO_CI_CHECKS"]).write_text('{"name":"reference","status":"PASS"}\n')
            return 0, b"", b"", "PASS", "", 1
        code, report = self.dispatch(modules, execute=execute)
        self.assertEqual((code, report["status"]), (0, "PASS"))
        self.assertEqual(built, ["./cmd/mio"])

    # ---- Native Verification deferral (GitHub-hosted runners have no trusted Native Seed) ----

    def test_native_verification_is_deferred_unless_exactly_enabled(self):
        with patch.dict(os.environ, {}, clear=False):
            os.environ.pop("MIO_NATIVE_VERIFICATION", None)
            self.assertEqual(plan.native_verification_state(), "deferred")
        for value in ("", "true", "1", "Enabled", "enabled ", "on"):
            self.assertEqual(plan.native_verification_state(value), "deferred", value)
        self.assertEqual(plan.native_verification_state("enabled"), "enabled")

    def test_deferred_quick_plan_runs_only_supplementary_modules_and_records_native_not_run(self):
        modules = runner.discover()
        complete = plan.select(["go.mod"], modules, native_verification="enabled")
        deferred = plan.select(["go.mod"], modules, native_verification="deferred")
        self.assertEqual(deferred["native_modules"], [])
        self.assertEqual(deferred["deferred_native_modules"], complete["native_modules"])
        self.assertIn("go-reference", deferred["macos_modules"])
        for name in deferred["macos_modules"] + deferred["reference_modules"]:
            self.assertNotEqual(modules[name].get("engine", "native"), "native", name)
        statuses = {m["module"]: m for m in deferred["modules"]}
        for name in deferred["deferred_native_modules"]:
            self.assertEqual(statuses[name]["status"], "NOT_RUN")
            self.assertIn("Native Verification is deferred", statuses[name]["reason"])
        self.assertEqual(complete["macos_modules"], [])

    def test_deferred_gate_passes_on_supplementary_checks_but_never_reports_native_pass(self):
        result = plan.select(["go.mod"], runner.discover(), native_verification="deferred")
        report = plan.gate(result, "success", "skipped", "success", "success")
        self.assertEqual(report["status"], "PASS")
        self.assertEqual(report["native_verification"]["status"], "NOT_RUN")
        self.assertIn("does not mean Mio native tests passed", report["note"])
        for job_result in ("failure", "cancelled", "skipped", ""):
            self.assertNotEqual(plan.gate(result, "success", "skipped", "success", job_result)["status"], "PASS")
            self.assertNotEqual(plan.gate(result, "success", "skipped", job_result, "success")["status"], "PASS")
            self.assertNotEqual(plan.gate(result, job_result, "skipped", "success", "success")["status"], "PASS")
        self.assertEqual(plan.gate(result, "success", "skipped", "success", "failure")["status"], "FAIL")
        # A native job result cannot turn the deferred native state into a pass.
        self.assertEqual(plan.gate(result, "success", "success", "success", "success")["native_verification"]["status"], "NOT_RUN")

    def test_enabled_gate_still_requires_the_native_job(self):
        result = plan.select(["go.mod"], runner.discover(), native_verification="enabled")
        self.assertEqual(plan.gate(result, "success", "success", "success")["status"], "PASS")
        for job_result in ("skipped", "cancelled", ""):
            self.assertEqual(plan.gate(result, "success", job_result, "success")["status"], "NOT_RUN")
        self.assertEqual(plan.gate(result, "success", "failure", "success")["status"], "FAIL")
        self.assertEqual(plan.gate(result, "success", "success", "success")["native_verification"]["status"], "PASS")

    def test_full_plan_defers_native_modules_and_keeps_every_other_module(self):
        modules = runner.discover()
        deferred = plan.full_plan(modules, "deferred")
        enabled = plan.full_plan(modules, "enabled")
        self.assertEqual((enabled["linux_modules"], enabled["macos_modules"]), ("all", "all"))
        self.assertEqual(deferred["native_verification"]["status"], "NOT_RUN")
        self.assertEqual(deferred["deferred_native_modules"], enabled["executed_native_modules"])
        self.assertIn("bootstrap", deferred["deferred_native_modules"])
        for target, field in (("linux-amd64", "linux_modules"), ("macos-arm64", "macos_modules")):
            chosen = deferred[field].split(",")
            expected = [n for n, m in modules.items() if target in m["targets"] and "full" in m["suites"]
                        and m.get("engine", "native") != "native" and not m.get("standalone")]
            self.assertEqual(sorted(chosen), sorted(expected))
            for name in chosen:
                self.assertNotEqual(modules[name].get("engine", "native"), "native")
        self.assertIn("go-reference", deferred["macos_modules"].split(","))

    def test_hosted_workflows_have_no_self_hosted_or_shared_macos_dependency(self):
        root = HERE.parents[1] / ".github" / "workflows"
        texts = {name: (root / name).read_text(encoding="utf-8") for name in ("ci-pr.yml", "ci-full.yml", "release.yml")}
        for name in ("ci-pr.yml", "ci-full.yml"):
            text = texts[name]
            for forbidden in ("self-hosted", "mio-native", "MIO_PR_RUNNER_READY", "ci-resource-macos-arm64-shared", "continue-on-error"):
                self.assertNotIn(forbidden, text, name + " contains " + forbidden)
            self.assertIn("macos-15", text)
        # Release keeps its strict persistent-host pipeline.
        self.assertIn("self-hosted", texts["release.yml"])
        self.assertIn("MIO_NATIVE_SEED", texts["release.yml"])

    def test_workflow_artifact_names_are_unique_and_run_scoped(self):
        import re
        root = HERE.parents[1] / ".github" / "workflows"
        for name in ("ci-pr.yml", "ci-full.yml"):
            names = re.findall(r"upload-artifact@v4\n(?:\s+if: .*\n)?\s+with:\n\s+name: (.*)\n",
                               (root / name).read_text(encoding="utf-8"))
            self.assertTrue(names, name)
            self.assertEqual(len(names), len(set(names)), name)
            for artifact in names:
                self.assertIn("github.run_id", artifact)
                self.assertIn("github.run_attempt", artifact)

    # ---- Full-run failures observed on GitHub-hosted runners (run 41825096644) ----

    def test_timeout_kill_survives_macos_eperm_for_zombie_only_groups(self):
        # macOS answers EPERM instead of ESRCH when only unreaped zombies remain in the process group.
        real = os.killpg
        calls = []
        def killpg(group, number):
            calls.append(number)
            real(group, number)
            if number == runner.signal.SIGKILL:
                raise PermissionError(1, "Operation not permitted")
        with patch.object(runner.os, "killpg", side_effect=killpg), patch.dict(os.environ, {"MIO_CI_CHECKS": "x"}):
            code, _, _, status, reason, _ = runner.execute([sys.executable, "-c", "import time; time.sleep(30)"], 0.2)
        self.assertEqual((status, reason), ("FAIL", "timeout"))
        self.assertEqual(calls[0], runner.signal.SIGTERM)

    def test_go_checks_do_not_inherit_the_harness_only_library_root(self):
        with patch.dict(os.environ, {"MIO_BUNKO": "/repo/bunko", "KEEP": "1"}):
            for kind in ("go", "go-required"):
                environment = runner.check_environment(kind)
                self.assertNotIn("MIO_BUNKO", environment)
                self.assertEqual(environment["KEEP"], "1")
            for kind in ("mio", "command"):
                self.assertIsNone(runner.check_environment(kind))

    def test_standalone_module_is_not_part_of_all_but_stays_selectable(self):
        modules = {"a": dict(module="a", engine="infrastructure", targets=["linux-amd64"], suites=["full"], script="a.sh"),
                   "long": dict(module="long", engine="reference", targets=["linux-amd64"], suites=["full"], script="long.sh",
                                reference_tools=[], standalone=True)}
        executed = []
        def execute(argv, timeout, env):
            executed.append(argv[1])
            Path(env["MIO_CI_CHECKS"]).write_text('{"name":"check","status":"PASS"}\n')
            return 0, b"", b"", "PASS", "", 1
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, MIO_CI_RESULTS=directory), \
             patch.object(runner, "discover", return_value=modules), \
             patch.object(runner.platform, "system", return_value="Linux"), \
             patch.object(runner.platform, "machine", return_value="x86_64"), \
             patch.object(runner.shutil, "which", return_value="tool"), \
             patch.object(runner, "execute", side_effect=execute):
            self.assertEqual(runner.run(["linux-amd64", "all", "full"]), 0)
            report = json.loads(sorted(Path(directory).glob("*/report.json"))[-1].read_text())
            states = {item["module"]: item for item in report["checks"]}
            self.assertEqual(states["long"]["status"], "SKIP")
            self.assertIn("standalone", states["long"]["reason"])
            self.assertNotIn("long.sh", executed)
            self.assertEqual(runner.run(["linux-amd64", "long", "full"]), 0)
            self.assertIn("long.sh", executed)

    def test_bootstrap_has_its_own_job_and_module_and_leaves_the_repository_check(self):
        root = HERE.parents[1]
        modules = runner.discover()
        module = modules["go-bootstrap-reference"]
        self.assertTrue(module["standalone"])
        self.assertEqual(module["suites"], ["full"])
        self.assertEqual(sorted(module["targets"]), ["linux-amd64", "macos-arm64"])
        self.assertGreaterEqual(module["module_timeout"], 3 * 3600)
        script = (root / "internal" / "bootstrap-reference" / "test.sh").read_text(encoding="utf-8")
        self.assertIn("./selfhost/bootstrap", script)
        self.assertIn("-timeout 175m", script)
        shared = (root / "internal" / "test.sh").read_text(encoding="utf-8")
        self.assertIn("grep -v '^Mio/selfhost/bootstrap$'", shared)
        # macos-arm64 runs the module under the system bash 3.2: no bash 4 builtins.
        self.assertNotIn("mapfile", shared)
        self.assertNotIn("readarray", shared)
        self.assertNotIn("go test -json -p 2 ./... ", shared)
        workflow = (root / ".github" / "workflows" / "ci-full.yml").read_text(encoding="utf-8")
        self.assertIn("go-bootstrap-reference full", workflow)
        self.assertIn("timeout-minutes: 210", workflow)
        plan_result = plan.full_plan(modules, "deferred")
        self.assertNotIn("go-bootstrap-reference", plan_result["linux_modules"].split(","))
        self.assertNotIn("go-bootstrap-reference", plan_result["macos_modules"].split(","))
        self.assertIn("go-bootstrap-reference", plan_result["standalone_modules"]["linux-amd64"])

    def test_go_aot_backends_and_their_modules_are_gone(self):
        # Mio-2.1 is self-hosted in pure Mio: no Go program generates native code, and no CI module depends on one.
        root = HERE.parents[1]
        for path in ("internal/aot", "cmd/mochi-aot", "cmd/mochi-aot-audit", "scripts/ci/legacy",
                     "scripts/check-native-aot-fixed-point.py", "scripts/check-native-bootstrap.sh"):
            self.assertFalse((root / path).exists(), path)
        modules = runner.discover()
        for name in ("go-aot-reference", "legacy-bootstrap", "go-aot-linux-amd64", "go-linux-native", "linux-runtime-reference"):
            self.assertNotIn(name, modules)
        for module in modules.values():
            self.assertTrue(set(module.get("reference_tools", ["mio"])) <= {"mio"}, module["module"])

    # ---- Native 3-platform verification (scripts/ci/native_platforms.py, .github/workflows/native.yml) ----

    def test_native_workflow_never_hides_failures_and_names_the_hosted_runners(self):
        import re
        root = HERE.parents[1] / ".github" / "workflows"
        text = (root / "native.yml").read_text(encoding="utf-8")
        for forbidden in ("continue-on-error", "|| true", "self-hosted", "MIO_NATIVE_SEED"):
            self.assertNotIn(forbidden, text)
        for runner_name in ("ubuntu-24.04", "macos-15", "windows-2025"):
            self.assertIn(runner_name, text)
        names = re.findall(r"upload-artifact@v4\n(?:\s+if: .*\n)?\s+with:\n\s+name: (.*)\n", text)
        self.assertGreaterEqual(len(names), 4)
        self.assertEqual(len(names), len(set(names)))
        for artifact in names:
            self.assertIn("github.run_id", artifact)
            self.assertIn("github.run_attempt", artifact)
        full = (root / "ci-full.yml").read_text(encoding="utf-8")
        self.assertIn("uses: ./.github/workflows/native.yml", full)

    def native_report(self, directory, target="linux-amd64"):
        return native.Report(target, directory)

    def fake_run(self, text, code=0):
        def run(report, label, argv, **kwargs):
            return code, text, "", 0.1
        return run

    def test_native_mio_test_never_counts_a_skip_as_a_pass(self):
        skipped = "--- SKIP: test_x (0.1s)\n    skipped: Mach-O test image needs a macOS ARM64 host\nTests complete: 0 passed, 0 failed\nTests skipped: 1\n"
        with tempfile.TemporaryDirectory() as directory, patch.object(native, "test_packages", return_value=["pkg"]):
            report = self.native_report(directory)
            with patch.object(native, "run", side_effect=self.fake_run(skipped)):
                native.native_mio_test(report, "mio", skips_allowed=True)
            self.assertEqual([(c["status"], c["required"]) for c in report.checks], [("SKIP", False)])
            self.assertIn("macOS ARM64", report.checks[0]["reason"])
            report = self.native_report(directory, "darwin-arm64")
            with patch.object(native, "run", side_effect=self.fake_run(skipped)):
                self.assertFalse(native.native_mio_test(report, "mio", skips_allowed=False) and report.data["status"] == "PASS")
            self.assertEqual(report.data["status"], "FAIL")

    def test_native_mio_test_requires_a_complete_passing_summary(self):
        cases = [("Tests complete: 3 passed, 0 failed\n", 0, "PASS"), ("Tests complete: 2 passed, 1 failed\n", 1, "FAIL"),
                 ("compilation failed\n", 1, "FAIL"), ("", 0, "FAIL"), ("Tests complete: 0 passed, 0 failed\n", 0, "FAIL")]
        for text, code, expected in cases:
            with tempfile.TemporaryDirectory() as directory, patch.object(native, "test_packages", return_value=["pkg"]):
                report = self.native_report(directory)
                with patch.object(native, "run", side_effect=self.fake_run(text, code)):
                    native.native_mio_test(report, "mio", skips_allowed=True)
                self.assertEqual(report.checks[0]["status"], expected, text)

    def test_native_summary_fails_on_missing_or_failed_platform_reports(self):
        with tempfile.TemporaryDirectory() as directory:
            reports = Path(directory) / "reports"
            for target, status in (("linux-amd64", "PASS"), ("darwin-arm64", "PASS")):
                report = native.Report(target, reports / ("native-report-" + target))
                report.check("x", status, "native")
            args = type("Args", (), dict(reports=str(reports), output=str(Path(directory) / "out"), run_examples=False))
            self.assertEqual(native.summary(args), 1)  # windows-amd64 is missing: NOT_RUN, never PASS
            report = native.Report("windows-amd64", reports / "native-report-windows-amd64")
            report.check("x", "PASS", "native")
            report.check("mio-test", "NOT_APPLICABLE", "native", "no spawn", required=False)
            self.assertEqual(native.summary(args), 0)
            args.run_examples = True
            self.assertEqual(native.summary(args), 1)  # examples were required but absent
            args.run_examples = False
            report.check("y", "NOT_RUN", "native", "missing")
            self.assertEqual(native.summary(args), 1)
            for data in json.loads((Path(directory) / "out" / "native-summary.json").read_text())["targets"].values():
                self.assertEqual(data["seed"]["status"], "NOT_RUN")

if __name__ == "__main__":
    unittest.main(verbosity=2)
