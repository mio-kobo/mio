#!/usr/bin/env python3
"""Verify the official project templates with a real Mio CLI.

Usage: verify.py --mio <path-to-mio> [--bunko-root <dir>]

Every template is copied to a temporary directory whose name contains a space and
Japanese characters, then built/run/tested with the given CLI. The script exits
non-zero if any step fails; nothing is skipped. Full distributions embed Bunko;
for a Core or source-built CLI pass --bunko-root (used for kiso/* imports).
"""
import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2] / "templates"

# (template, working directory, argv after "mio", expected stdout lines, needs bunko root for kiso)
STEPS = [
    ("hello", ".", ["run", "main.mio"], ["Hello, Mio!"], False),
    ("cli", ".", ["run", "main.mio"], ["usage: mio run main.mio -- <name>..."], True),
    ("cli", ".", ["run", "main.mio", "--", "Mio", "Mochi"], ["Mio", "Mochi"], True),
    ("multi-file", ".", ["run", "main.mio"], ["12"], False),
    ("library", ".", ["run", "main.mio"], ["10"], False),
    ("library", ".", ["test", "mathx", "--format=short"], ["mathx:test_clamp_inside_range: PASS", "mathx:test_clamp_outside_range: PASS", "Tests complete: 2 passed, 0 failed"], True),
    ("unit-test", ".", ["test", "tests", "--format=short"], ["tests:test_double: PASS", "tests:test_double_zero: PASS", "Tests complete: 2 passed, 0 failed"], True),
    ("stdlib-app", ".", ["run", "main.mio"], ["readme.mio", ".mio", "hai"], True),
]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", required=True)
    parser.add_argument("--bunko-root")
    args = parser.parse_args()
    failures = 0
    with tempfile.TemporaryDirectory(prefix="mio templates 日本語 ") as temporary:
        for name, cwd, argv, expected, needs_bunko in STEPS:
            target = Path(temporary) / name
            if not target.exists():
                shutil.copytree(ROOT / name, target)
            env = dict(os.environ)
            env.pop("MIO_BUNKO", None)
            if needs_bunko and args.bunko_root:
                env["MIO_BUNKO"] = args.bunko_root
            command = [args.mio, *argv]
            result = subprocess.run(command, cwd=target / cwd, env=env, capture_output=True, text=True, timeout=300)
            lines = result.stdout.splitlines()
            ok = result.returncode == 0 and all(any(line.startswith(want) for line in lines) for want in expected)
            print(("PASS" if ok else "FAIL"), name, " ".join(argv))
            if not ok:
                failures += 1
                print(f"  exit={result.returncode}\n  stdout={result.stdout!r}\n  stderr={result.stderr[:400]!r}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
