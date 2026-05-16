package mthreads

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/common"
	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/mtml"
)

var libPath string

func init() {
	RootCmd.PersistentFlags().StringVar(&libPath, "libmtml", "libmtml.so",
		"path to libmtml.so (or libfakegpu.so when running locally)")
}

var RootCmd = &cobra.Command{
	Use:   "mt-smi",
	Short: "mt-smi is a fake equivalent of Moore Threads' mt-smi tool",
	Long:  `mt-smi reads MTML through the fake-gpu hook and renders a mt-smi-like table.`,
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
		total, used, _ := d.Memory()
		util, tempC, _ := d.GPUStats()
		pwr, _ := d.PowerUsage()
		gpus = append(gpus, common.GPU{
			Idx:      i,
			Name:     name,
			UUID:     uuid,
			TotalMem: total,
			UsedMem:  used,
			Util:     util,
			TempC:    tempC,
			PowerMW:  pwr,
		})
		_ = d.Free()
	}

	fmt.Println(time.Now().Format(time.ANSIC))
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetTitle(fmt.Sprintf("MT-SMI (fake)    Driver Version: %s", driver))
	t.AppendHeader(table.Row{"GPU", "Name", "UUID", "Temp", "Pwr", "Memory-Usage", "GPU-Util"})
	for _, g := range gpus {
		t.AppendRow(table.Row{
			strconv.Itoa(int(g.Idx)),
			g.Name,
			g.UUID,
			fmt.Sprintf("%dC", g.TempC),
			fmt.Sprintf("%dW", g.PowerMW/1000),
			fmt.Sprintf("%d MiB / %d MiB", g.UsedMem/1024/1024, g.TotalMem/1024/1024),
			fmt.Sprintf("%d%%", g.Util),
		})
	}
	t.Render()
	return nil
}
