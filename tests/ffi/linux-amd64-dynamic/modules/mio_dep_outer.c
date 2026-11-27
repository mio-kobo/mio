#include <stdint.h>
int32_t mio_inner_value(void);
/* Resolved through the DT_NEEDED entry on libmio_dep_inner.so (found via $ORIGIN). */
int32_t mio_outer_value(void) { return mio_inner_value() + 1; }
