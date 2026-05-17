# fake-gpu 支持 MThreads（摩尔线程）GPU — 设计文档

> 状态：草案 · 2026-05-16
> 作者：协作设计
> 目标读者：fake-gpu 维护者、想模拟国产 GPU 的测试/平台工程师

---

## 1. 背景与目标

当前 `fake-gpu` 只能模拟 NVIDIA GPU（通过劫持 NVML / CUDA Driver / CUDA Runtime），用于在没有真卡的 K8s 环境里测试 GPU 调度与上层应用。

随着国产 GPU 在 K8s 生态中铺开，**HAMi 已原生支持 MThreads（资源名 `mthreads.com/vgpu`）**，但 HAMi 只解决"调度伪装"，**应用容器内执行 `mthreads-gmi` 或调用 `musaXxx` API 仍会失败**，因为没有真实 driver。

### 目标

在 fake-gpu 现有架构基础上，**追加一套"MThreads 伪装栈"**，让：

1. 应用容器内 `mthreads-gmi` 输出与 yaml 配置一致的假数据
2. 应用程序调用 `musa*` / `mtml*` API 时不报错（返回伪造的设备/内存/状态）
3. 与 NVIDIA 路径**并存且互不影响**（同节点可同时模拟两种 GPU）

### 非目标

- ❌ 不做真实计算（fake-gpu 本就明确不计算）
- ❌ 不实现 vGPU 切分逻辑（那是 HAMi 的工作）
- ❌ 不模拟 MUSA 的完整 API 覆盖率（先覆盖"查询类"API，能跑通发现/管理类工具即可）

---

## 2. 现有架构回顾

### 2.1 模块关系

```
                       ┌─────────────────────────────────┐
                       │  conf/fake-gpu.yaml             │
                       │  (单一事实来源，描述假 GPU)     │
                       └────────────────┬────────────────┘
                                        │ 读取
                                        ▼
   ┌──────────────────────────────────────────────────────────────────┐
   │ libfakegpu.so  (CMakeLists.txt: GLOB src/*/*.cpp)                │
   │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐    │
   │  │ src/nvml/    │  │ src/cuda/    │  │ src/cudart/          │    │
   │  │ nvml_hook    │  │ cuda_hook    │  │ cudart_hook          │    │
   │  │ (2093 行)    │  │ (2312 行)    │  │ (1760 行)            │    │
   │  └──────┬───────┘  └──────┬───────┘  └──────────┬───────────┘    │
   │         │ 共用             │                     │                │
   │         ▼                  ▼                     ▼                │
   │  ┌─────────────────────────────────────────────────────────┐     │
   │  │ src/common/ (GPU/PCI/NVLink/DCGM 结构体 + yaml 解析)    │     │
   │  └─────────────────────────────────────────────────────────┘     │
   └──────────────────────────────────────────────────────────────────┘
                                        ▲
                                        │ bind-mount 为:
                                        │   /usr/lib*/libnvidia-ml.so.1
                                        │   /usr/lib*/libcuda.so.1
                                        │   /usr/lib*/libcudart.so
                                        │
   ┌──────────────────────────────────────────────────────────────────┐
   │ cmd/device-injector  (NRI plugin)                                │
   │ 触发条件：容器 env 含 NVIDIA_VISIBLE_DEVICES                     │
   │ 注入：libfakegpu.so + nvidia-smi + fake-gpu.yaml                 │
   └──────────────────────────────────────────────────────────────────┘
                                        ▲
                                        │
   ┌──────────────────────────────────────────────────────────────────┐
   │ cmd/nvidia-smi  (Go 二进制，读 yaml 直接输出表格)                │
   └──────────────────────────────────────────────────────────────────┘
```

### 2.2 关键不变量

| 不变量 | 体现位置 | 说明 |
|--------|---------|------|
| 单一配置文件 | `conf/fake-gpu.yaml` | 顶层 key 是 `nvidia:` |
| 单一共享库 | `libfakegpu.so` | 同时导出 nvml/cuda/cudart 全部符号 |
| 库被映射为三个文件 | device-injector | `libnvidia-ml.so.1`、`libcuda.so.1`、`libcudart.so` |
| 触发器 | `NVIDIA_VISIBLE_DEVICES` env | 由 nvidia-device-plugin/HAMi 注入 |
| Hook 模式 | 所有 `*_hook.cpp` | `HOOK_C_API HOOK_DECL_EXPORT` 直接定义函数，让符号查找命中本 so |

### 2.3 注入流程（device-injector）

