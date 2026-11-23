#!/usr/bin/env python3
"""Darwin ARM64 FFI end-to-end acceptance with actual Apple C objects."""

import argparse
import json
import platform
import shlex
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
CASES = HERE / "cases"


class Report:
    def __init__(self, output):
        self.output = output
        self.output.mkdir(parents=True, exist_ok=True)
        self.checks = []

    def record(self, name, status, **details):
        self.checks.append(dict(name=name, status=status, **details))
        print(f"{status} {name}", flush=True)

    def run(self, name, command, status=0, stdout=None, diagnostic=None):
        argv = [str(part) for part in command]
        try:
            process = subprocess.run(argv, cwd=ROOT, capture_output=True, timeout=120)
        except (OSError, subprocess.TimeoutExpired) as error:
            log = self.output / (name + ".log")
            log.write_text(f"$ {shlex.join(argv)}\n{error}\n", encoding="utf-8")
            self.record(name, "FAIL", log=str(log))
            return False

        log = self.output / (name + ".log")
        log.write_bytes(
            f"$ {shlex.join(argv)}\nexit={process.returncode}\n--- stdout ---\n".encode()
            + process.stdout + b"\n--- stderr ---\n" + process.stderr
        )
        succeeded = (
            (process.returncode > 0 if status == "error" else process.returncode == status)
            and (stdout is None or (process.stdout == stdout and not process.stderr))
            and (diagnostic is None or diagnostic in process.stderr)
        )
        self.record(name, "PASS" if succeeded else "FAIL", log=str(log))
        return succeeded

    def finish(self):
        (self.output / "report.json").write_text(
            json.dumps({"target": "darwin-arm64", "checks": self.checks}, indent=2) + "\n",
            encoding="utf-8",
        )
        if any(check["status"] == "FAIL" for check in self.checks):
            return 1
        return 0 if any(check["status"] == "PASS" for check in self.checks) else 2


