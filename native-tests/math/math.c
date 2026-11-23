#include "../export.h"
API int32_t mio_add_i32(int32_t a, int32_t b) { return a + b; }
API int64_t mio_add_i64(int64_t a, int64_t b) { return a + b; }
API double mio_add_f64(double a, double b) { return a + b; }
API double mio_multiply_f64(double a, double b) { return a * b; }
API float mio_add_f32(float a, float b) { return a + b; }
API double mio_mixed(int32_t a, double b, uint64_t c, float d, int64_t e, double f) {
    return a + 2*b + 3*c + 4*d + 5*e + 6*f;
}
API uint64_t mio_u64(uint64_t x) { return x; }
API double mio_twelve(double a, int32_t b, float c, uint64_t d, double e, int64_t f,
                      float g, uint32_t h, double i, int32_t j, float k, uint64_t l) {
    return a+b+c+d+e+f+g+h+i+j+k+l;
}
