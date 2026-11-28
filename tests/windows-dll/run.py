#!/usr/bin/env python3
"""Required Windows AMD64 DLL integration checks; failure is never a skip."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
EXPECTED = {
    "math": ["mio_add", "mio_sum6"], "utility": ["mio_abs"],
    "bytes": ["mio_low_byte"], "encoding": ["mio_decimal"],
    "standard": ["mio_standard", "mio_zero"],
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def verify(path, expected):
    """Independent PE oracle, including sorted exports, imports and DIR64 fixups."""
    data = path.read_bytes()
    def u16(at): return struct.unpack_from("<H", data, at)[0]
    def u32(at): return struct.unpack_from("<I", data, at)[0]
    def u64(at): return struct.unpack_from("<Q", data, at)[0]
    require(data[:2] == b"MZ", "DOS signature")
    pe = u32(60)
    require(data[pe:pe+4] == b"PE\0\0", "PE signature")
    require(u16(pe+4) == 0x8664 and u16(pe+20) == 240, "AMD64 optional-header size")
    require(u16(pe+22) & 0x2000 and not u16(pe+22) & 1, "DLL, relocations not stripped")
    opt = pe+24
    require(u16(opt) == 0x20B and u32(opt+16) == 0, "PE32+, no DllMain")
    require(u32(opt+32) == 4096 and u32(opt+36) == 512, "alignments")
    require(u64(opt+24) == 0x140000000, "image base")
    require(u32(opt+108) == 16, "directory count")
    sections = []
    for i in range(u16(pe+6)):
        at = opt+240+i*40
        size, rva, raw_size, offset = struct.unpack_from("<IIII", data, at+8)
        flags = u32(at+36)
        require(rva % 4096 == 0 and raw_size % 512 == 0 and offset+raw_size <= len(data), "section bounds")
        require(not (flags & 0x20000000 and flags & 0x80000000), "W^X sections")
        sections.append((rva, size, offset, raw_size, flags))
    def offset(rva, size=1):
        matches = [s for s in sections if s[0] <= rva and rva+size <= s[0]+min(s[1], s[3])]
        require(len(matches) == 1, "RVA maps to one initialized section")
        return matches[0][2]+rva-matches[0][0]
    def cstr(rva):
        at = offset(rva)
        end = data.index(0, at)
        offset(rva, end-at+1)
        return data[at:end].decode("ascii")
    def directory(index): return struct.unpack_from("<II", data, opt+112+index*8)
    export_rva, export_size = directory(0)
    at = offset(export_rva, export_size)
    require(cstr(u32(at+12)) == path.name, "DLL export name")
    require(u32(at+16) == 1 and u32(at+20) == len(expected) and u32(at+24) == len(expected), "export counts and ordinal base")
    eat, names, ordinals = u32(at+28), u32(at+32), u32(at+36)
    actual = []
    for i in range(len(expected)):
        actual.append(cstr(u32(offset(names+4*i, 4))))
        ordinal = u16(offset(ordinals+2*i, 2))
        require(ordinal < len(expected), "ordinal range")
        target = u32(offset(eat+4*ordinal, 4))
        require(not export_rva <= target < export_rva+export_size, "no accidental forwarder")
        require(any(s[0] <= target < s[0]+s[1] and s[4] & 0x20000000 for s in sections), "export targets executable code")
    require(actual == sorted(expected), "exact sorted undecorated exports")
    imports, size = directory(1)
    at = offset(imports, size)
    require(cstr(u32(at+12)).upper() == "KERNEL32.DLL", "runtime imports")
    thunk = u32(at)
    actual_imports = []
    while u64(offset(thunk, 8)):
        actual_imports.append(cstr(u64(offset(thunk, 8))+2))
        thunk += 8
    require(actual_imports == ["GetStdHandle", "WriteFile", "ExitProcess"], "exact import dependencies")
    reloc, size = directory(5)
    at, end, fixups = offset(reloc, size), offset(reloc, size)+size, 0
    while at < end:
        page, block = u32(at), u32(at+4)
        require(page % 4096 == 0 and block >= 8 and block % 4 == 0 and at+block <= end, "relocation block bounds")
        for entry in range(at+8, at+block, 2):
            kind, within = u16(entry) >> 12, u16(entry) & 4095
            require(kind in (0, 10), "DIR64 or ABSOLUTE padding")
            if kind == 10:
                offset(page+within, 8)
                fixups += 1
        at += block
    require(at == end and fixups > 0, "real base relocations")
    pdata, size = directory(3)
    require(size > 0 and size % 12 == 0, "exception directory")
    previous = 0
    for at in range(offset(pdata, size), offset(pdata, size)+size, 12):
        begin, end, unwind = u32(at), u32(at+4), u32(at+8)
        require(begin >= previous and end > begin, "sorted non-overlapping unwind ranges")
        offset(begin, end-begin)
        header = offset(unwind, 4)
        require(data[header] == 1, "version 1 unwind without handlers")
        offset(unwind, 4+((data[header+2]+1)//2)*4)
        previous = end
    return {"exports": actual, "relocations": fixups, "sha256": hashlib.sha256(data).hexdigest()}


def msvc_environment():
    if shutil.which("cl") and shutil.which("dumpbin"):
        return dict(os.environ)
    vswhere = Path(os.environ.get("ProgramFiles(x86)", r"C:\Program Files (x86)")) / "Microsoft Visual Studio/Installer/vswhere.exe"
    installation = subprocess.check_output([str(vswhere), "-latest", "-products", "*", "-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath"], text=True).strip()
    require(installation, "MSVC x64 tools required")
    vcvars = Path(installation) / "VC/Auxiliary/Build/vcvars64.bat"
    result = subprocess.run(f'"{vcvars}" >nul && set', shell=True, capture_output=True, text=True, check=True)
    env = {key.upper(): value for key, value in os.environ.items()}
    for line in result.stdout.splitlines():
        if "=" in line and not line.startswith("="):
            key, value = line.split("=", 1)
            env[key.upper()] = value
    return env


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=ROOT / "build/windows-dll")
    options = parser.parse_args()
    output, mio = options.output.resolve(), options.mio.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = {"status": "NOT_RUN", "commands": [], "dlls": {}, "windows_execution": "NOT_RUN"}
    report_path = output / "report.json"
    report_path.write_text(json.dumps(report, indent=2)+"\n")
    def run(label, command, cwd=ROOT, env=None, success=True):
        command = list(command)
        if env is not None:
            command[0] = shutil.which(str(command[0]), path=env.get("PATH", env.get("Path"))) or command[0]
        result = subprocess.run(list(map(str, command)), cwd=cwd, env=env, capture_output=True, timeout=600)
        (output / (label+".stdout.log")).write_bytes(result.stdout)
        (output / (label+".stderr.log")).write_bytes(result.stderr)
        report["commands"].append({"label": label, "command": list(map(str, command)), "exit_code": result.returncode})
        require((result.returncode == 0) == success, f"{label}: exit {result.returncode}: {result.stdout.decode(errors='replace')} {result.stderr.decode(errors='replace')}")
        return result
    try:
        require(platform.system() == "Windows" and platform.machine().lower() in ("amd64", "x86_64"), "real Windows AMD64 execution required")
        env = msvc_environment()
        report["compiler_sha256"] = hashlib.sha256(mio.read_bytes()).hexdigest()
        for module, exports in EXPECTED.items():
            dll = output / ("mio_"+module+".dll")
            command = [mio, "build", ROOT / "tests/windows-dll/fixtures" / module, "--target", "windows-amd64", "--emit=dll", "--bunko-root", ROOT / "bunko", "-o", dll]
            run(module, command)
            report["dlls"][module] = verify(dll, exports)
            run(module+"-dumpbin", ["dumpbin", "/headers", "/exports", "/imports", "/relocations", "/unwindinfo", dll], env=env)
            before = dll.read_bytes()
            run(module+"-determinism", command)
            require(before == dll.read_bytes(), module+" deterministic image")
        # Oracle sensitivity: broken signatures, flags, export directory and relocation data must fail.
        original = (output / "mio_math.dll").read_bytes()
        pe = struct.unpack_from("<I", original, 60)[0]
        for index, at in enumerate((0, pe+4, pe+22, pe+24, pe+24+112, pe+24+112+5*8)):
            broken = bytearray(original)
            broken[at] ^= 0xFF
            path = output / "mio_math.dll"
            path.write_bytes(broken)
            try:
                verify(path, EXPECTED["math"])
            except (ValueError, struct.error, IndexError):
                pass
            else:
                raise ValueError("oracle accepted corruption "+str(index))
            finally:
                path.write_bytes(original)
        host = output / "host.exe"
        run("c-host-build", ["cl", "/nologo", "/W4", "/WX", "/O2", "/std:c11", ROOT / "tests/windows-dll/host.c", "/Fo"+str(output / "host.obj"), "/Fe"+str(host)], cwd=output, env=env)
        run("c-host-execute", [host], cwd=output)
        report["windows_execution"] = "PASS"
        negative = {
            "float": "koukai waza bad(a: float64) -> float64 { kaesu a; }",
            "string": "koukai waza bad(a: string) -> string { kaesu a; }",
            "array": "koukai waza bad(a: []int64) -> int64 { kaesu len(a); }",
            "unit": "koukai waza bad() -> unit {}",
            "private": "waza bad(a: int64) -> int64 { kaesu a; }",
            "inferred": "koukai waza bad(a) -> int64 { kaesu a; }",
            "initializer": "chant(42); koukai waza bad() -> int64 { kaesu 42; }",
            "global": "kaku value = 1; koukai waza bad() -> int64 { kaesu value; }",
            "duplicate": "koukai waza bad() -> int64 { kaesu 1; } koukai waza bad() -> int64 { kaesu 2; }",
        }
        for name, source in negative.items():
            path = output / ("negative-"+name+".mio")
            path.write_text(source+"\n", encoding="utf-8")
            target = output / ("negative-"+name+".dll")
            target.unlink(missing_ok=True)
            result = run("negative-"+name, [mio, "build", path, "--target", "windows-amd64", "--emit=dll", "--bunko-root", ROOT / "bunko", "-o", target], success=False)
            diagnostic = result.stdout+result.stderr
            require((b"error[" in diagnostic) if name in ("inferred", "duplicate") else (b"E_DLL_" in diagnostic), "explicit source or DLL diagnostic")
            require(not target.exists(), "failed build produced no DLL")
        result = run("negative-target", [mio, "build", ROOT / "tests/windows-dll/fixtures/math", "--target", "linux-amd64", "--emit=dll", "--lang", "ja-JP"], success=False)
        diagnostic = (result.stdout+result.stderr).decode("utf-8")
        require("E_DLL_OPTIONS" in diagnostic and "ネイティブ" in diagnostic, "localized DLL option diagnostic")
        exe = output / "regression.exe"
        run("exe-build", [mio, "build", ROOT / "tests/windows-dll/exe.mio", "--target", "windows-amd64", "--bunko-root", ROOT / "bunko", "-o", exe])
        result = run("exe-execute", [exe])
        require(result.stdout.strip() == b"42", "existing Windows EXE result")
        report["status"] = "PASS"
    except Exception as error:
        report["status"], report["error"] = "FAIL", str(error)
        raise
    finally:
        report_path.write_text(json.dumps(report, indent=2)+"\n", encoding="utf-8")
    print("PASS: five Mio DLLs, independent PE verification, native C host and EXE regression")


if __name__ == "__main__":
    main()
