# kiso/aot/elf

Pure Mio Linux x86-64 ELF64 (`ET_EXEC`) writer. Layout:

- `0x0000`: ELF header and the program headers, inside the read/execute
  segment (`PT_LOAD`, `R+X`, base `0x400000`, `p_filesz == p_memsz == text_end`).
- `0x1000`: code. The entry point is `0x401000 + entry_offset`.
- a second `PT_LOAD` (`R+W`) at the fixed address `0x10000000` with
  `p_filesz = 0` and `p_memsz = bss_size`, i.e. zero-filled memory with no bytes
  in the file. It is omitted when `bss_size` is 0.
- `PT_GNU_STACK` with `R+W` (non-executable stack).
- after the code, a `.shstrtab` and section headers (`.text`, `.bss`,
  `.shstrtab`) that are not loaded; they only make the file easier to inspect
  with `readelf`/`objdump`.

`executable` fails closed (`EMPTY_TEXT`, `INVALID_ENTRY`, `INVALID_BSS`,
`IMAGE_TOO_LARGE`) on an empty code section, an entry offset outside the code, a
negative or oversized `bss_size` (above 256 MiB), and code so large that
`0x400000 + text_end` would reach the data segment at `0x10000000` (the RX and RW
mappings must not overlap).

There is no relocation or symbol-table support: all addresses are resolved by the
caller before the image is written.

## Tests

`elf_test.mio` holds the native Mio tests. `check_elf.py` is an independent check run by the CI
module `kiso/aot/elf` (`test.sh`, `ci-module.json`): it has a small Mio program write two images,
parses them with Python's `struct` and compares every header field with values computed from the
ELF specification, cross-checks with GNU `readelf` when one is installed, and executes the image on
a Linux x86-64 host. On other hosts the execution part, and without `readelf` the readelf part, are
printed as `NOT_RUN`; they are not counted as passed.
