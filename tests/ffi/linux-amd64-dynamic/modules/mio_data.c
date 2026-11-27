// Exported data next to a function, for the function-versus-data checks of kiso/ffi/linux.
// Built twice: normally (read-only data in a non-executable segment) and with -z noseparate-code, which places
// .rodata in the executable segment so that only the ELF symbol type identifies it as data.
#include <stdint.h>

const int32_t mio_data_table[4] = {1, 2, 3, 4};
int32_t mio_data_counter = 7;

int32_t mio_data_function(int32_t x) { return x + mio_data_table[3] + mio_data_counter; }
