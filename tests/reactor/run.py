#!/usr/bin/env python3
"""Native stress, load and signal tests for kiso/reactor on Linux x86-64 (real loopback sockets, real epoll).

The driver builds tests/reactor/tool.mio with the compiler under test (normal and --gc-stress) and runs it:
  * stress   repeated open/submit/poll/cancel/close rounds with garbage allocation while operations are pending, normal and
             --gc-stress, repeated to expose intermittent failures;
  * load     N loopback connections registered with one reactor; a timed idle phase must cost (almost) no CPU, then every
             connection becomes readable once and each readiness must be reported exactly once;
  * eintr    a poll loop interrupted by SIGSTOP/SIGCONT (Linux returns EINTR from epoll_wait): early returns are counted and
             must never surface as errors.
Measured numbers (wall time, CPU time from wait4, connection counts, poll counts) are printed and written to results.json; nothing
is estimated. On any other host the execution is printed as NOT_RUN and is not a pass.
"""
import argparse
import json
import os
from pathlib import Path
import platform
import resource
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
TOOL = ROOT / "tests/reactor/tool.mio"


def run(argv, timeout=900, stop_cont=None):
    start = time.monotonic()
    child = subprocess.Popen([str(a) for a in argv], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, cwd=ROOT)
    if stop_cont:
        time.sleep(stop_cont[0])
        os.kill(child.pid, signal.SIGSTOP)
        time.sleep(stop_cont[1])
        os.kill(child.pid, signal.SIGCONT)
    deadline = start + timeout
    while True:
        pid, status, usage = os.wait4(child.pid, os.WNOHANG)
        if pid:
            break
        if time.monotonic() > deadline:
            os.kill(child.pid, signal.SIGKILL)
            pid, status, usage = os.wait4(child.pid, 0)
            out, err = child.stdout.read(), child.stderr.read()
            return dict(code=-9, out=out, err=err + "\ntimeout", wall=time.monotonic() - start, cpu=usage.ru_utime + usage.ru_stime)
        time.sleep(0.01)
    out, err = child.stdout.read(), child.stderr.read()
    return dict(code=os.waitstatus_to_exitcode(status), out=out, err=err, wall=time.monotonic() - start, cpu=usage.ru_utime + usage.ru_stime)


