#!/usr/bin/env python3
"""Build shared C fixtures locally; never download dependencies."""
import argparse
import json
import platform
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
MODULES = ("math", "string", "buffer", "state", "version")


def build(output):
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    system = platform.system()
    if system == "Windows" and platform.machine().lower() in ("amd64", "x86_64"):
        sys.path.insert(0, str(ROOT / "tests/ffi/windows-amd64"))
        from run import c_tools
        compiler, _, env = c_tools()
        if not compiler:
            raise RuntimeError("MSVC/clang-cl and Windows SDK are required")
        sources = {f"mio_ffi_{m}": ROOT / f"native-tests/{m}/{m}.c" for m in MODULES}
        sources["mio_native_ffi"] = ROOT / "native/ffi/windows.c"
        for name, source in sources.items():
            command = [compiler, "/nologo", "/utf-8", "/std:c11", "/LD", "/O2", "/MT", "/W4", "/WX",
                       "/Fo" + str(output / (name + ".obj")), str(source), "/link",
                       "/OUT:" + str(output / (name + ".dll")),
                       "/IMPLIB:" + str(output / (name + ".lib"))]
            execute(command, output, name, env)
    elif system == "Darwin":
        compiler = shutil.which("clang")
        if not compiler:
            raise RuntimeError("Xcode Command Line Tools are required")
        for m in MODULES:
            name = f"libmio_ffi_{m}"
            execute([compiler, "-std=c11", "-dynamiclib", "-O2", "-Wall", "-Wextra", "-Werror",
                     str(ROOT / f"native-tests/{m}/{m}.c"), "-o", str(output / (name + ".dylib"))], output, name)
    else:
        raise RuntimeError("requires Windows AMD64 or macOS; Linux dynamic loading is unchanged")
    return output


def execute(command, output, name, env=None):
    proc = subprocess.run(command, cwd=output, env=env, capture_output=True, timeout=120)
    (output / (name + ".build.log")).write_bytes(proc.stdout + proc.stderr)
    (output / (name + ".command.json")).write_text(json.dumps(command, indent=2), encoding="utf-8")
    if proc.returncode:
        raise RuntimeError(f"{name} build failed: {(proc.stdout + proc.stderr).decode(errors='replace')}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    build(args.output)
