#!/usr/bin/env python3
"""Real compiled multi-threaded Mio programs on Linux x86-64 (OS threads registered with the collector).

Each program in this directory is built with the native CLI ($MIO_CI_MIO or --mio) for linux-amd64, normally and with --gc-stress, and
executed repeatedly. Every run must exit 0 and print exactly the expected output; a hang (timeout), a crash or one differing byte fails.
Repetition is deliberate: a race in registration, the stop-the-world handshake or the heap lock shows up as an occasional wrong total,
a deadlock or a crash, not as a deterministic failure. On any other host the result is NOT_RUN (never PASS).

Programs (expected output):
  basic        start / join / result through a handle
  gc_stress    5 mutators building linked structures; threaded total == sequential total; main blocked in join during collections
  captures     entry closures and the data they capture survive collections after the creator dropped every other reference
  nested       threads starting and joining threads, three levels deep
  many         32 thread lifetimes (8 at a time x 4 rounds), more than the cores
  reclaim      ~19 GiB of garbage through an 8 GiB heap from 5 threads: only finishes if concurrent collections reclaim memory
  channel_*    zero-copy channels (kiso/thread): producer/consumer, close and failed-send hand-back, shared sub-objects / cycles / deep and
               large graphs, 3 producers x 3 consumers; reject_*: compile-time rejection of use after send, live alias, double send
"""
import argparse
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
PROGRAMS = {
    "basic": b"hai\n7\n500500\n2001000\n4501500\n",
    "gc_stress": b"hai\n84768000\n",
    "captures": b"80000\n4950\n31950\n",
    "nested": b"776050\nhai\n",
    "many": b"4024056\n" * 4,
    "reclaim": b"300000\n",
    "channel_basic": b"50\n2600\n50\n",
    "channel_close": b"hai\n24\nhai\n33\niie\n11\nhai\n",
    "channel_graphs": b"99\n1\n2000\n399980000\nhai\n3\n",
    "channel_mpmc": b"600\n1379100\n",
    "summary_copy": b"198\n5\n",
    # Stage one hardening (docs/threading-stages.md)
    "lifecycle_cycles": b"252000\n0\n126000\n",
    "invalid_handles": b"-22\n-22\n-22\niie\nhai\n7\niie\nhai\niie\n7\nhai\n-5\n",
    "creation_limit": b"hai\nhai\nhai\nhai\n1\n",
    "shutdown_active": b"199990000\nhai\n",
    "channel_races": b"80\n",
    "gc_lifecycle": b"913200\n",
    "channel_lifecycle": b"2000\nhai\n9\nhai\n1\n",
    "channel_alloc_failure": b"hai\n" * 14,
}
# Programs whose process must be killed by a fault (the guard page of a thread stack): name -> (stdout before the fault, signal number)
FAULTS = {"stack_overflow": (b"hai\n", 11)}
HEAVY = {"lifecycle_cycles", "creation_limit", "shutdown_active", "channel_alloc_failure"}   # fewer repetitions

# Programs that must be REJECTED by the compiler, with the diagnostic code that must appear: ownership is checked at compile time.
NEGATIVE = {"reject_use_after_send": "E0702", "reject_alias_send": "E0704", "reject_double_send": "E0703",
            "reject_escape_global": "E0701"}
STRESS_SKIPPED = {"reclaim", "lifecycle_cycles", "creation_limit", "shutdown_active"}   # stress collects on every allocation: 19 GiB of garbage (reclaim) or thousands of threads would take far too long


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--mio", default=os.environ.get("MIO_CI_MIO", "mio"))
    parser.add_argument("--runs", type=int, default=25)
    parser.add_argument("--bunko-root", default=str(ROOT / "bunko"))
    args = parser.parse_args()
    if not (platform.system() == "Linux" and platform.machine() in ("x86_64", "amd64")):
        print("execution: NOT_RUN (host is %s %s, not Linux x86_64)" % (platform.system(), platform.machine()))
        return 0
    failures = []
    with tempfile.TemporaryDirectory() as work:
        for name, expected in PROGRAMS.items():
            for mode in ("", "--gc-stress"):
                if mode and name in STRESS_SKIPPED:
                    continue
                image = Path(work) / (name + (".stress" if mode else ""))
                command = [args.mio, "build", str(HERE / (name + ".mio")), "--target=linux-amd64", "--bunko-root", args.bunko_root, "-o", str(image)]
                if mode:
                    command.insert(3, mode)
                built = subprocess.run(command, capture_output=True, text=True, timeout=900)
                if built.returncode != 0 or not image.is_file():
                    failures.append("%s%s: build failed: %s" % (name, " " + mode if mode else "", (built.stderr or built.stdout)[-400:]))
                    continue
                runs = 3 if name == "reclaim" or name in HEAVY else args.runs
                for index in range(runs):
                    try:
                        result = subprocess.run([str(image)], capture_output=True, timeout=120)
                    except subprocess.TimeoutExpired:
                        failures.append("%s%s run %d: timeout (hang)" % (name, " " + mode if mode else "", index))
                        break
                    if (result.returncode, result.stdout, result.stderr) != (0, expected, b""):
                        failures.append("%s%s run %d: exit=%s stdout=%r stderr=%r want %r" % (
                            name, " " + mode if mode else "", index, result.returncode, result.stdout[:200], result.stderr[:200], expected))
                        break
                else:
                    print("%s%s: PASS (%d runs)" % (name, " " + mode if mode else "", runs))
        for name, (before, signal_number) in FAULTS.items():
            image = Path(work) / name
            built = subprocess.run([args.mio, "build", str(HERE / (name + ".mio")), "--target=linux-amd64", "--bunko-root", args.bunko_root, "-o", str(image)],
                                   capture_output=True, text=True, timeout=900)
            if built.returncode != 0:
                failures.append("%s: build failed: %s" % (name, (built.stderr or built.stdout)[-300:]))
                continue
            for index in range(3):
                try:
                    result = subprocess.run([str(image)], capture_output=True, timeout=60)
                except subprocess.TimeoutExpired:
                    failures.append("%s run %d: timeout (hang)" % (name, index))
                    break
                if result.returncode != -signal_number or result.stdout != before:
                    failures.append("%s run %d: exit=%s stdout=%r, want death by signal %d after %r" % (name, index, result.returncode, result.stdout[:100], signal_number, before))
                    break
            else:
                print("%s: PASS (3 runs, killed by signal %d at the guard page)" % (name, signal_number))
        for name, code in NEGATIVE.items():
            built = subprocess.run([args.mio, "build", str(HERE / (name + ".mio")), "--target=linux-amd64", "--bunko-root", args.bunko_root,
                                    "-o", str(Path(work) / name)], capture_output=True, text=True, timeout=900)
            text = built.stdout + built.stderr
            if built.returncode == 0 or code not in text:
                failures.append("%s: expected a compile error %s, got exit %s: %s" % (name, code, built.returncode, text[-300:]))
            else:
                print("%s: PASS (rejected with %s)" % (name, code))
    if failures:
        for line in failures:
            print("FAIL " + line, file=sys.stderr)
        return 1
    print("execution: PASS (every program, normal and --gc-stress, repeated)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
