// mthreads-gmi —— Moore Threads mt-smi 的克隆 CLI。
//
// 二进制本身只是 cobra 入口,实际表格渲染、子命令、设备字段填充全在
// pkg/mthreads。运行时它会通过 --libmtml 或默认路径 dlopen libfakegpu.so,
// 然后用 MTML API 拉数据 —— 跟真实 mthreads-gmi 调真实 libmtml.so 是一样的链路,
// 唯一区别是被 dlopen 的库被 NRI bind-mount 换成了 fake-gpu 的 stub。
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
