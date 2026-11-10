#!/usr/bin/env python3
"""Native verification of the pure-Mio toolchain on darwin-arm64, linux-amd64 and windows-amd64.

Engines are recorded separately for native execution, build and PE inspection, the Go reference VM
("reference") and the Go-hosted bootstrap host ("go-hosted"). Statuses are PASS, FAIL, SKIP, NOT_RUN and NOT_APPLICABLE
(a check that does not exist on that platform, with the reason); only PASS counts as passed.

  linux    Stage A of the native compiler is lowered by the pure-Mio backend running in the Go reference VM (go-hosted,
           NOT a trusted Native Seed). Native A->B->C->D compiler and CLI fixed points, native `mio test`, Runtime ABI
           contract (default and --gc-stress) with the VM differential, then the darwin-arm64 and windows-amd64 Stage A
           CLIs are cross-built by the native Linux CLI (twice, by two generations, which must agree).
  darwin   On macos-15. Stage A = the cross-built CLI (SHA-256 checked against the Linux manifest). Native B->C->D,
  windows  native `mio test`, Runtime ABI contract and differential. A == B (cross build equals native build) is required.
           Windows also runs the two TCP echo examples as separate native processes.
  summary  Combines the platform reports; fails unless every required check is PASS.

This candidate suite does not use the trusted GF4 seed. Stable distribution verifies it separately.
Python only starts processes, compares bytes and writes JSON; it generates no code.
"""
import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import struct
import subprocess
import sys
import time
from pathlib import Path

import examples_native

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tests/windows-amd64"))
from verify_pe import verify_winsock

sys.path.insert(0, str(ROOT / "scripts/distribution"))
from seed_evidence import GF4_SEED_SHA256
from prepare import EMPTY

STARTED = time.monotonic()
# Wall-clock budget for one platform (below the job timeout) so a slow host still uploads a complete report.
BUDGET_SECONDS = int(os.environ.get("MIO_NATIVE_BUDGET_SECONDS", "6600"))
TARGETS = {"linux": "linux-amd64", "linux-arm64": "linux-arm64", "darwin-amd64": "darwin-amd64", "windows-arm64": "windows-arm64", "darwin": "darwin-arm64", "windows": "windows-amd64"}
# Go-hosted Stage A programs per Linux target: the self-contained compiler program, its lowering driver and its messages.
LINUX_PROFILES = {
    "linux-amd64": dict(compiler="selfhost/linux-compiler", stage_a="selfhost/native-aot-linux", tag="linux-compiler", aot="native-aot-linux"),
    "linux-arm64": dict(compiler="selfhost/linux-arm64-compiler", stage_a="selfhost/native-aot-linux-arm64", tag="linux-arm64-compiler", aot="native-aot-linux-arm64"),
    "darwin-amd64": dict(compiler="selfhost/darwin-amd64-compiler", stage_a="selfhost/native-aot-darwin-amd64", tag="darwin-amd64-compiler", aot="native-aot-darwin-amd64"),
    "windows-arm64": dict(compiler="selfhost/windows-arm64-compiler", stage_a="selfhost/native-aot-windows-arm64", tag="windows-arm64-compiler", aot="native-aot-windows-arm64"),
}
EXPECTED_HOST = {"linux-amd64": ("Linux", ("x86_64", "amd64")), "linux-arm64": ("Linux", ("aarch64", "arm64")), "darwin-amd64": ("Darwin", ("x86_64", "amd64")), "windows-arm64": ("Windows", ("arm64", "aarch64")), "darwin-arm64": ("Darwin", ("arm64", "aarch64")),
                 "windows-amd64": ("Windows", ("amd64", "x86_64"))}
EXE = {"windows-amd64": ".exe", "windows-arm64": ".exe"}


def is_windows(target):
    return target in ("windows-amd64", "windows-arm64")
TCP_PACKAGE = "bunko/kiso/net/tcp"
ECHO_SERVER = "examples/tcp_echo_server.mio"
ECHO_CLIENT = "examples/tcp_echo_client.mio"


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


class Report:
    def __init__(self, target, output, require_seed=False):
        self.target, self.output = target, Path(output).resolve()
        self.output.mkdir(parents=True, exist_ok=True)
        self.logs = self.output / "logs"
        self.logs.mkdir(exist_ok=True)
        self.checks, self.hashes = [], {}
        self.run_examples = False
        self.exec_prefix = []
        self.require_seed = require_seed
        self.data = dict(schema_version=1, suite="native-platforms", target=target,
                         source_sha=os.environ.get("GITHUB_SHA") or git("rev-parse", "HEAD"),
                         runner=dict(os=os.environ.get("RUNNER_OS", ""), arch=os.environ.get("RUNNER_ARCH", ""),
                                     name=os.environ.get("RUNNER_NAME", ""), image=os.environ.get("ImageOS", ""),
                                     image_version=os.environ.get("ImageVersion", ""),
                                     system=platform.system(), machine=platform.machine(), platform=platform.platform()),
                         run=dict(id=os.environ.get("GITHUB_RUN_ID", ""), attempt=os.environ.get("GITHUB_RUN_ATTEMPT", ""),
                                  url="%s/%s/actions/runs/%s" % (os.environ.get("GITHUB_SERVER_URL", ""),
                                                                 os.environ.get("GITHUB_REPOSITORY", ""),
                                                                 os.environ.get("GITHUB_RUN_ID", ""))),
                         seed=dict(status="NOT_RUN", sha256=GF4_SEED_SHA256,
                                   reason="the candidate suite does not use the trusted GF4 Native Seed; "
                                          "Stage A comes from the Go-hosted reference VM and is not seed-verified"),
                         checks=self.checks, hashes=self.hashes)
        inputs = ROOT / "distribution-input.json"
        if inputs.is_file():
            self.data["distribution_input"] = json.loads(inputs.read_text())

    def check(self, name, status, engine, reason="", required=True, **extra):
        item = dict(name=name, status=status, engine=engine, required=required, reason=reason)
        item.update(extra)
        self.checks.append(item)
        print("%-15s %-34s %-10s %s" % (status, name, engine, reason), flush=True)
        self.save()
        return status == "PASS"

    def save(self):
        failed = [c["name"] for c in self.checks if c["required"] and c["status"] != "PASS"]
        status = "FAIL" if failed else "PASS"
        if self.require_seed and self.data["seed"]["status"] != "PASS":
            status = ("FAIL" if self.data["seed"]["status"] == "FAIL"
                      or any(c["status"] == "FAIL" for c in self.checks) else "NOT_RUN")
        self.data["status"] = status
        self.data["failed_required"] = failed
        (self.output / "report.json").write_text(json.dumps(self.data, indent=2) + "\n")


def git(*args):
    try:
        return subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return ""


