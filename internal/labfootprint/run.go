package labfootprint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// Config is one harness run.
type Config struct {
	// Binary is the omniglass binary whose `lab run` brings the lab up.
	Binary string
	// DockerHost is the daemon the lab runs on.
	DockerHost string
	// Project is the throwaway lab's compose project, its own.
	Project  string
	Scenario string
	Cadence  string
	Warmup   time.Duration
	Window   time.Duration
	Windows  int
	// ReadHours are the series read ranges, in hours; ReadsEach is how many
	// times each picked series is read at each.
	ReadHours []int
	ReadsEach int
	// ReadCPUs is the CPU the server and Postgres are held to while the
	// reads are timed.
	ReadCPUs float64
	// Out is where progress goes; the lab's own output goes to LabLog.
	Out    io.Writer
	LabLog io.Writer
}

// The services the harness measures, by their ready-file names.
var services = map[string]string{"server": "LAB_SERVER", "node": "LAB_NODE", "db": "LAB_DB"}

// harness is one run's live state.
type harness struct {
	cfg  Config
	dc   *client.Client
	env  map[string]string
	http *http.Client
}

func (h *harness) say(format string, a ...any) { fmt.Fprintf(h.cfg.Out, format+"\n", a...) }

// Measure brings a throwaway lab up, measures it, tears it down, and checks
// that nothing it created is left.
func Measure(ctx context.Context, cfg Config) (rep Report, err error) {
	if cfg.Project == "" || cfg.Project == lab.DefaultProject {
		return Report{}, fmt.Errorf("the lab footprint needs a throwaway project of its own, never %s", lab.DefaultProject)
	}
	dc, err := client.New(client.WithHost(cfg.DockerHost))
	if err != nil {
		return Report{}, fmt.Errorf("docker: %w", err)
	}
	defer dc.Close()
	h := &harness{cfg: cfg, dc: dc, http: &http.Client{Timeout: 2 * time.Minute}}

	dir, err := os.MkdirTemp("", "labfootprint-")
	if err != nil {
		return Report{}, err
	}
	ready := filepath.Join(dir, "lab.env")
	defer func() { _ = os.Remove(ready); _ = os.Remove(dir) }()

	h.say("bringing up the throwaway lab %s (scenario %s, cadence %s)", cfg.Project, cfg.Scenario, cfg.Cadence)
	cmd := exec.Command(cfg.Binary, "lab", "run", "--ephemeral", "--project", cfg.Project, "--port", "0",
		"--ready-file", ready, "--while-pid", strconv.Itoa(os.Getpid()),
		"--scenario", cfg.Scenario, "--cadence", cfg.Cadence)
	cmd.Stdout, cmd.Stderr = cfg.LabLog, cfg.LabLog
	if err := cmd.Start(); err != nil {
		return Report{}, fmt.Errorf("start the lab: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		if derr := h.down(cmd, exited); derr != nil && err == nil {
			err = derr
		}
	}()

	if h.env, err = waitReady(ctx, ready, exited); err != nil {
		return Report{}, err
	}
	return h.measure(ctx)
}

// waitReady waits for the lab's ready file, or for the lab to exit first.
func waitReady(ctx context.Context, path string, exited <-chan error) (map[string]string, error) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return ParseReadyEnv(string(b))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-exited:
			return nil, fmt.Errorf("the lab exited before it was ready: %v", err)
		case <-t.C:
		}
	}
}

// down stops the lab with one SIGTERM (its own teardown removes what it
// created), waits for it, then checks nothing labeled with the project is
// left behind.
func (h *harness) down(cmd *exec.Cmd, exited <-chan error) error {
	if cmd.ProcessState == nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(3 * time.Minute):
			return fmt.Errorf("the lab %s did not stop within 3 minutes of SIGTERM (pid %d)", h.cfg.Project, cmd.Process.Pid)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	left, err := h.leftovers(ctx)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("the lab %s left behind %s", h.cfg.Project, strings.Join(left, ", "))
	}
	h.say("torn down: no container, network or volume of %s is left", h.cfg.Project)
	return nil
}

