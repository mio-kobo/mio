#!/usr/bin/env python3
"""Independently runnable thread benchmark phases (benchmarks/threads/efficiency.mio) for darwin-arm64 and linux-amd64.

Every phase is built as its own program (the final `run();` line of efficiency.mio is replaced by one phase call, so workloads are byte-for-byte the
ones of the combined program) and run as its own process with its own budget: an unfinished phase cannot hide the others. A phase is COMPLETE only
if the process exited 0, was not killed by the budget, printed exactly the expected measurement line(s) and the work count matches. Partial output of a
timed-out phase is kept as partial evidence and never counted as a measurement.

  phases:   start_join fan_join pingpong mpmc alloc_1 alloc_2 alloc_4 alloc_s1 alloc_s4 mpmc_probe mpmc_long   (and `all`: the original combined program, original 240 s budget)
  examples: scripts/ci/thread_bench.py --mio build/mio --target darwin-arm64 --repeat 5 --output build/bench/candidate
            scripts/ci/thread_bench.py --mio build/mio-candidate --baseline-mio build/mio-baseline --phase mpmc --phase pingpong --repeat 5

--baseline-mio builds the same phases with a second compiler and ALTERNATES baseline and candidate runs (B C B C ...). The image is only executed when the
host can run it (darwin-arm64 on macOS ARM64, linux-amd64 on Linux x86-64); otherwise the phase is reported NOT_RUN after a successful build.
Exit status: 0 when every requested phase was COMPLETE (or NOT_RUN because of the host), 1 otherwise.
"""
import argparse
import hashlib
import json
import os
import platform
import resource
import statistics
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "benchmarks/threads/efficiency.mio"
# phase -> (source call, expected output name, expected work count, per-phase budget in seconds)
PHASES = {
    "start_join": ("start_join();", "start_join", 20000, 120),
    "fan_join": ("fan_join();", "fan_join", 6400, 120),
    "pingpong": ("pingpong();", "pingpong", 20000, 120),
    "mpmc": ("mpmc();", "mpmc", 400000, 240),          # 200000 sends + 200000 receives
    "alloc_1": ("alloc_scale(1);", "alloc_1", 2000000, 120),   # weak scaling: 2000000 loop iterations PER WORKER
    "alloc_2": ("alloc_scale(2);", "alloc_2", 4000000, 120),
    "alloc_4": ("alloc_scale(4);", "alloc_4", 8000000, 120),
    "mpmc_probe": ("mpmc_probe();", "mpmc_probe", None, 120),
    "alloc_s1": ("alloc_strong(1);", "alloc_s1", 4000000, 120),   # STRONG scaling: 4000000 iterations in total over 1 or 4 threads
    "alloc_s4": ("alloc_strong(4);", "alloc_s4", 4000000, 120),
    # the original reproducer program tests/threads/mpmc_long.mio, unchanged, with its own 120 s watchdog
    "mpmc_long": (None, "mpmc_long", None, 120),
}
FILES = {"mpmc_long": ROOT / "tests/threads/mpmc_long.mio"}
ORDER = ["start_join", "fan_join", "pingpong", "mpmc", "alloc_1", "alloc_2", "alloc_4"]
ALL_EXPECTED = {name: PHASES[name][2] for name in ORDER}
ALL_BUDGET = 240   # the budget of the original darwin-thread-bench run


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def host_target():
    system, machine = platform.system(), platform.machine().lower()
    if system == "Darwin" and machine in ("arm64", "aarch64"):
        return "darwin-arm64"
    if system == "Linux" and machine in ("x86_64", "amd64"):
        return "linux-amd64"
    return None


def build(mio, call, target, work, label, bunko_root, phase=None, symbols=False, observe=False, env=None):
    if phase in FILES:
        source = FILES[phase]
    else:
        text = SOURCE.read_text()
        marker = "\nrun();\n"
        if not text.endswith(marker):
            raise SystemExit("efficiency.mio must end with a bare `run();` line")
        source = work / (label + ".mio")
        source.write_text(text[:-len(marker)] + "\n" + call + "\n")
    image = work / label
    argv = [str(mio), "build", str(source), "--target=" + target, "-o", str(image)]
    if symbols and target == "darwin-arm64":
        argv += ["--emit-symbols", str(work / (label + ".sym"))]
    if observe and target == "darwin-arm64":
        argv += ["--observe"]
    if bunko_root:
        argv += ["--bunko-root", str(bunko_root)]
    done = subprocess.run(argv, cwd=ROOT, capture_output=True, text=True, env=dict(os.environ, **(env or {})))
    if done.returncode != 0 or not image.is_file():
        return None, (done.stderr or done.stdout)[-300:]
    os.chmod(image, 0o755)
    return image, ""


