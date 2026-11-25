#!/usr/bin/env python3
"""Linux x86-64 (System V AMD64) C ABI end-to-end acceptance with real C objects.

Every positive check builds a Mio program with the native compiler under test, links real C code compiled by the
host's GCC and/or Clang, executes the produced ELF and compares its output. Compiling a fixture alone is never
counted as evidence. Statuses are PASS, FAIL, SKIP, NOT_RUN and NOT_APPLICABLE; cross-compilation is not execution.
"""

import argparse
import json
import os
import platform
import shlex
import shutil
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
CASES = HERE / "cases"
SHARED = ROOT / "tests/ffi/darwin-arm64/cases"  # target-neutral negative programs and the shared scalar program


class Report:
    def __init__(self, output):
        self.output = output
        self.output.mkdir(parents=True, exist_ok=True)
        self.checks = []

    def record(self, name, status, **details):
        self.checks.append(dict(name=name, status=status, **details))
        print(f"{status} {name}", flush=True)

    def run(self, name, command, status=0, stdout=None, diagnostic=None, contains=(), absent=(), timeout=180):
        argv = [str(part) for part in command]
        log = self.output / (name + ".log")
        try:
            env = dict(os.environ, MIO_LANG="en-US", LC_ALL="C")
            process = subprocess.run(argv, cwd=ROOT, env=env, capture_output=True, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired) as error:
            log.write_text(f"$ {shlex.join(argv)}\n{error}\n", encoding="utf-8")
            self.record(name, "FAIL", log=str(log))
            return None
        log.write_bytes(
            f"$ {shlex.join(argv)}\nexit={process.returncode}\n--- stdout ---\n".encode()
            + process.stdout + b"\n--- stderr ---\n" + process.stderr
        )
        succeeded = (
            (process.returncode > 0 if status == "error" else process.returncode == status)
            and (stdout is None or (process.stdout == stdout and not process.stderr))
            and (diagnostic is None or diagnostic in process.stderr)
            and all(item in process.stdout + process.stderr for item in contains)
            and not any(item in process.stdout + process.stderr for item in absent)
        )
        self.record(name, "PASS" if succeeded else "FAIL", log=str(log))
        return process if succeeded else None

    def finish(self):
        (self.output / "report.json").write_text(
            json.dumps({"target": "linux-amd64", "host": platform.platform(), "checks": self.checks}, indent=2) + "\n",
            encoding="utf-8",
        )
        if any(check["status"] == "FAIL" for check in self.checks):
            return 1
        return 0 if any(check["status"] == "PASS" for check in self.checks) else 2


def have_tool(name):
    return shutil.which(name) is not None


