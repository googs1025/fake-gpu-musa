package mthreads

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chaunceyjiang/fake-gpu/pkg/mthreads/common"
)

// TestRenderSampleOutput pins the column layout of the fake mthreads-gmi
// table. The reference is the real tool's output shape — multi-row groups
// split by two pipes, dashed rules above/below, Processes block at the end.
// We intentionally don't compare time/date (it floats), just structure.
func TestRenderSampleOutput(t *testing.T) {
	var buf bytes.Buffer
	render(&buf, "2.7.0", []common.GPU{{
		Idx:        0,
		Name:       "MTT S4000",
		UUID:       "MTGPU-0",
		BusID:      "00000000:12:00.0",
		TotalMem:   49152 * 1024 * 1024,
		UsedMem:    4 * 1024 * 1024,
		Util:       0,
		TempC:      43,
		PowerMW:    60000,
		MpcCapable: true,
	}}, nil)

	got := buf.String()
	want := []string{
		"mthreads-gmi:1.14.0",
		"Driver Version:2.7.0",
		"ID   Name",
		"PCIe",
		"%GPU",
		"Mem",
		"Device Type",
		"Pcie Lane Width",
		"MPC Capable",
		"ECC Mode",
		"0    MTT S4000",
		"00000000:12:00.0",
		"0%",
		"4MiB(49152MiB)",
		"Physical",
		"16x(16x)",
		"43C",
		"YES",
		"N/A",
		"Processes:",
		"No running processes found",
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, got)
		}
	}
	t.Logf("sample render:\n%s", got)
}

// MpcCapable=false flips the "MPC Capable" cell from YES to NO; pin that
// branch too so the fallback path doesn't silently regress.
func TestRenderMpcNotCapable(t *testing.T) {
	var buf bytes.Buffer
	render(&buf, "2.7.0", []common.GPU{{
		Idx:      0,
		Name:     "MTT S80",
		BusID:    "0000:00:1F.0",
		TotalMem: 16 * 1024 * 1024 * 1024,
		TempC:    40,
	}}, nil)
	got := buf.String()
	if !strings.Contains(got, "40C   NO") {
		t.Errorf("expected 'NO' in MPC Capable column, got:\n%s", got)
	}
}

// TestRenderProcessesRows — 当传入非空 processes 列表时,Processes 块要渲染成
// 表格行,不再是 "No running processes found"。MTML 公开头没有 process API,
// 这条路全在 Go 端伪造,所以渲染层是行为分界点,必须钉住。
func TestRenderProcessesRows(t *testing.T) {
	var buf bytes.Buffer
	gpus := []common.GPU{
		{Idx: 0, Name: "MTT S80", BusID: "0000:00:10.0", TotalMem: 16 * 1024 * 1024 * 1024, UsedMem: 4096 * 1024 * 1024, Util: 25, TempC: 45},
		{Idx: 1, Name: "MTT S80", BusID: "0000:00:11.0", TotalMem: 16 * 1024 * 1024 * 1024, UsedMem: 12288 * 1024 * 1024, Util: 75, TempC: 45},
	}
	processes := []common.Process{
		{GPUIdx: 0, PID: 1, Name: "/app/.venv/bin/python", UsedMem: 4096 * 1024 * 1024},
		{GPUIdx: 1, PID: 1, Name: "/app/.venv/bin/python", UsedMem: 12288 * 1024 * 1024},
	}
	render(&buf, "2.7.0", gpus, processes)
	got := buf.String()
	if strings.Contains(got, "No running processes found") {
		t.Errorf("expected process rows, got fallback message:\n%s", got)
	}
	for _, s := range []string{"/app/.venv/bin/python", "4096MiB", "12288MiB"} {
		if !strings.Contains(got, s) {
			t.Errorf("output missing %q\nfull output:\n%s", s, got)
		}
	}
}

// TestCollectProcessesSkipsIdle — 0 used 的 GPU 不入表,跟真实 mthreads-gmi
// 空闲场景行为一致。这里不真去读 /proc/1/cmdline,只验过滤逻辑。
func TestCollectProcessesSkipsIdle(t *testing.T) {
	gpus := []common.GPU{
		{Idx: 0, UsedMem: 0},
		{Idx: 1, UsedMem: 1024 * 1024 * 1024},
		{Idx: 2, UsedMem: 0},
	}
	got := collectProcesses(gpus)
	if len(got) != 1 {
		t.Fatalf("expected 1 process, got %d", len(got))
	}
	if got[0].GPUIdx != 1 || got[0].PID != 1 || got[0].UsedMem != 1024*1024*1024 {
		t.Errorf("unexpected entry: %+v", got[0])
	}
}
