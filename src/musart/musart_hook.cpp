#include "musart_subset.h"
#include "macro_common.h"
#include "trace_profile.h"

// libmusart.so stub.
//
// Symbols cover the enumeration / attribute / memory query subset extracted
// from MUSA SDK 3.1.0 libmusart.so (185 public musa* APIs total). Compute,
// memcpy, memset, and stream APIs are excluded — fake-gpu workloads never
// reach them. Every entry returns an error so callers see "no device"
// instead of crashing, mirroring src/cudart/cudart_hook.cpp.

// ---- version / error -------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaRuntimeGetVersion(int *runtimeVersion) {
    HOOK_TRACE_PROFILE("musaRuntimeGetVersion");
    if (!runtimeVersion) return musaErrorInvalidValue;
    *runtimeVersion = 4000;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDriverGetVersion(int *driverVersion) {
    HOOK_TRACE_PROFILE("musaDriverGetVersion");
    if (!driverVersion) return musaErrorInvalidValue;
    *driverVersion = 4000;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT const char *musaGetErrorName(musaError_t error) {
    HOOK_TRACE_PROFILE("musaGetErrorName");
    return "musaErrorNoDevice";
}

HOOK_C_API HOOK_DECL_EXPORT const char *musaGetErrorString(musaError_t error) {
    HOOK_TRACE_PROFILE("musaGetErrorString");
    return "fake-gpu musart stub: no device";
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetLastError(void) {
    HOOK_TRACE_PROFILE("musaGetLastError");
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaPeekAtLastError(void) {
    HOOK_TRACE_PROFILE("musaPeekAtLastError");
    return musaErrorNoDevice;
}

// ---- device enumeration / attributes ---------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceCount(int *count) {
    HOOK_TRACE_PROFILE("musaGetDeviceCount");
    if (!count) return musaErrorInvalidValue;
    *count = 0;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDevice(int *device) {
    HOOK_TRACE_PROFILE("musaGetDevice");
    if (device) *device = 0;
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaSetDevice(int device) {
    HOOK_TRACE_PROFILE("musaSetDevice");
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceFlags(unsigned int *flags) {
    HOOK_TRACE_PROFILE("musaGetDeviceFlags");
    if (flags) *flags = 0;
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaSetDeviceFlags(unsigned int flags) {
    HOOK_TRACE_PROFILE("musaSetDeviceFlags");
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceProperties(struct musaDeviceProp *prop, int device) {
    HOOK_TRACE_PROFILE("musaGetDeviceProperties");
    return musaErrorInvalidValue;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceGetAttribute(int *value, int attr, int device) {
    HOOK_TRACE_PROFILE("musaDeviceGetAttribute");
    if (value) *value = 0;
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceGetPCIBusId(char *pciBusId, int len, int device) {
    HOOK_TRACE_PROFILE("musaDeviceGetPCIBusId");
    if (pciBusId && len > 0) pciBusId[0] = '\0';
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceGetByPCIBusId(int *device, const char *pciBusId) {
    HOOK_TRACE_PROFILE("musaDeviceGetByPCIBusId");
    if (device) *device = 0;
    return musaErrorNoDevice;
}

// ---- device lifecycle ------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceSynchronize(void) {
    HOOK_TRACE_PROFILE("musaDeviceSynchronize");
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceReset(void) {
    HOOK_TRACE_PROFILE("musaDeviceReset");
    return musaSuccess;
}

// ---- memory ----------------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaMemGetInfo(size_t *free, size_t *total) {
    HOOK_TRACE_PROFILE("musaMemGetInfo");
    if (free)  *free  = 0;
    if (total) *total = 0;
    return musaErrorNoDevice;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaMalloc(void **devPtr, size_t size) {
    HOOK_TRACE_PROFILE("musaMalloc");
    if (devPtr) *devPtr = nullptr;
    return musaErrorMemoryAllocation;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaFree(void *devPtr) {
    HOOK_TRACE_PROFILE("musaFree");
    return musaSuccess;
}
