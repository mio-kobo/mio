#include "../export.h"
#include <stdlib.h>
typedef struct { int64_t value; } State;
static uint32_t live;
API void *mio_state_new(int64_t value) {
    State *s = malloc(sizeof(*s)); if (s) { s->value = value; ++live; } return s;
}
API int64_t mio_state_get(const void *s) { return ((const State *)s)->value; }
API void mio_state_set(void *s, int64_t value) { ((State *)s)->value = value; }
API void mio_state_free(void *s) { if (s) { free(s); --live; } }
API uint32_t mio_state_live(void) { return live; }
