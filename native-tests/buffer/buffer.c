#include "../export.h"
#include <string.h>
#include <stdlib.h>
API void *mio_buffer_new(size_t size) { return calloc(size ? size : 1, 1); }
API void mio_buffer_free(void *data) { free(data); }
API uint8_t mio_buffer_byte(const void *data, size_t index) { return ((const uint8_t *)data)[index]; }
API size_t mio_buffer_copy(void *out, size_t capacity, const void *in, size_t length) {
    if (length > capacity) return 0;
    if (length) memmove(out, in, length);
    return length;
}
API void mio_buffer_transform(void *data, size_t length, uint8_t mask) {
    unsigned char *p = data; for (size_t i = 0; i < length; ++i) p[i] ^= mask;
}
