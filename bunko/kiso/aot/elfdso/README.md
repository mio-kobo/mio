# kiso/aot/elfdso

Pure Mio reader for the declared dependencies of the native linker (`mio build --shared --linker native`): Linux
x86-64 ELF64 shared objects (exported dynamic symbols with their ELF type, `DT_SONAME`) and the bounded subset of GNU
ld scripts system libraries use instead of a `.so` (`OUTPUT_FORMAT`, `GROUP`/`INPUT` with absolute paths, `AS_NEEDED`).

Inputs are untrusted: every offset, size, count and string index is bounds-checked. Errors: `NOT_ELF`,
`UNSUPPORTED_ELF_CLASS`, `NOT_SHARED_OBJECT`, `UNSUPPORTED_MACHINE`, `INVALID_PROGRAM_HEADERS`, `INVALID_SECTION_HEADERS`,
`NO_DYNAMIC_SYMBOLS`, `INVALID_DYNSYM`, `INVALID_DYNSTR`, `INVALID_SYMBOL_NAME`, `INVALID_DYNAMIC`, `INVALID_SONAME`,
`UNSUPPORTED_SCRIPT ...`. A section header table is required; symbol versions are not read.
