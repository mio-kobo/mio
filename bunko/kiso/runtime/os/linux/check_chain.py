#!/usr/bin/env python3
"""Independent end-to-end check of the pure-Mio Linux x86-64 runtime chain (outside testing.expect).

A Mio program (built with the compiler under test, $MIO_CI_MIO, and executed) builds a runtime-IR program
with kiso/runtime/ir, links it with kiso/runtime/os/linux (which uses kiso/runtime/cpu/amd64,
kiso/aot/x86_64 and kiso/aot/elf) and writes the resulting ELF. This script then

  1. parses the ELF header with Python's struct (always): ET_EXEC, EM_X86_64, entry inside the code;
  2. on a Linux x86-64 host only, executes the image and requires exit status 0 and exactly the ten
     messages below on stdout and nothing on stderr. The image checks, on its own, a counted loop, the
     monotonic clock, mmap/mprotect/madvise/munmap, futex wait/wake, unsigned divide (including by zero),
     a seven-argument call (r10), lock xadd / cmpxchg, and the documented platform contracts, and prints a
     message after each passing section; a failing section exits with a distinct non-zero status.
On any other host step 2 is printed as "execution: NOT_RUN (...)": it is not counted as passed, and the
bytes the Mio code produced are then only checked structurally.

On linux-amd64 the native CI runs this check with the native CLI (scripts/ci/native_platforms.py).
Exit status: 0 all executed checks passed, 1 a check failed.
Environment: MIO_CI_MIO (compiler), MIO_CI_MODULE_DIR (scratch directory).
"""
import json
import os
from pathlib import Path
import platform
import struct
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[5]
MESSAGES = ["hello\n", "sum\n", "clock\n", "memory\n", "futex\n", "divide\n", "args7\n", "atomic\n", "rules\n", "done\n"]
HEAD = """yobu "kiso/runtime/ir";
yobu "kiso/runtime/os/linux";
yobu "kiso/runtime/gc";
yobu "kiso/array";
yobu "kiso/fs";
kaku functions = linux.platform_program();
"""
TAIL = """
// weighted(a0..a6) = a0 + 10*a1 + 100*a2 + ... exercises all seven argument registers.
kaku weighted = ir.function("weighted");
kaku w = 1; kaku wi = 0; kaku acc = 7;
ir.put(weighted, ir.constant, acc, 0, 0, 0);
aida (wi < 7) {
  ir.put(weighted, ir.constant, 10, 0, 0, w); ir.put(weighted, ir.multiply, 11, wi, 10, 0); ir.put(weighted, ir.add, acc, acc, 11, 0);
  w = w * 10; wi = wi + 1;
}
ir.put(weighted, ir.result, 0, acc, 0, 0);
array.push(functions, weighted);

kaku main = ir.function("main");
waza fail(f: ir.Function, code: int64, label: string) -> unit {
  ir.branch(f, ir.label, 0, label); ir.put(f, ir.constant, 30, 0, 0, code); ir.put(f, ir.result, 0, 30, 0, 0);
}
ir.invoke_function(main, "say_hello", 1, []);
// ---- sum 1..100 with a counted loop
ir.put(main, ir.constant, 1, 0, 0, 0); ir.put(main, ir.constant, 2, 0, 0, 1); ir.put(main, ir.constant, 3, 0, 0, 101); ir.put(main, ir.constant, 5, 0, 0, 1);
ir.branch(main, ir.label, 0, "loop"); ir.put(main, ir.less_unsigned, 4, 2, 3, 0); ir.branch(main, ir.zero, 4, "loop_done");
ir.put(main, ir.add, 1, 1, 2, 0); ir.put(main, ir.add, 2, 2, 5, 0); ir.branch(main, ir.jump, 0, "loop");
ir.branch(main, ir.label, 0, "loop_done"); ir.put(main, ir.constant, 6, 0, 0, 5050); ir.put(main, ir.equal, 7, 1, 6, 0); ir.branch(main, ir.zero, 7, "fail_sum");
ir.invoke_function(main, "say_sum", 8, []);
// ---- monotonic clock never goes backwards and is non-zero
ir.invoke_function(main, "platform_now", 9, []); ir.invoke_function(main, "platform_now", 10, []);
ir.put(main, ir.less_unsigned, 11, 10, 9, 0); ir.branch(main, ir.nonzero, 11, "fail_clock"); ir.branch(main, ir.zero, 9, "fail_clock");
ir.invoke_function(main, "say_clock", 8, []);
// ---- virtual memory: reserve, commit, write/read, decommit (reads zero), release
ir.put(main, ir.constant, 12, 0, 0, 1048576); ir.invoke_function(main, "platform_reserve", 13, [12]);
ir.put(main, ir.constant, 14, 0, 0, -4096); ir.put(main, ir.less_unsigned, 15, 13, 14, 0); ir.branch(main, ir.zero, 15, "fail_memory");
ir.invoke_function(main, "platform_commit", 16, [13, 12]); ir.branch(main, ir.nonzero, 16, "fail_memory");
ir.put(main, ir.constant, 17, 0, 0, 11259375); ir.put(main, ir.store, 0, 13, 17, 4096); ir.put(main, ir.load, 18, 13, 0, 4096);
ir.put(main, ir.equal, 19, 17, 18, 0); ir.branch(main, ir.zero, 19, "fail_memory");
ir.invoke_function(main, "platform_decommit", 16, [13, 12]); ir.branch(main, ir.nonzero, 16, "fail_memory");
ir.invoke_function(main, "platform_commit", 16, [13, 12]); ir.branch(main, ir.nonzero, 16, "fail_memory");
ir.put(main, ir.load, 18, 13, 0, 4096); ir.branch(main, ir.nonzero, 18, "fail_memory");
ir.invoke_function(main, "platform_release", 16, [13, 12]); ir.branch(main, ir.nonzero, 16, "fail_memory");
ir.invoke_function(main, "say_memory", 8, []);
// ---- futex: changed value returns at once (0); unchanged value times out (1) after >= 20 ms
ir.put(main, ir.scratch_address, 20, 0, 0, 8); ir.put(main, ir.constant, 21, 0, 0, 5); ir.put(main, ir.store, 0, 20, 21, 0);
ir.put(main, ir.constant, 22, 0, 0, 6); ir.put(main, ir.constant, 23, 0, 0, 20000000);
ir.invoke_function(main, "platform_futex_wait", 24, [20, 22, 23]); ir.branch(main, ir.nonzero, 24, "fail_futex");
ir.invoke_function(main, "platform_now", 9, []);
ir.invoke_function(main, "platform_futex_wait", 24, [20, 21, 23]); ir.put(main, ir.constant, 25, 0, 0, 1); ir.put(main, ir.equal, 26, 24, 25, 0); ir.branch(main, ir.zero, 26, "fail_futex");
ir.invoke_function(main, "platform_now", 10, []); ir.put(main, ir.subtract, 27, 10, 9, 0); ir.put(main, ir.less_unsigned, 28, 27, 23, 0); ir.branch(main, ir.nonzero, 28, "fail_futex");
ir.invoke_function(main, "say_futex", 8, []);
// ---- unsigned divide: 100/7 = 14, divide by zero yields 0 (same as AArch64 UDIV)
ir.put(main, ir.constant, 1, 0, 0, 100); ir.put(main, ir.constant, 2, 0, 0, 7); ir.put(main, ir.constant, 3, 0, 0, 0);
ir.put(main, ir.divide_unsigned, 4, 1, 2, 0); ir.put(main, ir.constant, 5, 0, 0, 14); ir.put(main, ir.equal, 6, 4, 5, 0); ir.branch(main, ir.zero, 6, "fail_divide");
ir.put(main, ir.divide_unsigned, 4, 1, 3, 0); ir.branch(main, ir.nonzero, 4, "fail_divide");
ir.invoke_function(main, "say_divide", 8, []);
// ---- seven-argument call (7th argument travels in r10)
kaku ai = 0; kaku args: []int64 = [];
aida (ai < 7) { ir.put(main, ir.constant, 10 + ai, 0, 0, ai + 1); array.push(args, 10 + ai); ai = ai + 1; }
ir.invoke_function(main, "weighted", 1, args); ir.put(main, ir.constant, 2, 0, 0, 7654321); ir.put(main, ir.equal, 3, 1, 2, 0); ir.branch(main, ir.zero, 3, "fail_args");
ir.invoke_function(main, "say_args7", 8, []);
// ---- atomics: fetch_add returns the old value; CAS returns the observed value and stores only on match
ir.put(main, ir.scratch_address, 20, 0, 0, 16); ir.put(main, ir.constant, 21, 0, 0, 10); ir.put(main, ir.store, 0, 20, 21, 0);
ir.put(main, ir.constant, 22, 0, 0, 5);
array.push(main.steps, ir.Step { kind: ir.fetch_add, destination: 23, left: 20, right: 22, immediate: 0, name: "", arguments: [], order: gc.seq_cst, failure_order: gc.relaxed });
ir.put(main, ir.equal, 24, 23, 21, 0); ir.branch(main, ir.zero, 24, "fail_atomic");
ir.put(main, ir.constant, 25, 0, 0, 99); ir.put(main, ir.constant, 26, 0, 0, 15);
array.push(main.steps, ir.Step { kind: ir.compare_exchange, destination: 27, left: 20, right: 25, immediate: 0, name: "", arguments: [26], order: gc.seq_cst, failure_order: gc.relaxed });
ir.put(main, ir.equal, 28, 27, 26, 0); ir.branch(main, ir.zero, 28, "fail_atomic");
ir.put(main, ir.load, 29, 20, 0, 0); ir.put(main, ir.equal, 28, 29, 25, 0); ir.branch(main, ir.zero, 28, "fail_atomic");
ir.put(main, ir.constant, 31, 0, 0, 7);
array.push(main.steps, ir.Step { kind: ir.compare_exchange, destination: 27, left: 20, right: 21, immediate: 0, name: "", arguments: [31], order: gc.seq_cst, failure_order: gc.relaxed });
ir.put(main, ir.equal, 28, 27, 25, 0); ir.branch(main, ir.zero, 28, "fail_atomic");
ir.put(main, ir.load, 29, 20, 0, 0); ir.put(main, ir.equal, 28, 29, 25, 0); ir.branch(main, ir.zero, 28, "fail_atomic");
ir.invoke_function(main, "say_atomic", 8, []);
// ---- platform contracts (futex word width, zero timeout, wake with no waiter, errno results)
ir.put(main, ir.scratch_address, 1, 0, 0, 24);
ir.put(main, ir.constant, 2, 0, 0, 5); ir.put(main, ir.store, 0, 1, 2, 0); ir.put(main, ir.constant, 3, 0, 0, 0); ir.put(main, ir.constant, 5, 0, 0, 1);
// a zero timeout on a matching word returns "timed out" (1) at once: it never waits forever
ir.invoke_function(main, "platform_futex_wait", 4, [1, 2, 3]); ir.put(main, ir.equal, 6, 4, 5, 0); ir.branch(main, ir.zero, 6, "fail_contract");
// only the low 32 bits are compared: word 2^32+5 still matches expected 5 ...
ir.put(main, ir.constant, 7, 0, 0, 4294967301); ir.put(main, ir.store, 0, 1, 7, 0);
ir.invoke_function(main, "platform_futex_wait", 4, [1, 2, 3]); ir.put(main, ir.equal, 6, 4, 5, 0); ir.branch(main, ir.zero, 6, "fail_contract");
// ... and expected 2^32+5 matches word 5
ir.put(main, ir.store, 0, 1, 2, 0);
ir.invoke_function(main, "platform_futex_wait", 4, [1, 7, 3]); ir.put(main, ir.equal, 6, 4, 5, 0); ir.branch(main, ir.zero, 6, "fail_contract");
// a different low half is "value changed" (0) although the high halves are equal
ir.put(main, ir.constant, 8, 0, 0, 4294967302);
ir.invoke_function(main, "platform_futex_wait", 4, [1, 8, 3]); ir.branch(main, ir.nonzero, 4, "fail_contract");
// waking with nobody waiting wakes zero waiters (and is not an error)
ir.invoke_function(main, "platform_futex_wake_all", 4, [1]); ir.branch(main, ir.nonzero, 4, "fail_contract");
ir.put(main, ir.constant, 9, 0, 0, 1); ir.invoke_function(main, "platform_futex_wake", 4, [1, 9]); ir.branch(main, ir.nonzero, 4, "fail_contract");
// platform_reserve with an absurd length fails with a negated errno, never with an address
ir.put(main, ir.constant, 10, 0, 0, 4611686018427387904); ir.invoke_function(main, "platform_reserve", 11, [10]);
ir.put(main, ir.constant, 12, 0, 0, -4095); ir.put(main, ir.less_unsigned, 13, 11, 12, 0); ir.branch(main, ir.nonzero, 13, "fail_contract");
// platform_release of an unaligned address is -EINVAL (-22)
ir.put(main, ir.constant, 14, 0, 0, 1); ir.put(main, ir.constant, 15, 0, 0, 4096); ir.invoke_function(main, "platform_release", 16, [14, 15]);
ir.put(main, ir.constant, 17, 0, 0, -22); ir.put(main, ir.equal, 18, 16, 17, 0); ir.branch(main, ir.zero, 18, "fail_contract");
ir.invoke_function(main, "say_rules", 8, []);
ir.invoke_function(main, "say_done", 8, []);
ir.put(main, ir.constant, 30, 0, 0, 0); ir.put(main, ir.result, 0, 30, 0, 0);
fail(main, 11, "fail_sum"); fail(main, 12, "fail_clock"); fail(main, 13, "fail_memory"); fail(main, 14, "fail_futex");
fail(main, 15, "fail_divide"); fail(main, 16, "fail_args"); fail(main, 17, "fail_atomic"); fail(main, 18, "fail_contract");
array.push(functions, main);
kaku image = linux.executable(functions, linux.bindings(), "main", 0);
moshi (image.error != "") { chant(image.error); }
sore { kaku written = fs.write_all(OUTPUT, image.bytes); moshi (written.error != mu) { chant("write failed"); } sore { chant(len(image.bytes)); } }
"""


