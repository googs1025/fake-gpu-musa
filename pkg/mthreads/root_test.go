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
	}})

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
	}})
	got := buf.String()
	if !strings.Contains(got, "40C   NO") {
		t.Errorf("expected 'NO' in MPC Capable column, got:\n%s", got)
	}
}
