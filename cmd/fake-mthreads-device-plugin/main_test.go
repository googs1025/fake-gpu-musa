package main

import (
	"context"
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
		devs := devicesForResource(c.resource, cards)
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
	if devs := devicesForResource("not-a-resource", cards); devs != nil {
		t.Errorf("unknown resource should yield nil, got %d devs", len(devs))
	}
}

// TestAllocateReturnsEmpty — load-bearing assertion for the integration
// design: device-plugin Allocate is intentionally empty so the NRI injector
// owns all bind-mounts and env injection (keyed off the HAMi
// mthreads.com/gpu-index annotation). If this regresses we'd start
// double-mounting or fighting NRI.
func TestAllocateReturnsEmpty(t *testing.T) {
	p := &pluginServer{resource: resourceVGPU, cards: 4}
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
