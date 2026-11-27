#!/usr/bin/env python3
"""Linux AMD64 distribution and libc compatibility checks for a native Mio CLI.

Run inside a distribution container (see setup.sh and the linux-compat job of .github/workflows/native.yml) on an
x86-64 host, so every program executes natively. Statuses: PASS, FAIL, NOT_RUN (a prerequisite is missing),
NOT_AVAILABLE (the feature is not in this source tree) and UNSUPPORTED (a documented incompatibility, for example a
glibc-linked binary on musl). Only PASS is a pass. report.json and logs go to --output.

  run.py --prebuild-only --mio MIO --output DIR   build the cross-distribution binaries on the host
  run.py --mio MIO --prebuilt DIR --output DIR     run every check in the current environment
"""
import argparse
import json
import os
import platform
import re
import shutil
import struct
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
CASES = HERE / "cases"
SHARED = ROOT / "tests" / "shared" / "linux-amd64"
PREBUILT = ("hello", "cabi", "ffi")


class Report:
    def __init__(self, output):
        self.output = output
        (output / "logs").mkdir(parents=True, exist_ok=True)
        self.checks = []
        self.environment = {}

    def record(self, name, status, detail=""):
        self.checks.append({"name": name, "status": status, "detail": detail})
        print(f"{status:14} {name}{(' - ' + detail) if detail else ''}", flush=True)

    def run(self, label, argv, env=None, cwd=None, timeout=600):
        argv = [str(a) for a in argv]
        try:
            p = subprocess.run(argv, capture_output=True, text=True, env=env, cwd=cwd, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired) as error:
            (self.output / "logs" / f"{label}.log").write_text(f"$ {' '.join(argv)}\n{error}\n")
            return None
        (self.output / "logs" / f"{label}.log").write_text(f"$ {' '.join(argv)}\n[rc={p.returncode}]\n--- stdout ---\n{p.stdout}\n--- stderr ---\n{p.stderr}\n")
        return p

    def finish(self):
        failed = [c for c in self.checks if c["status"] == "FAIL"]
        (self.output / "report.json").write_text(json.dumps({"environment": self.environment, "checks": self.checks}, indent=2) + "\n")
        counts = {}
        for c in self.checks:
            counts[c["status"]] = counts.get(c["status"], 0) + 1
        print("\n" + ", ".join(f"{v} {k}" for k, v in sorted(counts.items())))
        return 1 if failed else 0


def interpreter(path):
    """PT_INTERP of an ELF64 file, '' when it has none (static), None when it is not ELF64."""
    data = Path(path).read_bytes()
    if data[:4] != b"\x7fELF" or data[4] != 2:
        return None
    phoff, = struct.unpack_from("<Q", data, 32)
    phentsize, phnum = struct.unpack_from("<HH", data, 54)
    for i in range(phnum):
        kind, = struct.unpack_from("<I", data, phoff + i * phentsize)
        if kind == 3:
            offset, = struct.unpack_from("<Q", data, phoff + i * phentsize + 8)
            size, = struct.unpack_from("<Q", data, phoff + i * phentsize + 32)
            return data[offset:offset + size].rstrip(b"\0").decode()
    return ""


def libc_identity(report):
    """('glibc', '2.36') or ('musl', '1.2.5') of this environment."""
    p = report.run("getconf", ["getconf", "GNU_LIBC_VERSION"])
    if p is not None and p.returncode == 0 and p.stdout.startswith("glibc"):
        return "glibc", p.stdout.split()[1]
    for loader in sorted(Path("/lib").glob("ld-musl-*.so.1")):
        p = report.run("musl-version", [loader])
        text = (p.stdout + p.stderr) if p is not None else ""
        match = re.search(r"Version ([0-9.]+)", text)
        return "musl", match.group(1) if match else "unknown"
    return "unknown", ""


def libc_soname(kind):
    return {"glibc": "libc.so.6", "musl": "libc.musl-x86_64.so.1"}.get(kind, "")


def build(report, mio, label, source, output, extra=(), env=None):
    p = report.run(f"build-{label}", [mio, "build", source, "-o", output, *extra], env=env)
    return p is not None and p.returncode == 0 and Path(output).is_file(), p


def no_bunko_env():
    env = dict(os.environ)
    env.pop("MIO_BUNKO", None)
    return env


def prebuild(args):
    out = Path(args.output).resolve()
    out.mkdir(parents=True, exist_ok=True)
    report = Report(out)
    for name in PREBUILT:
        ok, p = build(report, args.mio, name, CASES / f"{name}.mio", out / name, env=no_bunko_env())
        report.record(f"prebuild {name}", "PASS" if ok else "FAIL", "" if ok else (p.stderr.strip()[:200] if p else "could not run mio"))
    return report.finish()


