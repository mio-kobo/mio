#define WIN32_LEAN_AND_MEAN
#include <windows.h>

/* 独立的 Windows 展开器验证真实桥帧，不依赖 PE writer 的解码函数。 */
int mio_c_unwind_check(DWORD64 rip, DWORD64 rsp, DWORD64 rbp) {
    CONTEXT context = {0};
    DWORD64 base = 0, establisher = 0;
    PVOID handler = NULL;
    PRUNTIME_FUNCTION entry = RtlLookupFunctionEntry(rip, &base, NULL);
    DWORD64 expected_rbp = *(DWORD64 *)rbp;
    DWORD64 expected_rip = *(DWORD64 *)(rbp + 8);
    if (!entry) return -1;
    context.ContextFlags = CONTEXT_FULL;
    context.Rip = rip; context.Rsp = rsp; context.Rbp = rbp;
    RtlVirtualUnwind(UNW_FLAG_NHANDLER, base, rip, entry, &context,
                    &handler, &establisher, NULL);
    return context.Rsp == rbp + 16 && context.Rbp == expected_rbp &&
           context.Rip == expected_rip ? 42 : -2;
}