// leftovers lists what still carries the project's label.
func (h *harness) leftovers(ctx context.Context) ([]string, error) {
	f := client.Filters{}
	f.Add("label", lab.LabelComposeProject+"="+h.cfg.Project)
	var left []string
	cs, err := h.dc.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}
	for _, c := range cs.Items {
		left = append(left, "container "+strings.Join(c.Names, ","))
	}
	ns, err := h.dc.NetworkList(ctx, client.NetworkListOptions{Filters: f})
	if err != nil {
		return nil, err
	}
	for _, n := range ns.Items {
		left = append(left, "network "+n.Name)
	}
	vs, err := h.dc.VolumeList(ctx, client.VolumeListOptions{Filters: f})
	if err != nil {
		return nil, err
	}
	for _, v := range vs.Items {
		left = append(left, "volume "+v.Name)
	}
	return left, nil
}

func (h *harness) measure(ctx context.Context) (Report, error) {
	run := Run{Scenario: h.cfg.Scenario, Cadence: h.cfg.Cadence, Started: time.Now(),
		Warmup: h.cfg.Warmup, Window: h.cfg.Window, Reads: map[int][]time.Duration{}}
	img, err := h.dc.ImageInspect(ctx, h.env["LAB_IMAGE"])
	if err == nil && img.Config != nil {
		run.Build = img.Config.Labels[lab.VersionLabel]
	}
	fleet, err := h.psql(ctx, `select (select count(*) from system), (select count(*) from component), (select count(*) from task where enabled)`)
	if err != nil {
		return Report{}, fmt.Errorf("read the fleet's size: %w", err)
	}
	if f := strings.Split(strings.TrimSpace(fleet), "|"); len(f) == 3 {
		run.Systems, _ = strconv.Atoi(f[0])
		run.Components, _ = strconv.Atoi(f[1])
		run.Tasks, _ = strconv.Atoi(f[2])
	}
	h.say("ready: %d systems, %d components, %d enabled tasks; warming up for %s", run.Systems, run.Components, run.Tasks, h.cfg.Warmup)
	if err := sleep(ctx, h.cfg.Warmup); err != nil {
		return Report{}, err
	}

	db0, use0, err := h.read(ctx)
	if err != nil {
		return Report{}, err
	}
	for i := 1; i <= h.cfg.Windows; i++ {
		if err := sleep(ctx, h.cfg.Window); err != nil {
			return Report{}, err
		}
		db1, use1, err := h.read(ctx)
		if err != nil {
			return Report{}, err
		}
		w, err := WindowOf(db0, db1, use0, use1)
		if err != nil {
			return Report{}, err
		}
		run.Windows = append(run.Windows, w)
		h.say("window %d of %d: %s metric rows a minute, %.0f millicores in all", i, h.cfg.Windows,
			count(w.MetricRowsPerMinute), w.Containers["server"].Millicores+w.Containers["node"].Millicores+w.Containers["db"].Millicores)
		db0, use0 = db1, use1
	}

	if err := h.timeReads(ctx, &run); err != nil {
		return Report{}, err
	}
	return Summarize(run), nil
}

// read takes one reading of the database and of every container.
func (h *harness) read(ctx context.Context) (DBCounters, map[string]Usage, error) {
	out, err := h.psql(ctx, DBCountersQuery)
	if err != nil {
		return DBCounters{}, nil, fmt.Errorf("read the database counters: %w", err)
	}
	db, err := ParseDBCounters(out)
	if err != nil {
		return DBCounters{}, nil, err
	}
	use := map[string]Usage{}
	for name, key := range services {
		u, err := h.usage(ctx, h.env[key])
		if err != nil {
			return DBCounters{}, nil, fmt.Errorf("read the %s container: %w", name, err)
		}
		use[name] = u
	}
	return db, use, nil
}

// usage is one container's cgroup, read once through the Engine.
func (h *harness) usage(ctx context.Context, id string) (Usage, error) {
	r, err := h.dc.ContainerStats(ctx, id, client.ContainerStatsOptions{})
	if err != nil {
		return Usage{}, err
	}
	defer r.Body.Close()
	var s container.StatsResponse
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		return Usage{}, err
	}
	return UsageFromStats(s)
}

