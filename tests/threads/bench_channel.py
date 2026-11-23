#!/usr/bin/env python3
"""Isolate waiter-aware channel wake cost on a real Linux x86-64 runner.
The same native compiler/program builds both variants; only thread.mio differs.
Python/Git are test dependencies, never native runtime dependencies.
"""
import argparse
import hashlib
import json
import os
import platform
from pathlib import Path
import shutil
import statistics
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
BEFORE = "9bfcc76faee57af87d0ae7d8fe06115d94ba5504"
BEFORE_SHA256 = "e20715983a3acf9fbd7347ad2006ffe4a842f8870580ea83643ebd5eb3056d34"
LIBRARY = "bunko/kiso/thread/thread.mio"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if platform.system() != "Linux" or platform.machine().lower() not in ("x86_64", "amd64"):
        print("NOT_RUN: Linux x86-64 native host required")
        return 2
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = dict(host=platform.platform(), cpus=os.cpu_count(), baseline_commit=BEFORE,
                  source_sha=subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                  compiler_sha256=hashlib.sha256(Path(args.mio).read_bytes()).hexdigest(), results=[])

    def save():
        (output / "results.json").write_text(json.dumps(report, indent=2) + "\n")

    def command(name, argv, timeout=180):
        result = subprocess.run(list(map(str, argv)), cwd=ROOT, capture_output=True, timeout=timeout)
        (output / (name + ".stdout")).write_bytes(result.stdout)
        (output / (name + ".stderr")).write_bytes(result.stderr)
        if result.returncode:
            raise RuntimeError(name + " exited " + str(result.returncode))
        return result.stdout

    before = subprocess.run(["git", "show", BEFORE + ":" + LIBRARY], cwd=ROOT, capture_output=True)
    if before.returncode:
        command("fetch-pinned-baseline", ["git", "fetch", "--no-tags", "--depth=1", "origin", BEFORE])
        before = subprocess.run(["git", "show", BEFORE + ":" + LIBRARY], cwd=ROOT, capture_output=True)
    if before.returncode or hashlib.sha256(before.stdout).hexdigest() != BEFORE_SHA256:
        raise RuntimeError("pinned baseline library unavailable or digest differs")
    before_root = output / "before-bunko"
    shutil.copytree(ROOT / "bunko", before_root)
    (before_root / "kiso/thread/thread.mio").write_bytes(before.stdout)
    report["library_sha256"] = dict(before=BEFORE_SHA256,
        after=hashlib.sha256((ROOT / LIBRARY).read_bytes()).hexdigest())
    report["program_sha256"] = hashlib.sha256((ROOT / "benchmarks/threads/channel_hot.mio").read_bytes()).hexdigest()
    images = {}
    for name, root in (("before", before_root), ("after", ROOT / "bunko")):
        image = output / name
        command("build-" + name, [args.mio, "build", ROOT / "benchmarks/threads/channel_hot.mio",
                "--target=linux-amd64", "--bunko-root", root, "-o", image])
        images[name] = image
    for repeat in range(3):
        for name in ("before", "after"):
            started = time.monotonic()
            raw = command("run-%s-%d" % (name, repeat), [images[name]])
            values = [int(value) for value in raw.splitlines()]
            if len(values) != 2 or values[0] < 0 or values[1] != 1999999000000:
                raise RuntimeError("benchmark output/work count differs: " + repr(raw))
            report["results"].append(dict(variant=name, repeat=repeat, milliseconds=values[0],
                                          seconds=time.monotonic() - started))
            save()
    report["median_ms"] = {name: statistics.median([r["milliseconds"] for r in report["results"] if r["variant"] == name])
                           for name in images}
    report["status"] = "PASS"
    save()
    print("PASS: Linux native Channel before/after, 3 runs each, 2000000 send/receive pairs; " + json.dumps(report["median_ms"]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
