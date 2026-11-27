#include <stdint.h>
#include <stdlib.h>

struct mio_state { int32_t magic; int32_t value; };
#define MIO_STATE_MAGIC 0x4d494f53

void *mio_state_create(void) {
    struct mio_state *state = malloc(sizeof *state);
    if (state == NULL) return NULL;
    state->magic = MIO_STATE_MAGIC;
    state->value = 0;
    return state;
}

/* Error codes: -1 for a null handle, -2 for a handle that is not a live state. */
int32_t mio_state_increment(void *handle) {
    struct mio_state *state = handle;
    if (state == NULL) return -1;
    if (state->magic != MIO_STATE_MAGIC) return -2;
    return ++state->value;
}

int32_t mio_state_get(void *handle) {
    struct mio_state *state = handle;
    if (state == NULL) return -1;
    if (state->magic != MIO_STATE_MAGIC) return -2;
    return state->value;
}

void mio_state_destroy(void *handle) {
    struct mio_state *state = handle;
    if (state == NULL) return;
    state->magic = 0;
    free(state);
}
