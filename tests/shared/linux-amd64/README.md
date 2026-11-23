# Linux AMD64 shared-library tests

`run.py` is the acceptance driver for `mio build --shared` (documented in
[docs/linux-amd64-shared-libraries.md](../../../docs/linux-amd64-shared-libraries.md)).

* `modules/mio_math`, `mio_bytes`, `mio_text`, `mio_collections`: wrappers over the real `kiso/math`, `kiso/bytes`,
  `kiso/string` and `kiso/array` modules, exporting scalar C ABI functions and opaque handles. `mio_extdep` depends on
  libm (declared with `-lm`).
* `host/host.c`: the C host (`dlopen`, `dlsym`, `dlclose`, calls, register-preservation probe, collector stress).
* `negative/`: sources that must be rejected, with the expected diagnostic.

```sh
python3 tests/shared/linux-amd64/run.py --mio ./mio --baseline ./mio-base --output build/shared
```

Needs a Linux x86-64 host with `readelf`, `nm`, `objdump`, `ldd`, `ld` (or lld) and a C compiler; `file`, `clang` and
`valgrind` are used when installed. Generated `.so` files are written to the output directory only and are never
committed.
