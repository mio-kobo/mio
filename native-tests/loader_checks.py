"""Supplemental native loader state-machine checks, separate from Mio execution."""
import ctypes as c


def check(output):
    dll = c.CDLL(str(output / "mio_native_ffi.dll"))

    def bind(name, ret, *args):
        fn = getattr(dll, "mio_ffi_" + name)
        fn.restype, fn.argtypes = ret, list(args)
        return fn

    u64, u32 = c.c_uint64, c.c_uint32
    new = bind("buffer_new", u64, u64)
    free = bind("buffer_free", u32, u64)
    put = bind("buffer_set", u32, u64, u64, c.c_uint8)
    open_lib = bind("open", u64, u64)
    close = bind("close", u32, u64)
    symbol = bind("symbol", u64, u64, u64, u64)
    release = bind("release", u32, u64)
    acquire = bind("acquire", c.c_void_p, u64, u64)
    finish = bind("finish", None, u64)
    error = bind("error", u32)
    resources = bind("resources", u32)

    def text(value):
        token = new(len(value))
        assert token
        for i, b in enumerate(value):
            assert put(token, i, b)
        return token

    # UTF-8 decoding and NUL rejection must happen before LoadLibraryExW.
    for value in (b"\xff", b"./x\0.dll", b"C:drive-relative.dll", b"\\root-relative.dll"):
        token = text(value)
        assert not open_lib(token) and error() == 6
        assert free(token)
    path = text(str(output / "mio_ffi_version.dll").encode("utf-8"))
    lib = open_lib(path)
    assert lib and free(path)
    name = text(b"mio_ffi_abi_version")
    fn = symbol(lib, name, 11)  # uint32, no parameters
    assert fn and free(name)
    address = acquire(fn, 11)
    assert c.CFUNCTYPE(u32)(address)() == 1
    assert not close(lib) and error() == 5
    assert not release(fn) and error() == 5
    finish(fn)
    assert release(fn) and close(lib)
    assert resources() == 0
    # Fixed capacity is a recoverable error; freed slots cannot revive stale IDs.
    tokens = [new(0) for _ in range(1024)]
    assert all(tokens)
    assert not new(0) and error() == 7
    for token in tokens:
        assert free(token)
    again = new(0)
    assert again and again != tokens[0] and not free(tokens[0])
    assert free(again) and resources() == 0
