package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// writeYAML drops a minimal fake-musa.yaml with N moorethreads entries so we
// can exercise countCardsFromYAML without dragging in the full schema.
func writeYAML(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-musa.yaml")
	body := "moorethreads:\n"
	for i := 0; i < n; i++ {
		body += "  - name: MTT-FAKE-" + string(rune('0'+i)) + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// writeYAMLWithMemory drops a fake-musa.yaml where each card has an
// explicit memory.total — used to exercise --memory-from-yaml math.
func writeYAMLWithMemory(t *testing.T, memBytes []uint64) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-musa.yaml")
	body := "moorethreads:\n"
	for i, m := range memBytes {
		body += fmt.Sprintf("  - name: MTT-FAKE-%d\n    memory:\n      total: %d\n", i, m)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// TestCountCardsFromYAML — N is derived from counting top-level moorethreads
// entries (decision: drive capacity from yaml, not a separate flag).
func TestCountCardsFromYAML(t *testing.T) {
	for _, n := range []int{0, 1, 4} {
		path := writeYAML(t, n)
		got, err := countCardsFromYAML(path)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if got != n {
			t.Errorf("n=%d: got %d", n, got)
		}
	}
}

// TestCountCardsFromYAML_RealConfig — the shipped conf/fake-musa.yaml must
// parse and yield at least one card; if this regresses, the chart will
// register a node with zero MThreads capacity.
func TestCountCardsFromYAML_RealConfig(t *testing.T) {
	got, err := countCardsFromYAML("../../conf/fake-musa.yaml")
	if err != nil {
		t.Fatalf("parse shipped config: %v", err)
	}
	if got < 1 {
		t.Errorf("shipped fake-musa.yaml has %d moorethreads entries, want >= 1", got)
	}
}

// TestDevicesForResource — IDs are namespaced per resource and counts follow
// HAMi's coresPerMthreadsGPU=16 / memSlicesPerCard=96 math.
func TestDevicesForResource(t *testing.T) {
	cards := 2
	cap := capacity{cards: cards, memSlices: cards * memSlicesPerCard}
	cases := []struct {
		resource string
		want     int
		prefix   string
	}{
		{resourceVGPU, cards, "MT-FAKE-VGPU-"},
		{resourceSGPUCore, cards * coresPerCard, "MT-FAKE-CORE-"},
		{resourceSGPUMem, cards * memSlicesPerCard, "MT-FAKE-MEM-"},
	}
	for _, c := range cases {
		devs := devicesForResource(c.resource, cap)
		if len(devs) != c.want {
			t.Errorf("%s: got %d devs, want %d", c.resource, len(devs), c.want)
		}
		if len(devs) > 0 && devs[0].Health != pluginapi.Healthy {
			t.Errorf("%s: device 0 not healthy", c.resource)
		}
		if len(devs) > 0 && devs[0].ID[:len(c.prefix)] != c.prefix {
			t.Errorf("%s: device 0 id=%q want prefix %q", c.resource, devs[0].ID, c.prefix)
		}
	}
	if devs := devicesForResource("not-a-resource", cap); devs != nil {
		t.Errorf("unknown resource should yield nil, got %d devs", len(devs))
	}
}

// TestMemSlicesFor exercises both modes of --memory-from-yaml.
//
//   - off (HAMi-compat): always N * 96, ignoring per-card memory.total.
//   - on:               sum(memory.total / 512MiB) per card. A 16 GiB S80
//                       gives 32; a 48 GiB S4000 gives 96. Heterogeneous
//                       node math must add up.
func TestMemSlicesFor(t *testing.T) {
	const gib = uint64(1) << 30
	cases := []struct {
		name     string
		memBytes []uint64
		fromYAML bool
		want     int
	}{
		{"single S80 constant-mode", []uint64{16 * gib}, false, memSlicesPerCard},
		{"single S80 yaml-mode", []uint64{16 * gib}, true, 32},
		{"single S4000 yaml-mode", []uint64{48 * gib}, true, 96},
		{"8x S80 constant-mode", repeat(16*gib, 8), false, 8 * memSlicesPerCard},
		{"8x S80 yaml-mode", repeat(16*gib, 8), true, 8 * 32},
		{"heterogeneous yaml-mode", []uint64{16 * gib, 48 * gib, 16 * gib}, true, 32 + 96 + 32},
		{"zero memory yaml-mode", []uint64{0, 0}, true, 0},
		{"zero memory constant-mode", []uint64{0, 0}, false, 2 * memSlicesPerCard},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cards := make([]cardInfo, len(c.memBytes))
			for i, m := range c.memBytes {
				cards[i] = cardInfo{Name: "MTT", MemoryBytes: m}
			}
			if got := memSlicesFor(cards, c.fromYAML); got != c.want {
				t.Errorf("memSlicesFor(%d cards, fromYAML=%v) = %d, want %d",
					len(cards), c.fromYAML, got, c.want)
			}
		})
	}
}

// TestReadCardsFromYAML round-trips the new memory.total field. Important
// because countCardsFromYAML used to be a single-field parse; if we
// regress on parsing memory the --memory-from-yaml mode silently reports
// zero capacity.
func TestReadCardsFromYAML(t *testing.T) {
	path := writeYAMLWithMemory(t, []uint64{17179869184, 51539607552, 0})
	cards, err := readCardsFromYAML(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []uint64{17179869184, 51539607552, 0}
	if len(cards) != len(want) {
		t.Fatalf("got %d cards, want %d", len(cards), len(want))
	}
	for i, c := range cards {
		if c.MemoryBytes != want[i] {
			t.Errorf("card %d: MemoryBytes=%d, want %d", i, c.MemoryBytes, want[i])
		}
	}
}

func repeat(v uint64, n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// TestAllocateReturnsEmpty — load-bearing assertion for the integration
// design: device-plugin Allocate is intentionally empty so the NRI injector
// owns all bind-mounts and env injection (keyed off the HAMi
// mthreads.com/gpu-index annotation). If this regresses we'd start
// double-mounting or fighting NRI.
func TestAllocateReturnsEmpty(t *testing.T) {
	p := &pluginServer{resource: resourceVGPU, cap: capacity{cards: 4, memSlices: 4 * memSlicesPerCard}}
	req := &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIDs: []string{"MT-FAKE-VGPU-0"}},
			{DevicesIDs: []string{"MT-FAKE-VGPU-1"}},
		},
	}
	resp, err := p.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got, want := len(resp.ContainerResponses), len(req.ContainerRequests); got != want {
		t.Fatalf("response count = %d, want %d", got, want)
	}
	for i, cr := range resp.ContainerResponses {
		if cr == nil {
			t.Errorf("response %d nil", i)
			continue
		}
		if len(cr.Mounts) != 0 || len(cr.Devices) != 0 || len(cr.Envs) != 0 || len(cr.Annotations) != 0 {
			t.Errorf("response %d not empty: mounts=%d devices=%d envs=%d annotations=%d",
				i, len(cr.Mounts), len(cr.Devices), len(cr.Envs), len(cr.Annotations))
		}
	}
}
