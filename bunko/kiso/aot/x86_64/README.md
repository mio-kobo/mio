# kiso/aot/x86_64

Pure Mio x86-64 (AMD64, 64-bit mode) machine-code encoder. It covers the small
instruction subset that will be needed by the Linux runtime-IR lowering in later
changes: 64-bit register moves, arithmetic and logic, `imul`/`div`, memory loads
and stores (`[base + disp32]`, with a SIB byte for `rsp`/`r12` bases), `lea`,
`push`/`pop`, `setcc`, `jmp`/`jcc`/`call` with `rel32` label fixups,
RIP-relative `lea`, `syscall`, `mfence`, and the atomic `xadd` / `cmpxchg` /
`xchg` forms (`lock`-prefixed, or implicitly locked for `xchg` with memory).

The language has no bit operators, so every field (REX, ModRM, SIB, immediates)
is composed with checked integer arithmetic. Invalid registers, immediates and
condition codes are recorded as errors on the assembler and reported by
`assemble`; nothing is silently truncated. Memory operands always use the
`disp32` form, which is valid but not the shortest encoding.

## Tests

`x86_64_test.mio` compares the assembler output with golden byte sequences. As an
independent cross-check, run

    python3 bunko/kiso/aot/x86_64/check_with_objdump.py

It extracts every `same(..., [bytes])` assertion, disassembles the bytes with
`objdump -D -b binary -m i386:x86-64 -M intel` (GNU binutils), and fails if a sequence
does not decode as complete instructions or if the decoded text differs from the entry
for that assertion in the script's `EXPECTED` table (so a byte sequence that decodes
cleanly but to a different instruction than its message is reported). It does not
execute anything and does not check assertions of other shapes. Exit status: 0 ok,
1 mismatch or undecodable, 2 usage, 3 GNU objdump not available (nothing checked). It is
a reviewer aid and is not run by the Mio test runner or any workflow.
