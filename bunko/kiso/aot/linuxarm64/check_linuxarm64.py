#!/usr/bin/env python3
"""Independent check of the Linux AArch64 image written by kiso/aot/linuxarm64 (outside testing.expect).

A small Mio program builds the hello image with linuxarm64.hello_image and writes it with kiso/fs. This script then
parses the file with Python's struct module, compares the ELF header, program headers and the exact instruction words
with values computed here from the ELF and AArch64 specifications, cross-checks with GNU readelf/objdump when they know
AArch64, and executes the image.

Layers and how they are reported (a layer that did not run is never a pass):
  struct parse   always.
  readelf        PASS, or "NOT_RUN (reason)".
  objdump        PASS when an AArch64-capable objdump is available, else "NOT_RUN (reason)".
  execution      only on a native Linux aarch64 host: the image must print the message and exit with status 7.
                 On any other host: "execution: NOT_RUN (host is ...)". Emulation is never used.
With --require-execution the script fails (exit 1) when execution did not run, so a native ARM64 CI job cannot pass by
skipping it. Exit status: 0 all executed checks passed, 1 a check failed.

Environment: MIO_CI_MIO (compiler or reference `mio` binary), MIO_CI_MODULE_DIR (scratch directory).
--reference-vm builds nothing: the Go reference `mio run` executes the writer program (used on hosts that have no
native Mio compiler yet, such as the ARM64 runner; the image written is still executed natively).
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
MESSAGE = "hello from mio linux-arm64\n"
STATUS = 7
IMAGE_BASE, CODE_OFFSET = 0x400000, 0x1000


def movz(rd, imm):
    return 0xD2800000 | (imm << 5) | rd


def expected_code():
    words = [movz(0, 1), 0x10000000 | ((28 >> 2) << 5) | 1, movz(2, len(MESSAGE)), movz(8, 64), 0xD4000001,
             movz(0, STATUS), movz(8, 94), 0xD4000001]
    code = b"".join(struct.pack("<I", w) for w in words) + MESSAGE.encode()
    return code + b"\0" * (-len(code) % 4)


def expect(condition, message):
    if not condition:
        raise AssertionError(message)


def check_image(data):
    code = expected_code()
    text_end = CODE_OFFSET + len(code)
    ident, etype, machine, version, entry, phoff, shoff, flags, ehsize, phentsize, phnum, shentsize, shnum, shstrndx = \
        struct.unpack_from("<16sHHIQQQIHHHHHH", data)
    expect(ident == b"\x7fELF\x02\x01\x01" + b"\0" * 9, "e_ident: ELF64, little endian, version 1, System V ABI")
    expect((etype, machine, version) == (2, 183, 1), "ET_EXEC, EM_AARCH64 (183), EV_CURRENT")
    expect(entry == IMAGE_BASE + CODE_OFFSET, "e_entry")
    expect((phoff, flags, ehsize, phentsize, phnum) == (64, 0, 64, 56, 2), "header sizes and counts")
    phdrs = [struct.unpack_from("<IIQQQQQQ", data, 64 + 56 * i) for i in range(phnum)]
    expect(phdrs[0] == (1, 5, 0, IMAGE_BASE, IMAGE_BASE, text_end, text_end, 0x10000), "PT_LOAD R+X, 64 KiB aligned")
    expect(phdrs[1] == (0x6474E551, 6, 0, 0, 0, 0, 0, 16), "PT_GNU_STACK R+W without X")
    expect(data[CODE_OFFSET:text_end] == code, "code: exact AArch64 instruction words and message bytes")


def tool(*names):
    for name in names:
        path = shutil.which(name)
        if path:
            return path
    return None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--reference-vm", action="store_true")
    parser.add_argument("--require-execution", action="store_true")
    args = parser.parse_args()
    mio = os.environ["MIO_CI_MIO"]
    work = Path(os.environ["MIO_CI_MODULE_DIR"])
    work.mkdir(parents=True, exist_ok=True)
    image = work / "linuxarm64-hello.bin"
    program = work / "linuxarm64-image.mio"
    program.write_text(
        'yobu "kiso/aot/linuxarm64";\nyobu "kiso/fs";\n'
        f'kaku r = linuxarm64.hello_image("{MESSAGE[:-1]}\\n", {STATUS});\n'
        f'moshi (r.error != "") {{ chant("error"); }}\n'
        f'kaku w = fs.write_all("{image}", r.bytes);\n'
        'chant(w.error == mu);\n', encoding="utf-8")
    if args.reference_vm:
        command = [mio, "run", str(program)]  # the reference VM locates Bunko from the working directory (the repository root)
    else:
        native = work / "linuxarm64-image.native"
        build = subprocess.run([mio, "build", str(program), "--bunko-root", str(ROOT / "bunko"), "-o", str(native)],
                               capture_output=True, text=True, timeout=600)
        (work / "linuxarm64-image.build.log").write_text(build.stdout + build.stderr)
        if build.returncode != 0:
            raise RuntimeError(f"building the Mio image writer failed: exit={build.returncode} {build.stderr!r}")
        command = [str(native)]
    run = subprocess.run(command, capture_output=True, text=True, timeout=600, cwd=ROOT)
    if run.returncode != 0 or run.stdout.strip() != "hai":
        raise RuntimeError(f"the Mio image writer failed: exit={run.returncode} stdout={run.stdout!r} stderr={run.stderr!r}")
    check_image(image.read_bytes())
    print("struct parse: PASS (ELF header, program headers and every instruction word compared)")

    readelf = tool("readelf", "greadelf")
    if readelf is None:
        print("readelf: NOT_RUN (no GNU readelf on PATH)")
    else:
        out = subprocess.run([readelf, "-h", "-l", "-W", str(image)], capture_output=True, text=True, check=True).stdout
        if "AArch64" not in out:
            print("readelf: NOT_RUN (this readelf does not know AArch64)")
        else:
            for needle in ("EXEC (Executable file)", "Entry point address:               0x401000", "GNU_STACK"):
                expect(needle in out, "readelf output lacks " + repr(needle))
            print(f"readelf: PASS ({readelf})")

    objdump = tool("aarch64-linux-gnu-objdump", "objdump")
    dis = None
    if objdump is not None:
        dis = subprocess.run([objdump, "-D", "-b", "binary", "-m", "aarch64", "--start-address=0x1000",
                              "--stop-address=0x1020", str(image)], capture_output=True, text=True)
    if dis is None or dis.returncode != 0 or "svc" not in dis.stdout:
        print("objdump: NOT_RUN (no AArch64-capable objdump)")
    else:
        for needle in ("mov\tx0, #0x1", "adr\tx1,", "mov\tx8, #0x40", "svc\t#0x0", f"mov\tx0, #0x{STATUS:x}", "mov\tx8, #0x5e"):
            expect(needle in dis.stdout, "objdump output lacks " + repr(needle))
        print(f"objdump: PASS ({objdump})")

    host = f"{platform.system()}/{platform.machine()}"
    executed = False
    if platform.system() == "Linux" and platform.machine() in ("aarch64", "arm64"):
        exe = work / "linuxarm64-hello"
        shutil.copyfile(image, exe)
        exe.chmod(0o755)
        proc = subprocess.run([str(exe)], capture_output=True, timeout=60)
        expect(proc.returncode == STATUS, f"exit status {proc.returncode}, expected {STATUS}")
        expect(proc.stdout == MESSAGE.encode(), f"stdout {proc.stdout!r}")
        expect(proc.stderr == b"", "stderr must be empty")
        print(f"execution: PASS (native {host}, exit {STATUS}, message printed)")
        executed = True
    else:
        print(f"execution: NOT_RUN (host is {host}, not Linux aarch64; no emulation is used)")
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
