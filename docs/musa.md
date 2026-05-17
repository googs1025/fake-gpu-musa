# MUSA / Moore Threads 支持

`fake-gpu` 可以与 NVIDIA 仿真**并存或单独**仿真 **Moore Threads MTT GPU**。MUSA Driver（`libmusa.so`）、MUSA Runtime（`libmusart.so`）、MTML（`libmtml.so`）这三组符号都编译进 `make build` 产出的同一份 `libfakegpu.so`；device-injector 将这唯一一份 `.so` 多次 bind-mount，按各厂商期望的文件名挂入容器。

## 提供的能力

- 一份 drop-in 的 `libmusa.so` / `libmusart.so` / `libmtml.so`，满足 `dlopen` 与各种枚举 / introspection 调用
- `mthreads-gmi` 可执行文件，按 `conf/fake-musa.yaml` 渲染出与 mthreads-gmi 形态一致的表格
- `MUSA_VISIBLE_DEVICES` 开关，与 `NVIDIA_VISIBLE_DEVICES` 行为一致，用于过滤容器可见的伪 GPU

## 刻意**不**提供的能力

- 真正的 MUSA 计算。所有 kernel 启动 / memcpy / runtime 状态相关 API 都会返回 MUSA 错误码（`musaErrorNoDevice`、`MU_ERROR_NOT_INITIALIZED` 等）—— 采取的策略是 **大声失败而不是伪造结果**
- MPC（MUSA Per-Container）虚拟化
- 类似 DCGM-Exporter 的指标采集。MTML 侧暂无对应实现

## 依赖

- containerd ≥ 1.7，启用 NRI（与 NVIDIA 路径一致）
- 使用 MUSA 的容器必须声明 `MUSA_VISIBLE_DEVICES`，取值可为字面量 `all`、逗号分隔的 UUID/索引列表，或 `void`（跳过注入）

## 在安装时切换厂商

Helm chart 提供 `vendor` 字段：

```bash
# 只仿真 NVIDIA（默认，行为不变）
helm install fake-gpu charts/fake-gpu --set vendor=nvidia

# 只仿真 MTT
helm install fake-gpu charts/fake-gpu --set vendor=musa

# 两个 injector 都装上 —— 但容器维度仍互斥
helm install fake-gpu charts/fake-gpu --set vendor=both
```

`vendor=both` 时，**device-injector 会拒绝注入**那些同时声明了 `NVIDIA_VISIBLE_DEVICES` 与 `MUSA_VISIBLE_DEVICES` 的容器。上层调度器（HAMi、各厂商 device-plugin、scheduler webhook）需要保证每个 Pod 只使用一种异构资源。

## 配置伪 MTT GPU 列表

编辑 `conf/fake-musa.yaml`，schema 与 `conf/fake-gpu.yaml` 类似，额外多了 `mtlink` 段：

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
    power:    { minLimit: 50000, maxLimit: 250000, defaultLimit: 250000, enforcedLimit: 250000, usage: 60000 }
    utilization: { gpu: 0, memory: 0 }
    memory:   { total: 17179869184, free: 17179869184 }
    pci:      { bus_id: "0000:00:1F.0", bus: 1, device_id: 0, domain_id: 0, sub_system_id: 0 }
    numa:     { node: 0, cpu_affinity: "0-7" }
    mtlink:   { version: "1.0", capacity: 1, bandwidth: 16, peer_gpu_uuids: [] }
```

Hook 读取的配置文件路径由 `FAKE_MUSA_CONFIG` 决定（device-injector 自动注入）。`FAKE_MUSA_SUFFIX` 会给每个 UUID 追加一段 per-node 后缀，单 chart 部署到多节点时很有用。

`conf/` 还提供了几种现成拓扑：

| 配置文件                       | 拓扑                                                |
| ------------------------------ | --------------------------------------------------- |
| `conf/fake-musa.yaml`          | 单卡 MTT S80                                        |
| `conf/fake-musa-s80x8.yaml`    | 8× MTT S80，跨两个 NUMA node                        |
| `conf/fake-musa-s4000x4.yaml`  | 4× MTT S4000，48 GiB 显存，`mpc_count=4`           |
| `conf/fake-musa-busy.yaml`     | 4× S80 混合负载（idle / light / heavy / saturated）|

ConfigMap 热替换示例：

```bash
kubectl create configmap fake-gpu-musa-configmap -n kube-system \
  --from-file=fake-musa.yaml=conf/fake-musa-s80x8.yaml \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl rollout restart daemonset/fake-gpu -n kube-system
```

## 本地试跑

```bash
make build BUILD_TYPE=Release
FAKE_MUSA_CONFIG=$PWD/conf/fake-musa.yaml \
  ./output/bin/mthreads-gmi --libmtml=$PWD/output/lib64/libfakegpu.so
```

期望输出：一行 `MTT S80`、`MTGPU-0`、`0 MiB / 16384 MiB`、`0%`。

## 符号来源

- `src/common/mtml_2.2.0.h` 来自 [`MooreThreads/mthreads-ml-py`](https://github.com/MooreThreads/mthreads-ml-py)
- MUSA Driver（`mu*`）与 MUSA Runtime（`musa*`）的符号清单是从 MUSA SDK 3.1.0 用 `nm -D` 提取后，裁剪为 introspection 类调用常用的 20+20 个枚举 / 属性 / 内存查询接口。[ollama PR #7554](https://github.com/ollama/ollama/pull/7554) 实现的 18 个符号是其子集

## 已知约束与风险

1. **不透明 handle ABI**：`MtmlDevice` 用 `intptr_t index+1` 编码。如果同一地址空间内同时加载了真实 MTML 库会冲突 —— 在 fake-gpu 的 bind-mount 沙箱里因从不共存所以无碍
2. **品牌枚举**：`MtmlBrandType` 仅定义 `MTML_BRAND_MTT=0`；消费者如要比较其他常量，需扩展 stub
3. **mthreads-gmi 视觉对齐**：渲染表格只是近似，不是与官方工具逐字节一致的克隆
4. **互斥拒绝的可观测性**：injector 因双厂商声明跳过容器时只会打 warning。下一步应该暴露计数指标或在准入层就拒绝 Pod
