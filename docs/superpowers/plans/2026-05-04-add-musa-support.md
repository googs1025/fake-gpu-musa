# 摩尔线程 MUSA 支持实施计划

> **给 agent 执行者：** 必需子 skill：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 按任务逐步实施本计划。步骤使用 checkbox（`- [ ]`）语法跟踪进度。

**目标：** 给 fake-gpu 项目添加摩尔线程 MUSA 支持，作为 NVIDIA 之外的并行实现，使该项目能够通过相同的 NRI bind-mount 机制在 Kubernetes 集群中伪造 MTT GPU。

**架构（已与 `docs/mthreads-support-design.md` 对齐）：** 镜像现有的 `src/nvml/` + `src/cuda/` + `src/cudart/` C++ hook 结构，新增 `src/mtml/` + `src/musa/` + `src/musart/`。**不拆 so** — 这 6 套 API 编进同一个 `libfakegpu.so`（CMake 已用 `file(GLOB src/*/*.cpp)`，新增子目录会自动纳入），由 device-injector bind-mount 为 6 个目的文件名（`libcuda.so.1`/`libnvidia-ml.so.1`/`libcudart.so` + `libmusa.so`/`libmtml.so`/`libmusart.so`）。镜像 `pkg/nvidia/` → `pkg/mthreads/`，包含 MTML 的 Go cgo 绑定和一个 `mt-smi` 命令。扩展 `cmd/device-injector`，加入 `--vendor` 标志（`nvidia` | `musa` | `both`）选择要 bind-mount 的目的文件名集合和要识别的环境变量；当 `vendor=both` 时**单容器内必须互斥**（不允许同时存在 `NVIDIA_VISIBLE_DEVICES` 与 `MUSA_VISIBLE_DEVICES`，命中即拒绝注入并记录 warning）。MTML 头文件直接 vendor 自 `MooreThreads/mthreads-ml-py`（无需安装 SDK）；MUSA Driver/Runtime 层的符号清单从 MUSA SDK 3.1.0 的 `libmusa.so`/`libmusart.so` 用 `nm -D` 提取后筛选（保留枚举/属性/上下文/MemGetInfo 路径上的 ~20+~20 个核心符号，砍掉所有 compute 路径），代码无需依赖 SDK 头文件。**所有 C++ hook 代码必须对齐 `src/cuda/cuda_hook.cpp` 风格**：每个导出函数用 `HOOK_C_API HOOK_DECL_EXPORT`（来自 `macro_common.h`）声明，函数体首行插入 `HOOK_TRACE_PROFILE("functionName")`（来自 `trace_profile.h`），类型 typedef 抽到独立的 `*_subset.h`，禁止手写 `extern "C"` + `__attribute__((visibility))`。

