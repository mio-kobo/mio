#include <stdint.h>
#include <stddef.h>
#include <intrin.h>

#define API __declspec(dllexport)
static int64_t cell = 123456789;
static int32_t calls;

API int32_t mio_c_add(int32_t a, int32_t b) { return a + b; }
API int64_t mio_c_mix(int64_t a, double b, int32_t c, float d) {
    return a + (int64_t)b + c + (int64_t)d;
}
API double mio_c_double(double a, double b) { return a * 2.0 + b; }
API float mio_c_float(float a, float b) { return a + b; }
API int32_t mio_c_many(int64_t a, int64_t b, int64_t c, int64_t d,
                       int64_t e, int64_t f, int64_t g) {
    /* 加权结果可以识别栈参数交换，简单求和不能。 */
    return (int32_t)(a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g);
}
API double mio_c_stack(int64_t a, double b, int32_t c, float d,
                        double e, float f, int16_t g, uint32_t h) {
    return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h;
}
API int32_t mio_c_no_args(void) { return 42; }
API void mio_c_void(void) { ++calls; }
API int32_t mio_c_calls(void) { return calls; }
API void *mio_c_pointer(void) { return &cell; }
API int64_t mio_c_read(void *p) { return p ? *(int64_t *)p : -1; }
API int32_t mio_c_null(void *p) { return p == NULL; }
API uint64_t mio_c_high(void) { return UINT64_C(0xfedcba9876543210); }
API uint64_t mio_c_echo(uint64_t x) { return x; }
API int8_t mio_c_i8(int8_t x) { return x; }
API uint8_t mio_c_u8(uint8_t x) { return x; }
API int16_t mio_c_i16(int16_t x) { return x; }
API uint16_t mio_c_u16(uint16_t x) { return x; }
API int32_t mio_c_i32(int32_t x) { return x; }
API uint32_t mio_c_u32(uint32_t x) { return x; }
API float mio_c_f32_bits(float x) { return x; }
API double mio_c_f64_bits(double x) { return x; }
API int32_t mio_c_negative_zero(float a, double b) {
    union { float f; uint32_t u; } x; union { double f; uint64_t u; } y;
    x.f = a; y.f = b;
    return x.u == UINT32_C(0x80000000) && y.u == UINT64_C(0x8000000000000000);
}
API double mio_c_nan(void) {
    union { double f; uint64_t u; } x;
    x.u = UINT64_C(0x7ff8123456789abc); return x.f;
}
API int32_t mio_c_nan_payload(double value) {
    union { double f; uint64_t u; } x;
    x.f = value; return x.u == UINT64_C(0x7ff8123456789abc);
}

/* 返回地址位于进入 C 时的 RSP；紧随其后的四槽属于调用者提供的 Shadow Space。 */
API __declspec(noinline) int32_t mio_c_frame(void) {
    volatile uintptr_t *ret = (uintptr_t *)_AddressOfReturnAddress();
    int aligned = ((uintptr_t)ret & 15) == 8;
    ret[1] = 0x11111111; ret[2] = 0x22222222;
    ret[3] = 0x33333333; ret[4] = 0x44444444;
    return aligned ? 42 : -1;
}
