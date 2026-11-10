#!/usr/bin/env python3
"""Native witness for the Linux x86-64 thread primitives (raw clone, futex join, FS-base TLS) -- OS level only.

A Mio program (built with the compiler under test, $MIO_CI_MIO, and executed) builds a runtime-IR program with
kiso/runtime/ir, links it with kiso/runtime/os/linux and writes the ELF. On a Linux x86-64 host this script then executes
the image: it must exit 0 and print exactly the messages below. The image itself, with a real second thread, checks that
  * the thread runs on its own stack with the import table installed and a distinct kernel thread id,
  * the join word is set by the kernel before the thread runs and cleared at exit (futex join),
  * each thread sees its own FS-base value (TLS) and the creator's is untouched,
  * 4 threads x 50,000 contended lock-xadd increments sum exactly, and
  * a release/acquire publication of a payload written before the release is visible to the acquirer.
No managed allocation happens: this is not a GC witness and the target flags stay false (see operations.mio).
On any other host the execution is printed as NOT_RUN and is not counted as a pass.
Environment: MIO_CI_MIO (compiler), MIO_CI_MODULE_DIR (scratch directory).
"""
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[5]
MESSAGES = ["start\n", "tid\n", "publish\n", "tls\n", "count\n", "done\n"]
THREADS = 4
INCREMENTS = 50000

HEAD = """yobu "kiso/runtime/ir";
yobu "kiso/runtime/os/linux";
yobu "kiso/runtime/gc";
yobu "kiso/array";
yobu "kiso/fs";
kaku functions = linux.platform_program();
"""

DATA_BASE = 268435456          # elf.data_base
BLOCK = DATA_BASE + 11 * 8     # the shared block follows the 11-slot import table
# Block layout (bytes): 0 counter, 8 flag, 16 payload, 32 child tid, 40 worker TLS mismatches, 48 child stack check,
# 64 + 8*i tid words (i < THREADS + 1), 256 + 64*i per-thread TLS blocks

