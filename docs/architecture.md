# fake-gpu 项目总览与架构

> 本文档汇总 `fake-gpu` 的项目目标、整体架构、核心组件、数据流与部署方式，作为仓库主入口的中文参考。
> 子专题文档：
> - [MThreads MUSA 支持设计](./mthreads-support-design.md)
> - [MUSA 使用指南](./musa.md)

---

## 1. 项目简介

`fake-gpu` 是一个**用软件伪造 GPU**的工具集，让没有物理 GPU 的环境也能运行依赖 GPU 的应用与运维工具。它做的事情可以一句话概括：

> 把 NVIDIA / Moore Threads 的用户态库（CUDA、CUDA Runtime、NVML、MUSA、MUSA Runtime、MTML）换成同名但只读 YAML 配置的假实现，并通过 containerd NRI 挂进容器。

主要能力：

- 用 YAML 配置文件模拟 GPU 拓扑、显存、利用率、功耗等信息
- 兼容 `nvidia-smi`、`mthreads-gmi`、`DCGM-Exporter` 等运维工具
- 兼容 CUDA Driver / CUDA Runtime / NVML / MUSA / MUSA Runtime / MTML 的 API 调用形态
- **不能跑真正的 CUDA / MUSA 计算**，只伪造接口返回值
- 对应用代码**零侵入**，通过 NRI bind-mount 注入
- 配合 [HAMi](https://github.com/Project-HAMi/HAMi) 或 NVIDIA/Moore Threads 官方 device-plugin 完成调度

适用场景：调度器开发、监控告警链路联调、Operator/CRD 验证、CI 环境批量测试、培训演示。

依赖：`containerd >= 1.7.0`（必须启用 NRI）。

---

## 2. 整体架构

### 2.1 鸟瞰图

```
                    ┌──────────────────────────────────────────────────┐
                    │                Kubernetes 控制面                 │
                    │   apiserver / scheduler / HAMi-mutator&scheduler │
                    └──────────────────────────────────────────────────┘
                                          │
            ┌─────────────────────────────┴─────────────────────────────┐
            │                                                           │
            ▼                                                           ▼
   ┌──────────────────┐                                       ┌────────────────────┐
   │  device-plugin   │   advertise: nvidia.com/gpu           │     用户 Pod       │
   │  (NVIDIA 或      │   或 mthreads.com/vgpu                │  resources.limits  │
   │   fake-mthreads) │                                       │   nvidia.com/gpu   │
   └──────────────────┘                                       │     或 vgpu        │
                                                              └────────────────────┘
                                          │ Pod 落到节点
                                          ▼
   ┌─────────────────────────── 节点（kubelet + containerd）─────────────────────────┐
   │                                                                                │
   │   ┌──────────────────────┐        NRI RunPodSandbox / CreateContainer          │
   │   │   containerd + NRI   │ ─────────────────────────────────────────────┐     │
   │   └──────────────────────┘                                              │     │
   │            ▲                                                            ▼     │
   │            │ 挂载注入          ┌──────────────────────────────────────────────┐│
   │            └─────────────────  │            device-injector (NRI)           ││
   │                                │  - 读取 Pod 的 GPU 资源/annotation         ││
   │                                │  - 决定挂入 NVIDIA 还是 MUSA 路径           ││
   │                                │  - 注入 libfakegpu.so + 配置 + 工具 + env  ││
   │                                └──────────────────────────────────────────────┘│
   │                                                                                │
   │   ┌──────────────────────┐        ConfigMap (fake-gpu.yaml / fake-musa.yaml)  │
   │   │   fake-gpu DaemonSet │  ──→  写到节点 /usr/local/fake-gpu/                │
   │   │  - 携带 libfakegpu.so│       并保留 libfakegpu.so / 工具 / device-injector│
   │   │  - 运行 device-      │                                                    │
   │   │    injector & 工具   │                                                    │
   │   └──────────────────────┘                                                    │
   └────────────────────────────────────────────────────────────────────────────────┘
                                          │
                                          ▼
                       ┌──────────────────────────────────────┐
                       │       用户容器内部（注入后）         │
                       │                                      │
                       │   App ──dlopen──▶ libcuda.so.1       │
                       │                  libcudart.so.X      │ ──┐
                       │                  libnvidia-ml.so.1   │   │ 全部指向
                       │                  libmusa.so          │   │ libfakegpu.so
                       │                  libmusart.so        │   │ （同一 .so 内）
                       │                  libmtml.so          │ ──┘
                       │                                      │
                       │   读取 /etc/fake-gpu/fake-gpu.yaml   │
                       │     或 /etc/fake-gpu/fake-musa.yaml  │
                       └──────────────────────────────────────┘
```

### 2.2 两条独立路径

仓库内并行维护两条仿真路径，**互不影响**：

| 路径    | 厂商             | 配置文件                       | 工具命令         | k8s 资源名               | 典型搭配                     |
| ------- | ---------------- | ------------------------------ | ---------------- | ------------------------ | ---------------------------- |
| NVIDIA  | NVIDIA           | `conf/fake-gpu.yaml`           | `nvidia-smi`     | `nvidia.com/gpu`         | 官方 device-plugin / HAMi    |
| MUSA    | Moore Threads    | `conf/fake-musa*.yaml`         | `mthreads-gmi`   | `mthreads.com/vgpu` 等   | HAMi + fake-mthreads-dp     |

两条路径共享同一个 `libfakegpu.so`：所有 hook 源码（`src/cuda/`、`src/cudart/`、`src/nvml/`、`src/musa/`、`src/musart/`、`src/mtml/`）都编译进同一个共享库，由 device-injector 通过**多次 bind-mount 同一个文件到不同 SONAME** 的方式让容器内的 `dlopen("libcuda.so.1")`、`dlopen("libmtml.so")` 等都返回这个伪造库。

---

## 3. 仓库目录速查

```
fake-gpu/
├── cmd/                        # 所有 Go 可执行入口
│   ├── device-injector/        # NRI 插件，进 DaemonSet 运行
│   ├── fake-mthreads-device-plugin/  # MThreads 路径专用 k8s device-plugin
│   ├── mthreads-gmi/           # CLI 包装，调用 pkg/mthreads
│   └── nvidia-smi/             # CLI 包装，调用 pkg/nvidia
├── pkg/
│   ├── nvidia/                 # nvidia-smi 渲染逻辑
│   └── mthreads/               # mthreads-gmi 渲染逻辑 + MTML cgo 绑定
│       ├── common/             # 跨子包 GPU 结构体
│       └── mtml/               # 通过 dlopen 调 libmtml.so 的 cgo 封装
├── src/                        # C++ hook 实现，编译成 libfakegpu.so
│   ├── common/                 # YAML 解析、log/trace、字符串工具
│   ├── cuda/      cuda_hook.cpp           # 2300+ 行 CUDA Driver API hook
│   ├── cudart/    cudart_hook.cpp         # 1700+ 行 CUDA Runtime API hook
│   ├── nvml/      nvml_hook.cpp           # 2000+ 行 NVML hook
│   ├── musa/      musa_hook.cpp           # MUSA Driver hook
│   ├── musart/    musart_hook.cpp         # MUSA Runtime hook
│   └── mtml/      mtml_hook.cpp           # Moore Threads MTML hook
├── conf/                       # YAML 配置样例
│   ├── fake-gpu.yaml           # 默认 NVIDIA 拓扑
│   ├── fake-musa.yaml          # 单卡 MTT S80
│   ├── fake-musa-s80x8.yaml    # 8 卡 S80 拓扑（跨 NUMA）
│   ├── fake-musa-s4000x4.yaml  # 4 卡 S4000（48 GiB 显存，mpc=4）
│   └── fake-musa-busy.yaml     # 混合负载演示（idle / light / heavy / saturated）
├── charts/fake-gpu/            # Helm chart
│   ├── values.yaml             # 镜像、vendor、device-plugin、NRI 开关
│   └── templates/
│       ├── daemonset.yaml      # DaemonSet：fake-gpu hook + device-injector
│       ├── mthreads-device-plugin.yaml
│       ├── configmap.yaml      # 装载 conf/*.yaml
│       ├── nri-config.yaml
│       └── rbac.yaml
├── CMakeLists.txt              # 把 src/**/*.cpp 编进单个 SHARED 库 fakegpu
├── Dockerfile                  # 多阶段构建：编 .so + Go 二进制 → 运行镜像
├── Makefile                    # build / docker-build / test 入口
├── docs/                       # 设计与使用文档（含本文件）
└── scripts/                    # 辅助脚本
```

---

## 4. 核心组件

### 4.1 `libfakegpu.so` — 用户态库劫持核心

- 由 `src/**/*.cpp` 通过 `CMakeLists.txt` 编成**一个**共享库
- 内含全部厂商 API hook：CUDA Driver / Runtime、NVML、MUSA / Runtime、MTML
- 真名 `libfakegpu.so`，但运行时被 bind-mount 成 `libcuda.so.1`、`libcudart.so.12`、`libnvidia-ml.so.1`、`libmusa.so`、`libmusart.so`、`libmtml.so` 等多种 SONAME
- 启动时读取容器内 `/etc/fake-gpu/fake-gpu.yaml` 或 `fake-musa.yaml`，把节点 GPU 拓扑装进内存
- 支持环境变量过滤可见设备：
  - NVIDIA：`NVIDIA_VISIBLE_DEVICES=<idx|uuid>,...` 或 `=all`
  - MUSA：`MUSA_VISIBLE_DEVICES=<idx|uuid>,...` 或 `=all`
- 通过 `HOOK_BUILD_DEBUG`（CMake 选项）开启 `HOOK_TRACE_PROFILE`，Debug 镜像会打印每个 hook 的 enter/exit 日志

### 4.2 `device-injector` — NRI 注入器（`cmd/device-injector`）

容器创建时被 containerd NRI 框架回调，按 Pod 资源决定要不要做注入、做哪一套：

```
       containerd 收到 CreateContainer
                    │
                    ▼
        NRI 回调 device-injector
                    │
   ┌────────────────┴────────────────┐
   │     按 Pod 资源/annotation       │
   │     与 --vendor 启动参数匹配     │
   └────────────────┬────────────────┘
                    │
       ┌────────────┴────────────┐
       ▼                         ▼
   NVIDIA 路径                MUSA 路径
   - libfakegpu.so bind →     - libfakegpu.so bind →
     libcuda.so.1               libmusa.so / libmusart.so
     libcudart.so.X             libmtml.so
     libnvidia-ml.so.1        - nvidia-smi → mthreads-gmi
   - nvidia-smi 工具          - 注入 MUSA_VISIBLE_DEVICES
   - 注入 NVIDIA_VISIBLE_DEVICES
   - 挂 /etc/fake-gpu/fake-gpu.yaml
                              - 挂 /etc/fake-gpu/fake-musa.yaml
```

关键设计点：

- **不动镜像**：通过 NRI 在 OCI spec 上追加 mount + env，对业务镜像零侵入
- **多 SONAME 同源**：同一份 `libfakegpu.so` 多次 bind-mount，避免维护多个产物
- **HAMi 直通**：MUSA 路径下读取 HAMi 写入的 `mthreads.com/gpu-index` annotation，转成 `MUSA_VISIBLE_DEVICES` 数字索引交给 hook 过滤

### 4.3 `fake-mthreads-device-plugin` — MThreads 设备插件（`cmd/fake-mthreads-device-plugin`）

实现 k8s device-plugin v1beta1 gRPC：

- 向 kubelet 注册三种资源：
  - `mthreads.com/vgpu`（按卡数）
  - `mthreads.com/sgpu-core`（每卡 16 切片）
  - `mthreads.com/sgpu-memory`（每卡 96 个 512 MiB 切片 = 48 GiB）
- `ListAndWatch` 周期上报 fake 设备列表
- `Allocate` 这一步**故意做得很薄**：实际的 vGPU 分配/选卡逻辑由 HAMi 闭源插件接管，本插件只负责让 kubelet 满意
- NVIDIA 路径不使用本插件，直接对接官方 nvidia-device-plugin 或 HAMi

### 4.4 `nvidia-smi` / `mthreads-gmi` — CLI 工具

- Go 写的轻量 CLI（`cmd/nvidia-smi`、`cmd/mthreads-gmi`），仅做参数解析与表格渲染
- 真正的数据来源是同一个 `libfakegpu.so`：`mthreads-gmi` 通过 `pkg/mthreads/mtml` 里的 cgo 绑定 `dlopen("libmtml.so")`，运行时被劫持到 fake 实现
- 渲染输出与官方工具**几乎像素级一致**，包括分隔线宽度、空列对齐等细节

### 4.5 配置文件 `conf/*.yaml`

每张 GPU 是一个 YAML 节点，主要字段：

```yaml
moorethreads:                # 或 nvidia:
  - name: MTT S80
    uuid: MTGPU-0
    driver_version: 2.7.0
    memory:
      total: 17179869184     # 16 GiB
      free:  17179869184
    utilization: {gpu: 0, memory: 0}
    power: {usage: 35000, ...}
    pci:    {bus_id: "0000:00:10.0", ...}
    numa:   {node: 0, cpu_affinity: "0-7"}
    mtlink: {...}
    compute:{capability_major: 2, ...}
```

修改 YAML 即可热替换拓扑——通过 ConfigMap 更新后 DaemonSet 入口会对比 md5 自动同步到节点目录，新建的 Pod 立刻看到新拓扑。

### 4.6 Helm Chart `charts/fake-gpu`

部署单元，包含：

- **DaemonSet `fake-gpu`**：每节点跑一个 Pod，作用是
  1. 把 `libfakegpu.so`、`mthreads-gmi`、`nvidia-smi`、`device-injector` 这些产物落到 `hostPath: /usr/local/fake-gpu/`
  2. 启动 `device-injector` NRI 插件
  3. 同步 ConfigMap 内容
- **ConfigMap**：装载 `fake-gpu.yaml` / `fake-musa.yaml`
- **NRI ConfigMap**：可选自动写 `/etc/nri/conf.d/`
- **fake-mthreads-device-plugin DaemonSet**：仅当 `mthreads.devicePlugin.enabled=true` 时部署
- 通过 `values.yaml` 的 `vendor` 字段（`nvidia` / `musa` / `both`）选路径，`both` 仍按容器维度互斥

---

## 5. 端到端数据流

以创建一个使用 MUSA 的 Pod 为例：

```
1. 用户提交 Pod:
       resources.limits: mthreads.com/vgpu: 1

2. HAMi mutator + scheduler:
       - 选节点 nodeA
       - 选 GPU 索引 = 2
       - 写 annotation: mthreads.com/gpu-index: "2"

3. kubelet (nodeA) 调用 containerd 创建容器

4. containerd 触发 NRI 回调 → device-injector
       - 读 Pod 资源与 annotation
       - 走 MUSA 路径
       - OCI spec 追加 mounts:
            /usr/local/fake-gpu/libfakegpu.so  →  /usr/lib/x86_64-linux-gnu/libmusa.so
            /usr/local/fake-gpu/libfakegpu.so  →  /usr/lib/x86_64-linux-gnu/libmusart.so
            /usr/local/fake-gpu/libfakegpu.so  →  /usr/lib/x86_64-linux-gnu/libmtml.so
            /usr/local/fake-gpu/mthreads-gmi   →  /usr/bin/mthreads-gmi
            /usr/local/fake-gpu/fake-musa.yaml →  /etc/fake-gpu/fake-musa.yaml
       - OCI spec 追加 env:
            MUSA_VISIBLE_DEVICES=2

5. 容器内进程 dlopen("libmusa.so") → 命中 fake 实现
       - 初始化时读取 /etc/fake-gpu/fake-musa.yaml
       - 用 MUSA_VISIBLE_DEVICES 过滤后只暴露 idx=2 的卡
       - 后续所有 API 调用都从内存里编造返回值

6. 用户跑 mthreads-gmi 也命中同一 libfakegpu.so，看到表格化输出
```

NVIDIA 路径流程同构，只是替换为 `nvidia.com/gpu`、`NVIDIA_VISIBLE_DEVICES`、CUDA / NVML SONAME。

---

## 6. 与 HAMi 的协作模式

仓库与 HAMi 之间的分工是经过设计的：

```
+--------------------+        +----------------------+        +-----------------------+
|   HAMi (开源)      |        |  MT 闭源 device-     |        |  fake-gpu             |
|  - webhook mutator |  --→   |  plugin (生产环境)   |  ←--   |  - fake-mthreads-dp   |
|  - scheduler ext   |        |  - 真正的 Allocate   |        |  - libfakegpu.so      |
|                    |        |    & vGPU 切分逻辑   |        |  - device-injector    |
+--------------------+        +----------------------+        +-----------------------+
            │                                                              ▲
            │  在 fake 环境下用 fake-mthreads-dp 替换 MT 闭源 plugin       │
            └──────────────────────────────────────────────────────────────┘
```

- HAMi 上游负责 webhook + scheduler，本仓库**不重复实现**
- 生产环境下 Moore Threads 的闭源 device-plugin 接管 `Allocate`
- 在没有真卡的环境，`fake-mthreads-device-plugin` 顶替闭源 plugin，配合 `libfakegpu.so` 让整条链路自洽
- 见 `docs/mthreads-support-design.md` 的设计章节

---

## 7. 部署与使用速记

### 7.1 Helm 安装

```shell
helm repo add fake-gpu-charts https://chaunceyjiang.github.io/fake-gpu
helm repo update

# NVIDIA 模式（默认）
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system

# MThreads（MUSA）模式
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system \
  --set vendor=musa \
  --set mthreads.devicePlugin.enabled=true
```

device-plugin 二选一：

```shell
# 推荐：HAMi
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm install hami hami-charts/hami -n kube-system

# 或：NVIDIA 官方
kubectl create -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.0/deployments/static/nvidia-device-plugin.yml
```

### 7.2 体验

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: fake-gpu
spec:
  containers:
  - name: fake-gpu
    image: nginx
    resources:
      limits:
        nvidia.com/gpu: 1
```

```shell
kubectl exec -it fake-gpu -- nvidia-smi
```

MUSA 用例参见 [docs/musa.md](./musa.md)。

### 7.3 本地编译

```shell
make docker-build IMAGE_VERSION=v0.2.0

helm template charts/fake-gpu \
  --set image.repository=chaunceyjiang/fake-gpu \
  --set image.tag=v0.2.0 \
  --set nri.runtime.patchConfig=false > install.yaml

kubectl apply -f install.yaml
```

---

## 8. 重要约束与边界

- **不做真实计算**：所有 CUDA / MUSA kernel 调用走 fake，不会真的算东西
- **不支持 GPU 直通校验**：诸如 ECC、固件升级、底层 driver IOCTL 等不在仿真范围
- **温度等少量字段当前为硬编码**（如 `src/mtml/mtml_hook.cpp` 中的 45°C），后续可改为 YAML 驱动
- **依赖 containerd NRI**：runtime 不支持 NRI 时整套机制失效
- **HAMi vGPU 分配在生产由闭源 plugin 完成**：本仓库的 fake plugin 仅替代该闭源组件用于测试

---

## 9. 延伸阅读

- [MThreads 支持设计文档](./mthreads-support-design.md)：包含 HAMi 切分语义、`sgpu-core` / `sgpu-memory` 推导、闭源 plugin 协议梳理
- [MUSA 使用指南](./musa.md)：操作手册视角的 MUSA 部署、注解、常见排错
- 仓库根目录 [`README.md`](../README.md)：英文版的简明使用说明
