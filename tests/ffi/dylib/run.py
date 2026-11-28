#!/usr/bin/env python3
"""Run Apple Silicon dylib integration with native and independent evidence."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import struct
import subprocess
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
BUNKO = ROOT / "bunko"
MODULES = (
    ("math", HERE / "math.mio", {
        "add": ("mio_math_add", "ggg"), "root": ("mio_math_sqrt", "jj"),
        "increment": ("mio_math_inc", "gg"),
        "signed_add": ("mio_math_signed_add", "eee"),
        "unsigned_add": ("mio_math_unsigned_add", "fff"),
        "half": ("mio_math_half", "ii"),
        "eighth": ("mio_math_eighth", "ggggggggg"), "mixed": ("mio_math_mixed", "jgjgj"),
    }),
    ("convert", "kiso/convert", {"scale_float64": ("mio_convert_scale_float64", "jgg")}),
    ("json", "kiso/encoding/json", {"digit": ("mio_json_digit", "lg")}),
    ("wide", HERE / "wide.mio", {
        "remember": ("mio_wide_remember", "vk"),
        "pointer_identity": ("mio_wide_pointer", "kk"),
        "unsigned_identity": ("mio_wide_unsigned", "hh"),
        "narrow": ("mio_wide_narrow", "gabcdef"),
        "signed_byte": ("mio_wide_signed_byte", "aa"),
        "unsigned_half": ("mio_wide_unsigned_half", "dd"),
        "spill": ("mio_wide_spill", "j" + "gjgi" * 4 + "aidjlkf"),
    }),
    ("state", HERE / "state.mio", {
        "current": ("mio_state_current", "g"), "next": ("mio_state_next", "g"),
        "initialize": ("mio_state_initialize", "vg"), "finalize": ("mio_state_finalize", "v"),
    }),
)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    mio = args.mio.resolve()
    output = args.output.resolve()
    logs = output / "logs"
    artifacts = output / "artifacts"
    for directory in (logs, artifacts):
        directory.mkdir(parents=True, exist_ok=True)

    report = {
        "source_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "host": f"{platform.system()} {platform.machine()}",
        "mio_sha256": hashlib.sha256(mio.read_bytes()).hexdigest(),
        "checks": {}, "libraries": {},
    }
    checks = report["checks"]

    def record(name, status, **fields):
        checks[name] = {"status": status, **fields}

    def run(name, argv, *, empty_path=False, success=True, contains=None, stderr_contains=None):
        argv = list(map(str, argv))
        env = dict(os.environ, PATH="") if empty_path else None
        try:
            result = subprocess.run(argv, cwd=ROOT, env=env, capture_output=True, timeout=180)
        except (OSError, subprocess.TimeoutExpired) as error:
            record(name, "FAIL", command=argv, error=str(error))
            raise
        (logs / f"{name}.stdout").write_bytes(result.stdout)
        (logs / f"{name}.stderr").write_bytes(result.stderr)
        valid = (result.returncode == 0) == success
        if contains is not None:
            valid &= contains.encode() in result.stdout + result.stderr
        if stderr_contains is not None:
            valid &= stderr_contains.encode() in result.stderr
        record(name, "PASS" if valid else "FAIL", command=argv, exit_code=result.returncode)
        if not valid:
            raise RuntimeError(f"{name} failed, see {logs}")
        return result.stdout.decode()

    libraries = []
    for name, _, _ in MODULES:
        for check in ("build", "file", "header", "dependencies", "symbols", "sign", "load-commands", "dyld-exports", "inspect"):
            record(f"{name}-{check}", "NOT_RUN")
    for name in ("data-fixture", "loader-build", "loader-codesign", "loader-run", "loader-gc-build",
                 "loader-gc-run", "loader-verdict", "c-build", "c-run", "reject-missing-export",
                 "reject-runtime-body", "reject-cross-target", "reject-recursion", "reject-duplicate-symbol",
                 "reject-missing-export-seatbelt", "foreign-fixture", "foreign-build", "foreign-run",
                 "foreign-gc-build", "foreign-gc-run", "foreign-codesign",
                 "memory-build", "memory-run", "memory-gc-build", "memory-gc-run", "memory-openssl",
                 "wide-c-build", "wide-c-run", "state-c-build", "state-c-run", "state-other-build",
                 "reject-dylib-top-level", "dependencies-leaf", "dependencies-middle", "dependencies-client",
                 "dependencies-loader-path-build", "dependencies-loader-path-run", "dependencies-executable-path-build",
                 "dependencies-executable-path-run", "dependencies-sign", "dependencies-export-list",
                 "dependencies-commands", "dependencies-loads", "dependencies-imports", "dependencies-fixups",
                 "dependencies-missing-leaf", "dependencies-undefined-symbol"):
        record(name, "NOT_RUN")

    try:
        if report["host"] != "Darwin arm64":
            raise RuntimeError("Apple Silicon is required for this integration test")
        for name, source, exports in MODULES:
            lib = artifacts / f"libmio_{name}.dylib"
            flags = [flag for source_name, (symbol, _) in exports.items()
                     for flag in ("--export", f"{source_name}={symbol}")]
            run(f"{name}-build", [mio, "build", "--kind", "dylib", source, *flags,
                                    "--bunko-root", BUNKO, "-o", lib], empty_path=True)
            libraries.append(lib)
            run(f"{name}-file", ["file", lib], contains="shared library arm64")
            header = run(f"{name}-header", ["otool", "-hv", lib], contains="DYLIB")
            deps = run(f"{name}-dependencies", ["otool", "-L", lib], contains=f"@rpath/{lib.name}")
            symbols = run(f"{name}-symbols", ["nm", "-gU", lib])
            run(f"{name}-sign", ["codesign", "--verify", "--strict", "--verbose=2", lib])
            commands = run(f"{name}-load-commands", ["otool", "-l", lib], contains="LC_CODE_SIGNATURE")
            trie = run(f"{name}-dyld-exports", ["xcrun", "dyld_info", "-exports", lib])
            expected = {"_mio_module_abi_version", "_mio_ffi_v1_g_mio_module_abi_version"}
            for symbol, prototype in exports.values():
                expected.update({f"_{symbol}", f"_mio_ffi_v1_{prototype}_{symbol}"})
            actual = {line.split()[-1] for line in symbols.splitlines()}
            raw = lib.read_bytes()
            magic, cpu, _, kind = struct.unpack_from("<4I", raw)
            valid = (magic, cpu, kind) == (0xFEEDFACF, 0x100000C, 6) and actual == expected
            valid &= "/usr/lib/libSystem.B.dylib" in deps
            valid &= all(command in commands for command in ("LC_DYLD_EXPORTS_TRIE", "LC_SYMTAB", "LC_ID_DYLIB"))
            valid &= {line.split()[-1] for line in trie.splitlines() if line.strip().startswith("0x")} == expected
            record(f"{name}-inspect", "PASS" if valid else "FAIL")
            report["libraries"][name] = dict(
                file=str(lib), sha256=hashlib.sha256(raw).hexdigest(),
                header=header, exports=sorted(actual), dependencies=deps,
            )
            if not valid:
                raise RuntimeError(f"unexpected Mach-O header, dependencies or exports for {lib}")

        data = artifacts / "libdata.dylib"
        run("data-fixture", ["xcrun", "clang", "-arch", "arm64", "-dynamiclib", HERE / "data.s", "-o", data])
        loader = artifacts / "loader"
        run("loader-build", [mio, "build", HERE / "loader.mio", "--bunko-root", BUNKO, "-o", loader], empty_path=True)
        run("loader-codesign", ["codesign", "--verify", "--strict", loader])
        transcript = run("loader-run", [loader, *libraries[:3], data], empty_path=True, contains="dylib loader: ok")
        iterations, direct_ms, ffi_ms = map(int, transcript.splitlines()[1:])
        report["timing"] = dict(iterations=iterations, native_ms=direct_ms, ffi_ms=ffi_ms)
        stress = artifacts / "loader-gc"
        run("loader-gc-build", [mio, "build", HERE / "loader.mio", "--gc-stress", "--bunko-root", BUNKO, "-o", stress], empty_path=True)
        run("loader-gc-run", [stress, *libraries[:3], data], empty_path=True, contains="dylib loader: ok")
        record("loader-verdict", "PASS")

        oracle = artifacts / "oracle"
        run("c-build", ["xcrun", "clang", "-arch", "arm64", "-Wall", "-Wextra", "-Werror", HERE / "oracle.c",
                        *libraries[:3], "-Wl,-rpath,@loader_path", "-o", oracle])
        run("c-run", [oracle], empty_path=True)

        wide_oracle = artifacts / "wide-oracle"
        run("wide-c-build", ["xcrun", "clang", "-arch", "arm64", "-mmacosx-version-min=11.0",
                             "-Wall", "-Wextra", "-Werror", HERE / "wide_oracle.c", libraries[3],
                             "-Wl,-rpath,@loader_path", "-o", wide_oracle])
        run("wide-c-run", [wide_oracle], empty_path=True)

        state_other = artifacts / "libmio_state_other.dylib"
        state_flags = [flag for source_name, (symbol, _) in MODULES[4][2].items()
                       for flag in ("--export", f"{source_name}={symbol}")]
        run("state-other-build", [mio, "build", "--kind=dylib", HERE / "state.mio", *state_flags,
                                  "--bunko-root", BUNKO, "-o", state_other], empty_path=True)
        state_oracle = artifacts / "state-oracle"
        run("state-c-build", ["xcrun", "clang", "-arch", "arm64", "-mmacosx-version-min=11.0",
                             "-Wall", "-Wextra", "-Werror", HERE / "state_oracle.c", "-o", state_oracle])
        run("state-c-run", [state_oracle, libraries[4], state_other], empty_path=True)

        foreign = artifacts / "libforeign.dylib"
        run("foreign-fixture", ["xcrun", "clang", "-arch", "arm64", "-mmacosx-version-min=11.0",
                               "-Wall", "-Wextra", "-Werror", "-O2", "-dynamiclib", HERE / "foreign.c", "-o", foreign])
        for suffix, flags in (("", []), ("-gc", ["--gc-stress"])):
            program = artifacts / f"foreign{suffix}"
            run(f"foreign{suffix}-build", [mio, "build", HERE / "foreign.mio", *flags,
                                           "--bunko-root", BUNKO, "-o", program], empty_path=True)
            run(f"foreign{suffix}-run", [program, foreign, libraries[0]], empty_path=True, contains="Darwin C FFI: ok")
        run("foreign-codesign", ["codesign", "--verify", "--strict", "--verbose=2", foreign, artifacts / "foreign"])

        crypto = next((path for path in (
            Path("/opt/homebrew/opt/openssl@3/lib/libcrypto.dylib"),
            Path("/opt/homebrew/opt/openssl/lib/libcrypto.dylib"),
        ) if path.is_file()), None)
        for suffix, flags in (("", []), ("-gc", ["--gc-stress"])):
            program = artifacts / f"memory{suffix}"
            run(f"memory{suffix}-build", [mio, "build", HERE / "memory.mio", *flags,
                                           "--bunko-root", BUNKO, "-o", program], empty_path=True)
            run(f"memory{suffix}-run", [program, foreign, *([crypto] if crypto else [])],
                empty_path=True, contains="Darwin native memory: ok")
        record("memory-openssl", "PASS" if crypto else "NOT_AVAILABLE",
               path=str(crypto) if crypto else None)

        dep = artifacts / "dependency"
        libdir = dep / "libs"
        linked = dep / "linked"
        for directory in (libdir, linked):
            directory.mkdir(parents=True, exist_ok=True)
        clang = ["xcrun", "clang", "-arch", "arm64", "-mmacosx-version-min=11.0", "-dynamiclib"]
        leaf = libdir / "libleaf.dylib"
        middle = libdir / "libmiddle.dylib"
        run("dependencies-leaf", [*clang, "-install_name", "@loader_path/libleaf.dylib",
                                  HERE / "dependency_leaf.c", "-o", leaf])
        run("dependencies-middle", [*clang, "-install_name", "@rpath/libmiddle.dylib",
                                    HERE / "dependency_middle.c", leaf, "-o", middle])
        client = dep / "client"
        run("dependencies-client", [mio, "build", HERE / "dependency_loader.mio",
                                    "--bunko-root", BUNKO, "-o", client], empty_path=True)
        for resolution, rpath in (("loader-path", "@loader_path/../libs"),
                                  ("executable-path", "@executable_path/libs")):
            image = linked / f"libmio_{resolution}.dylib"
            run(f"dependencies-{resolution}-build",
                [mio, "build", "--kind=dylib", HERE / "dependency.mio", "--export=evaluate=mio_dep_eval",
                 "--export=clock=mio_dep_clock", "-L", libdir, "-l", "middle", "-framework", "CoreFoundation",
                 "--rpath", rpath, "--bunko-root", BUNKO, "-o", image])
            run(f"dependencies-{resolution}-run", [client, image], empty_path=True,
                contains="Darwin dependency loader: ok")
        run("dependencies-sign", ["codesign", "--verify", "--strict", "--verbose=2", image])
        exported = run("dependencies-export-list", ["nm", "-gU", image])
        expected = {"_mio_dep_eval", "_mio_dep_clock", "_mio_module_abi_version",
                    "_mio_ffi_v1_gg_mio_dep_eval", "_mio_ffi_v1_j_mio_dep_clock", "_mio_ffi_v1_g_mio_module_abi_version"}
        if {line.split()[-1] for line in exported.splitlines()} != expected:
            record("dependencies-export-list", "FAIL", reason="unexpected exports")
            raise RuntimeError("dependency image exports differ from declared ABI")
        run("dependencies-commands", ["otool", "-l", image], contains="@executable_path/libs")
        run("dependencies-loads", ["otool", "-L", image], contains="CoreFoundation.framework")
        run("dependencies-imports", ["xcrun", "dyld_info", "-imports", image], contains="_mio_dependency_middle")
        run("dependencies-fixups", ["xcrun", "dyld_info", "-fixups", image], contains="_CFAbsoluteTimeGetCurrent")
        moved = leaf.with_suffix(".missing")
        leaf.rename(moved)
        try:
            run("dependencies-missing-leaf", [client, image], empty_path=True, success=False, contains="libleaf.dylib")
        finally:
            moved.rename(leaf)
        unresolved = dep / "unresolved.mio"
        unresolved.write_text('waza external() -> int64 = c "missing_dependency_symbol";\nwaza get() -> int64 { kaesu external(); }\n')
        run("dependencies-undefined-symbol",
            [mio, "build", "--kind=dylib", unresolved, "--export=get=mio_dep_missing", "--bunko-root", BUNKO,
             "-o", linked / "libmio_missing.dylib"], success=False, stderr_contains="missing_dependency_symbol")

        invalid = artifacts / "libinvalid.dylib"
        build = [mio, "build", "--kind=dylib", "--bunko-root", BUNKO, "-o", invalid]
        run("reject-missing-export", [*build, HERE / "math.mio"], success=False, contains="--export")
        # Apply the sandbox to the compiler only so the harness can capture
        # its inherited stderr even when opening /dev/stderr is forbidden.
        sandbox = ["/usr/bin/sandbox-exec", "-p", '(version 1) (allow default) (deny file-write* (subpath "/dev"))']
        run("reject-missing-export-seatbelt", [*sandbox, *build, HERE / "math.mio"], success=False,
            stderr_contains="--export")
        run("reject-runtime-body", [*build, "kiso/convert", "--export=parse_int=mio_parse"], success=False, contains="E_DYLIB")
        run("reject-cross-target", [*build, HERE / "math.mio", "--export=add=mio_add", "--target=linux-amd64"],
            success=False, contains="Darwin ARM64")
        run("reject-duplicate-symbol", [*build, HERE / "math.mio", "--export=add=mio_same",
                                        "--export=root=mio_same"], success=False, contains="E_DYLIB")
        initializer = artifacts / "initializer.mio"
        initializer.write_text("waza initial() -> int64 { kaesu 1; }\nkaku value = initial();\nwaza get() -> int64 { kaesu value; }\n")
        run("reject-dylib-top-level", [*build, initializer, "--export=get=mio_get"],
            success=False, contains="module scope supports only scalar literal initialization")
        recursive = artifacts / "recursive.mio"
        recursive.write_text("waza loop(a: int64) -> int64 { kaesu loop(a); }\n")
        run("reject-recursion", [*build, recursive, "--export=loop=mio_loop"], success=False, contains="recursive")
    except (OSError, RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
        report["error"] = str(error)
        print(error, file=sys.stderr)
    finally:
        (output / "report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return int(any(check["status"] != "PASS" for name, check in checks.items()
                   if not (name == "memory-openssl" and check["status"] == "NOT_AVAILABLE"))
               or "error" in report)


if __name__ == "__main__":
    sys.exit(main())
