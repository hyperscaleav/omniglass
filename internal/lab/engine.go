package lab

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Engine is the slice of the Docker Engine API the lab uses. The runner
// drives this, never the client directly, so its bring-up, teardown and reap
// logic is testable against a fake; the real Engine is the moby client
// (NewDockerEngine), proven against a real daemon by the integration test.
type Engine interface {
	Ping(ctx context.Context) (EngineInfo, error)
	ImageLabels(ctx context.Context, ref string) (labels map[string]string, found bool, err error)
	PullImage(ctx context.Context, ref string) error
	CreateNetwork(ctx context.Context, name string, labels map[string]string) error
	RemoveNetwork(ctx context.Context, name string) error
	Networks(ctx context.Context, labels map[string]string) ([]Resource, error)
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string) error
	Volumes(ctx context.Context, labels map[string]string) ([]Resource, error)
	// CreateContainer creates and starts a service's container on the
	// network, reachable there by its service name.
	RunContainer(ctx context.Context, svc Service, network string) (id string, err error)
	Inspect(ctx context.Context, id string) (ContainerState, error)
	Wait(ctx context.Context, id string) (exitCode int, err error)
	Logs(ctx context.Context, id string, tail int) (string, error)
	StopAndRemove(ctx context.Context, id string) error
	Containers(ctx context.Context, labels map[string]string) ([]Resource, error)
}

// EngineInfo is what a ping reports.
type EngineInfo struct {
	APIVersion string
	OSType     string
}

// Resource is a container, network or volume the lab found by its labels.
type Resource struct {
	ID     string
	Name   string
	Labels map[string]string
}

// ContainerState is the part of an inspect the lab reads.
type ContainerState struct {
	Running    bool
	Restarting bool
	Status     string
	ExitCode   int
	// Health is the healthcheck's status, empty without one.
	Health string
	// Published maps a container port to the host address it is bound on.
	Published map[int]string
	// CapAdd and User are what the container runs with, for the
	// integration test's privilege check.
	CapAdd []string
	User   string
}

// dockerEngine is the Engine over the moby client.
type dockerEngine struct {
	c *client.Client
}

// NewDockerEngine connects to the daemon at host, negotiating the API
// version.
func NewDockerEngine(host string) (Engine, error) {
	c, err := client.New(client.WithHost(host))
	if err != nil {
		return nil, err
	}
	return &dockerEngine{c: c}, nil
}

func filters(labels map[string]string) client.Filters {
	f := client.Filters{}
	for _, k := range SortedKeys(labels) {
		f.Add("label", k+"="+labels[k])
	}
	return f
}

func (d *dockerEngine) Ping(ctx context.Context) (EngineInfo, error) {
	r, err := d.c.Ping(ctx, client.PingOptions{NegotiateAPIVersion: true})
	if err != nil {
		return EngineInfo{}, err
	}
	return EngineInfo{APIVersion: r.APIVersion, OSType: r.OSType}, nil
}

func (d *dockerEngine) ImageLabels(ctx context.Context, ref string) (map[string]string, bool, error) {
	r, err := d.c.ImageInspect(ctx, ref)
	if cerrdefs.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if r.Config == nil {
		return map[string]string{}, true, nil
	}
	return r.Config.Labels, true, nil
}

func (d *dockerEngine) PullImage(ctx context.Context, ref string) error {
	r, err := d.c.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Wait(ctx)
}

func (d *dockerEngine) CreateNetwork(ctx context.Context, name string, labels map[string]string) error {
	_, err := d.c.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge", Labels: labels})
	return err
}

func (d *dockerEngine) RemoveNetwork(ctx context.Context, name string) error {
	_, err := d.c.NetworkRemove(ctx, name, client.NetworkRemoveOptions{})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func (d *dockerEngine) Networks(ctx context.Context, labels map[string]string) ([]Resource, error) {
	r, err := d.c.NetworkList(ctx, client.NetworkListOptions{Filters: filters(labels)})
	if err != nil {
		return nil, err
	}
	var out []Resource
	for _, n := range r.Items {
		out = append(out, Resource{ID: n.ID, Name: n.Name, Labels: n.Labels})
	}
	return out, nil
}

func (d *dockerEngine) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	_, err := d.c.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: labels})
	return err
}

