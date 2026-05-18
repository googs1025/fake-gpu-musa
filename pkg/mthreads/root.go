package mthreads

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/common"
	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/mtml"
)

// Fake display constants. Real mthreads-gmi sources these from kernel/PCI
// state we don't model; pin reasonable defaults so the table looks right.
const (
	gmiVersion    = "1.14.0"
	deviceType    = "Physical"
	pcieLaneWidth = "16x(16x)"
	eccMode       = "N/A"
)

var libPath string

func init() {
	RootCmd.PersistentFlags().StringVar(&libPath, "libmtml", "libmtml.so",
		"path to libmtml.so (or libfakegpu.so when running locally)")
}

var RootCmd = &cobra.Command{
	Use:   "mthreads-gmi",
	Short: "mthreads-gmi is a fake equivalent of Moore Threads' mthreads-gmi tool",
	Long:  `mthreads-gmi reads MTML through the fake-gpu hook and renders a mthreads-gmi-like table.`,
	Run: func(_ *cobra.Command, _ []string) {
		if err := run(); err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
	},
}

func run() error {
	if err := mtml.Load(libPath); err != nil {
		return err
	}
	lib, err := mtml.Init()
	if err != nil {
		return err
	}
	defer lib.Shutdown()

	sys, err := lib.System()
	if err != nil {
		return err
	}
	defer sys.Free()
	driver, err := sys.DriverVersion()
	if err != nil {
		return err
	}

	count, err := lib.DeviceCount()
	if err != nil {
		return err
	}

	gpus := make([]common.GPU, 0, count)
	for i := uint(0); i < count; i++ {
		d, err := lib.Device(i)
		if err != nil {
			return err
		}
		name, _ := d.Name()
		uuid, _ := d.UUID()
		busID, _ := d.BusID()
		total, used, _ := d.Memory()
		util, tempC, _ := d.GPUStats()
		pwr, _ := d.PowerUsage()
		gpus = append(gpus, common.GPU{
			Idx:        i,
			Name:       name,
			UUID:       uuid,
			BusID:      busID,
			TotalMem:   total,
			UsedMem:    used,
			Util:       util,
			TempC:      tempC,
			PowerMW:    pwr,
			MpcCapable: d.MpcCapable(),
		})
		_ = d.Free()
	}

	render(os.Stdout, driver, gpus, collectProcesses(gpus))
	return nil
}

// collectProcesses 为每张 UsedMem>0 的 GPU 合成一条 fake 进程记录。
// 与 NVIDIA 路径对称:PID 取容器约定的 1,Name 现读 /proc/1/cmdline。
// 所有 GPU 都空闲时返回 nil,渲染层据此回退到 "No running processes found"。
func collectProcesses(gpus []common.GPU) []common.Process {
	cmdline := readPid1Cmdline()
	var out []common.Process
	for _, g := range gpus {
		if g.UsedMem == 0 {
			continue
		}
		out = append(out, common.Process{
			GPUIdx:  g.Idx,
			PID:     1,
			Name:    cmdline,
			UsedMem: g.UsedMem,
		})
	}
	return out
}

// readPid1Cmdline 读 /proc/1/cmdline 并把 NUL 分隔符还原成空格。
// 容器内 PID 1 就是工作负载;读不到时退回 "unknown" 而不是 fatal,
// 这样在宿主机直接跑 mthreads-gmi 也不会崩。
func readPid1Cmdline() string {
	data, err := os.ReadFile("/proc/1/cmdline")
	if err != nil {
		return "unknown"
	}
	s := strings.TrimRight(string(data), "\x00")
	s = strings.ReplaceAll(s, "\x00", " ")
	if s == "" {
		return "unknown"
	}
	return s
}

// Layout copies the visible structure of the real mthreads-gmi output:
// 3 row groups per GPU, two vertical pipes splitting (id+name) | (pcie) |
// (gpu/mem stats). Widths are sized so total rule == 63 chars.
const (
	colID   = 5  // "0    "
	colName = 15 // "MTT S4000      "
	colPCI  = 20 // "00000000:12:00.0    "
	colGPU  = 6  // "100%  "
	colMem  = 15 // "4MiB(49152MiB) " — pads to 63-char rule width
)

