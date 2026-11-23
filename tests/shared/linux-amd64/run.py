#!/usr/bin/env python3
"""Linux x86-64 shared-library generation acceptance (`mio build --shared`).

Builds four independent shared libraries and one aggregate from real Mio modules, inspects every generated file with
the system ELF tools, loads them from a real C host with dlopen/dlsym/dlclose, calls the exported functions, and runs
the negative and regression checks. Compiling alone is never counted as evidence. Statuses: PASS, FAIL, NOT_RUN.
"""

import argparse
import json
import os
import platform
import re
import shlex
import shutil
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
MODULES = HERE / "modules"
NEGATIVE = HERE / "negative"
HOST_C = HERE / "host" / "host.c"

# The exact C ABI of each stand-alone library. A library exports precisely this set: nothing else, ever.
EXPECTED = {
    "math": ["mio_math_abs32", "mio_math_add", "mio_math_clamp64", "mio_math_dot10", "mio_math_floor32", "mio_math_gcd",
             "mio_math_hypot_sq", "mio_math_is_even", "mio_math_lcm", "mio_math_max32", "mio_math_mul64", "mio_math_sqrt64"],
    "bytes": ["mio_bytes_adler32", "mio_bytes_free", "mio_bytes_get", "mio_bytes_index", "mio_bytes_len", "mio_bytes_new",
              "mio_bytes_write_byte"],
    "text": ["mio_text_byte_len", "mio_text_count_vowels", "mio_text_free", "mio_text_has_prefix", "mio_text_index",
             "mio_text_is_utf8", "mio_text_new", "mio_text_push_byte", "mio_text_rune_count"],
    "collections": ["mio_collections_contains", "mio_collections_free", "mio_collections_get", "mio_collections_len",
                    "mio_collections_new", "mio_collections_push", "mio_collections_sort", "mio_collections_sum"],
}
EXPECTED["std"] = sorted(sum(EXPECTED.values(), []))
HOST_SUITES = ["math", "bytes", "text", "collections", "std", "multi", "stress", "reload"]


class Report:
    def __init__(self, output):
        self.output = output
        output.mkdir(parents=True, exist_ok=True)
        self.checks = []
        self.measurements = {}

    def record(self, name, status, **details):
        self.checks.append(dict(name=name, status=status, **details))
        print(f"{status} {name}", flush=True)

    def tool(self, name, argv, *, check=0, env=None, timeout=300, cwd=ROOT):
        """Runs argv (a list: never a shell string), logs it, returns the CompletedProcess."""
        argv = [str(part) for part in argv]
        log = self.output / "logs" / (re.sub(r"[^A-Za-z0-9_.-]+", "_", name) + ".log")
        log.parent.mkdir(parents=True, exist_ok=True)
        started = time.monotonic()
        try:
            process = subprocess.run(argv, cwd=cwd, capture_output=True, timeout=timeout, env=env)
        except (OSError, subprocess.TimeoutExpired) as error:
            log.write_text(f"$ {shlex.join(argv)}\n{error}\n", encoding="utf-8")
            return None
        process.seconds = time.monotonic() - started
        log.write_bytes(f"$ {shlex.join(argv)}\nexit={process.returncode}\n--- stdout ---\n".encode()
                        + process.stdout + b"\n--- stderr ---\n" + process.stderr)
        return process

    def expect(self, name, process, ok, **details):
        self.record(name, "PASS" if process is not None and ok else "FAIL", **details)
        return process if process is not None and ok else None

    def finish(self):
        (self.output / "report.json").write_text(json.dumps(
            {"target": "linux-amd64", "host": platform.platform(), "checks": self.checks, "measurements": self.measurements},
            indent=2) + "\n", encoding="utf-8")
        failed = [c for c in self.checks if c["status"] == "FAIL"]
        passed = [c for c in self.checks if c["status"] == "PASS"]
        print(f"\n{len(passed)} passed, {len(failed)} failed, {len(self.checks) - len(passed) - len(failed)} not run")
        return 1 if failed else 0


