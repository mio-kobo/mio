// Fault injection for kiso/ffi/linux cleanup tests: an LD_PRELOAD library whose dlclose fails the first
// MIO_FFI_FAULT_DLCLOSE times it is called, then forwards to the real dlclose.
#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdlib.h>

static int remaining = -1;

int dlclose(void *handle) {
    static int (*real)(void *);
    if (remaining < 0) {
        const char *value = getenv("MIO_FFI_FAULT_DLCLOSE");
        remaining = value ? atoi(value) : 0;
    }
    if (remaining > 0) {
        remaining--;
        return -1;
    }
    if (!real) real = (int (*)(void *))dlsym(RTLD_NEXT, "dlclose");
    return real(handle);
}
