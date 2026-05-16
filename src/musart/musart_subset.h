#ifndef __MUSA_HOOK_MUSART_SUBSET_H__
#define __MUSA_HOOK_MUSART_SUBSET_H__

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// MUSA Runtime API subset.
//
// MThreads does not ship a public musa_runtime.h equivalent. Symbol names
// and shapes mirror CUDA Runtime API (which MUSA closely clones), cross
// referenced with the public symbol list extracted from libmusart.so in
// MUSA SDK 3.1.0. Only the enumeration / attribute / memory query surface
// is declared here — compute / memcpy / memset APIs are excluded.

enum musaError {
    musaSuccess                  = 0,
    musaErrorInvalidValue        = 1,
    musaErrorMemoryAllocation    = 2,
    musaErrorInitializationError = 3,
    musaErrorNoDevice            = 38,
    musaErrorInvalidDevice       = 101,
    musaErrorNotSupported        = 801,
    musaErrorUnknown             = 999,
};

typedef enum musaError musaError_t;

// Opaque device-property struct. The caller links against MThreads' real
// musa_runtime.h which carries the full layout; this hook only returns an
// error without writing into the buffer, so the layout is unused here.
struct musaDeviceProp;

#ifdef __cplusplus
}
#endif

#endif  // __MUSA_HOOK_MUSART_SUBSET_H__
