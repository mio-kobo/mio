#!/usr/bin/env python3
"""M19a Darwin ARM64 GC baseline. Fresh evidence only; no latency pass threshold.

Build once, sign once, warm up, then interleave independent native processes.
Latency includes process launch and exit diagnostics. It is NOT HTTP latency.
Use --baseline-ref to build the same fixtures with a frozen source revision.
"""
import argparse
import hashlib
import io
import json
import math
import os
from pathlib import Path
import platform
import random
import signal
import shutil
import subprocess
import tarfile
import time


BUCKETS = 64


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def percentile(samples, quantile):
    """Nearest rank, without interpolation or pretending small N is precise."""
    if not samples:
        return None
    return sorted(samples)[max(0, math.ceil(len(samples) * quantile) - 1)]


def parse_metrics(data):
    result = {}
    for line in data.decode("ascii").splitlines():
        if not line.startswith("mio-gc "):
            raise ValueError(f"unexpected stderr: {line!r}")
        key, sep, value = line[7:].partition("=")
        if not sep or not value.isdecimal() or key in result:
            raise ValueError(f"invalid or duplicate metric: {line!r}")
        result[key] = int(value)
        if result[key] > (1 << 64) - 1:
            raise ValueError(f"metric exceeds uint64: {key}")
    if not result:
        return result
    required = {"gc_count", "gc_last_ticks", "gc_total_ticks", "gc_max_ticks",
                "timer_frequency_hz", "runtime_ticks", "allocated_bytes",
                "allocated_objects", "heap_capacity", "heap_live_bytes",
                "heap_peak_live_bytes", "heap_cursor"} | {f"gc_pause_bucket_{i}" for i in range(BUCKETS)}
    if not required <= result.keys():
        raise ValueError(f"missing metrics: {sorted(required - result.keys())}")
    if sum(result[f"gc_pause_bucket_{i}"] for i in range(BUCKETS)) != result["gc_count"]:
        raise ValueError("pause histogram lost samples")
    if not (0 < result["timer_frequency_hz"] and
            result["gc_last_ticks"] <= result["gc_max_ticks"] <= result["gc_total_ticks"] <= result["runtime_ticks"]):
        raise ValueError("invalid GC clock accounting")
    los_live = result.get("los_live_bytes", 0)
    if los_live > result["heap_live_bytes"]:
        raise ValueError("LOS occupancy exceeds unified occupied bytes")
    if not (result["heap_live_bytes"] <= result["heap_peak_live_bytes"] <= result["heap_capacity"] and
            result["heap_peak_live_bytes"] <= result["allocated_bytes"] and
            result["heap_live_bytes"] - los_live <= result["heap_cursor"] <= result["heap_capacity"]):
        raise ValueError("invalid heap accounting")
    if "heap_committed" in result:
        memory_keys = {"heap_limit", "heap_goal", "heap_reserved", "pressure_level", "pressure_sequence", "pressure_transitions", "los_live_bytes", "los_reclaimed_bytes"}
        if not memory_keys <= result.keys():
            raise ValueError("incomplete M19c memory snapshot")
        if (result["heap_committed"] > min(result["heap_limit"], result["heap_reserved"]) or
            result["heap_goal"] > result["heap_capacity"] or result["pressure_level"] > 3 or result["pressure_sequence"] % 2):
            raise ValueError("invalid M19c memory budget/publication")
    histogram = [result[f"gc_pause_bucket_{i}"] for i in range(BUCKETS)]
    if result["gc_count"]:
        index = min(63, max(0, (max(1, result["gc_max_ticks"]) - 1).bit_length()))
        if histogram[index] == 0 or any(histogram[index + 1:]):
            raise ValueError("histogram and maximum disagree")
    elif result["gc_total_ticks"] or result["gc_max_ticks"] or result["gc_last_ticks"]:
        raise ValueError("pause reported without a collection")
    generation_fields = {"minor_gc_count", "major_gc_count", "young_live_bytes", "old_live_bytes",
                         "promoted_objects", "promoted_bytes", "promotion_threshold", "remembered_set_entries"}
    if generation_fields & result.keys():
        if not generation_fields <= result.keys():
            raise ValueError("missing generation metrics")
        for kind in ("minor", "major"):
            keys = {f"{kind}_gc_count", f"{kind}_gc_total_ticks", f"{kind}_gc_max_ticks"} | {f"{kind}_gc_pause_bucket_{i}" for i in range(BUCKETS)}
            if not keys <= result.keys():
                raise ValueError("missing generation metrics")
            if sum(result[f"{kind}_gc_pause_bucket_{i}"] for i in range(BUCKETS)) != result[f"{kind}_gc_count"]:
                raise ValueError("generation histogram lost samples")
            hist = [result[f"{kind}_gc_pause_bucket_{i}"] for i in range(BUCKETS)]
            maximum, total = result[f"{kind}_gc_max_ticks"], result[f"{kind}_gc_total_ticks"]
            if maximum > total:
                raise ValueError("invalid generation timing")
            if result[f"{kind}_gc_count"]:
                index = min(63, (max(1, maximum) - 1).bit_length())
                if not hist[index] or any(hist[index + 1:]):
                    raise ValueError("generation histogram and maximum disagree")
            elif maximum or total:
                raise ValueError("generation pause without collection")
        if max(result[f"{k}_gc_max_ticks"] for k in ("minor", "major")) != result["gc_max_ticks"]:
            raise ValueError("generation maximum does not match aggregate")
        for aggregate, suffix in (("gc_count", "count"), ("gc_total_ticks", "total_ticks")):
            if result[aggregate] != sum(result[f"{k}_gc_{suffix}"] for k in ("minor", "major")):
                raise ValueError("generation accounting does not sum")
        if result["young_live_bytes"] + result["old_live_bytes"] != result["heap_live_bytes"]:
            raise ValueError("generation live bytes do not sum")
        if not 2 <= result["promotion_threshold"] <= 8:
            raise ValueError("invalid promotion threshold")
        for i in range(BUCKETS):
            if result[f"gc_pause_bucket_{i}"] != sum(result[f"{k}_gc_pause_bucket_{i}"] for k in ("minor", "major")):
                raise ValueError("generation histograms do not sum")
    return result


