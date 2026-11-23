#!/usr/bin/env python3
"""Compile Mio to PE and actually invoke DLL exports on Windows AMD64."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
from build import build, MODULES, ROOT


def quote(text):
    return json.dumps(str(text).replace("\\", "/"), ensure_ascii=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mio", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--check-embedded", action="store_true", help="also compile using the Full CLI's embedded Bunko")
    for name in ("sqlite", "zlib", "openssl"):
        parser.add_argument("--" + name, type=Path)
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = {"host": platform.platform(), "engine": "native", "status": "NOT_RUN", "checks": []}

    def record(name, status, **details):
        report["checks"].append(dict(name=name, status=status, **details))
        (output / "report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
        print(status, name, flush=True)

    if platform.system() != "Windows" or platform.machine().lower() not in ("amd64", "x86_64"):
        record("Windows Mio execution", "NOT_RUN", reason="requires a real Windows AMD64 host")
        return 2
    ctypes.windll.kernel32.SetErrorMode(0x8003)
    try:
        build(output)
        record("C runtime and five DLL fixtures", "PASS")
    except (RuntimeError, OSError, subprocess.TimeoutExpired) as e:
        record("C build", "FAIL", reason=str(e))
        return 1
    mio = args.mio.resolve()
    report["hashes"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in [mio, *output.glob("*.dll")]}
    from loader_checks import check
    try:
        check(output)
        record("native loader busy, input, capacity and resource checks", "PASS")
    except Exception as e:
        record("native loader state machine", "FAIL", reason=repr(e))
    lines = ['yobu "kiso/ffi"; yobu "kiso/os";',
             'waza expect(ok:bool, label:string)->unit { moshi(!ok){chant(label);os.exit(1);} }']

    def expect(expression, label):
        lines.append(f'expect({expression}, {quote(label)});')

    def library(name, path):
        lines.append(f'kaku {name} = ffi.open({quote(path)});')
        expect(f'{name}.error == uint32(0)', 'open-' + name)

    def function(name, lib, export, ret, params):
        decl = ", ".join(["token:uint64"] + [f"a{i}:{t}" for i, t in enumerate(params)])
        lines.append(f'waza {name}({decl})->{ret} = c "mio.dynamic.dll" "{name}";')
        types = "[" + ",".join(quote(t) for t in params) + "]"
        lines.append(f'kaku {name}_ref = ffi.symbol({lib}.handle,{quote(export)},{quote(ret)},{types});')
        expect(f'{name}_ref.error == uint32(0)', 'symbol-' + name)
        return name + "(" + name + "_ref.handle"

    for m in MODULES:
        library(m, f"./mio_ffi_{m}.dll")
    for name, export, ret, params, values, expected in [
        ("add32", "mio_add_i32", "int32", ["int32", "int32"], "int32(-12),int32(54)", "int32(42)"),
        ("add64", "mio_add_i64", "int64", ["int64", "int64"], "4000000000,42", "4000000042"),
        ("addf", "mio_add_f64", "float64", ["float64", "float64"], "1.25,2.5", "3.75"),
        ("mulf", "mio_multiply_f64", "float64", ["float64", "float64"], "2.5,4.0", "10.0"),
        ("addf32", "mio_add_f32", "float32", ["float32", "float32"], "float32(1.25),float32(2.5)", "float32(3.75)"),
        ("mixed", "mio_mixed", "float64", ["int32", "float64", "uint64", "float32", "int64", "float64"],
         "int32(1),2.0,uint64(3),float32(4.0),5,6.0", "91.0"),
        ("u64", "mio_u64", "uint64", ["uint64"], "uint64(-1)", "uint64(-1)"),
        ("twelve", "mio_twelve", "float64", ["float64", "int32", "float32", "uint64", "float64", "int64", "float32", "uint32", "float64", "int32", "float32", "uint64"],
         "1.0,int32(2),float32(3.0),uint64(4),5.0,6,float32(7.0),uint32(8),9.0,int32(10),float32(11.0),uint64(12)", "78.0"),
    ]:
        call = function(name, "math", export, ret, params)
        expect(call + "," + values + ") == " + expected, name)
    lines.append('kaku repeats=0; aida(repeats<100){expect(add32(add32_ref.handle,int32(19),int32(23))==int32(42),"repeat");repeats=repeats+1;}')
    call = function("version_call", "version", "mio_ffi_abi_version", "uint32", [])
    expect(call + ") == uint32(1)", "ABI version")
    expect('ffi.address(version_call_ref.handle)!=cptr(0)', "function address")
    lines.append('kaku input = ffi.bytes([uint8(230),uint8(151),uint8(165),uint8(230),uint8(156),uint8(172),uint8(232),uint8(170),uint8(158)]);')
    lines.append('kaku output = ffi.buffer_new(uint64(16));')
    function("utf8", "string", "mio_utf8_length", "uint64", ["cptr", "uint64"])
    expect('utf8(utf8_ref.handle,ffi.buffer_data(input),uint64(9))==uint64(3)', "UTF8 input")
    function("textout", "string", "mio_utf8_output", "uint64", ["cptr", "uint64"])
    expect('textout(textout_ref.handle,ffi.buffer_data(output),uint64(8))==uint64(9)', "short string buffer")
    expect('ffi.buffer_get(output,uint64(0))==uint8(0)', "short buffer untouched")
    expect('textout(textout_ref.handle,ffi.buffer_data(output),uint64(16))==uint64(9)', "UTF8 output")
    function("copy", "buffer", "mio_buffer_copy", "uint64", ["cptr", "uint64", "cptr", "uint64"])
    expect('copy(copy_ref.handle,ffi.buffer_data(output),uint64(16),ffi.buffer_data(input),uint64(9))==uint64(9)', "buffer copy")
    lines.append('kaku i=0; aida(i<9){expect(ffi.buffer_get(input,uint64(i))==ffi.buffer_get(output,uint64(i)),"bytes");i=i+1;}')
    expect('copy(copy_ref.handle,cptr(0),uint64(0),cptr(0),uint64(0))==uint64(0)', "empty buffer")
    expect('copy(copy_ref.handle,ffi.buffer_data(output),uint64(8),ffi.buffer_data(input),uint64(9))==uint64(0)', "buffer boundary")
    function("transform", "buffer", "mio_buffer_transform", "unit", ["cptr", "uint64", "uint8"])
    lines.append('transform(transform_ref.handle,ffi.buffer_data(output),uint64(9),uint8(255));')
    expect('ffi.buffer_get(output,uint64(0))==uint8(25)', "buffer transform")
    function("state_new", "state", "mio_state_new", "cptr", ["int64"])
    function("state_get", "state", "mio_state_get", "int64", ["cptr"])
    function("state_set", "state", "mio_state_set", "unit", ["cptr", "int64"])
    function("state_free", "state", "mio_state_free", "unit", ["cptr"])
    function("state_live", "state", "mio_state_live", "uint32", [])
    lines.append('kaku object=state_new(state_new_ref.handle,41); expect(object!=cptr(0),"state allocation");')
    expect('state_get(state_get_ref.handle,object)==41', "state initial")
    lines.append('state_set(state_set_ref.handle,object,42);')
    expect('state_get(state_get_ref.handle,object)==42', "state update")
    expect('state_live(state_live_ref.handle)==uint32(1)', "state static")
    lines.append('state_free(state_free_ref.handle,object);')
    expect('state_live(state_live_ref.handle)==uint32(0)', "state free")
    for lib, export, name, ret, params, values, test in [
        ("kernel32.dll", "GetCurrentProcessId", "pid", "uint32", [], "", ">uint32(0)"),
        ("user32.dll", "IsWindow", "window", "int32", ["cptr"], ",cptr(0)", "==int32(0)"),
        ("bcrypt.dll", "BCryptGenRandom", "random", "int32", ["cptr", "cptr", "uint32", "uint32"],
         ",cptr(0),ffi.buffer_data(output),uint32(16),uint32(2)", "==int32(0)"),
    ]:
        library(name + "lib", lib)
        call = function(name, name + "lib", export, ret, params)
        expect(call + values + ")" + test, lib + " actual call")
        lines.append(f"ffi.close({name}lib.handle);")
    # Real installed third-party libraries: omitted dependencies remain explicit NOT_RUN.
    for name in ("sqlite", "zlib", "openssl"):
        path = getattr(args, name)
        if not path:
            record(name, "NOT_RUN", reason="supply --" + name + " with a locally installed AMD64 DLL")
            continue
        library(name, path.resolve())
        if name == "sqlite":
            # Some distributors compile SQLITE_OMIT_AUTOINIT. Initialize explicitly.
            function("sqlite_initialize", name, "sqlite3_initialize", "int32", [])
            expect('sqlite_initialize(sqlite_initialize_ref.handle)==int32(0)', "SQLite initialize")
            function("sqlite_version", name, "sqlite3_libversion", "cptr", [])
            expect('sqlite_version(sqlite_version_ref.handle)!=cptr(0)', "SQLite version")
            function("sqlite_open", name, "sqlite3_open", "int32", ["cptr", "cptr"])
            function("sqlite_close", name, "sqlite3_close", "int32", ["cptr"])
            lines.append('kaku dbname=ffi.bytes([uint8(58),uint8(109),uint8(101),uint8(109),uint8(111),uint8(114),uint8(121),uint8(58)]); kaku dbout=ffi.buffer_new(uint64(8));')
            expect('sqlite_open(sqlite_open_ref.handle,ffi.buffer_data(dbname),ffi.buffer_data(dbout))==int32(0)', "SQLite open")
            expect('ffi.buffer_pointer(dbout)!=cptr(0)', "SQLite handle")
            expect('sqlite_close(sqlite_close_ref.handle,ffi.buffer_pointer(dbout))==int32(0)', "SQLite close")
            function("sqlite_shutdown", name, "sqlite3_shutdown", "int32", [])
            expect('sqlite_shutdown(sqlite_shutdown_ref.handle)==int32(0)', "SQLite shutdown")
            lines.append('ffi.buffer_free(dbname);ffi.buffer_free(dbout);')
        elif name == "openssl":
            function("ssl_version", name, "OpenSSL_version", "cptr", ["int32"])
            expect('ssl_version(ssl_version_ref.handle,int32(0))!=cptr(0)', "OpenSSL version")
        else:
            # Windows is LLP64: zlib uLong is uint32, including length out parameters.
            function("bound", name, "compressBound", "uint32", ["uint32"])
            expect('bound(bound_ref.handle,uint32(9))>uint32(9)', "zlib bound")
            for func in ("compress", "uncompress"):
                function("z" + func, name, func, "int32", ["cptr", "cptr", "cptr", "uint32"])
            lines.append('kaku compressed=ffi.buffer_new(uint64(64)); kaku length=ffi.buffer_new(uint64(4));ffi.buffer_set(length,uint64(0),uint8(64));')
            expect('zcompress(zcompress_ref.handle,ffi.buffer_data(compressed),ffi.buffer_data(length),ffi.buffer_data(input),uint32(9))==int32(0)', "zlib compress")
            lines.append('kaku compressed_length=uint32(ffi.buffer_get(length,uint64(0)));ffi.buffer_set(length,uint64(0),uint8(16));')
            expect('zuncompress(zuncompress_ref.handle,ffi.buffer_data(output),ffi.buffer_data(length),ffi.buffer_data(compressed),compressed_length)==int32(0)', "zlib uncompress")
            expect('ffi.buffer_get(length,uint64(0))==uint8(9)', "zlib output length")
            lines.append('kaku z=0;aida(z<9){expect(ffi.buffer_get(input,uint64(z))==ffi.buffer_get(output,uint64(z)),"zlib roundtrip");z=z+1;}ffi.buffer_free(compressed);ffi.buffer_free(length);')
        lines.append(f"ffi.close({name}.handle);")
    unicode_dir = output / "资料 with spaces"
    unicode_dir.mkdir(exist_ok=True)
    shutil.copyfile(output / "mio_ffi_version.dll", unicode_dir / "version.dll")
    for name, path in (("relative_unicode", "./资料 with spaces/version.dll"), ("absolute_unicode", unicode_dir / "version.dll")):
        library(name, path)
        call = function(name + "call", name, "mio_ffi_abi_version", "uint32", [])
        expect(call + ") == uint32(1)", name + " call")
        lines.append(f"ffi.close({name}.handle);")
    (output / "wrong.dll").write_bytes(b"not a PE dynamic library")
    for path in ("./missing.dll", "./wrong.dll", "mio_ffi_math.dll"):
        # The last name must NOT search application/CWD: bare names are System32-only.
        lines.append(f'kaku missing_{len(lines)} = ffi.open({quote(path)});')
        last = "missing_" + str(len(lines) - 1)
        expect(f'{last}.error==uint32(2) && {last}.os_error!=uint32(0)', "load error " + path)
    lines.append('kaku absent=ffi.symbol(math.handle,"absent_export","unit",[]);')
    expect('absent.error==uint32(3) && absent.os_error!=uint32(0)', "missing export")
    lines.append('kaku data_export=ffi.symbol(version.handle,"mio_exported_data","uint32",[]);')
    expect('data_export.error==uint32(3)', "non-executable export rejected")
    lines.append('kaku unsupported=ffi.symbol(math.handle,"mio_add_i32","string",[]);')
    expect('unsupported.error==uint32(4)', "unsupported type")
    expect('ffi.check(add32_ref.handle,ffi.signature("float64",["int32","int32"]))==uint32(0)', "signature mismatch")
    expect('ffi.release(addf_ref.handle)==uint32(1)', "release symbol")
    expect('ffi.release(addf_ref.handle)==uint32(0)', "double release symbol")
    expect('ffi.buffer_set(output,uint64(16),uint8(1))==uint32(0)', "buffer bounds")
    for m in MODULES:
        expect(f'ffi.close({m}.handle)==uint32(1)', 'close-' + m)
        expect(f'ffi.close({m}.handle)==uint32(0)', 'double-close-' + m)
    expect('ffi.check(add32_ref.handle,ffi.signature("int32",["int32","int32"]))==uint32(0)', "use after close")
    lines.append('kaku iteration=0; aida(iteration<1100){kaku lib=ffi.open("./mio_ffi_version.dll");expect(lib.error==uint32(0),"reopen");kaku fn=ffi.symbol(lib.handle,"mio_ffi_abi_version","uint32",[]);expect(fn.error==uint32(0),"resolve repeat");expect(version_call(fn.handle)==uint32(1),"repeated version call");expect(ffi.close(lib.handle)==uint32(1),"reclose");iteration=iteration+1;}')
    expect('ffi.check(version_call_ref.handle,ffi.signature("uint32",[]))==uint32(0)', "stale generation after reuse")
    expect('ffi.address(version_call_ref.handle)==cptr(0)', "stale address rejected")
    lines.append('ffi.buffer_free(input);expect(ffi.buffer_free(output)==uint32(1),"free buffer");expect(ffi.buffer_free(output)==uint32(0),"double free buffer");expect(ffi.resources()==uint32(0),"native resource leak");chant("PASS");')
    source = "\n".join(lines)

    def run_case(name, source, expected_code=0, expected_out=b"PASS\n", extra=(), embedded=False):
        src, exe = output / (name + ".mio"), output / (name + ".exe")
        src.write_text(source, encoding="utf-8")
        exe.unlink(missing_ok=True)
        command = [str(mio), "build", str(src), "--target=windows-amd64", "-o", str(exe), *extra]
        environment = dict(os.environ)
        if embedded:
            environment.pop("MIO_BUNKO", None)
            environment.pop("MIO_BUNKO_ROOT", None)
        else:
            command += ["--bunko-root", str(ROOT / "bunko")]
        proc = subprocess.run(command, capture_output=True, timeout=300, cwd=output if embedded else ROOT, env=environment)
        (output / (name + ".build.log")).write_bytes(proc.stdout + proc.stderr)
        (output / (name + ".command.json")).write_text(json.dumps(command, indent=2), encoding="utf-8")
        record(name + " build", "PASS" if proc.returncode == 0 and exe.is_file() else "FAIL", exit_code=proc.returncode)
        if proc.returncode or not exe.is_file():
            return
        # A different CWD proves relative loads use the executable directory.
        proc = subprocess.run([str(exe)], cwd=ROOT, capture_output=True, timeout=120)
        (output / (name + ".stdout.log")).write_bytes(proc.stdout)
        (output / (name + ".stderr.log")).write_bytes(proc.stderr)
        actual = proc.stdout.replace(b"\r\n", b"\n")
        ok = proc.returncode == expected_code and actual == expected_out
        if expected_code == 125:
            ok = ok and b"E_FFI_CALL" in proc.stderr
        record(name + " native", "PASS" if ok else "FAIL", exit_code=proc.returncode)

    run_case("fixtures", source)
    run_case("fixtures-stress", source, extra=("--gc-stress",))
    if args.check_embedded:
        run_case("embedded-fixtures", source, embedded=True)
    prefix = 'yobu "kiso/ffi"; waza invoke(token:uint64,a:int32,b:int32)->int32=c "mio.dynamic.dll" "invoke";'
    for name, setup in {
        "invalid-token": 'kaku token=uint64(123);',
        "closed-token": 'kaku l=ffi.open("./mio_ffi_math.dll");kaku f=ffi.symbol(l.handle,"mio_add_i32","int32",["int32","int32"]);kaku token=f.handle;ffi.close(l.handle);',
        "wrong-signature": 'kaku l=ffi.open("./mio_ffi_math.dll");kaku f=ffi.symbol(l.handle,"mio_add_f64","float64",["float64","float64"]);kaku token=f.handle;',
    }.items():
        run_case(name, prefix + setup + 'chant(invoke(token,int32(1),int32(2)));', 125, b"")
    # Includes the imported uint64/cptr struct-field regression that the new API exposed.
    run_case("imported-scalars", '''yobu "kiso/ffi";
        waza identity(x:uint64)->uint64 {kaesu x;}
        kaku result=ffi.Result{handle:uint64(-1),error:uint32(0),os_error:uint32(0)};
        moshi(identity(result.handle)!=uint64(-1)){chant("FAIL");}sore{chant("PASS");}''')
    for name, declaration in {
        "missing-token": 'waza bad()->int32=c "mio.dynamic.dll" "bad";',
        "pointer-token": 'waza bad(token:cptr)->int32=c "mio.dynamic.dll" "bad";',
        "too-many": 'waza bad(token:uint64,' + ','.join(f'x{i}:int64' for i in range(13)) + ')->int32=c "mio.dynamic.dll" "bad";',
        "managed-argument": 'waza bad(token:uint64,x:string)->int32=c "mio.dynamic.dll" "bad";',
    }.items():
        src = output / (name + ".mio")
        src.write_text(declaration, encoding="utf-8")
        proc = subprocess.run([str(mio), "build", str(src), "--bunko-root", str(ROOT / "bunko"), "--target=windows-amd64", "-o", str(output / (name + ".exe"))], capture_output=True, timeout=60)
        (output / (name + ".build.log")).write_bytes(proc.stdout + proc.stderr)
        record(name + " diagnostic", "PASS" if proc.returncode != 0 and b"E_FFI_" in proc.stdout + proc.stderr else "FAIL")
    for name in ("sqlite", "zlib", "openssl"):
        if getattr(args, name):
            record(name + " Mio calls", "PASS" if all(c["status"] == "PASS" for c in report["checks"] if c["name"].startswith("fixtures")) else "FAIL")
    failed = any(c["status"] == "FAIL" for c in report["checks"])
    report["status"] = "FAIL" if failed else "PASS"
    report["optional_not_run"] = [c["name"] for c in report["checks"] if c["status"] == "NOT_RUN"]
    (output / "report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
