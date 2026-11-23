#!/usr/bin/env python3
"""Runs benchmarks/async/bench.mio and baseline.mio natively (Linux x86-64) and prints medians.

Each mode runs RUNS times (default 7); the table reports median / min / max wall milliseconds as measured inside the program
(os.monotonic_ms) plus the work size. There is no threshold: nothing here passes or fails on timing. NOT_RUN off linux-amd64.
"""
import argparse, platform, statistics, subprocess, sys, tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
MODES = ["await_completed", "nested_calls", "task_throughput", "construct_only", "schedule_only", "suspend_resume_many",
         "gc_overhead", "reactor_poll_cost", "reactor_round_trip", "tcp_concurrent"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mio", required=True)
    ap.add_argument("--runs", type=int, default=7)
    args = ap.parse_args()
    if platform.system() != "Linux" or platform.machine() not in ("x86_64", "AMD64"):
        print("NOT_RUN: native async benchmarks need linux-amd64")
        return 0
    with tempfile.TemporaryDirectory() as tmp:
        built = {}
        for name in ("bench", "baseline"):
            out = Path(tmp) / name
            subprocess.run([args.mio, "build", str(HERE / f"{name}.mio"), "--bunko-root", str(ROOT / "bunko"), "-o", str(out)], check=True)
            built[name] = out
        print(f"{'program':10} {'mode':22} {'work':>10} {'median ms':>10} {'min':>6} {'max':>6}")
        for name, modes in (("bench", MODES), ("baseline", ["call", "closure_call", "closure_create", "alloc5"])):
            for mode in modes:
                times, work, label = [], "", mode or "-"
                for _ in range(args.runs):
                    cmd = [str(built[name])] + ([mode] if mode else [])
                    r = subprocess.run(cmd, capture_output=True, text=True, timeout=300)
                    lines = r.stdout.split()
                    if r.returncode != 0 or len(lines) < 3:
                        print(f"FAIL {name} {label}: exit {r.returncode}"); return 1
                    work = lines[-2]; times.append(int(lines[-1]))
                print(f"{name:10} {label:22} {work:>10} {statistics.median(times):>10.0f} {min(times):>6} {max(times):>6}")
    return 0


sys.exit(main())
