// Independent Darwin ARM64 kernel oracle for the emitted allocation lock.
// The Mio test image separately exercises the managed-mutator handshake.
#include <assert.h>
#include <errno.h>
#include <mach/mach.h>
#include <pthread.h>
#include <signal.h>
#include <stdatomic.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <time.h>

enum { free_word, held, waiters };

static _Alignas(128) _Atomic uint64_t lock_word;

struct contender {
    _Atomic int stage;
    _Atomic int permit;
    pthread_t thread;
    unsigned timeout;
    int status;
};

static uint64_t now_ns(void)
{
    struct timespec ts;
    assert(clock_gettime(CLOCK_MONOTONIC, &ts) == 0);
    return (uint64_t)ts.tv_sec * 1000000000 + (uint64_t)ts.tv_nsec;
}

static void await_at_least(_Atomic int *word, int value)
{
    const uint64_t deadline = now_ns() + 5000000000;
    while (atomic_load_explicit(word, memory_order_acquire) < value) {
        assert(now_ns() < deadline);
        sched_yield();
    }
}

static int ulock(unsigned syscall, uint64_t op, uint64_t value, uint64_t timeout)
{
    register uint64_t a0 __asm("x0") = op;
    register void *a1 __asm("x1") = &lock_word;
    register uint64_t a2 __asm("x2") = value;
    register uint64_t a3 __asm("x3") = timeout;
    register uint64_t a16 __asm("x16") = syscall;
    unsigned failed;
    __asm volatile("svc #0x80\n\tcset %w[failed], cs"
        : "+r"(a0), "+r"(a1), [failed]"=r"(failed)
        : "r"(a2), "r"(a3), "r"(a16)
        : "cc", "memory");
    return failed ? (int)a0 : 0;
}

static uint64_t release(void)
{
    uint64_t before;
    __asm volatile("swpl xzr, %0, [%1]" : "=r"(before) : "r"(&lock_word) : "memory");
    return before;
}

static void *contend(void *opaque)
{
    struct contender *c = opaque;
    uint64_t before = held;
    if (!atomic_compare_exchange_strong_explicit(&lock_word, &before, waiters,
            memory_order_release, memory_order_relaxed)) {
        assert(before == waiters);
    }
    atomic_store_explicit(&c->stage, 1, memory_order_release);
    await_at_least(&c->permit, 1);
    atomic_store_explicit(&c->stage, 2, memory_order_release);
    await_at_least(&c->permit, 2);
    atomic_store_explicit(&c->stage, 3, memory_order_release);
    c->status = ulock(515, 5, waiters, c->timeout);
    return NULL;
}

static void start(struct contender *c, unsigned timeout)
{
    c->timeout = timeout;
    assert(pthread_create(&c->thread, NULL, contend, c) == 0);
    await_at_least(&c->stage, 1);
}

static void wait_in_kernel(struct contender *c)
{
    atomic_store_explicit(&c->permit, 2, memory_order_release);
    await_at_least(&c->stage, 3);
    const uint64_t deadline = now_ns() + 1000000000;
    for (;;) {
        thread_basic_info_data_t info;
        mach_msg_type_number_t count = THREAD_BASIC_INFO_COUNT;
        assert(thread_info(pthread_mach_thread_np(c->thread), THREAD_BASIC_INFO,
                   (thread_info_t)&info, &count) == KERN_SUCCESS);
        // After stage 3 this thread has no other blocking operation.
        if (info.run_state == TH_STATE_WAITING) {
            return;
        }
        assert(now_ns() < deadline);
        sched_yield();
    }
}

static void join(struct contender *c, int expected)
{
    assert(pthread_join(c->thread, NULL) == 0);
    if (c->status != expected) {
        fprintf(stderr, "wait status %d, expected %d\n", c->status, expected);
    }
    assert(c->status == expected);
}

static void release_before_wait(bool at_syscall)
{
    struct contender c = {0};
    atomic_store_explicit(&lock_word, held, memory_order_relaxed);
    start(&c, 200000);
    if (at_syscall) {
        atomic_store_explicit(&c.permit, 1, memory_order_release);
        await_at_least(&c.stage, 2);
    }
    assert(release() == waiters);
    assert(ulock(516, 0x105, 0, 0) == ENOENT);
    assert(atomic_load_explicit(&lock_word, memory_order_acquire) == free_word);
    atomic_store_explicit(&c.permit, 2, memory_order_release);
    join(&c, 0);
    puts(at_syscall ? "B pre-syscall mismatch" : "A post-advertisement mismatch");
}

static void wake_all(void)
{
    struct contender c[4] = {0};
    atomic_store_explicit(&lock_word, held, memory_order_relaxed);
    for (unsigned i = 0; i < 4; ++i) {
        start(&c[i], 2000000);
        wait_in_kernel(&c[i]);
    }
    puts("C four kernel waiters");
    assert(release() == waiters);
    assert(ulock(516, 0x105, 0, 0) == 0);
    for (unsigned i = 0; i < 4; ++i) {
        join(&c[i], 0);
    }
    puts("D wake-all returned four waiters");
}

static void timeout_over_change(void)
{
    struct contender c = {0};
    atomic_store_explicit(&lock_word, held, memory_order_relaxed);
    start(&c, 150000);
    wait_in_kernel(&c);
    // Fault injection: make the owed notification fail after state 2 -> 0.
    // A new holder arrives while the old waiter is still in the kernel.
    assert(release() == waiters);
    assert(ulock(516, 0x1ff, 0, 0) == EINVAL);
    uint64_t before = free_word;
    assert(atomic_compare_exchange_strong(&lock_word, &before, held));
    join(&c, ETIMEDOUT);
    assert(release() == held);
    before = free_word;
    assert(atomic_compare_exchange_strong(&lock_word, &before, held));
    assert(release() == held);
    puts("G timeout across release and reacquire");
}

static volatile sig_atomic_t caught;
static void handle_signal(int signal_number)
{
    caught = signal_number;
}

static void interrupt_wait(void)
{
    struct contender c = {0};
    struct sigaction act = {0};
    act.sa_handler = handle_signal;
    assert(sigemptyset(&act.sa_mask) == 0);
    assert(sigaction(SIGUSR1, &act, NULL) == 0);
    atomic_store_explicit(&lock_word, held, memory_order_relaxed);
    start(&c, 2000000);
    wait_in_kernel(&c);
    assert(pthread_kill(c.thread, SIGUSR1) == 0);
    join(&c, EINTR);
    assert(caught == SIGUSR1);
    assert(release() == waiters);
    assert(ulock(516, 0x105, 0, 0) == ENOENT);
    assert(ulock(515, 255, waiters, 1000) == EINVAL);
    puts("H EINTR and EINVAL");
}

int main(void)
{
    release_before_wait(false);
    release_before_wait(true);
    wake_all();
    timeout_over_change();
    interrupt_wait();
    return 0;
}
