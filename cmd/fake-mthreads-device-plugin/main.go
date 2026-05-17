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
//
// Memory capacity has two modes, picked by --memory-from-yaml:
//   - off (default): sgpu-memory = N * memSlicesPerCard. Matches HAMi's
//     hardcoded assumption that every MTT card is 48 GiB. Safe when the
//     HAMi mutator auto-fills sgpu-memory=96 for `vgpu: 1` requests.
//   - on: sgpu-memory = sum(card.memory.total / 512MiB). Reports real
//     per-card capacity (e.g. 32 slices for a 16 GiB S80). HAMi will not
//     schedule a Pod that asks for more slices than advertised, so
//     callers must request sgpu-memory explicitly instead of relying on
//     the 96-default. Useful for surfacing the hardcoded-96 mismatch
//     before pushing a fix upstream.
//
// 拓扑(一个进程,三个 plugin socket,与同一个 NRI injector 协作):
//
//	         fake-musa.yaml (N 张卡,可选 memory.total)
//	                       │
//	                       ▼
//	             ┌───────────────────┐
//	             │  capacity{cards,  │
//	             │   memSlices}      │
//	             └─────────┬─────────┘
//	                       │
//	  ┌────────────────────┼────────────────────┐
//	  ▼                    ▼                    ▼
//	pluginServer        pluginServer        pluginServer
//	mthreads.com/vgpu   .../sgpu-core       .../sgpu-memory
//	  N 个设备           N*16 个设备         memSlices 个设备
//	  └────────┬───────────┴────────┬──────────┘
//	           ▼                    ▼
//	    向 kubelet 注册 device-plugin(3 个 socket)
//	           │
//	           ▼
//	    node 对外声明 mthreads.com/* 资源容量
//	           │
//	           ▼
//	    HAMi scheduler 决定 GPU index,
//	    把 mthreads.com/gpu-index 注解打到 Pod 上
//	           │
//	           ▼
//	    (这个 plugin 的 Allocate 故意返回空,理由见下面)
//	           │
//	           ▼
//	    fake-gpu NRI device-injector 读注解,
//	    bind-mount libfakegpu.so + 设置 MUSA_VISIBLE_DEVICES
//
// 空 Allocate 是关键设计:如果在这里返回 mount/env,会跟 NRI injector
// 重复挂载,并和 kubelet 抢容器 env 的注入权。
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

// cardInfo carries the per-card fields the device-plugin needs from
// fake-musa.yaml. Memory.Total is bytes (matches the YAML), kept as
// uint64 so we don't lose precision on 48 GiB+ cards.
type cardInfo struct {
	Name        string
	MemoryBytes uint64
}