def run_once(image, budget, sample_after=0, sample_path=None):
    """Run one image under a budget with per-child resource usage from wait4 (user/system CPU, peak RSS, context switches of THIS process only, exact for
    runs that are killed at the budget too). The child gets its own process group so the budget kill reaches it. A run still alive after sample_after
    seconds (macOS only) gets a `sample` of all threads written to sample_path."""
    import signal, tempfile
    start = time.monotonic()
    with tempfile.TemporaryFile() as out_f, tempfile.TemporaryFile() as err_f:
        proc = subprocess.Popen([str(image)], stdout=out_f, stderr=err_f, start_new_session=True)
        sampled, timed_out, status, usage = None, False, None, None
        while True:
            pid, status, usage = os.wait4(proc.pid, os.WNOHANG)
            if pid:
                break
            elapsed = time.monotonic() - start
            if elapsed >= budget:
                timed_out = True
                os.killpg(proc.pid, signal.SIGKILL)
                _, status, usage = os.wait4(proc.pid, 0)
                break
            if sample_after and sampled is None and elapsed >= sample_after and sample_path and Path("/usr/bin/sample").exists():
                taken = subprocess.run(["/usr/bin/sample", str(proc.pid), "3", "1", "-mayDie", "-file", str(sample_path)], capture_output=True, text=True, timeout=120)
                sampled = taken.returncode == 0 and Path(sample_path).is_file()
                continue
            time.sleep(0.005)
        wall = time.monotonic() - start
        proc.returncode = 0
        out_f.seek(0)
        err_f.seek(0)
        out, err = out_f.read().decode(errors="replace"), err_f.read().decode(errors="replace")
    code = None if timed_out else os.waitstatus_to_exitcode(status)
    scale = 1024 if platform.system() == "Linux" else 1   # ru_maxrss is KiB on Linux, bytes on macOS
    return dict(exit=code, timed_out=timed_out, wall_s=round(wall, 3), user_s=round(usage.ru_utime, 3), system_s=round(usage.ru_stime, 3), stdout=out,
                stderr=err[-2000:], stderr_full=err, sampled=sampled, maxrss_bytes=usage.ru_maxrss * scale,
                voluntary_csw=usage.ru_nvcsw, involuntary_csw=usage.ru_nivcsw)


BOUNDS_NS = ["1us", "4us", "16us", "64us", "256us", "1ms", "4ms", "16ms", "inf"]   # upper bounds of the 9 observed wait buckets


def parse_observation(err):
    """`mio-thread name=value` and `mio-gc name=value` lines of an observed (--observe) image's stderr."""
    values = {}
    for line in err.splitlines():
        for tag in ("mio-thread ", "mio-gc "):
            if line.startswith(tag) and "=" in line:
                name, _, value = line[len(tag):].partition("=")
                if value.strip().isdigit():
                    values[("thread." if tag == "mio-thread " else "gc.") + name] = int(value)
    return values


def summarize_waits(values):
    """Per kind (heap_enter, poll, block_leave): calls, mean ns, and the upper bound of the bucket holding p50/p95/p99 (bucket resolution, not interpolated)."""
    result = {}
    for kind in ("heap_enter", "poll", "block_leave"):
        buckets = [values.get("thread.%s_bucket_%d" % (kind, i), 0) for i in range(9)]
        calls = sum(buckets)
        if not calls:
            continue
        def at(fraction):
            need, seen = fraction * calls, 0
            for index, count in enumerate(buckets):
                seen += count
                if seen >= need:
                    return BOUNDS_NS[index]
            return BOUNDS_NS[-1]
        result[kind] = dict(calls=calls, mean_ns=round(values.get("thread.%s_total_ns" % kind, 0) / calls), max_ns=values.get("thread.%s_max_ns" % kind, 0),
                            p50_bucket_upper=at(0.50), p95_bucket_upper=at(0.95), p99_bucket_upper=at(0.99), buckets=buckets)
    return result


