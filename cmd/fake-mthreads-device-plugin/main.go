// fake-mthreads-device-plugin — k8s device-plugin v1beta1 server that
// registers three MThreads resources (mthreads.com/vgpu, sgpu-core,
// sgpu-memory) so HAMi's scheduler treats this node as having real MTT
// cards. We do NOT have real /dev/mtgpu* devices, so Allocate returns an
// empty response — the actual libfakegpu.so bind-mounts and env injection
// are done downstream by the fake-gpu NRI injector, triggered by the
// `mthreads.com/gpu-index` annotation that HAMi writes onto the Pod.
//
// Device count N is derived from counting entries under `moorethreads:` in
// FAKE_MUSA_CONFIG (default /etc/fake-gpu/fake-musa.yaml). Each "card"
// translates to HAMi's resource math (see pkg/device/mthreads/device.go in
// HAMi): coresPerMthreadsGPU=16, memoryPerMthreadsGPU=96 (== 48 GiB at 512
// MiB per slice).
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	// HAMi constants — pkg/device/mthreads/device.go
	coresPerCard     = 16  // sgpu-core units per physical card
	memSlicesPerCard = 96  // sgpu-memory units per card (each unit = 512 MiB)
	memSliceBytes    = 512 * 1024 * 1024

	resourceVGPU      = "mthreads.com/vgpu"
	resourceSGPUCore  = "mthreads.com/sgpu-core"
	resourceSGPUMem   = "mthreads.com/sgpu-memory"

	// HAMi annotation that contains the comma-separated GPU index list the
	// scheduler picked for this Pod. The NRI injector reads it to decide
	// what MUSA_VISIBLE_DEVICES to set inside the container.
	hamiGPUIndexAnnotation = "mthreads.com/gpu-index"
)

var log = logrus.StandardLogger()

