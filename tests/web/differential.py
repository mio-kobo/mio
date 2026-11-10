#!/usr/bin/env python3
"""Web VM differential and Mochi compatibility checks.

Engines (never mixed up in the report):
  reference  the Go reference CLI (`go build ./cmd/mio`): `mio run`, `mio build`, `mio exec`
  web        the Web reference VM (build/web/mio.wasm) under Node, via tests/web/wasm_host.cjs
  native     the pure-Mio native CLI (`mio build --emit=mochi`), only as a Mochi producer

Checks:
  source/<case>        web `run` of the source == reference `mio run` (stdout, exit code, rendered diagnostics)
  check/<case>         web `check` == reference `mio check` (exit code, diagnostics)
  go-mochi/<case>      reference `mio build` bytes == web `compile` bytes; web `exec` == reference `mio exec`
  native-mochi/<case>  native `--emit=mochi` bytes: web `exec` == reference `mio exec`
  closed/<case>        sources that must be rejected by the browser's closed Bunko set
  malformed/<name>     corrupted Mochi: web and reference both reject with the same message; web never crashes

Every check is PASS or FAIL. A missing tool is a FAIL unless the caller explicitly omits that engine
(no --native-mio means the native-mochi group is not part of this run and is listed as NOT_RUN, which
fails the run unless --allow-not-run is given). Exit status is 0 only if every check passed.
"""
import argparse
import json
import os
import random
import shutil
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CASE_DIRS = [ROOT / "tests/web/cases", ROOT / "tests/native-differential/cases"]
# The reference CLI uses vm.DefaultInstructionLimit; the web VM is given the same budget here.
REFERENCE_LIMIT = 10_000_000


class Web:
    def __init__(self, build_dir, log):
        # Go runtime messages (including the expected fatal out-of-memory of limits/memory) go to the log.
        self.log = open(log, "w")
        self.proc = subprocess.Popen(["node", str(ROOT / "tests/web/wasm_host.cjs"), str(build_dir)],
                                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.log,
                                     text=True, encoding="utf-8")

    def call(self, request):
        request.setdefault("instructionLimit", REFERENCE_LIMIT)
        self.proc.stdin.write(json.dumps(request, ensure_ascii=False) + "\n")
        self.proc.stdin.flush()
        line = self.proc.stdout.readline()
        if not line:
            raise RuntimeError("wasm host exited")
        return json.loads(line)

    def close(self):
        self.proc.stdin.close()
        self.proc.wait(timeout=60)
        self.log.close()


def b64(data):
    import base64
    return base64.b64encode(data).decode()


def unb64(text):
    import base64
    return base64.b64decode(text or "")


def run(argv, cwd, env=None, timeout=600):
    p = subprocess.run([str(a) for a in argv], cwd=cwd, capture_output=True, timeout=timeout, env=env)
    return p.returncode, p.stdout.decode("utf-8", "replace"), p.stderr.decode("utf-8", "replace")


def rendered(result):
    return "".join(d["rendered"] for d in result.get("diagnostics", []))


class Report:
    def __init__(self):
        self.checks = []

    def add(self, name, ok, detail="", status=None):
        status = status or ("PASS" if ok else "FAIL")
        self.checks.append(dict(name=name, status=status, detail=detail))
        print("%-8s %s%s" % (status, name, (" -- " + detail) if detail and status != "PASS" else ""), flush=True)


def compare(report, name, ref, web, compare_stderr=True):
    code, out, err = ref
    problems = []
    if web.get("status") in ("internal_error", "host_error"):
        problems.append("web %s: %s" % (web.get("status"), web.get("error")))
    if web.get("exitCode") != code:
        problems.append("exit reference=%s web=%s" % (code, web.get("exitCode")))
    if web.get("stdout") != out:
        problems.append("stdout reference=%r web=%r" % (out[:200], web.get("stdout", "")[:200]))
    if compare_stderr and rendered(web) != err:
        problems.append("diagnostics reference=%r web=%r" % (err[:300], rendered(web)[:300]))
    report.add(name, not problems, "; ".join(problems))


def cases():
    found = []
    for directory in CASE_DIRS:
        for path in sorted(directory.glob("*.mio")):
            found.append(path)
    return found


