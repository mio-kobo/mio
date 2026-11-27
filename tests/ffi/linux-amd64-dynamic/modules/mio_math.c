#include <stdint.h>

int32_t mio_add(int32_t a, int32_t b) { return (int32_t)((uint32_t)a + (uint32_t)b); }
int32_t mio_sub(int32_t a, int32_t b) { return (int32_t)((uint32_t)a - (uint32_t)b); }
double mio_multiply(double a, double b) { return a * b; }

/* Extra exports covering the remaining FFI ABI v1 scalar types. */
int64_t mio_mul64(int64_t a, int64_t b) { return (int64_t)((uint64_t)a * (uint64_t)b); }
float mio_scale32(float a, float b) { return a * b; }
uint8_t mio_is_even(int32_t a) { return (uint8_t)((a & 1) == 0); }

/* System V AMD64 probes used by test_ffi_abi. */
int64_t mio_many_ints(int64_t a, int64_t b, int64_t c, int64_t d, int64_t e, int64_t f, int64_t g, int64_t h) {
    return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f + 7 * g + 8 * h; /* g and h are passed on the stack */
}

double mio_many_doubles(double d0, double d1, double d2, double d3, double d4, double d5, double d6, double d7, double d8, double d9) {
    return d0 + 2 * d1 + 3 * d2 + 4 * d3 + 5 * d4 + 6 * d5 + 7 * d6 + 8 * d7 + 9 * d8 + 10 * d9; /* d8, d9 on the stack */
}

/* Integer and SSE registers run out independently: 8 integers (2 on the stack) and 9 doubles (1 on the stack), plus float and int8. */
double mio_mixed(int64_t a, double x0, int32_t b, float y, int64_t c, int64_t d, int64_t e, int64_t f, int64_t g, int8_t h,
                 double x1, double x2, double x3, double x4, double x5, double x6, double x7) {
    return (double)a + x0 * 2 + (double)b * 3 + (double)y * 4 + (double)c * 5 + (double)d * 6 + (double)e * 7 + (double)f * 8 +
           (double)g * 9 + (double)h * 10 + x1 * 11 + x2 * 12 + x3 * 13 + x4 * 14 + x5 * 15 + x6 * 16 + x7 * 17;
}

/* (RSP + 8) & 15 at function entry; 0 when the caller aligned the stack correctly before CALL. */
__asm__(".text\n.globl mio_stack_misalignment\n.type mio_stack_misalignment, @function\nmio_stack_misalignment:\n"
        "  lea 8(%rsp), %rax\n  and $15, %rax\n  ret\n.size mio_stack_misalignment, .-mio_stack_misalignment\n");

/* Overwrites every caller-saved register it can reach and returns; Mio values must survive. */
int64_t mio_clobber(int64_t x) {
    volatile int64_t junk[16];
    int i;
    for (i = 0; i < 16; i++) junk[i] = x + i;
    return x ^ junk[15];
}
