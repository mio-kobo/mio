#!/usr/bin/env python3
"""Real compiled hidouki / matsu programs on Linux x86-64 (native executables, not the Go VM).

Every program in tests/async/runtime is built with the native self-hosted CLI (--mio), normally and with --gc-stress, and executed
REPEAT times each. An exit status, the exact stdout and (for the failure program) a stderr fragment must match; a hang (timeout),
a crash or one differing byte fails. Reactor programs use real loopback sockets and the epoll reactor.

PASS / FAIL / NOT_RUN only. On a host that is not linux-amd64 every program is NOT_RUN (cross-compilation is not native execution).
Nothing is converted into a skip.
"""
import argparse
import json
import platform
import subprocess
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
RUNTIME = HERE / "runtime"

# name -> (exit status, stdout, stderr fragment or None). Expected values were derived by hand from the language semantics (FIFO cold-task
# executor) and then confirmed by running; they are frozen here, not read back from the implementation.
PROGRAMS = {
    "nested": (30, "", None),
    "fast_path": (0, "5\n1500000\n199990000\n", None),
    "suspend_resume": (0, "101\n102\n1\n103\n50\n2\n201\n202\n203\n8\n9\n0\n10\n4\n", None),
    "failed": (0, "hai\n4\n5\nhidouki function ended without kaesu\nhai\nboom\niie\niie\niie\nboom\n1\n", None),
    "failed_await": (1, "1\n", "awaited task failed: boom"),
    "scheduling": (0, "1\n0\n1\n1\n4\n21\n22\n", None),
    "shutdown": (0, "4950\n0\n0\niie\n101\n", None),
    "multi_ordering": (0, "4006\n1000\n3003\n0\n2001\n0\n1\n2\n3\n4\n2001\n9\n", None),
    "gc_suspended": (0, "910320\n4000\n", None),
    "gc_continuations": (0, "601\n301\n", None),
    "gc_after_completion": (0, "160000\n157540\n", None),
    "gc_no_leak": (0, "8749825000\nhai\n", None),
    "net_concurrent": (0, "160\n440\n", None),
    "net_ready_unrelated": (0, "1\n3\n4\n2\n3\n500\n3\n", None),
    "net_ping_pong": (0, "1\n0\nhai\nhai\n0\n0\n", None),
    "net_close_suspended": (0, "200\n0\n1000\n0\n", None),
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", required=True, help="native linux-amd64 self-hosted mio CLI")
    parser.add_argument("--bunko", default=str(ROOT / "bunko"))
    parser.add_argument("--repeat", type=int, default=3)
    parser.add_argument("--only", default="")
    parser.add_argument("--output", default=None, help="JSON report path")
    args = parser.parse_args()
    native = platform.system() == "Linux" and platform.machine() in ("x86_64", "AMD64")
    rows, failures = [], 0
    with tempfile.TemporaryDirectory() as tmp:
        for name, (code, out, err) in PROGRAMS.items():
            if args.only and name not in args.only.split(","):
                continue
            source = RUNTIME / (name + ".mio")
            row = {"program": name, "variants": {}}
            for variant, flags in (("normal", []), ("gc-stress", ["--gc-stress"])):
                binary = str(Path(tmp) / (name + "." + variant))
                if not native:
                    row["variants"][variant] = "NOT_RUN"
                    continue
                build = subprocess.run([args.mio, "build", str(source), "--bunko-root", args.bunko, "-o", binary] + flags,
                                       capture_output=True, text=True, timeout=600)
                if build.returncode != 0:
                    row["variants"][variant] = "FAIL (build): " + (build.stdout + build.stderr)[:300]
                    failures += 1
                    print("FAIL %s [%s] build: %s" % (name, variant, (build.stdout + build.stderr)[:300]))
                    continue
                result = "PASS"
                for attempt in range(args.repeat):
                    started = time.time()
                    try:
                        proc = subprocess.run([binary], capture_output=True, timeout=300)
                    except subprocess.TimeoutExpired:
                        result = "FAIL (timeout, attempt %d)" % attempt
                        break
                    if proc.returncode != code or proc.stdout.decode(errors="replace") != out or \
                            (err is not None and err not in proc.stderr.decode(errors="replace")):
                        result = "FAIL (attempt %d: exit %d, stdout %r, stderr %r)" % (
                            attempt, proc.returncode, proc.stdout.decode(errors="replace")[:200], proc.stderr.decode(errors="replace")[:200])
                        break
                row["variants"][variant] = result
                if result != "PASS":
                    failures += 1
                    print("FAIL %s [%s]: %s" % (name, variant, result))
            rows.append(row)
    summary = "FAIL" if failures else ("NOT_RUN" if not native else "PASS")
    print("%s: %d programs x 2 variants x %d runs, %d failures" % (summary, len(rows), args.repeat, failures))
    if args.output:
        Path(args.output).write_text(json.dumps({"status": summary, "results": rows}, indent=1))
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