def word(text):
    """The 64-bit constant that holds up to 8 ASCII bytes, little endian (what the IR stores to build a message)."""
    data = text.encode("ascii")
    if len(data) > 8:
        raise ValueError("more than 8 bytes cannot be packed into one 64-bit IR constant: " + repr(text))
    return int.from_bytes(data, "little")


def source(output):
    say = ""
    for message in MESSAGES:
        name = message.rstrip("\n")
        say += f"""
kaku say_{name} = ir.function("say_{name}");
ir.put(say_{name}, ir.scratch_address, 1, 0, 0, 0); ir.put(say_{name}, ir.constant, 2, 0, 0, {word(message)});
ir.put(say_{name}, ir.store, 0, 1, 2, 0); ir.put(say_{name}, ir.constant, 3, 0, 0, {len(message)});
ir.call(say_{name}, 103, 4, [1, 3]); ir.put(say_{name}, ir.result, 0, 4, 0, 0);
array.push(functions, say_{name});
"""
    return HEAD + say + TAIL.replace("OUTPUT", json.dumps(str(output)), 1)


def main():
    mio = os.environ["MIO_CI_MIO"]
    work = Path(os.environ["MIO_CI_MODULE_DIR"])
    image = work / "runtime-chain.elf"
    program = work / "runtime-chain.mio"
    program.write_text(source(image), encoding="utf-8")
    native = work / "runtime-chain.builder"
    # The builder runs on this host: build it for the host's native target explicitly.
    host = ["--target=linux-amd64"] if (platform.system(), platform.machine()) in (("Linux", "x86_64"), ("Linux", "amd64")) else []
    build = subprocess.run([mio, "build", str(program), *host, "--bunko-root", str(ROOT / "bunko"), "-o", str(native)],
                           capture_output=True, text=True, timeout=900)
    (work / "runtime-chain.build.log").write_text(build.stdout + build.stderr)
    if build.returncode != 0:
        raise RuntimeError(f"building the Mio image builder failed: exit={build.returncode} {build.stderr[-2000:]!r}")
    run = subprocess.run([str(native)], capture_output=True, text=True, timeout=900)
    (work / "runtime-chain.builder.stdout.log").write_text(run.stdout)
    (work / "runtime-chain.builder.stderr.log").write_text(run.stderr)
    if run.returncode != 0 or not run.stdout.strip().isdigit() or not image.is_file():
        raise RuntimeError(f"the Mio image builder failed: exit={run.returncode} stdout={run.stdout!r} stderr={run.stderr[-2000:]!r}")
    data = image.read_bytes()
    if len(data) != int(run.stdout.strip()):
        raise RuntimeError(f"image file is {len(data)} bytes but the builder reported {run.stdout.strip()}")
    ident, etype, machine, _, entry = struct.unpack_from("<16sHHIQ", data)
    if ident[:7] != b"\x7fELF\x02\x01\x01" or (etype, machine) != (2, 62) or not 0x401000 <= entry < 0x400000 + len(data):
        raise RuntimeError("the produced file is not a Linux x86-64 ET_EXEC image with its entry in the code")
    print(f"image: PASS ({len(data)} bytes, valid ELF64 x86-64 header)")
    if platform.system() == "Linux" and platform.machine() == "x86_64":
        os.chmod(image, 0o755)
        result = subprocess.run([str(image)], capture_output=True, timeout=30)
        want = "".join(MESSAGES).encode()
        if (result.returncode, result.stdout, result.stderr) != (0, want, b""):
            raise RuntimeError(f"native run: exit={result.returncode} stdout={result.stdout!r} stderr={result.stderr!r}; want exit 0 and {want!r}")
        print("execution: PASS (exit 0, all ten sections reported)")
    else:
        print(f"execution: NOT_RUN (host is {platform.system()} {platform.machine()}, not Linux x86_64)")
    print("runtime chain independent oracle PASS")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError) as error:
        print("runtime chain independent oracle FAIL: " + str(error), file=sys.stderr)
        sys.exit(1)
