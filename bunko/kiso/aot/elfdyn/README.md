# kiso/aot/elfdyn

Pure Mio Linux x86-64 ELF64 shared-object (`ET_DYN`) linker for self-contained Mio modules: the
`mio build --shared --linker native` path. Input is the `elfrel.ObjectSpec` the `ET_REL` writer receives; output is a
loadable shared object. No external program is run.

Accepted relocations: `R_X86_64_PC32` (2) from `.text` to the module's data area, resolved at link time. A module with
`R_X86_64_PLT32` (a declared external C call) fails with `UNSUPPORTED_EXTERNAL <symbol>`; any other type with
`UNSUPPORTED_RELOCATION type N`. Other failures: `INVALID_SONAME`, `EMPTY_TEXT`, `TEXT_TOO_LARGE`, `INVALID_BSS`,
`INVALID_EXPORT`, `DUPLICATE_EXPORT <symbol>`, `INVALID_LOCAL`, `INVALID_RELOCATION_OFFSET`,
`INVALID_RELOCATION_SYMBOL`, `INVALID_RELOCATION_ADDEND`, `OVERLAPPING_RELOCATIONS`.

Output: `PT_LOAD` R (headers, `.gnu.hash`, `.dynsym`, `.dynstr`), `PT_LOAD` R+X (`.text`), `PT_LOAD` R+W (`.dynamic`,
`.bss` from the next page), `PT_DYNAMIC`, `PT_GNU_RELRO`, non-executable `PT_GNU_STACK`; `DT_SONAME`, `DT_GNU_HASH`,
`DT_STRTAB`, `DT_SYMTAB`, `DT_STRSZ`, `DT_SYMENT`, `DT_FLAGS=BIND_NOW`, `DT_FLAGS_1=NOW`. No dynamic relocations, GOT,
PLT or `DT_NEEDED`. Deterministic. See [docs/linux-amd64-shared-libraries.md](../../../../docs/linux-amd64-shared-libraries.md).
