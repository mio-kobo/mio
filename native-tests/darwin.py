#!/usr/bin/env python3
"""Validate shared fixture Mach-O exports. This is NOT a Mio FFI execution test."""
import argparse
import ctypes as c
import json
from pathlib import Path
import platform
from build import build


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = {"host": platform.platform(), "engine": "C ABI fixture host (ctypes)", "checks": []}

    def record(name, status, **details):
        report["checks"].append(dict(name=name, status=status, **details))
        (output / "report.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
        print(status, name, flush=True)

    record("Darwin Mio dynamic FFI", "NOT_RUN", reason="Darwin backend rejects Windows C prototype metadata; no System V/AAPCS64 native FFI bridge")
    if platform.system() != "Darwin":
        record("Mach-O fixture execution", "NOT_RUN", reason="requires a real macOS host")
        return 2
    try:
        build(output)

        def bind(module, name, result, parameters):
            lib = c.CDLL(str(output / ("libmio_ffi_" + module + ".dylib")))
            fn = getattr(lib, name)
            fn.restype, fn.argtypes = result, parameters
            return fn

        assert bind("math", "mio_add_i32", c.c_int32, [c.c_int32, c.c_int32])(19, 23) == 42
        assert bind("math", "mio_add_i64", c.c_int64, [c.c_int64, c.c_int64])(4000000000, 42) == 4000000042
        assert bind("math", "mio_add_f64", c.c_double, [c.c_double, c.c_double])(1.25, 2.5) == 3.75
        assert bind("math", "mio_multiply_f64", c.c_double, [c.c_double, c.c_double])(2.5, 4) == 10
        assert bind("math", "mio_mixed", c.c_double, [c.c_int32, c.c_double, c.c_uint64, c.c_float, c.c_int64, c.c_double])(1, 2, 3, 4, 5, 6) == 91
        record("math dylib calls", "PASS")
        text = "日本語".encode()
        data, out = c.create_string_buffer(text), c.create_string_buffer(16)
        assert bind("string", "mio_utf8_length", c.c_size_t, [c.c_void_p, c.c_size_t])(data, len(text)) == 3
        assert bind("string", "mio_utf8_output", c.c_size_t, [c.c_void_p, c.c_size_t])(out, 16) == 9
        assert out.raw[:9] == text
        record("string dylib calls", "PASS")
        copy = bind("buffer", "mio_buffer_copy", c.c_size_t, [c.c_void_p, c.c_size_t, c.c_void_p, c.c_size_t])
        assert copy(out, 16, data, 9) == 9 and out.raw[:9] == text
        assert copy(None, 0, None, 0) == 0
        bind("buffer", "mio_buffer_transform", None, [c.c_void_p, c.c_size_t, c.c_uint8])(out, 9, 255)
        assert out.raw[0] == 25
        record("buffer dylib calls", "PASS")
        create = bind("state", "mio_state_new", c.c_void_p, [c.c_int64])
        get = bind("state", "mio_state_get", c.c_int64, [c.c_void_p])
        set_value = bind("state", "mio_state_set", None, [c.c_void_p, c.c_int64])
        free = bind("state", "mio_state_free", None, [c.c_void_p])
        live = bind("state", "mio_state_live", c.c_uint32, [])
        obj = create(41)
        assert obj
        try:
            assert get(obj) == 41 and live() == 1
            set_value(obj, 42)
            assert get(obj) == 42
        finally:
            free(obj)
        assert live() == 0
        record("state dylib calls", "PASS")
        assert bind("version", "mio_ffi_abi_version", c.c_uint32, [])() == 1
        record("version dylib call", "PASS")
        return 0
    except Exception as error:
        record("Mach-O fixture execution", "FAIL", reason=str(error))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