def dynsyms(path, defined):
    """Dynamic symbols of an object through `nm -D`; `defined` selects --defined-only, otherwise --undefined-only."""
    process = subprocess.run(["nm", "-D", "--defined-only" if defined else "--undefined-only", str(path)], capture_output=True, text=True)
    names = []
    for line in process.stdout.splitlines():
        parts = line.split()
        if parts:
            names.append(parts[-1].split("@")[0])
    return sorted(names)


def inspect_library(report, name, path, soname, expected, needed=()):
    """ELF inspection of one generated file with the real system tools; every assertion reads the tools' output."""
    def run(label, argv):
        process = report.tool(f"{name}.{label}", argv)
        return process.stdout.decode(errors="replace") if process is not None and process.returncode == 0 else None

    if shutil.which("file"):
        desc = run("file", ["file", "-b", path])
        report.record(f"{name}: file reports ELF 64-bit x86-64 shared object", "PASS" if desc and "ELF 64-bit LSB shared object" in desc and "x86-64" in desc else "FAIL", detail=desc)
    else:
        report.record(f"{name}: file(1) reports ELF 64-bit x86-64 shared object", "NOT_RUN", reason="the file command is not installed; readelf -h below checks the same fields")
    header = run("readelf-h", ["readelf", "-h", path]) or ""
    ok = all(item in header for item in ("ELF64", "2's complement, little endian", "Version:                           1 (current)",
                                         "DYN (Shared object file)", "Advanced Micro Devices X86-64"))
    report.record(f"{name}: ELF header is ELFCLASS64 ELFDATA2LSB ET_DYN EM_X86_64 version 1", "PASS" if ok else "FAIL")
    program = run("readelf-l", ["readelf", "-W", "-l", path]) or ""
    loads = [line.split() for line in program.splitlines() if line.strip().startswith("LOAD")]
    wx = [line for line in loads if "W" in "".join(line[6:-1]) and "E" in "".join(line[6:-1])]
    stack = re.search(r"GNU_STACK\s+(?:\S+\s+){5}([RWE ]+?)\s+0x", program)
    report.record(f"{name}: PT_LOAD segments present, none writable and executable", "PASS" if loads and not wx else "FAIL")
    report.record(f"{name}: PT_GNU_STACK present and not executable", "PASS" if stack and "E" not in stack.group(1) else "FAIL")
    report.record(f"{name}: PT_DYNAMIC and PT_GNU_RELRO present", "PASS" if "DYNAMIC" in program and "GNU_RELRO" in program else "FAIL")
    sections = run("readelf-S", ["readelf", "-W", "-S", path]) or ""
    report.record(f"{name}: .dynsym .dynstr .dynamic and a hash section exist", "PASS" if all(s in sections for s in (".dynsym", ".dynstr", ".dynamic")) and (".gnu.hash" in sections or ".hash" in sections) else "FAIL")
    dynamic = run("readelf-d", ["readelf", "-d", path]) or ""
    soname_ok = re.search(r"\(SONAME\)\s+Library soname: \[" + re.escape(soname) + r"\]", dynamic) is not None
    report.record(f"{name}: DT_SONAME is {soname}", "PASS" if soname_ok else "FAIL")
    found = re.findall(r"\(NEEDED\)\s+Shared library: \[([^\]]+)\]", dynamic)
    report.record(f"{name}: DT_NEEDED is exactly {sorted(needed)}", "PASS" if sorted(found) == sorted(needed) else "FAIL", detail=found)
    report.record(f"{name}: no TEXTREL", "PASS" if "TEXTREL" not in dynamic and "TEXTREL" not in (run("readelf-flags", ["readelf", "-W", "-d", path]) or "") else "FAIL")
    symbols = run("readelf-Ws", ["readelf", "-W", "-s", "--dyn-syms", path]) or ""
    relocations = run("readelf-r", ["readelf", "-W", "-r", path]) or ""
    nm_defined = dynsyms(path, True)
    nm_undefined = dynsyms(path, False)
    report.record(f"{name}: dynamic symbol table defines exactly the {len(expected)} declared exports (nm -D)", "PASS" if nm_defined == sorted(expected) else "FAIL", detail=sorted(set(nm_defined) ^ set(expected)))
    objdump = run("objdump-T", ["objdump", "-T", path]) or ""
    report.record(f"{name}: objdump -T agrees (every export is a .text function, nothing else)", "PASS" if all(re.search(r"\.text\s+\S+\s+(?:Base\s+)?" + re.escape(s) + r"\b", objdump) for s in expected) and not re.search(r"\brt\.|\bfn\.", objdump) else "FAIL")
    unexpected_undefined = [s for s in nm_undefined if s not in needed_symbols(needed)]
    report.record(f"{name}: no unexpected undefined dynamic symbols", "PASS" if not unexpected_undefined else "FAIL", detail=unexpected_undefined)
    leaked = [s for s in re.findall(r"\S+$", symbols, re.M) if re.match(r"(rt\.|fn\.|shared\.|\$)", s)]
    dynsym_section = symbols.split("Symbol table '.symtab'")[0]
    leaked_dyn = [s for s in re.findall(r"\S+$", dynsym_section, re.M) if re.match(r"(rt\.|fn\.|shared\.|\$)", s)]
    report.record(f"{name}: no internal runtime symbol is exported", "PASS" if not leaked_dyn else "FAIL", detail=leaked_dyn[:5])
    rela_dyn = re.search(r"Relocation section '\.rela\.dyn'", relocations)
    non_import = [l for l in relocations.splitlines() if re.match(r"\s*[0-9a-f]{8,}\s", l) and not re.search(r"R_X86_64_(GLOB_DAT|JUMP_SLOT)", l)]
    report.record(f"{name}: dynamic relocations are only GLOB_DAT/JUMP_SLOT imports (none for self-contained modules)", "PASS" if not non_import and (needed or not rela_dyn) else "FAIL", detail=non_import[:3])
    text = report.tool(f"{name}.objdump-d", ["objdump", "-d", "-j", ".text", "--no-show-raw-insn", path])
    fixed = [l.strip() for l in (text.stdout.decode(errors="replace") if text else "").splitlines() if re.search(r"movabs \$0x1000[0-9a-f]{4}\b", l)]
    report.record(f"{name}: no instruction in .text loads a fixed data-area address (position independent)", "PASS" if text is not None and not fixed else "FAIL", detail=fixed[:2])
    ldd = report.tool(f"{name}.ldd", ["ldd", "-r", path])
    bad = ldd is None or ldd.returncode != 0 or b"undefined symbol" in ldd.stdout + ldd.stderr
    report.record(f"{name}: ldd -r reports no unresolved symbol", "FAIL" if bad else "PASS")
    return symbols


