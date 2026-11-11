# Debugger evaluation and proposal

Status: proposal for maintainer review. No compiler, runtime or ABI change is made by this document.

## What exists today (checked)

Bytecode (reference VM, `internal/vm`):

- Runtime errors carry a call stack of `{function, bytecode offset}` (`vm.RuntimeError`, `RuntimeFrame`).
- Bytecode (`internal/code`) has no source line table, no local-variable names and no scope information.
  A debugger can therefore show function names and bytecode offsets, but cannot map them to source lines.
- `mio disasm` and `mio exec` exist for compiled Mochi files.

Native AOT (official v2.1.2 linux-amd64 Full, checked by building a small program):

- The produced ELF is a plain executable whose only section is `.text`: no `.symtab`, no `.debug_*`
  (DWARF) sections. Without them gdb/lldb can set an address breakpoint but know nothing about functions,
  lines or variables.
- `--emit-symbols` is rejected outside darwin-arm64 source builds ("applies only to darwin-arm64 native
  builds from source"); its output format is not documented and was not evaluated here.
- No debugger (gdb, lldb) is installed in this environment, so native debugging was **not** attempted and
  is not claimed to work.

Language server and editors expose no debugging today.

## Capability gaps

| Need | Bytecode VM | Native AOT |
| --- | --- | --- |
| Breakpoints at source lines | missing: no line table | missing: no line table / DWARF |
| Stepping | missing: no hook in the VM loop; no line boundaries | missing |
| Stack traces | partial: function + offset only | missing |
| Variable inspection / watch | missing: no local names or scopes | missing |
| Runtime error inspection | present (stack of function/offset) | not checked |

## Proposed architecture

Four layers, each independently useful:

1. Debug information generation (compiler). Emit a line table mapping instruction offsets to
   file:line:column, and scope/local tables with names, types and slots. For bytecode, a side section in the
   Mochi file; for native, either DWARF line tables or a documented side file. Compatible with existing
   artifacts when absent.
2. Runtime debugging support. For the VM: an opt-in hook invoked at line boundaries (breakpoints, step,
   pause), plus frame and local access. No cost when no debugger is attached. For native: rely on
   DWARF and an external debugger (lldb/gdb) rather than inventing a runtime protocol.
3. Debug adapter. A single `mio dap` (stdio) implementing the Debug Adapter Protocol, backed by the VM hooks
   for bytecode programs. For native programs it would wrap lldb-dap or gdb's DAP mode once DWARF exists,
   so no second native debugger is written.
4. Editor integration. VS Code debug contribution, JetBrains run configuration, and editor snippets that
   only point at `mio dap`. No editor-specific logic.

## Smallest useful first step

Line tables in bytecode plus a VM line hook is the smallest change that enables breakpoints, stepping and a
source-mapped stack for `mio run`. It touches the compiler and VM, so it needs maintainer approval; it
does not change language semantics or the native ABI.

## Open questions for maintainers

1. May the bytecode format gain an optional debug section (version bump or ignored-if-unknown)?
2. For native, is DWARF generation in the AOT backend acceptable, or is a side symbol file preferred?
3. Should `mio dap` live in the core repository as a subcommand, or in a separate tool?
4. What is the intended format of `--emit-symbols` on darwin-arm64?

Until these are answered nothing beyond this proposal will be implemented.