def verify(report, mio, reference, baseline):
    if (platform.system(), platform.machine()) not in (("Linux", "x86_64"), ("Linux", "amd64")):
        report.record("linux-amd64", "SKIP", reason="requires a native Linux x86-64 host")
        return
    if not mio.is_file():
        report.record("current-source-compiler", "NOT_RUN", reason=f"missing compiler {mio}")
        return

    def build(case, artifact, options=(), status=0, diagnostic=None, compiler=mio, base=CASES, contains=()):
        return report.run(
            artifact.name + "-build",
            [compiler, "build", base / (case + ".mio"), "--bunko-root", ROOT / "bunko", "-o", artifact, *options],
            status=status, diagnostic=diagnostic, contains=contains,
        )

    def run_program(case, name, expected_file=None, options=(), expected=None, base=CASES, status=0):
        image = report.output / name
        if build(case, image, options, base=base) is None:
            report.record(name + "-run", "NOT_RUN", reason="executable failed to build")
            return None
        if expected is None:
            expected = (base / (expected_file or case + ".expected")).read_bytes()
        return report.run(name + "-run", [image], status=status, stdout=expected) and image

    # C compilers available on this host. Clang is optional; a missing one is a SKIP, never a pass.
    compilers = []
    for name in ("gcc", "clang"):
        if have_tool(name):
            compilers.append(name)
        else:
            report.record(name + "-available", "SKIP", reason=f"{name} is not installed")
    if not compilers and have_tool("cc"):
        compilers.append("cc")
    if not compilers:
        report.record("c-compiler", "NOT_RUN", reason="no C compiler available")
        return
    if not have_tool("ar"):
        report.record("ar", "NOT_RUN", reason="ar is not installed; static library checks cannot run")

    for cc in compilers:
        out = report.output / cc
        out.mkdir(exist_ok=True)
        flags = ["-O2", "-Wall", "-Wextra", "-Werror"]
        objects = {}
        for stem in ("scalars", "sysv"):
            obj = out / (stem + ".o")
            pic = out / (stem + "-pic.o")
            ready = report.run(f"{cc}-object-{stem}", [cc, *flags, "-c", HERE / f"fixtures/{stem}.c", "-o", obj])
            pic_ready = report.run(f"{cc}-pic-object-{stem}", [cc, *flags, "-fPIC", "-c", HERE / f"fixtures/{stem}.c", "-o", pic])
            objects[stem] = (obj if ready else None, pic if pic_ready else None)
        static_dir, shared_dir = out / "static", out / "shared"
        static_dir.mkdir(exist_ok=True)
        shared_dir.mkdir(exist_ok=True)
        archive = static_dir / "libmio_ffi_static.a"
        shared = shared_dir / "libmio_ffi_fixture.so"
        all_obj = [objects[s][0] for s in objects]
        all_pic = [objects[s][1] for s in objects]
        have_archive = (all(all_obj) and have_tool("ar")
                        and report.run(f"{cc}-static-archive", ["ar", "-rcs", archive, *all_obj]) is not None)
        have_shared = (all(all_pic)
                       and report.run(f"{cc}-shared-library", [cc, "-shared", *all_pic, "-o", shared]) is not None)

        for case, base in (("scalars", SHARED), ("sysv", CASES), ("widths", CASES), ("stack", CASES), ("gc", CASES)):
            tag = f"{cc}-{case}"
            # object files: every --link keeps the command-line order
            if all(all_obj):
                opts = []
                for obj in all_obj:
                    opts += ["--link", obj]
                run_program(case, tag + "-object", base=base, options=opts, expected_file=case + ".expected")
            else:
                report.record(tag + "-object", "NOT_RUN", reason="C fixture unavailable")
            if have_archive:
                run_program(case, tag + "-archive", base=base, options=["-L", static_dir, "-lmio_ffi_static"],
                            expected_file=case + ".expected")
                run_program(case, tag + "-archive-path", base=base, options=["--link", archive],
                            expected_file=case + ".expected")
            else:
                report.record(tag + "-archive", "NOT_RUN", reason="static archive unavailable")
            if have_shared:
                run_program(case, tag + "-shared", base=base, options=["--link", shared, "--rpath", shared_dir],
                            expected_file=case + ".expected")
                run_program(case, tag + "-shared-name", base=base,
                            options=["-L", shared_dir, "-lmio_ffi_fixture", "--rpath", shared_dir],
                            expected_file=case + ".expected")
            else:
                report.record(tag + "-shared", "NOT_RUN", reason="shared library unavailable")
        gc_options = []
        for obj in all_obj:
            if obj:
                gc_options += ["--link", obj]
        if all(all_obj):
            run_program("gc", f"{cc}-gc-stress", options=["--gc-stress", *gc_options])
            run_program("stack", f"{cc}-stack-gc-stress", options=["--gc-stress", *gc_options])
            run_program("stdio", f"{cc}-stdio", options=gc_options)
            exit_image = report.output / f"{cc}-exit"
            if build("exit", exit_image, gc_options):
                report.run(f"{cc}-exit-run", [exit_image], status=3, stdout=b"C stdio flushed\n")

        if cc == compilers[0] and all(all_obj):
            inspect_elf(report, mio, build, run_program, all_obj, have_shared, shared, shared_dir)

    # System libraries need no user fixture: libc is linked by the C driver, libm by -lm.
    run_program("system", "system", expected=b"hai\n")
    run_program("imported", "imported", expected=b"hai\n", base=CASES)
    run_program("libs", "libs", options=["-lm"])
    build("libs", report.output / "libs-no-libm", status="error", diagnostic=b"sqrt")
    report.run("native-test-command-system-library", [mio, "test", HERE / "native", "--bunko-root", ROOT / "bunko", "--format=json"])

    run_program("no-ffi", "ordinary", expected=b"42\n")
    if baseline:
        if build("no-ffi", report.output / "baseline", compiler=baseline) and (report.output / "ordinary").is_file():
            identical = (report.output / "baseline").read_bytes() == (report.output / "ordinary").read_bytes()
            report.record("ordinary-binary-identity", "PASS" if identical else "FAIL")
        # Ordinary programs of every other target are byte-identical to the baseline compiler's output: the Linux C ABI
        # work must not alter existing Mach-O, PE or ARM64 ELF images (cross builds; not native execution on those hosts).
        for target in ("linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64"):
            current, previous = report.output / ("identity-current-" + target), report.output / ("identity-baseline-" + target)
            if build("no-ffi", current, ["--target", target]) and build("no-ffi", previous, ["--target", target], compiler=baseline):
                report.record("ordinary-identity-" + target, "PASS" if current.read_bytes() == previous.read_bytes() else "FAIL")
            else:
                report.record("ordinary-identity-" + target, "NOT_RUN", reason="a build failed")
    else:
        report.record("ordinary-binary-identity", "SKIP", reason="baseline compiler not supplied")
    report.record("darwin-arm64-c-abi", "NOT_APPLICABLE",
                  reason="the Darwin ARM64 C ABI runs on Apple silicon only: tests/ffi/darwin-arm64/run.py")

    bytecode = report.output / "system.mochi"
    if build("system", bytecode, ["--emit=mochi"]):
        report.run("exec-mochi", [mio, "exec", bytecode, "--bunko-root", ROOT / "bunko"], stdout=b"hai\n")
    else:
        report.record("exec-mochi", "NOT_RUN", reason="Mochi emission failed")
    report.run("run-command", [mio, "run", CASES / "system.mio", "--bunko-root", ROOT / "bunko"], stdout=b"hai\n")
    report.run("check-command", [mio, "check", CASES / "sysv.mio", "--bunko-root", ROOT / "bunko"])

    negatives(report, mio, build, reference)