```
Pod 创建 → containerd → NRI → device-injector.CreateContainer()
                                  │
                                  ├─ 检查 env NVIDIA_VISIBLE_DEVICES
                                  │   有效 → injectGPU = injectGPUTypeGPU
                                  │
                                  ├─ 对每个搜索路径 × 每个目标文件名 bind-mount libfakegpu.so
                                  │   /lib/libnvidia-ml.so.1 → libfakegpu.so
                                  │   /lib/libcuda.so.1      → libfakegpu.so
                                  │   /lib/libcudart.so      → libfakegpu.so
                                  │   (x86_64-linux-gnu / aarch64-linux-gnu 同样)
                                  │
                                  ├─ bind-mount fake-gpu.yaml 到 /usr/local/fake-gpu/
                                  ├─ bind-mount nvidia-smi（如配置了 overrideCommand）
                                  └─ 注入 env FAKE_GPU_CONFIG / FAKE_GPU_SUFFIX
```

> **重要观察**：`cmd/device-injector/main.go:30` 已定义了 `injectGPUTypevGPU` 枚举但未使用。说明作者**预留过多类型扩展点**，本设计可以接续这一扩展点。

---

## 3. MThreads 技术栈映射

| NVIDIA 概念 | MThreads 对应 | 头文件 | k8s 资源名 |
|------------|-------------|--------|------------|
| NVML (管理) | **MTML** | `mtml.h` | — |
| CUDA Driver API | **MUSA Driver API** | `musa.h` | — |
| CUDA Runtime API | **MUSA Runtime API** | `musa_runtime.h` | — |
| cuBLAS / cuDNN / NCCL | mublas / mudnn / mccl | `mublas.h` 等 | — |
| `nvidia-smi` 命令 | **`mthreads-gmi`** | — | — |
| `libnvidia-ml.so.1` | **`libmtml.so.1`** | — | — |
| `libcuda.so.1` | **`libmusa.so`**（或 `libmusa.so.1`） | — | — |
| `libcudart.so` | **`libmusart.so`** | — | — |
| Device plugin 资源 | nvidia.com/gpu | — | **`mthreads.com/vgpu`** |
| 设备节点 | `/dev/nvidia0..N` | — | **`/dev/mtgpu0..N`** + `/dev/mtgpu_uvm` |
| 容器 env 触发 | `NVIDIA_VISIBLE_DEVICES` | — | **`MUSA_VISIBLE_DEVICES`**（HAMi 约定） |

> 命名规律：`cuda*` → `musa*`，`nv*` → `mt*`/`mu*`，函数签名几乎一一对应。

---

## 4. 设计方案

### 4.1 总体策略：**单 so 多入口，配置区分厂商**

继续维持"一个 `libfakegpu.so` 导出所有符号"的模式，**不拆分 so**（已决策）。理由：

1. CMake `GLOB src/*/*.cpp` 已天然支持新增子目录
2. 函数命名空间天然不冲突（`nvml*` vs `mtml*`，`cuda*` vs `musa*`）
3. 简化 device-injector 逻辑：一份 so 应付两家
4. 减少镜像层、降低维护成本

### 4.2 目录结构

```
src/
├── common/                  # 共用结构体（不动）
│   ├── common.h             # 已有 GPU 结构，需要小幅扩展，见 §4.3
│   ├── macro_common.h
│   └── trace_profile.h
├── nvml/                    # 不动
├── cuda/                    # 不动
├── cudart/                  # 不动
├── mtml/                    # 【新增】对应 NVML
│   ├── mtml_subset.h        # 摘抄 MTML 类型/枚举定义
│   ├── mtml_hook.cpp        # mtmlInit / mtmlDeviceGetCount 等
│   └── export_table.h       # 占位/注释，与 nvml 风格一致
├── musa/                    # 【新增】对应 CUDA Driver
│   ├── musa_subset.h
│   └── musa_hook.cpp        # musaInit / muDeviceGet 等
└── musart/                  # 【新增】对应 CUDA Runtime
    ├── musart_subset.h
    └── musart_hook.cpp      # musaMalloc / musaMemcpy 等
```

### 4.3 配置文件演进

**向后兼容**地新增 `mthreads:` 顶层 key：

```yaml
# conf/fake-gpu.yaml
nvidia:           # 现有，不动
  - name: NVIDIA Tesla T4
    ...

mthreads:         # 【新增】
  - name: MTT S4000
    uuid: MTGPU-0
    driver_version: "2.6.0"
    musa_version: 30100         # 对齐 NVIDIA 的 cuda_version 整数风格
    architecture: 3             # MUSA arch (PingNan/MoTuoQuan/Heng/QuYi 等)
    serial: "MT0001234567890"
    vbios_version: "1.0.0"
    image_version: "1.0"
    brand: "MTT"
    cuda_cores: 0               # 不适用，置 0
    memory:
      total: 51539607552         # 48GB
      free:  51539607552
    utilization:
      gpu: 0
      memory: 0
    power:
      usage: 50000
      defaultLimit: 350000
      minLimit: 100000
      maxLimit: 350000
      enforcedLimit: 350000
    pci:
      bus_id: "0000:01:00.0"
      bus: 1
      device_id: 1
      domain_id: 0
      sub_system_id: 0
```

