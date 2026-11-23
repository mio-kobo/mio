; 实际破坏全部 volatile 寄存器，保留 Win64 的 nonvolatile 集合。
PUBLIC mio_c_clobber
PUBLIC mio_c_unwind
EXTERN mio_c_unwind_check:PROC
.code
mio_c_clobber PROC
    mov rcx, 111
    mov rdx, 222
    mov r8, 333
    mov r9, 444
    mov r10, 555
    mov r11, 666
    pcmpeqd xmm0, xmm0
    pcmpeqd xmm1, xmm1
    pcmpeqd xmm2, xmm2
    pcmpeqd xmm3, xmm3
    pcmpeqd xmm4, xmm4
    pcmpeqd xmm5, xmm5
    mov rax, 42
    ret
mio_c_clobber ENDP

; 捕获调用桥的真实 RIP/RSP/RBP，再交给系统 RtlVirtualUnwind 独立验证。
mio_c_unwind PROC FRAME
    sub rsp, 40
    .allocstack 40
    .endprolog
    mov rcx, [rsp+40]
    lea rdx, [rsp+48]
    mov r8, rbp
    call mio_c_unwind_check
    add rsp, 40
    ret
mio_c_unwind ENDP
END