// countCardsFromYAML returns the number of `moorethreads:` entries. We use
// yaml.v3 directly (instead of sigs.k8s.io/yaml) because we only need a
// count, not strict typing — keeps the binary lean.
func countCardsFromYAML(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg struct {
		Moorethreads []struct {
			Name string `yaml:"name"`
		} `yaml:"moorethreads"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return len(cfg.Moorethreads), nil
}

// devicesForResource builds a stable, deterministic device list for a
// resource. IDs are namespaced by resource so kubelet won't conflate the
// three resources' identity spaces.
//
// vgpu:        N entries, IDs "MT-FAKE-0..N-1"
// sgpu-core:   N * coresPerCard entries
// sgpu-memory: N * memSlicesPerCard entries
func devicesForResource(resource string, cards int) []*pluginapi.Device {
	count := 0
	prefix := ""
	switch resource {
	case resourceVGPU:
		count = cards
		prefix = "MT-FAKE-VGPU"
	case resourceSGPUCore:
		count = cards * coresPerCard
		prefix = "MT-FAKE-CORE"
	case resourceSGPUMem:
		count = cards * memSlicesPerCard
		prefix = "MT-FAKE-MEM"
	default:
		return nil
	}
	devs := make([]*pluginapi.Device, 0, count)
	for i := 0; i < count; i++ {
		devs = append(devs, &pluginapi.Device{
			ID:     fmt.Sprintf("%s-%d", prefix, i),
			Health: pluginapi.Healthy,
		})
	}
	return devs
}

// pluginServer implements one device-plugin gRPC service for one resource.
type pluginServer struct {
	resource  string
	socket    string
	cards     int
	server    *grpc.Server
	stop      chan struct{}
}

func newPluginServer(resource string, cards int, socketDir string) *pluginServer {
	// Socket name avoids "/" in the resource name (kubelet conventions).
	base := strings.ReplaceAll(resource, "/", "_")
	return &pluginServer{
		resource: resource,
		cards:    cards,
		socket:   filepath.Join(socketDir, "fake-mthreads-"+base+".sock"),
		stop:     make(chan struct{}),
	}
}

func (p *pluginServer) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired:                false,
		GetPreferredAllocationAvailable: false,
	}, nil
}

func (p *pluginServer) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	devs := devicesForResource(p.resource, p.cards)
	if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: devs}); err != nil {
		return err
	}
	log.Infof("%s: ListAndWatch sent %d devices", p.resource, len(devs))
	// Block until stop. Our fake device list never changes.
	select {
	case <-p.stop:
	case <-stream.Context().Done():
	}
	return nil
}

// Allocate returns empty ContainerAllocateResponses by design — fake-gpu's
// NRI injector handles all bind-mounts and env injection downstream. The
// HAMi scheduler still functions because the assignment decision is
// communicated to the injector via the mthreads.com/gpu-index annotation,
// not via this Allocate response.
func (p *pluginServer) Allocate(_ context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := &pluginapi.AllocateResponse{
		ContainerResponses: make([]*pluginapi.ContainerAllocateResponse, len(req.ContainerRequests)),
	}
	for i := range req.ContainerRequests {
		resp.ContainerResponses[i] = &pluginapi.ContainerAllocateResponse{}
	}
	log.Debugf("%s: Allocate returning %d empty container responses", p.resource, len(req.ContainerRequests))
	return resp, nil
}

func (p *pluginServer) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

func (p *pluginServer) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (p *pluginServer) serve() error {
	_ = os.Remove(p.socket)
	lis, err := net.Listen("unix", p.socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", p.socket, err)
	}
	p.server = grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(p.server, p)
	go func() {
		if err := p.server.Serve(lis); err != nil {
			log.Errorf("%s: grpc serve error: %v", p.resource, err)
		}
	}()
	// Wait for socket to be ready before kubelet registration. A short dial
	// with retry handles the race between Serve() starting and the socket
	// becoming connectable.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := grpc.NewClient("unix://"+p.socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("socket %s never became ready", p.socket)
}

func (p *pluginServer) register(kubeletSocket string) error {
	conn, err := grpc.NewClient("unix://"+kubeletSocket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial kubelet %s: %w", kubeletSocket, err)
	}
	defer conn.Close()
	client := pluginapi.NewRegistrationClient(conn)
	_, err = client.Register(context.Background(), &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     filepath.Base(p.socket),
		ResourceName: p.resource,
	})
	if err != nil {
		return fmt.Errorf("register %s: %w", p.resource, err)
	}
	log.Infof("%s: registered with kubelet", p.resource)
	return nil
}

func (p *pluginServer) shutdown() {
	close(p.stop)
	if p.server != nil {
		p.server.GracefulStop()
	}
	_ = os.Remove(p.socket)
}

func main() {
	var (
		configPath string
		socketDir  string
		kubeletSock string
		verbose    bool
	)
	flag.StringVar(&configPath, "config", "/etc/fake-gpu/fake-musa.yaml",
		"fake-musa.yaml path; entries under moorethreads: determine N cards")
	flag.StringVar(&socketDir, "socket-dir", pluginapi.DevicePluginPath,
		"directory for plugin unix sockets (kubelet's device-plugins dir)")
	flag.StringVar(&kubeletSock, "kubelet-socket", filepath.Join(pluginapi.DevicePluginPath, "kubelet.sock"),
		"kubelet device-plugin registration socket")
	flag.BoolVar(&verbose, "verbose", false, "debug logging")
	flag.Parse()

	log.SetFormatter(&logrus.TextFormatter{PadLevelText: true})
	if verbose {
		log.SetLevel(logrus.DebugLevel)
	}

	cards, err := countCardsFromYAML(configPath)
	if err != nil {
		log.Fatalf("count cards: %v", err)
	}
	if cards == 0 {
		log.Warnf("%s contains 0 moorethreads entries — registering with N=0 (HAMi will see zero capacity)", configPath)
	}
	log.Infof("derived N=%d cards from %s", cards, configPath)

	resources := []string{resourceVGPU, resourceSGPUCore, resourceSGPUMem}
	plugins := make([]*pluginServer, 0, len(resources))
	for _, r := range resources {
		p := newPluginServer(r, cards, socketDir)
		if err := p.serve(); err != nil {
			log.Fatalf("serve %s: %v", r, err)
		}
		if err := p.register(kubeletSock); err != nil {
			log.Fatalf("register %s: %v", r, err)
		}
		plugins = append(plugins, p)
	}

	log.Infof("fake-mthreads-device-plugin ready (cards=%d, resources=%v)", cards, resources)

	// Block until SIGTERM/SIGINT, then GracefulStop each gRPC server and
	// unlink the unix sockets. Kubelet restart will delete our socket
	// anyway and the DaemonSet will restart us — but a clean shutdown
	// keeps the host's device-plugins/ dir tidy on rolling updates.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	log.Info("shutting down")
	for _, p := range plugins {
		p.shutdown()
	}
}