**`common.h` 改动 — 字段必填表 + 解析方案**：

| yaml 字段 | NVIDIA | MThreads | 处理 |
|----------|:------:|:--------:|------|
| `name / uuid / driver_version / brand / serial / vbios_version / image_version` | ✅必填 | ✅必填 | 现状保持 |
| `architecture / cuda_cores / cuda_version`(MT 用 `musa_version`) | ✅必填 | ✅必填 | MTGPU 把 `musa_version` 映射到同一 `cuda_version` int 字段 |
| `memory / power / utilization / pci` | ✅必填 | ✅必填 | 现状保持 |
| `nvlink` | ✅必填 | ❌不适用 | **改为可选**：`if (node["nvlink"]) node["nvlink"] >> gpu.nvlink;` |
| `numa` | ✅必填 | ⚠️可选 | **改为可选** |
| `mig` | ✅必填 | ❌不适用 | **改为可选** |
| `events` | 已可选 | ❌不适用 | 现状已是可选，无需改 |
| `dcgm` | ✅必填 | ❌不适用（决策 #3） | **改为可选** |

**关键改动**：`common.h:196-211` 的 `operator>>(GPU)` 把上述 4 个字段从硬性 `as<>()` 改为存在性判断。**这是向后兼容的**——现有 NVIDIA yaml 依然包含全部字段，解析行为不变。

**结构复用方案**：

```cpp
// common.h（追加）
using MTGPUList = std::vector<GPU>;  // 复用 GPU 结构（在 operator>> 改为可选字段后才安全）
extern MTGPUList mthreads_gpus;
```

> 复用 `GPU` 结构的前提是上面 4 个字段变可选。**如果嫌共用结构会让 NVIDIA 代码读到"空 nvlink"产生误判**，可以反过来：保留 `GPU` 结构原样，新增独立 `MTGPU` 结构 + 独立 `operator>>`。当前判断："改可选 + 复用"成本最低，因为 NVIDIA 代码读这些字段的位置都已经做了存在性检查（如 nvlink 里就有 `if (node["peer_gpu_uuids"])`）。

### 4.4 Hook 实现策略

> **核心约定（已决策）**：所有"计算类"API（内存、kernel 启动、流、同步）一律返回对应错误码，**不伪造执行**。这与现有 `cudart_hook.cpp` / `cuda_hook.cpp` 中"返回 `cudaErrorInvalidValue` / `cudaErrorMemoryAllocation`"的风格一致。fake-gpu 只伪造**查询/管理类** API。

> **API 前缀待 P0 阶段实地确认**：MUSA Driver / Runtime 是否都用 `musa*` 前缀（部分公开材料显示 Driver Runtime 不像 NVIDIA `cu*` vs `cuda*` 那样严格区分）。下表先按"Driver=`mu*`、Runtime=`musa*`"的最大可能划分，P0 阶段读到真实头文件后修正。

#### MTML（最高优先级，对应国产 `mthreads-gmi`）

| API | 行为 |
|-----|------|
| `mtmlLibraryInit` | 调用 `init_mthreads()`（见 §4.5） |
| `mtmlLibraryShutDown` | 对齐现有 `nvmlShutdown` 实现风格（待 P0 阶段读 nvml_hook.cpp 后定型） |
| `mtmlLibraryCountDevice` | 返回 `mthreads_gpus.size()` |
| `mtmlLibraryInitDeviceByIndex` | 按 index 返回 handle（强转为 `GPU*` 指针） |
| `mtmlDeviceGetName` | 拷贝 yaml 的 `name` |
| `mtmlDeviceGetUUID` | 拷贝 `uuid`（若 `FAKE_GPU_SUFFIX` 设置则追加） |
| `mtmlDeviceGetMemoryInfo` | 填 `total/free/used` |
| `mtmlDeviceGetGpuUtilization` | 填 `utilization.gpu` |
| `mtmlDeviceGetPowerUsage` | 填 `power.usage` |
| `mtmlDeviceGetPciInfo` | 填 PCI |
| 其他 100+ API | 第一阶段返回 `MTML_ERROR_NOT_SUPPORTED`，按使用频率逐步实现 |

#### MUSA Runtime（次优先，应用最常用）

