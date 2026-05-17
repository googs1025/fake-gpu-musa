// libmusa.so stub —— MUSA Driver API 的伪实现。
//
// 暴露的符号是从 MUSA SDK 3.1.0 真实 libmusa.so(共 305 个 mu* 公开 API)里
// 用 nm -D 抽出后,按"introspection / 枚举 / 属性 / context / MemGetInfo"
// 这几类裁剪出来的。计算路径(kernel launch / memcpy / stream / event /
// graph)故意不实现 —— fake-gpu 不模拟计算,任何走到那里的调用都视为错误。
//
// 设计原则: 大声失败而不是伪造结果 —— device 枚举类返回 MUSA_ERROR_NO_DEVICE
// 让调用方看到"没有设备",计算类返回错误码而不是假成功。形状参照
// src/cuda/cuda_hook.cpp。
//
// 调用链:
//   容器代码 ──► dlopen("libmusa.so") ──► 命中 NRI 挂入的 libfakegpu.so
//                                          └─► 本文件中的 mu* 函数
//
#include "musa_subset.h"
#include "macro_common.h"
#include "trace_profile.h"

#include <cstring>

// ---- init / version / error ------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MUresult muInit(unsigned int Flags) {
    HOOK_TRACE_PROFILE("muInit");
    return MUSA_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDriverGetVersion(int *driverVersion) {
    HOOK_TRACE_PROFILE("muDriverGetVersion");
    if (!driverVersion) return MUSA_ERROR_INVALID_VALUE;
    *driverVersion = 4000;
    return MUSA_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muGetErrorName(MUresult error, const char **pStr) {
    HOOK_TRACE_PROFILE("muGetErrorName");
    if (!pStr) return MUSA_ERROR_INVALID_VALUE;
    *pStr = "MUSA_ERROR_UNKNOWN";
    return MUSA_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muGetErrorString(MUresult error, const char **pStr) {
    HOOK_TRACE_PROFILE("muGetErrorString");
    if (!pStr) return MUSA_ERROR_INVALID_VALUE;
    *pStr = "fake-gpu musa stub: no device";
    return MUSA_SUCCESS;
}

// ---- device enumeration ----------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGetCount(int *count) {
    HOOK_TRACE_PROFILE("muDeviceGetCount");
    if (!count) return MUSA_ERROR_INVALID_VALUE;
    *count = 0;
    return MUSA_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGet(MUdevice *device, int ordinal) {
    HOOK_TRACE_PROFILE("muDeviceGet");
    if (device) *device = 0;
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGetName(char *name, int len, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDeviceGetName");
    if (name && len > 0) name[0] = '\0';
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGetUuid_v2(MUuuid *uuid, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDeviceGetUuid_v2");
    if (uuid) std::memset(uuid->bytes, 0, sizeof(uuid->bytes));
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGetAttribute(int *pi, MUdevice_attribute attrib, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDeviceGetAttribute");
    if (pi) *pi = 0;
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceComputeCapability(int *major, int *minor, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDeviceComputeCapability");
    if (major) *major = 0;
    if (minor) *minor = 0;
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceTotalMem_v2(size_t *bytes, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDeviceTotalMem_v2");
    if (bytes) *bytes = 0;
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGetPCIBusId(char *pciBusId, int len, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDeviceGetPCIBusId");
    if (pciBusId && len > 0) pciBusId[0] = '\0';
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDeviceGetByPCIBusId(MUdevice *dev, const char *pciBusId) {
    HOOK_TRACE_PROFILE("muDeviceGetByPCIBusId");
    if (dev) *dev = 0;
    return MUSA_ERROR_NO_DEVICE;
}

// ---- primary context -------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MUresult muDevicePrimaryCtxRetain(MUcontext *pctx, MUdevice dev) {
    HOOK_TRACE_PROFILE("muDevicePrimaryCtxRetain");
    if (pctx) *pctx = nullptr;
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muDevicePrimaryCtxRelease_v2(MUdevice dev) {
    HOOK_TRACE_PROFILE("muDevicePrimaryCtxRelease_v2");
    return MUSA_SUCCESS;
}

// ---- explicit context ------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MUresult muCtxCreate_v2(MUcontext *pctx, unsigned int flags, MUdevice dev) {
    HOOK_TRACE_PROFILE("muCtxCreate_v2");
    if (pctx) *pctx = nullptr;
    return MUSA_ERROR_NO_DEVICE;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muCtxDestroy_v2(MUcontext ctx) {
    HOOK_TRACE_PROFILE("muCtxDestroy_v2");
    return MUSA_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muCtxGetCurrent(MUcontext *pctx) {
    HOOK_TRACE_PROFILE("muCtxGetCurrent");
    if (pctx) *pctx = nullptr;
    return MUSA_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MUresult muCtxSetCurrent(MUcontext ctx) {
    HOOK_TRACE_PROFILE("muCtxSetCurrent");
    return MUSA_ERROR_INVALID_CONTEXT;
}

// ---- memory query ----------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MUresult muMemGetInfo_v2(size_t *free, size_t *total) {
    HOOK_TRACE_PROFILE("muMemGetInfo_v2");
    if (free)  *free  = 0;
    if (total) *total = 0;
    return MUSA_ERROR_NOT_INITIALIZED;
}
