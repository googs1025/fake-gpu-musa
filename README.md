# fake-gpu

[English](./README.en.md) | 中文

> 没有物理 GPU 也能跑通 Moore Threads MUSA 和 NVIDIA CUDA 软件栈、运维工具和 HAMi 调度链路的测试工具。

## 项目简介

`fake-gpu` 用一份 YAML 模拟 GPU 的全部用户态接口，通过 containerd NRI 自动注入到目标容器，让没有真卡的环境也能跑通完整的 GPU 链路。它提供两条**对等**的仿真路径：

- **Moore Threads（MUSA）路径**：MUSA Driver / MUSA Runtime / MTML + `mthreads-gmi`
- **NVIDIA 路径**：CUDA Driver / CUDA Runtime / NVML + `nvidia-smi`

两条路径共用同一份 `libfakegpu.so`，通过 bind-mount 到不同的 SONAME 切换形态。典型用途：

- 跑 `mthreads-gmi` / `nvidia-smi` / DCGM 类监控工具
- 联调依赖 MUSA 或 CUDA 用户态库的应用
- 完整走通 HAMi 的 mutator + scheduler + device-plugin 链路
- 调度器、Operator、CRD、CI 流水线的 GPU 相关功能验证

> 这是测试工具，**不会真正执行 MUSA / CUDA 计算 kernel**。

## 主要能力

- 通过 YAML 配置文件描述 GPU 拓扑、显存、利用率、功耗等指标
- 支持 **MUSA Driver / Runtime / MTML API** 与 `mthreads-gmi`
- 支持 **CUDA Driver / Runtime / NVML API** 与 `nvidia-smi`
- 支持 DCGM-Exporter
- 自带 `fake-mthreads-device-plugin`，向 kubelet 注册 `mthreads.com/vgpu` / `sgpu-core` / `sgpu-memory`，可替换 MThreads 闭源 device-plugin 用于测试
- 兼容 [HAMi](https://github.com/Project-HAMi/HAMi)：MUSA 路径下 annotation `mthreads.com/gpu-index` 直通到 hook 的 `MUSA_VISIBLE_DEVICES`
- 对应用代码零侵入，纯 bind-mount 注入

## 依赖

- containerd ≥ 1.7.0（必须启用 NRI）
- Kubernetes 集群

## 部署

### 安装 Helm chart

```shell
helm repo add fake-gpu-charts https://chaunceyjiang.github.io/fake-gpu
helm repo update
```

#### Moore Threads（MUSA）模式

```shell
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system \
  --set vendor=musa \
  --set mthreads.devicePlugin.enabled=true
```

#### NVIDIA 模式（默认）

```shell
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system
```

### 配套 device-plugin

二选一，推荐 HAMi：

```shell
# 推荐：HAMi（同时支持 NVIDIA 与 MThreads）
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm install hami hami-charts/hami -n kube-system

# 或：NVIDIA 官方 device-plugin（仅 NVIDIA 路径）
kubectl create -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.0/deployments/static/nvidia-device-plugin.yml
```

> 生产环境中 MThreads 路径的 `Allocate` 由闭源 device-plugin 负责；在 fake 环境下，`fake-mthreads-device-plugin` 顶替闭源插件，让整条 HAMi 链路自洽。

## 使用示例

### Moore Threads（MUSA）

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: musa-demo
  annotations:
    mthreads.com/gpu-index: "0"
spec:
  containers:
  - name: shell
    image: ubuntu:22.04
    command: ["sleep", "infinity"]
    resources:
      limits:
        mthreads.com/vgpu: 1
```

```shell
kubectl exec -it musa-demo -- mthreads-gmi
Sun May 17 16:33:00 2026
---------------------------------------------------------------
    mthreads-gmi:1.14.0          Driver Version:2.7.0
---------------------------------------------------------------
ID   Name           |PCIe                |%GPU  Mem
     Device Type    |Pcie Lane Width     |Temp  MPC Capable
                    |                    |      ECC Mode
+-------------------------------------------------------------+
0    MTT S80        |0000:00:1F.0        |0%    0MiB(16384MiB)
     Physical       |16x(16x)            |45C   NO
                                         |      N/A
---------------------------------------------------------------
```

`conf/` 下提供了几种现成场景，通过更新 ConfigMap 即可热替换：

| 配置文件                       | 拓扑                                                |
| ------------------------------ | --------------------------------------------------- |
| `conf/fake-musa.yaml`          | 单卡 MTT S80                                        |
| `conf/fake-musa-s80x8.yaml`    | 8× MTT S80，跨两个 NUMA node                        |
| `conf/fake-musa-s4000x4.yaml`  | 4× MTT S4000，48 GiB 显存，`mpc_count=4`           |
| `conf/fake-musa-busy.yaml`     | 4× S80 混合负载（idle / light / heavy / saturated）|

详细用法见 [docs/musa.md](docs/musa.md)。

### NVIDIA

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
+---------------------------------------------------------------------------------------+
| NVIDIA-SMI 470.129.06           Driver Version: 440.33.01          CUDA Version: 12.2 |
+-----------------------------------------+----------------------+----------------------+
| GPU  Name        Persistence-M          | Bus-Id        Disp.A | Volatile Uncorr. ECC |
| Fan  Temp  Perf  Pwr:Usage/Cap          |         Memory-Usage | GPU-Util  Compute M. |
|                                         |                      |               MIG M. |
+-----------------------------------------+----------------------+----------------------+
|   1  NVIDIA Tesla P4                Off |                  Off |                  Off |
| N/A   33C    P8    11W /  70W           |   3200MiB / 15411MiB |       0%     Default |
|                                         |                      |                  N/A |
+-----------------------------------------+----------------------+----------------------+

+---------------------------------------------------------------------------------------+
| Processes:                                                                            |
|  GPU   GI   CI        PID   Type   Process name                            GPU Memory |
|        ID   ID                                                             Usage      |
+---------------------------------------------------------------------------------------+
|    1   N/A  N/A       19       G   /usr/local/nginx                           3200MiB |
+---------------------------------------------------------------------------------------+
```

## 从源码编译

```shell
make docker-build IMAGE_VERSION=v0.2.0

helm template charts/fake-gpu \
  --set image.repository=chaunceyjiang/fake-gpu \
  --set image.tag=v0.2.0 \
  --set nri.runtime.patchConfig=false > install.yaml

kubectl apply -f install.yaml
```

## 文档

- [docs/architecture.md](docs/architecture.md) — 整体架构（中文，含 ASCII 架构图）
- [docs/mthreads-support-design.md](docs/mthreads-support-design.md) — MThreads 支持设计文档
- [docs/musa.md](docs/musa.md) — MUSA 使用指南

## 贡献

欢迎以 issue / PR 的形式参与：

1. Fork 仓库
2. 基于 `main` 切出 feature 分支
3. 提交带有清晰 commit message 的修改
4. 推到自己的 fork 后向上游发起 PR

请确保改动通过现有测试，并遵循仓库的编码风格。
