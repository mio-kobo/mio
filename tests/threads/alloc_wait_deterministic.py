#!/usr/bin/env python3
"""Run the Darwin ARM64 allocation-lock kernel witnesses and GC rendezvous."""
import argparse
import json
import os
import platform
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def execute(argv, tag, env=None, timeout=20):
    command = [str(arg) for arg in argv]
    try:
        result = subprocess.run(
            command, cwd=ROOT, env=dict(os.environ, **(env or {})),
            capture_output=True, timeout=timeout,
        )
        status, stdout, stderr = result.returncode, result.stdout, result.stderr
    except subprocess.TimeoutExpired as failure:
        status, stdout, stderr = None, failure.stdout or b"", failure.stderr or b""
    tag.with_suffix(".out").write_bytes(stdout)
    tag.with_suffix(".err").write_bytes(stderr)
    record = dict(command=command, environment=env or {}, exit=status, timeout_s=timeout)
    tag.with_suffix(".json").write_text(json.dumps(record, indent=2) + "\n")
    return status, stdout, stderr


def counters(stderr):
    return {
        key.removeprefix("mio-thread "): int(value)
        for line in stderr.decode().splitlines()
        for key, _, value in [line.partition("=")]
        if key.startswith("mio-thread ") and value.isdigit()
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--repeat", type=int, default=5)
    args = parser.parse_args()
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        print("NOT_RUN: Darwin ARM64 required")
        return 0
    if args.repeat < 1:
        parser.error("--repeat must be positive")
    work = args.output.resolve()
    work.mkdir(parents=True, exist_ok=True)

    kernel = work / "alloc-ulock-kernel"
    if execute(
        ["clang", "-O2", "-Wall", "-Wextra", "-Werror", "-std=c11",
         HERE / "alloc_ulock_deterministic.c", "-o", kernel],
        work / "kernel-build",
    )[0] != 0:
        return 1

    expected_kernel = (
        b"A post-advertisement mismatch\nB pre-syscall mismatch\n"
        b"C four kernel waiters\nD wake-all returned four waiters\n"
        b"G timeout across release and reacquire\nH EINTR and EINVAL\n"
    )
    cases = [
        ("kernel", kernel, 0, expected_kernel, {}),
        ("park", "epoch", 0, b"3\n73\n88\n", {"alloc_wait_test_phase": 3}),
        ("errno", "errno", 0, b"31\n", {"alloc_wait_error": 1}),
        ("owner", "errno", 90, b"", {}),
    ]
    passed = True
    for name, image, expected_status, expected_out, expected_counts in cases:
        if name != "kernel":
            source = HERE / f"alloc_wait_{image}.mio"
            image = work / name
            if execute(
                [args.mio.resolve(), "build", source, "--target=darwin-arm64", "--observe",
                 "-o", image],
                work / f"{name}-build", {"MIO_ALLOC_WAIT_TEST": name},
            )[0] != 0:
                passed = False
                continue

        for repeat in range(args.repeat):
            status, stdout, stderr = execute([image], work / f"{name}-{repeat}")
            ok = (status, stdout) == (expected_status, expected_out)
            if name in ("kernel", "owner"):
                ok = ok and not stderr
            else:
                values = counters(stderr)
                ok = ok and all(values.get(key) == value for key, value in expected_counts.items())
                if name == "errno":
                    ok = ok and values.get("alloc_wait_epoch_refused", 0) > 0
            print(f"{name} {repeat} {'PASS' if ok else 'FAIL'} exit={status}")
            passed = passed and ok
    return int(not passed)


if __name__ == "__main__":
    raise SystemExit(main())