def histogram_percentile_upper(histogram, quantile, frequency):
    count = sum(histogram)
    if not count:
        return None
    rank, seen = math.ceil(count * quantile), 0
    for i, n in enumerate(histogram):
        seen += n
        if seen >= rank:
            # Bucket 63 is an overflow bucket, so has no finite upper bound.
            return None if i == 63 else (1 << i) / frequency
    raise ValueError("invalid histogram")


def run_sample(entry, directory, label, timeout):
    """wait4 returns this child's peak RSS and CPU, never cumulative children."""
    out_path, err_path = directory / (label + ".stdout"), directory / (label + ".stderr")
    start = time.monotonic_ns()
    with out_path.open("xb") as out, err_path.open("xb") as err:
        child = subprocess.Popen([entry["path"]], stdout=out, stderr=err,
                                 cwd=directory, env={"PATH": ""}, start_new_session=True)
        expired = False
        reaped = False

        def deadline(signum, frame):
            raise TimeoutError("native workload deadline")

        previous_alarm = signal.signal(signal.SIGALRM, deadline)
        try:
            signal.setitimer(signal.ITIMER_REAL, timeout)
            try:
                _, status, usage = os.wait4(child.pid, 0)
                reaped = True
            except TimeoutError:
                expired = True
                os.killpg(child.pid, signal.SIGKILL)
                _, status, usage = os.wait4(child.pid, 0)
                reaped = True
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous_alarm)
            if reaped:
                # Popen must not wait again for the child already reaped here.
                child.returncode = os.waitstatus_to_exitcode(status)
            else:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
    elapsed = (time.monotonic_ns() - start) / 1e9
    stdout, stderr = out_path.read_bytes(), err_path.read_bytes()
    if expired or child.returncode != 0:
        raise RuntimeError(f"{label}: timeout={expired}, exit={child.returncode}; see {err_path}")
    if stdout != entry["expected_stdout"].encode():
        raise RuntimeError(f"{label}: incorrect output: {stdout!r}")
    metrics = parse_metrics(stderr)
    if entry["observation"] and entry["name"] != "compute" and not metrics:
        raise RuntimeError(f"{label}: missing observation")
    if not entry["observation"] and metrics:
        raise RuntimeError(f"{label}: observation disabled but stderr is nonempty")
    if metrics and metrics["heap_capacity"] != entry["heap_capacity"]:
        raise RuntimeError(f"{label}: unexpected capacity")
    if metrics and entry["name"] != "compute" and metrics["gc_count"] == 0:
        raise RuntimeError(f"{label}: fixture did not collect")
    return {"label": label, "wall_seconds": elapsed, "user_cpu_seconds": usage.ru_utime,
            "system_cpu_seconds": usage.ru_stime, "peak_rss_bytes": usage.ru_maxrss,
            "exit_code": child.returncode, "metrics": metrics,
            "stdout_sha256": hashlib.sha256(stdout).hexdigest(),
            "stderr_sha256": hashlib.sha256(stderr).hexdigest()}


