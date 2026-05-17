// cuda-smoke.c — dlopen libfakegpu.so and verify the CUDA Driver/Runtime
// stubs return CUDA_ERROR_INVALID_VALUE / cudaErrorInvalidValue (T2/T3 of
// the dual-coverage matrix). Symmetric to musa-smoke.c — the design
// decision in src/cuda/cuda_hook.cpp and src/cudart/cudart_hook.cpp is
// "fail loud rather than fake compute"; this smoke test pins that
// contract in CI without needing a real CUDA SDK sample.
//
//   cuInit(0)                  -> CUDA_ERROR_INVALID_VALUE (1)
//   cuDeviceGetCount(&n)       -> CUDA_ERROR_INVALID_VALUE (1)
//   cudaSetDevice(0)           -> cudaErrorInvalidValue    (1)
//   cudaMemGetInfo(&f, &t)     -> cudaErrorInvalidValue    (1)
//
// Exit codes: 0 ok; 1 dlopen / dlsym failure; 10..13 individual checks.

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

    int (*cuInit)(unsigned)                  = (int (*)(unsigned))dlsym(h, "cuInit");
    int (*cuDevCount)(int *)                 = (int (*)(int *))dlsym(h, "cuDeviceGetCount");
    int (*cudaSet)(int)                      = (int (*)(int))dlsym(h, "cudaSetDevice");
    int (*cudaMemInfo)(size_t *, size_t *)   = (int (*)(size_t *, size_t *))dlsym(h, "cudaMemGetInfo");

    if (!cuInit || !cuDevCount || !cudaSet || !cudaMemInfo) {
        fprintf(stderr, "dlsym: missing one of cuInit/cuDeviceGetCount/cudaSetDevice/cudaMemGetInfo\n");
        return 1;
    }

    if (cuInit(0) != 1 /* CUDA_ERROR_INVALID_VALUE */) {
        fprintf(stderr, "cuInit(0) did not return CUDA_ERROR_INVALID_VALUE\n");
        return 10;
    }

    int n = -1;
    if (cuDevCount(&n) != 1 /* CUDA_ERROR_INVALID_VALUE */) {
        fprintf(stderr, "cuDeviceGetCount did not return CUDA_ERROR_INVALID_VALUE\n");
        return 11;
    }

    if (cudaSet(0) != 1 /* cudaErrorInvalidValue */) {
        fprintf(stderr, "cudaSetDevice did not return cudaErrorInvalidValue\n");
        return 12;
    }

    size_t free_b = 0, total_b = 0;
    if (cudaMemInfo(&free_b, &total_b) != 1 /* cudaErrorInvalidValue */) {
        fprintf(stderr, "cudaMemGetInfo did not return cudaErrorInvalidValue\n");
        return 13;
    }

    puts("cuda-smoke ok");
    return 0;
}
