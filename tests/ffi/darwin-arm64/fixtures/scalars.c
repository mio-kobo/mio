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

int64_t mio_clobber(int64_t x) {
    __asm__ volatile(
        "mov x0, #37; mov x1, #37; mov x2, #37; mov x3, #37; mov x4, #37; mov x5, #37; "
        "mov x6, #37; mov x7, #37; mov x8, #37; mov x9, #37; mov x10, #37; mov x11, #37; "
        "mov x12, #37; mov x13, #37; mov x14, #37; mov x15, #37; mov x16, #37; mov x17, #37; "
        "movi v0.16b, #37; movi v1.16b, #37; movi v2.16b, #37; movi v3.16b, #37; "
        "movi v4.16b, #37; movi v5.16b, #37; movi v6.16b, #37; movi v7.16b, #37"
        ::: "x0", "x1", "x2", "x3", "x4", "x5", "x6", "x7",
                     "x8", "x9", "x10", "x11", "x12", "x13", "x14", "x15",
                     "x16", "x17", "v0", "v1", "v2", "v3", "v4", "v5", "v6", "v7");
    return x + 1;
}

void mio_print(void) { fputs("C stdio flushed\n", stdout); }