def numbers(text):
    values = {}
    for line in text.splitlines():
        if "=" in line:
            name, _, value = line.partition("=")
            try:
                values[name.strip()] = int(value)
            except ValueError:
                pass
    return values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", required=True, type=Path)
    parser.add_argument("--output", type=Path, default=ROOT / "build/reactor-evidence")
    parser.add_argument("--repeats", type=int, default=20)
    parser.add_argument("--gc-repeats", type=int, default=5)
    parser.add_argument("--pairs", type=int, default=1000)
    parser.add_argument("--idle-ms", type=int, default=3000)
    args = parser.parse_args()
    host = (platform.system(), platform.machine())
    if host not in (("Linux", "x86_64"), ("Linux", "AMD64"), ("Linux", "aarch64"), ("Linux", "arm64"), ("Darwin", "arm64")):
        print("execution: NOT_RUN (the reactor backend exists on Linux x86-64, Linux arm64 and Darwin arm64; host is %s %s)" % host)
        return 0
    darwin = host[0] == "Darwin"
    args.output.mkdir(parents=True, exist_ok=True)
    results = dict(host=dict(system=platform.system(), machine=platform.machine(), kernel=platform.release(), cpus=os.cpu_count()), checks=[])
    failures = []

    def record(name, ok, **details):
        results["checks"].append(dict(name=name, status="PASS" if ok else "FAIL", **details))
        print("%-4s %-32s %s" % ("PASS" if ok else "FAIL", name, json.dumps(details, sort_keys=True)), flush=True)
        if not ok:
            failures.append(name)

    soft, hard = resource.getrlimit(resource.RLIMIT_NOFILE)
    wanted = 2 * args.pairs + 256
    if soft < wanted:
        resource.setrlimit(resource.RLIMIT_NOFILE, (min(hard, wanted), hard))
    soft, hard = resource.getrlimit(resource.RLIMIT_NOFILE)
    results["host"]["nofile_soft"] = soft
    normal, stressed = args.output / "reactor-tool", args.output / "reactor-tool-gcstress"
    for path, flags in ((normal, []), (stressed, ["--gc-stress"])):
        built = run([args.mio, "build", TOOL, "--bunko-root", ROOT / "bunko", "-o", path, *flags], timeout=600)
        record("build" + ("-gc-stress" if flags else ""), built["code"] == 0, detail=(built["err"] or built["out"])[:200])
        if built["code"] != 0:
            print(json.dumps(results, indent=1))
            return 1

    # stress, normal and under --gc-stress, repeated
    for label, exe, rounds, pairs, repeats in (("stress", normal, 30, 16, args.repeats), ("stress-gc", stressed, 8, 8, args.gc_repeats)):
        bad, walls, events = 0, [], 0
        for _ in range(repeats):
            r = run([exe, "stress", rounds, pairs], timeout=600)
            walls.append(round(r["wall"], 3))
            if r["code"] != 0 or "OK" not in r["out"]:
                bad += 1
                print(r["out"], r["err"])
            events += numbers(r["out"]).get("events", 0)
        record(label, bad == 0, repeats=repeats, rounds=rounds, pairs=pairs, readiness_events_total=events, failed_runs=bad, wall_seconds_min=min(walls), wall_seconds_max=max(walls))

    # load: idle CPU is the difference between a run with and without the idle phase
    if soft < wanted:
        record("load", False, reason="RLIMIT_NOFILE %d is below the %d descriptors needed" % (soft, wanted))
    else:
        base = run([normal, "load", args.pairs, 0], timeout=900)
        idle = run([normal, "load", args.pairs, args.idle_ms], timeout=900)
        n = numbers(idle["out"])
        ok = base["code"] == 0 and idle["code"] == 0 and "OK" in idle["out"] and n.get("handled") == args.pairs
        extra_cpu = idle["cpu"] - base["cpu"]
        efficient = extra_cpu < 0.1 * (args.idle_ms / 1000.0) and n.get("idle_polls", 99) <= 3 and n.get("idle_events", 1) == 0
        record("load-correctness", ok, connections=args.pairs, handled=n.get("handled"), stale=n.get("stale"), setup_ms=n.get("setup_ms"),
               burst_ms=n.get("burst_ms"), burst_polls=n.get("burst_polls"), nofile_soft=soft)
        record("load-idle-efficiency", ok and efficient, connections=args.pairs, idle_ms_requested=args.idle_ms, idle_ms_actual=n.get("idle_ms_actual"),
               idle_polls=n.get("idle_polls"), cpu_seconds_without_idle=round(base["cpu"], 3), cpu_seconds_with_idle=round(idle["cpu"], 3),
               extra_cpu_seconds_for_idle=round(extra_cpu, 3), wall_seconds_with_idle=round(idle["wall"], 3),
               rule="extra CPU for the idle period < 10% of its length, at most 3 polls, zero idle events")

    # EINTR: stop/continue makes epoll_wait return EINTR on Linux; it must come back as an early, error-free return. macOS may restart
    # kevent after SIGCONT, so there an early return is reported but not required (no error is still required).
    r = run([normal, "eintr", 2000], timeout=120, stop_cont=(0.4, 0.3))
    n = numbers(r["out"])
    record("eintr", r["code"] == 0 and "OK" in r["out"] and n.get("errors") == 0 and (darwin or n.get("early_returns", 0) >= 1),
           polls=n.get("polls"), early_returns=n.get("early_returns"), errors=n.get("errors"), wall_seconds=round(r["wall"], 3))

    (args.output / "results.json").write_text(json.dumps(results, indent=1) + "\n")
    print("execution: %s" % ("FAIL (%s)" % ", ".join(failures) if failures else "PASS (all checks)"))
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
