package main

import (
	"strings"
	"testing"

	"github.com/containerd/nri/pkg/api"
	"github.com/sirupsen/logrus"
)

// newCtr returns a minimal api.Container fixture; only the env list matters
// to injectMounts.
func newCtr(name string, env ...string) *api.Container {
	return &api.Container{Name: name, Env: env}
}

func newPod() *api.PodSandbox { return &api.PodSandbox{Name: "test-pod"} }

// initTestGlobals primes the package-level globals injectMounts reads from
// without dragging in cobra/flag parsing.
func initTestGlobals(t *testing.T, v string) {
	t.Helper()
	log = logrus.New()
	log.SetOutput(testWriter{t})
	sourceHostPath = "/usr/local/fake-gpu"
	gpusuffix = ""
	verbose = false
	vendor = v
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func countDestPrefix(a *api.ContainerAdjustment, basename string) int {
	n := 0
	for _, m := range a.Mounts {
		if strings.HasSuffix(m.Destination, "/"+basename) {
			n++
		}
	}
	return n
}

func envValue(a *api.ContainerAdjustment, key string) (string, bool) {
	for _, e := range a.Env {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

// TestMutualExclusion: vendor=both, one container with both NVIDIA and
// MUSA env vars must yield zero mounts and zero envs (decision: refuse,
// don't try to mix).
func TestMutualExclusion(t *testing.T) {
	initTestGlobals(t, "both")
	pod := newPod()
	ctr := newCtr("dual",
		"NVIDIA_VISIBLE_DEVICES=all",
		"MUSA_VISIBLE_DEVICES=all",
	)
	adj := &api.ContainerAdjustment{}
	if err := injectMounts(pod, ctr, adj); err != nil {
		t.Fatalf("injectMounts returned error: %v", err)
	}
	if len(adj.Mounts) != 0 {
		t.Fatalf("expected 0 mounts on refusal, got %d", len(adj.Mounts))
	}
	if len(adj.Env) != 0 {
		t.Fatalf("expected 0 env on refusal, got %d", len(adj.Env))
	}
}

// TestVendorBoth: vendor=both with three containers — pure NVIDIA, pure
// MUSA, and no GPU env. Each should land on its own injection plan and
// the third should be a no-op.
func TestVendorBoth(t *testing.T) {
	initTestGlobals(t, "both")
	pod := newPod()
	overrideCommand = nil // no CLI shims so we isolate the vendor library counts

	tests := []struct {
		name        string
		env         []string
		wantNvFiles bool
		wantMuFiles bool
		wantConfig  string // "" if neither expected
	}{
		{"nvidia-only", []string{"NVIDIA_VISIBLE_DEVICES=all"}, true, false, "FAKE_GPU_CONFIG"},
		{"musa-only", []string{"MUSA_VISIBLE_DEVICES=all"}, false, true, "FAKE_MUSA_CONFIG"},
		{"neither", []string{"FOO=bar"}, false, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctr := newCtr(tc.name, tc.env...)
			adj := &api.ContainerAdjustment{}
			if err := injectMounts(pod, ctr, adj); err != nil {
				t.Fatalf("injectMounts: %v", err)
			}

			hasNv := countDestPrefix(adj, "libcuda.so.1") > 0
			hasMu := countDestPrefix(adj, "libmusa.so") > 0
			if hasNv != tc.wantNvFiles {
				t.Errorf("nvidia mounts present=%v want=%v", hasNv, tc.wantNvFiles)
			}
			if hasMu != tc.wantMuFiles {
				t.Errorf("musa mounts present=%v want=%v", hasMu, tc.wantMuFiles)
			}

			if tc.wantConfig == "" {
				if len(adj.Mounts) != 0 || len(adj.Env) != 0 {
					t.Errorf("expected no-op for container without vendor env, got %d mounts / %d env", len(adj.Mounts), len(adj.Env))
				}
				return
			}
			if _, ok := envValue(adj, tc.wantConfig); !ok {
				t.Errorf("expected env %s to be set", tc.wantConfig)
			}
		})
	}
}

// TestNvidiaOnlyVendor: vendor=nvidia must ignore MUSA env even when the
// container declares it (so a chart accidentally landing on a MUSA-only
// node still behaves like the legacy NVIDIA path).
func TestNvidiaOnlyVendor(t *testing.T) {
	initTestGlobals(t, "nvidia")
	pod := newPod()
	overrideCommand = nil
	ctr := newCtr("musa-only", "MUSA_VISIBLE_DEVICES=all")
	adj := &api.ContainerAdjustment{}
	if err := injectMounts(pod, ctr, adj); err != nil {
		t.Fatalf("injectMounts: %v", err)
	}
	if len(adj.Mounts) != 0 {
		t.Fatalf("expected MUSA to be ignored under vendor=nvidia, got %d mounts", len(adj.Mounts))
	}
}