def run(report, label, argv, cwd=ROOT, stdin=None, timeout=1800, env=None):
    """Runs a process, keeps its output in logs/<label>.log; returns (exit code or None on timeout, stdout, stderr)."""
    argv = [*report.exec_prefix, *argv]
    started = time.monotonic()
    environment = dict(os.environ, MIO_BUNKO=str(ROOT / "bunko"))
    for name, value in (env or {}).items():
        if value is None:
            environment.pop(name, None)
        else:
            environment[name] = str(value)
    try:
        proc = subprocess.run([str(a) for a in argv], cwd=cwd, input=stdin, capture_output=True, timeout=timeout, env=environment)
        code, out, err = proc.returncode, proc.stdout, proc.stderr
    except subprocess.TimeoutExpired as error:
        code, out, err = None, error.stdout or b"", (error.stderr or b"") + b"\n[timed out]"
    except OSError as error:
        # The image could not even be started (for example WinError 193, an invalid executable): a failure with its reason.
        code, out, err = -1, b"", ("cannot start the process: %s" % error).encode()
    seconds = time.monotonic() - started
    with open(report.logs / (label + ".log"), "wb") as stream:
        stream.write(("$ %s\n[cwd %s] [exit %s] [%.1fs]\n" % (" ".join(str(a) for a in argv), cwd, code, seconds)).encode())
        stream.write(b"--- stdout\n" + out + b"\n--- stderr\n" + err)
    return code, out.decode(errors="replace"), err.decode(errors="replace"), seconds


def brief(text, limit=300):
    text = " ".join(text.split())
    return text[-limit:]


def verify_host(report):
    system, machines = EXPECTED_HOST[report.target]
    actual = (platform.system(), platform.machine().lower())
    ok = actual[0] == system and actual[1] in machines
    return report.check("runner-os-arch", "PASS" if ok else "FAIL", "host",
                        "" if ok else "expected %s/%s, runner is %s/%s" % (system, machines[0], actual[0], actual[1]),
                        detected="%s/%s" % actual)


CLI_BUILD_TIMEOUT = 1500  # a self-build that needs longer is reported FAIL (with its log), never left to hang the job


def peak_rss_mib(err):
    """Peak RSS from macOS `/usr/bin/time -l` (bytes) or GNU `time -v` (KiB); 0 when absent."""
    match = re.search(r"(\d+)\s+maximum resident set size", err)
    if match:
        return int(match.group(1)) // (1024 * 1024)
    match = re.search(r"Maximum resident set size \(kbytes\): (\d+)", err)
    return int(match.group(1)) // 1024 if match else 0


def cli_build(report, label, cli, entry, target, output, extra=(), cwd=ROOT):
    argv = [cli, "build", entry, "--target=" + target, "-o", output, *extra]
    if not report.exec_prefix and platform.system() == "Darwin" and Path("/usr/bin/time").exists():
        argv = ["/usr/bin/time", "-l", *argv]  # resource usage only; the build itself is unchanged
    code, out, err, seconds = run(report, label, argv, cwd=cwd, timeout=CLI_BUILD_TIMEOUT)
    ok = code == 0 and Path(output).is_file()
    if ok and not is_windows(target):
        os.chmod(output, 0o755)
    reason = "timed out after %ds" % CLI_BUILD_TIMEOUT if code is None else "exit %s: %s" % (code, brief(err or out))
    rss = peak_rss_mib(err)
    if rss:
        reason += " (peak RSS %d MiB)" % rss
    print("    %s: %.1fs%s" % (label, seconds, ", peak RSS %d MiB" % rss if rss else ""), flush=True)
    return ok, reason, seconds


def cli_chain(report, first, target, work, engine="native", checks=False):
    """first builds selfhost/cli -> B, B -> C, C -> D for `target`; requires B == C == D. Returns the path of B."""
    suffix = EXE.get(target, "")
    paths = {"A": Path(first)}
    for previous, name in (("A", "B"), ("B", "C"), ("C", "D")):
        output = work / ("mio-%s%s" % (name, suffix))
        ok, reason, seconds = cli_build(report, "cli-" + name, paths[previous], "selfhost/cli", target, output)
        if not report.check("cli-stage-" + name, "PASS" if ok else "FAIL", engine,
                            "" if ok else "%s could not build stage %s: %s" % (previous, name, reason),
                            seconds=round(seconds, 1), built_by=previous, detail=reason if ok else ""):
            return None
        paths[name] = output
        report.hashes["cli-" + name] = sha256(output)
    same = sha256(paths["B"]) == sha256(paths["C"]) == sha256(paths["D"])
    report.check("cli-fixed-point", "PASS" if same else "FAIL", engine, "" if same else "B, C and D differ",
                 B=report.hashes["cli-B"], C=report.hashes["cli-C"], D=report.hashes["cli-D"])
    if same and checks:
        cli_checks(report, paths["B"], target, work)
    return paths["B"] if same else None


def cli_checks(report, cli, target, work):
    """Locale, help, theme and machine-output checks on the native CLI itself."""
    ansi = "\x1b["
    def check(name, argv, code=0, contains=(), absent=(), env=None):
        actual, out, err, _ = run(report, "cli-" + name, [cli, *argv], cwd=work, timeout=180, env=env)
        text = out + err
        problems = []
        if actual != code:
            problems.append("exit %s" % actual)
        for value in contains:
            if value not in text:
                problems.append("missing %r" % value)
        for value in absent:
            if value in text:
                problems.append("unexpected %r" % value)
        return report.check("cli-i18n-" + name, "PASS" if not problems else "FAIL", "native",
                            "" if not problems else brief(", ".join(problems)), detail=brief(text))
    check("japanese", ["--lang", "ja-JP", "help"], contains=("使用方法", "コマンド"), absent=(ansi,), env={"NO_COLOR": None, "CI": None})
    check("region-fallback", ["--lang", "fr-CA", "help", "build"], contains=("Construire", "mio build <source>"), absent=(ansi,))
    check("explicit-color", ["--color=always", "--theme=dark", "help"], contains=(ansi,), env={"NO_COLOR": "1", "TERM": "xterm-256color"})
    check("no-color", ["--color=never", "help"], absent=(ansi,), env={"NO_COLOR": None, "CI": None})
    check("auto-no-color", ["help"], absent=(ansi,), env={"NO_COLOR": "1", "CI": None})
    check("unknown-command", ["--lang", "zh-CN", "help", "missing-command"], code=2, contains=("E_COMMAND", "未知命令"))
    check("version", ["--lang", "en-US", "--version"], contains=("mio v2.1.2",))
    bad = work / "i18n-machine-bad.mio"
    bad.write_text('chant(missing_symbol);\n', encoding="utf-8")
    check("machine-json", ["--lang", "ja-JP", "--color=always", "check", str(bad), "--format=json"], code=1,
          contains=('"code":', '"severity":"error"'), absent=(ansi, "不明な"))
    check("lsp-help-fr", ["--lang=fr-FR", "lsp", "--help"],
          contains=("Utilisation: mio lsp", "--bunko-root"), absent=(ansi,))
    missing_bunko = work / "i18n-nonexistent-bunko"
    check("bunko-root-de", ["--lang=de-DE", "check", str(bad), "--bunko-root", str(missing_bunko)], code=2,
          contains=("E_BUNKO_ROOT", "Bunko-Verzeichnis", "kiso/", str(missing_bunko)), absent=(ansi,))
    check("diagnostic-fr", ["--lang=fr-FR", "--color=never", "check", str(bad)], code=1,
          contains=("erreur[", "missing_symbol"), absent=(ansi,))
    # Frontend accepts this source; the native backend rejects the 600-value
    # expression frame. The original E_LOWER detail must survive localization.
    backend = work / "i18n-backend-failure.mio"
    backend.write_text("kaku values = [%s];\nchant(values[0]);\n" % ",".join(str(index) for index in range(600)),
                       encoding="utf-8")
    output = work / "i18n-backend-failure.bin"
    check("backend-error", ["--lang", "en-US", "build", str(backend), "--target=darwin-arm64", "-o", str(output)], code=1,
          contains=("E_LOWER 6 / 0",))
    check("backend-verbose", ["--lang", "en-US", "build", str(backend), "--target=darwin-arm64", "--verbose", "-o", str(output)], code=1,
          contains=("E_LOWER 6 / 0", "cause: source analysis failed"))
    check("backend-json", ["--lang", "en-US", "--color=always", "build", str(backend), "--target=darwin-arm64", "--format=json", "-o", str(output)],
          code=1, contains=('"code":"E0401"', '"message":"E_LOWER 6 / 0"'), absent=(ansi,))


