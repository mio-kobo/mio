#include "../export.h"
#include <string.h>
API const void *mio_utf8_data(void) { return "\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e"; }
API size_t mio_utf8_length(const void *bytes, size_t length) {
    const unsigned char *p = bytes; size_t count = 0;
    for (size_t i = 0; i < length; ++i) if ((p[i] & 0xc0) != 0x80) ++count;
    return count;
}
API size_t mio_utf8_output(void *out, size_t capacity) {
    static const unsigned char text[] = {0xe6,0x97,0xa5,0xe6,0x9c,0xac,0xe8,0xaa,0x9e};
    if (capacity < sizeof(text)) return sizeof(text);
    memcpy(out, text, sizeof(text)); return sizeof(text);
}
