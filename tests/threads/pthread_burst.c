// Evidence, not a Mio test: create and join threads back to back with a plain pthread loop, using the 8 MiB stack attribute of the Darwin worker
// (bunko/kiso/runtime/os/darwin/worker.mio) and the shape of benchmarks/threads/efficiency.mio start_join. No Mio code runs here.
//
// A Darwin `start_join` sample once ended with status 41, a failed pthread creation inside thread_open_shared. This program shows whether the OS alone
// refuses pthread_create under that burst on the same runner, and with which errno. Unlike a Mio program it does not stop at the first refusal: it counts
// them, so a rate can be read off. A refusal is never retried or hidden.
//
// usage: pthread_burst [threads]            prints one line: "pthread_burst threads=N created=C refused=R first_refused_at=I first_errno=E"
// exit status: 0 when every thread was created and joined, 1 when any creation was refused, 2 on a join failure or bad usage
#include <errno.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>

#define WORKER_STACK_BYTES 8388608

static void *nothing(void *argument) { return argument; }

int main(int argc, char **argv) {
    long threads = 20000;
    if (argc > 2) { fprintf(stderr, "usage: %s [threads]\n", argv[0]); return 2; }
    if (argc == 2) {
        char *end = NULL;
        threads = strtol(argv[1], &end, 10);
        if (end == argv[1] || *end != '\0' || threads < 1) { fprintf(stderr, "bad thread count\n"); return 2; }
    }
    pthread_attr_t attributes;
    if (pthread_attr_init(&attributes) != 0 || pthread_attr_setstacksize(&attributes, WORKER_STACK_BYTES) != 0) { fprintf(stderr, "attribute setup failed\n"); return 2; }
    long created = 0, refused = 0, first_at = -1;
    int first_errno = 0;
    for (long index = 0; index < threads; index++) {
        pthread_t thread;
        int result = pthread_create(&thread, &attributes, nothing, NULL);
        if (result != 0) {
            if (refused++ == 0) { first_at = index; first_errno = result; }
            continue;
        }
        created++;
        if (pthread_join(thread, NULL) != 0) { fprintf(stderr, "join failed at %ld\n", index); return 2; }
    }
    pthread_attr_destroy(&attributes);
    printf("pthread_burst threads=%ld created=%ld refused=%ld first_refused_at=%ld first_errno=%d\n", threads, created, refused, first_at, first_errno);
    return refused ? 1 : 0;
}
