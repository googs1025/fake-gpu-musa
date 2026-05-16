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

// MtGPU — Moore Threads GPU descriptor, parallels GPU in common.h.
//   - mpc_count is the MUSA equivalent of MIG max_count (0 = MPC disabled).
//   - mtbios_version is MThreads-specific firmware string; vbios_version
//     keeps the conventional name used by NVIDIA tools.
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
    }
};

using MtGPUList = std::vector<MtGPU>;