func render(w io.Writer, driver string, gpus []common.GPU, processes []common.Process) {
	rule := strings.Repeat("-", 63)
	plus := "+" + strings.Repeat("-", 61) + "+"

	fmt.Fprintln(w, time.Now().Format(time.ANSIC))
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "    mthreads-gmi:%s          Driver Version:%s\n", gmiVersion, driver)
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "%-*s%-*s|%-*s|%-*s%-*s\n",
		colID, "ID", colName, "Name", colPCI, "PCIe", colGPU, "%GPU", colMem, "Mem")
	fmt.Fprintf(w, "%-*s%-*s|%-*s|%-*s%-*s\n",
		colID, "", colName, "Device Type", colPCI, "Pcie Lane Width", colGPU, "Temp", colMem, "MPC Capable")
	// Third row drops the first pipe so the PCIe column reads as one
	// continuous blank — matches real mthreads-gmi output.
	fmt.Fprintf(w, "%-*s%-*s %-*s|%-*s%-*s\n",
		colID, "", colName, "", colPCI, "", colGPU, "", colMem, "ECC Mode")
	fmt.Fprintln(w, plus)
	for _, g := range gpus {
		mem := fmt.Sprintf("%dMiB(%dMiB)", g.UsedMem/1024/1024, g.TotalMem/1024/1024)
		mpc := "NO"
		if g.MpcCapable {
			mpc = "YES"
		}
		// Real mthreads-gmi quirk: only the first data row leaves the Mem
		// cell unpadded (62-char line). Rows 2 and 3 still pad to colMem
		// so the table's right edge stays flush.
		fmt.Fprintf(w, "%-*d%-*s|%-*s|%-*s%s\n",
			colID, g.Idx, colName, g.Name, colPCI, g.BusID,
			colGPU, fmt.Sprintf("%d%%", g.Util), mem)
		fmt.Fprintf(w, "%-*s%-*s|%-*s|%-*s%-*s\n",
			colID, "", colName, deviceType, colPCI, pcieLaneWidth,
			colGPU, fmt.Sprintf("%dC", g.TempC), colMem, mpc)
		fmt.Fprintf(w, "%-*s%-*s %-*s|%-*s%-*s\n",
			colID, "", colName, "", colPCI, "", colGPU, "", colMem, eccMode)
	}
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	renderProcesses(w, processes)
}

// renderProcesses 渲染表尾 "Processes:" 块。列宽与 render 主表对齐到 63 字符标尺。
// processes 为空时退回 "No running processes found",和真实 mthreads-gmi 一致。
// 进程名超 37 字符截断 + 省略号,避免把列对齐撑坏。
func renderProcesses(w io.Writer, processes []common.Process) {
	rule := strings.Repeat("-", 63)
	plus := "+" + strings.Repeat("-", 61) + "+"

	fmt.Fprintln(w, rule)
	fmt.Fprintln(w, "Processes:")
	fmt.Fprintf(w, "%-*s%-*s%-*s%s\n", colID, "ID", 10, "PID", 37, "Process name", "GPU Memory")
	fmt.Fprintf(w, "%-*s%-*s%-*s%s\n", colID, "", 10, "", 37, "", "     Usage")
	fmt.Fprintln(w, plus)
	if len(processes) == 0 {
		fmt.Fprintln(w, "   No running processes found")
	} else {
		for _, p := range processes {
			name := p.Name
			if len(name) > 37 {
				name = name[:34] + "..."
			}
			fmt.Fprintf(w, "%-*d%-*d%-*s%dMiB\n",
				colID, p.GPUIdx, 10, p.PID, 37, name, p.UsedMem/1024/1024)
		}
	}
	fmt.Fprintln(w, rule)
}
