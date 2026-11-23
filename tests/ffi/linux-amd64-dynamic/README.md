# Linux AMD64 shared-library tests

End-to-end tests for loading native `.so` modules from Mio programs (see [docs/linux-dynamic-libraries.md](../../../docs/linux-dynamic-libraries.md)).

```sh
python3 tests/ffi/linux-amd64-dynamic/run.py --mio /path/to/native/mio --output build/ffi-dynamic --bench
```

* `modules/` C sources of the native modules (math, buffer, state, zlib, SQLite, OpenSSL, dependency pair). Built with `cc -fPIC -shared` into the output directory; no binaries are committed.
* `ffi/` the Mio `ffi` package (open, symbol, typed calls, close).
* `support/` assertion helper.
* `cases/` the Mio programs: `test_ffi_*`, `traps/`, and `static/` and `dynamic/` programs for the ELF layout checks.

Needs `cc`, `readelf`, `nm`, `ldd` and the zlib, SQLite3 and OpenSSL development packages (`build-essential zlib1g-dev libsqlite3-dev libssl-dev` on Ubuntu). Use `--include-dir` / `--lib-dir` if the headers live outside the default search paths. `report.json`, raw logs and `bench.txt` are written to the output directory.