> **2026-05-17 更新**：MUSA Runtime 的"查询类"API 改为**从 yaml 伪造**，
> 不再延续 fail-loud。理由：MUSA SDK 自带的 `musaInfo`（CUDA deviceQuery 等
> 价物）和 PyTorch 的 musa 后端 device 检测都直接调用 Runtime 查询 API，
> 返回错误会让这些工具在容器里完全跑不起来。改造范围 = 查询 / 元数据 API；
> 计算类（Malloc/Memcpy/LaunchKernel）仍按决策 #1 fail-loud。

| API | 行为 |
|-----|------|
| `musaGetDeviceCount` | 返回 `g_gpus.size()`（yaml-backed） |
| `musaGetDeviceProperties` | 从 `MtGPU.compute` 填充 musaDeviceProp（layout 镜像 cudaDeviceProp） |
| `musaSetDevice` / `musaGetDevice` | 维护 thread-local 当前设备 ID |
| `musaGetDeviceFlags` / `musaSetDeviceFlags` | 返回 `musaSuccess`（不区分 flag） |
| `musaMemGetInfo` | 从当前设备的 `MtGPU.memory.total/free` 返回 |
| `musaDeviceGetPCIBusId` / `musaDeviceGetByPCIBusId` | 由 `MtGPU.pci.bus_id` 双向查找 |
| `musaDeviceGetAttribute` | 返回 `value=0` + `musaSuccess`（任何 attr 都报"feature not supported"，避免崩溃） |
| `musaDeviceSynchronize` / `musaDeviceReset` | 返回 `musaSuccess`（无操作）|
| `musaMalloc` / `musaFree` | 仍 fail-loud：`musaErrorMemoryAllocation`（决策 #1）|
| `musaMemcpy` / `musaLaunchKernel` 等 | 仍 fail-loud（未在子集导出）|

#### MUSA Driver

策略与 MUSA Runtime 相同：**返回错误优于伪造执行**。最小骨架函数（P3 实施）：

| API | 行为 |
|-----|------|
| `muInit` | 直接返回 `MUSA_SUCCESS`（或对应等价值）|
| `muDeviceGetCount` | 返回 `mthreads_gpus.size()` |
| `muDeviceGet` | 按 ordinal 返回 device handle |
| `muDeviceGetName` / `muDeviceGetAttribute` / `muDeviceTotalMem` | 从 yaml 填充 |
| `muMemAlloc` / `muMemcpy*` / `muLaunchKernel` | 返回 `MUSA_ERROR_*`（不伪造）|

### 4.5 初始化逻辑（共用并区分）

```cpp
// src/common/common.h 追加（伪代码）
extern GPUList nvidia_gpus;     // 现有，保持
extern MTGPUList mthreads_gpus; // 新增

// 现有的 init() 不改名（避免侵入 nvml_hook.cpp / cuda_hook.cpp / cudart_hook.cpp
// 中所有 callsite，sweep 风险与收益不成比例）。
// init() 保持只负责 NVIDIA 路径。
// 新增独立函数：
void init_mthreads();  // 读 config["mthreads"]，被 mtmlLibraryInit 调用
```

**关键设计点**：

1. **延迟初始化**：现有 `init()` 在 `nvmlInit_v2` 中按需调用；`init_mthreads()` 在 `mtmlLibraryInit` 中按需调用，**互不依赖**
2. **互斥锁**：`init_mthreads()` 用**独立 mutex**，不复用现有 `global_mutex`（避免 NVIDIA 初始化阻塞 MThreads 初始化）
3. **可见性环境变量隔离**：
   - NVIDIA 侧仍读 `NVIDIA_VISIBLE_DEVICES`
   - MThreads 侧读 `MUSA_VISIBLE_DEVICES`
4. **UUID 后缀复用**：`FAKE_GPU_SUFFIX` 同时作用于两类设备（已有逻辑，无需改）

### 4.6 device-injector 改造

#### 触发条件扩展 + 互斥校验（决策 #4）

```go
// cmd/device-injector/main.go（增改）
hasNvidia := findEnvWithName("NVIDIA_VISIBLE_DEVICES", ctr.Env)
hasMusa   := findEnvWithName("MUSA_VISIBLE_DEVICES", ctr.Env)

// 互斥校验：同时声明两家 → 报错拒绝注入，记日志（决策 #4）
if hasNvidia && hasMusa {
    log.Warnf("%s: both NVIDIA_VISIBLE_DEVICES and MUSA_VISIBLE_DEVICES present; "+
              "fake-gpu refuses to inject (mutually exclusive). Set only one.",
              containerName(pod, ctr))
    return nil
}

switch {
case hasNvidia:
    if env, ok := findEnvWithNameAndValue("NVIDIA_VISIBLE_DEVICES", ctr.Env); ok && env != "void" {
        injectGPU = injectGPUTypeGPU
    }
case hasMusa:  // 【新增分支】
    if env, ok := findEnvWithNameAndValue("MUSA_VISIBLE_DEVICES", ctr.Env); ok && env != "void" {
        injectGPU = injectGPUTypeMTGPU
    }
case findEnvWithName("NVIDIA_REQUIRE_CUDA", ctr.Env) && findEnvWithName("CUDA_VERSION", ctr.Env):
    injectGPU = injectGPUTypeGPU
    visibleAllDevice = true
}
```