**技术栈：**
- C++11、CMake 3.12+、yaml-cpp（已在构建中）
- Go 1.22、cgo（用于 MTML wrapper）
- containerd NRI（项目已有）
- `mtml_2.2.0.h` 来自 [MooreThreads/mthreads-ml-py](https://github.com/MooreThreads/mthreads-ml-py)
- MUSA Driver/Runtime 符号清单从 MUSA SDK 3.1.0 的 `libmusa.so`(305 公开 API)/`libmusart.so`(185 公开 API) 用 `nm -D` 提取后筛选保留 ~20+~20 个核心符号；ollama PR#7554 的 18 个最小集作为子集已被完整覆盖

**不在范围内：**
- 真实的 MUSA 计算支持（存根都返回错误，与现有 CUDA hook 行为一致）
- HAMi/device-plugin 集成（MUSA device plugin 不在本仓库内）
- MPC（多实例）虚拟化查询（仅返回"不支持"存根）

---

## 文件结构

**新增文件：**
- `src/common/mtml_2.2.0.h` — 从 mthreads-ml-py vendor 进来的 MTML 头文件
- `src/mtml/mtml_subset.h` — 给 hook 用的小子集再导出
- `src/mtml/mtml_hook.cpp` — 在 YAML 配置之上实现 MTML API
- `src/musa/musa_subset.h` — MUSA Driver 类型/错误码 subset（类比 `src/cuda/cuda_subset.h`）
- `src/musa/musa_hook.cpp` — 20 个 driver-API 符号存根（覆盖 init/version/error/device 枚举/属性/ctx/MemGetInfo）
- `src/musart/musart_subset.h` — MUSA Runtime 类型/错误码 subset（类比 `src/cudart/cudart_subset.h`）
- `src/musart/musart_hook.cpp` — 20 个 runtime-API 符号存根（覆盖 version/error/device 枚举/属性/同步/MemGetInfo + Malloc/Free）
- `pkg/mthreads/mtml/mtml.go` — libmtml.so 的 cgo 绑定
- `pkg/mthreads/common/gpu.go` — mt-smi 中表示一行 MUSA GPU 的 Go struct
- `pkg/mthreads/root.go` — `mt-smi` 的 cobra 命令
- `cmd/mt-smi/main.go` — 入口点
- `conf/fake-musa.yaml` — 示例 MUSA GPU 配置
- `docs/musa.md` — MUSA 后端使用文档

**修改文件：**
- `CMakeLists.txt` — 把 `src/mtml`、`src/musa`、`src/musart` 加入 include_directories；`fakegpu` 目标无需新增（已通过 `file(GLOB src/*/*.cpp)` 自动包含新子目录）
- `Makefile` — 增加 `mt-smi` 构建目标和镜像版本打戳
- `Dockerfile` — 把 `mt-smi` 二进制和 `fake-musa.yaml` 配置拷贝进镜像（**不**新增任何 .so，由现有 libfakegpu.so 承担 MUSA 符号导出）
- `entrypoint.sh` — 透传 `--vendor` 标志，并从两个配置中统计 GPU 数量
- `cmd/device-injector/main.go` — 增加 `--vendor` 标志、MUSA 环境变量检测、MUSA 目的文件名挂载列表、`vendor=both` 时的单容器互斥检查
- `charts/fake-gpu/values.yaml` — 增加 `vendor` 字段
- `charts/fake-gpu/templates/configmap.yaml` — 包含 `fake-musa.yaml`
- `charts/fake-gpu/templates/daemonset.yaml` — 把 vendor 透传给容器，挂载 musa configmap
- `README.md` — 链接到 `docs/musa.md`

---

## 任务 1：vendor MTML 头文件并创建骨架目录

**文件：**
- 创建：`src/common/mtml_2.2.0.h`
- 创建：`src/mtml/mtml_subset.h`
- 创建：`src/mtml/.gitkeep`、`src/musa/.gitkeep`、`src/musart/.gitkeep`

- [ ] **步骤 1：拉取上游 MTML 头文件**

```bash
mkdir -p src/mtml src/musa src/musart
curl -fsSL \
  https://raw.githubusercontent.com/MooreThreads/mthreads-ml-py/main/mtml_2.2.0.h \
  -o src/common/mtml_2.2.0.h
```

验证：文件大约 80KB，开头是 `* Copyright ©2020-2022 Moore Threads`。

- [ ] **步骤 2：创建 `src/mtml/mtml_subset.h`，仅再导出 hook 实际接触到的部分**

```cpp
#pragma once
// Subset re-export of vendored MTML header. Hooks should include this file
// instead of the full vendored header so we have a stable surface to grep for
// when MTML upstream evolves.
#include "mtml_2.2.0.h"
```

- [ ] **步骤 3：用项目编译器验证头文件能解析**

```bash
g++ -std=c++11 -fsyntax-only -x c++ src/mtml/mtml_subset.h
```

预期：exit 0，无输出。

- [ ] **步骤 4：提交**

```bash
git add src/common/mtml_2.2.0.h src/mtml/mtml_subset.h \
        src/mtml/.gitkeep src/musa/.gitkeep src/musart/.gitkeep
git commit -m "feat(musa): vendor MTML 2.2.0 header from mthreads-ml-py"
```

---

## 任务 2：实现 libmusa.so 存根（MUSA Driver API）

20 个符号从 MUSA SDK 3.1.0 `libmusa.so` 公开 API（305 个 `mu*` 符号）中筛选——覆盖 init、版本/错误字符串、设备枚举、设备属性（compute capability/total mem/PCI bus id）、显式和 primary context、MemGetInfo。砍掉计算路径（kernel launch、memcpy、stream、event、graph，~285 个），因为 fake-gpu 场景下没人会真跑算子。**代码风格对齐 `src/cuda/cuda_hook.cpp`**（`HOOK_C_API HOOK_DECL_EXPORT` + `HOOK_TRACE_PROFILE`，类型 typedef 抽到独立 subset 头）。每个存根返回"无设备"让调用方优雅降级，而不是 segfault。

> **符号筛选原则**：保留枚举（apps 必查"我有几张卡"）+ 属性（apps 必查卡的能力）+ ctx（PyTorch 等 framework 会先建 ctx）+ 内存信息（监控类必读）。剔除 compute（fake-gpu 永远不会被真正调用，被调到等于测试场景偏离）+ 异步原语（同 compute）。

**文件：**
- 创建：`src/musa/musa_subset.h`
- 创建：`src/musa/musa_hook.cpp`

- [ ] **步骤 1：写 `src/musa/musa_subset.h`（类型与错误码 subset，类比 `src/cuda/cuda_subset.h`）**

只声明 20 个函数会用到的类型/错误码：`MUresult`、`MUdevice`、`MUcontext`、`MUuuid`，以及 `MUSA_SUCCESS`/`MUSA_ERROR_*` 几个错误码（数值对齐 CUDA Driver API）。具体内容见仓库现有的 `src/musa/musa_subset.h`。

- [ ] **步骤 2：写 `src/musa/musa_hook.cpp`（对齐 cuda_hook.cpp 风格）**

20 个符号分 5 组：

| 组 | 符号 |
|----|------|
| init/version/error | `muInit`、`muDriverGetVersion`、`muGetErrorName`、`muGetErrorString` |
| 设备枚举 | `muDeviceGetCount`、`muDeviceGet`、`muDeviceGetName`、`muDeviceGetUuid_v2`、`muDeviceGetAttribute`、`muDeviceComputeCapability`、`muDeviceTotalMem_v2`、`muDeviceGetPCIBusId`、`muDeviceGetByPCIBusId` |
| primary ctx | `muDevicePrimaryCtxRetain`、`muDevicePrimaryCtxRelease_v2` |
| 显式 ctx | `muCtxCreate_v2`、`muCtxDestroy_v2`、`muCtxGetCurrent`、`muCtxSetCurrent` |
| 内存查询 | `muMemGetInfo_v2` |

每个函数体只有 2-3 行：`HOOK_TRACE_PROFILE("functionName")` + 输出参数置零 + `return MUSA_ERROR_NO_DEVICE`（少数 init/version/destroy 返回 `MUSA_SUCCESS`）。完整代码见仓库现有的 `src/musa/musa_hook.cpp`。

签名样板（一个组示意）：
```cpp
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
```

- [ ] **步骤 3：单独编译成 .so 做 sanity check**

```bash
g++ -std=c++11 -shared -fPIC -fvisibility=hidden \
    -Isrc/common -Isrc/musa \
    src/musa/musa_hook.cpp -o /tmp/libmusa-check.so
```

预期：exit 0。`-Isrc/common` 让 `macro_common.h` / `trace_profile.h` 可见，`-Isrc/musa` 让 `musa_subset.h` 可见。macOS 上会看到 `syscall` 的 deprecated warning（来自 `macro_common.h`，与 cuda_hook.cpp 相同情况），可忽略。

- [ ] **步骤 4：验证 20 个符号都已导出**

Linux 构建环境（CMake 实际跑的地方）：
```bash
nm -D --defined-only /tmp/libmusa-check.so | awk '{print $3}' | sort > /tmp/got.txt
```

macOS 本地：
```bash
nm -gU /tmp/libmusa-check.so | awk '{print $NF}' | sed 's/^_//' | sort > /tmp/got.txt
```

然后比对：
```bash
cat <<'EOF' | sort > /tmp/want.txt
muCtxCreate_v2
muCtxDestroy_v2
muCtxGetCurrent
muCtxSetCurrent
muDeviceComputeCapability
muDeviceGet
muDeviceGetAttribute
muDeviceGetByPCIBusId
muDeviceGetCount
muDeviceGetName
muDeviceGetPCIBusId
muDeviceGetUuid_v2
muDevicePrimaryCtxRelease_v2
muDevicePrimaryCtxRetain
muDeviceTotalMem_v2
muDriverGetVersion
muGetErrorName
muGetErrorString
muInit
muMemGetInfo_v2
EOF
diff /tmp/want.txt /tmp/got.txt
```

预期：无 diff 输出。

- [ ] **步骤 5：提交**

```bash
git add src/musa/musa_subset.h src/musa/musa_hook.cpp
git commit -m "feat(musa): add libmusa.so stub with 20 driver-API symbols"
```

---

## 任务 3：实现 libmusart.so 存根（MUSA Runtime API）

20 个符号从 MUSA SDK 3.1.0 `libmusart.so` 公开 API（185 个 `musa*` 符号）中筛选——覆盖 version、error 字符串、`musaGetLastError`/`PeekAtLastError`、设备枚举、设备属性、`musaGetDeviceProperties`、PCI bus id 互查、`DeviceSynchronize`/`Reset`、`MemGetInfo`、最小的 `musaMalloc`/`musaFree`（少数应用启动期会试探性分配 1 字节验证 runtime 可用）。**风格对齐 `src/cudart/cudart_hook.cpp`**（项目宏 + trace_profile + 独立 subset 头）。

**文件：**
- 创建：`src/musart/musart_subset.h`
- 创建：`src/musart/musart_hook.cpp`

- [ ] **步骤 1：写 `src/musart/musart_subset.h`（类比 `src/cudart/cudart_subset.h`）**

声明 `musaError_t` 枚举（值对齐 `cudaError_t`，`musaSuccess=0`、`musaErrorNoDevice=38`、`musaErrorInvalidValue=1` 等），以及前向声明 `struct musaDeviceProp`（不写字段——存根永远不向其写入，调用方按真实 SDK 头定义的尺寸分配缓冲区即可）。具体内容见仓库现有的 `src/musart/musart_subset.h`。

- [ ] **步骤 2：写 `src/musart/musart_hook.cpp`（对齐 cudart_hook.cpp 风格）**

20 个符号分 4 组：

| 组 | 符号 |
|----|------|
| version/error | `musaRuntimeGetVersion`、`musaDriverGetVersion`、`musaGetErrorName`、`musaGetErrorString`、`musaGetLastError`、`musaPeekAtLastError` |
| 设备枚举/属性 | `musaGetDeviceCount`、`musaGetDevice`、`musaSetDevice`、`musaGetDeviceFlags`、`musaSetDeviceFlags`、`musaGetDeviceProperties`、`musaDeviceGetAttribute`、`musaDeviceGetPCIBusId`、`musaDeviceGetByPCIBusId` |
| 同步/重置 | `musaDeviceSynchronize`、`musaDeviceReset` |
| 内存 | `musaMemGetInfo`、`musaMalloc`、`musaFree` |

每个函数体只有 2-3 行：`HOOK_TRACE_PROFILE("functionName")` + 输出参数置零 + 返回错误码（`Get*` 返回 `musaErrorNoDevice`，`Malloc` 返回 `musaErrorMemoryAllocation`，`Reset`/`Free` 返回 `musaSuccess`，`GetErrorName/String` 返回固定字符串）。完整代码见仓库现有的 `src/musart/musart_hook.cpp`。

签名样板（一个组示意）：
```cpp
HOOK_C_API HOOK_DECL_EXPORT musaError_t musaGetDeviceCount(int *count) {
    HOOK_TRACE_PROFILE("musaGetDeviceCount");
    if (!count) return musaErrorInvalidValue;
    *count = 0;
    return musaSuccess;
}
HOOK_C_API HOOK_DECL_EXPORT const char *musaGetErrorString(musaError_t error) {
    HOOK_TRACE_PROFILE("musaGetErrorString");
    return "fake-gpu musart stub: no device";
}
HOOK_C_API HOOK_DECL_EXPORT musaError_t musaMalloc(void **devPtr, size_t size) {
    HOOK_TRACE_PROFILE("musaMalloc");
    if (devPtr) *devPtr = nullptr;
    return musaErrorMemoryAllocation;
}
```

- [ ] **步骤 3：编译并验证符号导出**

```bash
g++ -std=c++11 -shared -fPIC -fvisibility=hidden \
    -Isrc/common -Isrc/musart \
    src/musart/musart_hook.cpp -o /tmp/libmusart-check.so
```

Linux 上：
```bash
nm -D --defined-only /tmp/libmusart-check.so | awk '{print $3}' | sort > /tmp/got.txt
```
macOS 上：
```bash
nm -gU /tmp/libmusart-check.so | awk '{print $NF}' | sed 's/^_//' | sort > /tmp/got.txt
```
比对：
```bash
cat <<'EOF' | sort > /tmp/want.txt
musaDeviceGetAttribute
musaDeviceGetByPCIBusId
musaDeviceGetPCIBusId
musaDeviceReset
musaDeviceSynchronize
musaDriverGetVersion
musaFree
musaGetDevice
musaGetDeviceCount
musaGetDeviceFlags
musaGetDeviceProperties
musaGetErrorName
musaGetErrorString
musaGetLastError
musaMalloc
musaMemGetInfo
musaPeekAtLastError
musaRuntimeGetVersion
musaSetDevice
musaSetDeviceFlags
EOF
diff /tmp/want.txt /tmp/got.txt
```

预期：无 diff 输出。

- [ ] **步骤 4：提交**

```bash
git add src/musart/musart_subset.h src/musart/musart_hook.cpp
git commit -m "feat(musa): add libmusart.so stub with 20 runtime-API symbols"
```

---

## 任务 4：MTML 公共类型与 YAML 加载器

镜像 `src/common/common.h` 中 `GPU` / `GPUList` 的模式，但用于 MUSA。复用现有的 `RAM` / `PCI` / `GPU_Util` / `GPU_Power` struct（这些是供应商无关的），并增加一个 MUSA 特有的顶层 struct。YAML 加载器从 `moorethreads:` 根 key 读取。

**文件：**
- 创建：`src/common/musa_common.h`
- 创建：`conf/fake-musa.yaml`

- [ ] **步骤 1：写 `src/common/musa_common.h`**

```cpp
#pragma once
#include <yaml-cpp/yaml.h>
#include <string>
#include <vector>

#include "common.h"   // reuse RAM, PCI, GPU_Util, GPU_Power, NUMA

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
            for (const auto &n : node["peer_gpu_uuids"])
                l.peer_gpus.push_back(n.as<std::string>());
        }
    }
};

struct MtGPU {
    std::string  name;
    std::string  uuid;
    std::string  driver_version;
    std::string  brand;
    std::string  vbios_version;
    std::string  mtbios_version;
    std::string  serial;
    int          mpc_count;          // analogue of MIG max_count, 0 = disabled
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
```

- [ ] **步骤 2：写 `conf/fake-musa.yaml`，包含一台 MTT S80 示例**

```yaml
moorethreads:
  - name: MTT S80
    uuid: MTGPU-0
    driver_version: 2.7.0
    brand: MTT
    vbios_version: 1.0.0.0
    mtbios_version: 0.4.0.4
    serial: MTT12345678
    mpc_count: 0
    power:
      minLimit: 50000
      maxLimit: 250000
      defaultLimit: 250000
      enforcedLimit: 250000
      usage: 60000
    utilization:
      gpu: 0
      memory: 0
    memory:
      total: 17179869184    # 16 GiB
      free:  17179869184
    pci:
      bus_id: 0000:00:1F.0
      bus: 1
      device_id: 0
      domain_id: 0
      sub_system_id: 0
    numa:
      node: 0
      cpu_affinity: 0-7
    mtlink:
      version: "1.0"
      capacity: 1
      bandwidth: 16
      peer_gpu_uuids: []
```

- [ ] **步骤 3：冒烟测试 YAML 能解析**

```bash
g++ -std=c++11 -fsyntax-only -Isrc/common src/common/musa_common.h
```

预期：exit 0。

- [ ] **步骤 4：提交**

```bash
git add src/common/musa_common.h conf/fake-musa.yaml
git commit -m "feat(musa): add MtGPU struct and sample fake-musa.yaml"
```

---

## 任务 5：MTML library + system + error 函数

本任务实现入口点系列：`mtmlLibraryInit`、`mtmlLibraryShutDown`、`mtmlLibraryCountDevice`、`mtmlLibraryInitDeviceByIndex`/`Uuid`、`mtmlLibraryFreeDevice`、`mtmlLibraryInitSystem`、`mtmlLibraryFreeSystem`、`mtmlLibraryGetVersion`、`mtmlSystemGetDriverVersion`、`mtmlErrorString`。

**文件：**
- 创建：`src/mtml/mtml_hook.cpp`

- [ ] **步骤 1：写文件骨架与配置加载器（对齐 nvml_hook.cpp 风格）**

```cpp
// MTML hook implementation. Backs MTML calls with values from a YAML config
// file pointed to by FAKE_MUSA_CONFIG. Mirrors src/nvml/nvml_hook.cpp.

#include "mtml_subset.h"
#include "musa_common.h"
#include "macro_common.h"
#include "trace_profile.h"

#include <yaml-cpp/yaml.h>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <mutex>

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
    if (const char *vis = std::getenv("MUSA_VISIBLE_DEVICES");
        vis && std::strcmp(vis, "all") != 0) {
        MtGPUList filtered;
        std::string s(vis);
        size_t prev = 0, pos;
        while ((pos = s.find(',', prev)) != std::string::npos) {
            std::string token = s.substr(prev, pos - prev);
            for (auto &g : g_gpus)
                if (g.uuid == token) filtered.push_back(g);
            prev = pos + 1;
        }
        std::string token = s.substr(prev);
        for (auto &g : g_gpus)
            if (g.uuid == token) filtered.push_back(g);
        g_gpus.swap(filtered);
    }
    g_inited = true;
}

const MtGPU *device_to_gpu(MtmlDevice *d) {
    // We cast the device opaque-pointer to (intptr_t)index+1 so that 0 is
    // never a valid handle.
    intptr_t idx = reinterpret_cast<intptr_t>(d) - 1;
    if (idx < 0 || idx >= static_cast<intptr_t>(g_gpus.size())) return nullptr;
    return &g_gpus[idx];
}
}  // namespace

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInit(MtmlLibrary **lib) {
    HOOK_TRACE_PROFILE("mtmlLibraryInit");
    std::lock_guard<std::mutex> lk(g_mu);
    load_config_locked();
    if (lib) *lib = reinterpret_cast<MtmlLibrary *>(0x1);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryShutDown(void) {
    HOOK_TRACE_PROFILE("mtmlLibraryShutDown");
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryGetVersion(MtmlLibrary *lib, char *version, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlLibraryGetVersion");
    if (!version || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(version, "2.2.0", len);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT const char *mtmlErrorString(MtmlReturn r) {
    HOOK_TRACE_PROFILE("mtmlErrorString");
    switch (r) {
        case MTML_SUCCESS: return "Success";
        case MTML_ERROR_INVALID_ARGUMENT: return "Invalid argument";
        case MTML_ERROR_NOT_SUPPORTED: return "Not supported";
        default: return "Unknown error";
    }
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryCountDevice(MtmlLibrary *lib, unsigned int *count) {
    HOOK_TRACE_PROFILE("mtmlLibraryCountDevice");
    if (!count) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    *count = static_cast<unsigned int>(g_gpus.size());
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitDeviceByIndex(MtmlLibrary *lib, unsigned int index,
                                                                    MtmlDevice **dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitDeviceByIndex");
    if (!dev) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    if (index >= g_gpus.size()) return MTML_ERROR_INVALID_ARGUMENT;
    *dev = reinterpret_cast<MtmlDevice *>(static_cast<intptr_t>(index) + 1);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitDeviceByUuid(MtmlLibrary *lib, const char *uuid,
                                                                   MtmlDevice **dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitDeviceByUuid");
    if (!dev || !uuid) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    for (size_t i = 0; i < g_gpus.size(); ++i) {
        if (g_gpus[i].uuid == uuid) {
            *dev = reinterpret_cast<MtmlDevice *>(static_cast<intptr_t>(i) + 1);
            return MTML_SUCCESS;
        }
    }
    return MTML_ERROR_NOT_FOUND;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryFreeDevice(MtmlDevice *dev) {
    HOOK_TRACE_PROFILE("mtmlLibraryFreeDevice");
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryInitSystem(MtmlLibrary *lib, MtmlSystem **sys) {
    HOOK_TRACE_PROFILE("mtmlLibraryInitSystem");
    if (sys) *sys = reinterpret_cast<MtmlSystem *>(0x2);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlLibraryFreeSystem(MtmlSystem *sys) {
    HOOK_TRACE_PROFILE("mtmlLibraryFreeSystem");
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlSystemGetDriverVersion(MtmlSystem *sys, char *version, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlSystemGetDriverVersion");
    if (!version || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::lock_guard<std::mutex> lk(g_mu);
    const std::string &v = g_gpus.empty() ? std::string("2.7.0")
                                          : g_gpus.front().driver_version;
    std::strncpy(version, v.c_str(), len);
    return MTML_SUCCESS;
}
```

**注意**：与原版本相比，所有 `extern "C" { ... }` 包裹块改成**每函数单行** `HOOK_C_API HOOK_DECL_EXPORT`；每个函数体首行加 `HOOK_TRACE_PROFILE("functionName")`；空 `MtmlLibrary *` / `MtmlSystem *` 形参补回参数名（保持 cuda_hook.cpp 风格）。

- [ ] **步骤 2：单独编译检查文件**

```bash
g++ -std=c++11 -fsyntax-only -Isrc/common -Isrc/mtml \
    src/mtml/mtml_hook.cpp
```

预期：exit 0（yaml-cpp 还没链接，但解析应当成功）。

- [ ] **步骤 3：提交**

```bash
git add src/mtml/mtml_hook.cpp
git commit -m "feat(musa): MTML library/system entry-point hooks"
```

---

## 任务 6：MTML 设备查询函数（Name、UUID、PCI、Brand、Power、Path）

追加到 `src/mtml/mtml_hook.cpp`。这些镜像 `pymtml.py` 中的 `mtmlDevice*` 系列。

**文件：**
- 修改：`src/mtml/mtml_hook.cpp`（追加到文件末尾，注意每个函数都用 `HOOK_C_API HOOK_DECL_EXPORT` 和 `HOOK_TRACE_PROFILE`，与任务 5 保持一致风格）

- [ ] **步骤 1：把设备查询函数追加到 `mtml_hook.cpp`**

```cpp
HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetIndex(MtmlDevice *d, unsigned int *index) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetIndex");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !index) return MTML_ERROR_INVALID_ARGUMENT;
    *index = static_cast<unsigned int>(g - &g_gpus[0]);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetName(MtmlDevice *d, char *name, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetName");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !name || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(name, g->name.c_str(), len);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetUUID(MtmlDevice *d, char *uuid, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetUUID");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !uuid || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(uuid, g->uuid.c_str(), len);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetBrand(MtmlDevice *d, MtmlBrandType *brand) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetBrand");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !brand) return MTML_ERROR_INVALID_ARGUMENT;
    // The header defines brand as an enum; fake-gpu always reports MTT.
    *brand = static_cast<MtmlBrandType>(1);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetSerialNumber(MtmlDevice *d, char *sn, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetSerialNumber");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !sn || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(sn, g->serial.c_str(), len);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetVbiosVersion(MtmlDevice *d, char *v, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetVbiosVersion");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !v || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(v, g->vbios_version.c_str(), len);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetMtBiosVersion(MtmlDevice *d, char *v, unsigned int len) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetMtBiosVersion");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !v || len == 0) return MTML_ERROR_INVALID_ARGUMENT;
    std::strncpy(v, g->mtbios_version.c_str(), len);
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetPciInfo(MtmlDevice *d, MtmlPciInfo *info) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetPciInfo");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !info) return MTML_ERROR_INVALID_ARGUMENT;
    std::memset(info, 0, sizeof(*info));
    std::strncpy(info->sbdf, g->pci.bus_id.c_str(), sizeof(info->sbdf) - 1);
    info->busId      = g->pci.bus_id[0];     // best-effort packing
    info->deviceId   = g->pci.device_id;
    info->domain     = g->pci.domain_id;
    info->subSystemId = g->pci.sub_system_id;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceGetPowerUsage(MtmlDevice *d, unsigned int *milliwatts) {
    HOOK_TRACE_PROFILE("mtmlDeviceGetPowerUsage");
    const MtGPU *g = device_to_gpu(d);
    if (!g || !milliwatts) return MTML_ERROR_INVALID_ARGUMENT;
    *milliwatts = g->power.usage;
    return MTML_SUCCESS;
}
```

- [ ] **步骤 2：验证仍能编译**

```bash
g++ -std=c++11 -fsyntax-only -Isrc/common -Isrc/mtml \
    src/mtml/mtml_hook.cpp
```

预期：exit 0。

- [ ] **步骤 3：提交**

```bash
git add src/mtml/mtml_hook.cpp
git commit -m "feat(musa): MTML device-query hooks (name, uuid, pci, power)"
```

---

## 任务 7：MTML 子对象（Memory、GPU、VPU）

MTML 遵循一种层次结构：必须先把 device 转成 `MtmlMemory` / `MtmlGpu` / `MtmlVpu` 这些不透明对象，才能读取其指标。

我们把子对象 handle 编码为复用 device handle 的 index，但用一个高位 tag 标记，让分派辅助函数能够区分。

**文件：**
- 修改：`src/mtml/mtml_hook.cpp`

- [ ] **步骤 1：增加辅助函数和子对象的 init/free（对齐任务 5/6 的项目宏风格）**

在 `mtml_hook.cpp` 末尾追加：

```cpp
namespace {
constexpr intptr_t kMemoryTag = 1LL << 32;
constexpr intptr_t kGpuTag    = 2LL << 32;
constexpr intptr_t kVpuTag    = 3LL << 32;

const MtGPU *handle_to_gpu(void *h, intptr_t tag) {
    intptr_t v = reinterpret_cast<intptr_t>(h);
    if ((v & ~0xFFFFFFFFLL) != tag) return nullptr;
    intptr_t idx = (v & 0xFFFFFFFFLL) - 1;
    if (idx < 0 || idx >= static_cast<intptr_t>(g_gpus.size())) return nullptr;
    return &g_gpus[idx];
}

void *make_handle(MtmlDevice *d, intptr_t tag) {
    intptr_t idx = reinterpret_cast<intptr_t>(d);
    return reinterpret_cast<void *>(tag | (idx & 0xFFFFFFFFLL));
}
}  // namespace

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceInitMemory(MtmlDevice *d, MtmlMemory **mem) {
    HOOK_TRACE_PROFILE("mtmlDeviceInitMemory");
    if (!device_to_gpu(d) || !mem) return MTML_ERROR_INVALID_ARGUMENT;
    *mem = static_cast<MtmlMemory *>(make_handle(d, kMemoryTag));
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceFreeMemory(MtmlMemory *mem) {
    HOOK_TRACE_PROFILE("mtmlDeviceFreeMemory");
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceInitGpu(MtmlDevice *d, MtmlGpu **gpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceInitGpu");
    if (!device_to_gpu(d) || !gpu) return MTML_ERROR_INVALID_ARGUMENT;
    *gpu = static_cast<MtmlGpu *>(make_handle(d, kGpuTag));
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceFreeGpu(MtmlGpu *gpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceFreeGpu");
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceInitVpu(MtmlDevice *d, MtmlVpu **vpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceInitVpu");
    if (!device_to_gpu(d) || !vpu) return MTML_ERROR_INVALID_ARGUMENT;
    *vpu = static_cast<MtmlVpu *>(make_handle(d, kVpuTag));
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlDeviceFreeVpu(MtmlVpu *vpu) {
    HOOK_TRACE_PROFILE("mtmlDeviceFreeVpu");
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlMemoryGetTotal(MtmlMemory *m, unsigned long long *bytes) {
    HOOK_TRACE_PROFILE("mtmlMemoryGetTotal");
    const MtGPU *g = handle_to_gpu(m, kMemoryTag);
    if (!g || !bytes) return MTML_ERROR_INVALID_ARGUMENT;
    *bytes = g->memory.total;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlMemoryGetUsed(MtmlMemory *m, unsigned long long *bytes) {
    HOOK_TRACE_PROFILE("mtmlMemoryGetUsed");
    const MtGPU *g = handle_to_gpu(m, kMemoryTag);
    if (!g || !bytes) return MTML_ERROR_INVALID_ARGUMENT;
    *bytes = g->memory.total - g->memory.free;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlMemoryGetUtilization(MtmlMemory *m, unsigned int *pct) {
    HOOK_TRACE_PROFILE("mtmlMemoryGetUtilization");
    const MtGPU *g = handle_to_gpu(m, kMemoryTag);
    if (!g || !pct) return MTML_ERROR_INVALID_ARGUMENT;
    *pct = g->utilization.memory;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlGpuGetUtilization(MtmlGpu *gpuh, unsigned int *pct) {
    HOOK_TRACE_PROFILE("mtmlGpuGetUtilization");
    const MtGPU *g = handle_to_gpu(gpuh, kGpuTag);
    if (!g || !pct) return MTML_ERROR_INVALID_ARGUMENT;
    *pct = g->utilization.gpu;
    return MTML_SUCCESS;
}

HOOK_C_API HOOK_DECL_EXPORT MtmlReturn mtmlGpuGetTemperature(MtmlGpu *gpuh, unsigned int *celsius) {
    HOOK_TRACE_PROFILE("mtmlGpuGetTemperature");
    const MtGPU *g = handle_to_gpu(gpuh, kGpuTag);
    if (!g || !celsius) return MTML_ERROR_INVALID_ARGUMENT;
    *celsius = 45;   // fake constant temperature
    return MTML_SUCCESS;
}
```

- [ ] **步骤 2：验证编译**

```bash
g++ -std=c++11 -fsyntax-only -Isrc/common -Isrc/mtml \
    src/mtml/mtml_hook.cpp
```

预期：exit 0。

- [ ] **步骤 3：提交**

```bash
git add src/mtml/mtml_hook.cpp
git commit -m "feat(musa): MTML memory/gpu/vpu sub-object hooks"
```

---

## 任务 8：CMake 集成 — 把 MTML/MUSA/MUSART 源文件并入 libfakegpu.so

**对齐 `docs/mthreads-support-design.md`：不拆 so**。现有的 `add_library(fakegpu SHARED ${HOOK_SRCS})` 已通过 `file(GLOB HOOK_SRCS ${PROJECT_SOURCE_DIR}/src/*/*.cpp)` 自动收集 `src/mtml/*.cpp`、`src/musa/*.cpp`、`src/musart/*.cpp`。本任务只需要把新子目录加入 include 路径，确保符号能编进同一个 `libfakegpu.so` 中。MUSA 在容器内的可见性由 device-injector 把这同一个 `libfakegpu.so` bind-mount 成 `libmusa.so` / `libmtml.so` / `libmusart.so` 三个目的文件名实现（见任务 11）。

**文件：**
- 修改：`CMakeLists.txt`（在已有 `include_directories` 后追加）

- [ ] **步骤 1：把新子目录加入 include 路径**

```cmake
include_directories(
    ${PROJECT_SOURCE_DIR}/src/mtml
    ${PROJECT_SOURCE_DIR}/src/musa
    ${PROJECT_SOURCE_DIR}/src/musart
)
```

**不要**新增 `add_library(fakemusa ...)` / `add_library(musa ...)` / `add_library(musart ...)` 目标——它们违背"不拆 so"决策，并会让消费者必须解析三套不同的 RPATH。`fakegpu` 已通过现有的 GLOB 把这三个目录的 cpp 一起编进。

- [ ] **步骤 2：用现有的 Makefile 目标构建**

```bash
make build BUILD_TYPE=Release
```

预期：`output/lib64/` 中仅含 `libfakegpu.so`（**不**会再产出 `libfakemusa.so`、`libmusa.so`、`libmusart.so`）。

- [ ] **步骤 3：验证 6 套 API 的符号都已在同一个 SO 中导出**

```bash
nm -D --defined-only output/lib64/libfakegpu.so | grep -c '^.* T nvml'   # expect ≥40 (NVML, 已有)
nm -D --defined-only output/lib64/libfakegpu.so | grep -c '^.* T cu'     # expect ≥30 (CUDA Driver, 已有)
nm -D --defined-only output/lib64/libfakegpu.so | grep -c '^.* T cuda'   # expect ≥20 (CUDA Runtime, 已有)
nm -D --defined-only output/lib64/libfakegpu.so | grep -c '^.* T mtml'   # expect ≥20 (新增 MTML)
nm -D --defined-only output/lib64/libfakegpu.so | grep -c '^.* T mu'     # expect ≥10 (新增 MUSA Driver, mu* 前缀)
nm -D --defined-only output/lib64/libfakegpu.so | grep -c '^.* T musa'   # expect ≥8  (新增 MUSA Runtime, musa* 前缀)
```

**符号冲突检查**：`cu*` 前缀已被 CUDA Driver 占用，新增的 `mu*` 前缀与之不重叠；`musa*` 与 CUDA Runtime 的 `cuda*` 不重叠。无需做命名空间隔离。

- [ ] **步骤 4：提交**

```bash
git add CMakeLists.txt
git commit -m "build(musa): include MTML/MUSA/MUSART subdirs into libfakegpu.so"
```

---

## 任务 9：MTML 的 Go cgo 绑定

镜像 `pkg/nvidia` → `vendor/github.com/NVIDIA/go-nvml` 的关系。我们写自己的最小子集；不 vendor 任何外部 Go MTML 包，因为 MooreThreads 官方组织里并没有。

**文件：**
- 创建：`pkg/mthreads/mtml/mtml.go`

- [ ] **步骤 1：写最小的 cgo wrapper**

```go
// Package mtml is a thin cgo wrapper around libmtml.so. It only covers
// what mt-smi needs; expand on demand.
package mtml

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

typedef int MtmlReturn;
typedef void MtmlLibrary;
typedef void MtmlSystem;
typedef void MtmlDevice;
typedef void MtmlMemory;
typedef void MtmlGpu;

static void *h = NULL;

#define LOAD(sym) sym##_fn = dlsym(h, #sym)

typedef MtmlReturn (*lib_init_fn)(MtmlLibrary**);
typedef MtmlReturn (*lib_shutdown_fn)(void);
typedef MtmlReturn (*lib_count_fn)(MtmlLibrary*, unsigned int*);
typedef MtmlReturn (*lib_init_dev_fn)(MtmlLibrary*, unsigned int, MtmlDevice**);
typedef MtmlReturn (*dev_name_fn)(MtmlDevice*, char*, unsigned int);
typedef MtmlReturn (*dev_uuid_fn)(MtmlDevice*, char*, unsigned int);
typedef MtmlReturn (*dev_init_mem_fn)(MtmlDevice*, MtmlMemory**);
typedef MtmlReturn (*mem_total_fn)(MtmlMemory*, unsigned long long*);
typedef MtmlReturn (*mem_used_fn)(MtmlMemory*, unsigned long long*);
typedef MtmlReturn (*dev_init_gpu_fn)(MtmlDevice*, MtmlGpu**);
typedef MtmlReturn (*gpu_util_fn)(MtmlGpu*, unsigned int*);
typedef MtmlReturn (*sys_drv_fn)(MtmlSystem*, char*, unsigned int);
typedef MtmlReturn (*lib_init_sys_fn)(MtmlLibrary*, MtmlSystem**);

static lib_init_fn      mtmlLibraryInit_fn;
static lib_shutdown_fn  mtmlLibraryShutDown_fn;
static lib_count_fn     mtmlLibraryCountDevice_fn;
static lib_init_dev_fn  mtmlLibraryInitDeviceByIndex_fn;
static dev_name_fn      mtmlDeviceGetName_fn;
static dev_uuid_fn      mtmlDeviceGetUUID_fn;
static dev_init_mem_fn  mtmlDeviceInitMemory_fn;
static mem_total_fn     mtmlMemoryGetTotal_fn;
static mem_used_fn      mtmlMemoryGetUsed_fn;
static dev_init_gpu_fn  mtmlDeviceInitGpu_fn;
static gpu_util_fn      mtmlGpuGetUtilization_fn;
static sys_drv_fn       mtmlSystemGetDriverVersion_fn;
static lib_init_sys_fn  mtmlLibraryInitSystem_fn;

static int load_lib(const char *path) {
    h = dlopen(path, RTLD_NOW | RTLD_GLOBAL);
    if (!h) return -1;
    LOAD(mtmlLibraryInit);
    LOAD(mtmlLibraryShutDown);
    LOAD(mtmlLibraryCountDevice);
    LOAD(mtmlLibraryInitDeviceByIndex);
    LOAD(mtmlDeviceGetName);
    LOAD(mtmlDeviceGetUUID);
    LOAD(mtmlDeviceInitMemory);
    LOAD(mtmlMemoryGetTotal);
    LOAD(mtmlMemoryGetUsed);
    LOAD(mtmlDeviceInitGpu);
    LOAD(mtmlGpuGetUtilization);
    LOAD(mtmlSystemGetDriverVersion);
    LOAD(mtmlLibraryInitSystem);
    return 0;
}

// Trampolines so Go can call function pointers indirectly.
static MtmlReturn lib_init(MtmlLibrary **l)      { return mtmlLibraryInit_fn(l); }
static MtmlReturn lib_shutdown(void)             { return mtmlLibraryShutDown_fn(); }
static MtmlReturn lib_count(MtmlLibrary *l, unsigned int *c) { return mtmlLibraryCountDevice_fn(l, c); }
static MtmlReturn lib_init_dev(MtmlLibrary *l, unsigned int i, MtmlDevice **d) { return mtmlLibraryInitDeviceByIndex_fn(l, i, d); }
static MtmlReturn dev_name(MtmlDevice *d, char *b, unsigned int n) { return mtmlDeviceGetName_fn(d, b, n); }
static MtmlReturn dev_uuid(MtmlDevice *d, char *b, unsigned int n) { return mtmlDeviceGetUUID_fn(d, b, n); }
static MtmlReturn dev_init_mem(MtmlDevice *d, MtmlMemory **m) { return mtmlDeviceInitMemory_fn(d, m); }
static MtmlReturn mem_total(MtmlMemory *m, unsigned long long *v) { return mtmlMemoryGetTotal_fn(m, v); }
static MtmlReturn mem_used(MtmlMemory *m, unsigned long long *v) { return mtmlMemoryGetUsed_fn(m, v); }
static MtmlReturn dev_init_gpu(MtmlDevice *d, MtmlGpu **g) { return mtmlDeviceInitGpu_fn(d, g); }
static MtmlReturn gpu_util(MtmlGpu *g, unsigned int *v) { return mtmlGpuGetUtilization_fn(g, v); }
static MtmlReturn sys_drv(MtmlSystem *s, char *b, unsigned int n) { return mtmlSystemGetDriverVersion_fn(s, b, n); }
static MtmlReturn lib_init_sys(MtmlLibrary *l, MtmlSystem **s) { return mtmlLibraryInitSystem_fn(l, s); }
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const SUCCESS = 0

type Library struct{ ptr unsafe.Pointer }
type System  struct{ ptr unsafe.Pointer }
type Device  struct{ ptr unsafe.Pointer }
type Memory  struct{ ptr unsafe.Pointer }
type GPU     struct{ ptr unsafe.Pointer }

// Load opens libmtml.so (or the fake equivalent on PATH) and resolves symbols.
func Load(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.load_lib(cpath) != 0 {
		return errors.New("mtml: dlopen failed: " + path)
	}
	return nil
}

func Init() (*Library, error) {
	var l unsafe.Pointer
	if r := C.lib_init((**C.MtmlLibrary)(unsafe.Pointer(&l))); r != SUCCESS {
		return nil, fmt.Errorf("mtmlLibraryInit failed: %d", int(r))
	}
	return &Library{ptr: l}, nil
}

func (l *Library) Shutdown() { C.lib_shutdown() }

func (l *Library) DeviceCount() (uint, error) {
	var n C.uint
	r := C.lib_count((*C.MtmlLibrary)(l.ptr), &n)
	if r != SUCCESS { return 0, fmt.Errorf("count: %d", int(r)) }
	return uint(n), nil
}

func (l *Library) Device(i uint) (*Device, error) {
	var d unsafe.Pointer
	r := C.lib_init_dev((*C.MtmlLibrary)(l.ptr), C.uint(i), (**C.MtmlDevice)(unsafe.Pointer(&d)))
	if r != SUCCESS { return nil, fmt.Errorf("device %d: %d", i, int(r)) }
	return &Device{ptr: d}, nil
}

func (l *Library) System() (*System, error) {
	var s unsafe.Pointer
	r := C.lib_init_sys((*C.MtmlLibrary)(l.ptr), (**C.MtmlSystem)(unsafe.Pointer(&s)))
	if r != SUCCESS { return nil, fmt.Errorf("system: %d", int(r)) }
	return &System{ptr: s}, nil
}

func (s *System) DriverVersion() (string, error) {
	buf := make([]byte, 80)
	r := C.sys_drv((*C.MtmlSystem)(s.ptr), (*C.char)(unsafe.Pointer(&buf[0])), 80)
	if r != SUCCESS { return "", fmt.Errorf("drv: %d", int(r)) }
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

func (d *Device) Name() (string, error) {
	buf := make([]byte, 64)
	r := C.dev_name((*C.MtmlDevice)(d.ptr), (*C.char)(unsafe.Pointer(&buf[0])), 64)
	if r != SUCCESS { return "", fmt.Errorf("name: %d", int(r)) }
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

func (d *Device) UUID() (string, error) {
	buf := make([]byte, 64)
	r := C.dev_uuid((*C.MtmlDevice)(d.ptr), (*C.char)(unsafe.Pointer(&buf[0])), 64)
	if r != SUCCESS { return "", fmt.Errorf("uuid: %d", int(r)) }
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

func (d *Device) Memory() (total, used uint64, err error) {
	var m unsafe.Pointer
	if r := C.dev_init_mem((*C.MtmlDevice)(d.ptr), (**C.MtmlMemory)(unsafe.Pointer(&m))); r != SUCCESS {
		return 0, 0, fmt.Errorf("init_mem: %d", int(r))
	}
	var t, u C.ulonglong
	C.mem_total((*C.MtmlMemory)(m), &t)
	C.mem_used((*C.MtmlMemory)(m), &u)
	return uint64(t), uint64(u), nil
}

func (d *Device) GPUUtil() (uint, error) {
	var g unsafe.Pointer
	if r := C.dev_init_gpu((*C.MtmlDevice)(d.ptr), (**C.MtmlGpu)(unsafe.Pointer(&g))); r != SUCCESS {
		return 0, fmt.Errorf("init_gpu: %d", int(r))
	}
	var pct C.uint
	C.gpu_util((*C.MtmlGpu)(g), &pct)
	return uint(pct), nil
}
```

- [ ] **步骤 2：验证包能编译**

```bash
go build ./pkg/mthreads/mtml/...
```

预期：exit 0。

- [ ] **步骤 3：提交**

```bash
git add pkg/mthreads/mtml/mtml.go
git commit -m "feat(musa): add Go cgo bindings for libmtml.so"
```

---

## 任务 10：mt-smi 命令

镜像 `cmd/nvidia-smi/main.go` 和 `pkg/nvidia/root.go`。通过任务 9 的绑定从 libmtml.so 读取数据，并打印一个类似真实 `mt-smi` 输出的表格。

**文件：**
- 创建：`pkg/mthreads/common/gpu.go`
- 创建：`pkg/mthreads/root.go`
- 创建：`cmd/mt-smi/main.go`

- [ ] **步骤 1：增加公共 GPU struct**

```go
package common

type GPU struct {
	Idx      uint
	Name     string
	UUID     string
	TotalMem uint64
	UsedMem  uint64
	Util     uint
}
```

- [ ] **步骤 2：增加 cobra 根命令**

`pkg/mthreads/root.go`：

```go
package mthreads

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/common"
	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/mtml"
)

var libPath string

func init() {
	RootCmd.PersistentFlags().StringVar(&libPath, "libmtml", "libmtml.so",
		"path to libmtml.so")
}

var RootCmd = &cobra.Command{
	Use:   "mt-smi",
	Short: "mt-smi is a fake equivalent of Moore Threads' mt-smi tool",
	Run: func(_ *cobra.Command, _ []string) {
		if err := run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
	},
}

func run() error {
	if err := mtml.Load(libPath); err != nil { return err }
	lib, err := mtml.Init()
	if err != nil { return err }
	defer lib.Shutdown()

	sys, _ := lib.System()
	driver, _ := sys.DriverVersion()

	count, err := lib.DeviceCount()
	if err != nil { return err }

	var gpus []common.GPU
	for i := uint(0); i < count; i++ {
		d, err := lib.Device(i)
		if err != nil { return err }
		name, _ := d.Name()
		uuid, _ := d.UUID()
		total, used, _ := d.Memory()
		util, _ := d.GPUUtil()
		gpus = append(gpus, common.GPU{
			Idx: i, Name: name, UUID: uuid,
			TotalMem: total, UsedMem: used, Util: util,
		})
	}

	fmt.Println(time.Now().Format(time.ANSIC))
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetTitle(fmt.Sprintf("MT-SMI (fake)    Driver Version: %s", driver))
	t.AppendHeader(table.Row{"GPU", "Name", "UUID", "Memory-Usage", "GPU-Util"})
	for _, g := range gpus {
		t.AppendRow(table.Row{
			strconv.Itoa(int(g.Idx)),
			g.Name,
			g.UUID,
			fmt.Sprintf("%d MiB / %d MiB", g.UsedMem/1024/1024, g.TotalMem/1024/1024),
			fmt.Sprintf("%d%%", g.Util),
		})
	}
	t.Render()
	return nil
}
```

- [ ] **步骤 3：增加二进制入口点**

`cmd/mt-smi/main.go`：

```go
package main

import (
	"fmt"
	"os"

	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads"
)

func main() {
	if err := mthreads.RootCmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
```

- [ ] **步骤 4：构建并运行**

```bash
make build BUILD_TYPE=Release
go build -o output/bin/mt-smi ./cmd/mt-smi
# 不拆 so：MTML 符号在 libfakegpu.so 中，dlopen 它即可
FAKE_MUSA_CONFIG=$PWD/conf/fake-musa.yaml \
  ./output/bin/mt-smi --libmtml=$PWD/output/lib64/libfakegpu.so
```

预期：表格显示一行，内容为 `MTT S80`、`MTGPU-0`、`0 MiB / 16384 MiB`、`0%`。

- [ ] **步骤 5：提交**

```bash
git add pkg/mthreads/ cmd/mt-smi/
git commit -m "feat(musa): add mt-smi command"
```

---

## 任务 11：device-injector — vendor 标志和 MUSA 库列表

**文件：**
- 修改：`cmd/device-injector/main.go`

- [ ] **步骤 1：增加 `--vendor` 标志和 MUSA 库表**

在 `main()` 中已有的 `flag.StringVar(&commands, ...)` 行之后追加：

```go
flag.StringVar(&vendor, "vendor", "nvidia",
    "GPU vendor to fake: nvidia | musa | both")
```

在文件顶部，已有的 `var (...)` 块旁边：

```go
var (
    vendor string
)

var musaLibraryFiles = []string{
    "libmusa.so",
    "libmusart.so",
    "libmtml.so",
}

var nvidiaLibraryFiles = []string{
    "libcuda.so.1",
    "libnvidia-ml.so.1",
    "libcudart.so",
}
```

- [ ] **步骤 2：把 `injectMounts` 重构为按 vendor 分派（含单容器互斥）**

**对齐 `docs/mthreads-support-design.md` §4.6 决策"先互斥"**：当 `--vendor=both` 时，单容器内仍**不允许**同时声明 `NVIDIA_VISIBLE_DEVICES` 和 `MUSA_VISIBLE_DEVICES`；命中则跳过注入并记录 warning，让上层调度器（HAMi / device-plugin / scheduler webhook）保证一个 Pod 一种异构资源。

把已有的硬编码 `filenames := []string{...}` 块（当前第 107-111 行）替换为按 vendor 选择的逻辑。同时把第 90-106 行的环境变量 `switch` 块和 `filenames` 块一起替换为：

```go
// detect requested vendors
type vendorPlan struct {
    libsToReplace []string
    sourceLib     string  // file under sourceHostPath that masquerades as each lib
    configEnv     string
    configFile    string
}
var plans []vendorPlan

wantNvidia := vendor == "nvidia" || vendor == "both"
wantMusa   := vendor == "musa"   || vendor == "both"

// Per-container probe: which vendors did this container actually request?
nvRequested := false
musaRequested := false

if wantNvidia {
    if env, ok := findEnvWithNameAndValue("NVIDIA_VISIBLE_DEVICES", ctr.Env); ok && env != "void" {
        nvRequested = true
        if env == "all" { visibleAllDevice = true }
    } else if findEnvWithName("NVIDIA_REQUIRE_CUDA", ctr.Env) &&
              findEnvWithName("CUDA_VERSION", ctr.Env) {
        nvRequested = true
        visibleAllDevice = true
    }
}
if wantMusa {
    if env, ok := findEnvWithNameAndValue("MUSA_VISIBLE_DEVICES", ctr.Env); ok && env != "void" {
        musaRequested = true
        if env == "all" { visibleAllDevice = true }
    }
}

// Mutual-exclusion gate (decision: 先互斥). Even with --vendor=both we refuse
// to inject when one container declares BOTH NVIDIA and MUSA env vars.
if nvRequested && musaRequested {
    log.Warnf("%s: refusing injection — container declares both NVIDIA_VISIBLE_DEVICES and MUSA_VISIBLE_DEVICES; vendors must be mutually exclusive per container",
        containerName(pod, ctr))
    return nil
}

if nvRequested {
    plans = append(plans, vendorPlan{
        libsToReplace: nvidiaLibraryFiles,
        sourceLib:     "libfakegpu.so",
        configEnv:     "FAKE_GPU_CONFIG",
        configFile:    "fake-gpu.yaml",
    })
}
if musaRequested {
    plans = append(plans, vendorPlan{
        libsToReplace: musaLibraryFiles,
        sourceLib:     "libfakegpu.so",  // 同一个 SO,bind-mount 成多个目的名 — 不拆 so
        configEnv:     "FAKE_MUSA_CONFIG",
        configFile:    "fake-musa.yaml",
    })
}

if len(plans) == 0 {
    log.Debugf("%s: no vendor matched", containerName(pod, ctr))
    return nil
}

for _, p := range plans {
    for _, fn := range p.libsToReplace {
        for _, lp := range librarySearchPaths {
            mounts = append(mounts, mount{
                Source:      fmt.Sprintf("%s/%s", sourceHostPath, p.sourceLib),
                Destination: fmt.Sprintf("%s/%s", lp, fn),
                Type:        "bind",
                Options:     mountOption,
            })
        }
    }
    mounts = append(mounts, mount{
        Source:      fmt.Sprintf("%s/%s", sourceHostPath, p.configFile),
        Destination: fmt.Sprintf("/usr/local/fake-gpu/%s", p.configFile),
        Type:        "bind",
        Options:     mountOption,
    })
}
```

**关键变化点（与原版对比）**：
1. `sourceLib` 对 MUSA 计划也固定为 `"libfakegpu.so"`，落实"不拆 so"
2. 新增 `nvRequested && musaRequested` 互斥闸门，落实"先互斥"
3. 把"是否请求"与"是否要注入"分开两段判断，方便互斥闸门夹在中间

文件底部（替换已有的 `a.AddEnv("FAKE_GPU_CONFIG", ...)` 块）：

```go
for _, p := range plans {
    a.AddEnv(p.configEnv, fmt.Sprintf("/usr/local/fake-gpu/%s", p.configFile))
}
if len(gpusuffix) > 0 {
    a.AddEnv("FAKE_GPU_SUFFIX", gpusuffix)
    a.AddEnv("FAKE_MUSA_SUFFIX", gpusuffix)
}
```

- [ ] **步骤 3：也覆盖 `mt-smi` 命令**

`commands` 标志的默认值改成：

```go
flag.StringVar(&commands, "override-commands",
    "nvidia-smi,vectorAdd,mt-smi",
    "Override commands in the container")
```

已有的 per-command 挂载循环（132-139 行）已经能处理这个，但它的 source 文件被硬编码为 `nvidia-smi`。把它改成命令名 → host 二进制的映射：

```go
overrideSourceMap := map[string]string{
    "nvidia-smi": "nvidia-smi",
    "vectorAdd":  "nvidia-smi",  // existing pre-image behaviour
    "mt-smi":     "mt-smi",
}
for _, command := range overrideCommand {
    src, ok := overrideSourceMap[command]
    if !ok { src = command }
    mounts = append(mounts, mount{
        Source:      fmt.Sprintf("%s/%s", sourceHostPath, src),
        Destination: "/usr/bin/" + command,
        Type:        "bind",
        Options:     mountOption,
    })
}
```

- [ ] **步骤 4：构建并验证仍能编译**

```bash
go build -o output/bin/device-injector ./cmd/device-injector
./output/bin/device-injector --help 2>&1 | grep vendor
```

预期：打印含有 `-vendor string` 的一行。

- [ ] **步骤 5：提交**

```bash
git add cmd/device-injector/main.go
git commit -m "feat(musa): add --vendor flag and MUSA injection paths to device-injector"
```

---

## 任务 12：Makefile、Dockerfile、entrypoint、Helm chart 更新

**文件：**
- 修改：`Makefile`
- 修改：`Dockerfile`
- 修改：`entrypoint.sh`
- 修改：`charts/fake-gpu/values.yaml`
- 修改：`charts/fake-gpu/templates/configmap.yaml`
- 修改：`charts/fake-gpu/templates/daemonset.yaml`

- [ ] **步骤 1：往 Makefile 加 `mt-smi` 构建目标**

替换已有的 `build-cmd` 行和文件末尾的目标块：

```makefile
build-cmd: device-injector nvidia-smi mt-smi

device-injector:
	$(GO) build -o ${OUTPUT_DIR}/bin/device-injector ./cmd/device-injector/main.go

nvidia-smi:
	$(GO) build -o ${OUTPUT_DIR}/bin/nvidia-smi ./cmd/nvidia-smi/main.go

mt-smi:
	$(GO) build -o ${OUTPUT_DIR}/bin/mt-smi ./cmd/mt-smi/main.go
```

- [ ] **步骤 2：更新 Dockerfile 拷贝 MUSA 配套产物**

**注意（对齐 `docs/mthreads-support-design.md` 决策"不拆 so"）**：MTML/MUSA/MUSART 符号已经在任务 8 里编进同一个 `libfakegpu.so`；**不要**额外 `COPY libfakemusa.so / libmusa.so / libmusart.so`。device-injector 会把这一个 `libfakegpu.so` bind-mount 成 6 个目的文件名。

在最终阶段追加：

```dockerfile
COPY --from=gobuild /go/src/github.com/chaunceyjiang/fake-gpu/output/bin/mt-smi /fake-gpu/mt-smi
COPY ./conf/fake-musa.yaml /fake-gpu/fake-musa.yaml
```

- [ ] **步骤 3：更新 entrypoint.sh 同时统计两份配置**

把已有的 `gpu_num=...` 行替换为：

```sh
nv_gpu_num=$(grep -c cuda_version /fake-gpu/fake-gpu.yaml 2>/dev/null || echo 0)
musa_gpu_num=$(grep -c '^  - name:' /fake-gpu/fake-musa.yaml 2>/dev/null || echo 0)
gpu_num=$((nv_gpu_num + musa_gpu_num))
```

- [ ] **步骤 4：往 values.yaml 加 `vendor`**

追加：

```yaml
# GPU vendor to simulate. One of: nvidia, musa, both
vendor: nvidia
```

- [ ] **步骤 5：把 vendor 透传到 daemonset.yaml**

在 `command:` 块中追加：

```yaml
            - -vendor
            - {{ .Values.vendor | quote }}
```

并在已有的 `config-file` 卷挂载旁边加上 musa config 文件挂载：

```yaml
          - name: musa-config-file
            mountPath: /fake-gpu/fake-musa.yaml
            subPath: fake-musa.yaml
```

在 `volumes:` 中加：

```yaml
      - name: musa-config-file
        configMap:
          name: {{ .Release.Name }}-configmap
```

- [ ] **步骤 6：更新 configmap.yaml 把两份配置都包含进去**

先读已有文件，然后增加第二个 key `fake-musa.yaml: |`，内容是 `conf/fake-musa.yaml` 的全部内容。

- [ ] **步骤 7：端到端构建冒烟测试**

```bash
make build BUILD_TYPE=Release
test -f output/lib64/libfakegpu.so   # 合并后唯一的 SO
# MTML/MUSA 符号应在同一个 libfakegpu.so 中
nm -D --defined-only output/lib64/libfakegpu.so | grep -q '^.* T mtmlLibraryInit'
nm -D --defined-only output/lib64/libfakegpu.so | grep -q '^.* T muInit'
nm -D --defined-only output/lib64/libfakegpu.so | grep -q '^.* T musaGetDeviceCount'
test -f output/bin/mt-smi
test -f output/bin/device-injector
test ! -f output/lib64/libfakemusa.so  # 应当不存在 — 已合并入 libfakegpu.so
helm lint charts/fake-gpu
```

预期：所有命令都没有错误，且 `libfakemusa.so` 不存在。

- [ ] **步骤 8：提交**

```bash
git add Makefile Dockerfile entrypoint.sh charts/
git commit -m "feat(musa): wire MUSA build, Docker, and Helm chart"
```

---

## 任务 13：文档

**文件：**
- 创建：`docs/musa.md`
- 修改：`README.md`

- [ ] **步骤 1：写 `docs/musa.md`**

涵盖以下内容：
- 这个特性能做什么（除 NVIDIA 外，并行/替换地伪造 MTT GPU）
- 前置条件：containerd ≥ 1.7、NRI 已开启
- 如何切换 vendor：`--set vendor=musa` 或 `--set vendor=both`
- GPU 列表的修改位置：`conf/fake-musa.yaml`
- Pod 环境变量：`MUSA_VISIBLE_DEVICES=all` 触发注入
- 局限：MUSA 计算返回错误（仅 mock）；MPC 虚拟化未实现；尚无 DCGM 等价物
- 来源标注：MTML 头来自 `MooreThreads/mthreads-ml-py`；MUSA Driver/Runtime 符号清单来自 MUSA SDK 3.1.0 `nm -D` 提取后筛选（20+20 个核心符号；ollama PR#7554 的 18 个为子集）

- [ ] **步骤 2：在 README.md 加一段指引**

在 "Features" 下加：
```
- Supports Moore Threads MUSA GPUs (see [docs/musa.md](docs/musa.md))
```

- [ ] **步骤 3：提交**

```bash
git add docs/musa.md README.md
git commit -m "docs(musa): add MUSA backend usage documentation"
```

---

## 任务 14：双覆盖测试矩阵（对齐设计文档 §7.0）

**对齐 `docs/mthreads-support-design.md` §7.0**：用户明确要求"测试的时候覆盖两者来测"。每个 P0 改动都必须在 NVIDIA 路径和 MThreads 路径上都跑过。任何 NVIDIA 回归（哪怕只是一行 vendor 分派的微小改动）就是阻断合并的硬性条件。

**文件：**
- 创建：`scripts/dual-coverage.sh` — 串起 NVIDIA + MUSA 双路径冒烟测试
- 修改：`charts/fake-gpu/templates/` — 增加 `vendor=both` 的部署示例（用于 T9）

- [ ] **步骤 1：实现 T1–T9 测试矩阵脚本**

`scripts/dual-coverage.sh` 串起以下九组用例。每组都要单独 exit-code 检查；任意一组失败立即终止整个脚本：

| ID  | 路径              | 用例                                                          | 验收 |
|-----|-----------------|-------------------------------------------------------------|----|
| T1  | NVIDIA           | `nvidia-smi -L` 列出 fake GPU                                  | 至少 1 行 |
| T2  | NVIDIA           | `cuInit + cuDeviceGetCount` 返回 fake-gpu.yaml 中的 GPU 数         | count == yaml |
| T3  | NVIDIA           | `cudaGetDeviceCount + cudaMalloc(1MB)` 走 fake CUDA Runtime    | 不 segfault，行为符合 cudart_hook.cpp |
| T4  | MTHREADS         | `mt-smi -L` 列出 fake MTT GPU                                  | 至少 1 行（来自 fake-musa.yaml） |
| T5  | MTHREADS         | `mtmlLibraryInit + mtmlLibraryCountDevice` 返回 fake-musa.yaml 中的 GPU 数 | count == yaml |
| T6  | MTHREADS         | `muInit + muDeviceGetCount` → 0；`muMemGetInfo_v2` → `MU_ERROR_NOT_INITIALIZED` | 决策"返回错误优于伪造执行" |
| T7  | MTHREADS         | `musaSetDevice + musaMemGetInfo` → `musaErrorNoDevice`             | 同上 |
| T8  | DEVICE-INJECTOR  | 单容器同时声明 `NVIDIA_VISIBLE_DEVICES=all` 和 `MUSA_VISIBLE_DEVICES=all` | injector 拒绝注入并打 warning（互斥决策） |
| T9  | DEVICE-INJECTOR  | `vendor=both`、容器 A 仅有 NV 环境变量、容器 B 仅有 MUSA 环境变量、容器 C 都没有 | A 只挂 NV 三个目的名、B 只挂 MUSA 三个目的名、C 不挂任何东西 |

```bash
#!/usr/bin/env bash
# scripts/dual-coverage.sh — 双覆盖测试矩阵（对齐 docs/mthreads-support-design.md §7.0）
set -euo pipefail

# 前置：libfakegpu.so 已构建（一个 SO，6 套 API）
LIB=${LIB:-output/lib64/libfakegpu.so}
test -f "$LIB" || { echo "missing $LIB — 先 make build"; exit 1; }

# T1
echo "=== T1: nvidia-smi -L ==="
LD_PRELOAD="$LIB" output/bin/nvidia-smi -L | grep -E '^GPU [0-9]+:' || { echo T1_FAIL; exit 1; }

# T2-T3：构建一个小的 CUDA Runtime smoke 可执行（或复用 src/cuda 现有 vectorAdd）
echo "=== T2/T3: CUDA Driver + Runtime smoke ==="
LD_PRELOAD="$LIB" output/bin/vectorAdd >/dev/null || { echo T2T3_FAIL; exit 1; }

# T4
echo "=== T4: mt-smi -L ==="
LD_PRELOAD="$LIB" output/bin/mt-smi -L | grep -E '^GPU [0-9]+:' || { echo T4_FAIL; exit 1; }

# T5：mt-smi 自身已经调用了 mtmlLibraryCountDevice；T5 复用 T4 输出行数
expected=$(grep -c '^  - name:' conf/fake-musa.yaml || echo 0)
got=$(LD_PRELOAD="$LIB" output/bin/mt-smi -L | grep -c '^GPU [0-9]+:')
[ "$got" = "$expected" ] || { echo "T5_FAIL: mtml count=$got expected=$expected"; exit 1; }

# T6-T7：用 dlsym + 调用，写一段 musa-smoke.c（放在 scripts/musa-smoke.c）
echo "=== T6/T7: MUSA Driver/Runtime stubs return errors ==="
LD_PRELOAD="$LIB" scripts/musa-smoke || { echo T6T7_FAIL; exit 1; }

# T8：构造同时声明两种 env 的 NRI fixture，调 injector 看输出
echo "=== T8: mutual exclusion ==="
go test ./cmd/device-injector -run TestMutualExclusion || { echo T8_FAIL; exit 1; }

# T9：vendor=both + 三种容器的注入计划
echo "=== T9: vendor=both per-container plan ==="
go test ./cmd/device-injector -run TestVendorBoth || { echo T9_FAIL; exit 1; }

echo "=== dual-coverage: ALL PASS ==="
```

- [ ] **步骤 2：补 `scripts/musa-smoke.c` 用于 T6/T7**

```c
// 通过 dlsym 调 libfakegpu.so 中的 MUSA 符号，验证存根行为
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
int main() {
    void *h = dlopen(getenv("LIB") ? getenv("LIB") : "libfakegpu.so", RTLD_NOW);
    if (!h) { fprintf(stderr, "dlopen: %s\n", dlerror()); return 1; }

    int (*muInit)(unsigned) = dlsym(h, "muInit");
    int (*muDevCount)(int*) = dlsym(h, "muDeviceGetCount");
    int (*muMemInfo)(size_t*, size_t*) = dlsym(h, "muMemGetInfo_v2");
    int (*musaSet)(int) = dlsym(h, "musaSetDevice");

    int n = -1; size_t f = 0, t = 0;
    if (muInit(0) != 0) return 10;
    if (muDevCount(&n) != 0 || n != 0) return 11;
    if (muMemInfo(&f, &t) != 3 /* MU_ERROR_NOT_INITIALIZED */) return 12;
    if (musaSet(0) != 38 /* musaErrorNoDevice */) return 13;
    puts("musa-smoke ok");
    return 0;
}
```

构建命令（放进 Makefile）：

```makefile
musa-smoke:
	$(CC) -o output/bin/musa-smoke scripts/musa-smoke.c -ldl
```

- [ ] **步骤 3：补 `cmd/device-injector` 单元测试覆盖 T8/T9**

新建 `cmd/device-injector/main_test.go`，覆盖：
- `TestMutualExclusion`：构造 pod 含一个容器，env 同时含 `NVIDIA_VISIBLE_DEVICES=all` 和 `MUSA_VISIBLE_DEVICES=all`，断言注入函数返回空 mount 列表并触发 warning。
- `TestVendorBoth`：构造 pod 含 3 个容器，`vendor=both`，断言每个容器的 mount 列表只对应它自己声明的 vendor。

- [ ] **步骤 4：CI 集成**

在 `Makefile` 增加 `make test-dual` 目标，把 `dual-coverage.sh` 串到现有 `make test` 之后。任何 T1–T9 失败都阻断 CI。

- [ ] **步骤 5：提交**

```bash
git add scripts/dual-coverage.sh scripts/musa-smoke.c cmd/device-injector/main_test.go Makefile
git commit -m "test(musa): add T1-T9 dual-coverage matrix (NVIDIA + MTHREADS paths)"
```

---

## 自审清单

- [x] **需求覆盖** — 我们讨论中的每一块（vendor MTML 头、40 个 MUSA 符号存根（20 driver+20 runtime）、并行目录布局、vendor 标志、环境变量、Helm 接线、双覆盖测试）都对应到了一个编号任务。
- [x] **占位符扫描** — 每个步骤都给了真实代码或真实命令，没有 "TBD" / "增加错误处理" 这类存根。
- [x] **类型一致性** — `MtGPU`/`MtGPUList`/`MtmlDevice`/`MtmlMemory`/`MtmlGpu` 在任务 4-10 中名称一致。
- [x] **函数一致性** — 任务 9 的 cgo trampoline 与任务 5-7 中定义的 C 符号一一对应。
- [x] **路径一致性** — 6 套 API 都在同一个 `libfakegpu.so`（**不拆 so**），device-injector 把它 bind-mount 成 6 个目的文件名（`libcuda.so.1` / `libnvidia-ml.so.1` / `libcudart.so` / `libmusa.so` / `libmtml.so` / `libmusart.so`）。`conf/fake-musa.yaml` 与 `FAKE_MUSA_CONFIG` 仅为 MUSA 配置入口，与 NVIDIA 配置入口 `conf/fake-gpu.yaml` / `FAKE_GPU_CONFIG` 并行存在。
- [x] **设计文档对齐** — 本计划与 `docs/mthreads-support-design.md` 的 4 项决策一致：(1) MUSA 计算 API 返回错误 (2) 不拆 so (3) DCGM 等价物先不支持 (4) 单容器内 NV / MUSA env vars 互斥。
- [x] **双覆盖测试** — 任务 14 的 T1–T9 矩阵覆盖 NVIDIA 与 MTHREADS 两条路径，任意一侧回归即阻断合并。

---

## 风险与待确认问题

1. **MTML 不透明指针 ABI** — 我们把 device handle 编码为 `intptr_t index+1`。如果在同一个进程地址空间里曾经加载过真实的 MTML 库（fake-gpu 几乎不会，但混合集群里可能出现），指针比较就不会匹配。缓解：本库只在 bind-mount 沙箱中使用，因此真实库从不会同时存在。
2. **Brand 枚举值** — 公开头文件里 `MtmlBrandType` 的枚举值除了 `MTML_BRAND_TYPE_*` 常量外没有详细文档；我们硬编码为 `1`（MTT）。如果消费者把它和某个具体常量比较，可能需要回头修任务 6 步骤 1。
3. **mt-smi 视觉保真度** — 表格输出是个粗略近似，并非真实 `mt-smi` 的逐字节复刻。要做屏幕抓取的消费者需要先对真实工具的输出建立测试用例，我们才能宣称 parity。
4. **HAMi 集成** — 不在本计划范围；Helm chart 仍只与 `nvidia-device-plugin` 共部署。后续计划应增加通过 HAMi 的 mthreads device plugin 注册 `mthreads.com/vgpu` 资源。
5. **单 SO 二进制体积膨胀** — 合并后 `libfakegpu.so` 同时承载 6 套 API，体积会比合并前增加 MTML+MUSA Driver+MUSA Runtime 三部分的总和（预估 +50KB ~ +200KB）。已与设计文档"不拆 so"决策权衡过——可观测性收益（统一加载、单点配置）大于体积代价。如果未来 SO 超过 5MB，回头评估是否切回拆分方案。
6. **互斥决策的可观察性** — 当 device-injector 因 NV/MUSA env 同时存在而拒绝注入时，只打 warning 不返回错误，可能让用户难以发现"为什么 fake GPU 没生效"。缓解：warning 中包含完整容器名 + 两个冲突 env 名；后续可加 Prometheus counter 暴露拒绝次数。
