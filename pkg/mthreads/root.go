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

	render(os.Stdout, driver, gpus)
	return nil
}

// Layout copies the visible structure of the real mthreads-gmi output:
// 3 row groups per GPU, two vertical pipes splitting (id+name) | (pcie) |
// (gpu/mem stats). Widths are sized so total rule == 63 chars.
const (
	colID   = 5  // "0    "
	colName = 15 // "MTT S4000      "
	colPCI  = 20 // "00000000:12:00.0    "
	colGPU  = 6  // "100%  "
)

func render(w io.Writer, driver string, gpus []common.GPU) {
	rule := strings.Repeat("-", 63)
	plus := "+" + strings.Repeat("-", 61) + "+"

	fmt.Fprintln(w, time.Now().Format(time.ANSIC))
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "    mthreads-gmi:%s          Driver Version:%s\n", gmiVersion, driver)
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "%-*s%-*s|%-*s|%-*s%s\n",
		colID, "ID", colName, "Name", colPCI, "PCIe", colGPU, "%GPU", "Mem")
	fmt.Fprintf(w, "%-*s%-*s|%-*s|%-*s%s\n",
		colID, "", colName, "Device Type", colPCI, "Pcie Lane Width", colGPU, "Temp", "MPC Capable")
	// Third row drops the first pipe so the PCIe column reads as one
	// continuous blank — matches real mthreads-gmi output.
	fmt.Fprintf(w, "%-*s%-*s %-*s|%-*s%s\n",
		colID, "", colName, "", colPCI, "", colGPU, "", "ECC Mode")
	fmt.Fprintln(w, plus)
	for _, g := range gpus {
		mem := fmt.Sprintf("%dMiB(%dMiB)", g.UsedMem/1024/1024, g.TotalMem/1024/1024)
		mpc := "NO"
		if g.MpcCapable {
			mpc = "YES"
		}
		fmt.Fprintf(w, "%-*d%-*s|%-*s|%-*s%s\n",
			colID, g.Idx, colName, g.Name, colPCI, g.BusID,
			colGPU, fmt.Sprintf("%d%%", g.Util), mem)
		fmt.Fprintf(w, "%-*s%-*s|%-*s|%-*s%s\n",
			colID, "", colName, deviceType, colPCI, pcieLaneWidth,
			colGPU, fmt.Sprintf("%dC", g.TempC), mpc)
		fmt.Fprintf(w, "%-*s%-*s %-*s|%-*s%s\n",
			colID, "", colName, "", colPCI, "", colGPU, "", eccMode)
	}
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w, "Processes:")
	fmt.Fprintf(w, "%-*s%-*s%-*s%s\n", colID, "ID", 10, "PID", 37, "Process name", "GPU Memory")
	fmt.Fprintf(w, "%-*s%-*s%-*s%s\n", colID, "", 10, "", 37, "", "     Usage")
	fmt.Fprintln(w, plus)
	fmt.Fprintln(w, "   No running processes found")
	fmt.Fprintln(w, rule)
}
