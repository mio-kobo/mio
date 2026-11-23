#include "../export.h"
#include <string.h>
API size_t mio_buffer_copy(void *out, size_t capacity, const void *in, size_t length) {
    if (length > capacity) return 0;
    if (length) memmove(out, in, length);
    return length;
}
API void mio_buffer_transform(void *data, size_t length, uint8_t mask) {
    unsigned char *p = data; for (size_t i = 0; i < length; ++i) p[i] ^= mask;
}
