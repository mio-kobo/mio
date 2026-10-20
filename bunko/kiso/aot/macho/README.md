# kiso/aot/macho

Pure Mio Darwin ARM64 Mach-O layout planner and binary writer. It mirrors the
working Go AOT layout: `__PAGEZERO`, `__TEXT` with `__text` and optional
`__const`, optional `__DATA,__data`, `__LINKEDIT`, dyld/build-version commands,
empty symbol commands, and `LC_MAIN` at file offset `0x300`.

`AddressPatch` records are resolved only after the final 16 KiB segment layout
is known. The emitted file contains no unresolved relocation records; its empty
`LC_SYMTAB`/`LC_DYSYMTAB` match the established executable format. The unsigned
byte array is deterministic and can be written with `kiso/fs`, then ad-hoc
signed by the host on macOS.
