.text
.p2align 2
.globl _mio_float_chant_runtime
_mio_float_chant_runtime:
    // x0 = IEEE754 位模式，w1 = 3(float32) 或 4(float64)
    // 帧内保存 x19..x28，不触碰调用者的 FP/LR 和业务寄存器。
    stp x29, x30, [sp, #-16]!
    mov x29, sp
    sub sp, sp, #4096
    sub sp, sp, #512
    stp x19, x20, [sp, #0]
    stp x21, x22, [sp, #16]
    stp x23, x24, [sp, #32]
    stp x25, x26, [sp, #48]
    stp x27, x28, [sp, #64]
    add x19, sp, #128          // 96×uint32 中央整数
    add x20, sp, #512          // 下边界
    add x21, sp, #896          // 上边界
    add x22, sp, #1280         // 十进制数字缓冲
    add x23, sp, #2080
    add x24, sp, #2880
    add x25, sp, #3680         // 向下取整候选
    add x26, sp, #3712         // 向上取整候选
    add x27, sp, #3744         // 输出缓冲
    str w1, [sp, #92]         // 保留 float 位数类型
    cmp w1, #3
    b.eq .Lf32

.Lf64:
    lsr x9, x0, #63
    str x9, [sp, #80]          // sign
    ubfx x10, x0, #52, #11   // 指数
    ubfx x11, x0, #0, #52    // fraction
    cmp x10, #2047
    b.eq .Lspecial
    orr x8, x10, x11
    cbz x8, .Lzero
    mov x14, #2                // 下边界距离普通半 ULP: delta=2
    cmp x10, #1
    b.ls .LnotHalf64
    cbnz x11, .LnotHalf64
    mov x14, #1                // 精确的 normal 2^n: delta=1
.LnotHalf64:
    cbz x10, .Lsub64
    orr x11, x11, #0x0010000000000000
    sub x13, x10, #1075      // exp2 = expBits - bias - mantbits
    b .LmantReady
.Lsub64:
    mov x13, #1074
    neg x13, x13
    b .LmantReady

.Lf32:
    lsr w9, w0, #31
    str x9, [sp, #80]
    ubfx w10, w0, #23, #8
    ubfx w11, w0, #0, #23
    cmp w10, #255
    b.eq .Lspecial
    orr w8, w10, w11
    cbz w8, .Lzero
    mov x14, #2
    cmp x10, #1
    b.ls .LnotHalf32
    cbnz x11, .LnotHalf32
    mov x14, #1
.LnotHalf32:
    cbz x10, .Lsub32
    orr x11, x11, #0x00800000
    sub x13, x10, #150
    b .LmantReady
.Lsub32:
    mov x13, #149
    neg x13, x13

.LmantReady:
    and x15, x11, #1
    eor x15, x15, #1
    str w15, [sp, #88]        // mantissa even -> 边界闭合

    // 将三组大整数初始化为 4m，4m-delta 和 4m+2。
    mov x0, x19
    mov x1, #72               // 3×96×4 = 1152 bytes
.Lclear:
    stp xzr, xzr, [x0], #16
    subs x1, x1, #1
    b.ne .Lclear
    lsl x11, x11, #2
    str x11, [x19]
    sub x8, x11, x14
    str x8, [x20]
    add x8, x11, #2
    str x8, [x21]

    // 为了比较舍入边界，将 V/L/U 统一扩大到相同的 10^-K 精度。
    // e2-2>=0 时乘 2；否则乘 5，且 K=-(e2-2)。
    sub x16, x13, #2
    mov x17, #2
    mov x8, #0
    cmp x16, #0
    b.ge .LscaleStart
    neg x16, x16
    mov x17, #5
    mov x8, x16
.LscaleStart:
    str x8, [sp, #96]         // decimal K
    cbz x16, .LscaleDone
.LscaleLoop:
    mov x0, x19
    mov x1, x17
    bl .LmulSmall
    mov x0, x20
    mov x1, x17
    bl .LmulSmall
    mov x0, x21
    mov x1, x17
    bl .LmulSmall
    subs x16, x16, #1
    b.ne .LscaleLoop
.LscaleDone:
    // 整数均在各自的 96×uint32 里，按除十取余生成最精确十进制数字。
    mov x0, x19
    add x1, x22, #800
    bl .Ldigits
    mov x22, x0
    str x1, [sp, #104]

    mov x0, x20
    add x1, x23, #800
    bl .Ldigits
    mov x23, x0
    str x1, [sp, #112]

    mov x0, x21
    add x1, x24, #800
    bl .Ldigits
    mov x24, x0
    str x1, [sp, #120]

    // 从 1 位开始，寻找第一个落在真实 IEEE 舍入区间内的十进制候选。
    // 若两个候选均可回舍到原值，取距离精确值更近的那个，平局取偶数。
    mov x14, #1
.Lprecision:
    ldr x11, [sp, #104]
    sub x15, x11, x14        // cut = N-p
    mov x3, #0
.LcopyDigits:
    ldrb w4, [x22, x3]
    strb w4, [x25, x3]
    strb w4, [x26, x3]
    add x3, x3, #1
    cmp x3, x14
    b.lt .LcopyDigits

    // floor candidate 长 p；ceil candidate 在最后一位 +1。
    sub x4, x14, #1
.Lincrement:
    ldrb w5, [x26, x4]
    cmp w5, #'9'
    b.ne .LincrementSimple
    mov w5, #'0'
    strb w5, [x26, x4]
    subs x4, x4, #1
    b.ge .Lincrement
    mov w5, #'1'
    strb w5, [x26]
    mov w5, #'0'
    strb w5, [x26, x14]
    add x16, x14, #1
    b .LincrementDone
.LincrementSimple:
    add w5, w5, #1
    strb w5, [x26, x4]
    mov x16, x14
.LincrementDone:
    mov x0, x25
    mov x1, x14
    mov x2, x15
    bl .Lfits
    mov x12, x0             // floor fits (helper 保留 x12-17)
    mov x0, x26
    mov x1, x16
    mov x2, x15
    bl .Lfits
    mov x13, x0             // ceil fits
    orr x0, x12, x13
    cbnz x0, .Lchoose
    add x14, x14, #1
    ldr w8, [sp, #92]
    cmp w8, #3
    mov x8, #17
    b.ne .LcheckMax
    mov x8, #9
.LcheckMax:
    cmp x14, x8
    b.le .Lprecision
    // 正确的有限 IEEE754 值绝不会走到这里；以非零状态退出。
    b .Linvalid

.Lchoose:
    cbz x12, .LpickCeil
    cbz x13, .LpickFloor
    ldr x8, [sp, #104]
    cmp x14, x8
    b.ge .LpickFloor
    ldrb w8, [x22, x14]
    cmp w8, #'5'
    b.lt .LpickFloor
    b.gt .LpickCeil
    // p+1 位等于 5 时，检查剩余小数是否非零。
    add x3, x14, #1
    ldr x4, [sp, #104]
.LtieCheck:
    cmp x3, x4
    b.ge .LtieEven
    ldrb w5, [x22, x3]
    cmp w5, #'0'
    b.ne .LpickCeil
    add x3, x3, #1
    b .LtieCheck
.LtieEven:
    sub x3, x14, #1
    ldrb w5, [x25, x3]
    tbz w5, #0, .LpickFloor
    b .LpickCeil

.LpickFloor:
    mov x28, x25
    mov x16, x14
    b .Lpicked
.LpickCeil:
    mov x28, x26
.Lpicked:
    // exponent: totalDecimalDigits - K - 1；去尾零不改变科学指数。
    add x13, x16, x15
    ldr x9, [sp, #96]
    sub x13, x13, x9
    sub x13, x13, #1
.LtrimTrailing:
    cmp x16, #1
    b.le .Lrender
    sub x3, x16, #1
    ldrb w5, [x28, x3]
    cmp w5, #'0'
    b.ne .Lrender
    sub x16, x16, #1
    b .LtrimTrailing

.Lrender:
    mov x17, x27
    ldr x8, [sp, #80]
    cbz x8, .LnoMinus
    mov w8, #'-'
    strb w8, [x17], #1
.LnoMinus:
    cmp x13, #-4
    b.lt .Lsci
    cmp x13, #6
    b.ge .Lsci
    add x12, x13, #1        // dp
    cmp x12, #0
    b.gt .LfixedPos
    mov w8, #'0'
    strb w8, [x17], #1
    mov w8, #'.'
    strb w8, [x17], #1
    neg x12, x12
.LfixedLeadZeros:
    cbz x12, .LfixedAllDigits
    mov w8, #'0'
    strb w8, [x17], #1
    sub x12, x12, #1
    b .LfixedLeadZeros
.LfixedPos:
    cmp x12, x16
    b.ge .LfixedIntegral
    // 精确小数点位于有效数字中间。
    mov x3, #0
.LfixedBefore:
    cmp x3, x12
    b.ge .LfixedDot
    ldrb w8, [x28, x3]
    strb w8, [x17], #1
    add x3, x3, #1
    b .LfixedBefore
.LfixedDot:
    mov w8, #'.'
    strb w8, [x17], #1
.LfixedAfter:
    cmp x3, x16
    b.ge .LfinishLine
    ldrb w8, [x28, x3]
    strb w8, [x17], #1
    add x3, x3, #1
    b .LfixedAfter
.LfixedIntegral:
    sub x12, x12, x16
.LfixedAllDigits:
    mov x3, #0
.LfixedDigits:
    cmp x3, x16
    b.ge .LfixedTrailZeros
    ldrb w8, [x28, x3]
    strb w8, [x17], #1
    add x3, x3, #1
    b .LfixedDigits
.LfixedTrailZeros:
    cbz x12, .LfinishLine
    mov w8, #'0'
    strb w8, [x17], #1
    sub x12, x12, #1
    b .LfixedTrailZeros

.Lsci:
    ldrb w8, [x28]
    strb w8, [x17], #1
    cmp x16, #1
    b.eq .Lexp
    mov w8, #'.'
    strb w8, [x17], #1
    mov x3, #1
.LsciDigits:
    cmp x3, x16
    b.ge .Lexp
    ldrb w8, [x28, x3]
    strb w8, [x17], #1
    add x3, x3, #1
    b .LsciDigits
.Lexp:
    mov w8, #'e'
    strb w8, [x17], #1
    cmp x13, #0
    b.lt .LexpNeg
    mov w8, #'+'
    strb w8, [x17], #1
    mov x3, x13
    b .LexpAbs
.LexpNeg:
    mov w8, #'-'
    strb w8, [x17], #1
    neg x3, x13
.LexpAbs:
    mov x4, #100
    cmp x3, #100
    b.lt .LexpTwoDigits
    udiv x5, x3, x4
    msub x3, x5, x4, x3
    add w5, w5, #'0'
    strb w5, [x17], #1
.LexpTwoDigits:
    mov x4, #10
    udiv x5, x3, x4
    msub x6, x5, x4, x3
    add w5, w5, #'0'
    add w6, w6, #'0'
    strb w5, [x17], #1
    strb w6, [x17], #1
    b .LfinishLine

.Lspecial:
    cbnz x11, .Lnan
    mov x17, x27
    ldr x8, [sp, #80]
    cbz x8, .LplusInf
    mov w8, #'-'
    b .LsignInf
.LplusInf:
    mov w8, #'+'
.LsignInf:
    strb w8, [x17], #1
    mov w8, #'I'
    strb w8, [x17], #1
    mov w8, #'n'
    strb w8, [x17], #1
    mov w8, #'f'
    strb w8, [x17], #1
    b .LfinishLine
.Lnan:
    mov x17, x27
    mov w8, #'N'
    strb w8, [x17], #1
    mov w8, #'a'
    strb w8, [x17], #1
    mov w8, #'N'
    strb w8, [x17], #1
    b .LfinishLine
.Lzero:
    mov x17, x27
    ldr x8, [sp, #80]
    cbz x8, .LzeroDigit
    mov w8, #'-'
    strb w8, [x17], #1
.LzeroDigit:
    mov w8, #'0'
    strb w8, [x17], #1
.LfinishLine:
    mov w8, #10
    strb w8, [x17], #1
    mov x0, #1
    mov x1, x27
    sub x2, x17, x27
    mov x16, #4  // Darwin SYS_write
    svc #0x80
    b .Lret
.Linvalid:
    mov x0, #1
    mov x16, #1
    svc #0x80
.Lret:
    ldp x19, x20, [sp, #0]
    ldp x21, x22, [sp, #16]
    ldp x23, x24, [sp, #32]
    ldp x25, x26, [sp, #48]
    ldp x27, x28, [sp, #64]
    add sp, sp, #4096
    add sp, sp, #512
    ldp x29, x30, [sp], #16
    ret

// x0=96×uint32, x1=2/5。由低位向高位做精确乘法。
.LmulSmall:
    mov x2, #0
    mov x3, #96
.LmulLoop:
    ldr w4, [x0]
    madd x5, x4, x1, x2
    str w5, [x0], #4
    lsr x2, x5, #32
    subs x3, x3, #1
    b.ne .LmulLoop
    ret

// x0=大整数指针 x1=800字节buffer结束地址
// 返回 x0=十进制数字起点 x1=有效数字长度，数据自高位向低位排列。
.Ldigits:
    mov x7, x0
    mov x8, x1
    mov x10, #0
    mov x12, #10
.LnextDigit:
    mov x2, #0
    add x3, x7, #380
    mov x4, #96
    mov x9, #0
.LdivLoop:
    ldr w5, [x3]
    orr x6, x5, x2, lsl #32
    udiv x11, x6, x12
    msub x2, x11, x12, x6
    str w11, [x3]
    orr x9, x9, x11
    sub x3, x3, #4
    subs x4, x4, #1
    b.ne .LdivLoop
    add w2, w2, #'0'
    sub x8, x8, #1
    strb w2, [x8]
    add x10, x10, #1
    cbnz x9, .LnextDigit
    mov x0, x8
    mov x1, x10
    ret

// x0=候选字符，x1=字符数，x2=尾部补零数。返回x0=是否回舍到目标二进制数。
// 调用链只有 .Lfits -> .LcmpDec，保留 x9..x17。
.Lfits:
    mov x17, x30
    mov x9, x0
    mov x10, x1
    mov x11, x2
    mov x3, x23
    ldr x4, [sp, #112]
    bl .LcmpDec
    cmp x0, #0
    b.lt .LfitsNo
    b.ne .LfitsUpper
    ldr w5, [sp, #88]
    cbz w5, .LfitsNo
.LfitsUpper:
    mov x0, x9
    mov x1, x10
    mov x2, x11
    mov x3, x24
    ldr x4, [sp, #120]
    bl .LcmpDec
    cmp x0, #0
    b.gt .LfitsNo
    b.ne .LfitsYes
    ldr w5, [sp, #88]
    cbz w5, .LfitsNo
.LfitsYes:
    mov x0, #1
    mov x30, x17
    ret
.LfitsNo:
    mov x0, #0
    mov x30, x17
    ret

// 精确比较: decimal(candidate*10^cut) 与 decimal(bound)。
// 返回 -1/0/1；x9..x17 始终不改变。
.LcmpDec:
    add x5, x1, x2
    cmp x5, x4
    b.lt .LcmpLess
    b.gt .LcmpGreater
    mov x6, #0
.LcmpLoop:
    cmp x6, x5
    b.ge .LcmpEqual
    cmp x6, x1
    b.lt .LcmpCandDigit
    mov w7, #'0'
    b .LcmpHaveDigit
.LcmpCandDigit:
    ldrb w7, [x0, x6]
.LcmpHaveDigit:
    ldrb w8, [x3, x6]
    cmp w7, w8
    b.lt .LcmpLess
    b.gt .LcmpGreater
    add x6, x6, #1
    b .LcmpLoop
.LcmpLess:
    mov x0, #1
    neg x0, x0
    ret
.LcmpGreater:
    mov x0, #1
    ret
.LcmpEqual:
    mov x0, #0
    ret