def core_chain(report, full_cli, target, work, checks=False):
    """Build the Core variant from an empty embedded table with the verified Full compiler.
    The source copy is disposable; Core must remain byte-identical across its own stages."""
    source = work / "core-source"
    if source.exists():
        shutil.rmtree(source)
    shutil.copytree(ROOT / "selfhost", source / "selfhost")
    (source / "selfhost/resources/bunko_data.mio").write_bytes(EMPTY.encode("utf-8"))
    suffix = EXE.get(target, "")
    paths = {"full": Path(full_cli)}
    for previous, name in (("full", "B"), ("B", "C"), ("C", "D")):
        output = work / ("mio-core-%s%s" % (name, suffix))
        ok, reason, seconds = cli_build(report, "core-cli-" + name, paths[previous], "selfhost/cli", target, output, cwd=source)
        if not report.check("core-cli-stage-" + name, "PASS" if ok else "FAIL", "native",
                            "" if ok else "%s could not build Core stage %s: %s" % (previous, name, reason),
                            seconds=round(seconds, 1), built_by=previous, detail=reason if ok else ""):
            return None
        report.hashes["core-cli-" + name] = sha256(output)
        paths[name] = output
    same = sha256(paths["B"]) == sha256(paths["C"]) == sha256(paths["D"])
    report.check("core-cli-fixed-point", "PASS" if same else "FAIL", "native", "" if same else "Core B, C and D differ",
                 B=report.hashes["core-cli-B"], C=report.hashes["core-cli-C"], D=report.hashes["core-cli-D"])
    if same and checks:
        cli_checks(report, paths["B"], target, work)
    return paths["B"] if same else None


def embedded_checks(report, full, core, target, work):
    """Exercise embedded and external Bunko resolution without modifying the repository."""
    suffix = EXE.get(target, "")
    base = work / "embedded-bunko"
    if base.exists():
        shutil.rmtree(base)
    (base / "cwd").mkdir(parents=True)
    def install(binary, name):
        root = base / name
        (root / "bin").mkdir(parents=True)
        cli = root / "bin" / ("mio.exe" if is_windows(target) else "mio")
        shutil.copyfile(binary, cli)
        if not is_windows(target):
            cli.chmod(0o755)
        return root, cli
    full_root, full_cli = install(full, "full")
    # A legacy adjacent tree must not displace the embedded default.
    (full_root / "bunko/kiso").mkdir(parents=True)
    core_root, core_cli = install(core, "core")
    cwd = base / "cwd"
    all_source = cwd / "all.mio"
    all_source.write_text("""yobu "kiso/math";
yobu "kiso/string";
yobu "kiso/array";
yobu "kiso/bytes";
yobu "kiso/io";
yobu "kiso/fs";
yobu "kiso/encoding/json";
yobu "kiso/regexp";
yobu "kiso/crypto/sha256";
yobu "kiso/os";
yobu "kiso/net/tcp";
chant(math.abs(-9));
chant(string.concat("a","b"));
kaku xs: []int64=[1,2]; array.push(xs,3); chant(len(xs));
kaku data: []uint8=[uint8(104),uint8(105)]; chant(bytes.equal(data,data));
kaku reader=io.reader_from_bytes(data); kaku dst: []uint8=[uint8(0),uint8(0)]; chant(io.read_full(reader,dst,0,2).count);
chant(fs.exists("all.mio"));
chant(json.parse(string.to_bytes("{}")).error);
chant(regexp.matches(regexp.compile("a+"),"aaa"));
chant(len(sha256.sum(data)));
chant(os.monotonic_ms()>=0);
chant(len(tcp.ipv4(uint8(127),uint8(0),uint8(0),uint8(1),0).octets));
""", encoding="utf-8")
    math_source = cwd / "math.mio"
    math_source.write_text('yobu "kiso/math"; chant(math.abs(-9));\n', encoding="utf-8")
    image = cwd / ("all" + suffix)
    code, out, err, _ = run(report, "full-embedded-build", [full_cli, "build", all_source, "--target=" + target, "-o", image],
                            cwd=cwd, env={"MIO_BUNKO": None})
    executed = ""
    if code == 0 and image.is_file():
        if not is_windows(target):
            image.chmod(0o755)
        run_code, executed, run_err, _ = run(report, "full-embedded-run", [image], cwd=cwd, env={"MIO_BUNKO": None})
        code = run_code if run_code != 0 else code
        err += run_err
    expected = ["9", "ab", "3", "hai", "2", "hai", "", "hai", "32", "hai", "4"]
    report.check("full-embedded-runtime", "PASS" if code == 0 and [line.strip() for line in executed.splitlines()] == expected else "FAIL",
                 "native", "" if code == 0 else "embedded build/run failed: " + brief(err or out), output=executed)
    transitive = cwd / "transitive.mio"
    transitive.write_text('yobu "kiso/string"; chant(string.concat("x","y"));\n', encoding="utf-8")
    code, out, err, _ = run(report, "full-embedded-transitive-check", [full_cli, "check", transitive], cwd=cwd, env={"MIO_BUNKO": None})
    report.check("full-embedded-transitive", "PASS" if code == 0 else "FAIL", "native", "" if code == 0 else brief(err or out))
    code, out, err, _ = run(report, "core-no-embedded", [core_cli, "check", math_source], cwd=cwd, env={"MIO_BUNKO": None})
    ok = code != 0 and "E_BUNKO_MISSING" in out + err
    report.check("core-no-embedded", "PASS" if ok else "FAIL", "native",
                 "" if ok else "Core unexpectedly resolved embedded Bunko" if code == 0 else brief(err or out))
    code, out, err, _ = run(report, "core-external-root", [core_cli, "check", math_source, "--bunko-root", ROOT / "bunko"],
                            cwd=cwd, env={"MIO_BUNKO": None})
    report.check("core-external-root", "PASS" if code == 0 else "FAIL", "native", "" if code == 0 else brief(err or out))
    # Adjacent executable-relative discovery remains available to Core.
    shutil.copytree(ROOT / "bunko", core_root / "bunko")
    code, out, err, _ = run(report, "core-adjacent", [core_cli, "check", math_source], cwd=cwd, env={"MIO_BUNKO": None})
    report.check("core-adjacent", "PASS" if code == 0 else "FAIL", "native", "" if code == 0 else brief(err or out))
    # An invalid explicit root must beat the embedded table and valid adjacent tree.
    code, out, err, _ = run(report, "explicit-root-precedence", [full_cli, "check", math_source, "--bunko-root", cwd / "missing"],
                            cwd=cwd, env={"MIO_BUNKO": ROOT / "bunko"})
    ok = code != 0 and "E_BUNKO_ROOT" in out + err
    report.check("explicit-root-precedence", "PASS" if ok else "FAIL", "native",
                 "" if ok else "invalid explicit root fell back to embedded Bunko", output=brief(err or out))
    missing = cwd / "missing.mio"
    missing.write_text('yobu "kiso/not/a/package";\n', encoding="utf-8")
    code, out, err, _ = run(report, "invalid-module-diagnostic", [full_cli, "check", missing], cwd=cwd, env={"MIO_BUNKO": None})
    ok = code != 0 and "kiso/not/a/package" in out + err
    report.check("invalid-module-diagnostic", "PASS" if ok else "FAIL", "native",
                 "" if ok else "missing module was accepted", output=brief(err or out))


