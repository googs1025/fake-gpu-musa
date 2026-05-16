# MUSA / Moore Threads support

fake-gpu can fake **Moore Threads MTT GPUs** alongside (or instead of)
NVIDIA GPUs. The MUSA Driver (`libmusa.so`), MUSA Runtime
(`libmusart.so`), and MTML (`libmtml.so`) symbols all live in the same
`libfakegpu.so` produced by `make build`; device-injector bind-mounts
that single SO under each vendor's expected filenames.

## What this gives you

- A drop-in `libmusa.so` / `libmusart.so` / `libmtml.so` that satisfies
  `dlopen` and enumeration / introspection calls.
- A `mthreads-gmi` binary that renders an mthreads-gmi-style table
  populated from `conf/fake-musa.yaml`.
- A `MUSA_VISIBLE_DEVICES` knob that mirrors `NVIDIA_VISIBLE_DEVICES`
  for filtering which fake GPUs a container sees.

## What this deliberately does NOT do

- Real MUSA compute. Any kernel-launch / memcpy / runtime-state API is
  stubbed to return a MUSA error code (`musaErrorNoDevice`,
  `MU_ERROR_NOT_INITIALIZED`, etc.) — the chosen path is **fail loudly
  rather than fake the result**.
- MPC (MUSA Per-Container) virtualization.
- A DCGM-Exporter equivalent. Metrics collection has no MTML-side
  counterpart yet.

## Requirements

- containerd ≥ 1.7 with NRI enabled (same as the NVIDIA path).
- The container expecting MUSA must declare `MUSA_VISIBLE_DEVICES`. A
  literal `all`, a comma-separated UUID list, or `void` (skip) is
  accepted.

## Switching vendor at install time

The Helm chart exposes a `vendor` value:

```bash
# fake only NVIDIA (default, behaviour unchanged)
helm install fake-gpu charts/fake-gpu --set vendor=nvidia

# fake only MTT
helm install fake-gpu charts/fake-gpu --set vendor=musa

# install the injector for both — still mutually exclusive per container
helm install fake-gpu charts/fake-gpu --set vendor=both
```

Under `vendor=both`, the **device-injector refuses to inject** any
container that declares both `NVIDIA_VISIBLE_DEVICES` and
`MUSA_VISIBLE_DEVICES`. Upper-layer schedulers (HAMi, the vendor
device-plugin, scheduler webhooks) are expected to keep each Pod on a
single heterogeneous resource.

## Configuring the fake MTT GPU list

Edit `conf/fake-musa.yaml`. The schema mirrors `conf/fake-gpu.yaml`
plus an `mtlink` block:

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

The hook reads the file pointed to by `FAKE_MUSA_CONFIG` (set by
device-injector). `FAKE_MUSA_SUFFIX` appends a per-node suffix to each
UUID, useful when a single chart deploys to multiple nodes.

## Trying it locally

```bash
make build BUILD_TYPE=Release
FAKE_MUSA_CONFIG=$PWD/conf/fake-musa.yaml \
  ./output/bin/mthreads-gmi --libmtml=$PWD/output/lib64/libfakegpu.so
```

Expected: one row showing `MTT S80`, `MTGPU-0`, `0 MiB / 16384 MiB`,
`0%`.

## Symbol sourcing

- `src/common/mtml_2.2.0.h` is vendored from
  [`MooreThreads/mthreads-ml-py`](https://github.com/MooreThreads/mthreads-ml-py).
- The MUSA Driver (`mu*`) and MUSA Runtime (`musa*`) symbol lists were
  extracted with `nm -D` from MUSA SDK 3.1.0 and trimmed to the 20+20
  enumeration / attribute / memory-query subset used by typical
  introspection callers. The 18 symbols implemented in
  [ollama PR #7554](https://github.com/ollama/ollama/pull/7554) are a
  subset.

## Known limitations and risks

1. **Opaque handle ABI.** `MtmlDevice` is encoded as `intptr_t index+1`.
   This breaks if a real MTML library is loaded into the same address
   space — fine inside a fake-gpu bind-mount sandbox where it never
   coexists.
2. **Brand enum.** `MtmlBrandType` only documents `MTML_BRAND_MTT=0`;
   consumers comparing against other constants may need to extend the
   stub.
3. **mthreads-gmi visual parity.** The rendered table is an
   approximation, not a byte-for-byte clone of the real tool.
4. **Mutex refusal observability.** When the injector skips a container
   for declaring both vendors, only a warning is logged. Surfacing a
   counter or refusing the Pod admission is a follow-up.
