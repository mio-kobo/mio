#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

typedef int64_t (*unary_fn)(int64_t);
typedef int64_t (*binary_fn)(int64_t, int64_t);
typedef int64_t (*six_fn)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t);
typedef int64_t (*zero_fn)(void);

static FARPROC symbol(HMODULE lib, const char *name) {
    FARPROC result = GetProcAddress(lib, name);
    if (!result) {
        fprintf(stderr, "GetProcAddress(%s): %lu\n", name, GetLastError());
        ExitProcess(2);
    }
    return result;
}

// Generated DIR64 operands address the IAT. Check the loader actually rebased
// those code operands, even though successful scalar calls do not use the traps.
static int relocated_iat(HMODULE lib) {
    const BYTE *image = (const BYTE *)lib;
    const IMAGE_DOS_HEADER *dos = (const IMAGE_DOS_HEADER *)image;
    const IMAGE_NT_HEADERS64 *nt = (const IMAGE_NT_HEADERS64 *)(image+dos->e_lfanew);
    IMAGE_DATA_DIRECTORY reloc = nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_BASERELOC];
    IMAGE_DATA_DIRECTORY iat = nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IAT];
    DWORD at = 0, fixups = 0;
    while (at < reloc.Size) {
        const IMAGE_BASE_RELOCATION *block = (const IMAGE_BASE_RELOCATION *)(image+reloc.VirtualAddress+at);
        const WORD *entries = (const WORD *)(block+1);
        DWORD i, count;
        if (block->SizeOfBlock < 8 || block->SizeOfBlock > reloc.Size-at) return 0;
        count = (block->SizeOfBlock-8)/2;
        for (i = 0; i < count; ++i) {
            if ((entries[i] >> 12) == IMAGE_REL_BASED_DIR64) {
                uintptr_t target;
                memcpy(&target, image+block->VirtualAddress+(entries[i]&4095), sizeof(target));
                if (target < (uintptr_t)image+iat.VirtualAddress ||
                    target+8 > (uintptr_t)image+iat.VirtualAddress+iat.Size) return 0;
                ++fixups;
            }
        }
        at += block->SizeOfBlock;
    }
    return fixups == 3;
}

// Independently exercise the OS unwinder for a wrapper just after its prologue.
static int unwind_wrapper(HMODULE lib, FARPROC proc) {
    DWORD64 base = 0;
    PRUNTIME_FUNCTION function = RtlLookupFunctionEntry((DWORD64)proc, &base, NULL);
    CONTEXT context = {0};
    DWORD64 stack[64] = {0};
    PVOID handler_data = NULL;
    DWORD64 frame = 0;
    if (!function || base != (DWORD64)lib) return 0;
    /* mio_add wrapper: push rbp; mov rbp,rsp; sub rsp,32. */
    stack[32] = 0x11223344; /* saved RBP */
    stack[33] = 0x55667788; /* return PC */
    context.Rip = (DWORD64)proc + 11;
    context.Rbp = (DWORD64)&stack[32];
    context.Rsp = (DWORD64)&stack[28];
    RtlVirtualUnwind(UNW_FLAG_NHANDLER, base, context.Rip, function,
                    &context, &handler_data, &frame, NULL);
    return context.Rip == 0x55667788 && context.Rbp == 0x11223344 &&
           context.Rsp == (DWORD64)&stack[34];
}

int wmain(void) {
    const wchar_t *files[] = {L"mio_math.dll", L"mio_utility.dll", L"mio_bytes.dll", L"mio_encoding.dll", L"mio_standard.dll"};
    HMODULE libs[5] = {0};
    unsigned cycle, i;
    int relocated = 0;
    for (cycle = 0; cycle < 3; ++cycle) {
        for (i = 0; i < 5; ++i) {
            libs[i] = LoadLibraryW(files[i]);
            if (!libs[i]) { fprintf(stderr, "LoadLibraryW(%u): %lu\n", i, GetLastError()); return 1; }
            if (!relocated_iat(libs[i])) return 9;
            if ((uintptr_t)libs[i] != UINT64_C(0x140000000)) relocated = 1;
        }
        binary_fn add = (binary_fn)symbol(libs[0], "mio_add");
        six_fn sum6 = (six_fn)symbol(libs[0], "mio_sum6");
        unary_fn absolute = (unary_fn)symbol(libs[1], "mio_abs");
        unary_fn low = (unary_fn)symbol(libs[2], "mio_low_byte");
        unary_fn decimal = (unary_fn)symbol(libs[3], "mio_decimal");
        binary_fn standard = (binary_fn)symbol(libs[4], "mio_standard");
        zero_fn zero = (zero_fn)symbol(libs[4], "mio_zero");
        for (i = 0; i < 1000; ++i) {
            if (add(10, 20) != 30 || add(-100, 30) != -70 ||
                add(INT64_C(0x100000000), 17) != INT64_C(0x100000011) ||
                sum6(1, 2, 3, 4, 5, 6) != 91 || sum6(-1, 2, -3, 4, -5, 6) != 21 ||
                absolute(-37) != 37 || absolute(19) != 19 || low(0x1234) != 0x34 ||
                decimal(55) != 7 || decimal(65) != -1 || standard(-10, 0x1234) != 69 || zero() != 42) return 3;
        }
        if (GetProcAddress(libs[0], "private_helper") || GetProcAddress(libs[4], "mio_add") ||
            GetProcAddress(libs[0], "missing_export")) return 4;
        if (!unwind_wrapper(libs[0], symbol(libs[0], "mio_add"))) return 5;
        for (i = 0; i < 5; ++i) {
            if (!FreeLibrary(libs[i])) return 6;
            if (GetModuleHandleW(files[i])) return 7;
        }
    }
    if (!relocated) return 8;
    puts("PASS: C ABI, six arguments, cross-module calls, relocation, unwind, unload/reload");
    return 0;
}