def smoke(report, cli, target, work, label_prefix=""):
    Path(work).mkdir(parents=True, exist_ok=True)
    code, out, err, _ = run(report, label_prefix + "cli-version", [cli, "--version"])
    report.check(label_prefix + "cli-version", "PASS" if code == 0 and out.startswith("mio ") else "FAIL", "native",
                 "" if code == 0 else "exit %s: %s" % (code, brief(err)), output=out.strip())
    project = work / "hello"
    project.mkdir(exist_ok=True)
    (project / "main.mio").write_text('chant("hello from the native Mio CLI");\nkaku n = 0; kaku i = 1;\n'
                                      'aida (i <= 100) { n = n + i; i = i + 1; }\nchant(n);\n')
    image = work / ("hello-image" + EXE.get(target, ""))
    ok, reason, _ = cli_build(report, label_prefix + "hello-build", cli, str(project / "main.mio"), target, image)
    if not ok:
        return report.check(label_prefix + "cli-hello", "FAIL", "native", "build " + reason)
    code, out, err, _ = run(report, label_prefix + "hello-run", [image], cwd=work)
    want = "hello from the native Mio CLI\n5050\n"
    ok = code == 0 and out == want
    return report.check(label_prefix + "cli-hello", "PASS" if ok else "FAIL", "native", "" if ok else "exit %s, stdout %r" % (code, out[:200]))


def windows_tcp_pe(report, image, label):
    try:
        symbols = verify_winsock(image.read_bytes())
    except (OSError, ValueError, struct.error, StopIteration) as error:
        return report.check(label, "FAIL", "pe-static", str(error))
    return report.check(label, "PASS", "pe-static", imports=symbols, sha256=sha256(image))


def windows_tcp_echo_checks(report, results, output):
    """Keep the Winsock PR gate, using the same pair and results as the Full example suite."""
    pair = [item for item in results if item["name"] in examples_native.TCP]
    for item in pair:
        role = "server" if item["name"] == "tcp_echo_server.mio" else "client"
        built = item["image_built"]
        report.check(f"windows-tcp-echo-{role}-build", "PASS" if built else "FAIL", "native-build",
                     "" if built else item["reason"], exit_code=item["build_exit_code"],
                     log=(output / item["logs"]["build.log"]).relative_to(report.output).as_posix())
    ok = len(pair) == 2 and all(item["status"] == "PASS" for item in pair)
    report.check("windows-tcp-echo", "PASS" if ok else "FAIL", "native",
                 "; ".join(f'{item["name"]}: {item["reason"]}' for item in pair if item["status"] != "PASS"),
                 results=(output / "results.json").relative_to(report.output).as_posix())


def windows_tcp_wildcard(report, cli, work):
    image = work / "tcp-wildcard.exe"
    label = "windows-tcp-wildcard"
    ok, reason, _ = cli_build(report, label + "-build", cli, "tests/windows-amd64/tcp_wildcard.mio", report.target, image)
    if not ok:
        return report.check(label, "FAIL", "native", "build " + reason)
    code, out, err, _ = run(report, label, [image], cwd=work, timeout=15)
    ok = code == 0 and out == "tcp wildcard: ok\n" and not err
    return report.check(label, "PASS" if ok else "FAIL", "native",
                        "" if ok else "exit %s: %s" % (code, brief(out + err)))


def mio_run_protocol(report, cli, work):
    """`mio run` builds for the host (the default target) and starts the program with the given arguments: the same
    argument strings (spaces, empty, quotes, backslashes, UTF-8) and the program's exit status on every platform."""
    project = work / "run-protocol"
    project.mkdir(parents=True, exist_ok=True)
    (project / "args.mio").write_text(
        'yobu "kiso/os";\nyobu "kiso/string";\n'
        'kaku args = os.arguments(); kaku i = 1;\n'
        'aida (i < len(args)) { chant(string.concat("[", string.concat(args[i], "]"))); i = i + 1; }\n'
        'os.exit(len(args) + 40);\n', encoding="utf-8")
    values = ["a b", "", 'q"x', "back\\\\", 'tr\\"q', "end\\", "\u65e5\u672c\u8a9e x"]
    code, out, err, _ = run(report, "mio-run-protocol", [cli, "run", "args.mio", "--", *values], cwd=project)
    expected = "".join("[%s]\n" % value for value in values)
    ok = code == len(values) + 41 and out.replace("\r\n", "\n") == expected
    report.check("mio-run-protocol", "PASS" if ok else "FAIL", "native",
                 "" if ok else "exit %s (expected %d), stdout %r, stderr %r" % (code, len(values) + 41, out[:300], brief(err)))


