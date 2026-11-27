#include <stdbool.h>
#include <stdint.h>

const uint64_t fixture_read_only = UINT64_C(0xfedcba9876543210);
uint64_t fixture_data;

void foreign_store(uint64_t value) {
    fixture_data = value;
}

const uint64_t *foreign_pointer(void) {
    return &fixture_read_only;
}

bool foreign_check_pointer(const uint64_t *pointer, uint64_t value) {
    return pointer == &fixture_read_only && *pointer == value && fixture_data == value;
}

int8_t foreign_i8(int8_t value) { return value - 1; }
uint8_t foreign_u8(uint8_t value) { return value + 1; }
int16_t foreign_i16(int16_t value) { return value - 1; }
uint16_t foreign_u16(uint16_t value) { return value + 1; }
int32_t foreign_i32(int32_t value) { return value - 1; }
uint32_t foreign_u32(uint32_t value) { return value + 1; }
int64_t foreign_i64(int64_t value) { return value - 1; }
uint64_t foreign_u64(uint64_t value) { return value + 1; }
float foreign_f32(float value) { return value * -0.5f; }
double foreign_f64(double value) { return value * -0.5; }

double foreign_stack(
    int64_t a0, double f0, int64_t a1, float f1,
    int64_t a2, double f2, int64_t a3, float f3,
    int64_t a4, double f4, int64_t a5, float f5,
    int64_t a6, double f6, int64_t a7, float f7,
    int8_t s0, float s1, uint16_t s2, double s3,
    bool s4, const uint64_t *s5, uint32_t s6
) {
    if (a0 != 10 || a1 != 11 || a2 != 12 || a3 != 13 ||
        a4 != 14 || a5 != 15 || a6 != 16 || a7 != 17 ||
        f0 != 0.5 || f1 != 1.5f || f2 != 2.5 || f3 != 3.5f ||
        f4 != 4.5 || f5 != 5.5f || f6 != 6.5 || f7 != 7.5f ||
        s0 != -13 || s1 != 8.5f || s2 != 65000 || s3 != 9.5 ||
        !s4 || s5 != &fixture_read_only || s6 != UINT32_C(0xfedcba98)) {
        return -1.0;
    }
    return 42.25;
}

int64_t mio_module_abi_version(void) { return 2; }
int64_t mio_foreign_fixture(void) { return 123; }
__asm__(".globl _mio_ffi_v1_g_mio_module_abi_version\n"
        ".set _mio_ffi_v1_g_mio_module_abi_version, _mio_module_abi_version\n"
        ".globl _mio_ffi_v1_g_mio_foreign_fixture\n"
        ".set _mio_ffi_v1_g_mio_foreign_fixture, _mio_foreign_fixture\n");