def build():
    out = []
    w = out.append
    w("""
kaku worker = ir.function("worker");
// argument = this thread's own TLS block address; the kernel installed it as the FS base at clone time
ir.put(worker, ir.copy, 20, 0, 0, 0);
ir.put(worker, ir.constant, 21, 0, 0, %(block)d);
ir.invoke_function(worker, "platform_tls_get", 22, []);
ir.put(worker, ir.equal, 23, 22, 20, 0); ir.branch(worker, ir.nonzero, 23, "tls_ok");
ir.put(worker, ir.constant, 24, 0, 0, 1);
array.push(worker.steps, ir.Step { kind: ir.fetch_add, destination: 29, left: 21, right: 24, immediate: 40, name: "", arguments: [], order: gc.seq_cst, failure_order: gc.relaxed });
ir.branch(worker, ir.label, 0, "tls_ok");
ir.put(worker, ir.constant, 25, 0, 0, 0); ir.put(worker, ir.constant, 26, 0, 0, %(inc)d); ir.put(worker, ir.constant, 27, 0, 0, 1);
ir.branch(worker, ir.label, 0, "loop"); ir.put(worker, ir.less_unsigned, 28, 25, 26, 0); ir.branch(worker, ir.zero, 28, "after");
array.push(worker.steps, ir.Step { kind: ir.fetch_add, destination: 29, left: 21, right: 27, immediate: 0, name: "", arguments: [], order: gc.seq_cst, failure_order: gc.relaxed });
ir.put(worker, ir.add, 25, 25, 27, 0); ir.branch(worker, ir.jump, 0, "loop");
ir.branch(worker, ir.label, 0, "after"); ir.put(worker, ir.constant, 30, 0, 0, 0); ir.put(worker, ir.result, 0, 30, 0, 0);
array.push(functions, worker);

kaku child = ir.function("child");
ir.put(child, ir.constant, 21, 0, 0, %(block)d);
ir.invoke_function(child, "platform_thread_id", 22, []); ir.put(child, ir.store, 0, 21, 22, 32);
ir.put(child, ir.scratch_address, 23, 0, 0, 0); ir.put(child, ir.constant, 24, 0, 0, 77); ir.put(child, ir.store, 0, 23, 24, 0);
ir.put(child, ir.load, 25, 23, 0, 0); ir.put(child, ir.equal, 26, 25, 24, 0); ir.put(child, ir.store, 0, 21, 26, 48);
ir.put(child, ir.constant, 27, 0, 0, 3405691582); ir.put(child, ir.store, 0, 21, 27, 16);
ir.put(child, ir.constant, 28, 0, 0, 1); ir.put(child, ir.release_store, 0, 21, 28, 8);
ir.put(child, ir.constant, 30, 0, 0, 0); ir.put(child, ir.result, 0, 30, 0, 0);
array.push(functions, child);

kaku main = ir.function("main");
waza fail(f: ir.Function, code: int64, label: string) -> unit {
  ir.branch(f, ir.label, 0, label); ir.put(f, ir.constant, 30, 0, 0, code); ir.put(f, ir.result, 0, 30, 0, 0);
}
ir.invoke_function(main, "say_start", 1, []);
ir.put(main, ir.constant, 2, 0, 0, %(block)d);
ir.put(main, ir.constant, 3, 0, 0, 65536);
ir.put(main, ir.constant, 5, 0, 0, -4095);
// our own FS base, to prove the children's TLS never touches it
ir.invoke_function(main, "platform_tls_get", 26, []); ir.put(main, ir.copy, 27, 26, 0, 0);
// ---- one thread: its own stack, kernel-assigned id (equal to what it sees), publication
ir.invoke_function(main, "platform_thread_stack", 4, [3]);
ir.put(main, ir.less_unsigned, 6, 4, 5, 0); ir.branch(main, ir.zero, 6, "fail_stack");
ir.put(main, ir.constant, 8, 0, 0, 64); ir.put(main, ir.add, 8, 2, 8, 0);
ir.function_pointer(main, "child", 9); ir.put(main, ir.constant, 10, 0, 0, 0); ir.put(main, ir.constant, 11, 0, 0, 0);
ir.invoke_function(main, "platform_thread_create", 12, [9, 10, 4, 3, 8, 11]);
ir.put(main, ir.constant, 13, 0, 0, 0); ir.put(main, ir.less_unsigned, 14, 13, 12, 0); ir.branch(main, ir.zero, 14, "fail_create");
ir.put(main, ir.less_unsigned, 14, 12, 5, 0); ir.branch(main, ir.zero, 14, "fail_create");
ir.invoke_function(main, "platform_thread_join", 16, [8]); ir.branch(main, ir.nonzero, 16, "fail_join");
ir.put(main, ir.load, 17, 8, 0, 0); ir.branch(main, ir.nonzero, 17, "fail_word");
ir.put(main, ir.load, 18, 2, 0, 32); ir.put(main, ir.equal, 19, 18, 12, 0); ir.branch(main, ir.zero, 19, "fail_tid");
ir.invoke_function(main, "platform_thread_id", 20, []); ir.put(main, ir.equal, 19, 20, 18, 0); ir.branch(main, ir.nonzero, 19, "fail_tid");
ir.put(main, ir.load, 21, 2, 0, 48); ir.put(main, ir.constant, 22, 0, 0, 1); ir.put(main, ir.equal, 19, 21, 22, 0); ir.branch(main, ir.zero, 19, "fail_marker");
ir.invoke_function(main, "say_tid", 1, []);
// ---- release/acquire publication of a payload written before the release store
ir.put(main, ir.acquire_load, 23, 2, 0, 8); ir.branch(main, ir.zero, 23, "fail_publish");
ir.put(main, ir.load, 24, 2, 0, 16); ir.put(main, ir.constant, 25, 0, 0, 3405691582); ir.put(main, ir.equal, 19, 24, 25, 0); ir.branch(main, ir.zero, 19, "fail_publish");
ir.invoke_function(main, "say_publish", 1, []);
""" % dict(block=BLOCK, inc=INCREMENTS))
    # THREADS workers, each with its own stack, tid word and TLS block
    for i in range(THREADS):
        tls = BLOCK + 256 + 64 * i
        tid = BLOCK + 128 + 8 * i
        w("""ir.invoke_function(main, "platform_thread_stack", 4, [3]);
ir.put(main, ir.less_unsigned, 6, 4, 5, 0); ir.branch(main, ir.zero, 6, "fail_count");
ir.put(main, ir.constant, 8, 0, 0, %(tid)d); ir.put(main, ir.constant, 10, 0, 0, %(tls)d);
ir.function_pointer(main, "worker", 9);
ir.invoke_function(main, "platform_thread_create", 12, [9, 10, 4, 3, 8, 10]);
ir.put(main, ir.constant, 13, 0, 0, 0); ir.put(main, ir.less_unsigned, 14, 13, 12, 0); ir.branch(main, ir.zero, 14, "fail_count");
ir.put(main, ir.less_unsigned, 14, 12, 5, 0); ir.branch(main, ir.zero, 14, "fail_count");
""" % dict(tid=tid, tls=tls))
    for i in range(THREADS):
        tid = BLOCK + 128 + 8 * i
        w("""ir.put(main, ir.constant, 8, 0, 0, %d); ir.invoke_function(main, "platform_thread_join", 16, [8]); ir.branch(main, ir.nonzero, 16, "fail_count");
""" % tid)
    w("""// ---- every worker saw its own FS base; ours is unchanged; the contended counter is exact
ir.put(main, ir.load, 17, 2, 0, 40); ir.branch(main, ir.nonzero, 17, "fail_tls");
ir.invoke_function(main, "platform_tls_get", 18, []); ir.put(main, ir.equal, 19, 18, 27, 0); ir.branch(main, ir.zero, 19, "fail_tls");
ir.invoke_function(main, "say_tls", 1, []);
ir.put(main, ir.load, 20, 2, 0, 0); ir.put(main, ir.constant, 21, 0, 0, %(total)d); ir.put(main, ir.equal, 19, 20, 21, 0); ir.branch(main, ir.zero, 19, "fail_count");
ir.invoke_function(main, "say_count", 1, []);
ir.invoke_function(main, "say_done", 1, []);
ir.put(main, ir.constant, 30, 0, 0, 0); ir.put(main, ir.result, 0, 30, 0, 0);
fail(main, 11, "fail_start"); fail(main, 16, "fail_stack"); fail(main, 17, "fail_create"); fail(main, 18, "fail_join"); fail(main, 19, "fail_word"); fail(main, 20, "fail_marker"); fail(main, 12, "fail_tid"); fail(main, 13, "fail_publish"); fail(main, 14, "fail_tls"); fail(main, 15, "fail_count");
array.push(functions, main);
kaku image = linux.executable(functions, linux.bindings(), "main", 4096);
moshi (image.error != "") { chant(image.error); }
sore { kaku written = fs.write_all(OUTPUT, image.bytes); moshi (written.error != mu) { chant("write failed"); } sore { chant(len(image.bytes)); } }
""" % dict(total=THREADS * INCREMENTS))
    return "".join(out)


