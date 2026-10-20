#!/usr/bin/env python3
"""M18 native AOT bootstrap. Never reuse an evidence directory or an output.

Go AOT is invoked exactly once, for Seed. Subsequent code generation runs in
a Darwin sandbox denying fork and execution of every other program. Python
only supplies paths, checks files, signs copies, and records provenance.
"""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import time


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    data = path.read_bytes()
    return {"path": str(path), "bytes": len(data),
            "sha256": hashlib.sha256(data).hexdigest()}


def validate_macho(path):
    data = path.read_bytes()
    magic, cpu, subtype, kind, count, size, flags, reserved = struct.unpack_from("<8I", data)
    require((magic, cpu, kind) == (0xFEEDFACF, 0x100000C, 2), "not ARM64 MH_EXECUTE")
    require(32 + size <= 768 <= len(data), "load command bounds")
    cursor, sections, segments, entry = 32, [], [], None
    for _ in range(count):
        command, length = struct.unpack_from("<II", data, cursor)
        require(length >= 8 and length % 8 == 0 and cursor + length <= 32 + size,
                "invalid load command")
        require(command != 0x1D, "unsigned image already contains LC_CODE_SIGNATURE")
        if command == 0x19:
            name, va, vs, off, fs, maximum, initial, nsects, segflags = struct.unpack_from("<16s4Q4I", data, cursor + 8)
            name = name.rstrip(b"\0").decode()
            require(length == 72 + 80 * nsects, "segment command size")
            require(off % 16384 == 0 and off + fs <= len(data), "segment file bounds/alignment")
            require(va % 16384 == 0 and fs <= vs, "segment VM bounds/alignment")
            segments.append({"name": name, "address": va, "size": vs, "offset": off, "file_size": fs})
            for j in range(nsects):
                sn, sg, addr, n, offset, alignment, reloc, nr, sf, r1, r2, r3 = struct.unpack_from("<16s16s2Q8I", data, cursor + 72 + j * 80)
                sn, sg = sn.rstrip(b"\0").decode(), sg.rstrip(b"\0").decode()
                require(sg == name and off <= offset and offset + n <= off + fs, "section file bounds")
                require(addr == va + offset - off and addr + n <= va + vs, "section VA mismatch")
                require(alignment <= 14 and offset % (1 << alignment) == 0, "section alignment")
                require(nr == 0 and reloc == 0, "unresolved relocations")
                sections.append({"name": sn, "segment": sg, "address": addr, "offset": offset, "size": n})
        elif command == 0x80000028:
            entry, stack = struct.unpack_from("<QQ", data, cursor + 8)
        cursor += length
    require(cursor == 32 + size, "load command total")
    text = next(s for s in sections if s["name"] == "__text")
    require(entry == text["offset"] == 768 and text["size"] % 4 == 0, "entry/text layout")
    require(len(data) % 16384 == 0, "file page alignment")
    return {"segments": segments, "sections": sections, "entry": entry,
            "machine_words": text["size"] // 4, "relocations": 0}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--gc-witness", action="store_true", help="also self-compile with a 256 MiB arena and observe real GC using LLDB")
    args = parser.parse_args()
    require(platform.system() == "Darwin" and platform.machine() == "arm64", "requires Darwin ARM64")
    root = Path(__file__).resolve().parent.parent
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=False)
    report = {"status": "RUNNING", "scope": "native bootstrap; repository regressions reported separately",
              "root": str(root), "commands": [], "artifacts": {}, "stages": []}

    def save():
        (evidence / "provenance.json").write_text(json.dumps(report, indent=2) + "\n")

    def run(label, argv, cwd=root, env=None, success=True):
        start = time.monotonic()
        record = {"label": label, "argv": [str(x) for x in argv], "cwd": str(cwd)}
        if env is not None:
            record["environment"] = env
        report["commands"].append(record)
        save()
        with (evidence / (label + ".stdout")).open("wb") as out, (evidence / (label + ".stderr")).open("wb") as err:
            result = subprocess.run(record["argv"], cwd=cwd, env=env, stdout=out, stderr=err, timeout=600)
        record.update(exit_code=result.returncode, wall_seconds=time.monotonic() - start)
        save()
        if success:
            require(result.returncode == 0, f"{label} exit {result.returncode}; see logs")
        return (evidence / (label + ".stdout")).read_bytes(), result.returncode

    def sign(name):
        unsigned, signed = evidence / (name + ".unsigned"), evidence / (name + ".native")
        layout = validate_macho(unsigned)
        report["artifacts"][name] = {**digest(unsigned), "layout": layout}
        shutil.copyfile(unsigned, signed)
        signed.chmod(0o700)
        run(name + "-sign", ["/usr/bin/codesign", "--force", "--sign", "-", signed])
        run(name + "-verify", ["/usr/bin/codesign", "--verify", "--strict", signed])
        report["artifacts"][name].update(signed=digest(signed), codesign_strict=True)
        save()
        return signed

    def generate(name, executable, source, output):
        require(not output.exists(), f"refusing existing output {output}")
        directory = evidence / (name + "-work")
        directory.mkdir()
        (directory / "input.config").write_text(str(source))
        (directory / "output.config").write_text(str(output))
        # JSON quoting also escapes the path for the Scheme string literal.
        profile = '(version 1)(allow default)(deny process-fork)(deny process-exec)(allow process-exec (literal ' + json.dumps(str(executable)) + '))'
        policy = directory / "sandbox.sb"
        policy.write_text(profile + "\n")
        before = digest(source)
        stdout, _ = run(name, ["/usr/bin/sandbox-exec", "-f", policy, executable], directory,
                        {"PATH": "", "HOME": str(directory)})
        require(stdout.startswith(b"native-aot: ok\n") and output.is_file(), f"{name} failed: {stdout!r}")
        require(digest(source) == before, "compiler modified its input")
        layout = validate_macho(output)
        require(stdout == f'native-aot: ok\n{layout["machine_words"]}\n{output.stat().st_size}\n'.encode(), "unexpected compiler diagnostics")
        report["stages"].append({"command": name, "compiler": digest(executable), "input": before,
                                 "output": digest(output), "sandbox": str(policy), "exit_code": 0})
        save()

    try:
        run("git-head", ["git", "rev-parse", "HEAD"])
        run("git-ci-ancestor", ["git", "merge-base", "--is-ancestor", "8b53a25", "HEAD"])
        run("git-status", ["git", "status", "--short"])
        run("git-diff", ["git", "diff", "--binary"])
        files = sorted(set(root.glob("bunko/**/*.mio")) | set(root.glob("selfhost/native-aot/*.mio")) |
                       set(root.glob("internal/**/*.go")) | set(root.glob("cmd/**/*.go")) |
                       {Path(__file__).resolve()})
        (evidence / "source-manifest.json").write_text(json.dumps([digest(f) for f in files], indent=2) + "\n")
        run("build-mio", ["go", "build", "-o", evidence / "mio", "./cmd/mio"])
        run("build-go-aot", ["go", "build", "-o", evidence / "go-aot-seed-only", "./cmd/mochi-aot"])
        source = evidence / "native-aot.mochi"
        run("build-mochi", [evidence / "mio", "build", "-o", source, root / "selfhost/native-aot"])
        require(source.read_bytes()[:7] == b"MOCHI\0\x0e", "self input must be Mochi v14")
        report["mochi"] = digest(source)
        run("build-seed", [evidence / "go-aot-seed-only", "-heap-mib", "1024", "-o", evidence / "seed.unsigned", source])
        seed = sign("seed")
        generate("seed-to-stage1", seed, source, evidence / "stage1.unsigned")
        for i in range(1, 5):
            executable = sign(f"stage{i}")
            generate(f"stage{i}-to-stage{i+1}", executable, source, evidence / f"stage{i+1}.unsigned")
        # Stage 5 is an extra continuation witness; Stage 4 actually executed.
        sign("stage5")
        baseline = (evidence / "stage2.unsigned").read_bytes()
        for i in (1, 3, 4, 5):
            require((evidence / f"stage{i}.unsigned").read_bytes() == baseline, f"native fixed point differs at stage {i}")
        if args.gc_witness:
            # The auxiliary builder changes only the generated image's arena.
            # Stage 4 compiles it; no second Go AOT invocation is permitted.
            helper = evidence / "gc-builder.mio"
            helper.write_text((root / "selfhost/native-aot/main.mio").read_text().replace("1073741824", "268435456"))
            helper_input = evidence / "gc-builder.mochi"
            run("build-gc-mochi", [evidence / "mio", "build", "-o", helper_input, helper])
            generate("stage4-to-gc-builder", evidence / "stage4.native", helper_input, evidence / "gc-builder.unsigned")
            helper_native = sign("gc-builder")
            generate("gc-builder-to-compiler", helper_native, source, evidence / "gc-compiler.unsigned")
            compiler_native = sign("gc-compiler")
            generate("gc-compiler-to-fixed-point", compiler_native, source, evidence / "gc-rebuilt.unsigned")
            sign("gc-rebuilt")
            require((evidence / "gc-rebuilt.unsigned").read_bytes() == baseline, "bounded GC compiler changed the fixed point")
            work = evidence / "gc-debug-work"
            work.mkdir()
            (work / "input.config").write_text(str(source))
            (work / "output.config").write_text(str(work / "image.unsigned"))
            # Resolve the loaded address after launch. Pre-launch raw-address
            # breakpoints can remain unresolved in stripped Mach-O executables.
            exit_at = (evidence / "gc-compiler.unsigned").read_bytes().find(
                struct.pack("<III", 0xD2800000, 0xD2800030, 0xD4001001), 768)
            require(exit_at >= 768, "normal exit sequence missing")
            observation = evidence / "gc-observation.json"
            inspect = ('import json; f = lldb.frame; p = lldb.process; e = lldb.SBError(); '
                       'm = f.FindRegister("x23").GetValueAsUnsigned(); '
                       'r = {"metadata": m, "bump": p.ReadUnsignedFromMemory(m, 8, e), '
                       '"end": p.ReadUnsignedFromMemory(m + 8, 8, e), '
                       '"gc_count": p.ReadUnsignedFromMemory(m + 16, 8, e), '
                       '"shadow_frame": f.FindRegister("x24").GetValueAsUnsigned(), '
                       '"exit_argument": f.FindRegister("x0").GetValueAsUnsigned()}; '
                       'assert e.Success(); open(' + repr(str(observation)) + ', "w").write(json.dumps(r, indent=2))')
            argv = ["/usr/bin/lldb", "--batch", compiler_native]
            for command in ("process launch --stop-at-entry",
                            "script b = lldb.target.GetModuleAtIndex(0).FindSection('__TEXT').GetLoadAddress(lldb.target); print(lldb.target.BreakpointCreateByAddress(b + " + str(exit_at + 4) + "))",
                            "continue", "register read pc x0 x23 x24", "script " + inspect, "continue"):
                argv += ["-o", command]
            run("gc-debug", argv, work)
            counters = json.loads(observation.read_text())
            require(counters["gc_count"] > 0 and counters["shadow_frame"] == 0 and counters["exit_argument"] == 0, "GC/shadow-frame witness failed")
            require(counters["end"] - counters["metadata"] - 288 == 268435456, "wrong bounded arena")
            require((work / "image.unsigned").read_bytes() == baseline, "debug GC output differs")
            report["gc_witness"] = counters
            save()
        # A malformed input must not yield a cached self image, even with exit 0.
        negative = evidence / "invalid-work"
        negative.mkdir()
        bad = negative / "invalid.mochi"
        bad.write_bytes(b"MOCHI\0\x0e")
        (negative / "input.config").write_text(str(bad))
        (negative / "output.config").write_text(str(negative / "must-not-exist"))
        policy = evidence / "stage4-to-stage5-work/sandbox.sb"
        stdout, _ = run("invalid-input", ["/usr/bin/sandbox-exec", "-f", policy, evidence / "stage4.native"], negative, {"PATH": ""})
        require(stdout == b"native-aot: decode failed\n" and not (negative / "must-not-exist").exists(), "invalid input accepted or cached output")
        # Prove the policy mechanism denies a second executable, not merely PATH lookup.
        probe = evidence / "sandbox-probe.sb"
        probe.write_text('(version 1)(allow default)(deny process-fork)(deny process-exec)(allow process-exec (literal "/bin/sh"))\n')
        _, code = run("sandbox-denial-probe", ["/usr/bin/sandbox-exec", "-f", probe, "/bin/sh", "-c", "exec /usr/bin/true"], success=False)
        require(code != 0, "sandbox did not deny child execution")
        report.update(status="BOOTSTRAP_PASS", native_fixed_point=True, go_aot_invocations=1,
                      stage4_executed=True, stage5_continuation=True)
        (evidence / "SHA256SUMS").write_text("".join(
            f'{value["sha256"]}  {name}.unsigned\n' for name, value in report["artifacts"].items()) +
            f'{report["mochi"]["sha256"]}  native-aot.mochi\n')
        save()
        print(f"Native AOT Stage 1=2=3=4=5; evidence: {evidence}")
    except BaseException as problem:
        report.update(status="FAILED", error=str(problem))
        save()
        raise


if __name__ == "__main__":
    main()