def summarize(samples):
    wall = [s["wall_seconds"] for s in samples]
    result = {"samples": len(samples), "wall_seconds": {
        "p50": percentile(wall, .5), "p95": percentile(wall, .95), "p99": percentile(wall, .99)},
        "max_peak_rss_bytes": max(s["peak_rss_bytes"] for s in samples),
        "cpu_seconds_p50": percentile([s["user_cpu_seconds"] + s["system_cpu_seconds"] for s in samples], .5),
        "gc": None}
    metrics = [s["metrics"] for s in samples if s["metrics"]]
    if metrics:
        hz = metrics[0]["timer_frequency_hz"]
        if any(m["timer_frequency_hz"] != hz for m in metrics):
            raise ValueError("timer frequency changed")
        histogram = [sum(m[f"gc_pause_bucket_{i}"] for m in metrics) for i in range(BUCKETS)]
        total_ticks = sum(m["gc_total_ticks"] for m in metrics)
        runtime_ticks = sum(m["runtime_ticks"] for m in metrics)
        total_bytes = sum(m["allocated_bytes"] for m in metrics)
        result["gc"] = {
            "kind": "full_stw_mark_sweep", "count": sum(histogram), "pause_histogram": histogram,
            "timer_frequency_hz": hz,
            "pause_p50_upper_seconds": histogram_percentile_upper(histogram, .5, hz),
            "pause_p95_upper_seconds": histogram_percentile_upper(histogram, .95, hz),
            "pause_p99_upper_seconds": histogram_percentile_upper(histogram, .99, hz),
            "pause_max_seconds": max(m["gc_max_ticks"] for m in metrics) / hz,
            "total_pause_seconds": total_ticks / hz,
            "gc_wall_fraction": total_ticks / runtime_ticks if runtime_ticks else None,
            "allocation_bytes_per_runtime_second": total_bytes * hz / runtime_ticks if runtime_ticks else None,
            "max_occupied_heap_bytes": max(m["heap_peak_live_bytes"] for m in metrics),
            "max_end_occupied_heap_bytes": max(m["heap_live_bytes"] for m in metrics),
            "gc_cpu_percent": None}
        if "minor_gc_count" in metrics[0]:
            result["gc"]["kind"] = "non_moving_generational_stw"
            for kind in ("minor", "major"):
                hist = [sum(m[f"{kind}_gc_pause_bucket_{i}"] for m in metrics) for i in range(BUCKETS)]
                result["gc"][kind] = {
                    "count": sum(hist), "pause_histogram": hist,
                    "total_pause_seconds": sum(m[f"{kind}_gc_total_ticks"] for m in metrics) / hz,
                    "pause_max_seconds": max(m[f"{kind}_gc_max_ticks"] for m in metrics) / hz,
                    **{f"pause_p{q}_upper_seconds": histogram_percentile_upper(hist, q / 100, hz) for q in (50, 95, 99)}}
            result["gc"]["promotion_thresholds"] = sorted({m["promotion_threshold"] for m in metrics})
            for key in ("young_live_bytes", "old_live_bytes", "promoted_objects", "promoted_bytes", "remembered_set_entries"):
                result["gc"]["max_" + key] = max(m[key] for m in metrics)
    return result


def source_manifest(root):
    paths = set()
    for pattern in ("internal/**/*.go", "cmd/**/*.go", "bunko/**/*.mio", "selfhost/**/*.mio", "scripts/*.py", "go.mod", "go.sum"):
        paths.update(root.glob(pattern))
    return [{"path": str(p.relative_to(root)), "sha256": digest(p)} for p in sorted(paths)]


