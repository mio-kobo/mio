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

`x86_64_test.mio` compares the assembler output with golden byte sequences. To
check that those sequences really are the instructions named in the assertion
messages, run

    python3 bunko/kiso/aot/x86_64/check_with_objdump.py

It extracts every `same(..., [bytes])` assertion, disassembles the bytes with
`objdump -D -b binary -m i386:x86-64 -M intel` (GNU binutils), prints the decoded
instruction next to the assertion message, and exits non-zero if any sequence does
not decode as a complete instruction. It is a reviewer aid and is not run by the
Mio test runner.
