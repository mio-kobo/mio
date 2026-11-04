#!/usr/bin/env python3
"""Cross-check the byte sequences of x86_64_test.mio with GNU objdump (Intel syntax).

Each `testing.expect(same(<bytes>, [n, n, ...]), "message")` assertion is written to a raw .bin
file and disassembled with `objdump -D -b binary -m i386:x86-64 -M intel`.

What it checks, per assertion:
  1. the bytes decode completely as instructions ("(bad)", a stray ".byte", a lone REX prefix, or
     an instruction cut off is a failure);
  2. the decoded instruction text equals the entry for that assertion message in EXPECTED below
     (exact comparison after collapsing white space and dropping objdump's trailing "# ..."
     comment; several instructions are joined with " ; ").
It also fails if an assertion message has no EXPECTED entry, or an EXPECTED entry matches no
assertion, so the table cannot silently go stale.

What it does NOT check:
  * semantics: it does not run the code, and it does not know what a message "means"; the
    EXPECTED table was written by decoding the current bytes and reviewing each line against the
    message. A wrong EXPECTED entry would be accepted.
  * assertions that do not have the `same(<expr>, [bytes])` shape (they are not extracted);
  * instructions the encoder does not emit.
  * Only GNU binutils formatting is known: EXPECTED follows objdump 2.44 spelling (for example
    "QWORD PTR [rbp-0x8]"). Another binutils version that prints an instruction differently is
    reported as a mismatch (exit 1) rather than silently accepted.

It is a reviewer aid: it is not run by the Mio test runner or by any workflow.

Usage: python3 check_with_objdump.py [--probe] [path/to/x86_64_test.mio]
  --probe  only look for a GNU objdump: exit 0 if one is available, 3 if not; checks nothing.
Requires: python3 and GNU binutils (objdump). Nothing is installed or modified.
The tool is searched as $MIO_OBJDUMP, then objdump, gobjdump (Homebrew binutils), x86_64-linux-gnu-objdump;
the first one that identifies itself as GNU objdump is used. Apple's objdump is LLVM's and is not accepted.
Exit status: 0 all sequences decode and match; 1 at least one failure; 2 usage / no assertions
found; 3 GNU objdump is not available (not found, or not GNU objdump) - nothing was checked.
"""
import os
import re
import shutil
import subprocess
import sys
import tempfile

EXIT_UNAVAILABLE = 3


def find_gnu_objdump():
    """Return the path of a GNU objdump, or exit with EXIT_UNAVAILABLE and a clear message."""
    candidates = [os.environ.get("MIO_OBJDUMP"), "objdump", "gobjdump", "x86_64-linux-gnu-objdump"]
    problems = []
    for name in filter(None, candidates):
        path = shutil.which(name)
        if path is None:
            problems.append(f"{name}: not found")
            continue
        try:
            first = subprocess.run([path, "--version"], capture_output=True, text=True, check=True).stdout.splitlines()[0]
        except (OSError, subprocess.CalledProcessError, IndexError) as error:
            problems.append(f"{path}: cannot run --version ({error})")
            continue
        if "GNU objdump" in first:
            return path
        problems.append(f"{path}: not GNU objdump ('{first}')")
    print("check_with_objdump.py: GNU objdump is not available; nothing was checked (" + "; ".join(problems) + ")", file=sys.stderr)
    sys.exit(EXIT_UNAVAILABLE)