func (d *dockerEngine) RemoveVolume(ctx context.Context, name string) error {
	_, err := d.c.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func (d *dockerEngine) Volumes(ctx context.Context, labels map[string]string) ([]Resource, error) {
	r, err := d.c.VolumeList(ctx, client.VolumeListOptions{Filters: filters(labels)})
	if err != nil {
		return nil, err
	}
	var out []Resource
	for _, v := range r.Items {
		out = append(out, Resource{ID: v.Name, Name: v.Name, Labels: v.Labels})
	}
	return out, nil
}

func (d *dockerEngine) RunContainer(ctx context.Context, svc Service, net string) (string, error) {
	cfg := &container.Config{
		Image:  svc.Image,
		Cmd:    svc.Cmd,
		Env:    svc.Env,
		User:   svc.User,
		Labels: svc.Labels,
	}
	if len(svc.Health) > 0 {
		cfg.Healthcheck = &container.HealthConfig{
			Test: svc.Health, Interval: time.Second, Timeout: 3 * time.Second, Retries: 60,
			StartInterval: 500 * time.Millisecond,
		}
	}
	host := &container.HostConfig{
		CapAdd:  svc.CapAdd,
		Sysctls: svc.Sysctls,
	}
	if svc.Restart != "" {
		host.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(svc.Restart)}
	}
	for _, m := range svc.Mounts {
		host.Mounts = append(host.Mounts, mount.Mount{Type: mount.TypeVolume, Source: m.Volume, Target: m.Target})
	}
	if p := svc.Publish; p != nil {
		port, err := network.ParsePort(strconv.Itoa(p.ContainerPort) + "/tcp")
		if err != nil {
			return "", err
		}
		ip, err := netip.ParseAddr(p.HostIP)
		if err != nil {
			return "", err
		}
		hostPort := ""
		if p.HostPort != 0 {
			hostPort = strconv.Itoa(p.HostPort)
		}
		cfg.ExposedPorts = network.PortSet{port: struct{}{}}
		host.PortBindings = network.PortMap{port: {{HostIP: ip, HostPort: hostPort}}}
	}
	netCfg := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
		net: {Aliases: []string{svc.Name}},
	}}
	created, err := d.c.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: cfg, HostConfig: host, NetworkingConfig: netCfg, Name: svc.Container,
	})
	if err != nil {
		return "", err
	}
	if _, err := d.c.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return created.ID, err
	}
	return created.ID, nil
}

func (d *dockerEngine) Inspect(ctx context.Context, id string) (ContainerState, error) {
	r, err := d.c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return ContainerState{}, err
	}
	c := r.Container
	st := ContainerState{Published: map[int]string{}}
	if c.State != nil {
		st.Running, st.Restarting, st.Status, st.ExitCode = c.State.Running, c.State.Restarting, string(c.State.Status), c.State.ExitCode
		if c.State.Health != nil {
			st.Health = string(c.State.Health.Status)
		}
	}
	if c.HostConfig != nil {
		st.CapAdd = c.HostConfig.CapAdd
	}
	if c.Config != nil {
		st.User = c.Config.User
	}
	if c.NetworkSettings != nil {
		for port, binds := range c.NetworkSettings.Ports {
			for _, b := range binds {
				if b.HostPort != "" {
					st.Published[int(port.Num())] = netipJoin(b.HostIP, b.HostPort)
					break
				}
			}
		}
	}
	return st, nil
}

func netipJoin(ip netip.Addr, port string) string {
	if ip.Is6() {
		return "[" + ip.String() + "]:" + port
	}
	return ip.String() + ":" + port
}

func (d *dockerEngine) Wait(ctx context.Context, id string) (int, error) {
	w := d.c.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case r := <-w.Result:
		if r.Error != nil && r.Error.Message != "" {
			return int(r.StatusCode), errors.New(r.Error.Message)
		}
		return int(r.StatusCode), nil
	case err := <-w.Error:
		return -1, err
	}
}

func (d *dockerEngine) Logs(ctx context.Context, id string, tail int) (string, error) {
	opts := client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true}
	if tail > 0 {
		opts.Tail = strconv.Itoa(tail)
	}
	r, err := d.c.ContainerLogs(ctx, id, opts)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, r); err != nil && !errors.Is(err, io.EOF) {
		return out.String(), err
	}
	return out.String(), nil
}

func (d *dockerEngine) StopAndRemove(ctx context.Context, id string) error {
	timeout := 10
	if _, err := d.c.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &timeout}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("stop %s: %w", id, err)
	}
	if _, err := d.c.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("remove %s: %w", id, err)
	}
	return nil
}

func (d *dockerEngine) Containers(ctx context.Context, labels map[string]string) ([]Resource, error) {
	r, err := d.c.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters(labels)})
	if err != nil {
		return nil, err
	}
	var out []Resource
	for _, c := range r.Items {
		name := c.ID
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, Resource{ID: c.ID, Name: name, Labels: c.Labels})
	}
	return out, nil
}