// psql runs one query in the database container and returns its unaligned
// output.
func (h *harness) psql(ctx context.Context, q string) (string, error) {
	ex, err := h.dc.ExecCreate(ctx, h.env["LAB_DB"], client.ExecCreateOptions{
		Cmd:          []string{"psql", "-U", "omniglass", "-d", "omniglass", "-AtX", "-v", "ON_ERROR_STOP=1", "-c", q},
		AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return "", err
	}
	att, err := h.dc.ExecAttach(ctx, ex.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", err
	}
	defer att.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, att.Reader); err != nil {
		return "", err
	}
	in, err := h.dc.ExecInspect(ctx, ex.ID, client.ExecInspectOptions{})
	if err != nil {
		return "", err
	}
	if in.ExitCode != 0 {
		return "", fmt.Errorf("psql exited %d: %s", in.ExitCode, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// timeReads holds the server and Postgres to ReadCPUs, then times each
// picked series' read at each range through the API, as a console chart
// reads it.
func (h *harness) timeReads(ctx context.Context, run *Run) error {
	series, err := h.pickSeries(ctx)
	if err != nil {
		return err
	}
	if len(series) == 0 {
		return errors.New("no component has a sampled metric to read")
	}
	nano := int64(h.cfg.ReadCPUs * 1e9)
	for _, key := range []string{"LAB_SERVER", "LAB_DB"} {
		if _, err := h.dc.ContainerUpdate(ctx, h.env[key], client.ContainerUpdateOptions{
			Resources: &container.Resources{NanoCPUs: nano}}); err != nil {
			return fmt.Errorf("hold %s to %.1f CPU: %w", h.env[key], h.cfg.ReadCPUs, err)
		}
	}
	h.say("timing %d series reads at each of %v hours, server and Postgres held to %.1f CPU", len(series)*h.cfg.ReadsEach, h.cfg.ReadHours, h.cfg.ReadCPUs)
	for _, hours := range h.cfg.ReadHours {
		for i := 0; i < h.cfg.ReadsEach; i++ {
			for _, s := range series {
				u := fmt.Sprintf("%s/api/v1/components/%s/metrics/%s/samples?hours=%d&limit=2000",
					h.env["LAB_HOST_URL"], url.PathEscape(s.Component), url.PathEscape(s.Metric), hours)
				start := time.Now()
				if err := h.get(ctx, u, nil); err != nil {
					return fmt.Errorf("read %s %s over %d h: %w", s.Component, s.Metric, hours, err)
				}
				run.Reads[hours] = append(run.Reads[hours], time.Since(start))
			}
		}
	}
	return nil
}

// pickSeries lists the components and their sampled metrics, and picks the
// series to time.
func (h *harness) pickSeries(ctx context.Context) ([]Series, error) {
	var list struct {
		Components []struct {
			ID string `json:"id"`
		} `json:"components"`
	}
	if err := h.get(ctx, h.env["LAB_HOST_URL"]+"/api/v1/components", &list); err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}
	var comps []ComponentMetrics
	for _, c := range list.Components {
		var m struct {
			Metrics []struct {
				Name      string `json:"metric_type_name"`
				IsSampled bool   `json:"is_sampled"`
			} `json:"metrics"`
		}
		if err := h.get(ctx, h.env["LAB_HOST_URL"]+"/api/v1/components/"+url.PathEscape(c.ID)+"/metrics", &m); err != nil {
			return nil, fmt.Errorf("list %s's metrics: %w", c.ID, err)
		}
		// By uuid: a lab's names repeat across systems (amp-1).
		cm := ComponentMetrics{Component: c.ID}
		for _, x := range m.Metrics {
			if x.IsSampled {
				cm.Metrics = append(cm.Metrics, x.Name)
			}
		}
		comps = append(comps, cm)
		if len(PickSeries(comps, 8, 2)) >= 8 {
			break
		}
	}
	return PickSeries(comps, 8, 2), nil
}

// get reads a URL with the lab's owner token, decoding JSON into v (nil
// drains the body).
func (h *harness) get(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.env["LAB_TOKEN"])
	resp, err := h.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if v == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
