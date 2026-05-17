#pragma once

#include <yaml-cpp/yaml.h>

#include <string>
#include <vector>

#include "common.h"  // reuse RAM, PCI, GPU_Util, GPU_Power, NUMA

// MtLink — Moore Threads' GPU interconnect, parallels NVLink.
struct MtLink {
    std::string version;
    int capacity;
    int bandwidth;
    std::vector<std::string> peer_gpus;

    friend void operator>>(const YAML::Node &node, MtLink &l) {
        l.version   = node["version"].as<std::string>();
        l.capacity  = node["capacity"].as<int>();
        l.bandwidth = node["bandwidth"].as<int>();
        if (node["peer_gpu_uuids"]) {
            for (const auto &peer : node["peer_gpu_uuids"]) {
                l.peer_gpus.push_back(peer.as<std::string>());
            }
        }
    }
};

// MtCompute — fields backing musaDeviceProp / musaInfo "deviceQuery" output.
// All fields are optional in yaml; defaults below mirror MTT S80 published
// specs so an unset yaml still produces a plausible musaInfo table.
struct MtCompute {
    int capability_major          = 2;
    int capability_minor          = 2;
    int multiprocessor_count      = 32;
    int clock_rate_khz            = 1900000;        // 1.90 GHz
    int memory_clock_rate_khz     = 1000000;        // 1.00 GHz
    int memory_bus_width          = 256;            // bits
    int l2_cache_size             = 4 * 1024 * 1024;  // 4 MiB
    int warp_size                 = 128;
    int max_threads_per_block     = 1024;
    int max_threads_per_mp        = 2048;
    int max_block_dim[3]          = {1024, 1024, 64};
    int max_grid_dim[3]           = {2147483647, 65535, 65535};
    size_t shared_mem_per_block   = 49152;
    size_t shared_mem_per_mp      = 65536;
    size_t total_const_mem        = 65536;
    int regs_per_block            = 65536;
    int regs_per_mp               = 65536;
    size_t texture_alignment      = 512;
    size_t mem_pitch              = 2147483647;

    friend void operator>>(const YAML::Node &n, MtCompute &c) {
        if (n["capability_major"])      c.capability_major      = n["capability_major"].as<int>();
        if (n["capability_minor"])      c.capability_minor      = n["capability_minor"].as<int>();
        if (n["multiprocessor_count"])  c.multiprocessor_count  = n["multiprocessor_count"].as<int>();
        if (n["clock_rate_khz"])        c.clock_rate_khz        = n["clock_rate_khz"].as<int>();
        if (n["memory_clock_rate_khz"]) c.memory_clock_rate_khz = n["memory_clock_rate_khz"].as<int>();
        if (n["memory_bus_width"])      c.memory_bus_width      = n["memory_bus_width"].as<int>();
        if (n["l2_cache_size"])         c.l2_cache_size         = n["l2_cache_size"].as<int>();
        if (n["warp_size"])             c.warp_size             = n["warp_size"].as<int>();
        if (n["max_threads_per_block"]) c.max_threads_per_block = n["max_threads_per_block"].as<int>();
        if (n["max_threads_per_mp"])    c.max_threads_per_mp    = n["max_threads_per_mp"].as<int>();
        if (n["shared_mem_per_block"])  c.shared_mem_per_block  = n["shared_mem_per_block"].as<size_t>();
        if (n["shared_mem_per_mp"])     c.shared_mem_per_mp     = n["shared_mem_per_mp"].as<size_t>();
        if (n["total_const_mem"])       c.total_const_mem       = n["total_const_mem"].as<size_t>();
        if (n["regs_per_block"])        c.regs_per_block        = n["regs_per_block"].as<int>();
        if (n["regs_per_mp"])           c.regs_per_mp           = n["regs_per_mp"].as<int>();
        if (n["texture_alignment"])     c.texture_alignment     = n["texture_alignment"].as<size_t>();
        if (n["mem_pitch"])             c.mem_pitch             = n["mem_pitch"].as<size_t>();
    }
};

// MtGPU — Moore Threads GPU descriptor, parallels GPU in common.h.
//   - mpc_count is the MUSA equivalent of MIG max_count (0 = MPC disabled).
//   - mtbios_version is MThreads-specific firmware string; vbios_version
//     keeps the conventional name used by NVIDIA tools.
//   - compute backs musart's musaGetDeviceProperties / musaInfo output.
struct MtGPU {
    std::string  name;
    std::string  uuid;
    std::string  driver_version;
    std::string  brand;
    std::string  vbios_version;
    std::string  mtbios_version;
    std::string  serial;
    int          mpc_count;
    RAM          memory;
    PCI          pci;
    NUMA         numa;
    GPU_Power    power;
    GPU_Util     utilization;
    MtLink       mtlink;
    MtCompute    compute;

    friend void operator>>(const YAML::Node &n, MtGPU &g) {
        g.name           = n["name"].as<std::string>();
        g.uuid           = n["uuid"].as<std::string>();
        g.driver_version = n["driver_version"].as<std::string>();
        g.brand          = n["brand"].as<std::string>();
        g.vbios_version  = n["vbios_version"].as<std::string>();
        g.mtbios_version = n["mtbios_version"].as<std::string>();
        g.serial         = n["serial"].as<std::string>();
        g.mpc_count      = n["mpc_count"] ? n["mpc_count"].as<int>() : 0;
        n["memory"]      >> g.memory;
        n["pci"]         >> g.pci;
        n["numa"]        >> g.numa;
        n["power"]       >> g.power;
        n["utilization"] >> g.utilization;
        n["mtlink"]      >> g.mtlink;
        if (n["compute"]) n["compute"] >> g.compute;
    }
};

using MtGPUList = std::vector<MtGPU>;
