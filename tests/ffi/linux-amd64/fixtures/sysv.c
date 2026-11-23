// System V AMD64 专用夹具。mio_c_* 是任务规定的函数；其余函数读取 CALL 入口处的栈，验证参数位置和对齐。
#include <stdint.h>

int32_t mio_c_add(int32_t a, int32_t b) {
    return a + b;
}

int64_t mio_c_mix(int64_t a, int32_t b) {
    return a * 100 + b;
}

double mio_c_double(double a, double b) {
    return a * 2.0 + b;
}

float mio_c_float(float a, float b) {
    return a + b;
}

int32_t mio_c_many_args(
    int64_t a,
    int64_t b,
    int64_t c,
    int64_t d,
    int64_t e,
    int64_t f,
    int64_t g,
    int64_t h,
    int64_t i
) {
    return (int32_t)(a+b+c+d+e+f+g+h+i);
}

int32_t mio_c_no_args(void) {
    return 42;
}

// 整数寄存器与 SSE 寄存器各自分配：a→RDI，b→XMM0，c→RSI，d→XMM1。
double mio_c_calculate(int64_t a, double b, int32_t c, float d) {
    return (double)a + b * 10.0 + (double)c * 100.0 + (double)d * 1000.0;
}

// 九个 double：前八个在 XMM0–XMM7，第九个在栈上。
double mio_c_nine_doubles(double a, double b, double c, double d, double e,
                          double f, double g, double h, double i) {
    return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h + 9*i;
}

// 十二个 float：前八个在 XMM0–XMM7，其余四个各占一个 8 字节栈槽。
float mio_c_twelve_floats(float a, float b, float c, float d, float e, float f,
                          float g, float h, float i, float j, float k, float l) {
    return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h + 9*i + 10*j + 11*k + 12*l;
}

// 整数寄存器耗尽之后，浮点参数仍然使用 XMM，整数参数继续上栈。
double mio_c_interleaved(int64_t a, double b, int64_t c, double d, int64_t e, double f,
                         int64_t g, double h, int64_t i, double j, int64_t k, double l,
                         int64_t m, double n) {
    return (double)(a + 2*c + 3*e + 4*g + 5*i + 6*k + 7*m) + b + 2*d + 3*f + 4*h + 5*j + 6*l + 7*n;
}

uint8_t mio_c_wrap_u8(uint8_t x) { return (uint8_t)(x + 10); }
int8_t mio_c_wrap_i8(int8_t x) { return (int8_t)(x - 10); }
uint16_t mio_c_wrap_u16(uint16_t x) { return (uint16_t)(x + 10); }
int16_t mio_c_wrap_i16(int16_t x) { return (int16_t)(x - 10); }
uint32_t mio_c_wrap_u32(uint32_t x) { return x + 10u; }
int32_t mio_c_wrap_i32(int32_t x) { return (int32_t)((uint32_t)x - 10u); }
uint64_t mio_c_wrap_u64(uint64_t x) { return x + 10u; }
int64_t mio_c_wrap_i64(int64_t x) { return (int64_t)((uint64_t)x - 10u); }

// 窄整数参数：被调用者按符号或零扩展后看到的值。
int64_t mio_c_widen_u8(uint8_t x) { return (int64_t)x; }
int64_t mio_c_widen_i8(int8_t x) { return (int64_t)x; }
int64_t mio_c_widen_u32(uint32_t x) { return (int64_t)x; }
int64_t mio_c_widen_i32(int32_t x) { return (int64_t)x; }

// 返回值的高位是未定义的：让低位正确而高位全是 1，Mio 必须自己扩展。
__asm__(
    ".text\n"
    ".globl mio_c_dirty_u8\n.type mio_c_dirty_u8,@function\n"
    "mio_c_dirty_u8:\n  movabs $0xFFFFFFFFFFFFFF80, %rax\n  ret\n"
    ".globl mio_c_dirty_i8\n.type mio_c_dirty_i8,@function\n"
    "mio_c_dirty_i8:\n  movabs $0x0000000000000080, %rax\n  ret\n"
    ".globl mio_c_dirty_u32\n.type mio_c_dirty_u32,@function\n"
    "mio_c_dirty_u32:\n  movabs $0xFFFFFFFF80000000, %rax\n  ret\n"
    ".globl mio_c_dirty_i32\n.type mio_c_dirty_i32,@function\n"
    "mio_c_dirty_i32:\n  movabs $0x0000000080000000, %rax\n  ret\n"
    ".globl mio_c_dirty_bool\n.type mio_c_dirty_bool,@function\n"
    "mio_c_dirty_bool:\n  movabs $0xFFFFFFFFFFFFFF01, %rax\n  ret\n"
    // 栈对齐探针：返回 (RSP + 8) % 16。CALL 压入返回地址后，入口处 RSP + 8 必须是 16 的倍数，所以正确结果是 0。
    ".globl mio_c_stack_alignment\n.type mio_c_stack_alignment,@function\n"
    "mio_c_stack_alignment:\n  lea 8(%rsp), %rax\n  and $15, %rax\n  ret\n"
    // 栈参数探针：第 7、8 个整数参数位于 8(%rsp) 和 16(%rsp)。返回 alignment * 1000000 + arg7 * 10 + arg8。
    ".globl mio_c_stack_probe\n.type mio_c_stack_probe,@function\n"
    "mio_c_stack_probe:\n  mov 8(%rsp), %rax\n  imul $10, %rax, %rax\n  add 16(%rsp), %rax\n"
    "  lea 8(%rsp), %rcx\n  and $15, %rcx\n  imul $1000000, %rcx, %rcx\n  add %rcx, %rax\n  ret\n"
    // 第九个 double 参数位于 8(%rsp)。
    ".globl mio_c_double_stack_probe\n.type mio_c_double_stack_probe,@function\n"
    "mio_c_double_stack_probe:\n  movsd 8(%rsp), %xmm0\n  ret\n"
);
int64_t mio_c_stack_alignment(void);