def os_process_protocol(report, cli, work):
    """kiso/os process operations with a real child, built for the host: spawn passes the argument strings unchanged and
    returns the child's exit status, a program that cannot be started reports 127, and exec replaces the caller
    (Windows: starts, waits and exits with the child's status), so the caller's exit status is the child's."""
    project = work / "os-process"
    project.mkdir(parents=True, exist_ok=True)
    child, parent = project / ("child" + EXE.get(report.target, "")), project / ("parent" + EXE.get(report.target, ""))
    (project / "child.mio").write_text(
        'yobu "kiso/os";\nyobu "kiso/string";\n'
        'kaku args = os.arguments(); kaku i = 1;\n'
        'aida (i < len(args)) { chant(string.concat("<", string.concat(args[i], ">"))); i = i + 1; }\n'
        'os.exit(len(args) + 20);\n', encoding="utf-8")
    (project / "parent.mio").write_text(
        'yobu "kiso/os";\n'
        'kaku args = os.arguments(); kaku child = args[1]; kaku missing = args[2];\n'
        'chant(os.spawn(child, [child, "a b", "", "q\\"x", "\u65e5\u672c"]));\n'
        'chant(os.spawn(missing, [missing]));\n'
        'os.exec(child, [child, "exec"]);\n'
        'chant("exec returned");\n', encoding="utf-8")
    built = all(cli_build(report, "os-process-" + name, cli, str(project / (name + ".mio")), report.target, image)[0]
                for name, image in (("child", child), ("parent", parent)))
    if not built:
        return report.check("os-process-protocol", "FAIL", "native", "the protocol programs did not build")
    code, out, err, _ = run(report, "os-process-protocol", [parent, child, project / "no-such-program"], cwd=project)
    expected = "<a b>\n<>\n<q\"x>\n<\u65e5\u672c>\n25\n127\n<exec>\n"
    ok = code == 22 and out.replace("\r\n", "\n") == expected
    reason = "" if ok else "exit %s (expected 22), stdout %r, stderr %r" % (code, out[:300], brief(err))
    # stdin readiness: a pipe holding data, and an empty pipe whose writer is closed (end of file), are both ready.
    reader = project / ("stdin" + EXE.get(report.target, ""))
    (project / "stdin.mio").write_text('yobu "kiso/os";\nchant(os.stdin_wait(5000));\nchant(os.stdin_ready());\n', encoding="utf-8")
    if ok and cli_build(report, "os-process-stdin", cli, str(project / "stdin.mio"), report.target, reader)[0]:
        for label, data in (("data", b"x\n"), ("eof", b"")):
            code, out, err, _ = run(report, "os-process-stdin-" + label, [reader], cwd=project, stdin=data)
            if code != 0 or out.replace("\r\n", "\n") != "1\n1\n":
                ok, reason = False, "stdin %s: exit %s, stdout %r (expected 1, 1), stderr %r" % (label, code, out, brief(err))
                break
    elif ok:
        ok, reason = False, "the stdin program did not build"
    report.check("os-process-protocol", "PASS" if ok else "FAIL", "native", reason)


def mio_test_protocol(report, cli, work):
    """mio test builds one image per package and selects each test by argument: a failing assertion must still fail
    exactly that test, passing tests must pass, the exit status must be non-zero, and --run must select tests."""
    project = work / "test-protocol"
    (project / "mixed").mkdir(parents=True, exist_ok=True)
    link = project / "bunko"
    if not link.exists() and platform.system() != "Windows":  # MIO_BUNKO (run) names the library on every host
        link.symlink_to(ROOT / "bunko", target_is_directory=True)
    (project / "mixed" / "m_test.mio").write_text(
        'yobu "kiso/testing";\n'
        'waza test_good() -> unit { testing.expect(1 + 1 == 2, "math"); }\n'
        'waza test_bad() -> unit { testing.expect(1 == 2, "one is not two"); }\n'
        'waza test_also_good() -> unit { testing.expect(hai, "ok"); }\n'
        'waza test_failed() -> unit { testing.fail("explicit failure"); }\n'
        'waza test_skipped() -> unit { testing.skip("not on this host"); }\n')
    code, out, err, _ = run(report, "mio-test-protocol-all", [cli, "test", "mixed"], cwd=project)
    text = out + err
    ok = (code != 0 and "Tests complete: 2 passed, 2 failed" in text and "FAIL: test_bad" in text and
          "assertion failed: one is not two" in text and "PASS: test_good" in text and "PASS: test_also_good" in text and
          "FAIL: test_failed" in text and "explicit failure" in text and "Tests skipped: 1" in text and
          "PASS: test_skipped" not in text)
    code2, out2, err2, _ = run(report, "mio-test-protocol-filter", [cli, "test", "mixed", "--run=good"], cwd=project)
    ok2 = code2 == 0 and "Tests complete: 2 passed, 0 failed" in out2 + err2 and "test_bad" not in out2 + err2
    report.check("mio-test-protocol", "PASS" if ok and ok2 else "FAIL", "native",
                 "" if ok and ok2 else "unexpected result: %s | %s" % (brief(text), brief(out2 + err2)))


def test_packages():
    found = set()
    for top in ("bunko", "selfhost"):
        for path in (ROOT / top).rglob("*_test.mio"):
            found.add(path.parent.relative_to(ROOT).as_posix())
    return sorted(found)


def count(pattern, text):
    match = re.search(pattern, text)
    return int(match.group(1)) if match else 0


def native_mio_test(report, cli, skips_allowed):
    """Every package with *_test.mio. A skipped test is never a pass: skips are reported separately (SKIP, with the
    test's own reason). Where every test must run (darwin-arm64, the primary target) a skip is a required failure."""
    failures = []
    for package in test_packages():
        label = "mio-test-" + package.replace("/", "_")
        allow_skip = skips_allowed and package != TCP_PACKAGE
        if time.monotonic() - STARTED > BUDGET_SECONDS:
            report.check("mio-test " + package, "NOT_RUN", "native", "platform time budget (%ds) exhausted" % BUDGET_SECONDS)
            failures.append(package)
            continue
        code, out, err, seconds = run(report, label, [cli, "test", package], timeout=900)
        text = out + err
        passed = count(r"Tests complete: (\d+) passed", text)
        failed = count(r"Tests complete: \d+ passed, (\d+) failed", text)
        skipped = count(r"Tests skipped: (\d+)", text)
        reasons = sorted({line.strip() for line in text.splitlines() if line.strip().startswith("skipped:")})
        complete = "Tests complete:" in text
        ok = code == 0 and complete and failed == 0 and (passed > 0 or skipped > 0)  # nothing executed is a failure
        status = "PASS" if ok and passed > 0 and skipped == 0 else "FAIL" if not ok else "SKIP"
        report.check("mio-test " + package, "PASS" if status == "PASS" else "FAIL" if status == "FAIL" else
                     ("SKIP" if allow_skip else "FAIL"), "native",
                     "" if status == "PASS" else ("exit %s: %s" % (code, brief(text)) if status == "FAIL" else
                                                  "%d skipped: %s" % (skipped, "; ".join(reasons)[:300])),
                     required=status != "SKIP" or not allow_skip, seconds=round(seconds, 1),
                     passed=passed, failed=failed, skipped=skipped)
        if status == "SKIP" and allow_skip and passed > 0:
            report.check("mio-test " + package + " (non-skipped)", "PASS", "native", "", passed=passed)
        if status == "FAIL" or (status == "SKIP" and not allow_skip):
            failures.append(package)
    return not failures


