#!/usr/bin/env python3
"""Alternate baseline and candidate Darwin FFI measurements on Apple Silicon."""

import argparse
import hashlib
import json
from pathlib import Path
import platform
import statistics
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def command(argv):
    return subprocess.check_output(list(map(str, argv)), cwd=ROOT, text=True, timeout=180)


def run_case(name, executable, libraries):
    lines = command([executable, *libraries]).splitlines()
    if lines[0] != "dylib loader: ok" or int(lines[1]) != 100000:
        raise ValueError(f"{name} legacy benchmark returned an unexpected transcript")
    return {"legacy_call_100000_ms": int(lines[3])}


def summarize(samples):
    return {name: {"samples": [row[name] for row in samples],
                   "median": statistics.median(row[name] for row in samples),
                   "variance": statistics.variance(row[name] for row in samples)}
            for name in samples[0]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-report", required=True, type=Path,
                        help="report.json from the original native dylib suite on this host")
    parser.add_argument("--mio", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--repetitions", default=9, type=int)
    args = parser.parse_args()
    if args.repetitions < 3 or platform.system() != "Darwin" or platform.machine() != "arm64":
        parser.error("use Apple Silicon and at least three repetitions")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    mio = args.mio.resolve()
    baseline = json.loads(args.baseline_report.read_text(encoding="utf-8"))
    base_compiler = Path(baseline["checks"]["math-build"]["command"][0])
    if baseline["host"] != "Darwin arm64" or digest(base_compiler) != baseline["mio_sha256"]:
        raise ValueError("the baseline report and compiler identity differ")
    base_dir = args.baseline_report.resolve().parent / "artifacts"
    base_libraries = [base_dir / f"libmio_{name}.dylib" for name in ("math", "convert", "json")]
    base_libraries.append(base_dir / "libdata.dylib")
    candidate = output / "candidate-dylib"
    command(["python3", ROOT / "tests/ffi/dylib/run.py", "--mio", mio, "--embedded-bunko", "--output", candidate])
    built = candidate / "artifacts"
    libraries = [built / artifact.name for artifact in base_libraries]
    checked = output / "checked"
    raw = output / "raw"
    workers = output / "workers"
    command([mio, "build", HERE / "checked.mio", "-o", checked])
    command([mio, "build", HERE / "workers.mio", "-o", workers])
    command(["xcrun", "clang", "-arch", "arm64", "-mmacosx-version-min=11.0",
             "-Wall", "-Wextra", "-Werror", "-O2", HERE / "raw.c", "-o", raw])
    results = {"baseline": [], "candidate": []}
    for sample in range(args.repetitions):
        order = ("baseline", "candidate") if sample % 2 == 0 else ("candidate", "baseline")
        for name in order:
            directory, dylibs, compiler = ((base_dir, base_libraries, base_compiler) if name == "baseline"
                                          else (built, libraries, mio))
            row = run_case(name, directory / "loader", dylibs)
            tiny = output / f"lib{name}.dylib"
            started = time.perf_counter_ns()
            command([compiler, "build", "--kind=dylib", ROOT / "tests/ffi/dylib/math.mio",
                     "--export=add=mio_math_add", "-o", tiny])
            row["one_export_build_ns"] = time.perf_counter_ns() - started
            row["one_export_bytes"] = tiny.stat().st_size
            if name == "candidate":
                for program in (checked, workers):
                    lines = command([program, dylibs[0]]).splitlines()
                    row.update(dict(zip(lines[::2], map(int, lines[1::2]), strict=True)))
                row["native_reference_call_10000"] = int(command([raw, dylibs[0]]))
            results[name].append(row)
    integration = json.loads((candidate / "report.json").read_text(encoding="utf-8"))
    data = {
        "blocking_worker_tests_ns": {
            name: integration["checks"][name]["duration_ns"] for name in ("threaded-run", "threaded-gc-run")
        },
        "host": platform.platform(),
        "model": command(["sysctl", "-n", "hw.model"]).strip(),
        "sdk": command(["xcrun", "--sdk", "macosx", "--show-sdk-version"]).strip(),
        "clang": command(["xcrun", "clang", "--version"]).splitlines()[0],
        "repetitions": args.repetitions,
        "baseline_commit": baseline["source_commit"],
        "candidate_commit": command(["git", "rev-parse", "HEAD"]).strip(),
        "compiler_sha256": {"baseline": digest(base_compiler), "candidate": digest(mio)},
        "binary_sha256": {"baseline_loader": digest(base_dir / "loader"),
                          "candidate_loader": digest(built / "loader"), "checked": digest(checked),
                          "raw": digest(raw), "workers": digest(workers)},
        "timing": {name: summarize(rows) for name, rows in results.items()},
        "notes": ["Timings are nanoseconds unless a field ends in _ms.",
                  "First load is the first load of the image in a fresh process. The filesystem cache is warm.",
                  "The unchecked Clang host is a reference measurement. It bypasses Mio ticket checks.",
                  "The managed round trip copies 64 KiB in each direction.",
                  "Worker samples include start and join as well as 1,024 checked calls per worker.",
                  "Blocking worker tests include the full integration workload and process startup."],
    }
    (output / "performance.json").write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
