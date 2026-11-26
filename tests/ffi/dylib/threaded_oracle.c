#include <assert.h>
#include <pthread.h>
#include <stdint.h>

extern uint64_t mio_wide_unsigned(uint64_t);
extern int64_t mio_math_add(int64_t, int64_t);

static void *worker(void *context) {
    const int64_t id = *(int64_t *)context;
    for (int64_t step = 0; step < 25000; ++step) {
        uint64_t input = UINT64_C(0x8000000000000000) + (uint64_t)step;
        assert(mio_wide_unsigned(input) == input);
        assert(mio_math_add(id, step) == id + step);
    }
    return 0;
}

int main(void) {
    pthread_t threads[4];
    int64_t ids[4] = {1, 2, 3, 4};
    for (int i = 0; i < 4; ++i) {
        assert(pthread_create(&threads[i], 0, worker, &ids[i]) == 0);
    }
    for (int i = 0; i < 4; ++i) {
        assert(pthread_join(threads[i], 0) == 0);
    }
    return 0;
}