def contract(report, cli, target, reference="", suite="contract"):
    """tests/runtime-abi/run.py --suite contract (RA-1 cases) or differential (former pureLowerPrograms corpus)."""
    output = report.output / ("runtime-abi" if suite == "contract" else "native-differential")
    argv = [sys.executable, ROOT / "tests/runtime-abi/run.py", "--suite", suite, "--mio", cli, "--target", target, "--output", output]
    if reference:
        argv += ["--reference", reference]
    code, out, err, seconds = run(report, "suite-" + suite, argv, timeout=5400)
    native_name = "runtime-abi-contract" if suite == "contract" else "native-differential"
    try:
        results = json.loads((output / "results.json").read_text())["results"]
    except (OSError, ValueError, KeyError):
        return report.check(native_name, "FAIL", "native", "runner produced no results: exit %s %s" % (code, brief(err)))
    ok_all = code == 0
    for engine in ("native", "reference"):
        if engine == "reference" and not reference:
            continue
        items = [r for r in results if r["engine"] in (engine, "contract")]
        bad = [r for r in items if r["status"] not in ("PASS", "NOT_APPLICABLE")]
        excluded = [r for r in items if r["status"] == "NOT_APPLICABLE"]
        name = native_name if engine == "native" else "vm-reference-" + suite
        report.check(name, "PASS" if items and not bad else "FAIL", engine,
                     "; ".join("%s/%s: %s" % (r["case"], r["mode"], r["reason"]) for r in bad)[:600],
                     cases=len({r["case"] for r in items}), passed=len([r for r in items if r["status"] == "PASS"]),
                     seconds=round(seconds, 1))
        if excluded:
            report.check(name + " (excluded)", "NOT_APPLICABLE", engine,
                         "; ".join("%s/%s: %s" % (r["case"], r["mode"], r["reason"]) for r in excluded)[:600], required=False)
        ok_all = ok_all and bool(items) and not bad
    return ok_all


# ---------------------------------------------------------------------------------------------------- linux

