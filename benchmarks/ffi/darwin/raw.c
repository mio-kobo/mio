#include <dlfcn.h>
#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <time.h>

typedef int64_t (*add_function)(int64_t, int64_t);

int main(int argc, char **argv) {
    if (argc != 2) {
        return 2;
    }
    void *library = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (!library) {
        return 3;
    }
    add_function add = (add_function)dlsym(library, "mio_math_add");
    if (!add) {
        return 4;
    }
    int64_t result = 0;
    uint64_t start = clock_gettime_nsec_np(CLOCK_UPTIME_RAW);
    for (int n = 0; n < 10000; ++n) {
        result += add(20, 22);
    }
    uint64_t elapsed = clock_gettime_nsec_np(CLOCK_UPTIME_RAW) - start;
    if (result != 420000 || dlclose(library) != 0) {
        return 5;
    }
    printf("%" PRIu64 "\n", elapsed);
    return 0;
}