**枚举新增（不改名）**：在 `cmd/device-injector/main.go:28-32` 的现有枚举末尾**追加** `injectGPUTypeMTGPU`，**保留** `injectGPUTypevGPU` 作为 NVIDIA vGPU 切分场景的预留扩展点：

```go
const (
    injectGPUTypeNone InjectGPUType = iota
    injectGPUTypevGPU       // NVIDIA vGPU（HAMi 切分），仍预留
    injectGPUTypeGPU
    injectGPUTypeMTGPU      // 【新增】MThreads GPU
)
```

#### 注入清单按类型分发

```go
type injectionSpec struct {
    libraryNames []string  // 要伪装的 so 文件名
    cmdNames     []string  // 要替换的命令名（受 overrideCommand 控制，与现状一致）
    envVars      map[string]string  // 要注入的 env
}

specs := map[InjectGPUType]injectionSpec{
    injectGPUTypeGPU: {
        libraryNames: []string{"libcuda.so.1", "libnvidia-ml.so.1", "libcudart.so"},
        cmdNames:     []string{"nvidia-smi"},
    },
    injectGPUTypeMTGPU: {
        libraryNames: []string{"libmtml.so.1", "libmusa.so", "libmusart.so"},
        cmdNames:     []string{"mthreads-gmi"},
    },
}
```

> **同一个 `libfakegpu.so` 文件 bind-mount 到 6 种不同名字**，符号查找按调用方需要命中对应 API。这是 ELF 动态链接的天然特性，无需多个 so。

### 4.7 假命令行工具

#### `cmd/mthreads-gmi/main.go`（新增）

参照 `cmd/nvidia-smi/main.go` 的 yaml→格式化输出模式，但顶层 key 用 `mthreads:`，输出格式参考真实 `mthreads-gmi` 的表格：

```
+-----------------------------------------------------------------------------+
| MTML 2.6.0          Driver Version: 2.6.0           MUSA Version: 3.1.0     |
+-------------------------------+----------------------+----------------------+
| Index  Name           Bus-Id  | Memory-Usage         | GPU-Util  Power      |
+-------------------------------+----------------------+----------------------+
|     0  MTT S4000  0000:01:00.0|     0MiB / 49152MiB  |      0%   50W / 350W |
+-------------------------------+----------------------+----------------------+
```

### 4.8 entrypoint.sh 扩展

```bash
# 现有：创建 /dev/nvidiaN（注意现有代码 `seq 0 $gpu_num` 有 off-by-one，
# 会多创建 1 个节点；新代码顺手用 (mtgpu_num - 1) 修正）
mtgpu_num=$(grep -c 'musa_version' /fake-gpu/fake-gpu.yaml)
if [ "$mtgpu_num" -gt 0 ]; then
  for i in $(seq 0 $((mtgpu_num - 1))); do
    mknod /host-dev/mtgpu$i c 234 $i   # major=234 仅占位，P2 阶段对齐 HAMi
                                       # pkg/device/mthreads/ 中的 device major
    chmod 666 /host-dev/mtgpu$i
  done
  mknod /host-dev/mtgpu_uvm c 235 0
  chmod 666 /host-dev/mtgpu_uvm
fi
```

cleanup 段同样需要按 `mtgpu_num` 删除上述设备节点。

### 4.9 Helm chart 改动

`values.yaml` 新增开关：

```yaml
vendor:
  nvidia:
    enabled: true   # 兼容旧版本，默认开
  mthreads:
    enabled: false  # 默认关，避免对纯 NVIDIA 用户造成噪音
```

`templates/daemonset.yaml` 按开关条件挂载/注入；`configmap.yaml` 把多厂商 yaml 合并下发。

---

## 5. 实施路线图

