#include <stddef.h>
#include <stdint.h>

size_t mio_buffer_length(const uint8_t *data, size_t len) {
    /* Counts the bytes up to the first NUL or len: reads at most len bytes. */
    size_t n = 0;
    if (data == NULL) return 0;
    while (n < len && data[n] != 0) n++;
    return n;
}

/* FNV-1a 64-bit over exactly len bytes. */
uint64_t mio_buffer_checksum(const uint8_t *data, size_t len) {
    uint64_t hash = 14695981039346656037ULL;
    size_t i;
    if (data == NULL) return 0;
    for (i = 0; i < len; i++) { hash ^= data[i]; hash *= 1099511628211ULL; }
    return hash;
}
