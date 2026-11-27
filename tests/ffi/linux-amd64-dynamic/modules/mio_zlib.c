#include <stddef.h>
#include <stdint.h>
#include <zlib.h>

/* Returns the compressed size, or -zlib_error. */
int64_t mio_zlib_compress(const uint8_t *src, size_t src_len, uint8_t *dst, size_t dst_cap) {
    uLongf out = (uLongf)dst_cap;
    int rc;
    if (src == NULL || dst == NULL) return -100;
    rc = compress2(dst, &out, src, (uLong)src_len, Z_BEST_COMPRESSION);
    return rc == Z_OK ? (int64_t)out : -(int64_t)(rc < 0 ? -rc : rc);
}

/* Returns the decompressed size, or -zlib_error (-3 data error, -5 buffer error). */
int64_t mio_zlib_decompress(const uint8_t *src, size_t src_len, uint8_t *dst, size_t dst_cap) {
    uLongf out = (uLongf)dst_cap;
    int rc;
    if (src == NULL || dst == NULL) return -100;
    rc = uncompress(dst, &out, src, (uLong)src_len);
    return rc == Z_OK ? (int64_t)out : -(int64_t)(rc < 0 ? -rc : rc);
}

uint32_t mio_zlib_crc32(const uint8_t *data, size_t len) {
    return (uint32_t)crc32(crc32(0L, Z_NULL, 0), data, (uInt)len);
}

uint64_t mio_zlib_bound(const uint8_t *unused, size_t len) {
    (void)unused;
    return (uint64_t)compressBound((uLong)len);
}