| 阶段 | 范围 | 验证标准 | 工作量估计 |
|------|------|---------|-----------|
| **P0：脚手架** | 建 `src/mtml/`、骨架 hook 函数（仅返回 SUCCESS/NOT_SUPPORTED）；`common.h` 增加 `mthreads:` 解析；CMake 验证编译通过 | `make build` 通过；`libfakegpu.so` `nm` 能看到 `mtmlLibraryInit` 等符号 | 0.5 天 |
| **P1：MTML 查询路径打通** | 实现 ~15 个高频 `mtmlDeviceGet*`；写 `cmd/mthreads-gmi`；entrypoint 创建 `/dev/mtgpu*` | 容器内执行 `mthreads-gmi` 输出与 yaml 一致 | 1.5 天 |
| **P2：device-injector 多厂商分发** | 触发条件 + 注入清单按类型分发；Helm 加 `vendor.mthreads.enabled` | 同一节点：装 HAMi → 同时拿 NVIDIA 测试 Pod 和 MThreads 测试 Pod，两边 `nvidia-smi` / `mthreads-gmi` 都能跑 | 1 天 |
| **P3：MUSA Runtime 骨架** | 实现 ~20 个最常调用的 `musa*`（GetDevice / GetDeviceCount / SetDevice / GetDeviceProperties / MemGetInfo），其余返回错误 | PyTorch musa 后端做 device 检测时不 crash | 2 天 |
| **P4：完善 + 文档** | Dockerfile 加 COPY mthreads-gmi、README 增 MThreads 用法章节、加 1~2 个 Go 单元测试覆盖 device-injector 互斥分支 | E2E 测试通过 | 1 天 |

**总计约 6 个工作日**，可独立 PR 提交，P0+P1 即能拿到"`mthreads-gmi` 在容器里跑通"的可演示成果。

> P4 **不包含** mt-exporter / DCGM 等价物（决策 #3 暂不支持）。

---

## 6. 风险与开放问题

### 6.1 风险

| 风险 | 缓解措施 |
|------|---------|
| **缺少公开 MTML/MUSA 头文件** — 摩尔线程开发者门户需注册，许可证条款需确认能否分发其头文件子集 | 不分发原始头文件，**自行声明所需的最小类型/枚举子集**（参照 `nvml_subset.h` 的做法），仅使用函数签名 |
| **真实 `mthreads-gmi` 表格格式变化** | 在 `cmd/mthreads-gmi` 里把表格模板提到模板字符串，可配置；E2E 测试只断言关键字段值 |
| **设备节点 major number 错误**导致真应用走 ioctl 卡死 | P0 阶段不创建 `/dev/mtgpu*`，先纯软件 hook；P2 阶段对齐 HAMi 文档中的 device cgroup whitelist |
| **同 so 同时被加载为 libcuda.so.1 和 libmusa.so** 在某些进程内出现符号污染 | 利用 `__attribute__((visibility("hidden")))`（CMake 已设默认 hidden）+ `HOOK_DECL_EXPORT` 显式导出列表，确保 nvml 私有 helper 不会被 musa 路径误调；增加单元测试加载两次验证 |
| **HAMi 注入 env 名称变更** | env 名做配置项（`-mthreads-trigger-env` flag），不写死 |

### 6.2 已决策事项（2026-05-16）

| # | 问题 | 决策 | 落实位置 |
|---|------|------|---------|
| 1 | `musaMalloc` 等计算类 API 行为 | **返回错误**，不做"假成功"模式（与现有 cudart 风格一致） | §4.4 全部计算类 API 表 |
| 2 | 是否拆 so | **不拆**，维持单一 `libfakegpu.so` | §4.1 |
| 3 | DCGM 等价物 / mt-exporter | **不支持**，yaml 不增加 `mtml:` 节点 | §4.3 dcgm 字段对 MThreads 改为可选；§5 P4 不含 mt-exporter |
| 4 | 同容器声明两家 GPU | **互斥**，device-injector 检测到双重声明则拒绝注入并 warn | §4.6 互斥校验代码 |

### 6.3 P0 阶段待实地验证

下列项不影响整体设计，但 P0 实施时**必须**先查证真实头文件/上游代码再下笔：

1. **MUSA Driver vs Runtime API 前缀**：本文假设 Driver=`mu*`、Runtime=`musa*`，需读 `musa.h` / `musa_runtime.h` 实地确认
2. **`mtmlLibraryShutDown` 语义**：参照现有 `nvmlShutdown` 实现风格统一
3. **HAMi 中 MThreads 资源名**：本文写 `mthreads.com/vgpu`，需 `kubectl describe node` 实地核对
4. **MThreads trigger env 变量名**：本文用 `MUSA_VISIBLE_DEVICES`，建议读 HAMi `pkg/device/mthreads/` 实际注入逻辑确认
5. **`/dev/mtgpu*` 设备 major number**：本文用占位 234/235，需对齐 HAMi 文档或真实驱动

---

## 7. 验收用例

### 7.0 双覆盖原则（重要）

