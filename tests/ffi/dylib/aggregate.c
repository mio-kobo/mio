#include <stddef.h>
#include <stdint.h>

struct darwin_pair {
    uint16_t count;
    double scale;
};

union darwin_storage {
    uint32_t word;
    uint8_t bytes[8];
};

struct darwin_packet {
    uint8_t tag;
    struct darwin_pair rows[3];
    union darwin_storage data;
    int64_t *external;
};

int64_t aggregate_measure(int64_t item) {
    const int64_t results[] = {
        sizeof(struct darwin_pair), _Alignof(struct darwin_pair),
        offsetof(struct darwin_pair, scale),
        sizeof(union darwin_storage), _Alignof(union darwin_storage),
        sizeof(struct darwin_packet), _Alignof(struct darwin_packet),
        offsetof(struct darwin_packet, tag), offsetof(struct darwin_packet, rows),
        offsetof(struct darwin_packet, data), offsetof(struct darwin_packet, external),
    };
    return item >= 0 && (size_t)item < sizeof(results) / sizeof(results[0]) ? results[item] : -1;
}

int64_t aggregate_update(struct darwin_packet *packet) {
    if (!packet || packet->tag != 7 || packet->rows[1].count != 123 ||
        packet->rows[1].scale != -2.25 || packet->data.word != UINT32_C(0x1234abcd) ||
        !packet->external || *packet->external != -12) {
        return -1;
    }
    packet->tag = 42;
    packet->rows[2].count = 0x1253;
    return 42;
}