def inspect_elf(report, mio, build, run_program, objects, have_shared, shared, shared_dir):
    """readelf / ldd inspection of the produced executables."""
    image = report.output / "inspect-libc"
    if not (build("system", image) and image.is_file()):
        report.record("elf-inspection", "NOT_RUN", reason="inspection image failed to build")
        return
    if have_tool("readelf"):
        report.run("elf-header", ["readelf", "-h", image],
                   contains=[b"Class:                             ELF64", b"little endian",
                             b"Machine:                           Advanced Micro Devices X86-64",
                             b"Type:                              EXEC"])
        report.run("elf-sections", ["readelf", "-SW", image], contains=[b".mio_bss", b"NOBITS", b"0000000010000000"])
        report.run("elf-interpreter", ["readelf", "-lW", image], contains=[b"INTERP", b"/ld-linux-x86-64.so.2", b"GNU_STACK"])
        report.run("elf-dynamic-symbols", ["readelf", "--dyn-syms", "-W", image], contains=[b"getpid"])
        report.run("elf-relocations", ["readelf", "-rW", image], contains=[b"R_X86_64_JUMP_SLOT", b"getpid"])
        objs = report.output / "inspect-object"
        opts = []
        for obj in objects:
            opts += ["--link", obj]
        if build("sysv", objs, opts):
            report.run("elf-defined-symbols", ["readelf", "-sW", objs], contains=[b"mio_c_add", b"FUNC"])
    else:
        report.record("elf-inspection", "SKIP", reason="readelf is not installed")
    if have_tool("ldd"):
        report.run("ldd-libc", ["ldd", image], contains=[b"libc.so.6"])
        if have_shared:
            shared_image = report.output / "inspect-shared"
            if build("sysv", shared_image, ["--link", shared, "--rpath", shared_dir]):
                report.run("ldd-shared", ["ldd", shared_image], contains=[b"libmio_ffi_fixture.so", b"libc.so.6"], absent=[b"not found"])
    else:
        report.record("ldd", "SKIP", reason="ldd is not installed")
    ordinary = report.output / "inspect-ordinary"
    if build("no-ffi", ordinary) and have_tool("readelf"):
        report.run("elf-ordinary-static", ["readelf", "-lW", ordinary], absent=[b"INTERP", b"DYNAMIC"])