本设计在主项目内合并实施（非旁路），因此每次 PR 必须**同时跑两条线的验收**：

| 测试线 | 关注点 | 失败说明 |
|--------|--------|---------|
| **A. NVIDIA 回归** | 现有功能完全不受影响 | 设计有缺陷或实施破坏了既有路径 |
| **B. MThreads 新功能** | 新增 mtml/musa/musart 行为符合预期 | 实施未完成 |
| **C. 互斥与共存** | 同节点跑两类 Pod 不互相干扰 | 隔离设计有问题 |

**最小测试矩阵：**

| # | 场景 | 类型 | 期望 |
|---|------|:----:|------|
| T1 | 现有 NVIDIA Pod（`NVIDIA_VISIBLE_DEVICES=all`）跑 `nvidia-smi` | A | 输出与改动前**逐字节一致** |
| T2 | 现有 `nvidia:` yaml（含全部字段）解析 | A | 无报错，所有字段值不变 |
| T3 | 仅含 `nvidia:` 的 yaml（无 `mthreads:` key） | A | `init_mthreads` 不被触发，无 warn |
| T4 | NVIDIA-only Pod 在 device-injector 互斥校验后行为 | A | 注入 libfakegpu.so 三个文件名，与改动前一致 |
| T5 | MThreads Pod（`MUSA_VISIBLE_DEVICES=all`）跑 `mthreads-gmi` | B | 输出 yaml 中配置的 MTT 设备 |
| T6 | 容器内 `dlopen("libmtml.so.1")` | B | 成功 |
| T7 | 同时设两个 env 的 Pod | C | injector 拒绝注入 + warn 日志 |
| T8 | 同节点先后部署 NVIDIA Pod + MThreads Pod | C | 两个 Pod 各自的 smi/gmi 命令都正确 |
| T9 | device-injector 单元测试覆盖 switch 三个分支（nvidia / musa / 互斥） | A+B+C | 三个分支全部通过 |

### 7.1 最小用例（不依赖 HAMi，仅验 fake-gpu 自身）

直接在 Pod env 中指定 `MUSA_VISIBLE_DEVICES`，绕开 device plugin 路径，专测 device-injector + libfakegpu.so：

```yaml
# test/mthreads-min-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: fake-mthreads-min
spec:
  containers:
  - name: app
    image: ubuntu:22.04
    command: ["sleep", "infinity"]
    env:
    - name: MUSA_VISIBLE_DEVICES
      value: "all"
```

```shell
kubectl exec -it fake-mthreads-min -- mthreads-gmi
# 期望：输出 yaml 中配置的 MTT S4000 表格

kubectl exec -it fake-mthreads-min -- bash -c '
  cat > /tmp/t.c << "EOF"
#include <dlfcn.h>
#include <stdio.h>
int main(){ void* h=dlopen("libmtml.so.1",RTLD_NOW);
  printf("%s\n", h?"OK":dlerror()); return 0; }
EOF
  apt-get update -qq && apt-get install -y gcc -qq >/dev/null
  gcc /tmp/t.c -o /tmp/t -ldl && /tmp/t'
# 期望：输出 "OK"
```

### 7.2 互斥校验用例（验决策 #4）

```yaml
# test/mthreads-conflict-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: fake-conflict
spec:
  containers:
  - name: app
    image: ubuntu:22.04
    command: ["sleep", "infinity"]
    env:
    - name: NVIDIA_VISIBLE_DEVICES
      value: "all"
    - name: MUSA_VISIBLE_DEVICES
      value: "all"
```

```shell
kubectl logs -n kube-system -l app.kubernetes.io/name=fake-gpu | grep "refuses to inject"
# 期望：能看到 device-injector 拒绝注入的 warn 日志
kubectl exec fake-conflict -- ls /usr/lib/libmtml.so.1 2>&1 | grep "No such"
# 期望：没被注入，文件不存在
```

### 7.3 完整 E2E（依赖 HAMi）

```yaml
# test/mthreads-hami-pod.yaml — 走标准 device plugin 路径
apiVersion: v1
kind: Pod
metadata:
  name: fake-mthreads-hami
spec:
  containers:
  - name: app
    image: ubuntu:22.04
    command: ["sleep", "infinity"]
    resources:
      limits:
        mthreads.com/vgpu: 1   # 注：实际资源名以 HAMi 当前版本为准（见 §6.3 #3）
```

### 7.4 NVIDIA 回归用例（必跑）

每次 PR 实施完毕后，在合并前必须验证现有 NVIDIA 路径未受影响：

```yaml
# test/nvidia-regression-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: fake-nvidia-regression
spec:
  containers:
  - name: app
    image: ubuntu:22.04
    command: ["sleep", "infinity"]
    env:
    - name: NVIDIA_VISIBLE_DEVICES
      value: "all"
```

