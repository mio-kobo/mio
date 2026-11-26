#include <dlfcn.h>
#include <stdio.h>
#include <string.h>

int main(int argc, char **argv) {
    if (argc != 2) {
        return 2;
    }
    void *framework = dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
                            RTLD_NOW | RTLD_LOCAL);
    if (!framework) {
        fprintf(stderr, "%s\n", dlerror());
        return 3;
    }
    dlclose(framework);

    void *foreign = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (foreign) {
        dlclose(foreign);
        fprintf(stderr, "library validation allowed an ad-hoc third-party image\n");
        return 4;
    }
    const char *error = dlerror();
    if (!error) {
        return 5;
    }
    puts(error);
    if (!strstr(error, "signature") && !strstr(error, "Team ID") && !strstr(error, "Library Validation")) {
        return 6;
    }
    puts("Darwin library validation: ok");
    return 0;
}
