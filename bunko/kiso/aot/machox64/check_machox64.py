#!/usr/bin/env python3
"""Independent check of the macOS x86-64 Mach-O image written by kiso/aot/machox64 (outside testing.expect).

A small Mio program writes one image whose code is `exit(42)` through raw Darwin system calls (mov eax,0x2000001;
mov edi,42; syscall) and one with a zero-fill data segment. This script then parses the files with Python's struct module
and compares every header field, load command, segment and section with values computed here from the Mach-O format, and
cross-checks with Apple's tools when they exist.

Layers (a layer that did not run is never a pass):
  struct parse   always.
  otool/lipo/file  PASS on macOS, "NOT_RUN (reason)" elsewhere: architecture x86_64, load commands and sections.
  execution      only on a native macOS x86-64 host: the image must exit with status 42. On any other host:
                 "execution: NOT_RUN (host is ...)"; Rosetta and emulation are never used for this layer.
With --require-execution the script fails (exit 1) when execution did not run.
Environment: MIO_CI_MIO (compiler or reference `mio` binary), MIO_CI_MODULE_DIR (scratch directory).
--reference-vm runs the writer program with the Go reference `mio run` (hosts without a native Mio compiler).
"""
import argparse
import os
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
TEXT_BASE, CODE_OFFSET, DATA_BASE = 0x400000, 0x1000, 0x10000000
EXIT_42 = bytes([0xB8, 0x01, 0x00, 0x00, 0x02, 0xBF, 0x2A, 0x00, 0x00, 0x00, 0x0F, 0x05])


def source(path_plain, path_data):
    def array(data):
        return "[" + ", ".join(f"uint8({b})" for b in data) + "]"
    return ('yobu "kiso/aot/machox64";\nyobu "kiso/fs";\n'
            f'kaku a = machox64.executable(machox64.ImageSpec {{ code: {array(EXIT_42)}, entry_offset: 0, bss_size: 0 }});\n'
            f'kaku b = machox64.executable(machox64.ImageSpec {{ code: {array(EXIT_42)}, entry_offset: 5, bss_size: 4104 }});\n'
            f'kaku wa = fs.write_all("{path_plain}", a.bytes);\nkaku wb = fs.write_all("{path_data}", b.bytes);\n'
            'chant(a.error == "" && b.error == "" && wa.error == mu && wb.error == mu);\n')


def expect(condition, message):
    if not condition:
        raise AssertionError(message)


def cstr(raw):
    return raw.split(b"\0", 1)[0].decode()


def check_image(data, entry_offset, bss_size):
    text_end = CODE_OFFSET + len(EXIT_42)
    file_size = (text_end + 0xFFF) // 0x1000 * 0x1000
    magic, cputype, cpusubtype, filetype, ncmds, sizeofcmds, flags, reserved = struct.unpack_from("<IiiIIIII", data)
    expect(magic == 0xFEEDFACF, "MH_MAGIC_64")
    expect(cputype == 0x01000007 and cpusubtype == 3, "CPU_TYPE_X86_64 (0x01000007), CPU_SUBTYPE_X86_64_ALL (3)")
    expect(filetype == 2 and flags == 0x85 and reserved == 0, "MH_EXECUTE with MH_NOUNDEFS|MH_DYLDLINK|MH_TWOLEVEL and no MH_PIE")
    has_data = bss_size > 0
    expect(ncmds == (9 if has_data else 8), "load command count")
    off, kinds, sections, entry = 32, [], {}, None
    segments = []
    for _ in range(ncmds):
        cmd, size = struct.unpack_from("<II", data, off)
        kinds.append(cmd)
        if cmd == 0x19:
            name = cstr(data[off + 8:off + 24])
            vmaddr, vmsize, fileoff, filesize, maxprot, initprot, nsects, _flags = struct.unpack_from("<QQQQiiII", data, off + 24)
            segments.append((name, vmaddr, vmsize, fileoff, filesize, maxprot, initprot, nsects))
            for i in range(nsects):
                so = off + 72 + 80 * i
                sect = cstr(data[so:so + 16]), cstr(data[so + 16:so + 32])
                addr, ssize, soffset, align, _r, _n, sflags = struct.unpack_from("<QQIIIII", data, so + 32)
                sections[sect] = (addr, ssize, soffset, align, sflags)
        elif cmd == 0x80000028:
            entry = struct.unpack_from("<Q", data, off + 8)[0]
        elif cmd == 0x32:
            platform_id, minos, sdk = struct.unpack_from("<III", data, off + 8)
            expect(platform_id == 1 and minos == 0xB0000 and sdk == 0xB0000, "LC_BUILD_VERSION macOS 11.0")
        elif cmd == 0xE:
            expect(data[off + 12:off + 25] == b"/usr/lib/dyld", "dylinker path")
        off += size
    expect(off == 32 + sizeofcmds, "sizeofcmds")
    expect(kinds.count(0x19) == (4 if has_data else 3) and 2 in kinds and 0xB in kinds and 0xE in kinds and 0x32 in kinds, "required load commands")
    by_name = {seg[0]: seg for seg in segments}
    expect(by_name["__PAGEZERO"][1:3] == (0, TEXT_BASE) and by_name["__PAGEZERO"][5:7] == (0, 0), "__PAGEZERO covers 0..0x400000 with no access")
    expect(by_name["__TEXT"][1:7] == (TEXT_BASE, file_size, 0, file_size, 5, 5), "__TEXT r-x maps the file from offset 0")
    expect(sections[("__text", "__TEXT")] == (TEXT_BASE + CODE_OFFSET, len(EXIT_42), CODE_OFFSET, 4, 0x80000400), "__text section")
    if has_data:
        size = (bss_size + 0xFFF) // 0x1000 * 0x1000
        expect(by_name["__DATA"][1:7] == (DATA_BASE, size, 0, 0, 3, 3), "__DATA rw- zero-fill at 0x10000000")
        expect(sections[("__bss", "__DATA")] == (DATA_BASE, bss_size, 0, 3, 1), "__bss is S_ZEROFILL")
        expect(by_name["__LINKEDIT"][1] == DATA_BASE + size, "__LINKEDIT after __DATA")
    expect(by_name["__LINKEDIT"][3] == file_size and by_name["__LINKEDIT"][4] == 0, "__LINKEDIT at end of file, empty")
    expect(entry == CODE_OFFSET + entry_offset, "LC_MAIN entryoff")
    expect(len(data) == file_size and data[CODE_OFFSET:text_end] == EXIT_42, "code bytes and file size")


