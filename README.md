# fake-gpu

## Project Overview

The `fake-gpu` project is designed to simulate GPU information, making it easier to test scenarios where a GPU is not available. This can be particularly useful for development and testing purposes in environments that lack physical GPU hardware.

## Features

- Simulate GPU information through configuration files
- Unable to Perform Genuine CUDA Computation
- Test GPU-related functions without GPU hardware
- Non-intrusive, no need to modify application code
- Supports CUDA Driver, CUDA Runtime, NVML API
- Supports nvidia-smi
- Supports DCGM-Exporter
- Supports Moore Threads MUSA GPUs — MUSA Driver, MUSA Runtime, MTML API, `mthreads-gmi`
- Ships a `fake-mthreads-device-plugin` for the MThreads path so HAMi-style scheduling works without the closed-source plugin
- See [docs/musa.md](docs/musa.md) for the MUSA user guide, [docs/mthreads-support-design.md](docs/mthreads-support-design.md) for the design, and [docs/architecture.md](docs/architecture.md) for the overall architecture (中文)

## Requirements

- containerd >= 1.7.0

## Usage

To use the fake GPU, follow these steps:

You should have a Kubernetes cluster running with containerd as the container runtime.
You should have already deployed [nvidia-device-plugin](https://github.com/NVIDIA/k8s-device-plugin) or [HAMi](https://github.com/Project-HAMi/HAMi).

### Deploy the fake GPU 

1. Download the latest release of the fake GPU.

``` shell
helm repo add fake-gpu-charts https://chaunceyjiang.github.io/fake-gpu
helm repo update
```

2. Deploy the fake GPU to your Kubernetes cluster.
  
``` shell
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system

```

You need to deploy a device plugin to advertise the fake GPU resource to the Kubernetes cluster. You can use either the NVIDIA device plugin `or` HAMi.

Nvidia Device Plugin or HAMi only chooses one of them. recommends using HAMi.

### Option 2: Deploy the [HAMi](https://github.com/Project-HAMi/HAMi) (Recommended)

``` shell
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm install hami hami-charts/hami  -n kube-system

```

### Option 1: Deploy the [nvidia-device-plugin](https://github.com/NVIDIA/k8s-device-plugin)

``` shell
 kubectl create -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.0/deployments/static/nvidia-device-plugin.yml
```

### Enjoy the fake GPU

Configure your application to use the GPU.

``` shell 
cat <<EOF | kubectl apply -f -
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
EOF
```

1. Run your application as you would with a real GPU.

``` shell
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


## Moore Threads (MUSA) Support

`fake-gpu` ships a parallel MThreads path that fakes the MUSA stack the same way the NVIDIA path fakes CUDA. The same `libfakegpu.so` is bind-mounted as `libmusa.so`, `libmusart.so`, and `libmtml.so` inside the container, and a fake `mthreads-gmi` is mounted alongside it.

### Components added for MThreads

- **MUSA / MUSA Runtime / MTML hooks** under `src/musa/`, `src/musart/`, `src/mtml/` — compiled into the same `libfakegpu.so`
- **`mthreads-gmi` CLI** (`cmd/mthreads-gmi`) — output mimics the real tool, including the table layout
- **`fake-mthreads-device-plugin`** (`cmd/fake-mthreads-device-plugin`) — advertises `mthreads.com/vgpu`, `mthreads.com/sgpu-core`, `mthreads.com/sgpu-memory` to kubelet so HAMi can schedule against fake cards
- **`device-injector --vendor=mthreads`** path — reads HAMi's `mthreads.com/gpu-index` annotation and exports `MUSA_VISIBLE_DEVICES` plus the right bind-mounts
- **MUSA YAML configs** under `conf/`:
  - `fake-musa.yaml` — single MTT S80
  - `fake-musa-s80x8.yaml` — 8× S80 across two NUMA nodes
  - `fake-musa-s4000x4.yaml` — 4× MTT S4000 (48 GiB, mpc=4)
  - `fake-musa-busy.yaml` — mixed load (idle / light / heavy / saturated)

### Deploy in MThreads mode

```shell
helm install fake-gpu fake-gpu-charts/fake-gpu -n kube-system \
  --set vendor=mthreads \
  --set mthreads.devicePlugin.enabled=true
```

Use [HAMi](https://github.com/Project-HAMi/HAMi) as the scheduler/mutator. In production the MThreads closed-source device-plugin handles `Allocate`; in fake environments `fake-mthreads-device-plugin` replaces it.

### Use the fake MThreads GPU

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

For multi-card setups, load a different config (e.g. `conf/fake-musa-s80x8.yaml`) into the `fake-gpu` ConfigMap and request multiple cards via `mthreads.com/vgpu: 8` plus a comma-separated `mthreads.com/gpu-index` annotation. See [docs/musa.md](docs/musa.md) for the full walk-through.

## Compilation

To compile the project, follow these steps:

``` shell
make docker-build IMAGE_VERSION=v0.2.0
helm template charts/fake-gpu --set imag.repository=chaunceyjiang/fake-gpu  --set image.tag=v0.2.0 --set nri.runtime.patchConfig=false > install.yaml
kubectl apply -f install.yaml
```

## Contributing

We welcome contributions to the `fake-gpu` project. To contribute, follow these guidelines:

1. Fork the repository.
2. Create a new branch for your feature or bugfix.
3. Make your changes and commit them with descriptive commit messages.
4. Push your changes to your forked repository.
5. Create a pull request to the main repository.

Please ensure your code follows the project's coding standards and includes appropriate tests.