```shell
# 跑出来的 nvidia-smi 输出，应该与 main 分支的输出逐字节一致
kubectl exec fake-nvidia-regression -- nvidia-smi > new.txt
diff main-baseline.txt new.txt
# 期望：无差异

# 验证 yaml 解析对 nvidia 段未改变行为
kubectl logs -n kube-system -l app.kubernetes.io/name=fake-gpu | grep "Number of NVIDIA GPUs"
# 期望：数字与 conf/fake-gpu.yaml 中 nvidia: 节点数一致

# 验证 dlopen 三个原始库名仍可用
kubectl exec fake-nvidia-regression -- bash -c '
  ls -la /usr/lib/x86_64-linux-gnu/libcuda.so.1 \
         /usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1 \
         /usr/lib/x86_64-linux-gnu/libcudart.so'
# 期望：三个文件都存在并指向 libfakegpu.so
```

---

## 8. 与上游 HAMi 的协作

HAMi 已经在 `pkg/device/mthreads/` 处理了 MThreads 调度和资源上报。**fake-gpu 不重复造轮子**，只补 HAMi 的下游空白：

```
┌─────────────────────────────────────────────────────────┐
│  HAMi: 上报 mthreads.com/vgpu 资源，调度，env 注入       │
│         MUSA_VISIBLE_DEVICES=MTGPU-0,MTGPU-1            │
└──────────────────────────┬──────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│  fake-gpu(本设计): NRI 捕获 env → 注入伪装库 / 命令     │
│                    → 应用看到"假设备"能查询             │
└─────────────────────────────────────────────────────────┘
```

**对接点**：定期跟踪 HAMi 中 MThreads env 变量名变化，保持 trigger env 同步。

---

## 9. 配套改动清单（实施时 checklist）

下列项分散在前文，汇总在此方便实施时对照：

| 文件 / 位置 | 改动类型 | 备注 |
|------------|---------|------|
| `src/mtml/{mtml_subset.h, mtml_hook.cpp, export_table.h}` | 新增 | P0/P1 |
| `src/musa/{musa_subset.h, musa_hook.cpp}` | 新增 | P3 |
| `src/musart/{musart_subset.h, musart_hook.cpp}` | 新增 | P3 |
| `cmd/mthreads-gmi/main.go` | 新增 | P1 |
| `src/common/common.h` | 改：`operator>>(GPU)` 4 字段改可选；新增 `MTGPUList` extern | P0 |
| `conf/fake-gpu.yaml` | 改：新增 `mthreads:` 顶层 key（仅示例数据，可空） | P0 |
| `cmd/device-injector/main.go` | 改：新增 enum、互斥校验、injectionSpec 分发 | P2 |
| `entrypoint.sh` | 改：新增 mtgpu 设备节点创建/清理（用修正后的 seq） | P2 |
| `Dockerfile` | 改：`COPY` mthreads-gmi 二进制；ENTRYPOINT 不变 | P1/P2 |
| `Makefile` | 改：`build-cmd` 增加 `mthreads-gmi` target | P1 |
| `charts/fake-gpu/values.yaml` | 改：新增 `vendor.{nvidia,mthreads}.enabled` 开关 | P2 |
| `charts/fake-gpu/templates/daemonset.yaml` | 改：条件挂载 | P2 |
| `README.md` | 改：新增 "MThreads support" 章节 | P4 |
| Go 单元测试 | 新增：`cmd/device-injector/*_test.go` 覆盖 injectMounts 三个分支（NVIDIA / MUSA / 互斥）| P4 |
| `test/` E2E 用例 | 新增：`nvidia-regression-pod.yaml`、`mthreads-min-pod.yaml`、`mthreads-conflict-pod.yaml`（见 §7） | P4 |

---

## 10. 总结

本设计在 fake-gpu 现有"hook + NRI 注入"架构上做**水平扩展**而非重构：

- **3 个新目录**（`src/mtml/`、`src/musa/`、`src/musart/`）+ **1 个新命令**（`cmd/mthreads-gmi`）
- **device-injector 一处分支 + 互斥校验**（按 env 走不同注入清单）
- **配置文件 1 个新顶层 key**（`mthreads:`），现有 `operator>>(GPU)` 4 字段改可选
- **CMake / Dockerfile / Helm chart 各动 ≤10 行**

4 项关键决策均已锁定（§6.2），剩余 5 个待实地验证项（§6.3）不阻塞设计，留 P0 阶段处理。

风险可控，路径清晰，**6 个工作日**内可交付第一个可演示版本（P0+P1）。
