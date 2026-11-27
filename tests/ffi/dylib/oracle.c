#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

extern int64_t mio_math_add(int64_t, int64_t);
extern double mio_math_sqrt(double);
extern int32_t mio_math_signed_add(int32_t, int32_t);
extern uint32_t mio_math_unsigned_add(uint32_t, uint32_t);
extern float mio_math_half(float);
extern int64_t mio_math_eighth(int64_t, int64_t, int64_t, int64_t,
                               int64_t, int64_t, int64_t, int64_t);
extern double mio_math_mixed(int64_t, double, int64_t, double);
extern double mio_convert_scale_float64(int64_t, int64_t);
extern bool mio_json_digit(int64_t);
extern int64_t mio_module_abi_version(void);

int main(void)
{
    return mio_module_abi_version() == 1 &&
           mio_math_add(20, 22) == 42 &&
           mio_math_sqrt(81.0) == 9.0 &&
           mio_math_signed_add(-10, 52) == 42 &&
           mio_math_unsigned_add(UINT32_MAX, 43) == 42 &&
           mio_math_half(84.0f) == 42.0f &&
           mio_math_eighth(1, 2, 3, 4, 5, 6, 7, 14) == 42 &&
           mio_math_mixed(17, 1.5, 22, 1.5) == 42.0 &&
           mio_convert_scale_float64(21, 1) == 42.0 &&
           mio_json_digit('9') && !mio_json_digit(':')
           ? EXIT_SUCCESS : EXIT_FAILURE;
}