def tool(name):
    return shutil.which(name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--reference-vm", action="store_true")
    parser.add_argument("--require-execution", action="store_true")
    args = parser.parse_args()
    mio = os.environ["MIO_CI_MIO"]
    work = Path(os.environ["MIO_CI_MODULE_DIR"])
    work.mkdir(parents=True, exist_ok=True)
    plain, data = work / "machox64-plain.bin", work / "machox64-data.bin"
    program = work / "machox64-image.mio"
    program.write_text(source(plain, data), encoding="utf-8")
    if args.reference_vm:
        command, cwd = [mio, "run", str(program)], ROOT
    else:
        native = work / "machox64-image.native"
        build = subprocess.run([mio, "build", str(program), "--bunko-root", str(ROOT / "bunko"), "-o", str(native)], capture_output=True, text=True, timeout=600)
        if build.returncode != 0:
            raise RuntimeError(f"building the Mio image writer failed: exit={build.returncode} {build.stderr!r}")
        command, cwd = [str(native)], None
    run = subprocess.run(command, capture_output=True, text=True, timeout=600, cwd=cwd)
    if run.returncode != 0 or run.stdout.strip() != "hai":
        raise RuntimeError(f"the Mio image writer failed: exit={run.returncode} stdout={run.stdout!r} stderr={run.stderr!r}")
    check_image(plain.read_bytes(), 0, 0)
    check_image(data.read_bytes(), 5, 4104)
    print("struct parse: PASS (header, every load command, segments, sections and code compared for two images)")

    if platform.system() == "Darwin" and tool("otool") and tool("lipo"):
        archs = subprocess.run(["lipo", "-archs", str(data)], capture_output=True, text=True).stdout.strip()
        expect(archs == "x86_64", f"lipo -archs: {archs!r}")
        out = subprocess.run(["otool", "-l", str(data)], capture_output=True, text=True, check=True).stdout
        for needle in ("segname __PAGEZERO", "segname __TEXT", "segname __DATA", "sectname __bss", "cmd LC_MAIN", "cmd LC_BUILD_VERSION", "minos 11.0", "platform 1"):
            expect(needle in out, "otool -l lacks " + repr(needle))
        print("otool/lipo: PASS (arch x86_64, segments, LC_MAIN, minos 11.0)")
    else:
        print(f"otool/lipo: NOT_RUN (needs macOS with Xcode command line tools; host is {platform.system()})")

    host = f"{platform.system()}/{platform.machine()}"
    executed = False
    if platform.system() == "Darwin" and platform.machine() == "x86_64":
        for image in (plain, data):
            exe = work / ("run-" + image.name)
            shutil.copyfile(image, exe)
            exe.chmod(0o755)
            proc = subprocess.run([str(exe)], capture_output=True, timeout=60)
            expect(proc.returncode == 42, f"{image.name}: exit {proc.returncode}, expected 42")
        print(f"execution: PASS (native {host}, both images exit 42)")
        executed = True
    else:
        print(f"execution: NOT_RUN (host is {host}, not macOS x86_64; no Rosetta or emulation is used)")
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
