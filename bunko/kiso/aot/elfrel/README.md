# kiso/aot/elfrel

Pure Mio Linux x86-64 ELF64 relocatable object (`ET_REL`) writer. It produces the object file that the system
linker turns into a shared library (`ET_DYN`) for `mio build --shared`.

Sections: `.text`, `.rela.text`, `.bss` (the NOBITS data area of the runtime), `.note.GNU-stack` (empty, so the
stack is not executable), `.symtab`, `.strtab`, `.shstrtab`. The symbol table holds the two local section symbols,
one `GLOBAL FUNC` (default visibility) per exported entry point and one undefined `GLOBAL` symbol per declared
external C dependency. Nothing else is global: a link exports exactly what was declared.

Relocations accepted (everything else fails closed with `UNSUPPORTED_RELOCATION`):

| type | value | use |
| --- | --- | --- |
| `R_X86_64_PC32` | 2 | rip-relative reference to the data area (`S + A - P`, `S` = `.bss`) |
| `R_X86_64_PLT32` | 4 | `call symbol@PLT` to a declared external C symbol |

Other failures: `EMPTY_TEXT`, `INVALID_BSS`, `INVALID_EXPORT`, `DUPLICATE_EXPORT`, `INVALID_RELOCATION_OFFSET`,
`INVALID_RELOCATION_SYMBOL`.
