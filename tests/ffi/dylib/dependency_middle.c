#include <stdint.h>

extern int64_t mio_dependency_leaf(int64_t);

int64_t mio_dependency_middle(int64_t input) {
    return mio_dependency_leaf(input) + 1;
}