def word(text):
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
    return HEAD + say + build().replace("OUTPUT", json.dumps(str(output)), 1)


def main():
    mio = os.environ["MIO_CI_MIO"]
    work = Path(os.environ["MIO_CI_MODULE_DIR"])
    image = work / "thread-witness.elf"
    program = work / "thread-witness.mio"
    program.write_text(source(image), encoding="utf-8")
    native = work / "thread-witness.builder"
    host = ["--target=linux-amd64"] if (platform.system(), platform.machine()) in (("Linux", "x86_64"), ("Linux", "amd64")) else []
    build_run = subprocess.run([mio, "build", str(program), *host, "--bunko-root", str(ROOT / "bunko"), "-o", str(native)],
                               capture_output=True, text=True, timeout=900)
    (work / "thread-witness.build.log").write_text(build_run.stdout + build_run.stderr)
    if build_run.returncode != 0:
        raise RuntimeError(f"building the Mio image builder failed: exit={build_run.returncode} {build_run.stderr[-2000:]!r}")
    run = subprocess.run([str(native)], capture_output=True, text=True, timeout=900)
    if run.returncode != 0 or not run.stdout.strip().isdigit() or not image.is_file():
        raise RuntimeError(f"the Mio image builder failed: exit={run.returncode} stdout={run.stdout!r} stderr={run.stderr[-2000:]!r}")
    data = image.read_bytes()
    if len(data) != int(run.stdout.strip()) or data[:7] != b"\x7fELF\x02\x01\x01":
        raise RuntimeError("the produced file is not the ELF image the builder reported")
    print(f"image: PASS ({len(data)} bytes, ELF64)")
    if platform.system() == "Linux" and platform.machine() == "x86_64":
        os.chmod(image, 0o755)
        want = "".join(MESSAGES).encode()
        for attempt in range(20):  # repeated: a race or a lost wake-up must not hide behind one lucky run
            result = subprocess.run([str(image)], capture_output=True, timeout=60)
            if (result.returncode, result.stdout, result.stderr) != (0, want, b""):
                raise RuntimeError(f"native run {attempt}: exit={result.returncode} stdout={result.stdout!r} stderr={result.stderr!r}; want exit 0 and {want!r}")
        print("execution: PASS (20 runs: 4 real threads, futex join, per-thread TLS, exact contended count, release/acquire)")
    else:
        print(f"execution: NOT_RUN (host is {platform.system()} {platform.machine()}, not Linux x86_64)")
    print("thread witness oracle PASS")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError) as error:
        print("thread witness oracle FAIL: " + str(error), file=sys.stderr)
        sys.exit(1)
