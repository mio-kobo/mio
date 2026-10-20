# kiso/aot/arm64

Pure Mio encoders for the AArch64 instruction forms used by the M17cd native
builder. Every operand is range checked before a 32-bit little-endian word is
written. `Assembler` provides deterministic forward/backward label fixups for
`B`, `BL`, `B.cond`, `CBZ`, and `CBNZ`.

The public encoder returns `Instruction { word, error }`; callers must not emit
the word when `error` is non-null. `adrp_offset` deliberately uses architectural
4 KiB pages even though Darwin Mach-O segments use 16 KiB alignment.