def parse(out):
    rows = {}
    for line in out.splitlines():
        parts = line.split(",")
        if len(parts) != 3:
            return None
        try:
            rows[parts[0]] = (int(parts[1]), int(parts[2]))
        except ValueError:
            return None
    return rows


def judge(phase, result):
    """COMPLETE / PARTIAL / FAIL plus the measurement (ms, work) of a complete run."""
    rows = parse(result["stdout"])
    if result["timed_out"]:
        return "PARTIAL", None, "budget exceeded (censored: the true duration is longer than the budget); partial output %r" % result["stdout"][:200]
    if result["exit"] != 0:
        return "FAIL", None, "exit %s stderr %r" % (result["exit"], result["stderr"][:120])
    if phase == "mpmc_long":   # tests/threads/mpmc_long.mio prints the received and the sent count
        return ("COMPLETE", {}, "") if result["stdout"] == "200000\n200000\n" else ("FAIL", None, "wrong output %r" % result["stdout"][:80])
    if rows is None:
        return "FAIL", None, "unparsable output %r" % result["stdout"][:120]
    if phase == "all":
        if rows.keys() != ALL_EXPECTED.keys() or any(rows[n][1] != w for n, w in ALL_EXPECTED.items()):
            return "FAIL", None, "wrong phase set or work counts %r" % rows
        return "COMPLETE", rows, ""
    _, name, work, _ = PHASES[phase]
    if phase == "mpmc_probe":
        probe = rows.get("mpmc_probe")
        workers = sorted(n for n in rows if n != "mpmc_probe")
        if probe is None or len(workers) != 8:
            return "FAIL", None, "probe lines missing: %r" % list(rows)
        sent = sum(rows[n][0] for n in workers if n.startswith("probe_producer"))
        if sent != probe[1]:
            return "FAIL", None, "delivered %d != sent %d" % (probe[1], sent)
        return "COMPLETE", rows, ""
    if set(rows) != {name} or rows[name][1] != work or rows[name][0] < 0:
        return "FAIL", None, "wrong output %r (expected one line %s with work %d)" % (rows, name, work)
    return "COMPLETE", rows, ""


