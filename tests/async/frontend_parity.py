#!/usr/bin/env python3
"""Frontend parity for hidouki / matsu: the Go reference frontend and the self-hosted frontend must agree with an independently written
expectation (first line of every corpus file: `// expect: accept` or `// expect: <code> <line>:<column>`) and with each other.

Both frontends are exercised through `mio check --format=json`: the Go reference CLI (built from ./cmd/mio) and the native self-hosted CLI
(--mio). Compared per program: accepted / rejected, the first diagnostic code, its source span (start and end offsets).
Result: PASS, FAIL, NOT_RUN (a frontend is unavailable). Nothing is converted into SKIP.
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]


def expectation(path):
    first = path.read_text().splitlines()[0]
    assert first.startswith("// expect: "), path
    parts = first[len("// expect: "):].split()
    if parts[0] == "accept":
        return ("accept", None, None)
    line, column = parts[1].split(":")
    return ("reject", parts[0], (int(line), int(column)))


def first_diagnostic(stdout):
    for line in stdout.splitlines():
        line = line.strip()
        if line.startswith("{"):
            try:
                return json.loads(line)
            except ValueError:
                continue
    return None


def run_check(command, cwd, source):
    proc = subprocess.run(command + [str(source)], cwd=cwd, capture_output=True, text=True, timeout=300)
    diag = first_diagnostic(proc.stdout + "\n" + proc.stderr)
    if proc.returncode == 0 and diag is None:
        return {"accepted": True}
    if diag is None:
        return {"accepted": False, "code": "NO_DIAGNOSTIC", "raw": (proc.stdout + proc.stderr)[:400]}
    span = diag["labels"][0]["span"] if diag.get("labels") else None
    return {
        "accepted": False,
        "code": diag["code"],
        "line": diag["location"]["line"],
        "column": diag["location"]["column"],
        "span": (span["start"]["offset"], span["end"]["offset"]) if span else None,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mio", help="native self-hosted mio CLI (linux-amd64)")
    parser.add_argument("--bunko", default=str(ROOT / "bunko"))
    parser.add_argument("--output", default=None)
    args = parser.parse_args()
    results = []
    failures = 0
    with tempfile.TemporaryDirectory() as tmp:
        go_bin = os.path.join(tmp, "mio-go")
        subprocess.run(["go", "build", "-o", go_bin, "./cmd/mio"], cwd=ROOT, check=True)
        frontends = [("go", [go_bin, "check", "--format=json"], ROOT)]
        if args.mio:
            frontends.append(("selfhost", [args.mio, "check", "--format=json", "--bunko-root", args.bunko, "--target=linux-amd64"], HERE / "frontend"))
        corpus = sorted((HERE / "frontend").glob("*.mio"))
        for path in corpus:
            kind, code, at = expectation(path)
            row = {"file": path.name, "expected": [kind, code, at]}
            for name, command, cwd in frontends:
                outcome = run_check(command, cwd, path)
                row[name] = outcome
                ok = outcome["accepted"] == (kind == "accept")
                if ok and kind == "reject":
                    ok = outcome["code"] == code and (outcome["line"], outcome["column"]) == at
                row[name + "_ok"] = ok
                if not ok:
                    failures += 1
                    print("FAIL %s [%s]: expected %s %s %s, got %s" % (path.name, name, kind, code, at, outcome))
            if len(frontends) == 2:
                g, s = row["go"], row["selfhost"]
                same = g["accepted"] == s["accepted"] and (g["accepted"] or (g["code"], g["span"]) == (s["code"], s["span"]))
                row["frontends_agree"] = same
                if not same:
                    failures += 1
                    print("FAIL %s: frontends disagree: go=%s selfhost=%s" % (path.name, g, s))
            results.append(row)
    status = "PASS" if failures == 0 else "FAIL"
    if not args.mio:
        status = status + " (go reference only; selfhost NOT_RUN)" if failures == 0 else status
    print("%s: %d programs, %d failures" % (status, len(results), failures))
    if args.output:
        Path(args.output).write_text(json.dumps({"status": status, "results": results}, indent=1, default=str))
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
