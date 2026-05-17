# fake-gpu

English | [中文](./README.md)

> A testing tool that lets you exercise Moore Threads MUSA and NVIDIA CUDA software stacks, ops tooling, and HAMi scheduling without physical GPUs.

## Overview

`fake-gpu` simulates the full user-space surface of a GPU from a single YAML config and bind-mounts the fake into target containers via containerd NRI. It ships **two peer paths**:

- **Moore Threads (MUSA)**: MUSA Driver / MUSA Runtime / MTML + `mthreads-gmi`
- **NVIDIA**: CUDA Driver / CUDA Runtime / NVML + `nvidia-smi`

Both paths share the same `libfakegpu.so`, switched by bind-mounting the library under different SONAMEs. Typical uses:

- Run `mthreads-gmi` / `nvidia-smi` / DCGM-style monitoring stacks
- Integration-test apps that depend on MUSA or CUDA user-space libraries
- Walk the full HAMi mutator + scheduler + device-plugin chain end-to-end
- Validate schedulers, operators, CRDs, and CI pipelines that touch GPUs

> This is a testing tool. It **does not run real MUSA / CUDA compute kernels**.

## Features

- YAML-driven GPU topology: memory, utilization, power, PCIe, NUMA, MTLink
- **MUSA Driver / Runtime / MTML API** support + `mthreads-gmi`
- **CUDA Driver / Runtime / NVML API** support + `nvidia-smi`
- DCGM-Exporter support
- Bundled `fake-mthreads-device-plugin` that advertises `mthreads.com/vgpu` / `sgpu-core` / `sgpu-memory` to kubelet, standing in for the closed-source MThreads plugin during testing
- Works with [HAMi](https://github.com/Project-HAMi/HAMi): on the MUSA path, the `mthreads.com/gpu-index` annotation flows through to the hook's `MUSA_VISIBLE_DEVICES`
- Zero-touch for application code — pure bind-mount injection

## Requirements

- containerd ≥ 1.7.0 (NRI must be enabled)
- A Kubernetes cluster

## Deploy

### Install the Helm chart

```shell
helm repo add fake-gpu-charts https://chaunceyjiang.github.io/fake-gpu
helm repo update
```

#### Moore Threads (MUSA) mode

```shell
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system \
  --set vendor=musa \
  --set mthreads.devicePlugin.enabled=true
```

#### NVIDIA mode (default)

```shell
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system
```

### Companion device-plugin

Pick one (HAMi recommended):

```shell
# Recommended: HAMi (supports both NVIDIA and MThreads)
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm install hami hami-charts/hami -n kube-system

# Or: NVIDIA official device-plugin (NVIDIA path only)
kubectl create -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.0/deployments/static/nvidia-device-plugin.yml
```

> In production, the MThreads path's `Allocate` is handled by the closed-source device-plugin. In fake environments, `fake-mthreads-device-plugin` replaces it so the HAMi chain stays consistent.

## Usage

### Moore Threads (MUSA)

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

Ready-made topologies under `conf/`, hot-swappable via the ConfigMap:

| Config                          | Topology                                              |
| ------------------------------- | ----------------------------------------------------- |
| `conf/fake-musa.yaml`           | single MTT S80                                        |
| `conf/fake-musa-s80x8.yaml`     | 8× MTT S80, split across two NUMA nodes              |
| `conf/fake-musa-s4000x4.yaml`   | 4× MTT S4000, 48 GiB memory, `mpc_count=4`           |
| `conf/fake-musa-busy.yaml`      | 4× S80 mixed load (idle / light / heavy / saturated) |

See [docs/musa.md](docs/musa.md) for the full walk-through.

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

## Build from source

```shell
make docker-build IMAGE_VERSION=v0.2.0

helm template charts/fake-gpu \
  --set image.repository=chaunceyjiang/fake-gpu \
  --set image.tag=v0.2.0 \
  --set nri.runtime.patchConfig=false > install.yaml

kubectl apply -f install.yaml
```

## Documentation

- [docs/architecture.md](docs/architecture.md) — Overall architecture (Chinese, ASCII diagrams)
- [docs/mthreads-support-design.md](docs/mthreads-support-design.md) — MThreads support design
- [docs/musa.md](docs/musa.md) — MUSA user guide

## Contributing

Issues and PRs are welcome:

1. Fork the repository
2. Create a feature branch off `main`
3. Commit with clear commit messages
4. Push to your fork and open a PR against the upstream

Please make sure your changes pass the existing tests and follow the repo's style.