# Assertion message -> exact objdump text of the bytes (binutils 2.44, Intel syntax).
EXPECTED = {
    'mov rax,rdi': 'mov rax,rdi',
    'mov r8,rcx': 'mov r8,rcx',
    'mov rdx,r9': 'mov rdx,r9',
    'add rax,rcx': 'add rax,rcx',
    'sub rax,rcx': 'sub rax,rcx',
    'imul rax,rcx': 'imul rax,rcx',
    'cmp rax,rcx': 'cmp rax,rcx',
    'div rcx': 'div rcx',
    'mov rax,[rbp-8]': 'mov rax,QWORD PTR [rbp-0x8]',
    'mov [rsp+16],rdi (SIB)': 'mov QWORD PTR [rsp+0x10],rdi',
    'mov r9,[r12+8] (SIB)': 'mov r9,QWORD PTR [r12+0x8]',
    'lea rsi,[rsp+256]': 'lea rsi,[rsp+0x100]',
    'sub rsp,320': 'sub rsp,0x140',
    'push/pop incl. r12': 'push rbx ; push r12 ; pop r12 ; pop rbx',
    'movabs rax,1': 'movabs rax,0x1',
    'movabs rdi,-1': 'movabs rdi,0xffffffffffffffff',
    'movabs r10,INT64_MAX': 'movabs r10,0x7fffffffffffffff',
    'movabs rax,INT64_MIN': 'movabs rax,0x8000000000000000',
    'lock xadd [rdi],rsi': 'lock xadd QWORD PTR [rdi+0x0],rsi',
    'lock cmpxchg [rdi],rsi': 'lock cmpxchg QWORD PTR [rdi+0x0],rsi',
    'mfence; syscall; ret': 'mfence ; syscall ; ret',
    'setb al; movzx eax,al': 'setb al ; movzx eax,al',
    'call r11; call [rbx+8]': 'call r11 ; call QWORD PTR [rbx+0x8]',
    'forward jmp and backward jcc': 'jmp 0x6 ; nop ; je 0x6',
    'lea rdi,[rip+0]': 'lea rdi,[rip+0x0]',
    'and rax,rcx': 'and rax,rcx',
    'or rax,rcx': 'or rax,rcx',
    'xor rax,rax': 'xor rax,rax',
    'test rax,rcx': 'test rax,rcx',
    'xchg rax,rcx': 'xchg rax,rcx',
    'add rsp,320': 'add rsp,0x140',
    'mov eax,231': 'mov eax,0xe7',
    'mov eax,eax': 'mov eax,eax',
    'xchg [rdi+0],rsi': 'xchg QWORD PTR [rdi+0x0],rsi',
    'sete cl; movzx ecx,cl': 'sete cl ; movzx ecx,cl',
    'call rel32 to a later label': 'call 0x6 ; ret ; ret',
    'imul r8,rax': 'imul r8,rax',
    'imul rax,r8': 'imul rax,r8',
    'imul r9,r10': 'imul r9,r10',
    'div r9': 'div r9',
    'lea r8,[rip+0]': 'lea r8,[rip+0x0]',
    'lock xadd [r12+0],r8': 'lock xadd QWORD PTR [r12+0x0],r8',
    'lock cmpxchg [rdi+0],r9': 'lock cmpxchg QWORD PTR [rdi+0x0],r9',
    'xchg [r13+0],r10': 'xchg QWORD PTR [r13+0x0],r10',
    'xchg [r12+0],rax': 'xchg QWORD PTR [r12+0x0],rax',
    'call [r12+8]': 'call QWORD PTR [r12+0x8]',
    'call [r13+16]': 'call QWORD PTR [r13+0x10]',
    'call r12; call r13': 'call r12 ; call r13',
    'push r8; push r15; pop r15; pop r8': 'push r8 ; push r15 ; pop r15 ; pop r8',
    'mov rax,[r13+8]': 'mov rax,QWORD PTR [r13+0x8]',
    'mov r8,[rbp-0x80000000]': 'mov r8,QWORD PTR [rbp-0x80000000]',
    'mov [r12-16],r9': 'mov QWORD PTR [r12-0x10],r9',
    'add rax,-0x80000000': 'add rax,0xffffffff80000000',
    'add rax,0x7fffffff': 'add rax,0x7fffffff',
    'add r9,-1': 'add r9,0xffffffffffffffff',
    'sub r10,-5': 'sub r10,0xfffffffffffffffb',
}


args = sys.argv[1:]
if args[:1] == ["--probe"]:
    print("GNU objdump: " + find_gnu_objdump())
    sys.exit(0)
default = os.path.join(os.path.dirname(os.path.abspath(__file__)), "x86_64_test.mio")
path = args[0] if args else default
try:
    text = open(path, encoding="utf-8").read()
except OSError as error:
    print(f"check_with_objdump.py: cannot read {path}: {error}", file=sys.stderr)
    sys.exit(2)
# `testing.expect(same(<expr>, [bytes]), "msg")` and `testing.expect(<cond> && same(<expr>, [bytes]), "msg")`
pattern = re.compile(r'testing\.expect\((?:[^;]*?&&\s*)?same\([^,]+,\s*\[([0-9,\s]+)\]\)\s*,\s*"([^"]+)"\)')
cases = [(m.group(2), bytes(int(x) for x in m.group(1).split(","))) for m in pattern.finditer(text)]
if not cases:
    print("no byte-sequence assertions found in " + path, file=sys.stderr)
    sys.exit(2)
objdump = find_gnu_objdump()

failures = 0
seen = set()
for message, data in cases:
    seen.add(message)
    with tempfile.NamedTemporaryFile(suffix=".bin", delete=False) as f:
        f.write(data)
        name = f.name
    try:
        out = subprocess.run(
            [objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", name],
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
    complete = consumed == len(data) and bool(decoded) and not broken
    actual = " ; ".join(d.split("#")[0].strip() for d in decoded)
    expected = EXPECTED.get(message)
    if not complete:
        status, detail = "FAIL", f"does not decode as complete instructions: {actual}"
    elif expected is None:
        status, detail = "FAIL", f"no EXPECTED entry for this message (objdump says: {actual})"
    elif actual != expected:
        status, detail = "FAIL", f"expected '{expected}' but objdump says '{actual}'"
    else:
        status, detail = "ok  ", actual
    failures += status == "FAIL"
    print(f"{status} {message:42s} -> {detail}")
for message in EXPECTED:
    if message not in seen:
        failures += 1
        print(f"FAIL {message:42s} -> EXPECTED entry matches no assertion in {os.path.basename(path)}")
print(f"\n{len(cases)} byte sequences, {failures} failed")
sys.exit(1 if failures else 0)
