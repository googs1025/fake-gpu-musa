// MTML hook implementation. Backs MTML calls with values from a YAML config
// file pointed to by FAKE_MUSA_CONFIG. Mirrors src/nvml/nvml_hook.cpp in
// shape; symbol surface tracks the vendored mtml_2.2.0.h.

#include "mtml_subset.h"
#include "musa_common.h"
#include "macro_common.h"
#include "trace_profile.h"

#include <yaml-cpp/yaml.h>

#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <mutex>
#include <string>

namespace {

std::mutex g_mu;
MtGPUList  g_gpus;
bool       g_inited = false;

void load_config_locked() {
    if (g_inited) return;
    const char *path = std::getenv("FAKE_MUSA_CONFIG");
    if (!path) {
        std::cerr << "FAKE_MUSA_CONFIG not set" << std::endl;
        std::exit(EXIT_FAILURE);
    }
    YAML::Node cfg = YAML::LoadFile(path);
    for (const auto &n : cfg["moorethreads"]) {
        MtGPU g;
        n >> g;
        g_gpus.push_back(g);
    }
    if (const char *suffix = std::getenv("FAKE_MUSA_SUFFIX")) {
        for (auto &g : g_gpus) g.uuid += "-" + std::string(suffix);
    }
    if (const char *vis = std::getenv("MUSA_VISIBLE_DEVICES")) {
        if (std::strcmp(vis, "all") != 0) {
            MtGPUList filtered;
            std::string s(vis);
            size_t prev = 0, pos;
            while ((pos = s.find(',', prev)) != std::string::npos) {
                std::string token = s.substr(prev, pos - prev);
                for (auto &g : g_gpus) {
                    if (g.uuid == token) filtered.push_back(g);
                }
                prev = pos + 1;
            }
            std::string token = s.substr(prev);
            for (auto &g : g_gpus) {
                if (g.uuid == token) filtered.push_back(g);
            }
            g_gpus.swap(filtered);
        }
    }
    g_inited = true;
}

// Encode a device handle as (intptr_t)index + 1 so 0 is never valid.
const MtGPU *device_to_gpu(const MtmlDevice *d) {
    intptr_t idx = reinterpret_cast<intptr_t>(d) - 1;
    if (idx < 0 || idx >= static_cast<intptr_t>(g_gpus.size())) return nullptr;
    return &g_gpus[idx];
}

}  // namespace

