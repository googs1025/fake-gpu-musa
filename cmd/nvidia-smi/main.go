// nvidia-smi —— NVIDIA nvidia-smi 的克隆 CLI。
//
// 二进制只做 cobra 入口,所有子命令和表格渲染都在 pkg/nvidia 里。运行时通过
// NVML(libnvidia-ml.so.1)取设备信息 —— 在容器里这条库的实体已经被 NRI
// bind-mount 成 fake-gpu 的 libfakegpu.so,数据从 FAKE_GPU_CONFIG yaml 来。
package main

import (
	"fmt"
	"os"

	"github.com/chaunceyjiang/fake-gpu/pkg/nvidia"
)

func main() {
	if err := nvidia.RootCmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
