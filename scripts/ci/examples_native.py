#!/usr/bin/env python3
"""Build and verify the example library with a native Mio CLI on the host target."""

import argparse
from contextlib import contextmanager
from dataclasses import dataclass
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import socket
import struct
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parents[2]
TCP = ("tcp_echo_server.mio", "tcp_echo_client.mio")
READY = b"hai\nMio TCP server waiting on 127.0.0.1\n"
PORT_IN_USE = b"iie\nTCP port already in use\n"
PE_GENERATOR = "windows-amd64/hello.mio"
PE_EXECUTION = PE_GENERATOR + " (generated PE)"


@dataclass(frozen=True)
class Expected:
    stdout: bytes
    stderr: bytes = b""
    exit_code: int = 0


# These goldens are literal UTF-8 bytes, including final LF and an empty stderr unless specified.
PROGRAMS = {
    "hello.mio": Expected(b"Hello, Mio!\n"),
    "variables_and_conditionals.mio": Expected(b"passed\n77\n"),
    "control_flow.mio": Expected(b"19\n7\n"),
    "local_scope.mio": Expected(b"local\n2\nglobal\n"),
    "functions.mio": Expected(b"42\n"),
    "recursion.mio": Expected(b"720\n"),
    "nullable.mio": Expected(b"42\nhai\n"),
    "structs_and_enums.mio": Expected(b"7\n75\n24\n0\n"),
    "generics.mio": Expected(b"Mio\nMochi\n42\nhai\n"),
    "closures.mio": Expected(b"1\n2\n1\n"),
    "maps.mio": Expected(b"43\nhai\n"),
    "arrays.mio": Expected(b"4\n99\n99\n1\n2\n"),
    "math.mio": Expected(b"6\n0\n3\n129\n"),
    "strings_and_utf8.mio": Expected("9\n5\nhai\nHello, 澪\n".encode()),
    "path.mio": Expected(b"examples/hello.mio\n.mio\nmath.mio\n"),
    "json.mio": Expected(b'Mio\n{"name":"Mio","version":21}\n'),
    "regexp.mio": Expected(b"hai\niie\n"),
    "sha256.mio": Expected(b"32\n186\n"),
    "filesystem_read.mio": Expected(b"Hello from Mio and Mochi!\n"),
    "os_arguments.mio": Expected("3\nMio and Mochi\n日本語\n".encode()),
    PE_GENERATOR: Expected(b""),
    TCP[0]: Expected(READY + b"3\nhai\n"),
    TCP[1]: Expected(b"hai\n3\nhai\n3\nhai\n77\n105\n111\n"),
}
TESTS = {"tests/basic_test.mio": {"test_arithmetic", "test_nullable"}}
PE_EXPECTED = Expected(b"Mio Windows AMD64\n", exit_code=48)


def host_target():
    host = (platform.system(), platform.machine().lower())
    if host in {("Darwin", "arm64"), ("Darwin", "aarch64")}:
        return "darwin-arm64"
    if host in {("Linux", "aarch64"), ("Linux", "arm64")}:
        return "linux-arm64"
    if host[1] in {"amd64", "x86_64"}:
        return {"Linux": "linux-amd64", "Windows": "windows-amd64"}.get(host[0])
    return None


def tcp_exclusion(target):
    coverage = ROOT / "scripts/ci/bunko_matrix_coverage.json"
    return json.loads(coverage.read_text(encoding="utf-8"))["unsupported"].get("kiso/net/tcp.*", {}).get(target)


def exclusions(target):
    excluded = {}
    if reason := tcp_exclusion(target):
        excluded.update(dict.fromkeys(TCP, reason))
    if target != "windows-amd64":
        excluded[PE_EXECUTION] = "The generated AMD64 PE requires Windows to execute. The host-native generator is checked separately."
    return excluded


def check_inventory(examples):
    found = {path.relative_to(examples).as_posix() for path in examples.rglob("*.mio") if path.is_file()}
    declared = PROGRAMS.keys() | TESTS.keys()
    if found != declared:
        raise ValueError(f"example inventory differs: unlisted={sorted(found - declared)}, missing={sorted(declared - found)}")


def image_path(work, target):
    return work / ("program.exe" if target == "windows-amd64" else "program")


