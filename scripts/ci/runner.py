#!/usr/bin/env python3
"""Local CI dispatcher and check protocol, using only Python's standard library."""
import json
import hashlib
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import time
import uuid
import math
from contextlib import contextmanager

ROOT = Path(__file__).resolve().parents[2]
for extra_dir in (ROOT / "scripts/ci", ROOT / "scripts/distribution"):
    if str(extra_dir) not in sys.path:
        sys.path.insert(0, str(extra_dir))

from seed_evidence import NATIVE_V21, seed_identity
import seed_source

TARGETS = {"linux-amd64": ("Linux", "x86_64"), "macos-arm64": ("Darwin", "arm64")}
ACTIVE_SEED = seed_identity(NATIVE_V21)
SEED_SHA256 = ACTIVE_SEED.sha256


@contextmanager
def record_lock(path):
    """Serialize parallel check writers, including Windows text append."""
    with open(str(path) + ".lock", "a+b") as lock:
        if os.name == "nt":
            import msvcrt
            lock.write(b"\0")
            lock.flush()
            lock.seek(0)
            msvcrt.locking(lock.fileno(), msvcrt.LK_LOCK, 1)
        else:
            import fcntl
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            if os.name == "nt":
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def execute(argv, timeout, env=None):
    """Capture both streams and kill the entire process group on POSIX timeout."""
    started = time.monotonic()
    try:
        proc = subprocess.Popen(list(map(str, argv)), cwd=ROOT, env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                start_new_session=os.name != "nt")
    except OSError as error:
        return 2, b"", str(error).encode(), "NOT_RUN", str(error), 0
    reason = ""
    def kill_tree():
        if os.name == "nt":
            subprocess.run(["taskkill", "/PID", str(proc.pid), "/T", "/F"],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if proc.poll() is None:
                proc.kill()
        else:
            def signal_group(number):
                try:
                    os.killpg(proc.pid, number)
                    return True
                except ProcessLookupError:
                    return False
                except PermissionError:
                    # macOS answers EPERM, not ESRCH, when the only members left in the group are zombies
                    # that have not been reaped yet (the leader exited on SIGTERM). Nothing alive remains to
                    # kill; any live descendant in the group would have made the call succeed.
                    return False
            if signal_group(signal.SIGTERM):
                # Check runners forward TERM to their own subprocess group.
                time.sleep(0.05 if os.environ.get("MIO_CI_CHECKS") else 0.5)
                signal_group(signal.SIGKILL)
    def interrupted(signum, frame):
        kill_tree()
        proc.communicate()
        raise SystemExit(128 + signum)
    previous = signal.signal(signal.SIGTERM, interrupted)
    try:
        out, err = proc.communicate(timeout=timeout)
        code = proc.returncode
        status = "PASS" if code == 0 else "FAIL"
    except subprocess.TimeoutExpired:
        kill_tree()
        out, err = proc.communicate()
        code, status, reason = 1, "FAIL", "timeout"
    except KeyboardInterrupt:
        kill_tree()
        proc.communicate()
        raise
    finally:
        signal.signal(signal.SIGTERM, previous)
    if status == "FAIL" and not reason:
        reason = "process exited with code " + str(code)
    return code, out, err, status, reason, round((time.monotonic() - started) * 1000)


def assess(kind, code, out, status, reason):
    details = []
    if status != "PASS":
        if status == "FAIL" and kind in ("go", "go-required", "mio"):
            _, _, details = assess(kind, 0, out, "PASS", "")
        return status, reason, details
    try:
        if kind in ("go", "go-required"):
            events = [json.loads(line) for line in out.splitlines() if line.strip()]
            details = [{"name": e["Package"] + "/" + e["Test"],
                        "status": {"pass": "PASS", "fail": "FAIL", "skip": "SKIP"}[e["Action"]],
                        "reason": "Go test skip; see log" if e["Action"] == "skip" else ""}
                       for e in events if e.get("Test") and e.get("Action") in ("pass", "fail", "skip")]
            if any(e.get("Action") == "fail" for e in events):
                return "FAIL", "Go reported a failed test", details
            if kind == "go-required" and any(e.get("Action") == "skip" for e in events):
                return "NOT_RUN", "required Go tests were skipped", details
            if not any(d["status"] == "PASS" for d in details):
                return "NOT_RUN", "no Go test executed and passed", details
        elif kind == "mio":
            report = json.loads(out)
            tests = [t for p in report["packages"] for t in p["tests"]]
            details = [{"name": t.get("name", "unnamed"), "status":
                        {"passed": "PASS", "failed": "FAIL", "skipped": "SKIP"}.get(t["status"], "NOT_RUN")}
                       for t in tests]
            if report["summary"]["failed"] or any(t["status"] not in ("passed", "skipped") for t in tests):
                return "FAIL", "Mio reported failure", details
            if any(t["status"] == "skipped" for t in tests):
                return "NOT_RUN", "required Mio tests were skipped", details
            if not tests or report["summary"]["passed"] != len(tests):
                return "NOT_RUN", "no tests or inconsistent Mio count", details
        elif kind != "command":
            return "NOT_RUN", "unknown check kind", details
    except (ValueError, KeyError, TypeError, AttributeError) as error:
        return "NOT_RUN", "invalid test report: " + str(error), details
    return "PASS", reason, details


def timeout_value(name, default):
    value = float(os.environ.get(name, default))
    if not math.isfinite(value) or value <= 0:
        raise ValueError(name + " must be a finite positive number")
    return value


def check_environment(kind):
    """Environment of a check's command.

    The runner exports MIO_BUNKO for the Mio commands it starts. Go tests run in a developer's environment, and
    MIO_BUNKO overrides the library root of the temporary workspaces the LSP tests create, so Go checks do not
    inherit it (the same tests pass locally without it).
    """
    if kind not in ("go", "go-required"):
        return None
    return {key: value for key, value in os.environ.items() if key != "MIO_BUNKO"}


def check(args):
    if len(args) < 3 or not os.environ.get("MIO_CI_CHECKS"):
        print("check requires an active module, kind, name and command", file=sys.stderr)
        return 2
    kind, name, *argv = args
    directory = Path(os.environ["MIO_CI_MODULE_DIR"])
    stem = uuid.uuid4().hex[:10]
    try:
        timeout = timeout_value("MIO_CI_CHECK_TIMEOUT", "2400")
    except ValueError as error:
        print(str(error), file=sys.stderr)
        return 2
    code, out, err, status, reason, duration = execute(argv, timeout, check_environment(kind))
    status, reason, details = assess(kind, code, out, status, reason)
    stdout, stderr = directory / (stem + ".stdout.log"), directory / (stem + ".stderr.log")
    stdout.write_bytes(out)
    stderr.write_bytes(err)
    record = dict(name=name, kind=kind, layer={"mio": "native", "go": "supplementary", "go-required": "supplementary"}.get(kind, "independent"), status=status, reason=reason, duration_ms=duration,
                  exit_code=code, command=argv, tests=details,
                  artifacts=[str(stdout), str(stderr)])
    with record_lock(os.environ["MIO_CI_CHECKS"]):
        with open(os.environ["MIO_CI_CHECKS"], "a", encoding="utf-8") as stream:
            stream.write(json.dumps(record) + "\n")
    print(f"{status}: {name} ({duration} ms)", flush=True)
    if status != "PASS":
        print((out + err).decode(errors="replace")[-12000:], file=sys.stderr)
    return 0 if status == "PASS" else (2 if status == "NOT_RUN" else 1)


def discover():
    modules = {}
    for base in (ROOT / "internal", ROOT / "bunko", ROOT / "selfhost", ROOT / "tools", ROOT / "scripts/ci"):
        for manifest in sorted(base.rglob("ci-module.json")):
            data = json.loads(manifest.read_text(encoding="utf-8"))
            name = data["module"]
            if (not isinstance(name, str) or not name or
                    any(part in ("", ".", "..") for part in name.split("/")) or
                    any(c not in "abcdefghijklmnopqrstuvwxyz0123456789-/" for c in name)):
                raise ValueError("invalid module ID: " + str(name))
            for field in ("targets", "suites"):
                if not isinstance(data[field], list) or not data[field] or not all(isinstance(v, str) for v in data[field]):
                    raise ValueError("invalid module " + field + ": " + name)
            if any(target not in TARGETS for target in data["targets"]):
                raise ValueError("unknown module target: " + name)
            for field in ("required_checks", "requires"):
                if not isinstance(data.get(field, []), list) or not all(isinstance(v, str) and v for v in data.get(field, [])):
                    raise ValueError("invalid module " + field + ": " + name)
            if not isinstance(data.get("standalone", False), bool):
                raise ValueError("invalid standalone flag: " + name)
            if "module_timeout" in data and (isinstance(data["module_timeout"], bool) or
                    not isinstance(data["module_timeout"], (int, float)) or not 0 < data["module_timeout"] <= 21600):
                raise ValueError("invalid module_timeout: " + name)
            if "reference_tools" in data and (not isinstance(data["reference_tools"], list) or
                    any(tool != "mio" for tool in data["reference_tools"])):
                raise ValueError("invalid reference_tools: " + name)
            suite_checks = data.get("suite_checks", {})
            if (not isinstance(suite_checks, dict) or
                    any(suite not in data["suites"] or not isinstance(names, list) or not all(isinstance(v, str) and v for v in names)
                        for suite, names in suite_checks.items())):
                raise ValueError("invalid suite_checks: " + name)
            if data.get("engine", "native") not in ("native", "reference", "infrastructure"):
                raise ValueError("unknown engine: " + name)
            if name in modules or not (manifest.parent / "test.sh").is_file():
                raise ValueError("duplicate module or missing test.sh: " + name)
            data["script"] = str(manifest.parent / "test.sh")
            modules[name] = data
    # A recursive parent test is useful, but must not conceal an unregistered
    # independently testable Bunko package introduced by a PR.
    for test in sorted((ROOT / "bunko").rglob("*_test.mio")):
        if not (test.parent / "ci-module.json").is_file():
            raise ValueError("unregistered Mio test package: " + str(test.parent.relative_to(ROOT)))
    for module in modules.values():
        parent_name = module.get("covered_by")
        if parent_name:
            parent = modules.get(parent_name)
            if (not parent or parent.get("engine", "native") != "native" or
                    module.get("engine", "native") != "native" or
                    Path(parent["script"]).parent not in Path(module["script"]).parent.parents):
                raise ValueError("invalid covered_by: " + module["module"])
    return dict(sorted(modules.items(), key=lambda pair: (pair[1].get("engine", "native") != "native", pair[0])))


def delegated(module, modules, target, suite):
    parent = modules.get(module.get("covered_by", ""))
    required = module.get("required_checks", []) + module.get("suite_checks", {}).get(suite, [])
    # Only the recursive Mio layer is delegated. Independent or suite-specific
    # oracles must still execute through the child's own entry point.
    return bool(parent and target in parent["targets"] and suite in parent["suites"]
                and required == ["native"])


def inventory():
    modules = discover()
    return [dict(module=m["module"], engine=m.get("engine", "native"),
                 script=str(Path(m["script"]).relative_to(ROOT)),
                 targets=m["targets"], suites=m["suites"],
                 standalone=bool(m.get("standalone", False)),
                 schedule={target: {suite: ("SKIP" if target not in m["targets"] or suite not in m["suites"]
                            else "STANDALONE" if m.get("standalone") else "DELEGATED" if delegated(m, modules, target, suite) else "RUN")
                            for suite in ("quick", "full")} for target in TARGETS})
            for m in modules.values()]


def run(args):
    started = time.monotonic()
    directory = Path(os.environ.get("MIO_CI_RESULTS", ROOT / "build/ci-results")).expanduser().resolve() / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:8])
    try:
        directory.mkdir(parents=True)
    except OSError as error:
        print("cannot create result directory: " + str(error), file=sys.stderr)
        return 2
    report = dict(schema_version=1, target=args[0] if args else "", module=args[1] if len(args) > 1 else "",
                  suite=args[2] if len(args) > 2 else "", status="NOT_RUN", checks=[], builds=[], artifacts=[str(directory)])
    report["source_revision"] = "UNKNOWN"
    report["source_worktree_dirty"] = None
    exit_code = 2
    try:
        if shutil.which("git"):
            revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, capture_output=True, timeout=10)
            dirty = subprocess.run(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=ROOT, capture_output=True, timeout=10)
            if revision.returncode == 0 and dirty.returncode == 0:
                report["source_revision"] = revision.stdout.decode().strip()
                report["source_worktree_dirty"] = bool(dirty.stdout.strip())
        if len(args) != 3:
            raise ValueError("usage: bash scripts/ci/run.sh <linux-amd64|macos-arm64> <module[,module...]|all> <quick|full|all|module-suite>")
        target, selected, suite = args
        module_timeout = timeout_value("MIO_CI_MODULE_TIMEOUT", "7200")
        timeout_value("MIO_CI_CHECK_TIMEOUT", "2400")
        suite = "full" if suite == "all" else suite
        if target not in TARGETS or TARGETS[target] != (platform.system(), platform.machine().lower()):
            raise ValueError("unsupported target or host mismatch: " + target)
        modules = discover()
        names = selected.split(",")
        if selected != "all" and (len(set(names)) != len(names) or any(name not in modules for name in names)):
            raise ValueError("unknown module: " + selected)
        chosen = list(modules.values()) if selected == "all" else [m for m in modules.values() if m["module"] in names]
        report["selected_modules"] = [m["module"] for m in chosen]
        report["checks"] = [dict(module=m["module"], engine=m.get("engine", "native"), status="NOT_RUN", reason="module not started", duration_ms=0, checks=[], artifacts=[]) for m in chosen]
        if selected == "all" and suite not in ("quick", "full"):
            raise ValueError("all modules require quick, full or all")
        for module in chosen:
            if target not in module["targets"] or suite not in module["suites"]:
                if selected != "all":
                    raise ValueError("unsupported target/module/suite combination")
        for tool in ("bash", "git"):
            if not shutil.which(tool):
                raise ValueError("required tool missing: " + tool)
        env = dict(os.environ, PYTHON=sys.executable, MIO_CI_ACTIVE="1", MIO_BUNKO=str(ROOT / "bunko"))
        env["MIO_CI_TOOLS"] = str(directory / "tools")
        # Reference binaries are development-only, never used to build a release.
        Path(env["MIO_CI_TOOLS"]).mkdir()
        reference_build = {}
        native_build = None
        native_compiler = Path(env["MIO_CI_TOOLS"]) / "mio-native"
        seed_env = os.environ.get("MIO_NATIVE_SEED")
        seed = Path(seed_env) if seed_env is not None else (ROOT / "bootstrap/seeds/native-v21" / ACTIVE_SEED.asset_name)
        seed_valid = None
        exit_code = 0
        for module, item in zip(chosen, report["checks"]):
            if target not in module["targets"] or suite not in module["suites"]:
                item.update(status="SKIP", reason=module.get("skip_reason", "not supported for this target/suite; see testing.md"))
                continue
            if selected == "all" and module.get("standalone"):
                item.update(status="SKIP", reason="standalone long-running module: not part of `all`; it runs in its own CI job "
                            "or when named explicitly (see docs/testing.md)")
                continue
            if module.get("covered_by") in report["selected_modules"] and delegated(module, modules, target, suite):
                item.update(status="SKIP", reason="native tests delegated to recursive package check: " + module["covered_by"])
                continue
            work = directory / module["module"].replace("/", "-")
            work.mkdir()
            checks = work / "checks.jsonl"
            module_env = dict(env, MIO_CI_MODULE_DIR=str(work), MIO_CI_CHECKS=str(checks))
            engine = module.get("engine", "native")
            required = module.get("required_checks", []) + module.get("suite_checks", {}).get(suite, [])
            dependencies = module.get("requires", ["codesign"] if engine == "native" else (["go"] if engine == "reference" else []))
            missing = [tool for tool in dependencies if not shutil.which(tool)]
            if missing:
                item.update(status="NOT_RUN", reason="required tools missing: " + ", ".join(missing),
                            checks=[dict(name=name, status="NOT_RUN", reason="dependencies missing") for name in required])
                exit_code = 2
                continue
            if engine == "native":
                if seed_valid is None:
                    seed_valid = seed.is_file() and sha256(seed) == SEED_SHA256
                    if seed_valid and SEED_SHA256 == ACTIVE_SEED.sha256:
                        try:
                            seed_source.verify_seed_file(
                                seed, ACTIVE_SEED,
                                check_arch=(platform.system() == "Darwin" and platform.machine().lower() in ("arm64", "aarch64")),
                                check_signature=(platform.system() == "Darwin"),
                            )
                        except (seed_source.SeedUnavailable, ValueError, OSError):
                            seed_valid = False
                if not seed_valid:
                    item.update(status="NOT_RUN", reason="verified MIO_NATIVE_SEED missing or Native V21 SHA256 mismatch",
                                checks=[dict(name=name, status="NOT_RUN", reason="native compiler unavailable") for name in required])
                    exit_code = 2
                    continue
                if native_build is None:
                    argv = [str(seed.resolve()), "build", "selfhost/cli", "--bunko-root", str(ROOT / "bunko"), "-o", str(native_compiler)]
                    code, out, err, status, reason, duration = execute(argv, module_timeout, env)
                    log = directory / "native-compiler-build.log"
                    log.write_bytes(out + err)
                    if status == "PASS" and (not native_compiler.is_file() or native_compiler.stat().st_size == 0):
                        status, reason = "FAIL", "compiler build produced no binary"
                    native_build = dict(name="current-source-native-compiler", command=argv, status=status,
                                        reason=reason, duration_ms=duration, artifacts=[str(log)],
                                        seed_kind=NATIVE_V21, seed_sha256=SEED_SHA256)
                    if status == "PASS":
                        native_build["compiler_sha256"] = sha256(native_compiler)
                    report["builds"].append(native_build)
                if native_build["status"] != "PASS":
                    item.update(status="NOT_RUN", reason="current-source compiler build failed; see builds",
                                checks=[dict(name=name, status="NOT_RUN", reason="compiler unavailable") for name in required])
                    exit_code = max(exit_code, 1 if native_build["status"] == "FAIL" else 2)
                    continue
                module_env["MIO_CI_MIO"] = str(native_compiler)
                module_env["MIO_CI_SEED"] = str(seed.resolve())
                item["compiler"] = dict(path=str(native_compiler), sha256=native_build["compiler_sha256"],
                                        seed_kind=NATIVE_V21, seed_sha256=SEED_SHA256)
            elif engine == "reference":
                tools = module.get("reference_tools", ["mio"])
                for tool in tools:
                    if tool not in reference_build:
                        argv = ["go", "build", "-o", str(Path(env["MIO_CI_TOOLS"]) / tool), "./cmd/" + tool]
                        code, out, err, status, reason, duration = execute(argv, 300, env)
                        log = directory / (tool + "-build.log")
                        log.write_bytes(out + err)
                        build = dict(name="reference-" + tool, status=status, reason=reason, command=argv, duration_ms=duration, artifacts=[str(log)])
                        reference_build[tool] = build
                        report["builds"].append(build)
                        if status != "PASS":
                            break
                    elif reference_build[tool]["status"] != "PASS":
                        break
                if any(tool not in reference_build or reference_build[tool]["status"] != "PASS" for tool in tools):
                    item.update(status="NOT_RUN", reason="reference tool build failed; see builds",
                                checks=[dict(name=name, status="NOT_RUN", reason="reference compiler unavailable") for name in required])
                    exit_code = max(exit_code, 1 if any(b["status"] == "FAIL" for b in reference_build.values()) else 2)
                    continue
            code, out, err, status, reason, duration = execute(["bash", module["script"], target, suite], module.get("module_timeout", module_timeout), module_env)
            (work / "module.log").write_bytes(out + err)
            print((out + err).decode(errors="replace"), end="", flush=True)
            try:
                records = [json.loads(line) for line in checks.read_text(encoding="utf-8").splitlines()] if checks.exists() else []
                if any(not isinstance(r, dict) or r.get("status") not in ("PASS", "FAIL", "SKIP", "NOT_RUN") or not r.get("name") for r in records):
                    raise ValueError("invalid check record")
            except (ValueError, TypeError) as error:
                records = [dict(name="check-protocol", status="NOT_RUN", reason=str(error))]
                code = 2
            for name in required:
                if not any(record["name"] == name for record in records):
                    records.append(dict(name=name, status="NOT_RUN", reason="required layer did not execute"))
            if engine == "native" and not any(r.get("kind") == "mio" and r.get("layer") == "native" for r in records):
                records.append(dict(name="native-protocol", status="NOT_RUN", reason="native module did not execute a Mio check"))
            item.update(checks=records, duration_ms=duration, exit_code=code, artifacts=[str(work / "module.log")])
            if code or any(c["status"] == "FAIL" for c in records):
                item.update(status="FAIL" if any(c["status"] == "FAIL" for c in records) or code != 2 else "NOT_RUN", reason=reason)
                exit_code = max(exit_code, 2 if code == 2 else 1)
            elif not records or any(c["status"] != "PASS" for c in records):
                item.update(status="NOT_RUN", reason="module did not complete required checks")
                exit_code = 2
            else:
                item["status"] = "PASS"
                item.pop("reason", None)
        if not any(item["status"] == "PASS" for item in report["checks"]) and exit_code == 0:
            exit_code = 2
            report["reason"] = "no module actually executed and passed"
    except (ValueError, OSError, KeyError, TypeError, subprocess.TimeoutExpired, KeyboardInterrupt, SystemExit) as error:
        report.update(status="NOT_RUN", reason=str(error))
        if isinstance(error, (KeyboardInterrupt, SystemExit)):
            report["reason"] = "CI interrupted before completing required modules"
        print(str(error), file=sys.stderr)
        exit_code = 2
    finally:
        failed = any(item["status"] == "FAIL" or any(c["status"] == "FAIL" for c in item.get("checks", [])) for item in report["checks"])
        failed = failed or any(b["status"] == "FAIL" for b in report["builds"])
        report["status"] = "FAIL" if failed else ("PASS" if exit_code == 0 else "NOT_RUN")
        report["exit_code"] = exit_code
        report["summary"] = {status: sum(item["status"] == status for item in report["checks"]) for status in ("PASS", "FAIL", "SKIP", "NOT_RUN")}
        report["duration_ms"] = round((time.monotonic() - started) * 1000)
        (directory / "report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print("Report: " + str(directory / "report.json"), flush=True)
    return exit_code


if __name__ == "__main__":
    if sys.argv[1:] == ["inventory"]:
        try:
            print(json.dumps(inventory(), indent=2))
        except (ValueError, OSError, KeyError, TypeError) as error:
            print("NOT_RUN: " + str(error), file=sys.stderr)
            sys.exit(2)
        sys.exit(0)
    if len(sys.argv) < 2 or sys.argv[1] not in ("run", "check"):
        sys.exit(2)
    sys.exit(run(sys.argv[2:]) if sys.argv[1] == "run" else check(sys.argv[2:]))
