#!/usr/bin/env python3
"""Independent check of a Windows ARM64 PE image produced by the pure-Mio pipeline (outside testing.expect).

A hello program is compiled to Mochi by the Go reference frontend and lowered to a PE32+ (ARM64) image by the pure-Mio
lowering (selfhost/native-aot-windows-arm64) running in the Go reference VM (go-hosted Stage A). This script then parses
the PE with Python's struct module and compares the machine type, optional header, section table, data directories and
the import table with values derived here from the PE format, and finally runs the image.

Layers (a layer that did not run is never a pass):
  struct parse  always: IMAGE_FILE_MACHINE_ARM64 (0xAA64), PE32+, console subsystem, entry inside .text, sections and
                imports (the Win32 functions the adapter needs).
  execution     only on a native Windows ARM64 host: stdout must be the program's output and the exit status 0.
                On any other host: "execution: NOT_RUN (host is ...)"; emulation is never used.
With --require-execution the script fails (exit 1) when execution did not run.
Arguments: --refvm <Go reference VM host binary> (go build ./tests/windows-amd64/refvm), --work <scratch directory>.
"""
import argparse
from pathlib import Path
import platform
import struct
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
PROGRAM = 'chant("hello from windows-arm64");\nchant(40 + 2);\n'
EXPECTED_STDOUT = "hello from windows-arm64\n42\n"
EXPECTED_IMPORTS = {"ExitProcess", "GetStdHandle", "WriteFile", "VirtualAlloc", "VirtualFree"}


def expect(condition, message):
    if not condition:
        raise AssertionError(message)


def build(refvm, work):
    work.mkdir(parents=True, exist_ok=True)
    source, mochi, image = work / "hello.mio", work / "hello.mochi", work / "hello.exe"
    source.write_text(PROGRAM, encoding="utf-8")
    run = subprocess.run([str(refvm), str(ROOT / "bunko"), "mochi", str(source), str(mochi)], capture_output=True, text=True, timeout=600)
    expect(run.returncode == 0, f"Go frontend failed: {run.stderr!r}")
    (work / "input.config").write_text(str(mochi))
    (work / "output.config").write_text(str(image))
    run = subprocess.run([str(refvm), str(ROOT / "bunko"), "run", str(ROOT / "selfhost/native-aot-windows-arm64/main.mio")],
                         cwd=work, capture_output=True, text=True, timeout=1800)
    expect(run.returncode == 0 and "native-aot-windows-arm64: ok" in run.stdout and image.is_file(), f"lowering failed: exit={run.returncode} {run.stderr!r}")
    return image


def check_pe(data):
    expect(data[:2] == b"MZ", "MZ")
    pe = struct.unpack_from("<I", data, 60)[0]
    expect(data[pe:pe + 4] == b"PE\0\0", "PE signature")
    machine, nsec, _stamp, _symtab, _nsyms, optsize, characteristics = struct.unpack_from("<HHIIIHH", data, pe + 4)
    expect(machine == 0xAA64, f"IMAGE_FILE_MACHINE_ARM64 expected, got {machine:#x}")
    expect(characteristics & 0x2 and characteristics & 0x20, "EXECUTABLE_IMAGE | LARGE_ADDRESS_AWARE")
    opt = pe + 24
    expect(struct.unpack_from("<H", data, opt)[0] == 0x20B, "PE32+ optional header")
    entry_rva = struct.unpack_from("<I", data, opt + 16)[0]
    image_base, section_alignment, file_alignment = struct.unpack_from("<QII", data, opt + 24)
    subsystem = struct.unpack_from("<H", data, opt + 68)[0]
    expect(image_base == 0x140000000 and subsystem == 3, "image base 0x140000000, console subsystem")
    expect(section_alignment == 0x1000 and file_alignment == 0x200, "alignments")
    sections = []
    for i in range(nsec):
        name, vsize, rva, rsize, roff = struct.unpack_from("<8sIIII", data, opt + optsize + 40 * i)
        sections.append((name.rstrip(b"\0").decode(), rva, vsize, roff, rsize))
    by_name = {s[0]: s for s in sections}
    expect(".text" in by_name and ".idata" in by_name, "sections .text and .idata")
    text = by_name[".text"]
    expect(text[1] <= entry_rva < text[1] + text[2], "entry point inside .text")

    def offset(rva):
        for _n, srva, vsize, roff, rsize in sections:
            if srva <= rva < srva + max(vsize, rsize):
                return roff + rva - srva
        raise AssertionError(f"RVA {rva:#x} is not in any section")
    import_rva = struct.unpack_from("<I", data, opt + 112 + 8)[0]
    iat_rva, iat_size = struct.unpack_from("<II", data, opt + 112 + 12 * 8)
    expect(import_rva == by_name[".idata"][1] and iat_size > 0, "import and IAT directories")
    names, at = set(), offset(import_rva)
    while True:
        lookup, _t, _c, name_rva, first_thunk = struct.unpack_from("<IIIII", data, at)
        if name_rva == 0:
            break
        expect(data[offset(name_rva):].split(b"\0")[0] in (b"kernel32.dll", b"ws2_32.dll"), "imports come from kernel32.dll / ws2_32.dll")
        entry = 0
        while True:
            value = struct.unpack_from("<Q", data, offset(lookup) + 8 * entry)[0]
            if value == 0:
                break
            names.add(data[offset(value & 0x7FFFFFFF) + 2:].split(b"\0")[0].decode())
            entry += 1
        at += 20
    expect(EXPECTED_IMPORTS <= names, f"imports {sorted(names)} lack {sorted(EXPECTED_IMPORTS - names)}")
    return sorted(names)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--refvm", required=True)
    parser.add_argument("--work", required=True)
    parser.add_argument("--require-execution", action="store_true")
    args = parser.parse_args()
    image = build(args.refvm, Path(args.work))
    names = check_pe(image.read_bytes())
    print(f"struct parse: PASS (ARM64 PE32+, sections and {len(names)} imports compared: {', '.join(names)})")
    host = f"{platform.system()}/{platform.machine()}"
    executed = False
    if platform.system() == "Windows" and platform.machine().lower() in ("arm64", "aarch64"):
        proc = subprocess.run([str(image)], capture_output=True, timeout=120)
        expect(proc.returncode == 0, f"exit status {proc.returncode}, stderr {proc.stderr!r}")
        expect(proc.stdout.decode().replace("\r\n", "\n") == EXPECTED_STDOUT, f"stdout {proc.stdout!r}")
        print(f"execution: PASS (native {host}, output and exit status 0)")
        executed = True
    else:
        print(f"execution: NOT_RUN (host is {host}, not Windows ARM64; no emulation is used)")
    if args.require_execution and not executed:
        print("FAIL: execution is required but did not run", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
