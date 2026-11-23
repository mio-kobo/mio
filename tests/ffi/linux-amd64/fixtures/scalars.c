// Linux x86-64（System V AMD64）测试夹具。函数名和原型与 Darwin ARM64 夹具一致，
// 这样同一份 Mio 程序（tests/ffi/darwin-arm64/cases/scalars.mio）可以在两个目标上运行。
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

int32_t mio_add(int32_t a, int32_t b) { return a + b; }
int32_t mio_no_args(void) { return -42; }
int8_t mio_signed8(int8_t x) { return x; }
int16_t mio_signed16(int16_t x) { return x; }
uint8_t mio_unsigned8(uint8_t x) { return x; }
uint16_t mio_unsigned16(uint16_t x) { return x; }
uint32_t mio_unsigned32(uint32_t x) { return x; }
uint64_t mio_unsigned64(uint64_t x) { return x ^ UINT64_C(0x8000000000000000); }
float mio_float(float a, float b) { return a * 2 + b; }
double mio_mixed(int64_t a, double b, int32_t c, float d) { return a + b * 2 + c + d; }

int64_t mio_stack(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e,
                  int64_t f, int64_t g, int64_t h, int8_t i, uint8_t j,
                  int16_t k, uint16_t l, int32_t m, uint32_t n, int64_t o) {
    return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h +
           9*i + 10*j + 11*k + 12*l + 13*m + 14*(int64_t)n + 15*o;
}

double mio_float_stack(float a, double b, float c, double d, float e,
                       double f, float g, double h, float i, int64_t j, double k, float l) {
    return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h + 9*i + 10*j + 11*k + 12*l;
}

double mio_both_stack(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e,
                      int64_t f, int64_t g, int64_t h, double i, double j, double k,
                      double l, double m, double n, double o, double p,
                      int8_t q, float r, uint16_t s, double t, _Bool u) {
    return a+b+c+d+e+f+g+h + i+j+k+l+m+n+o+p + q + r * 2 + s * 3 + t * 4 + u;
}

void *mio_new(int64_t x) {
    int64_t *p = malloc(sizeof(*p));
    if (p != NULL) *p = x;
    return p;
}

int64_t mio_load(const void *p) { return p == NULL ? -1 : *(const int64_t *)p; }
void mio_store(void *p, int64_t x) { *(int64_t *)p = x; }
_Bool mio_not(_Bool x) { return !x; }

// 破坏全部调用者保存寄存器（RAX、RCX、RDX、RSI、RDI、R8–R11）和 XMM0–XMM15，用来验证 Mio 不把值留在这些寄存器里跨调用。
// 被调用者保存寄存器（RBX、RBP、R12–R15）由编译器按 System V 规则保存，不在此处破坏。
int64_t mio_clobber(int64_t x) {
    volatile int64_t kept = x + 1;
    __asm__ volatile(
        "mov $37, %%rax; mov $37, %%rcx; mov $37, %%rdx; mov $37, %%rsi; mov $37, %%rdi; "
        "mov $37, %%r8; mov $37, %%r9; mov $37, %%r10; mov $37, %%r11; "
        "movq %%rax, %%xmm0; movq %%rax, %%xmm1; movq %%rax, %%xmm2; movq %%rax, %%xmm3; "
        "movq %%rax, %%xmm4; movq %%rax, %%xmm5; movq %%rax, %%xmm6; movq %%rax, %%xmm7; "
        "movq %%rax, %%xmm8; movq %%rax, %%xmm9; movq %%rax, %%xmm10; movq %%rax, %%xmm11; "
        "movq %%rax, %%xmm12; movq %%rax, %%xmm13; movq %%rax, %%xmm14; movq %%rax, %%xmm15"
        ::: "rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11",
            "xmm0", "xmm1", "xmm2", "xmm3", "xmm4", "xmm5", "xmm6", "xmm7",
            "xmm8", "xmm9", "xmm10", "xmm11", "xmm12", "xmm13", "xmm14", "xmm15");
    return kept;
}

void mio_print(void) { fputs("C stdio flushed\n", stdout); }
