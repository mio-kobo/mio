#!/usr/bin/env python3
"""Linux AMD64 shared-library (dlopen) end-to-end tests.

Builds the six native modules (plus the dependency pair) with `cc -fPIC -shared`, inspects them with
readelf/nm, compiles every Mio case in cases/ with the given Mio, runs the programs and checks the
output, then verifies static and dynamic ELF layouts. Raw logs and report.json go to --output.
"""
import argparse, json, os, shutil, subprocess, sys, time
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]

MODULES = {  # file -> (sources, extra link flags, expected exports, expected NEEDED)
    "libmio_math.so": (["mio_math.c"], [], ["mio_add", "mio_sub", "mio_multiply"], []),
    "libmio_buffer.so": (["mio_buffer.c"], [], ["mio_buffer_length", "mio_buffer_checksum"], []),
    "libmio_state.so": (["mio_state.c"], [], ["mio_state_create", "mio_state_increment", "mio_state_get", "mio_state_destroy"], []),
    "libmio_zlib.so": (["mio_zlib.c"], ["-lz"], ["mio_zlib_compress", "mio_zlib_decompress", "mio_zlib_crc32"], ["libz.so"]),
    "libmio_sqlite.so": (["mio_sqlite.c"], ["-lsqlite3"], ["mio_sqlite_open_memory", "mio_sqlite_exec", "mio_sqlite_close"], ["libsqlite3.so"]),
    "libmio_crypto.so": (["mio_crypto.c"], ["-lcrypto"], ["mio_sha256"], ["libcrypto.so"]),
    "libmio_dep_inner.so": (["mio_dep_inner.c"], [], ["mio_inner_value"], []),
    "libmio_dep_outer.so": (["mio_dep_outer.c"], ["-L{out}", "-lmio_dep_inner", "-Wl,-rpath,$ORIGIN"], ["mio_outer_value"], ["libmio_dep_inner.so"]),
}

results = []


def log_path(out, name):
    return out / "logs" / (name.replace("/", "_") + ".log")


