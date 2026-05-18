package common

type GPU struct {
	Idx        uint
	Name       string
	UUID       string
	BusID      string
	TotalMem   uint64
	UsedMem    uint64
	Util       uint
	TempC      int
	PowerMW    uint
	MpcCapable bool
}

// Process 在 mthreads-gmi 的 "Processes:" 块里渲染成一行。
// MTML 2.2.0 公开头里没有 GPU compute 进程枚举 API,这里完全在 Go 端伪造:
// 每张 UsedMem>0 的 GPU 合成一条记录,PID 用容器约定的 1,Name 从
// /proc/1/cmdline 读取。跟 pkg/nvidia/common.Process 同形状,渲染逻辑也对称。
type Process struct {
	GPUIdx  uint
	PID     uint32
	Name    string
	UsedMem uint64
}
