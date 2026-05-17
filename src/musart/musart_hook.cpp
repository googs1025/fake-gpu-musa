#include "musart_subset.h"
#include "musa_common.h"
#include "macro_common.h"
#include "trace_profile.h"

#include <yaml-cpp/yaml.h>

#include <algorithm>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <mutex>
#include <string>

// libmusart.so stub.
//
// Symbols cover the enumeration / attribute / memory query subset extracted
// from MUSA SDK 3.1.0 libmusart.so (185 public musa* APIs total). Compute,
// memcpy, memset, and stream APIs are excluded — fake-gpu workloads never
// reach them. Query APIs (GetDeviceCount, GetDeviceProperties, MemGetInfo,
// SetDevice/GetDevice, GetPCIBusId) are fake-implemented from
// FAKE_MUSA_CONFIG yaml so tools like musaInfo (MUSA SDK deviceQuery clone)
// can render device tables. Compute APIs (Malloc/Free) still return errors,
// matching the design doc's "no fake compute" contract.

namespace {

std::mutex g_mu;
MtGPUList  g_gpus;
bool       g_inited = false;

// Per-thread current device. CUDA Runtime semantics: each host thread tracks
// its own "current" device set by cudaSetDevice; default 0. We mirror.
thread_local int tl_current_device = 0;

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
            // Accept both numeric indices ("0,1") and full UUIDs
            // ("MTGPU-0[-suffix]") — see mtml_hook.cpp for rationale.
            auto match = [](const MtGPU &g, size_t idx, const std::string &t) {
                if (g.uuid == t) return true;
                if (!t.empty() && t.find_first_not_of("0123456789") == std::string::npos) {
                    return idx == static_cast<size_t>(std::stoul(t));
                }
                return false;
            };
            MtGPUList filtered;
            std::string s(vis);
            size_t prev = 0, pos;
            auto consume = [&](const std::string &token) {
                for (size_t i = 0; i < g_gpus.size(); ++i) {
                    if (match(g_gpus[i], i, token)) filtered.push_back(g_gpus[i]);
                }
            };
            while ((pos = s.find(',', prev)) != std::string::npos) {
                consume(s.substr(prev, pos - prev));
                prev = pos + 1;
            }
            consume(s.substr(prev));
            g_gpus.swap(filtered);
        }
    }
    g_inited = true;
}

// ensure_inited — load config once, lazy. Called from every entry point so
// the first musa* call from any thread triggers the read.
void ensure_inited() {
    std::lock_guard<std::mutex> lk(g_mu);
    load_config_locked();
}

const MtGPU *device_at(int idx) {
    if (idx < 0 || idx >= static_cast<int>(g_gpus.size())) return nullptr;
    return &g_gpus[idx];
}

}  // namespace

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
    return "musaSuccess";
}

