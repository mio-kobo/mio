#!/usr/bin/env python3
"""Static check of the heap-exclusion classification of the darwin-arm64 thread build (tests/threads/heap_epochs.mio).

The program is cross-built with `mio build --target=darwin-arm64 --emit-symbols`; the image is decoded (Mach-O __text, AArch64 BL) and, per Mio
function, the call sites of the runtime routines `attached_heap_enter` (enter a stopped Epoch) and `attached_roots_poll` (solved Safepoint poll)
are counted. This is a static code-generation check: it runs on any host, needs no macOS, and says nothing about run-time behaviour (that is
tests/threads/heap_epochs.mio executed by darwin-thread-programs on a real Mac). Expectations:
  - every function keeps a Safepoint poll before every opcode (polls + retains > 0; a retained poll is `attached_roots_retain`, which republishes the roots
    and keeps the thread's own epoch instead of releasing it, see the design document, section 8);
  - epoch retention never appears without an epoch to retain: scalar_loop and read_loop have no retain sites, ref_store (an allocation followed by
    stores in the same straight-line run) has at least one;
  - scalar_loop, read_loop (scalar stores, array/field reads) and alloc_only (struct/array literals bound to non-captured locals; allocation lock) have NO
    heap-entering site;
  - ref_store, captured and nullable (reference/array stores, captured cell, nullable boxing) keep at least one.
Exit 0 on PASS, 1 on FAIL, 2 on a build problem.
"""
import argparse
import bisect
import os
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NO_SITES = ("scalar_loop", "read_loop", "alloc_only")
KEEP_SITES = ("ref_store", "captured", "nullable")


def text_section(data):
    count = struct.unpack_from("<I", data, 16)[0]
    at = 32
    for _ in range(count):
        command, size = struct.unpack_from("<II", data, at)
        if command == 0x19:
            for index in range(struct.unpack_from("<I", data, at + 64)[0]):
                base = at + 72 + index * 80
                if data[base:base + 16].rstrip(b"\0") == b"__text":
                    return struct.unpack_from("<I", data, base + 48)[0], struct.unpack_from("<Q", data, base + 40)[0]
        at += size
    raise ValueError("no __text section")


def count_sites(image, symbols):
    """name -> [heap_enter call sites, roots_poll call sites, roots_retain call sites]; runtime routines ('rt:<name>') are ignored."""
    data = Path(image).read_bytes()
    offset, size = text_section(data)
    entries = []
    for line in Path(symbols).read_text().splitlines():
        word, _, name = line.partition(" ")
        if word.isdigit():
            entries.append((int(word), name))
    entries.sort()
    words = [w for w, _ in entries]
    routine = {name[3:]: word for word, name in entries if name.startswith("rt:")}
    heap, poll, retain = routine["attached_heap_enter"], routine["attached_roots_poll"], routine.get("attached_roots_retain", -1)
    result = {}
    for index in range(size // 4):
        word = struct.unpack_from("<I", data, offset + 4 * index)[0]
        if word >> 26 != 0b100101:   # BL
            continue
        delta = word & 0x3ffffff
        if delta >> 25:
            delta -= 1 << 26
        target = index + delta
        if target not in (heap, poll, retain):
            continue
        name = entries[bisect.bisect_right(words, index) - 1][1]
        if name.startswith("rt:"):
            continue
        result.setdefault(name, [0, 0, 0])[0 if target == heap else 1 if target == poll else 2] += 1
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", default=os.environ.get("MIO_CI_MIO"), required=os.environ.get("MIO_CI_MIO") is None)
    parser.add_argument("--bunko-root", default=None)
    parser.add_argument("--keep", default=None, help="directory to keep the image and the symbol map in")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory() as scratch:
        work = Path(args.keep or scratch)
        work.mkdir(parents=True, exist_ok=True)
        image, symbols = work / "heap_epochs.img", work / "heap_epochs.sym"
        command = [args.mio, "build", str(ROOT / "tests/threads/heap_epochs.mio"), "--target=darwin-arm64", "--emit-symbols", str(symbols), "-o", str(image)]
        if args.bunko_root:
            command += ["--bunko-root", args.bunko_root]
        built = subprocess.run(command, cwd=ROOT, capture_output=True, text=True)
        if built.returncode != 0 or not image.is_file() or not symbols.is_file():
            print("BUILD FAILED (exit %d): %s" % (built.returncode, (built.stderr or built.stdout)[-400:]))
            return 2
        sites = count_sites(image, symbols)
    failures = []
    for name in NO_SITES + KEEP_SITES:
        heap, poll, retain = sites.get(name, [0, 0, 0])
        print("%-12s heap_enter sites %3d   safepoint polls %3d   retained polls %3d" % (name, heap, poll, retain))
        if name in NO_SITES and retain != 0:
            failures.append("%s: %d retained polls without an epoch to retain" % (name, retain))
        if name == "ref_store" and retain == 0:
            failures.append("ref_store: no retained poll (epoch retention not applied)")
        if poll + retain == 0:
            failures.append("%s: no Safepoint poll (missing function or polls removed)" % name)
        if name in NO_SITES and heap != 0:
            failures.append("%s: %d heap-entering sites, expected 0" % (name, heap))
        if name in KEEP_SITES and heap == 0:
            failures.append("%s: no heap-entering site, expected at least one (an unproven opcode lost its Epoch)" % name)
    print("FAIL: " + "; ".join(failures) if failures else "PASS")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
