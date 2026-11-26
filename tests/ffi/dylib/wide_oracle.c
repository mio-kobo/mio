#include <assert.h>
#include <stdbool.h>
#include <stdint.h>

extern void mio_wide_remember(void *);
extern void *mio_wide_pointer(void *);
extern uint64_t mio_wide_unsigned(uint64_t);
extern int64_t mio_wide_narrow(int8_t, uint8_t, int16_t, uint16_t, int32_t, uint32_t);
extern int8_t mio_wide_signed_byte(int8_t);
extern uint16_t mio_wide_unsigned_half(uint16_t);
extern double mio_wide_spill(
    int64_t, double, int64_t, float, int64_t, double, int64_t, float,
    int64_t, double, int64_t, float, int64_t, double, int64_t, float,
    int8_t, float, uint16_t, double, bool, void *, uint32_t
);

int main(void) {
    int64_t value = 37;
    mio_wide_remember(&value);
    assert(mio_wide_pointer(&value) == &value);
    assert(mio_wide_pointer(0) == 0);
    assert(mio_wide_unsigned(UINT64_C(0xfedcba9876543210)) == UINT64_C(0xfedcba9876543210));
    assert(mio_wide_narrow(-100, 201, -12000, 65000, -1000000000, UINT32_C(4000000000))
           == INT64_C(3000053101));
    assert(mio_wide_signed_byte(-127) == -127);
    assert(mio_wide_unsigned_half(64001) == 64001);
    assert(mio_wide_spill(10, 0.5, 11, 1.5f, 12, 2.5, 13, 3.5f,
                          14, 4.5, 15, 5.5f, 16, 6.5, 17, 7.5f,
                          -13, 8.5f, 65000, 9.5, true, &value, UINT32_C(0xfedcba98)) == 42.25);
    return 0;
}
