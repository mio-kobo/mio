#include <assert.h>
#include <dlfcn.h>
#include <stdint.h>

typedef int64_t (*read_state)(void);
typedef void (*initialize_state)(int64_t);
typedef void (*finalize_state)(void);

int main(int argc, char **argv) {
    assert(argc == 3);
    void *first = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    void *second = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    void *independent = dlopen(argv[2], RTLD_NOW | RTLD_LOCAL);
    assert(first && second && independent);
    read_state current = (read_state)dlsym(first, "mio_state_current");
    read_state next = (read_state)dlsym(second, "mio_state_next");
    read_state independent_current = (read_state)dlsym(independent, "mio_state_current");
    initialize_state initialize = (initialize_state)dlsym(first, "mio_state_initialize");
    finalize_state finalize = (finalize_state)dlsym(second, "mio_state_finalize");
    assert(current && next && independent_current && initialize && finalize);
    assert(current() == 39 && next() == 42 && current() == 42);
    initialize(50);
    assert(current() == 50 && next() == 53 && independent_current() == 39);
    assert(dlclose(first) == 0);
    assert(next() == 56);
    finalize();
    assert(next() == 3);
    assert(dlclose(second) == 0 && dlclose(independent) == 0);
    for (int cycle = 0; cycle < 3; ++cycle) {
        void *reopened = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
        assert(reopened);
        current = (read_state)dlsym(reopened, "mio_state_current");
        initialize = (initialize_state)dlsym(reopened, "mio_state_initialize");
        assert(current && initialize);
        initialize(39);
        assert(current() == 39);
        assert(dlclose(reopened) == 0);
    }
    return 0;
}