HOOK_C_API HOOK_DECL_EXPORT const char *musaGetErrorString(musaError_t error) {
    HOOK_TRACE_PROFILE("musaGetErrorString");
    return "fake-gpu musart stub";
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetLastError(void) {
    HOOK_TRACE_PROFILE("musaGetLastError");
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaPeekAtLastError(void) {
    HOOK_TRACE_PROFILE("musaPeekAtLastError");
    return musaSuccess;
}

// ---- device enumeration / attributes ---------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceCount(int *count) {
    HOOK_TRACE_PROFILE("musaGetDeviceCount");
    if (!count) return musaErrorInvalidValue;
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    *count = static_cast<int>(g_gpus.size());
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDevice(int *device) {
    HOOK_TRACE_PROFILE("musaGetDevice");
    if (!device) return musaErrorInvalidValue;
    ensure_inited();
    *device = tl_current_device;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaSetDevice(int device) {
    HOOK_TRACE_PROFILE("musaSetDevice");
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    if (device < 0 || device >= static_cast<int>(g_gpus.size())) {
        return musaErrorInvalidDevice;
    }
    tl_current_device = device;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceFlags(unsigned int *flags) {
    HOOK_TRACE_PROFILE("musaGetDeviceFlags");
    if (!flags) return musaErrorInvalidValue;
    *flags = 0;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaSetDeviceFlags(unsigned int flags) {
    HOOK_TRACE_PROFILE("musaSetDeviceFlags");
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceProperties(struct musaDeviceProp *prop, int device) {
    HOOK_TRACE_PROFILE("musaGetDeviceProperties");
    if (!prop) return musaErrorInvalidValue;
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_at(device);
    if (!g) return musaErrorInvalidDevice;

    std::memset(prop, 0, sizeof(*prop));

    std::strncpy(prop->name, g->name.c_str(), sizeof(prop->name) - 1);
    // UUID: pack the yaml string into the 16-byte slot. Real MUSA UUIDs are
    // binary; the fake's "MTGPU-0" is human-readable, so truncate-to-fit
    // keeps musaInfo's hex print readable.
    std::memcpy(prop->uuid.bytes, g->uuid.c_str(),
                std::min<size_t>(sizeof(prop->uuid.bytes), g->uuid.size()));

    prop->totalGlobalMem              = static_cast<size_t>(g->memory.total);
    prop->multiProcessorCount         = g->compute.multiprocessor_count;
    prop->clockRate                   = g->compute.clock_rate_khz;
    prop->memoryClockRate             = g->compute.memory_clock_rate_khz;
    prop->memoryBusWidth              = g->compute.memory_bus_width;
    prop->l2CacheSize                 = g->compute.l2_cache_size;
    prop->major                       = g->compute.capability_major;
    prop->minor                       = g->compute.capability_minor;
    prop->warpSize                    = g->compute.warp_size;
    prop->maxThreadsPerBlock          = g->compute.max_threads_per_block;
    prop->maxThreadsPerMultiProcessor = g->compute.max_threads_per_mp;
    for (int i = 0; i < 3; ++i) prop->maxThreadsDim[i] = g->compute.max_block_dim[i];
    for (int i = 0; i < 3; ++i) prop->maxGridSize[i]   = g->compute.max_grid_dim[i];
    prop->sharedMemPerBlock           = g->compute.shared_mem_per_block;
    prop->sharedMemPerMultiprocessor  = g->compute.shared_mem_per_mp;
    prop->totalConstMem               = g->compute.total_const_mem;
    prop->regsPerBlock                = g->compute.regs_per_block;
    prop->regsPerMultiprocessor       = g->compute.regs_per_mp;
    prop->textureAlignment            = g->compute.texture_alignment;
    prop->memPitch                    = g->compute.mem_pitch;

    prop->pciBusID    = g->pci.bus;
    prop->pciDeviceID = g->pci.device_id;
    prop->pciDomainID = g->pci.domain_id;

    prop->computeMode                  = 0;  // cudaComputeModeDefault
    prop->canMapHostMemory             = 1;
    prop->unifiedAddressing            = 1;
    prop->concurrentKernels            = 1;
    prop->streamPrioritiesSupported    = 1;
    prop->asyncEngineCount             = 2;
    prop->maxBlocksPerMultiProcessor   = 16;
    prop->ECCEnabled                   = 0;
    prop->integrated                   = 0;
    prop->tccDriver                    = 0;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceGetAttribute(int *value, int attr, int device) {
    HOOK_TRACE_PROFILE("musaDeviceGetAttribute");
    if (!value) return musaErrorInvalidValue;
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    if (!device_at(device)) return musaErrorInvalidDevice;
    // musaDeviceAttr enum is closed-source and covers 100+ attrs. Returning
    // 0+success means "feature not supported" for boolean caps, which is a
    // safe default for callers probing optional features without crashing.
    *value = 0;
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceGetPCIBusId(char *pciBusId, int len, int device) {
    HOOK_TRACE_PROFILE("musaDeviceGetPCIBusId");
    if (!pciBusId || len <= 0) return musaErrorInvalidValue;
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_at(device);
    if (!g) return musaErrorInvalidDevice;
    std::strncpy(pciBusId, g->pci.bus_id.c_str(), len - 1);
    pciBusId[len - 1] = '\0';
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceGetByPCIBusId(int *device, const char *pciBusId) {
    HOOK_TRACE_PROFILE("musaDeviceGetByPCIBusId");
    if (!device || !pciBusId) return musaErrorInvalidValue;
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    for (size_t i = 0; i < g_gpus.size(); ++i) {
        if (g_gpus[i].pci.bus_id == pciBusId) {
            *device = static_cast<int>(i);
            return musaSuccess;
        }
    }
    return musaErrorInvalidDevice;
}

// ---- device lifecycle ------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceSynchronize(void) {
    HOOK_TRACE_PROFILE("musaDeviceSynchronize");
    return musaSuccess;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaDeviceReset(void) {
    HOOK_TRACE_PROFILE("musaDeviceReset");
    return musaSuccess;
}

// ---- memory ----------------------------------------------------------------

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaMemGetInfo(size_t *free, size_t *total) {
    HOOK_TRACE_PROFILE("musaMemGetInfo");
    if (!free || !total) return musaErrorInvalidValue;
    ensure_inited();
    std::lock_guard<std::mutex> lk(g_mu);
    const MtGPU *g = device_at(tl_current_device);
    if (!g) return musaErrorNoDevice;
    *total = static_cast<size_t>(g->memory.total);
    *free  = static_cast<size_t>(g->memory.free);
    return musaSuccess;
}

// Compute path stays fail-loud per design doc §4.4: fake-gpu does not
// pretend to allocate, copy, launch kernels, etc.
HOOK_C_API HOOK_DECL_EXPORT musaError_t musaMalloc(void **devPtr, size_t size) {
    HOOK_TRACE_PROFILE("musaMalloc");
    if (devPtr) *devPtr = nullptr;
    return musaErrorMemoryAllocation;
}

HOOK_C_API HOOK_DECL_EXPORT musaError_t musaFree(void *devPtr) {
    HOOK_TRACE_PROFILE("musaFree");
    return musaSuccess;
}