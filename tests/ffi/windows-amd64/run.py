#!/usr/bin/env python3
"""用 Native CLI 编译并执行真实 Windows AMD64 PE；没有 VM 或模拟器回退。"""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
sys.path.insert(0, str(ROOT / "tests/windows-amd64"))
from verify_pe import parse, read_imports, verify

EXPECTED = {
    "scalars": "42\n42\n19\nhai\nhai\n140\nhai\n2\n42\n142\n174\n-128\n255\n-32768\n65535\n-2147483648\n4294967295\n",
    "pointers": "hai\n123456789\n1\n0\n-81985529216486896\nhai\n-1\n",
    "system": "hai\nhai\nhai\nhai\n12345\n42\n",
    "heap": "live GC root\n42\n",
    "registers": "103200\n42\n52\n",
    "winsock": "hai\nhai\n",
    "scalar_backend": "42\n140\n42\n123456789\n",
    "float_bits": "1\n1\n",
}
NEGATIVE = {
    "variadic": 'waza bad(x: int64, ...) -> int64 = c "x.dll"; bad(1);',
    "struct": 'kata Box { n:int64; } waza bad(x: Box) -> unit = c "x.dll";',
    "array": 'waza bad(x: []uint8) -> unit = c "x.dll";',
    "string": 'waza bad(x: string) -> unit = c "x.dll";',
    "callback": 'waza bad(x: waza() -> unit) -> unit = c "x.dll";',
    "cpp": 'waza bad() -> unit = cpp "x.dll";',
    "bool": 'waza bad(x: bool) -> unit = c "x.dll";',
    "void_parameter": 'waza bad(x: unit) -> unit = c "x.dll";',
    "return_type": 'waza bad() -> string = c "x.dll";',
    "missing_return": 'waza bad() = c "x.dll";',
    "missing_parameter": 'waza bad(x) -> unit = c "x.dll";',
    "missing_dll": 'waza bad() -> unit = c;',
    "empty_dll": 'waza bad() -> unit = c "";',
    "invalid_dll": 'waza bad() -> unit = c "../x.dll";',
    "invalid_export": 'waza bad() -> unit = c "x.dll" "a|b";',
    "duplicate_name": 'waza bad() -> unit = c "x.dll"; waza bad() -> unit = c "x.dll";',
    "conflicting_export": 'waza a() -> int32 = c "x.dll" "f"; waza b() -> float64 = c "X.DLL" "f";',
    "argument_count": 'waza f(x: int64) -> unit = c "x.dll"; f();',
    "argument_type": 'waza f(x: int64) -> unit = c "x.dll"; f(1.0);',
    "return_mismatch": 'waza f() -> float64 = c "x.dll"; kaku n:int64=f();',
    "gc_pointer": 'waza f(x: cptr) -> unit = c "x.dll"; f("GC object");',
    "pointer_cast": 'kaku pointer = cptr(123);',
    "pointer_gc_cast": 'kaku pointer = cptr([1,2]);',
    "generic": 'waza f[T](x:T) -> unit = c "x.dll";',
}


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def c_tools():
    """优先使用现有开发环境，否则只读取已安装的 VS / SDK；不安装工具。"""
    env = dict(os.environ)
    cl = shutil.which("cl") or shutil.which("clang-cl")
    ml = shutil.which("ml64")
    if cl and env.get("INCLUDE") and env.get("LIB"):
        return cl, ml, env
    vswhere = Path(os.environ.get("ProgramFiles(x86)", "C:/Program Files (x86)")) / "Microsoft Visual Studio/Installer/vswhere.exe"
    if not vswhere.is_file():
        return None, None, env
    found = subprocess.run([str(vswhere), "-latest", "-products", "*", "-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath"], capture_output=True, text=True, timeout=30)
    installation = found.stdout.strip()
    if not installation:
        return None, None, env
    versions = sorted((Path(installation) / "VC/Tools/MSVC").glob("*"))
    sdk = Path(os.environ.get("ProgramFiles(x86)", "C:/Program Files (x86)")) / "Windows Kits/10"
    sdks = sorted(p for p in (sdk / "Include").glob("*") if (p / "ucrt").is_dir())
    if not versions or not sdks:
        return None, None, env
    vc, headers = versions[-1], sdks[-1]
    cl, ml = vc / "bin/Hostx64/x64/cl.exe", vc / "bin/Hostx64/x64/ml64.exe"
    env["INCLUDE"] = ";".join(map(str, [vc / "include", headers / "ucrt", headers / "shared", headers / "um"]))
    env["LIB"] = ";".join(map(str, [vc / "lib/x64", sdk / "Lib" / headers.name / "ucrt/x64", sdk / "Lib" / headers.name / "um/x64"]))
    return str(cl) if cl.is_file() else None, str(ml) if ml.is_file() else None, env


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--check-embedded", action="store_true", help="also verify Full CLI defaults without an external Bunko tree")
    args = parser.parse_args()
    output, mio = args.output.resolve(), args.mio.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = {"suite": "windows-amd64-c-ffi", "target": "windows-amd64", "engine": "native", "host": platform.platform(), "checks": [], "hashes": {}}
    (output / "report.json").write_text(json.dumps(dict(report, status="NOT_RUN")), encoding="utf-8")

    def record(name, status, **details):
        report["checks"].append(dict(name=name, status=status, **details))
        statuses = {c["status"] for c in report["checks"]}
        report["status"] = "FAIL" if "FAIL" in statuses else "NOT_RUN" if statuses & {"SKIP", "NOT_RUN"} else "PASS"
        (output / "report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        print(status, name, flush=True)

    def run(label, argv, env=None, cwd=ROOT):
        try:
            proc = subprocess.run(list(map(str, argv)), cwd=cwd, env=env, capture_output=True, timeout=300)
            code, out, err = proc.returncode, proc.stdout, proc.stderr
        except subprocess.TimeoutExpired as e:
            code, out, err = None, e.stdout or b"", (e.stderr or b"") + b"\nTIMEOUT"
        except OSError as e:
            code, out, err = None, b"", str(e).encode("utf-8")
        (output / (label + ".stdout.log")).write_bytes(out)
        (output / (label + ".stderr.log")).write_bytes(err)
        (output / (label + ".command.json")).write_text(json.dumps(dict(argv=list(map(str, argv)), exit_code=code), indent=2), encoding="utf-8")
        return code, out, err

    def build(label, source, extra=()):
        image = output / (label + ".exe")
        image.unlink(missing_ok=True)
        code, out, err = run(label + "-build", [mio, "build", source, "--bunko-root", ROOT / "bunko", "--target=windows-amd64", "-o", image, *extra])
        good = code == 0 and image.is_file()
        record(label + "-build", "PASS" if good else "FAIL", exit_code=code)
        if good:
            report["hashes"][image.name] = sha(image)
        return image if good else None

    if platform.system() != "Windows" or platform.machine().lower() not in ("amd64", "x86_64"):
        record("native-host", "NOT_RUN", reason="requires real Windows AMD64")
        return 2
    if not mio.is_file():
        record("native-cli", "NOT_RUN", reason="native compiler is missing")
        return 2
    # Loader 负向用例必须返回错误码，不能弹出阻塞 CI 的 Windows 错误对话框。
    ctypes.windll.kernel32.SetErrorMode(0x8003)
    try:
        parse(mio.read_bytes())
    except (OSError, ValueError, struct.error) as error:
        record("native-cli", "FAIL", reason=str(error))
        return 1
    report["hashes"]["compiler"] = sha(mio)
    record("native-host", "PASS")
    cl, ml, env = c_tools()
    if not cl:
        record("c-fixture-build", "SKIP", reason="MSVC or clang-cl with Windows SDK is unavailable")
        record("native-c-calls", "NOT_RUN", reason="C fixture is unavailable")
        return 2
    dll = output / "mio_ffi_fixture.dll"
    dll.unlink(missing_ok=True)
    obj = output / "probes.obj"
    objects, exports = [], []
    if ml:
        code, _, _ = run("asm-fixture", [ml, "/nologo", "/c", "/Fo" + str(obj), HERE / "fixtures/probes.asm"], env)
        record("asm-fixture", "PASS" if code == 0 else "FAIL")
        if code != 0:
            return 1
        objects = [obj, HERE / "fixtures/unwind.c"]
        exports = ["/EXPORT:mio_c_clobber", "/EXPORT:mio_c_unwind"]
    else:
        record("register-unwind-probes", "SKIP", reason="ml64 is unavailable")
    code, _, _ = run("c-fixture", [cl, "/nologo", "/utf-8", "/LD", "/O2", "/W4", "/Fo" + str(output) + os.sep,
                                  HERE / "fixtures/mio_ffi_fixture.c", *objects, "/link", "/OUT:" + str(dll), "/IMPLIB:" + str(output / "fixture.lib"), *exports], env)
    record("c-fixture-build", "PASS" if code == 0 and dll.is_file() else "FAIL", compiler=cl)
    if code != 0 or not dll.is_file():
        return 1
    report["hashes"][dll.name] = sha(dll)
    if args.check_embedded:
        isolated = output / "embedded"
        isolated.mkdir(exist_ok=True)
        source, image = isolated / "main.mio", isolated / "main.exe"
        source.write_text('yobu "kiso/encoding/win64c"; waza f() -> int32 = c "mio_ffi_fixture.dll" "mio_c_no_args"; chant(win64c.descriptor("float32")); chant(f());', encoding="utf-8")
        image.unlink(missing_ok=True)
        shutil.copyfile(dll, isolated / dll.name)
        environment = dict(os.environ)
        environment.pop("MIO_BUNKO", None)
        environment.pop("MIO_BUNKO_ROOT", None)
        code, _, _ = run("embedded-build", [mio, "build", source, "--target=windows-amd64", "-o", image], env=environment, cwd=isolated)
        record("embedded-build", "PASS" if code == 0 and image.is_file() else "FAIL")
        if image.is_file():
            report["hashes"]["embedded/main.exe"] = sha(image)
            code, out, err = run("embedded-run", [image], env=environment, cwd=isolated)
            record("embedded-native", "PASS" if (code,out,err)==(0,b"3\n42\n",b"") else "FAIL", exit_code=code)
    if ml:
        builder = build("register-probe-builder", HERE / "backend_probe.mio")
        if builder:
            image = output / "register-probe.exe"
            image.unlink(missing_ok=True)
            code, _, _ = run("register-probe-generate", [builder, image])
            record("register-probe-generate", "PASS" if code == 0 and image.is_file() else "FAIL")
            if image.is_file():
                report["hashes"][image.name] = sha(image)
                code, out, err = run("register-probe-run", [image])
                record("all-nonvolatile-registers", "PASS" if (code, out, err) == (42, b"", b"") else "FAIL", exit_code=code)
    # 标量后端也使用共享桥；它原有的浮点表达式限制继续明确拒绝。
    builder = build("scalar-builder", HERE / "scalar_builder.mio")
    if builder:
        bytecode = output / "scalar.mochi"
        bytecode.unlink(missing_ok=True)
        code, _, _ = run("scalar-mochi", [mio, "build", HERE / "cases/scalar_backend.mio", "--emit=mochi", "--bunko-root", ROOT / "bunko", "-o", bytecode])
        record("scalar-mochi", "PASS" if code == 0 and bytecode.is_file() else "FAIL")
        if bytecode.is_file():
            image = output / "lowerwin.exe"
            image.unlink(missing_ok=True)
            code, _, _ = run("lowerwin-generate", [builder, bytecode, image])
            record("lowerwin-generate", "PASS" if code == 0 and image.is_file() else "FAIL")
            if image.is_file():
                report["hashes"][image.name] = sha(image)
                code, out, err = run("lowerwin-run", [image])
                record("lowerwin-native", "PASS" if (code, out, err) == (0, EXPECTED["scalar_backend"].encode(), b"") else "FAIL", exit_code=code)
    for name, expected in EXPECTED.items():
        if name == "registers" and not ml:
            continue
        for stress in (False, True):
            label = name + ("-stress" if stress else "")
            image = build(label, HERE / "cases" / (name + ".mio"), ["--gc-stress"] if stress else [])
            if image is None:
                continue
            data = image.read_bytes()
            try:
                structure = verify(data, expect_relocations=False)
                imports = read_imports(data, parse(data))
                assert len({x.lower() for x in imports}) == len(imports), "duplicate DLL descriptor"
                assert "this_dll_must_not_be_imported.dll" not in imports, "unused declaration was imported"
                if name == "system":
                    assert len(imports) == 2
                    assert sum("GetCurrentProcessId" in symbols for symbols in imports.values()) == 1
                record(label + "-pe", "PASS", imports={dll: list(symbols) for dll, symbols in imports.items()}, unwind_functions=structure["functions"])
            except (ValueError, AssertionError) as error:
                record(label + "-pe", "FAIL", reason=str(error))
            code, out, err = run(label + "-run", [image])
            record(label + "-native", "PASS" if (code, out, err) == (0, expected.encode(), b"") else "FAIL", exit_code=code)
    image = build("cross-package", HERE / "cases/cross_package/main.mio")
    if image:
        code, out, err = run("cross-package-run", [image])
        record("cross-package-native", "PASS" if (code, out, err) == (0,b"42\n",b"") else "FAIL", exit_code=code)
    declarations = ['waza a() -> int32 = c "mio_ffi_fixture.dll" "mio_c_no_args";',
                    'waza b() -> int32 = c "mio_ffi_fixture.dll" "mio_c_calls";',
                    'waza p() -> uint32 = c "kernel32.dll" "GetCurrentProcessId";']
    ordered_imports = []
    for label, declarations_in_order in (("order-a", declarations), ("order-b", declarations[::-1])):
        source = output / (label + ".mio")
        source.write_text("\n".join(declarations_in_order) + '\nchant(a()); chant(b()); chant(p()==p());', encoding="utf-8")
        image = build(label, source)
        if image:
            imports = read_imports(image.read_bytes(), parse(image.read_bytes()))
            ordered_imports.append([(dll, list(symbols)) for dll, symbols in imports.items()])
            code, out, err = run(label + "-native", [image])
            record(label + "-native", "PASS" if (code, out, err) == (0,b"42\n0\nhai\n",b"") else "FAIL")
    record("deterministic-import-order", "PASS" if len(ordered_imports)==2 and ordered_imports[0]==ordered_imports[1] else "FAIL")
    for name, source in NEGATIVE.items():
        path = output / ("negative-" + name + ".mio")
        path.write_text(source, encoding="utf-8")
        image = output / ("negative-" + name + ".exe")
        image.unlink(missing_ok=True)
        code, out, err = run("negative-" + name, [mio, "build", path, "--target=windows-amd64", "--bunko-root", ROOT / "bunko", "-o", image])
        record("reject-" + name, "PASS" if code not in (None, 0) and b"error[" in out + err and not image.exists() else "FAIL", exit_code=code)
    source = output / "wrong-target.mio"
    source.write_text('waza pid() -> uint32 = c "kernel32.dll" "GetCurrentProcessId"; chant(pid());', encoding="utf-8")
    for target in ("linux-amd64", "darwin-arm64", "windows-arm64", "linux-arm64", "darwin-amd64"):
        image = output / ("wrong-target-" + target)
        image.unlink(missing_ok=True)
        code, out, err = run("reject-" + target, [mio, "build", source, "--target=" + target, "--bunko-root", ROOT / "bunko", "-o", image])
        record("reject-" + target, "PASS" if code not in (None, 0) and b"E_FFI_TARGET" in out + err and not image.exists() else "FAIL", exit_code=code)
    # DLL/导出是否存在属于 Windows loader 的职责，不能虚构为编译期类型诊断。
    for label, dll_name, symbol, expected in (("missing-dll", "mio_ffi_missing_98af.dll", "f", 0xC0000135), ("missing-export", "mio_ffi_fixture.dll", "mio_ffi_missing_98af", 0xC0000139)):
        path = output / (label + ".mio")
        path.write_text(f'waza f() -> unit = c "{dll_name}" "{symbol}"; f();', encoding="utf-8")
        image = build(label, path)
        if image:
            code, _, _ = run(label + "-loader", [image])
            record(label + "-loader", "PASS" if code is not None and code & 0xFFFFFFFF == expected else "FAIL", exit_code=code)
    return 0 if report["status"] == "PASS" else 1 if report["status"] == "FAIL" else 2


if __name__ == "__main__":
    sys.exit(main())