def negatives(report, mio, build, reference):
    # Unsupported targets: C declarations are rejected at compile time and never fall back to Mio's own convention.
    for target in ("linux-arm64", "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64"):
        build("system", report.output / ("reject-" + target), ["--target", target], "error", b"E_CABI", base=CASES)
        ordinary = report.output / ("ordinary-" + target)
        magic = {"linux-arm64": b"\x7fELF", "darwin-amd64": b"\xcf\xfa\xed\xfe", "darwin-arm64": b"\xcf\xfa\xed\xfe",
                 "windows-amd64": b"MZ", "windows-arm64": b"MZ"}[target]
        if build("no-ffi", ordinary, ["--target", target], base=CASES):
            report.record(ordinary.name + "-format", "PASS" if ordinary.read_bytes().startswith(magic) else "FAIL")
    # Link options are meaningless without a C declaration or for another target.
    build("no-ffi", report.output / "link-without-c", ["-lm"], "error", b"E_CABI", base=CASES)
    build("system", report.output / "link-cross-target", ["--target", "linux-arm64", "-lm"], "error", b"E_CABI", base=CASES)
    build("system", report.output / "framework", ["-framework", "CoreFoundation"], "error", b"darwin-arm64 only", base=CASES)
    build("system", report.output / "framework-path", ["-F", "/tmp"], "error", b"darwin-arm64 only", base=CASES)
    # Programs with C calls and OS threads are rejected (the thread runtime owns the FS base register).
    build("threads", report.output / "threads", status="error", diagnostic=b"OS threads", base=CASES)
    # Source-level rejections shared with every C ABI target.
    for case, diagnostic in (
        ("wrong-arity", b"argument count"),
        ("wrong-type", b"argument type mismatch"),
        ("struct-by-value", b"fixed prototype"),
        ("variadic", b"variadic parameters are unsupported"),
        ("invalid-symbol", b"C declaration suffix"),
        ("conflicting-prototypes", b"conflicting prototypes"),
        ("callback", b"fixed prototype"),
        ("invalid-pointer", b"constructs null"),
    ):
        build(case, report.output / case, status="error", diagnostic=diagnostic, base=SHARED)
    # Link-time failures keep the system linker's diagnostic.
    build("unresolved", report.output / "unresolved", status="error", diagnostic=b"mio_fixture_missing_symbol", base=SHARED)
    build("system", report.output / "missing-lib", ["-lmio_fixture_missing_library"], "error", b"mio_fixture_missing_library", base=CASES)
    build("system", report.output / "missing-object", ["--link", report.output / "missing.o"], "error", b"does not exist", base=CASES)
    wrong = report.output / "wrong.txt"
    wrong.write_text("not a linkable file")
    build("system", report.output / "wrong-extension", ["--link", wrong], "error", b"requires an existing .o, .a or .so", base=CASES)
    garbage = report.output / "garbage.o"
    garbage.write_text("this is not an ELF object")
    build("system", report.output / "garbage-object", ["--link", garbage], "error", b"E_CABI_LINK", base=CASES)
    build("system", report.output / "missing-value", ["--link"], "error", b"--link", base=CASES)
    build("system", report.output / "bad-library-name", ["-l-oops"], "error", b"invalid library", base=CASES)
    if reference:
        report.run("reference-check", [reference, "check", CASES / "sysv.mio"])
        for case, diagnostic in (
            ("struct-by-value", b"fixed prototype"),
            ("variadic", b"variadic parameters are unsupported"),
            ("invalid-symbol", b"C declaration suffix"),
            ("callback", b"fixed prototype"),
            ("invalid-pointer", b"constructs null"),
        ):
            report.run("reference-" + case, [reference, "check", SHARED / (case + ".mio")], status="error", diagnostic=diagnostic)
        report.run("reference-execution-rejected", [reference, "run", CASES / "system.mio"], status="error",
                   diagnostic=b"Go Reference VM cannot execute")
    else:
        report.record("reference-vm", "SKIP", reason="Go Reference VM not supplied")


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