def verify(report, mio, reference, baseline):
    if (platform.system(), platform.machine()) != ("Darwin", "arm64"):
        report.record("darwin-arm64", "SKIP", reason="requires Apple Silicon macOS")
        return
    if not mio.is_file():
        report.record("current-source-compiler", "NOT_RUN", reason=f"missing compiler {mio}")
        return
    clang = ["/usr/bin/xcrun", "--sdk", "macosx", "clang", "-arch", "arm64", "-mmacosx-version-min=11.0"]
    fixture = HERE / "fixtures/scalars.c"
    obj = report.output / "scalars.o"
    lib = report.output / "libmio_ffi_static.a"
    dylib = report.output / "libmio_ffi_fixture.dylib"
    try:
        tool = subprocess.run(["/usr/bin/xcrun", "--find", "clang"], capture_output=True, timeout=20)
    except (OSError, subprocess.TimeoutExpired) as error:
        report.record("xcode-clang", "NOT_RUN", reason=str(error))
        return
    if tool.returncode:
        report.record("xcode-clang", "NOT_RUN", reason=tool.stderr.decode(errors="replace").strip())
        return

    def build(case, artifact, options=(), status=0, diagnostic=None, compiler=mio):
        return report.run(
            artifact.name + "-build",
            [compiler, "build", CASES / (case + ".mio"), "--bunko-root", ROOT / "bunko", "-o", artifact, *options],
            status=status,
            diagnostic=diagnostic,
        )

    def run_program(case, name, expected, options=()):
        image = report.output / name
        if build(case, image, options):
            report.run(name + "-sign", ["/usr/bin/codesign", "--verify", "--strict", image])
            report.run(name + "-run", [image], stdout=expected)
        else:
            report.record(name + "-run", "NOT_RUN", reason="executable failed to build")

    obj_ready = report.run("clang-object", [*clang, "-O2", "-Wall", "-Wextra", "-Werror", "-c", fixture, "-o", obj])
    lib_ready = obj_ready and report.run("static-archive", ["/usr/bin/ar", "-rcs", lib, obj])
    dylib_ready = report.run("clang-dylib", [*clang, "-dynamiclib", obj, "-install_name", "@rpath/" + dylib.name, "-o", dylib])
    expected = (CASES / "scalars.expected").read_bytes()

    for name, ready, options in (
        ("object", obj_ready, ["--link", obj]),
        ("archive", lib_ready, ["-L", report.output, "-lmio_ffi_static"]),
        ("dylib", dylib_ready, ["--link", dylib, "--rpath", report.output]),
    ):
        if ready:
            run_program("scalars", name, expected, options)
        else:
            report.record(name, "NOT_RUN", reason="C fixture unavailable")

    run_program("system", "system", b"hai\n")
    run_program("imported", "imported", b"hai\n")
    report.run("native-test-command", [mio, "test", HERE / "native", "--bunko-root", ROOT / "bunko", "--format=json"])
    run_program("framework", "framework", b"hai\n", ["-framework", "CoreFoundation"])
    run_program("thread", "thread", b"hai\n", ["--gc-stress"])
    run_program("no-ffi", "ordinary", b"42\n")
    if baseline:
        if build("no-ffi", report.output / "baseline", compiler=baseline) and (report.output / "ordinary").is_file():
            identical = (report.output / "baseline").read_bytes() == (report.output / "ordinary").read_bytes()
            report.record("ordinary-binary-identity", "PASS" if identical else "FAIL")
    else:
        report.record("ordinary-binary-identity", "SKIP", reason="baseline compiler not supplied")

    bytecode = report.output / "system.mochi"
    if build("system", bytecode, ["--emit=mochi"]):
        report.run("exec-mochi", [mio, "exec", bytecode, "--bunko-root", ROOT / "bunko"], stdout=b"hai\n")
    else:
        report.record("exec-mochi", "NOT_RUN", reason="Mochi emission failed")

    targets = {
        "darwin-amd64": b"\xcf\xfa\xed\xfe",
        "linux-arm64": b"\x7fELF",
        "linux-amd64": b"\x7fELF",
        "windows-arm64": b"MZ",
        "windows-amd64": b"MZ",
    }
    for target, magic in targets.items():
        build("system", report.output / target, ["--target", target], "error", b"require native darwin-arm64")
        ordinary = report.output / ("ordinary-" + target)
        if build("no-ffi", ordinary, ["--target", target]):
            identified = ordinary.is_file() and ordinary.read_bytes().startswith(magic)
            report.record(ordinary.name + "-format", "PASS" if identified else "FAIL")
    for case, diagnostic in (
        ("wrong-arity", b"argument count"),
        ("wrong-type", b"argument type mismatch"),
        ("struct-by-value", b"fixed prototype"),
        ("variadic", b"variadic parameters are unsupported"),
        ("invalid-symbol", b"C declaration suffix"),
        ("conflicting-prototypes", b"conflicting prototypes"),
        ("callback", b"fixed prototype"),
        ("invalid-pointer", b"constructs null"),
        ("unresolved", b"_mio_fixture_missing_symbol"),
    ):
        build(case, report.output / case, status="error", diagnostic=diagnostic)
    build("system", report.output / "missing-lib", ["-lmio_fixture_missing_library"], "error", b"mio_fixture_missing_library")
    build("system", report.output / "missing-object", ["--link", report.output / "missing.o"], "error", b"does not exist")
    if reference:
        report.run("reference-check", [reference, "check", CASES / "scalars.mio"])
        for case, diagnostic in (
            ("struct-by-value", b"fixed prototype"),
            ("variadic", b"variadic parameters are unsupported"),
            ("invalid-symbol", b"C declaration suffix"),
            ("callback", b"fixed prototype"),
            ("invalid-pointer", b"constructs null"),
        ):
            report.run("reference-" + case, [reference, "check", CASES / (case + ".mio")],
                       status="error", diagnostic=diagnostic)
        report.run(
            "reference-execution-rejected",
            [reference, "run", CASES / "system.mio"],
            status="error",
            diagnostic=b"Go Reference VM cannot execute",
        )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--reference", type=Path)
    parser.add_argument("--baseline", type=Path)
    args = parser.parse_args()
    report = Report(args.output.resolve())
    verify(report, args.mio.resolve(), args.reference and args.reference.resolve(), args.baseline and args.baseline.resolve())
    return report.finish()


if __name__ == "__main__":
    sys.exit(main())
