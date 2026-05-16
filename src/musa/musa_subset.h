#ifndef __MUSA_HOOK_MUSA_SUBSET_H__
#define __MUSA_HOOK_MUSA_SUBSET_H__

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// MUSA Driver API subset.
//
// MThreads does not ship a public musa.h equivalent. Symbol names and the
// shape of the type/error surface mirror CUDA Driver API (which MUSA closely
// clones), cross-referenced with the public symbol list extracted from
// libmusa.so in MUSA SDK 3.1.0. Only the surface needed by enumeration,
// attribute query, context, and MemGetInfo paths is declared here — compute
// APIs (kernel launch, stream, memcpy) are intentionally excluded.

typedef int MUresult;
typedef int MUdevice;
typedef struct MUctx_st *MUcontext;

typedef struct MUuuid_st {
    char bytes[16];
} MUuuid;

// Error codes (numeric values copied from CUDA Driver API)
#define MUSA_SUCCESS                       0
#define MUSA_ERROR_INVALID_VALUE           1
#define MUSA_ERROR_OUT_OF_MEMORY           2
#define MUSA_ERROR_NOT_INITIALIZED         3
#define MUSA_ERROR_DEINITIALIZED           4
#define MUSA_ERROR_NO_DEVICE             100
#define MUSA_ERROR_INVALID_DEVICE        101
#define MUSA_ERROR_INVALID_CONTEXT       201
#define MUSA_ERROR_NOT_SUPPORTED         801
#define MUSA_ERROR_UNKNOWN               999

// Device attribute enum — only sentinel values used by stubs.
// Real MUSA exposes a parallel of CU_DEVICE_ATTRIBUTE_*; full enumeration
// is omitted because all stubs return MUSA_ERROR_NO_DEVICE before reading it.
typedef int MUdevice_attribute;

#ifdef __cplusplus
}
#endif

#endif  // __MUSA_HOOK_MUSA_SUBSET_H__