def compare(code, stdout, stderr, expected):
    if not isinstance(expected, Expected):
        raise ValueError("missing expected output")
    if code != expected.exit_code:
        raise ValueError(f"exit {code}, expected {expected.exit_code}")
    for name, actual, wanted in (("stdout", stdout, expected.stdout), ("stderr", stderr, expected.stderr)):
        if actual != wanted:
            raise ValueError(f"{name} mismatch: expected {wanted[:200]!r}, got {actual[:200]!r}")


@contextmanager
def child(argv, cwd, env, stdout, stderr):
    with stdout.open("wb") as out, stderr.open("wb") as err:
        proc = subprocess.Popen([str(arg) for arg in argv], cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=out, stderr=err, start_new_session=os.name != "nt")
        try:
            yield proc
        finally:
            if proc.poll() is None:
                if os.name == "nt":
                    try:
                        subprocess.run(["taskkill", "/PID", str(proc.pid), "/T", "/F"],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
                    except (OSError, subprocess.TimeoutExpired):
                        pass
                    if proc.poll() is None:
                        proc.kill()
                else:
                    try:
                        os.killpg(proc.pid, signal.SIGKILL)
                    except (ProcessLookupError, PermissionError):
                        # macOS can report EPERM for a group containing only unreaped zombies.
                        pass
                proc.wait(timeout=5)


def seconds_left(deadline):
    seconds = deadline - time.monotonic()
    if seconds <= 0:
        raise TimeoutError("time budget exhausted")
    return seconds


def wait(proc, deadline):
    try:
        return proc.wait(timeout=seconds_left(deadline))
    except subprocess.TimeoutExpired as error:
        raise TimeoutError("process timed out") from error


def wait_for_server(proc, stdout, deadline):
    while not stdout.read_bytes().startswith(READY):
        if proc.poll() is not None:
            return False
        time.sleep(min(0.02, seconds_left(deadline)))
    return True


def loopback_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class Suite:
    def __init__(self, cli, target, output, work, deadline, bunko, tcp_only, inspect_tcp_image):
        self.cli, self.target, self.output, self.work, self.deadline = cli, target, output, work, deadline
        self.env = dict(os.environ, MIO_BUNKO=str(bunko))
        self.results = {}
        self.tcp_only, self.inspect_tcp_image = tcp_only, inspect_tcp_image
        excluded = exclusions(target)
        names = TCP if tcp_only else ["inventory", *sorted(PROGRAMS.keys() | TESTS.keys()), PE_EXECUTION]
        for name in names:
            # Preserve path boundaries in the log names, including future nested examples.
            logs = {kind: f"{name}.{kind}" for kind in ("build.log", "stdout", "stderr")} if name != "inventory" else {}
            for path in logs.values():
                log = output / path
                log.parent.mkdir(parents=True, exist_ok=True)
                log.write_bytes(b"")
            self.results[name] = dict(name=name, target=target, engine="native", required=name not in excluded,
                                      status="NOT_APPLICABLE" if name in excluded else "NOT_RUN",
                                      reason=excluded.get(name, "not started"), build_exit_code=None, run_exit_code=None,
                                      image_built=False, seconds=0, logs=logs)

    def save(self):
        results = list(self.results.values())
        ok = all(item["status"] == "PASS" for item in results if item["required"])
        temp = self.output / "results.json.tmp"
        temp.write_text(json.dumps(dict(target=self.target, status="PASS" if ok else "FAIL", examples=results),
                                   indent=2) + "\n", encoding="utf-8")
        temp.replace(self.output / "results.json")
        return ok

    @contextmanager
    def case(self, name, on_success="PASS", reason=""):
        item = self.results[name]
        started = time.monotonic()
        try:
            yield item
        except (OSError, ValueError) as error:
            item.update(status="FAIL", reason=str(error))
        else:
            item.update(status=on_success, reason=reason)
        finally:
            item["seconds"] = round(item["seconds"] + time.monotonic() - started, 3)
            self.save()

    def logs(self, name):
        return {kind: self.output / path for kind, path in self.results[name]["logs"].items()}

    def build(self, name, work):
        image = image_path(work, self.target)
        argv = [self.cli, "build", ROOT / "examples" / name, "--target=" + self.target, "-o", image]
        build_log = self.logs(name)["build.log"]
        deadline = min(self.deadline, time.monotonic() + 600)
        try:
            with child(argv, ROOT, self.env, work / "build.stdout", work / "build.stderr") as proc:
                code = wait(proc, deadline)
                self.results[name]["build_exit_code"] = code
        finally:
            with build_log.open("wb") as log:
                log.write((repr([str(arg) for arg in argv]) + "\n").encode())
                for stream in ("stdout", "stderr"):
                    path = work / ("build." + stream)
                    if path.exists():
                        log.write(f"--- {stream}\n".encode() + path.read_bytes())
        if code != 0 or not image.is_file():
            raise ValueError(f"build exit {code}, image {'present' if image.is_file() else 'missing'}")
        if self.target != "windows-amd64":
            image.chmod(0o755)
        self.results[name]["image_built"] = True
        return image

    def execute(self, name, argv, cwd, deadline):
        logs = self.logs(name)
        with child(argv, cwd, self.env, logs["stdout"], logs["stderr"]) as proc:
            code = wait(proc, deadline)
        self.results[name]["run_exit_code"] = code
        return code, logs["stdout"].read_bytes(), logs["stderr"].read_bytes()

    def standalone(self, name):
        work = self.work / name
        with self.case(name):
            work.mkdir(parents=True)
            image = self.build(name, work)
            args = []
            if name == "filesystem_read.mio":
                fixture = Path("examples/data/greeting.txt")
                (work / fixture.parent).mkdir(parents=True)
                shutil.copyfile(ROOT / fixture, work / fixture)
            if name == "os_arguments.mio":
                args = ["Mio and Mochi", "日本語"]
            compare(*self.execute(name, [image, *args], work, min(self.deadline, time.monotonic() + 30)),
                    PROGRAMS.get(name))
            if name == PE_GENERATOR:
                generated = work / "hello-windows.exe"
                data = generated.read_bytes()
                if len(data) < 64 or data[:2] != b"MZ":
                    raise ValueError("missing DOS header in generated PE")
                pe = struct.unpack_from("<I", data, 60)[0]
                if pe < 64 or data[pe:pe + 6] != b"PE\0\0\x64\x86" or data[pe + 24:pe + 26] != b"\x0b\x02":
                    raise ValueError("generated image is not AMD64 PE32+")
                if PE_EXPECTED.stdout not in data:
                    raise ValueError("generated PE is missing its stdout payload")
        if name == PE_GENERATOR and self.target == "windows-amd64" and self.results[name]["status"] == "PASS":
            with self.case(PE_EXECUTION):
                compare(*self.execute(PE_EXECUTION, [generated], work, min(self.deadline, time.monotonic() + 30)),
                        PE_EXPECTED)

    def mio_test(self):
        # One invocation discovers and executes every test in the example package.
        work = self.work / "tests"
        deadline = min(self.deadline, time.monotonic() + 600)
        first = next(iter(TESTS))
        with self.case(first):
            shutil.copytree(ROOT / "examples/tests", work / "examples/tests")
            code, stdout, stderr = self.execute(first, [self.cli, "test", "examples/tests", "--format=json"],
                                                work, deadline)
            for name in TESTS:
                self.logs(name)["build.log"].write_text("Build and run are combined in mio test. See stdout/stderr.\n",
                                                       encoding="utf-8")
                if name != first:
                    self.results[name]["run_exit_code"] = code
                    self.logs(name)["stdout"].write_bytes(stdout)
                    self.logs(name)["stderr"].write_bytes(stderr)
            if code != 0 or stderr:
                raise ValueError(f"mio test exit {code}, stderr {stderr[:200]!r}")
            try:
                report = json.loads(stdout)
                actual = sorted((Path(work / test["path"]).resolve(), test["name"], test["status"])
                                for package in report["packages"] for test in package["tests"])
                summary = report["summary"]
                counts = (summary["status"], summary["passed"], summary["failed"])
            except (KeyError, TypeError) as error:
                raise ValueError("invalid mio test JSON report") from error
            expected = sorted(((work / "examples" / name).resolve(), test, "passed")
                              for name, tests in TESTS.items() for test in tests)
            if actual != expected or counts != ("passed", len(expected), 0):
                raise ValueError(f"mio test did not discover and pass the expected tests: {actual!r}")
        for name in TESTS:
            if name != first:
                self.results[name].update({key: self.results[first][key] for key in ("status", "reason", "seconds")})

    def tcp(self):
        images = {}
        for name in TCP:
            work = self.work / name
            # Build both even when one fails. A build alone cannot pass the example.
            with self.case(name, on_success="NOT_RUN", reason="waiting for both TCP images to run"):
                work.mkdir()
                image = self.build(name, work)
                if self.inspect_tcp_image and not self.inspect_tcp_image(name, image):
                    raise ValueError("TCP image failed PE inspection")
                images[name] = image
        if len(images) != 2:
            return
        server, client = TCP
        started = time.monotonic()
        deadline = min(self.deadline, started + 30)
        logs = self.logs(server)
        with self.case(server):
            for _ in range(5):
                port = str(loopback_port())
                with child([images[server], port], self.work, self.env, logs["stdout"], logs["stderr"]) as proc:
                    if not wait_for_server(proc, logs["stdout"], deadline):
                        self.results[server]["run_exit_code"] = proc.returncode
                        if (proc.returncode, logs["stdout"].read_bytes(), logs["stderr"].read_bytes()) == (75, PORT_IN_USE, b""):
                            continue
                        raise ValueError(f"TCP server exited before readiness (exit {proc.returncode})")
                    with self.case(client):
                        compare(*self.execute(client, [images[client], port], self.work, deadline), PROGRAMS.get(client))
                    if self.results[client]["status"] != "PASS":
                        raise ValueError("TCP client failed; server stopped")
                    code = wait(proc, deadline)
                    self.results[server]["run_exit_code"] = code
                    compare(code, logs["stdout"].read_bytes(), logs["stderr"].read_bytes(), PROGRAMS.get(server))
                    break
            else:
                raise ValueError("could not bind a loopback port after five address conflicts")

    def run(self, unavailable):
        self.save()
        if not self.tcp_only:
            with self.case("inventory"):
                check_inventory(ROOT / "examples")
            if self.results["inventory"]["status"] != "PASS":
                unavailable = "example inventory is invalid"
        for name, item in self.results.items():
            if item["status"] == "NOT_RUN":
                item["reason"] = unavailable or "platform time budget exhausted before starting"
        if unavailable:
            return self.save()
        if not self.tcp_only:
            for name in PROGRAMS:
                if name not in TCP and time.monotonic() < self.deadline:
                    self.standalone(name)
            if time.monotonic() < self.deadline:
                self.mio_test()
        if self.results[TCP[0]]["required"] and time.monotonic() < self.deadline:
            self.tcp()
        return self.save()


def run_suite(cli, target, output, deadline, bunko=ROOT / "bunko", unavailable="", *, tcp_only=False, inspect_tcp_image=None):
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    if host_target() != target:
        unavailable = f"native {target} execution requires a matching host (found {host_target()})"
    if not cli:
        unavailable = unavailable or "native cli-B did not reach a fixed point"
    with tempfile.TemporaryDirectory(prefix="mio-examples-") as directory:
        suite = Suite(cli, target, output, Path(directory), deadline, bunko, tcp_only, inspect_tcp_image)
        suite.run(unavailable)
    return list(suite.results.values())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", default="mio", help="native Mio CLI executable")
    parser.add_argument("--target", choices=("linux-amd64", "linux-arm64", "darwin-arm64", "windows-amd64"), default=host_target())
    parser.add_argument("--output", type=Path)
    parser.add_argument("--bunko-root", type=Path, default=Path(os.environ.get("MIO_BUNKO", ROOT / "bunko")))
    args = parser.parse_args()
    if not args.target:
        parser.error("this host has no supported native target")
    cli = shutil.which(args.mio)
    output = args.output or ROOT / "build/native" / args.target / "examples"
    results = run_suite(Path(cli).resolve() if cli else None, args.target, output, time.monotonic() + 1800,
                        args.bunko_root.resolve(), unavailable=f"Mio CLI not found: {args.mio}" if not cli else "")
    for item in results:
        print(f'{item["status"]:15} {item["name"]} {item["reason"]}')
    return int(any(item["required"] and item["status"] != "PASS" for item in results))


if __name__ == "__main__":
    raise SystemExit(main())