def malformed_inputs(valid):
    """Deterministic corruptions of a valid Mochi file."""
    out = [("empty", b""), ("signature", b"MOCHA" + valid[5:]), ("short-header", valid[:6])]
    for version in (0, 17, 255, 0xFFFF):
        out.append(("version-%d" % version, valid[:5] + struct.pack(">H", version) + valid[7:]))
    for cut in sorted({7, 8, 11, len(valid) // 3, len(valid) // 2, len(valid) - 1}):
        out.append(("truncated-%d" % cut, valid[:cut]))
    out.append(("trailing-byte", valid + b"\x00"))
    out.append(("huge-constant-count", valid[:7] + struct.pack(">I", 0xFFFFFFF0) + valid[11:]))
    rng = random.Random(14)
    for index in range(48):
        data = bytearray(valid)
        for _ in range(rng.randint(1, 3)):
            position = rng.randrange(7, len(data))
            data[position] = rng.randrange(256)
        out.append(("bitflip-%02d" % index, bytes(data)))
    return out


BROWSER_CASES = ["enums", "unicode", "closures", "maps", "structs", "nullable", "functions",
                 "compile_type_error", "runtime_divide_by_zero"]


def write_fixtures(directory, work, native_mochi, capability_mochi, report):
    """Inputs for tests/web/browser: sources, reference results and Mochi files from both producers."""
    directory = directory.resolve()
    directory.mkdir(parents=True, exist_ok=True)
    index = []
    env = dict(os.environ, MIO_BUNKO=str(ROOT / "bunko"), MIO_LANG="en-US")
    for name in BROWSER_CASES:
        case_dir = work / name
        source = (case_dir / "main.mio").read_text(encoding="utf-8")
        code, out, err = run([REFERENCE[0], "run", "--color=never", "main.mio"], case_dir, env)
        entry = dict(name=name, source=source, exitCode=code, stdout=out, stderr=err)
        if (case_dir / "go.mochi").is_file():
            shutil.copy(case_dir / "go.mochi", directory / (name + ".go.mochi"))
            entry["goMochi"] = name + ".go.mochi"
        if name in native_mochi:
            (directory / (name + ".native.mochi")).write_bytes(native_mochi[name])
            entry["nativeMochi"] = name + ".native.mochi"
            code, out, err = run([REFERENCE[0], "exec", directory / (name + ".native.mochi")], case_dir, env)
            entry["nativeExec"] = dict(exitCode=code, stdout=out, stderr=err)
        index.append(entry)
    for origin, data in capability_mochi.items():
        (directory / ("capability.%s.mochi" % origin)).write_bytes(data)
    (directory / "bad-version.mochi").write_bytes(b"MOCHI\x00\x63" + bytes(16))
    (directory / "index.json").write_text(json.dumps(dict(cases=index, capability=sorted(capability_mochi)),
                                                     ensure_ascii=False, indent=2) + "\n")
    problems = [e["name"] for e in index if "nativeExec" in e and e["nativeExec"]["stderr"].startswith("mio: decode")]
    report.add("fixtures/written", not problems, "reference could not read native fixtures: %s" % problems)


REFERENCE = []


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--web", default=str(ROOT / "build/web"), help="static build directory with mio.wasm")
    parser.add_argument("--reference-mio", required=True, help="Go reference CLI (go build ./cmd/mio)")
    parser.add_argument("--native-mio", default="", help="pure-Mio native CLI used as a Mochi producer")
    parser.add_argument("--output", default=str(ROOT / "build/web-report/differential.json"))
    parser.add_argument("--fixtures", default="", help="also write browser test fixtures (Mochi files and reference results) here")
    parser.add_argument("--allow-not-run", action="store_true", help="local runs without the native CLI only")
    args = parser.parse_args()

    report = Report()
    reference = Path(args.reference_mio).resolve()
    REFERENCE.append(reference)
    native = Path(args.native_mio).resolve() if args.native_mio else None
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    web = Web(Path(args.web).resolve(), output.with_name("wasm-host.log"))
    # Render the CLI in the playground renderer's default locale for byte comparisons.
    env = dict(os.environ, MIO_BUNKO=str(ROOT / "bunko"), MIO_LANG="en-US")
    work = Path(tempfile.mkdtemp(prefix="mio-web-diff-"))
    try:
        native_mochi = {}
        for path in cases():
            name = path.stem if path.parent.name == "cases" and path.parent.parent.name == "web" else "nd-" + path.stem
            source = path.read_text(encoding="utf-8")
            # Both engines see the program as main.mio so the rendered diagnostics are comparable.
            case_dir = work / name
            case_dir.mkdir()
            (case_dir / "main.mio").write_text(source, encoding="utf-8")

            ref_run = run([reference, "run", "--color=never", "main.mio"], case_dir, env)
            compare(report, "source/" + name, ref_run, web.call({"operation": "run", "source": source}))

            code, out, err = run([reference, "check", "--color=never", "main.mio"], case_dir, env)
            web_check = web.call({"operation": "check", "source": source})
            ok = web_check["exitCode"] == code and (code != 0 or web_check["status"] == "checked") \
                and (code == 0 or rendered(web_check) == err) and web_check["stdout"] == ""
            report.add("check/" + name, ok, "exit reference=%s web=%s %r" % (code, web_check["exitCode"], rendered(web_check)[:200]))

            code, _, err = run([reference, "build", "-o", "go.mochi", "main.mio"], case_dir, env)
            web_compile = web.call({"operation": "compile", "source": source})
            if code == 0:
                go_bytes = (case_dir / "go.mochi").read_bytes()
                same = unb64(web_compile.get("mochi")) == go_bytes
                report.add("go-mochi/%s/bytes" % name, same, "web compile bytes differ from mio build" if not same else "")
                ref_exec = run([reference, "exec", "go.mochi"], case_dir, env)
                compare(report, "go-mochi/%s/exec" % name, ref_exec, web.call({"operation": "exec", "mochi": b64(go_bytes)}))
            else:
                report.add("go-mochi/%s/rejected" % name, web_compile["status"] == "diagnostic" and web_compile["exitCode"] == 1,
                           "reference build failed but web compile gave %s" % web_compile["status"])

            if native:
                code, out, err = run([native, "build", "main.mio", "--emit=mochi", "-o", "native.mochi"], case_dir, env)
                if code != 0:
                    # Compile errors: the native CLI must agree that the program is invalid.
                    report.add("native-mochi/%s/rejected" % name, ref_run[0] != 0 and web_compile["status"] == "diagnostic",
                               "native CLI rejected a program the reference accepts: %s" % (err or out)[:200])
                    continue
                data = (case_dir / "native.mochi").read_bytes()
                native_mochi[name] = data
                ref_exec = run([reference, "exec", "native.mochi"], case_dir, env)
                web_exec = web.call({"operation": "exec", "mochi": b64(data)})
                compare(report, "native-mochi/%s/exec" % name, ref_exec, web_exec)
                # The native front end and the reference front end must agree on observable behaviour.
                same = ref_exec[0] == ref_run[0] and ref_exec[1] == ref_run[1]
                report.add("native-mochi/%s/matches-source-run" % name, same,
                           "native Mochi exec differs from reference source run: %r vs %r" % (ref_exec[1][:200], ref_run[1][:200]))
            else:
                report.add("native-mochi/" + name, False, "no --native-mio given", status="NOT_RUN")

        # Host capabilities: the reference CLI really performs the call; the web VM must deny it.
        capability_mochi = {}
        for path in sorted((ROOT / "tests/web/capability").glob("*.mio")):
            case_dir = work / ("cap-" + path.stem)
            case_dir.mkdir()
            shutil.copy(path, case_dir / "main.mio")
            producers = [("go", [reference, "build", "-o", "cap.mochi", "main.mio"])]
            if native:
                producers.append(("native", [native, "build", "main.mio", "--emit=mochi", "-o", "cap.mochi"]))
            for origin, argv in producers:
                code, out, err = run(argv, case_dir, env)
                if code != 0:
                    report.add("capability/%s/%s" % (origin, path.stem), False, "build failed: " + (err or out)[:200])
                    continue
                data = (case_dir / "cap.mochi").read_bytes()
                capability_mochi[origin] = data
                result = web.call({"operation": "exec", "mochi": b64(data)})
                ok = result["status"] == "runtime_error" and "host capability is not available" in result.get("error", "") \
                    and result["stdout"] == "before\n"
                report.add("capability/%s/%s" % (origin, path.stem), ok, "%s %r" % (result["status"], result.get("error", "")[:200]))

        for path in sorted((ROOT / "tests/web/closed").glob("*.mio")):
            result = web.call({"operation": "run", "source": path.read_text(encoding="utf-8")})
            ok = result["status"] == "diagnostic" and result["stdout"] == "" and "unsupported Bunko package" in result.get("error", "")
            report.add("closed/" + path.stem, ok, "%s: %s" % (result["status"], result.get("error", "")[:200]))

        seeds = {"go": (work / "closures" / "go.mochi").read_bytes()}
        if "closures" in native_mochi:
            seeds["native"] = native_mochi["closures"]
        for origin, valid in seeds.items():
            for label, data in malformed_inputs(valid):
                target = work / ("bad-%s-%s.mochi" % (origin, label))
                target.write_bytes(data)
                code, out, err = run([reference, "exec", target], work, env)
                result = web.call({"operation": "exec", "mochi": b64(data)})
                problems = []
                if result["status"] in ("internal_error", "host_error"):
                    problems.append("web crashed: %s" % result.get("error"))
                if result["exitCode"] != code or result["stdout"] != out:
                    problems.append("exit/stdout reference=%s %r web=%s %r" % (code, out[:80], result["exitCode"], result["stdout"][:80]))
                if err.startswith("mio: decode: "):
                    if result["status"] != "rejected" or "mio: decode: " + result.get("error", "") + "\n" != err:
                        problems.append("reference %r web %s %r" % (err.strip()[:160], result["status"], result.get("error", "")[:160]))
                elif code != 0 and rendered(result) != err:
                    problems.append("runtime diagnostics differ: %r vs %r" % (err[:160], rendered(result)[:160]))
                report.add("malformed/%s/%s" % (origin, label), not problems, "; ".join(problems))

        oversized = web.call({"operation": "exec", "mochi": b64(b"MOCHI" + bytes(4 * 1024 * 1024))})
        report.add("limits/mochi-size", oversized["status"] == "rejected" and "limit" in oversized.get("error", ""), str(oversized.get("error")))
        oversized = web.call({"operation": "run", "source": "chant(1);" + " " * (256 * 1024)})
        report.add("limits/source-size", oversized["status"] == "rejected" and "source limit" in oversized.get("error", ""), str(oversized.get("error")))
        flood = web.call({"operation": "run", "source": 'aida (hai) { chant("0123456789abcdef0123456789abcdef"); }'})
        report.add("limits/stdout", flood["status"] == "runtime_error" and len(flood["stdout"].encode()) == 1024 * 1024
                   and "output limit exceeded" in flood.get("error", ""), "%s %d" % (flood["status"], len(flood["stdout"])))
        budget = web.call({"operation": "run", "source": "aida (hai) {}", "instructionLimit": 1000})
        report.add("limits/instructions", budget["status"] == "runtime_error" and "instruction limit 1000 exceeded" in budget.get("error", ""),
                   str(budget.get("error")))
        excessive = web.call({"operation": "run", "source": "chant(1);", "instructionLimit": 3_000_000_000})
        report.add("limits/instruction-cap", excessive["status"] == "rejected", str(excessive.get("error")))
        hog = web.call({"operation": "run", "source": (ROOT / "tests/web/limits/memory_exhaustion.mio").read_text()})
        after = web.call({"operation": "run", "source": 'chant("fresh");'})
        report.add("limits/memory", hog["status"] == "host_error" and "Go runtime exited" in hog.get("error", "")
                   and after["stdout"] == "fresh\n", "%s / %s" % (hog.get("status"), after.get("stdout")))
        unknown = web.call({"operation": "shell", "source": "chant(1);"})
        report.add("limits/operation", unknown["status"] == "rejected", str(unknown.get("error")))
        if args.fixtures:
            write_fixtures(Path(args.fixtures), work, native_mochi, capability_mochi, report)
    finally:
        web.close()
        shutil.rmtree(work, ignore_errors=True)

    failed = [c for c in report.checks if c["status"] == "FAIL"]
    not_run = [c for c in report.checks if c["status"] == "NOT_RUN"]
    status = "FAIL" if failed or (not_run and not args.allow_not_run) else "PASS"
    output.write_text(json.dumps(dict(suite="web-differential", status=status, checks=report.checks,
                                      passed=len(report.checks) - len(failed) - len(not_run),
                                      failed=len(failed), not_run=len(not_run)), indent=2, ensure_ascii=False) + "\n")
    print("%s: %d checks, %d failed, %d not run" % (status, len(report.checks), len(failed), len(not_run)))
    return 0 if status == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