def run(out, name, cmd, env=None, cwd=None, timeout=300, expect_rc=0):
    start = time.time()
    p = subprocess.run(cmd, env=env, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    log_path(out, name).write_text("$ %s\n[rc=%d]\n--- stdout ---\n%s\n--- stderr ---\n%s\n" % (" ".join(map(str, cmd)), p.returncode, p.stdout, p.stderr))
    return p, time.time() - start


def record(name, status, detail=""):
    results.append({"name": name, "status": status, "detail": detail})
    print("%-6s %s%s" % (status, name, (" - " + detail) if detail else ""), flush=True)


def build_modules(args, out):
    lib = out / "lib"
    lib.mkdir(parents=True, exist_ok=True)
    cflags = ["-O2", "-Wall", "-Wextra", "-Werror", "-fPIC", "-shared"]
    if args.include_dir:
        cflags += ["-I" + d for d in args.include_dir]
    link_dirs = ["-L" + d for d in args.lib_dir]
    for name, (sources, flags, exports, needed) in MODULES.items():
        target = lib / name
        cmd = [args.cc] + cflags + [str(HERE / "modules" / s) for s in sources] + ["-o", str(target)] + link_dirs + [f.replace("{out}", str(lib)) for f in flags]
        p, _ = run(out, "build_" + name, cmd)
        if p.returncode != 0:
            record("build " + name, "FAIL", p.stderr.strip()[:300])
            return False
        record("build " + name, "PASS")
    return True


def inspect_modules(args, out):
    lib = out / "lib"
    ok = True
    for name, (sources, flags, exports, needed) in MODULES.items():
        target = lib / name
        header, _ = run(out, "readelf_h_" + name, ["readelf", "-h", str(target)])
        text = header.stdout
        good = "ELF64" in text and "Advanced Micro Devices X86-64" in text and "DYN (Shared object file)" in text
        record("elf64 x86-64 shared object " + name, "PASS" if good else "FAIL")
        ok &= good
        dyn, _ = run(out, "readelf_d_" + name, ["readelf", "-d", str(target)])
        missing = [n for n in needed if ("[" + n) not in dyn.stdout]
        record("DT_NEEDED " + name, "PASS" if not missing else "FAIL", "missing: %s" % missing if missing else "")
        ok &= not missing
        nm, _ = run(out, "nm_D_" + name, ["nm", "-D", "--defined-only", str(target)])
        absent = [e for e in exports if (" T " + e) not in nm.stdout]
        record("exports " + name, "PASS" if not absent else "FAIL", "missing: %s" % absent if absent else "")
        ok &= not absent
        if shutil.which("file"):
            run(out, "file_" + name, ["file", str(target)])
    ldd, _ = run(out, "ldd_libmio_zlib", ["ldd", str(lib / "libmio_zlib.so")])
    resolved = "libz.so" in ldd.stdout and "not found" not in ldd.stdout
    record("ldd resolves libmio_zlib.so transitive dependency", "PASS" if resolved else "FAIL")
    ok &= resolved
    (lib / "not_elf.so").write_text("this is not an ELF file\n")
    data = (lib / "libmio_math.so").read_bytes()
    (lib / "truncated.so").write_bytes(data[:64])
    return ok


def mio_env(args):
    env = dict(os.environ)
    if args.bunko:
        env["MIO_BUNKO"] = args.bunko
    return env


def readelf_text(out, name, flag, path):
    p, _ = run(out, name, ["readelf", flag, str(path)])
    return p.stdout


def run_cases(args, out):
    lib = out / "lib"
    bin_dir = out / "bin"
    bin_dir.mkdir(exist_ok=True)
    env = mio_env(args)
    run_env = dict(os.environ)
    run_env["MIO_FFI_LIBDIR"] = str(lib)
    names = sorted(p.stem for p in (HERE / "cases").glob("test_ffi_*.mio"))
    if args.only:
        names = [n for n in names if n in args.only]
    variants = [("", [])] + ([("-gc-stress", ["--gc-stress"])] if not args.no_gc_stress else [])
    for base, extra in [(n, v) for n in names for v in variants]:
        name = base
        label, options = extra
        exe = bin_dir / (name + label)
        name_label = name + label
        p, _ = run(out, "mio_build_" + name_label, [args.mio, "build", str(HERE / "cases" / (name + ".mio")), "-o", str(exe)] + options, env=env)
        if p.returncode != 0:
            record(name_label, "FAIL", "mio build failed: " + (p.stderr.strip().splitlines() or [""])[0])
            continue
        # run from the lib directory so that "./libmio_math.so" resolves
        p, secs = run(out, "run_" + name_label, [str(exe)], env=run_env, cwd=lib, timeout=900)
        lines = p.stdout.strip().splitlines()
        passed = p.returncode == 0 and lines and lines[-1] == name + " PASS"
        record(name_label, "PASS" if passed else "FAIL", "%.2fs" % secs if passed else "rc=%d: %s" % (p.returncode, " | ".join(lines[-3:])))


def elf_modes(args, out):
    env = mio_env(args)
    bin_dir = out / "bin"
    static_src = HERE / "cases" / "static" / "static_program.mio"
    dynamic_src = HERE / "cases" / "dynamic" / "dynamic_program.mio"
    for label, src in (("static", static_src), ("dynamic", dynamic_src)):
        exe = bin_dir / (label + "_program")
        p, _ = run(out, "mio_build_" + label, [args.mio, "build", str(src), "-o", str(exe)], env=env)
        if p.returncode != 0:
            record("%s ELF builds" % label, "FAIL", p.stderr.strip()[:200])
            continue
        headers = readelf_text(out, "readelf_h_" + label, "-h", exe)
        phdrs = readelf_text(out, "readelf_l_" + label, "-l", exe)
        dyn = readelf_text(out, "readelf_d_" + label, "-d", exe)
        r, _ = run(out, "run_" + label + "_program", [str(exe)])
        record("%s ELF runs" % label, "PASS" if r.returncode == 0 and r.stdout.strip() == "ok" else "FAIL", r.stdout.strip()[:100])
        has_interp = "INTERP" in phdrs
        has_dynamic = "DYNAMIC" in phdrs
        needed = "(NEEDED)" in dyn
        if label == "static":
            good = "ELF64" in headers and not has_interp and not has_dynamic and not needed
            record("static ELF has no PT_INTERP, PT_DYNAMIC or DT_NEEDED", "PASS" if good else "FAIL")
        else:
            good = has_interp and has_dynamic and "libc.so.6" in dyn
            record("dynamic ELF has PT_INTERP and DT_NEEDED libc", "PASS" if good else "FAIL")


def trap_cases(args, out):
    env = mio_env(args)
    bin_dir = out / "bin"
    p, _ = run(out, "mio_build_nil_function", [args.mio, "build", str(HERE / "cases/traps/nil_function.mio"), "-o", str(bin_dir / "nil_function")], env=env)
    if p.returncode != 0:
        record("null function pointer traps", "FAIL", "build failed")
    else:
        r, _ = run(out, "run_nil_function", [str(bin_dir / "nil_function")])
        trapped = r.returncode != 0 and "nil reference" in (r.stdout + r.stderr) and "unreachable" not in r.stdout
        record("null function pointer traps instead of jumping to 0", "PASS" if trapped else "FAIL", "rc=%d" % r.returncode)
    p, _ = run(out, "mio_build_empty_buffer", [args.mio, "build", str(HERE / "cases/traps/empty_buffer.mio"), "-o", str(bin_dir / "empty_buffer")], env=env)
    if p.returncode != 0:
        record("empty buffer is a valid argument", "FAIL", "build failed")
    else:
        r, _ = run(out, "run_empty_buffer", [str(bin_dir / "empty_buffer")])
        record("empty buffer is a valid argument", "PASS" if r.returncode == 0 and r.stdout.strip() == "hai" else "FAIL")
    p, _ = run(out, "mio_build_stale_address", [args.mio, "build", str(HERE / "cases/traps/stale_address.mio"), "-o", str(bin_dir / "stale_address")], env=env)
    if p.returncode != 0:
        record("stale linux.address traps instead of calling an unloaded library", "FAIL", "build failed")
    else:
        r, _ = run(out, "run_stale_address", [str(bin_dir / "stale_address")], cwd=out / "lib")
        lines = r.stdout.split()
        trapped = r.returncode != 0 and "nil reference" in (r.stdout + r.stderr) and lines[:2] == ["42", "before"] and "unreachable" not in r.stdout
        record("stale linux.address traps instead of calling an unloaded library", "PASS" if trapped else "FAIL", "rc=%d" % r.returncode)


def bench(args, out):
    lib = out / "lib"
    shutil.copy(lib / "libmio_math.so", lib / "libmio_math_cold.so")
    exe = out / "bin" / "linux_ffi_bench"
    p, _ = run(out, "mio_build_bench", [args.mio, "build", str(ROOT / "benchmarks/ffi/linux_ffi_bench.mio"), "-L", str(lib), "-lmio_math", "--rpath", str(lib), "-o", str(exe)], env=mio_env(args))
    if p.returncode != 0:
        record("benchmark builds", "FAIL")
        return
    env = dict(os.environ, MIO_FFI_LIBDIR=str(lib), MIO_FFI_COLD_LIB=str(lib / "libmio_math_cold.so"))
    r, _ = run(out, "run_bench", [str(exe)], env=env, timeout=900)
    (out / "bench.txt").write_text(r.stdout)
    record("benchmark runs and results agree", "PASS" if r.returncode == 0 and r.stdout.strip().endswith("DONE") else "FAIL")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mio", required=True, help="native Mio CLI built from the source under test")
    ap.add_argument("--output", required=True)
    ap.add_argument("--bunko", default=str(ROOT / "bunko"))
    ap.add_argument("--cc", default=os.environ.get("CC", "cc"))
    ap.add_argument("--include-dir", action="append", default=[])
    ap.add_argument("--lib-dir", action="append", default=[])
    ap.add_argument("--only", action="append", default=[])
    ap.add_argument("--skip-modes", action="store_true")
    ap.add_argument("--no-gc-stress", action="store_true")
    ap.add_argument("--bench", action="store_true", help="also run benchmarks/ffi/linux_ffi_bench.mio")
    args = ap.parse_args()
    out = Path(args.output).resolve()
    shutil.rmtree(out, ignore_errors=True)
    (out / "logs").mkdir(parents=True)
    if not build_modules(args, out) or not inspect_modules(args, out):
        finish(out)
    run_cases(args, out)
    if not args.skip_modes:
        trap_cases(args, out)
        elf_modes(args, out)
    if args.bench:
        bench(args, out)
    finish(out)


def finish(out):
    failed = [r for r in results if r["status"] == "FAIL"]
    (out / "report.json").write_text(json.dumps({"results": results, "failed": len(failed), "total": len(results)}, indent=1))
    print("\n%d checks, %d failed" % (len(results), len(failed)))
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