def summarize(values):
    if not values:
        return {}
    return dict(n=len(values), values=values, median=statistics.median(values), min=min(values), max=max(values),
                spread_pct=round(100.0 * (max(values) - min(values)) / statistics.median(values), 1) if statistics.median(values) else 0.0)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--mio", required=True, help="compiler under test")
    parser.add_argument("--baseline-mio", help="second compiler; baseline and candidate runs alternate")
    parser.add_argument("--target", default=host_target(), choices=("darwin-arm64", "linux-amd64"))
    parser.add_argument("--phase", action="append", choices=list(PHASES) + ["all"], help="repeatable; default: the seven benchmark phases")
    parser.add_argument("--repeat", type=int, default=3)
    parser.add_argument("--budget", type=int, help="override every phase budget (seconds); recorded in the report")
    parser.add_argument("--bunko-root", help="Bunko root for BOTH compilers (omit to use each compiler's embedded Bunko)")
    parser.add_argument("--sample-after", type=int, default=0, help="macOS: `sample` every run still alive after this many seconds into <output>/sample-*.txt")
    parser.add_argument("--observe", action="store_true", help="darwin-arm64: INSTRUMENTED runs (never timing): build with --observe, once with and once without bounded epoch retention "
                        "(MIO_OBSERVE_NO_RETENTION=1, same compiler), and report epoch entries, wait-bucket distributions, GC pause counters and peak RSS")
    parser.add_argument("--symbols", action="store_true", help="darwin-arm64: build with --emit-symbols (<output>/<phase>-<label>.sym) so samples can be attributed")
    parser.add_argument("--output", default=str(ROOT / "build/thread-bench"))
    args = parser.parse_args()
    if args.target is None:
        raise SystemExit("--target is required on this host")
    phases = args.phase or ORDER
    work = Path(args.output)
    work.mkdir(parents=True, exist_ok=True)
    runnable = host_target() == args.target
    contenders = [("candidate", args.mio)] + ([("baseline", args.baseline_mio)] if args.baseline_mio else [])
    contender_env = {}
    if args.observe:
        contenders = [("observed", args.mio), ("observed-noretention", args.mio)]
        contender_env = {"observed-noretention": {"MIO_OBSERVE_NO_RETENTION": "1"}}
    report = dict(host=dict(system=platform.system(), machine=platform.machine(), release=platform.release(), cpus=os.cpu_count()),
                  target=args.target, executed=runnable, repeat=args.repeat,
                  compilers={label: dict(path=str(path), sha256=sha256(path)) for label, path in contenders}, phases={})
    failed = False
    for phase in phases:
        call = "run();" if phase == "all" else PHASES[phase][0]
        budget = args.budget or (ALL_BUDGET if phase == "all" else PHASES[phase][3])
        entry = dict(budget_s=budget, expected_work=ALL_EXPECTED if phase == "all" else PHASES[phase][2], contenders={})
        images = {}
        for label, path in contenders:
            if phase == "all":   # the original combined program, unmodified
                image = work / ("all-" + label)
                done = subprocess.run([str(path), "build", str(SOURCE), "--target=" + args.target, "-o", str(image)] +
                                      (["--bunko-root", args.bunko_root] if args.bunko_root else []), cwd=ROOT, capture_output=True, text=True)
                image, why = (image, "") if done.returncode == 0 and image.is_file() else (None, (done.stderr or done.stdout)[-300:])
                if image:
                    os.chmod(image, 0o755)
            else:
                image, why = build(path, call, args.target, work, "%s-%s" % (phase, label), args.bunko_root, phase, args.symbols, args.observe, contender_env.get(label))
            if image is None:
                entry["contenders"][label] = dict(status="FAIL", reason="build failed: " + why)
                failed = True
                continue
            images[label] = image
            entry["contenders"][label] = dict(image_sha256=sha256(image), runs=[])
        if not runnable:
            for label in images:
                entry["contenders"][label]["status"] = "NOT_RUN"
                entry["contenders"][label]["reason"] = "host %s cannot execute a %s image" % (host_target(), args.target)
        else:
            for repeat in range(args.repeat):
                for label, _ in contenders:      # alternate: candidate, baseline, candidate, baseline ...
                    if label not in images:
                        continue
                    tag = "%s-%s-%d" % (phase, label, repeat)
                    result = run_once(images[label], budget, args.sample_after, work / ("sample-%s.txt" % tag))
                    (work / (tag + ".out")).write_text(result["stdout"])   # complete raw output of every run, also of failed and timed-out ones
                    (work / (tag + ".err")).write_text(result["stderr_full"])
                    status, rows, why = judge(phase, result)
                    extra = {}
                    if args.observe:   # instrumented run: epoch/wait/GC counters printed by the image itself at exit
                        values = parse_observation(result["stderr_full"])
                        extra = dict(observation=values, waits=summarize_waits(values), instrumented=True)
                        if not values and status == "COMPLETE":
                            status, why = "FAIL", "observed image printed no mio-thread/mio-gc counters"
                    entry["contenders"][label]["runs"].append(dict(status=status, reason=why, rows=rows, **extra, **{k: v for k, v in result.items() if k not in ("stdout", "stderr_full")}))
                    print("%-10s %-9s run %d: %-8s exit %s %.1fs %s" % (phase, label, repeat, status, result["exit"], result["wall_s"], why), flush=True)
            for label in images:
                runs = entry["contenders"][label]["runs"]
                complete = [r for r in runs if r["status"] == "COMPLETE"]
                entry["contenders"][label]["status"] = "COMPLETE" if len(complete) == len(runs) and runs else ("PARTIAL" if any(r["status"] == "PARTIAL" for r in runs) else "FAIL")
                if phase not in ("all", "mpmc_probe", "mpmc_long") and complete:
                    entry["contenders"][label]["elapsed_ms"] = summarize([r["rows"][PHASES[phase][1]][0] for r in complete])
                entry["contenders"][label]["wall_s"] = summarize([r["wall_s"] for r in runs])
                entry["contenders"][label]["cpu_s"] = summarize([round(r["user_s"] + r["system_s"], 3) for r in runs])
                if entry["contenders"][label]["status"] != "COMPLETE" and phase != "mpmc_probe":
                    failed = True
        report["phases"][phase] = entry
        (work / "report.json").write_text(json.dumps(report, indent=1))
    print("report: %s" % (work / "report.json"))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
