#!/usr/bin/env python3
"""Decode every expected byte sequence of x86_64_test.mio with GNU objdump (Intel syntax).

Each `testing.expect(same(<bytes>, [n, n, ...]), "message")` assertion is written to a raw
.bin file and disassembled with `objdump -D -b binary -m i386:x86-64 -M intel`, so a reviewer can
see that the bytes decode to the instruction named in the message. The script exits non-zero if
any sequence does not decode cleanly ("(bad)", a stray ".byte", or an instruction cut off).

Usage: python3 check_with_objdump.py [path/to/x86_64_test.mio]
Requires: python3 and binutils (objdump). Nothing is installed or modified.
"""
import os
import re
import subprocess
import sys
import tempfile

default = os.path.join(os.path.dirname(os.path.abspath(__file__)), "x86_64_test.mio")
path = sys.argv[1] if len(sys.argv) > 1 else default
text = open(path, encoding="utf-8").read()
# `testing.expect(same(<expr>, [bytes]), "msg")` and `testing.expect(<cond> && same(<expr>, [bytes]), "msg")`
pattern = re.compile(r'testing\.expect\((?:[^;]*?&&\s*)?same\([^,]+,\s*\[([0-9,\s]+)\]\)\s*,\s*"([^"]+)"\)')
cases = [(m.group(2), bytes(int(x) for x in m.group(1).split(","))) for m in pattern.finditer(text)]
if not cases:
    sys.exit("no byte-sequence assertions found in " + path)

failures = 0
for message, data in cases:
    with tempfile.NamedTemporaryFile(suffix=".bin", delete=False) as f:
        f.write(data)
        name = f.name
    try:
        out = subprocess.run(
            ["objdump", "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", name],
            capture_output=True, text=True, check=True).stdout
    finally:
        os.unlink(name)
    decoded, consumed = [], 0
    for line in out.splitlines():
        m = re.match(r"\s*[0-9a-f]+:\t([0-9a-f ]+?)\s*\t(.*)", line)
        if m:  # an instruction line (hex bytes, then the mnemonic)
            consumed += len(m.group(1).split())
            decoded.append(" ".join(m.group(2).split()))
        else:
            m = re.match(r"\s*[0-9a-f]+:\t([0-9a-f ]+)$", line)
            if m:  # continuation of a long instruction
                consumed += len(m.group(1).split())
    # "(bad)", a stray ".byte", or a lone REX prefix means the bytes are not a complete instruction.
    broken = any("(bad)" in d or d.startswith(".byte") or re.fullmatch(r"rex(\.\w+)?", d) for d in decoded)
    ok = consumed == len(data) and bool(decoded) and not broken
    failures += 0 if ok else 1
    print(f"{'ok  ' if ok else 'FAIL'} {message:42s} -> {' ; '.join(decoded)}")
print(f"\n{len(cases)} byte sequences, {failures} failed to decode")
sys.exit(1 if failures else 0)
