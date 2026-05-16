// musa-smoke.c — dlopen libfakegpu.so and verify the MUSA Driver/Runtime
// stubs return the documented "fail loud" error codes (T6/T7 of the dual
// coverage matrix). The design decision is "return error rather than fake
// compute"; this smoke test pins that contract in CI.
//
//   muInit(0)                 -> 0 (MUSA_SUCCESS)
//   muDeviceGetCount(&n)      -> 0 with n == 0
//   muMemGetInfo_v2(&f, &t)   -> MU_ERROR_NOT_INITIALIZED (3)
//   musaSetDevice(0)          -> musaErrorNoDevice (38)
//
// Exit codes: 0 ok; 1 dlopen failure; 10..13 individual check failures.

#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <stddef.h>

int main(void) {
    const char *path = getenv("LIB");
    if (!path || !*path) path = "libfakegpu.so";

    void *h = dlopen(path, RTLD_NOW);
    if (!h) {
        fprintf(stderr, "dlopen(%s): %s\n", path, dlerror());
        return 1;
    }

    int  (*muInit)(unsigned)                  = (int (*)(unsigned))dlsym(h, "muInit");
    int  (*muDevCount)(int *)                 = (int (*)(int *))dlsym(h, "muDeviceGetCount");
    int  (*muMemInfo)(size_t *, size_t *)     = (int (*)(size_t *, size_t *))dlsym(h, "muMemGetInfo_v2");
    int  (*musaSet)(int)                      = (int (*)(int))dlsym(h, "musaSetDevice");

    if (!muInit || !muDevCount || !muMemInfo || !musaSet) {
        fprintf(stderr, "dlsym: missing one of muInit/muDeviceGetCount/muMemGetInfo_v2/musaSetDevice\n");
        return 1;
    }

    if (muInit(0) != 0) {
        fprintf(stderr, "muInit(0) did not return MUSA_SUCCESS\n");
        return 10;
    }

    int n = -1;
    if (muDevCount(&n) != 0 || n != 0) {
        fprintf(stderr, "muDeviceGetCount: rc=%d count=%d (expected rc=0 count=0)\n",
                muDevCount(&n), n);
        return 11;
    }

    size_t free_b = 0, total_b = 0;
    int rc = muMemInfo(&free_b, &total_b);
    if (rc != 3 /* MU_ERROR_NOT_INITIALIZED */) {
        fprintf(stderr, "muMemGetInfo_v2: rc=%d (expected 3 / MU_ERROR_NOT_INITIALIZED)\n", rc);
        return 12;
    }

    rc = musaSet(0);
    if (rc != 38 /* musaErrorNoDevice */) {
        fprintf(stderr, "musaSetDevice: rc=%d (expected 38 / musaErrorNoDevice)\n", rc);
        return 13;
    }

    puts("musa-smoke ok");
    return 0;
}
