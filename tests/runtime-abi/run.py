#!/usr/bin/env python3
"""Runtime ABI contract tests (RA-1, docs/runtime-abi.md) for one native target.

Every case in tests/runtime-abi/cases/*.mio is built by a native Mio CLI (`mio build --target=<target>`), executed on
the host and its stdout compared byte for byte with cases/<name>.expected. Each case also checks its own invariants
and ends with "<name>: ok", so an expected file can never encode a failure. Modes:

  normal      the target's default collector configuration
  gc-stress   `mio build --gc-stress` (collection at (almost) every allocation / tiny threshold)

`--reference <refvm>` additionally runs every case in the Go reference VM (tests/windows-amd64/refvm, which prints only
the program's output, unlike the REPL-style `mio run`) and requires the same transcript:
that is the VM / Native differential and is recorded separately (engine "reference").

Results (PASS / FAIL / NOT_RUN per case and mode) go to <output>/results.json; nothing is ever reported as PASS that
was not built, executed and compared. --build-only (cross-target images) records execution as NOT_RUN.
Python only builds paths, starts processes and compares bytes; it generates no code.
"""
import argparse
import hashlib
import json
import os
import platform
import subprocess
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
SUITES = {"contract": HERE / "cases", "differential": ROOT / "tests" / "native-differential" / "cases"}
# Programs whose run time is quadratic under per-allocation collection (Darwin --gc-stress): default mode only, as in
# the former Go harness (TestLinuxAMD64PureMioLoweringUnderGCStress). Reported NOT_APPLICABLE, never PASS.
STRESS_EXCLUDED = {"differential": {"gc_garbage", "gc_keeps_live_data"}}
TARGETS = ("darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64")


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def host_target():
    system, machine = platform.system(), platform.machine().lower()
    if system == "Darwin" and machine in ("arm64", "aarch64"):
        return "darwin-arm64"
    if system == "Darwin" and machine in ("x86_64", "amd64"):
        return "darwin-amd64"
    if system == "Linux" and machine in ("x86_64", "amd64"):
        return "linux-amd64"
    if system == "Linux" and machine in ("aarch64", "arm64"):
        return "linux-arm64"
    if system == "Windows" and machine in ("amd64", "x86_64"):
        return "windows-amd64"
    if system == "Windows" and machine in ("arm64", "aarch64"):
        return "windows-arm64"
    return "%s-%s" % (system.lower(), machine)