// ---- library lifecycle -----------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInit(MtmlLibrary **lib) {
    HOOK_TRACE_PROFILE("mtmlLibraryInit");
    if (!lib) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    load_config_locked();
    *lib = reinterpret_cast<MtmlLibrary *>(0x1);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryShutDown(MtmlLibrary *lib) {
    HOOK_TRACE_PROFILE("mtmlLibraryShutDown");
    if (!lib) return MTML_ERROR_INVALID_ARGUMENT;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryGetVersion(const MtmlLibrary *lib, char *version, unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlLibraryGetVersion");
    if (!lib || !version || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(version, "2.2.0", length);
    version[length - 1] = '\0';
    return MTML_SUCCESS;
}

// ---- error string ----------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT const char *mtmlErrorString(MtmlReturn result) {
    HOOK_TRACE_PROFILE("mtmlErrorString");
    switch (result) {
        case MTML_SUCCESS:                return "Success";
        case MTML_ERROR_DRIVER_NOT_LOADED:return "Driver not loaded";
        case MTML_ERROR_DRIVER_FAILURE:   return "Driver failure";
        case MTML_ERROR_INVALID_ARGUMENT: return "Invalid argument";
        case MTML_ERROR_NOT_SUPPORTED:    return "Not supported";
        case MTML_ERROR_NO_PERMISSION:    return "No permission";
        case MTML_ERROR_INSUFFICIENT_SIZE:return "Insufficient size";
        case MTML_ERROR_NOT_FOUND:        return "Not found";
        case MTML_ERROR_INSUFFICIENT_MEMORY:return "Insufficient memory";
        case MTML_ERROR_DRIVER_TOO_OLD:   return "Driver too old";
        case MTML_ERROR_DRIVER_TOO_NEW:   return "Driver too new";
        case MTML_ERROR_TIMEOUT:          return "Timeout";
        case MTML_ERROR_RESOURCE_IS_BUSY: return "Resource is busy";
        case MTML_ERROR_UNKNOWN:          return "Unknown error";
        default:                          return "Unknown error";
    }
}

// ---- device init / free ----------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryCountDevice(const MtmlLibrary *lib, unsigned int *count) {
    HOOK_TRACE_PROFILE("mtmlLibraryCountDevice");
    if (!lib || !count) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    *count = static_cast<unsigned int>(g_gpus.size());
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitDeviceByIndex(const MtmlLibrary *lib, unsigned int index,
                                                                    MtmlDevice **dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitDeviceByIndex");
    if (!lib || !dev) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    if (index >= g_gpus.size()) return MTML_ERROR_INVALID_ARGUMENT;
    *dev = reinterpret_cast<MtmlDevice *>(static_cast<intptr_t>(index) + 1);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitDeviceByUuid(const MtmlLibrary *library, const char *uuid,
                                                                   MtmlDevice **dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitDeviceByUuid");
    if (!library || !uuid || !dev) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    for (size_t i = 0; i < g_gpus.size(); ++i) {
        if (g_gpus[i].uuid == uuid) {
            *dev = reinterpret_cast<MtmlDevice *>(static_cast<intptr_t>(i) + 1);
            return MTML_SUCCESS;
        }
    }
    return MTML_ERROR_NOT_FOUND;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitDeviceByPciSbdf(const MtmlLibrary *lib, const char *pciSbdf,
                                                                      MtmlDevice **dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitDeviceByPciSbdf");
    if (!lib || !pciSbdf || !dev) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    for (size_t i = 0; i < g_gpus.size(); ++i) {
        if (g_gpus[i].pci.bus_id == pciSbdf) {
            *dev = reinterpret_cast<MtmlDevice *>(static_cast<intptr_t>(i) + 1);
            return MTML_SUCCESS;
        }
    }
    return MTML_ERROR_NOT_FOUND;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryFreeDevice(MtmlDevice *dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryFreeDevice");
    if (!dev) return MTML_ERROR_INVALID_ARGUMENT;
    return MTML_SUCCESS;
}

// ---- system ----------------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitSystem(const MtmlLibrary *lib, MtmlSystem **sys) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitSystem");
    if (!lib || !sys) return MTML_ERROR_INVALID_ARGUMENT;
    *sys = reinterpret_cast<MtmlSystem *>(0x2);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryFreeSystem(MtmlSystem *sys) {
    HOOK_TRACE_PROFILE("mtmlLibraryFreeSystem");
    if (!sys) return MTML_ERROR_INVALID_ARGUMENT;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlSystemGetDriverVersion(const MtmlSystem *sys, char *version,
                                                                   unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlSystemGetDriverVersion");
    if (!sys || !version || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const std::string &v = g_gpus.empty() ? std::string("2.7.0")
                                          : g_gpus.front().driver_version;
    std::strncpy(version, v.c_str(), length);
    version[length - 1] = '\0';
    return MTML_SUCCESS;
}

// ---- device queries --------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetIndex(const MtmlDevice *dev, unsigned int *index) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetIndex");
    if (!dev || !index) return MTML_ERROR_INVALID_ARGUMENT;
    intptr_t idx = reinterpret_cast<intptr_t>(dev) - 1;
    std::lock_guard<std::mutex> lk(g_mu);
    if (idx < 0 || idx >= static_cast<intptr_t>(g_gpus.size())) return MTML_ERROR_INVALID_ARGUMENT;
    *index = static_cast<unsigned int>(idx);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetUUID(const MtmlDevice *dev, char *uuid, unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetUUID");
    if (!dev || !uuid || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(uuid, g->uuid.c_str(), length);
    uuid[length - 1] = '\0';
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetBrand(const MtmlDevice *dev, MtmlBrandType *type) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetBrand");
    if (!dev || !type) return MTML_ERROR_INVALID_ARGUMENT;
    *type = MTML_BRAND_MTT;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetName(const MtmlDevice *dev, char *name, unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetName");
    if (!dev || !name || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(name, g->name.c_str(), length);
    name[length - 1] = '\0';
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetPciInfo(const MtmlDevice *dev, MtmlPciInfo *pci) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetPciInfo");
    if (!dev || !pci) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    std::memset(pci, 0, sizeof(*pci));
    std::strncpy(pci->sbdf, g->pci.bus_id.c_str(), MTML_DEVICE_PCI_SBDF_BUFFER_SIZE - 1);
    pci->segment        = static_cast<unsigned int>(g->pci.domain_id);
    pci->bus            = static_cast<unsigned int>(g->pci.bus);
    pci->device         = static_cast<unsigned int>(g->pci.device_id);
    pci->pciDeviceId    = 0;
    pci->pciSubsystemId = static_cast<unsigned int>(g->pci.sub_system_id);
    return MTML_SUCCESS;
}

// mtmlDeviceGetMpcMode reports whether MPC (Multi Primary Core) is enabled.
// fake-gpu doesn't model the per-instance virtualization that real MPC
// implies; we just signal "supported but disabled" when mpc_count > 0 so
// callers like mthreads-gmi can render MPC Capable=YES. Devices with
// mpc_count == 0 return MTML_ERROR_NOT_SUPPORTED — same shape as real
// non-MPC silicon.
HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetMpcMode(const MtmlDevice *dev, MtmlMpcMode *mode) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetMpcMode");
    if (!dev || !mode) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    if (g->mpc_count <= 0) return MTML_ERROR_NOT_SUPPORTED;
    *mode = MTML_DEVICE_MPC_DISABLE;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetPowerUsage(const MtmlDevice *dev, unsigned int *power) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetPowerUsage");
    if (!dev || !power) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    *power = static_cast<unsigned int>(g->power.usage);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetGpuPath(const MtmlDevice *dev, char *path, unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetGpuPath");
    if (!dev || !path || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    intptr_t idx = reinterpret_cast<intptr_t>(dev) - 1;
    std::lock_guard<std::mutex> lk(g_mu);
    if (idx < 0 || idx >= static_cast<intptr_t>(g_gpus.size())) return MTML_ERROR_INVALID_ARGUMENT;
    std::string p = "/dev/mtgpu" + std::to_string(idx);
    std::strncpy(path, p.c_str(), length);
    path[length - 1] = '\0';
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetVbiosVersion(const MtmlDevice *dev, char *version, unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetVbiosVersion");
    if (!dev || !version || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(version, g->vbios_version.c_str(), length);
    version[length - 1] = '\0';
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetMtBiosVersion(const MtmlDevice *dev, char *version, unsigned int length) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetMtBiosVersion");
    if (!dev || !version || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(dev);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(version, g->mtbios_version.c_str(), length);
    version[length - 1] = '\0';
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetSerialNumber(const MtmlDevice *device, unsigned int length, char *serialNumber) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetSerialNumber");
    if (!device || !serialNumber || length == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_to_gpu(device);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(serialNumber, g->serial.c_str(), length);
    serialNumber[length - 1] = '\0';
    return MTML_SUCCESS;
}

// ---- sub-objects: Memory / GPU / VPU ---------------------------------------
// Sub-object handles encode the original device index in the low 32 bits and a
// type tag in the high bits, so a single dispatch helper can route any of them
// back to g_gpus without per-object bookkeeping.

namespace {

constexpr intptr_t kMemoryTag = 1LL << 32;
constexpr intptr_t kGpuTag    = 2LL << 32;
constexpr intptr_t kVpuTag    = 3LL << 32;

const MtGPU *handle_to_gpu(const void *h, intptr_t tag) {
    intptr_t v = reinterpret_cast<intptr_t>(h);
    if ((v & ~0xFFFFFFFFLL) != tag) return nullptr;
    intptr_t idx = (v & 0xFFFFFFFFLL) - 1;
    if (idx < 0 || idx >= static_cast<intptr_t>(g_gpus.size())) return nullptr;
    return &g_gpus[idx];
}

void *make_handle(const MtmlDevice *d, intptr_t tag) {
    intptr_t idx = reinterpret_cast<intptr_t>(d);
    return reinterpret_cast<void *>(tag | (idx & 0xFFFFFFFFLL));
}

}  // namespace

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceInitMemory(const MtmlDevice *dev, MtmlMemory **mem) {
    HOOK_TRACE_PROFILE("mtmlDeviceInitMemory");
    if (!mem) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    if (!device_to_gpu(dev)) return MTML_ERROR_INVALID_ARGUMENT;
    *mem = static_cast<MtmlMemory *>(make_handle(dev, kMemoryTag));
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceFreeMemory(MtmlMemory *mem) {
    HOOK_TRACE_PROFILE("mtmlDeviceFreeMemory");
    if (!mem) return MTML_ERROR_INVALID_ARGUMENT;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceInitGpu(const MtmlDevice *dev, MtmlGpu **gpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceInitGpu");
    if (!gpu) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    if (!device_to_gpu(dev)) return MTML_ERROR_INVALID_ARGUMENT;
    *gpu = static_cast<MtmlGpu *>(make_handle(dev, kGpuTag));
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceFreeGpu(MtmlGpu *gpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceFreeGpu");
    if (!gpu) return MTML_ERROR_INVALID_ARGUMENT;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceInitVpu(const MtmlDevice *dev, MtmlVpu **vpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceInitVpu");
    if (!vpu) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    if (!device_to_gpu(dev)) return MTML_ERROR_INVALID_ARGUMENT;
    *vpu = static_cast<MtmlVpu *>(make_handle(dev, kVpuTag));
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceFreeVpu(MtmlVpu *vpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceFreeVpu");
    if (!vpu) return MTML_ERROR_INVALID_ARGUMENT;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlMemoryGetTotal(const MtmlMemory *mem, unsigned long long *total) {
    HOOK_TRACE_PROFILE("mtmlMemoryGetTotal");
    if (!total) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = handle_to_gpu(mem, kMemoryTag);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    *total = g->memory.total;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlMemoryGetUsed(const MtmlMemory *mem, unsigned long long *used) {
    HOOK_TRACE_PROFILE("mtmlMemoryGetUsed");
    if (!used) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = handle_to_gpu(mem, kMemoryTag);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    *used = g->memory.total - g->memory.free;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlMemoryGetUtilization(const MtmlMemory *mem, unsigned int *utilization) {
    HOOK_TRACE_PROFILE("mtmlMemoryGetUtilization");
    if (!utilization) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = handle_to_gpu(mem, kMemoryTag);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    *utilization = g->utilization.memory;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlGpuGetUtilization(const MtmlGpu *gpu, unsigned int *utilization) {
    HOOK_TRACE_PROFILE("mtmlGpuGetUtilization");
    if (!utilization) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = handle_to_gpu(gpu, kGpuTag);
    if (!g) return MTML_ERROR_INVALID_ARGUMENT;
    *utilization = g->utilization.gpu;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlGpuGetTemperature(const MtmlGpu *gpu, int *temp) {
    HOOK_TRACE_PROFILE("mtmlGpuGetTemperature");
    if (!temp) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    if (!handle_to_gpu(gpu, kGpuTag)) return MTML_ERROR_INVALID_ARGUMENT;
    *temp = 45;
    return MTML_SUCCESS;
}
