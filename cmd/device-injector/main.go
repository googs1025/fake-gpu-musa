package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

var (
	log             *logrus.Logger
	verbose         bool
	sourceHostPath  string
	confPath        string
	gpusuffix       string
	vendor          string
	mountOption     = []string{"rbind", "ro", "rprivate"}
	overrideCommand = []string{}
)

// One libfakegpu.so masquerades as each of these inside the container.
// Per docs/mthreads-support-design.md the SO stays unified — only the
// bind-mount destination filenames differ between vendors.
var nvidiaLibraryFiles = []string{
	"libcuda.so.1",
	"libnvidia-ml.so.1",
	"libcudart.so",
}

var musaLibraryFiles = []string{
	"libmusa.so",
	"libmusart.so",
	"libmtml.so",
}

// an annotated mount
type mount struct {
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Options     []string `json:"options"`
}

// our injector plugin
type plugin struct {
	stub stub.Stub
}

// CreateContainer handles container creation requests.
func (p *plugin) CreateContainer(_ context.Context, pod *api.PodSandbox, ctr *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	if verbose {
		dump("CreateContainer", "pod", pod, "container", ctr)
	}

	adjust := &api.ContainerAdjustment{}

	if err := injectMounts(pod, ctr, adjust); err != nil {
		return nil, nil, err
	}

	if verbose {
		dump(containerName(pod, ctr), "ContainerAdjustment", adjust)
	}

	return adjust, nil, nil
}

func findEnvWithName(name string, env []string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			return true
		}
	}
	return false
}

func findEnvWithNameAndValue(name string, env []string) (string, bool) {
	for _, e := range env {
		if strings.HasPrefix(e, name+"=") {
			e = strings.TrimPrefix(e, name+"=")
			return e, true
		}
	}
	return "", false
}

// vendorPlan describes one vendor's bind-mount intent: which library
// filenames should be masqueraded by `sourceLib`, which env var carries
// the config path, and which yaml on the host backs that config.
type vendorPlan struct {
	libsToReplace []string
	sourceLib     string
	configEnv     string
	configFile    string
}

func injectMounts(pod *api.PodSandbox, ctr *api.Container, a *api.ContainerAdjustment) error {
	var mounts []mount
	visibleAllDevice := false

	wantNvidia := vendor == "nvidia" || vendor == "both"
	wantMusa := vendor == "musa" || vendor == "both"

	nvRequested := false
	musaRequested := false

	if wantNvidia {
		if env, ok := findEnvWithNameAndValue("NVIDIA_VISIBLE_DEVICES", ctr.Env); ok && env != "void" {
			nvRequested = true
			if env == "all" {
				visibleAllDevice = true
			}
		} else if findEnvWithName("NVIDIA_REQUIRE_CUDA", ctr.Env) &&
			findEnvWithName("CUDA_VERSION", ctr.Env) {
			nvRequested = true
			visibleAllDevice = true
		}
	}
	if wantMusa {
		if env, ok := findEnvWithNameAndValue("MUSA_VISIBLE_DEVICES", ctr.Env); ok && env != "void" {
			musaRequested = true
			if env == "all" {
				visibleAllDevice = true
			}
		}
	}

	// Mutex gate (decision: vendors are mutually exclusive per container).
	// Even with --vendor=both we refuse to inject when one container
	// declares BOTH env vars; the upper scheduler must keep one Pod to
	// one heterogeneous resource.
	if nvRequested && musaRequested {
		log.Warnf("%s: refusing injection — container declares both NVIDIA_VISIBLE_DEVICES and MUSA_VISIBLE_DEVICES; vendors must be mutually exclusive per container",
			containerName(pod, ctr))
		return nil
	}

	var plans []vendorPlan
	if nvRequested {
		plans = append(plans, vendorPlan{
			libsToReplace: nvidiaLibraryFiles,
			sourceLib:     "libfakegpu.so",
			configEnv:     "FAKE_GPU_CONFIG",
			configFile:    "fake-gpu.yaml",
		})
		if verbose {
			log.Infof("%s: injecting NVIDIA GPU...", containerName(pod, ctr))
		}
	}
	if musaRequested {
		plans = append(plans, vendorPlan{
			libsToReplace: musaLibraryFiles,
			sourceLib:     "libfakegpu.so", // 同一个 SO；bind-mount 成多个目的名
			configEnv:     "FAKE_MUSA_CONFIG",
			configFile:    "fake-musa.yaml",
		})
		if verbose {
			log.Infof("%s: injecting MUSA GPU...", containerName(pod, ctr))
		}
	}

	if len(plans) == 0 {
		log.Debugf("%s: no vendor matched", containerName(pod, ctr))
		return nil
	}

	librarySearchPaths := []string{
		"/lib",
		"/usr/lib64",
		"/usr/lib/x86_64-linux-gnu",
		"/usr/lib/aarch64-linux-gnu",
		"/lib64",
		"/lib/x86_64-linux-gnu",
		"/lib/aarch64-linux-gnu",
	}

	for _, p := range plans {
		for _, fn := range p.libsToReplace {
			for _, lp := range librarySearchPaths {
				mounts = append(mounts, mount{
					Source:      fmt.Sprintf("%s/%s", sourceHostPath, p.sourceLib),
					Destination: fmt.Sprintf("%s/%s", lp, fn),
					Type:        "bind",
					Options:     mountOption,
				})
			}
		}
		mounts = append(mounts, mount{
			Source:      fmt.Sprintf("%s/%s", sourceHostPath, p.configFile),
			Destination: fmt.Sprintf("/usr/local/fake-gpu/%s", p.configFile),
			Type:        "bind",
			Options:     mountOption,
		})
	}

	// CLI shims — one host binary may back several command names.
	overrideSourceMap := map[string]string{
		"nvidia-smi": "nvidia-smi",
		"vectorAdd":  "nvidia-smi", // existing pre-image behaviour
		"mt-smi":     "mt-smi",
	}
	for _, command := range overrideCommand {
		src, ok := overrideSourceMap[command]
		if !ok {
			src = command
		}
		mounts = append(mounts, mount{
			Source:      fmt.Sprintf("%s/%s", sourceHostPath, src),
			Destination: "/usr/bin/" + command,
			Type:        "bind",
			Options:     mountOption,
		})
	}

	if len(mounts) == 0 {
		log.Debugf("%s: no mounts annotated...", containerName(pod, ctr))
		return nil
	}

	if verbose {
		dump(containerName(pod, ctr), "annotated mounts", mounts)
	}

	for _, m := range mounts {
		a.AddMount(m.toNRI())
		if !verbose {
			log.Infof("%s: injected mount %q -> %q...", containerName(pod, ctr),
				m.Source, m.Destination)
		}
	}

	for _, p := range plans {
		dest := fmt.Sprintf("/usr/local/fake-gpu/%s", p.configFile)
		a.AddEnv(p.configEnv, dest)
		if !verbose {
			log.Infof("%s: injected env %q -> %q...", containerName(pod, ctr),
				p.configEnv, dest)
		}
	}
	if len(gpusuffix) > 0 {
		a.AddEnv("FAKE_GPU_SUFFIX", gpusuffix)
		a.AddEnv("FAKE_MUSA_SUFFIX", gpusuffix)
		if !verbose && visibleAllDevice {
			log.Infof("%s: injected suffix %q", containerName(pod, ctr), gpusuffix)
		}
	}
	return nil
}

