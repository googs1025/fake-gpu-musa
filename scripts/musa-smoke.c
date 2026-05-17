// musa-smoke.c — dlopen libfakegpu.so and verify MUSA Driver vs Runtime
// contracts (T6/T7 of the dual coverage matrix).
//
// Two layers, two contracts:
//   - Driver API (mu*, libmusa.so): fail-loud. fake-gpu does not fake the
//     driver because the driver layer rarely runs without real silicon, so
//     callers that reach it deserve a clear "no device" signal.
//   - Runtime API (musa*, libmusart.so): fake-implemented for query paths
//     (GetDeviceCount, SetDevice, GetDeviceProperties, MemGetInfo) so
//     deviceQuery-style tools (musaInfo) render a plausible table. Compute
//     APIs (Malloc/Free/Memcpy) still fail-loud.
//
// Pinned expectations:
//   muInit(0)                 -> 0 (MUSA_SUCCESS)
//   muDeviceGetCount(&n)      -> 0 with n == 0          (driver: no device)
//   muMemGetInfo_v2(&f, &t)   -> MU_ERROR_NOT_INITIALIZED (3)
//   musaGetDeviceCount(&n)    -> 0 with n >= 1          (runtime: yaml-backed)
//   musaSetDevice(0)          -> 0 (musaSuccess)        (runtime: yaml-backed)
//
// Exit codes: 0 ok; 1 dlopen failure; 10..14 individual check failures.

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
    int  (*musaCount)(int *)                  = (int (*)(int *))dlsym(h, "musaGetDeviceCount");
    int  (*musaSet)(int)                      = (int (*)(int))dlsym(h, "musaSetDevice");

    if (!muInit || !muDevCount || !muMemInfo || !musaCount || !musaSet) {
        fprintf(stderr, "dlsym: missing one of muInit/muDeviceGetCount/"
                        "muMemGetInfo_v2/musaGetDeviceCount/musaSetDevice\n");
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

    int rn = -1;
    rc = musaCount(&rn);
    if (rc != 0 || rn < 1) {
        fprintf(stderr, "musaGetDeviceCount: rc=%d count=%d (expected rc=0 count>=1)\n", rc, rn);
        return 13;
    }

    rc = musaSet(0);
    if (rc != 0 /* musaSuccess */) {
        fprintf(stderr, "musaSetDevice: rc=%d (expected 0 / musaSuccess)\n", rc);
        return 14;
    }

    puts("musa-smoke ok");
    return 0;
}