def needed_symbols(needed):
    return {"libm.so.6": {"sqrt", "cbrt"}}.get(needed[0], set()) if needed else set()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", required=True, help="the native mio compiler under test")
    parser.add_argument("--baseline", help="a mio built from the base commit: static executables must be byte-identical")
    parser.add_argument("--bunko-root", default=str(ROOT / "bunko"))
    parser.add_argument("--output", default=str(ROOT / "build/shared-linux-amd64"))
    args = parser.parse_args()
    mio = str(Path(args.mio).resolve())
    out = Path(args.output).resolve()
    shutil.rmtree(out, ignore_errors=True)
    report = Report(out)
    if platform.system() != "Linux" or platform.machine() not in ("x86_64", "AMD64"):
        report.record("host is Linux x86-64", "NOT_RUN", reason=platform.platform())
        report.finish()
        return 2
    libs = out / "lib"
    libs.mkdir(parents=True)
    base = ["--bunko-root", args.bunko_root]

    # ---- A. independent libraries and B. the aggregate, built individually from real module directories
    sizes = {}
    for name in ("math", "bytes", "text", "collections"):
        target = libs / f"libmio_{name}.so"
        process = report.tool(f"build.{name}", [mio, "build", MODULES / f"mio_{name}", "--shared", "--soname", f"libmio_{name}.so", "-o", target, *base])
        report.expect(f"build libmio_{name}.so from modules/mio_{name}", process, process is not None and process.returncode == 0 and target.is_file() and not process.stderr,
                      seconds=round(process.seconds, 2) if process else None)
        if target.is_file():
            sizes[target.name] = target.stat().st_size
            report.measurements[f"build_seconds.{name}"] = round(process.seconds, 2)
    target = libs / "libmio_std.so"
    process = report.tool("build.std", [mio, "build", *(MODULES / f"mio_{n}" for n in ("math", "bytes", "text", "collections")), "--shared", "--soname", "libmio_std.so", "-o", target, *base])
    report.expect("build libmio_std.so aggregating four modules (several input directories)", process, process is not None and process.returncode == 0 and target.is_file() and not process.stderr)
    if target.is_file():
        sizes[target.name] = target.stat().st_size
        report.measurements["build_seconds.std"] = round(process.seconds, 2)
    verbose = report.tool("build.verbose-log", [mio, "build", MODULES / "mio_math", "--shared", "--verbose", "-o", out / "verbose.so", *base])
    report.expect("--verbose prints the reproducible link command (argv, no shell)", verbose,
                  verbose is not None and verbose.returncode == 0 and re.search(rb"mio: link: \S*(ld|lld|cc|gcc|clang)\S* -shared (-nostdlib )?", verbose.stderr) is not None)
    first = (libs / "libmio_math.so").read_bytes() if (libs / "libmio_math.so").is_file() else b""
    again = report.tool("build.math-again", [mio, "build", MODULES / "mio_math", "--shared", "--soname", "libmio_math.so", "-o", out / "again.so", *base])
    report.expect("builds are reproducible (second build is byte-identical)", again, again is not None and (out / "again.so").is_file() and (out / "again.so").read_bytes() == first and first != b"")
    report.measurements["sizes_bytes"] = sizes

    # ---- ELF inspection of every generated file
    for name in ("math", "bytes", "text", "collections", "std"):
        path = libs / f"libmio_{name}.so"
        if path.is_file():
            inspect_library(report, f"libmio_{name}", path, f"libmio_{name}.so", EXPECTED[name])
    # exported-symbol conflict detection across the aggregate: no duplicates among the standalone libraries
    flat = sum((EXPECTED[n] for n in ("math", "bytes", "text", "collections")), [])
    report.record("aggregate: no duplicate exported symbol across the four modules", "PASS" if len(flat) == len(set(flat)) else "FAIL")

    # ---- C host: dlopen / dlsym / calls / dlclose
    compilers = [c for c in ("gcc", "clang", "cc") if shutil.which(c)]
    hosts = {}
    for cc in compilers[:2] if "gcc" in compilers and "clang" in compilers else compilers[:1]:
        exe = out / f"host-{cc}"
        process = report.tool(f"host.compile.{cc}", [cc, "-O1", "-Wall", "-Werror", "-o", exe, HOST_C, "-ldl", "-lm"])
        if report.expect(f"compile the C host with {cc}", process, process is not None and process.returncode == 0, stderr=process.stderr.decode()[:300] if process else ""):
            hosts[cc] = exe
    if not hosts:
        report.record("a C compiler is available for the host program", "NOT_RUN")
    for cc, exe in hosts.items():
        for suite in HOST_SUITES:
            process = report.tool(f"host.{cc}.{suite}", [exe, libs, suite])
            ok = process is not None and process.returncode == 0 and b"FAIL" not in process.stdout and process.stdout.rstrip().endswith(f"{suite}: OK".encode())
            checks = process.stdout.count(b"PASS ") if process else 0
            report.record(f"C host ({cc}) suite {suite}: dlopen RTLD_NOW|RTLD_LOCAL, dlsym, calls, dlclose ({checks} checks)", "PASS" if ok else "FAIL",
                          output=(process.stdout.decode(errors="replace")[-400:] if process and not ok else ""), exit=process.returncode if process else None)
            if suite == "stress" and process is not None:
                report.measurements[f"stress_seconds.{cc}"] = round(process.seconds, 2)
    # LD_BIND_NOW and a fresh process per library: lazy binding must not hide a missing symbol
    if hosts:
        exe = next(iter(hosts.values()))
        env = dict(os.environ, LD_BIND_NOW="1", LD_DEBUG="")
        process = report.tool("host.bindnow.std", [exe, libs, "std"], env=env)
        report.expect("aggregate loads and runs with LD_BIND_NOW=1", process, process is not None and process.returncode == 0)
        valgrind = shutil.which("valgrind")
        if valgrind:
            process = report.tool("host.valgrind.math", [valgrind, "--error-exitcode=9", "-q", exe, libs, "math"], timeout=600)
            report.expect("valgrind: no invalid access in the generated math library", process, process is not None and process.returncode == 0)
        else:
            report.record("valgrind memory check", "NOT_RUN", reason="valgrind is not installed")

    # ---- declared external dependency (libm): honest DT_NEEDED, and a link failure without the declaration
    extdep = MODULES / "mio_extdep"
    without = report.tool("extdep.without-lm", [mio, "build", extdep, "--shared", "-o", out / "no_lm.so", *base])
    report.expect("a module calling C sqrt without -lm fails to link with a clear diagnostic", without,
                  without is not None and without.returncode != 0 and b"E_SHARED_LINK" in without.stderr and b"undefined reference" in without.stderr and not (out / "no_lm.so").exists())
    with_lm = report.tool("extdep.with-lm", [mio, "build", extdep, "--shared", "-lm", "--soname", "libmio_extdep.so", "-o", libs / "libmio_extdep.so", *base])
    if report.expect("the same module links with the declared dependency -lm", with_lm, with_lm is not None and with_lm.returncode == 0 and (libs / "libmio_extdep.so").is_file()):
        inspect_library(report, "libmio_extdep", libs / "libmio_extdep.so", "libmio_extdep.so", ["mio_extdep_cbrt", "mio_extdep_sqrt"], needed=["libm.so.6"])
        source = out / "extdep.c"
        source.write_text('#include <dlfcn.h>\n#include <math.h>\n#include <stdio.h>\nint main(int c,char**v){void*h=dlopen(v[1],RTLD_NOW|RTLD_LOCAL);if(!h){puts(dlerror());return 1;}'
                          'double(*a)(double)=dlsym(h,"mio_extdep_sqrt");double(*b)(double)=dlsym(h,"mio_extdep_cbrt");if(!a||!b)return 2;'
                          'volatile double x=27.0,y=2.25;if(a(y)!=1.5||b(x)!=cbrt(x))return 3;puts("extdep OK");return dlclose(h);}\n')
        if hosts:
            cc = next(iter(hosts))
            report.tool("extdep.compile", [cc, "-o", out / "extdep-host", source, "-ldl", "-lm"])
            process = report.tool("extdep.run", [out / "extdep-host", libs / "libmio_extdep.so"])
            report.expect("C host calls Mio -> libm sqrt/cbrt through the declared dependency", process, process is not None and process.returncode == 0 and b"extdep OK" in process.stdout)

    # ---- negative checks: diagnostics, no artifact, never a silent success
    def negative(name, argv, needle, label, env=None):
        target = out / f"neg-{name}.so"
        process = report.tool(f"negative.{name}", [mio, "build", *argv, "--shared", "-o", target, *base], env=env)
        report.expect(f"negative: {label}", process, process is not None and process.returncode != 0 and needle.encode() in process.stdout + process.stderr and not target.exists())
    negative("non_scalar", [NEGATIVE / "non_scalar_export"], "E_CABI", "exporting a string parameter is rejected (GC objects never cross the boundary)")
    negative("not_public", [NEGATIVE / "not_public"], "E_CABI", "`export` on a private function is rejected")
    negative("bad_symbol", [NEGATIVE / "bad_symbol"], "E_CABI", "an invalid C symbol name is rejected")
    negative("reserved_main", [NEGATIVE / "reserved_main"], "E_CABI", "the reserved symbol `main` is rejected")
    negative("generic", [NEGATIVE / "generic_export"], "E_CABI", "an exported generic function is rejected")
    negative("no_prototype", [NEGATIVE / "missing_return_type"], "E_CABI", "an export without an explicit prototype is rejected")
    negative("duplicate_in_module", [NEGATIVE / "duplicate_in_module"], "duplicate exported symbol same_symbol", "duplicate exported symbols in one module are rejected")
    negative("duplicate_across", [NEGATIVE / "duplicate_across" / "a", NEGATIVE / "duplicate_across" / "b"], "duplicate exported symbol dup_symbol", "duplicate exported symbols across aggregated modules are rejected")
    negative("no_export", [NEGATIVE / "no_export"], "E_SHARED_EXPORT no exported function", "a module without any export is rejected")
    negative("native_linker", [MODULES / "mio_math", "--linker", "native"], "E_SHARED_LINKER the Mio native linker is reserved", "--linker native is reserved and says so")
    negative("missing_linker_path", [MODULES / "mio_math", "--linker", "/nonexistent/bin/ld"], "E_SHARED_LINKER linker not found: /nonexistent/bin/ld", "an explicit but missing linker is a clear error")
    empty = out / "empty-path"
    empty.mkdir()
    negative("no_linker_installed", [MODULES / "mio_math"], "E_SHARED_LINKER no linker found", "no linker on PATH is a clear error",
             env={"PATH": str(empty), "HOME": str(out), "MIO_LD": "", "MIO_CC": "", "CC": ""})
    wrong_target = report.tool("negative.target", [mio, "build", MODULES / "mio_math", "--shared", "--target", "darwin-arm64", "-o", out / "t.so", *base])
    report.expect("negative: --shared is refused for other targets", wrong_target, wrong_target is not None and wrong_target.returncode != 0 and b"--shared applies only" in wrong_target.stderr)
    no_flag = report.tool("negative.soname-without-shared", [mio, "build", MODULES / "mio_math", "--soname", "x.so", "-o", out / "t.so", *base])
    report.expect("negative: --soname without --shared is refused", no_flag, no_flag is not None and no_flag.returncode != 0 and b"require --shared" in no_flag.stderr)
    # Non-PIC input must not become shared-library output: a -fno-pic object with an absolute relocation is refused by the link.
    if hosts:
        cc = next(iter(hosts))
        bad_c = out / "nonpic.c"
        bad_c.write_text("int counter; int *address(void) { static int *p = &counter; return p; }\nint nonpic_value(void) { return counter; }\n")
        bad_o = out / "nonpic.o"
        compiled = report.tool("nonpic.compile", [cc, "-c", "-fno-pic", "-mcmodel=small", "-O0", "-fno-pie", "-o", bad_o, bad_c])
        if compiled is not None and compiled.returncode == 0:
            process = report.tool("negative.nonpic", [mio, "build", MODULES / "mio_math", "--shared", "--link", bad_o, "-o", out / "nonpic.so", *base])
            report.expect("negative: a non-PIC object linked into a shared library is refused (no TEXTREL output)", process,
                          process is not None and process.returncode != 0 and b"E_SHARED_LINK" in process.stderr and not (out / "nonpic.so").exists())

    # ---- --emit=object: the ET_REL stage on its own, inspected with readelf
    obj = out / "math.o"
    process = report.tool("emit.object", [mio, "build", MODULES / "mio_math", "--shared", "--emit=object", "-o", obj, *base])
    if report.expect("--emit=object writes the relocatable object", process, process is not None and process.returncode == 0 and obj.is_file()):
        header = report.tool("object.readelf-h", ["readelf", "-h", obj]).stdout.decode()
        rel = report.tool("object.readelf-r", ["readelf", "-W", "-r", obj]).stdout.decode()
        sections = report.tool("object.readelf-S", ["readelf", "-W", "-S", obj]).stdout.decode()
        symbols = report.tool("object.nm", ["nm", obj]).stdout.decode()
        types = set(re.findall(r"R_X86_64_\w+", rel))
        report.record("object: ET_REL x86-64 with .text .rela.text .bss .note.GNU-stack .symtab", "PASS" if "REL (Relocatable file)" in header and all(s in sections for s in (".text", ".rela.text", ".bss", ".note.GNU-stack", ".symtab")) else "FAIL")
        report.record("object: only R_X86_64_PC32 relocations (rip-relative data, no absolute address)", "PASS" if types == {"R_X86_64_PC32"} else "FAIL", detail=sorted(types))
        defined = sorted(l.split()[-1] for l in symbols.splitlines() if re.match(r"[0-9a-f]+ T ", l))
        report.record("object: exactly the declared exports are global (T)", "PASS" if defined == sorted(EXPECTED["math"]) else "FAIL")
        text = report.tool("object.objdump", ["objdump", "-d", "-j", ".text", "--no-show-raw-insn", obj]).stdout.decode()
        absolute = [l for l in text.splitlines() if re.search(r"movabs \$0x1000[0-9a-f]{4}\b", l)]
        report.record("object: no instruction loads a fixed data-area address (movabs of 0x10000000+)", "PASS" if not absolute else "FAIL", detail=absolute[:2])

    # ---- regression: static executables are untouched
    hello = out / "hello.mio"
    hello.write_text('chant("static ok");\nchant(6 * 7);\n')
    static = out / "hello-static"
    process = report.tool("static.build", [mio, "build", hello, "-o", static, *base])
    run = report.tool("static.run", [static]) if static.is_file() else None
    report.expect("static ELF executable still builds and runs", run, process is not None and process.returncode == 0 and run is not None and run.stdout == b"static ok\n42\n")
    if static.is_file():
        elf = report.tool("static.readelf", ["readelf", "-h", static]).stdout.decode()
        report.record("static executable is still ET_EXEC with no dynamic section", "PASS" if "EXEC (Executable file)" in elf else "FAIL")
    if args.baseline:
        baseline = out / "hello-baseline"
        report.tool("static.baseline", [args.baseline, "build", hello, "-o", baseline, *base])
        identical = baseline.is_file() and static.is_file() and baseline.read_bytes() == static.read_bytes()
        examples = []
        for example in ("arrays", "closures", "generics", "json", "maps", "nullable", "recursion", "regexp"):
            a, b = out / f"ex-{example}.base", out / f"ex-{example}.new"
            report.tool(f"static.example.{example}.base", [args.baseline, "build", ROOT / "examples" / f"{example}.mio", "-o", a, *base])
            report.tool(f"static.example.{example}.new", [mio, "build", ROOT / "examples" / f"{example}.mio", "-o", b, *base])
            examples.append(a.is_file() and b.is_file() and a.read_bytes() == b.read_bytes())
        report.record("static executables are byte-identical to the base commit's compiler (hello + 8 examples)", "PASS" if identical and all(examples) else "FAIL", examples=examples)
    else:
        report.record("static executables byte-identical to the base commit's compiler", "NOT_RUN", reason="--baseline not given")

    return report.finish()


if __name__ == "__main__":
    sys.exit(main())