// Convert a device to the NRI API representation.
func (m *mount) toNRI() *api.Mount {
	apiMnt := &api.Mount{
		Source:      m.Source,
		Destination: m.Destination,
		Type:        m.Type,
		Options:     m.Options,
	}
	return apiMnt
}

// Construct a container name for log messages.
func containerName(pod *api.PodSandbox, container *api.Container) string {
	if pod != nil {
		return pod.Name + "/" + container.Name
	}
	return container.Name
}

// Dump one or more objects, with an optional global prefix and per-object tags.
func dump(args ...interface{}) {
	var (
		prefix string
		idx    int
	)

	if len(args)&0x1 == 1 {
		prefix = args[0].(string)
		idx++
	}

	for ; idx < len(args)-1; idx += 2 {
		tag, obj := args[idx], args[idx+1]
		msg, err := yaml.Marshal(obj)
		if err != nil {
			log.Infof("%s: %s: failed to dump object: %v", prefix, tag, err)
			continue
		}

		if prefix != "" {
			log.Infof("%s: %s:", prefix, tag)
			for _, line := range strings.Split(strings.TrimSpace(string(msg)), "\n") {
				log.Infof("%s:    %s", prefix, line)
			}
		} else {
			log.Infof("%s:", tag)
			for _, line := range strings.Split(strings.TrimSpace(string(msg)), "\n") {
				log.Infof("  %s", line)
			}
		}
	}
}

func main() {
	var (
		pluginName string
		pluginIdx  string
		commands   string
		opts       []stub.Option
		err        error
	)

	log = logrus.StandardLogger()
	log.SetFormatter(&logrus.TextFormatter{
		PadLevelText: true,
	})

	flag.StringVar(&pluginName, "name", "", "plugin name to register to NRI")
	flag.StringVar(&pluginIdx, "idx", "", "plugin index to register to NRI")
	flag.BoolVar(&verbose, "verbose", false, "enable (more) verbose logging")
	flag.StringVar(&sourceHostPath, "source-path", "/usr/local/fake-gpu", "source host path for mounts")
	flag.StringVar(&gpusuffix, "gpu-uuid-suffix", "", "gpu uuid suffix for fake gpu")
	flag.StringVar(&confPath, "conf", "", "fake gpu config file path")
	flag.StringVar(&commands, "override-commands", "nvidia-smi,vectorAdd,mt-smi", "Override commands in the container")
	flag.StringVar(&vendor, "vendor", "nvidia", "GPU vendor to fake: nvidia | musa | both")
	flag.Parse()
	overrideCommand = strings.Split(commands, ",")
	if pluginName != "" {
		opts = append(opts, stub.WithPluginName(pluginName))
	}
	if pluginIdx != "" {
		opts = append(opts, stub.WithPluginIdx(pluginIdx))
	}

	p := &plugin{}
	if p.stub, err = stub.New(p, opts...); err != nil {
		log.Fatalf("failed to create plugin stub: %v", err)
	}
	err = p.stub.Run(context.Background())
	if err != nil {
		log.Errorf("plugin exited with error %v", err)
		os.Exit(1)
	}
}