def glibc_requirements(report, path):
    """The highest GLIBC_x.y symbol version a dynamic binary requires, via objdump -T (binutils)."""
    if not shutil.which("objdump"):
        return None
    p = report.run(f"objdump-T-{Path(path).name}", ["objdump", "-T", path])
    versions = re.findall(r"GLIBC_([0-9]+)\.([0-9]+)", p.stdout if p else "")
    if not versions:
        return ""
    major, minor = max((int(a), int(b)) for a, b in versions)
    return f"GLIBC_{major}.{minor}"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--prebuilt", help="directory of binaries built on another distribution (--prebuild-only)")
    parser.add_argument("--prebuild-only", action="store_true")
    parser.add_argument("--bunko-root", default=str(ROOT / "bunko"))
    args = parser.parse_args()
    mio = str(Path(args.mio).resolve())
    os.chmod(mio, 0o755)
    if args.prebuild_only:
        args.mio = mio
        return prebuild(args)
    out = Path(args.output).resolve()
    shutil.rmtree(out, ignore_errors=True)
    report = Report(out)
    bin_dir = out / "bin"
    bin_dir.mkdir()
    release = {}
    if Path("/etc/os-release").is_file():
        for line in Path("/etc/os-release").read_text().splitlines():
            if "=" in line:
                key, value = line.split("=", 1)
                release[key] = value.strip('"')
    kind, version = libc_identity(report)
    report.environment = {"distribution": release.get("PRETTY_NAME", "unknown"), "libc": kind, "libc_version": version,
                          "machine": platform.machine(), "kernel": platform.release()}
    print(json.dumps(report.environment), flush=True)
    if platform.machine() not in ("x86_64", "AMD64"):
        report.record("native x86-64 execution", "NOT_RUN", f"machine is {platform.machine()}; emulation is not evidence")
        return report.finish()

    # 1. the CLI itself
    interp = interpreter(mio)
    report.record("mio CLI is a static ELF64 (no PT_INTERP)", "PASS" if interp == "" else "FAIL", repr(interp))
    p = report.run("mio-version", [mio, "version"])
    report.record("mio CLI runs", "PASS" if p is not None and p.returncode == 0 else "FAIL", (p.stdout.strip().splitlines() or [""])[0] if p else "")

    # 2. static executable with the embedded Bunko only
    ok, p = build(report, mio, "hello", CASES / "hello.mio", bin_dir / "hello", env=no_bunko_env())
    run = report.run("run-hello", [bin_dir / "hello"]) if ok else None
    report.record("static executable builds (embedded Bunko, no MIO_BUNKO) and runs", "PASS" if run is not None and run.stdout == "static ok\n42\n" else "FAIL",
                  "" if ok else (p.stderr.strip()[:200] if p else ""))
    if ok:
        report.record("static executable has no PT_INTERP", "PASS" if interpreter(bin_dir / "hello") == "" else "FAIL")

    # 3. dynamically linked executable through the system C driver
    has_cc = any(shutil.which(c) for c in ("cc", "gcc", "clang"))
    if not has_cc:
        report.record("dynamically linked executable", "NOT_RUN", "no C compiler driver in this environment")
    else:
        ok, p = build(report, mio, "cabi", CASES / "cabi.mio", bin_dir / "cabi", env=no_bunko_env())
        run = report.run("run-cabi", [bin_dir / "cabi"]) if ok else None
        report.record("dynamically linked executable (C declaration) builds and runs", "PASS" if run is not None and run.stdout == "5\n" else "FAIL",
                      "" if ok else (p.stderr.strip()[:300] if p else ""))
        if ok:
            interp = interpreter(bin_dir / "cabi")
            expected = {"glibc": "/lib64/ld-linux-x86-64.so.2", "musl": "/lib/ld-musl-x86_64.so.1"}.get(kind)
            report.record("PT_INTERP is this libc's loader", "PASS" if interp == expected and Path(interp).exists() else "FAIL", interp)
            if kind == "glibc":
                need = glibc_requirements(report, bin_dir / "cabi")
                report.environment["dynamic_executable_requires"] = need
        # 4. run-time loading and native memory through kiso/ffi/linux
        soname = libc_soname(kind)
        if not soname:
            report.record("kiso/ffi/linux against this libc", "NOT_RUN", f"unknown libc {kind}")
        else:
            env = dict(os.environ, MIO_COMPAT_LIBC=soname)
            for label, extra in (("", ()), (" --gc-stress", ("--gc-stress",))):
                name = "ffi" + label.replace(" --", "-")
                ok, p = build(report, mio, name, CASES / "ffi.mio", bin_dir / name, extra, env=no_bunko_env())
                run = report.run(f"run-{name}", [bin_dir / name], env=env) if ok else None
                lines = run.stdout.strip().splitlines() if run is not None else []
                report.record(f"kiso/ffi/linux loading, calls and native memory against {soname}{label}", "PASS" if run is not None and run.returncode == 0 and lines[-1:] == ["ffi compat PASS"] else "FAIL",
                              "" if ok else (p.stderr.strip()[:300] if p else ""))
                if ok and kind == "glibc" and not label:
                    report.environment["ffi_executable_requires"] = glibc_requirements(report, bin_dir / name)
        if kind == "glibc":
            p = report.run("mio-test-ffi-linux", [mio, "test", "bunko/kiso/ffi/linux", "--bunko-root", args.bunko_root, "--lang", "en-US"], cwd=ROOT)
            text = (p.stdout + p.stderr) if p else ""
            report.record("mio test bunko/kiso/ffi/linux", "PASS" if p is not None and p.returncode == 0 and re.search(r"Tests complete: [1-9]\d* passed, 0 failed", text) else "FAIL")
        else:
            report.record("mio test bunko/kiso/ffi/linux", "NOT_RUN", "the package tests open glibc library names (libc.so.6, libm.so.6); cases/ffi.mio covers this libc")

    # 5. shared libraries: native linker and system linker, loaded by a C host built here
    host = bin_dir / "host"
    if has_cc:
        p = report.run("host-compile", ["cc", "-O1", "-o", host, SHARED / "host" / "host.c", "-ldl", "-lm"])
        if p is None or p.returncode != 0:
            host = None
            report.record("C host for shared libraries compiles", "FAIL", p.stderr.strip()[:200] if p else "")
    else:
        host = None
    for linker, extra in (("native", ["--linker", "native"]), ("system", [])):
        libs = out / f"lib-{linker}"
        libs.mkdir()
        built = True
        reason = ""
        for name in ("math", "bytes", "text", "collections"):
            p = report.run(f"shared-{linker}-{name}", [mio, "build", SHARED / "modules" / f"mio_{name}", "--shared", *extra, "--soname", f"libmio_{name}.so",
                                                        "-o", libs / f"libmio_{name}.so", "--bunko-root", args.bunko_root])
            if p is None or p.returncode != 0:
                built = False
                reason = (p.stderr.strip() if p else "")[:300]
                break
        if built:
            modules = [SHARED / "modules" / f"mio_{n}" for n in ("math", "bytes", "text", "collections")]
            p = report.run(f"shared-{linker}-std", [mio, "build", *modules, "--shared", *extra, "--soname", "libmio_std.so", "-o", libs / "libmio_std.so", "--bunko-root", args.bunko_root])
            if p is None or p.returncode != 0:
                built = False
                reason = (p.stderr.strip() if p else "")[:300]
        label = f"shared libraries with the {linker} linker"
        if not built and linker == "native" and "reserved" in reason:
            report.record(label, "NOT_AVAILABLE", "--linker native is not implemented in this source tree")
            continue
        if not built and linker == "system" and not (shutil.which("ld") or has_cc):
            report.record(label, "NOT_RUN", "no linker in this environment")
            continue
        report.record(f"{label}: build", "PASS" if built else "FAIL", reason)
        if built and host is not None:
            for suite in ("math", "bytes", "text", "collections", "std", "stress", "reload"):
                p = report.run(f"host-{linker}-{suite}", [host, libs, suite])
                ok = p is not None and p.returncode == 0 and p.stdout.rstrip().endswith(f"{suite}: OK")
                report.record(f"{label}: C host suite {suite}", "PASS" if ok else "FAIL")
        elif built:
            report.record(f"{label}: C host", "NOT_RUN", "no C compiler for the host program")

    # 6. binaries built on another distribution (the CI host)
    if args.prebuilt:
        for name in PREBUILT:
            binary = Path(args.prebuilt) / name
            if not binary.is_file():
                report.record(f"prebuilt {name} runs here", "NOT_RUN", "not built")
                continue
            os.chmod(binary, 0o755)
            env = dict(os.environ, MIO_COMPAT_LIBC=libc_soname(kind) or "libc.so.6")
            p = report.run(f"prebuilt-{name}", [binary], env=env)
            expected = {"hello": "static ok\n42\n", "cabi": "5\n"}.get(name)
            ok = p is not None and p.returncode == 0 and (p.stdout == expected if expected else p.stdout.rstrip().endswith("ffi compat PASS"))
            dynamic = interpreter(binary) not in ("", None)
            if not ok and dynamic and kind == "musl":
                report.record(f"prebuilt glibc {name} runs here", "UNSUPPORTED", "a glibc-linked dynamic executable does not run on musl; build it on musl")
            else:
                report.record(f"prebuilt {name} (built on the CI host) runs here", "PASS" if ok else "FAIL",
                              "" if ok else ((p.stderr or p.stdout).strip()[:200] if p else "could not execute"))
    return report.finish()


if __name__ == "__main__":
    sys.exit(main())