def linux(args, target="linux-amd64"):
    profile = LINUX_PROFILES[target]
    report = Report(target, args.output)
    report.run_examples = args.run_examples
    if not verify_host(report):
        return finish(report)
    work = report.output / "work"
    work.mkdir(exist_ok=True)
    refvm = Path(args.refvm).resolve()
    # Stage A: Go reference frontend -> Mochi; pure-Mio lowerlinux running in the Go VM -> ELF. Explicitly go-hosted.
    mochi = work / (profile["tag"] + ".mochi")
    code, out, err, seconds = run(report, "stage-a-mochi", [refvm, ROOT / "bunko", "mochi", ROOT / (profile["compiler"] + "/main.mio"), mochi])
    if not report.check("stage-a-frontend", "PASS" if code == 0 else "FAIL", "go-hosted",
                        "" if code == 0 else brief(err), seconds=round(seconds, 1),
                        note="Go reference frontend compiles %s to Mochi" % profile["compiler"]):
        return finish(report)
    lowering = work / "stage-a-lowering"
    lowering.mkdir(exist_ok=True)
    suffix = EXE.get(target, "")
    compiler_a = work / ("compiler-A" + suffix)
    (lowering / "input.config").write_text(str(mochi))
    (lowering / "output.config").write_text(str(compiler_a))
    code, out, err, seconds = run(report, "stage-a-lowering", [refvm, ROOT / "bunko", "run", ROOT / (profile["stage_a"] + "/main.mio")], cwd=lowering, timeout=3600)
    ok = code == 0 and (profile["aot"] + ": ok") in out and compiler_a.is_file()
    if not report.check("stage-a-lowering", "PASS" if ok else "FAIL", "go-hosted", "" if ok else "exit %s: %s" % (code, brief(err or out)),
                        seconds=round(seconds, 1), note="pure-Mio %s lowering executed by the Go reference VM; not a trusted seed" % target):
        return finish(report)
    os.chmod(compiler_a, 0o755)
    report.hashes["compiler-A"] = sha256(compiler_a)
    # Native compiler fixed point
    paths = {"A": compiler_a}
    for previous, name in (("A", "B"), ("B", "C"), ("C", "D")):
        output = work / ("compiler-" + name + suffix)
        code, out, err, seconds = run(report, "compiler-" + name, [paths[previous]], stdin=("%s\n%s\n" % (profile["compiler"], output)).encode())
        ok = code == 0 and out == profile["tag"] + ": ok\n" and output.is_file()
        if not report.check("compiler-stage-" + name, "PASS" if ok else "FAIL", "native", "" if ok else "exit %s: %s" % (code, brief(err or out)),
                            seconds=round(seconds, 1), built_by=previous):
            return finish(report)
        os.chmod(output, 0o755)
        paths[name] = output
        report.hashes["compiler-" + name] = sha256(output)
    same = report.hashes["compiler-B"] == report.hashes["compiler-C"] == report.hashes["compiler-D"]
    report.check("compiler-fixed-point", "PASS" if same else "FAIL", "native", "" if same else "B, C and D differ")
    if not same:
        return finish(report)
    # CLI: compiler B builds selfhost/cli (CLI A); then the CLI builds itself A -> B -> C -> D
    cli_a = work / ("mio-A" + suffix)
    code, out, err, seconds = run(report, "cli-A", [paths["B"]], stdin=("selfhost/cli\n%s\n" % cli_a).encode())
    ok = code == 0 and out == profile["tag"] + ": ok\n" and cli_a.is_file()
    if not report.check("cli-stage-A", "PASS" if ok else "FAIL", "native", "" if ok else "exit %s: %s" % (code, brief(err or out)), built_by="compiler-B"):
        return finish(report)
    os.chmod(cli_a, 0o755)
    report.hashes["cli-A"] = sha256(cli_a)
    cli = cli_chain(report, cli_a, target, work, checks=True)
    if cli is None:
        return finish(report)
    core = core_chain(report, cli, target, work, checks=True)
    if core is None:
        return finish(report)
    embedded_checks(report, cli, core, target, work)
    smoke(report, cli, target, work)
    mio_run_protocol(report, cli, work)
    os_process_protocol(report, cli, work)
    mio_test_protocol(report, cli, work)
    native_mio_test(report, cli, skips_allowed=True)
    contract(report, cli, target, reference=str(refvm))
    contract(report, cli, target, reference=str(refvm), suite="differential")
    if target != "linux-amd64":
        # The remaining steps are specific to the amd64 hub: the x86-64 runtime chain and the cross-built Stage A images.
        return finish(report, cli)
    chain = work / "runtime-chain"
    chain.mkdir(exist_ok=True)
    code, out, err, seconds = run(report, "linux-runtime-chain", [sys.executable, ROOT / "bunko/kiso/runtime/os/linux/check_chain.py"],
                                  env=dict(MIO_CI_MIO=str(cli), MIO_CI_MODULE_DIR=str(chain)), timeout=1800)
    executed = "execution: NOT_RUN" not in out + err
    report.check("linux-runtime-chain", "PASS" if code == 0 and executed else "FAIL", "native",
                 "" if code == 0 and executed else "exit %s: %s" % (code, brief(err or out)), seconds=round(seconds, 1),
                 note="kiso/runtime/ir -> cpu/amd64 -> x86_64 -> elf image built by a native Mio program and executed")
    # Stage A of the other targets, cross-built by two native generations that must agree
    seeds = report.output / "stage-a"
    seeds.mkdir(exist_ok=True)
    manifest = {}
    for target in ("darwin-arm64", "windows-amd64"):
        name = "mio-%s%s" % (target, EXE.get(target, ""))
        first, second = seeds / name, work / ("again-" + name)
        ok1, reason1, _ = cli_build(report, "cross-" + target, cli, "selfhost/cli", target, first)
        ok2, reason2, _ = cli_build(report, "cross-again-" + target, work / "mio-C", "selfhost/cli", target, second)
        ok = ok1 and ok2 and sha256(first) == sha256(second)
        reason = "" if ok else (reason1 if not ok1 else reason2 if not ok2 else "generations B and C cross-build different images")
        if report.check("cross-stage-a " + target, "PASS" if ok else "FAIL", "native", reason, sha256=sha256(first) if ok1 else ""):
            manifest[target] = dict(file=name, sha256=sha256(first), built_by="linux-amd64 cli-B", source_sha=report.data["source_sha"])
            report.hashes["stage-a-" + target] = manifest[target]["sha256"]
    (seeds / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    windows_echo = work / "cross-tcp-echo-server.exe"
    ok, reason, seconds = cli_build(report, "windows-tcp-cross-build", cli, ECHO_SERVER, "windows-amd64", windows_echo)
    report.check("windows-tcp-cross-build", "PASS" if ok else "FAIL", "cross-build", "" if ok else reason,
                 seconds=round(seconds, 1))
    if ok:
        windows_tcp_pe(report, windows_echo, "windows-tcp-cross-pe")
    else:
        report.check("windows-tcp-cross-pe", "NOT_RUN", "pe-static", "cross build failed")
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a") as stream:
            if not report.data["failed_required"] and len(manifest) == 2:
                stream.write("stage_a_ready=true\n")
            for target, item in manifest.items():
                stream.write("%s=%s\n" % (target.replace("-", "_"), item["sha256"]))
    return finish(report, cli)


# ---------------------------------------------------------------------------------------------------- darwin / windows

def follower(args, target):
    report = Report(target, args.output)
    report.run_examples = args.run_examples
    if not verify_host(report):
        return finish(report)
    work = report.output / "work"
    work.mkdir(exist_ok=True)
    stage_a = Path(args.stage_a).resolve()
    expected = args.expect_sha256
    manifest_sha = ""
    try:
        manifest_sha = json.loads((stage_a.parent / "manifest.json").read_text())[target]["sha256"]
    except (OSError, ValueError, KeyError):
        pass
    actual = sha256(stage_a) if stage_a.is_file() else ""
    ok = bool(actual) and actual == expected == manifest_sha
    if not report.check("stage-a-sha256", "PASS" if ok else "FAIL", "host",
                        "" if ok else "downloaded %s, job output %s, manifest %s" % (actual or "missing", expected, manifest_sha or "missing"),
                        sha256=actual, provenance="cross-built by the native linux-amd64 CLI of this run (go-hosted root)"):
        return finish(report)
    report.hashes["cli-A"] = actual
    first = work / stage_a.name
    shutil.copyfile(stage_a, first)
    if not is_windows(target):
        os.chmod(first, 0o755)
    # A quick signal that Stage A works on this host before the long self-builds.
    smoke(report, first, target, work / "smoke-A", label_prefix="stage-a-")
    cli = cli_chain(report, first, target, work, checks=True)
    if cli is not None:
        core = core_chain(report, cli, target, work, checks=True)
        if core is not None:
            embedded_checks(report, cli, core, target, work)
        same = report.hashes["cli-A"] == report.hashes["cli-B"]
        report.check("cross-equals-native", "PASS" if same else "FAIL", "native",
                     "" if same else "the Linux cross build (A) differs from this host's native build (B)")
        smoke(report, cli, target, work)
        if target == "darwin-arm64":
            darwin_profile(report, cli, work)
        mio_run_protocol(report, cli, work)
        os_process_protocol(report, cli, work)
        mio_test_protocol(report, cli, work)
        # darwin-arm64 is the primary target: every test must run there. Elsewhere a test may skip with its own reason
        # (Mach-O images that need a macOS host); skips are reported separately and never count as passes.
        native_mio_test(report, cli, skips_allowed=target != "darwin-arm64")
        if target == "windows-amd64":
            windows_tcp_wildcard(report, cli, work)
        contract(report, cli, target)
        contract(report, cli, target, suite="differential")
    return finish(report, cli)


def text_section_offset(image):
    """File offset of __TEXT,__text in a 64-bit Mach-O (code word 0 of the symbol map)."""
    data = Path(image).read_bytes()
    count = struct.unpack_from("<I", data, 16)[0]; at = 32
    for _ in range(count):
        command, size = struct.unpack_from("<II", data, at)
        if command == 0x19:
            sections = struct.unpack_from("<I", data, at + 64)[0]
            for index in range(sections):
                base = at + 72 + index * 80
                if data[base:base + 16].rstrip(b"\0") == b"__text":
                    return struct.unpack_from("<I", data, base + 48)[0]
        at += size
    return None


def sample_profile(sample_text, image, symbols_path):
    """Leaf counts of /usr/bin/sample output mapped to Mio function / runtime label names."""
    import bisect
    text_offset = text_section_offset(image)
    entries = []
    for line in Path(symbols_path).read_text().splitlines():
        word, _, name = line.partition(" ")
        if word.isdigit():
            entries.append((int(word), name))
    entries.sort()
    words = [w for w, _ in entries]
    counts = {}
    section = sample_text.split("Sort by top of stack", 1)
    rows = section[1] if len(section) == 2 else ""
    for match in re.finditer(r"\+ (0x[0-9a-f]+)\s+\[0x[0-9a-f]+\]\s+(\d+)\s*$", rows, re.M):
        offset, count = int(match.group(1), 16), int(match.group(2))
        name = "?"
        if text_offset is not None and offset >= text_offset:
            index = bisect.bisect_right(words, (offset - text_offset) // 4) - 1
            if index >= 0:
                name = entries[index][1]
        counts[name] = counts.get(name, 0) + count
    total = sum(counts.values()) or 1
    return [(name, count, round(100.0 * count / total, 1)) for name, count in sorted(counts.items(), key=lambda kv: -kv[1])[:40]]


def darwin_profile(report, cli, work):
    """Self-build of the CLI by an observation build of itself: the collector/memory counters, the time and peak RSS of
    a real compiler workload on this host, and (diagnostic only) a /usr/bin/sample leaf profile mapped through the
    build's symbol map. Observation must not change behaviour, so the output must equal B."""
    observed = work / "mio-observed"
    symbols = work / "mio-observed.symbols"
    ok, reason, seconds = cli_build(report, "observed-build", cli, "selfhost/cli", "darwin-arm64", observed,
                                    extra=("--observe", "--emit-symbols", str(symbols)))
    if not report.check("observed-cli-build", "PASS" if ok else "FAIL", "native", "" if ok else reason, seconds=round(seconds, 1)):
        return
    output = work / "mio-from-observed"
    log = report.logs / "observed-self-build.log"
    argv = ["/usr/bin/time", "-l", str(observed), "build", "selfhost/cli", "--target=darwin-arm64", "-o", str(output)]
    started = time.monotonic()
    with open(log, "wb") as stream:
        stream.write(("$ %s\n" % " ".join(argv)).encode())
        child = subprocess.Popen(argv, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                 env=dict(os.environ, MIO_BUNKO=str(ROOT / "bunko")))
        sample_text = ""
        compiler = None
        for _ in range(50):
            time.sleep(0.1)
            found = subprocess.run(["pgrep", "-P", str(child.pid)], capture_output=True, text=True).stdout.split()
            if found:
                compiler = found[0]; break
        if compiler and Path("/usr/bin/sample").exists():
            sampled = subprocess.run(["/usr/bin/sample", compiler, "60", "2", "-mayDie"], capture_output=True, text=True, timeout=300)
            sample_text = sampled.stdout
            (report.logs / "observed-self-build.sample.txt").write_text(sample_text + sampled.stderr)
        try:
            out, err = child.communicate(timeout=CLI_BUILD_TIMEOUT)
            code = child.returncode
        except subprocess.TimeoutExpired:
            child.kill(); out, err = child.communicate(); code = None
        stream.write(b"--- stdout\n" + out + b"\n--- stderr\n" + err)
    seconds = time.monotonic() - started
    err = err.decode(errors="replace")
    counters = dict(re.findall(r"mio-gc (\w+)=(\d+)", err))
    timing = dict((name, float(value)) for value, name in re.findall(r"([\d.]+) (real|user|sys)", err))
    same = code == 0 and output.is_file() and sha256(output) == report.hashes.get("cli-B")
    profile = sample_profile(sample_text, observed, symbols) if sample_text else []
    report.check("observed-self-build", "PASS" if same else "FAIL", "native",
                 "" if same else "exit %s or output differs from cli-B: %s" % (code, brief(err)),
                 seconds=round(seconds, 1), peak_rss_mib=peak_rss_mib(err), time=timing, gc=counters, profile=profile)
    print("    profile: time %s rss %d MiB top %s" % (timing, peak_rss_mib(err), profile[:12]), flush=True)


def finish(report, cli=None):
    windows_echo = report.target == "windows-amd64" and cli is not None
    if report.run_examples or windows_echo:
        output = report.output / "examples" if report.run_examples else report.logs / "windows-tcp-echo"

        def inspect_winsock(name, image):
            role = "server" if name == "tcp_echo_server.mio" else "client"
            return windows_tcp_pe(report, image, f"windows-tcp-echo-{role}-pe")

        results = examples_native.run_suite(
            cli, report.target, output, STARTED + BUDGET_SECONDS, tcp_only=not report.run_examples,
            inspect_tcp_image=inspect_winsock if windows_echo else None,
        )
        if windows_echo:
            windows_tcp_echo_checks(report, results, output)
        for item in results if report.run_examples else ():
            details = dict(item)
            name = details.pop("name")
            details["logs"] = {key: "examples/" + path for key, path in details["logs"].items()}
            report.check("example " + name, **details)
    report.save()
    print("%s: %s" % (report.target, report.data["status"]))
    if report.data["failed_required"]:
        print("failed required checks: " + ", ".join(report.data["failed_required"]))
    return 0 if report.data["status"] == "PASS" else 1


# ---------------------------------------------------------------------------------------------------- summary

def summary(args):
    rows, overall, combined = [], "PASS", {}
    found = {}
    for path in sorted(Path(args.reports).rglob("report.json")):
        try:
            data = json.loads(path.read_text())
        except ValueError:
            continue
        if data.get("suite") == "native-platforms":
            found[data.get("target")] = path
    for target in (args.targets.split(",") if args.targets else ("linux-amd64", "darwin-arm64", "windows-amd64")):
        path = found.get(target)
        if path is None:
            overall = "FAIL"
            rows.append("| %s | NOT_RUN | report missing (job failed before writing it or was not scheduled) | NOT_RUN |" % target)
            combined[target] = dict(status="NOT_RUN")
            continue
        data = json.loads(path.read_text())
        combined[target] = data
        if args.run_examples:
            checks = {item["name"]: item for item in data["checks"]}
            excluded = examples_native.exclusions(target)
            names = {"inventory", examples_native.PE_EXECUTION} | examples_native.PROGRAMS.keys() | examples_native.TESTS.keys()
            for name in sorted(names):
                label = "example " + name
                if label not in checks:
                    data["checks"].append(dict(name=label, status="NOT_RUN", engine="native", required=name not in excluded,
                                               reason="example check missing from platform report"))
        data["failed_required"] = [item["name"] for item in data["checks"] if item["required"] and item["status"] != "PASS"]
        status = "FAIL" if data["status"] != "PASS" or data["failed_required"] else "PASS"
        data["status"] = status
        if status != "PASS":
            overall = "FAIL"
        counts = {}
        for item in data["checks"]:
            counts[item["status"]] = counts.get(item["status"], 0) + 1
        rows.append("| %s | %s | %s | %s |" % (target, status,
                    ", ".join("%s %d" % kv for kv in sorted(counts.items())), data["seed"]["status"]))
    jobs = {name: result for name, variable in (("linux", "NATIVE_LINUX_JOB_RESULT"), ("follower", "NATIVE_FOLLOWER_JOB_RESULT"),
                                   ("linux-arm64", "NATIVE_LINUX_ARM64_JOB_RESULT"), ("darwin-amd64", "NATIVE_DARWIN_AMD64_JOB_RESULT"),
                                   ("windows-arm64", "NATIVE_WINDOWS_ARM64_JOB_RESULT"))
            if (result := os.environ.get(variable))}
    if any(result != "success" for result in jobs.values()):
        overall = "FAIL"
    text = ["## Native verification: %s" % overall, "",
            "Stage A is go-hosted (Go reference VM runs the pure-Mio Linux lowering); the trusted GF4 Native Seed is "
            "NOT_RUN. Native execution, builds and static PE checks are recorded separately.", "",
            "| target | status | checks | seed |", "|---|---|---|---|", *rows, ""]
    text.extend(f"- Native {name} job: {result}" for name, result in jobs.items() if result != "success")
    for target, data in combined.items():
        for item in data.get("checks", []):
            if item["status"] != "PASS":
                text.append("- %s `%s` %s (%s): %s" % (target, item["name"], item["status"], item["engine"], item["reason"]))
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    (output / "native-summary.json").write_text(json.dumps(dict(status=overall, jobs=jobs, targets=combined), indent=2) + "\n")
    (output / "native-summary.md").write_text("\n".join(text) + "\n")
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as stream:
            stream.write("\n".join(text) + "\n")
    print("\n".join(text))
    return 0 if overall == "PASS" else 1


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("linux", "linux-arm64", "darwin-amd64", "windows-arm64"):
        p = sub.add_parser(name)
        p.add_argument("--refvm", required=True)
        p.add_argument("--output", required=True)
    for name in ("darwin", "windows"):
        p = sub.add_parser(name)
        p.add_argument("--stage-a", required=True)
        p.add_argument("--expect-sha256", required=True)
        p.add_argument("--output", required=True)
    p = sub.add_parser("summary")
    p.add_argument("--reports", required=True)
    p.add_argument("--output", required=True)
    p.add_argument("--targets", default="", help="comma-separated targets that must all have a report (default: the three release targets)")
    for command in sub.choices.values():
        command.add_argument("--run-examples", action="store_true")
    args = parser.parse_args()
    if args.command == "linux":
        return linux(args)
    if args.command == "linux-arm64":
        return linux(args, "linux-arm64")
    if args.command == "darwin-amd64":
        return linux(args, "darwin-amd64")
    if args.command == "windows-arm64":
        return linux(args, "windows-arm64")
    if args.command == "summary":
        return summary(args)
    return follower(args, TARGETS[args.command])


if __name__ == "__main__":
    sys.exit(main())