// readCardsFromYAML parses the `moorethreads:` list and pulls out the
// fields we need for capacity math. We use yaml.v3 directly (instead of
// sigs.k8s.io/yaml) because we only need a few fields, not strict typing
// — keeps the binary lean.
func readCardsFromYAML(path string) ([]cardInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg struct {
		Moorethreads []struct {
			Name   string `yaml:"name"`
			Memory struct {
				Total uint64 `yaml:"total"`
			} `yaml:"memory"`
		} `yaml:"moorethreads"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]cardInfo, 0, len(cfg.Moorethreads))
	for _, m := range cfg.Moorethreads {
		out = append(out, cardInfo{Name: m.Name, MemoryBytes: m.Memory.Total})
	}
	return out, nil
}

// countCardsFromYAML is a thin len() wrapper kept for callers that only
// need the card count.
func countCardsFromYAML(path string) (int, error) {
	cards, err := readCardsFromYAML(path)
	return len(cards), err
}

// memSlicesFor decides how many sgpu-memory slices to advertise for the
// node. See the file header for the two modes.
func memSlicesFor(cards []cardInfo, fromYAML bool) int {
	if !fromYAML {
		return len(cards) * memSlicesPerCard
	}
	n := 0
	for _, c := range cards {
		n += int(c.MemoryBytes / memSliceBytes)
	}
	return n
}

// capacity holds the advertised totals for the three resources, decoupled
// from cards count so memory can either follow HAMi's 96-slice constant
// or the YAML's real memory.total.
type capacity struct {
	cards     int // -> vgpu count; sgpu-core = cards * coresPerCard
	memSlices int // -> sgpu-memory count
}

// devicesForResource builds a stable, deterministic device list for a
// resource. IDs are namespaced by resource so kubelet won't conflate the
// three resources' identity spaces.
//
// vgpu:        cap.cards entries, IDs "MT-FAKE-VGPU-0..N-1"
// sgpu-core:   cap.cards * coresPerCard entries
// sgpu-memory: cap.memSlices entries (constant-mode or YAML-mode)
func devicesForResource(resource string, cap capacity) []*pluginapi.Device {
	count := 0
	prefix := ""
	switch resource {
	case resourceVGPU:
		count = cap.cards
		prefix = "MT-FAKE-VGPU"
	case resourceSGPUCore:
		count = cap.cards * coresPerCard
		prefix = "MT-FAKE-CORE"
	case resourceSGPUMem:
		count = cap.memSlices
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
	resource string
	socket   string
	cap      capacity
	server   *grpc.Server
	stop     chan struct{}
}

func newPluginServer(resource string, cap capacity, socketDir string) *pluginServer {
	// Socket name avoids "/" in the resource name (kubelet conventions).
	base := strings.ReplaceAll(resource, "/", "_")
	return &pluginServer{
		resource: resource,
		cap:      cap,
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
	devs := devicesForResource(p.resource, p.cap)
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
		configPath      string
		socketDir       string
		kubeletSock     string
		verbose         bool
		memoryFromYAML  bool
	)
	flag.StringVar(&configPath, "config", "/etc/fake-gpu/fake-musa.yaml",
		"fake-musa.yaml path; entries under moorethreads: determine N cards")
	flag.StringVar(&socketDir, "socket-dir", pluginapi.DevicePluginPath,
		"directory for plugin unix sockets (kubelet's device-plugins dir)")
	flag.StringVar(&kubeletSock, "kubelet-socket", filepath.Join(pluginapi.DevicePluginPath, "kubelet.sock"),
		"kubelet device-plugin registration socket")
	flag.BoolVar(&verbose, "verbose", false, "debug logging")
	flag.BoolVar(&memoryFromYAML, "memory-from-yaml", false,
		"derive sgpu-memory capacity from each card's memory.total instead of HAMi's hardcoded 96 slices/card. "+
			"See file header for the HAMi compatibility tradeoff.")
	flag.Parse()

	log.SetFormatter(&logrus.TextFormatter{PadLevelText: true})
	if verbose {
		log.SetLevel(logrus.DebugLevel)
	}

	cards, err := readCardsFromYAML(configPath)
	if err != nil {
		log.Fatalf("read cards: %v", err)
	}
	if len(cards) == 0 {
		log.Warnf("%s contains 0 moorethreads entries — registering with N=0 (HAMi will see zero capacity)", configPath)
	}
	cap := capacity{
		cards:     len(cards),
		memSlices: memSlicesFor(cards, memoryFromYAML),
	}
	if memoryFromYAML {
		log.Infof("derived N=%d cards, memSlices=%d (from YAML memory.total) from %s",
			cap.cards, cap.memSlices, configPath)
	} else {
		log.Infof("derived N=%d cards, memSlices=%d (HAMi-compat: %d/card) from %s",
			cap.cards, cap.memSlices, memSlicesPerCard, configPath)
	}

	resources := []string{resourceVGPU, resourceSGPUCore, resourceSGPUMem}
	plugins := make([]*pluginServer, 0, len(resources))
	for _, r := range resources {
		p := newPluginServer(r, cap, socketDir)
		if err := p.serve(); err != nil {
			log.Fatalf("serve %s: %v", r, err)
		}
		if err := p.register(kubeletSock); err != nil {
			log.Fatalf("register %s: %v", r, err)
		}
		plugins = append(plugins, p)
	}

	log.Infof("fake-mthreads-device-plugin ready (cards=%d, memSlices=%d, resources=%v)",
		cap.cards, cap.memSlices, resources)

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