def run(argv, timeout, cwd=None, env=None):
    started = time.monotonic()
    try:
        proc = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, timeout=timeout)
        return proc.returncode, proc.stdout, proc.stderr, time.monotonic() - started, False
    except subprocess.TimeoutExpired as error:
        return None, error.stdout or b"", error.stderr or b"", time.monotonic() - started, True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", required=True, help="native Mio CLI that builds the cases")
    parser.add_argument("--target", required=True, choices=TARGETS)
    parser.add_argument("--suite", default="contract", choices=sorted(SUITES))
    parser.add_argument("--output", required=True)
    parser.add_argument("--modes", default="normal,gc-stress")
    parser.add_argument("--cases", default="", help="comma separated subset (default: all)")
    parser.add_argument("--build-only", action="store_true", help="build, never execute (cross-target images)")
    parser.add_argument("--reference", default="", help="Go reference VM host (go build ./tests/windows-amd64/refvm) for the VM differential")
    parser.add_argument("--timeout", type=int, default=600)
    args = parser.parse_args()

    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    mio = Path(args.mio).resolve()
    CASES = SUITES[args.suite]
    names = sorted(p.stem for p in CASES.glob("*.mio"))
    if args.cases:
        wanted = args.cases.split(",")
        missing = [n for n in wanted if n not in names]
        if missing:
            sys.exit("unknown cases: %s" % ", ".join(missing))
        names = wanted
    host = host_target()
    execute = not args.build_only
    if execute and host != args.target:
        sys.exit("host %s cannot execute %s images; use --build-only" % (host, args.target))
    env = dict(os.environ, MIO_BUNKO=str(ROOT / "bunko"))
    suffix = ".exe" if args.target in ("windows-amd64", "windows-arm64") else ""
    results = []

    def record(name, mode, engine, status, reason="", **extra):
        item = dict(case=name, mode=mode, engine=engine, target=args.target, status=status, reason=reason)
        item.update(extra)
        results.append(item)
        print("%-8s %-18s %-10s %-9s %s" % (status, name, mode, engine, reason), flush=True)

    for name in names:
        source = CASES / (name + ".mio")
        expected_path = CASES / (name + ".expected")
        expected = expected_path.read_bytes() if expected_path.exists() else None
        # contract cases must succeed and end with "<name>: ok"; differential cases may end in a runtime error, whose
        # exit class (0 or nonzero, from the reference VM) is in <name>.exit
        exit_path = CASES / (name + ".exit")
        want_success = args.suite == "contract" or (exit_path.exists() and exit_path.read_text().strip() == "0")
        if expected is None or (args.suite == "contract" and not expected.endswith((name + ": ok\n").encode())) or \
                (args.suite != "contract" and not exit_path.exists()):
            record(name, "-", "contract", "FAIL", "missing expected transcript / exit class (contract cases must end with '%s: ok')" % name)
            continue
        for mode in args.modes.split(","):
            if mode == "gc-stress" and name in STRESS_EXCLUDED.get(args.suite, ()):
                record(name, mode, "native", "NOT_APPLICABLE", "excluded from gc-stress: quadratic under per-allocation collection")
                continue
            image = output / ("%s-%s%s" % (name, mode, suffix))
            argv = [str(mio), "build", str(source.relative_to(ROOT)), "--target=" + args.target, "-o", str(image)]
            if mode == "gc-stress":
                argv.append("--gc-stress")
            elif mode != "normal":
                sys.exit("unknown mode " + mode)
            code, out, err, seconds, timed_out = run(argv, args.timeout, cwd=ROOT, env=env)
            (output / ("%s-%s.build.log" % (name, mode))).write_bytes(out + err)
            if timed_out or code != 0 or not image.exists():
                record(name, mode, "native", "FAIL", "build failed (exit %s): %s" % (code, err.decode(errors="replace").strip()[:300]),
                       build_seconds=round(seconds, 3))
                continue
            built = dict(image_sha256=sha256(image), build_seconds=round(seconds, 3))
            if not execute:
                record(name, mode, "native", "NOT_RUN", "built only: %s images cannot run on %s" % (args.target, host), **built)
                continue
            code, out, err, seconds, timed_out = run([str(image)], args.timeout, cwd=output)
            (output / ("%s-%s.stdout" % (name, mode))).write_bytes(out)
            (output / ("%s-%s.stderr" % (name, mode))).write_bytes(err)
            built.update(run_seconds=round(seconds, 3), exit_code=code)
            if timed_out:
                record(name, mode, "native", "FAIL", "timed out after %ss" % args.timeout, **built)
            elif (code == 0) != want_success:
                record(name, mode, "native", "FAIL", "exit %s (expected %s): %s" % (code, "0" if want_success else "nonzero",
                       err.decode(errors="replace").strip()[:300]), **built)
            elif out != expected:
                record(name, mode, "native", "FAIL", "stdout differs from %s" % expected_path.name, **built)
            else:
                record(name, mode, "native", "PASS", **built)
        if args.reference:
            code, out, err, seconds, timed_out = run([args.reference, str(ROOT / "bunko"), "run", str(source)], args.timeout, cwd=ROOT, env=env)
            (output / ("%s-reference.stdout" % name)).write_bytes(out)
            (output / ("%s-reference.stderr" % name)).write_bytes(err)
            if timed_out or (code == 0) != want_success:
                record(name, "vm", "reference", "FAIL", "reference VM exit %s: %s" % (code, err.decode(errors="replace").strip()[:300]))
            elif out != expected:
                record(name, "vm", "reference", "FAIL", "reference VM stdout differs from %s" % expected_path.name)
            else:
                record(name, "vm", "reference", "PASS", run_seconds=round(seconds, 3))

    summary = {}
    for item in results:
        summary[item["status"]] = summary.get(item["status"], 0) + 1
    report = dict(schema_version=1, suite="runtime-abi-" + args.suite, contract="RA-1", target=args.target, host=host,
                  host_platform=platform.platform(), compiler=str(mio), compiler_sha256=sha256(mio),
                  executed=execute, summary=summary, results=results)
    (output / "results.json").write_text(json.dumps(report, indent=2) + "\n")
    print("summary: %s" % json.dumps(summary, sort_keys=True))
    if summary.get("FAIL"):
        return 1
    if not execute:
        return 2  # nothing was executed: never a PASS
    return 0


if __name__ == "__main__":
    sys.exit(main())