def freeze_revision(root, revision, destination):
    commit = subprocess.check_output(["git", "rev-parse", "--verify", revision + "^{commit}"], cwd=root, text=True).strip()
    data = subprocess.check_output(["git", "archive", "--format=tar", commit], cwd=root)
    destination.mkdir()
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        # Git-generated paths must still stay inside the snapshot directory.
        for member in archive.getmembers():
            path = Path(member.name)
            if path.is_absolute() or ".." in path.parts or member.issym() or member.islnk():
                raise ValueError(f"unsafe archive entry: {member.name}")
        archive.extractall(destination, filter="data")
    # Harness is copied verbatim and recorded separately from frozen sources.
    manifest = source_manifest(destination)
    harness = root / "internal/aot/m19_observation_test.go"
    (destination / "internal/aot/m19_observation_test.go").write_bytes(harness.read_bytes())
    return {"commit": commit, "git_archive_sha256": hashlib.sha256(data).hexdigest(),
            "source_manifest": manifest, "harness_sha256": digest(harness)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--m19c", action="store_true", help="include LOS and automatic-budget fixtures; run separately from historical comparison")
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--baseline-evidence", type=Path, help="reuse and verify retained M19a images for interleaved same-session comparison")
    parser.add_argument("--baseline-ref", help="frozen pre-M19 commit, benchmarked with the same harness and heap sizes")
    parser.add_argument("--m18-evidence", type=Path, help="archive existing M18 acceptance and verify its fixed-point images")
    parser.add_argument("--iterations", type=int, default=30)
    parser.add_argument("--warmups", type=int, default=2)
    parser.add_argument("--timeout", type=float, default=15)
    args = parser.parse_args()
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        parser.error("requires Darwin ARM64 (wait4 ru_maxrss is bytes here)")
    if args.iterations < 1 or args.warmups < 0 or args.timeout <= 0:
        parser.error("iterations/timeout must be positive and warmups nonnegative")
    if args.baseline_ref and args.baseline_evidence:
        parser.error("choose baseline-ref or baseline-evidence")
    if args.m19c and (args.baseline_ref or args.baseline_evidence):
        parser.error("run M19c specialized fixtures separately from the historical fixture comparison")
    root = Path(__file__).resolve().parent.parent
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=False)
    report = {"schema_version": 1, "status": "RUNNING", "platform": platform.platform(),
              "machine": platform.machine(), "logical_cpus": os.cpu_count(),
              "physical_memory_bytes": int(subprocess.check_output(["/usr/sbin/sysctl", "-n", "hw.memsize"], text=True)),
              "cpu_model": subprocess.check_output(["/usr/sbin/sysctl", "-n", "machdep.cpu.brand_string"], text=True).strip(),
              "python_version": platform.python_version(),
              "go_version": subprocess.check_output(["go", "version"], text=True).strip(),
              "timestamp_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
              "iterations": args.iterations, "warmups": args.warmups, "shuffle_seed": 19,
              "latency_scope": "native process launch through exit, includes exit telemetry; not HTTP latency",
              "heap_scope": "occupied physical object spans including headers/padding; unreachable objects stay occupied until GC",
              "heap_committed_bytes": None, "http_p99": None, "queue_wait_p99": None,
              "rejected_requests": None, "minor_gc_pause_p99": None,
              "commands": [], "workloads": []}

    def save():
        (evidence / "report.json").write_text(json.dumps(report, indent=2) + "\n")

    def command(label, argv, cwd=root, env=None):
        record = {"label": label, "argv": [str(a) for a in argv], "cwd": str(cwd)}
        report["commands"].append(record)
        save()
        with (evidence / (label + ".stdout")).open("xb") as out, (evidence / (label + ".stderr")).open("xb") as err:
            proc = subprocess.run(argv, cwd=cwd, env=env, stdout=out, stderr=err, timeout=600)
        record["exit_code"] = proc.returncode
        save()
        if proc.returncode:
            raise RuntimeError(f"{label} exit {proc.returncode}; see evidence logs")

    save()
    try:
        report["source_manifest"] = source_manifest(root)
        report["git_head"] = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
        command("git-status", ["git", "status", "--short"])
        command("git-diff", ["git", "diff", "--binary"])
        if args.m18_evidence:
            old = args.m18_evidence.resolve()
            archive = evidence / "m18"; archive.mkdir()
            for name in ("acceptance.json", "provenance.json", "source-manifest.json", "SHA256SUMS"):
                (archive / name).write_bytes((old / name).read_bytes())
            acceptance = json.loads((old / "acceptance.json").read_text())
            expected = acceptance["unsigned_fixed_point_sha256"]
            if acceptance["status"] != "M18_PASS" or any(digest(old / f"stage{i}.unsigned") != expected for i in range(1, 6)):
                raise ValueError("M18 acceptance or fixed-point hashes invalid")
            report["m18"] = {"evidence": str(old), "unsigned_fixed_point_sha256": expected,
                             "acceptance_sha256": digest(old / "acceptance.json")}
        builds = [("current", root)]
        if args.baseline_ref:
            baseline = evidence / "baseline-source"
            report["baseline"] = freeze_revision(root, args.baseline_ref, baseline)
            builds.insert(0, ("baseline", baseline))
            if args.m18_evidence:
                frozen = {f["path"]: f["sha256"] for f in report["baseline"]["source_manifest"]}
                old_manifest = json.loads((args.m18_evidence / "source-manifest.json").read_text())
                mismatches = []
                for old_file in old_manifest:
                    relative = next((p for p in frozen if old_file["path"].endswith("/" + p)), None)
                    if relative is None or frozen[relative] != old_file["sha256"]:
                        mismatches.append(relative or old_file["path"])
                report["m18"]["baseline_source_comparison"] = {
                    "entries_checked": len(old_manifest), "mismatches": mismatches,
                    "note": "Source revision may include later CLI changes; all differences are explicit."}

        entries = []
        if args.baseline_evidence:
            old = args.baseline_evidence.resolve()
            previous = json.loads((old / "report.json").read_text())
            if previous["status"] != "PASS":
                raise ValueError("baseline evidence did not pass")
            images = evidence / "baseline-images"; images.mkdir()
            retained = [e for e in previous["workloads"] if e["revision"] == "current"]
            report["baseline"] = {"evidence": str(old), "report_sha256": digest(old / "report.json"),
                                  "source_manifest": previous["source_manifest"], "images": []}
            for entry in retained:
                original = Path(entry["path"])
                if digest(original) != entry["sha256"]:
                    raise ValueError("retained baseline image hash mismatch")
                target = images / original.name
                shutil.copy2(original, target)
                command("baseline-verify-" + original.stem, ["/usr/bin/codesign", "--verify", "--strict", target])
                copied = {k: entry[k] for k in ("name", "engine", "observation", "heap_capacity", "expected_stdout", "sha256")}
                copied.update(revision="baseline", path=str(target))
                entries.append(copied)
                report["baseline"]["images"].append({"original": str(original), "sha256": entry["sha256"]})
            for fixture in (old / "current-images").glob("*.mochi"):
                shutil.copy2(fixture, images / fixture.name)
        for revision, source in builds:
            images = evidence / (revision + "-images")
            env = dict(os.environ, MIO_M19C_BENCH="1" if args.m19c else "0", MIO_M19_BENCH_DIR=str(images), MIO_M19_BASELINE="1" if revision == "baseline" else "0")
            command(revision + "-build", ["go", "test", "./internal/aot", "-run", "^TestM19BuildBenchmarks$", "-count=1", "-v", "-timeout=8m"], source, env)
            for entry in json.loads((images / "manifest.json").read_text()):
                entry.update(revision=revision, sha256=digest(Path(entry["path"])))
                entries.append(entry)
        if args.baseline_ref or args.baseline_evidence:
            # The benchmark input bytes must agree across compiler revisions.
            for name in {e["name"] for e in entries}:
                if (evidence / "baseline-images" / (name + ".mochi")).read_bytes() != (evidence / "current-images" / (name + ".mochi")).read_bytes():
                    raise ValueError(f"baseline/current Mochi differs: {name}")
        raw_dir = evidence / "samples"; raw_dir.mkdir()
        rng = random.Random(19)
        measured = {e["path"]: [] for e in entries}
        report["samples"] = []
        for warmup in (True, False):
            rounds = args.warmups if warmup else args.iterations
            for round_index in range(rounds):
                order = entries.copy(); rng.shuffle(order)
                for entry in order:
                    label = f'{entry["revision"]}-{Path(entry["path"]).stem}-{"warmup" if warmup else "run"}-{round_index:03}'
                    sample = run_sample(entry, raw_dir, label, args.timeout)
                    sample.update(warmup=warmup, path=entry["path"])
                    report["samples"].append(sample)
                    if not warmup:
                        measured[entry["path"]].append(sample)
                save()
        for entry in entries:
            summary = summarize(measured[entry["path"]])
            snapshots = [sample["metrics"] for sample in measured[entry["path"]] if sample.get("metrics")]
            memory_fields = ("heap_goal", "heap_limit", "heap_committed", "heap_reserved", "los_live_bytes", "los_reclaimed_bytes", "pressure_level", "pressure_transitions", "pressure_sequence")
            if snapshots and "heap_committed" in snapshots[0]:
                summary["memory"] = {field: {"min": min(m[field] for m in snapshots), "max": max(m[field] for m in snapshots)} for field in memory_fields}
                summary["memory_scope"] = "conservative managed writable-page charge; physical residency measured separately by wait4 peak RSS"
            report["workloads"].append({**entry, **summary})
        # Reproducibility includes untracked source files through the manifest.
        if report["source_manifest"] != source_manifest(root):
            raise RuntimeError("source files changed during benchmark")
        report["status"] = "PASS"
        save()
        print(f'Native GC benchmark complete: {evidence / "report.json"}')
    except BaseException as error:
        report.update(status="FAILED", error=str(error))
        save()
        raise


if __name__ == "__main__":
    main()
