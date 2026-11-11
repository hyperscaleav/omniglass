package labfootprint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// fleetSizeSQL reads a lab's fleet: its systems, components and enabled
// tasks. The platform system (og/platform, which the boot seed plants on
// every server since #1625) is the server's own, not the scenario's, so it
// is not counted: a scale lab at 300 systems reads 300, which the perf gate
// compares with its baseline's size (#1640).
const fleetSizeSQL = `select
	(select count(*) from system s left join system_type st on st.id = s.system_type_id
	 where st.id is null or not (st.namespace = 'og' and st.name = 'platform')),
	(select count(*) from component),
	(select count(*) from task where enabled)`

// ScaleConfig is one scale measurement (#1556).
type ScaleConfig struct {
	// Binary, when set, is the omniglass binary whose `lab run` brings up a
	// throwaway scale lab of Systems systems for the run and tears it down
	// after. Empty attaches to the scale lab already running.
	Binary   string
	Scenario string
	Systems  int
	// Image, when set, is the omniglass image the brought-up lab runs (lab
	// run's own --image); empty is the binary's own build.
	Image string
	// DockerHost is the daemon the lab runs on.
	DockerHost string
	// ReadyTimeout bounds a bring-up (lab run's own --ready-timeout).
	ReadyTimeout time.Duration
	// InduceUnregistered brings the lab up with one device answering a
	// property key no driver registers (lab run's --induce-unregistered,
	// #1698), so the delivery table shows a refusal as property loss.
	InduceUnregistered bool
	Warmup             time.Duration
	Window             time.Duration
	// FleetReads is how many console fleet-view reads are timed, spread
	// evenly over the window.
	FleetReads int
	// Backlog is how many aged rows the retention drain probe plants; 0
	// skips the probe, which restarts the server.
	Backlog int64
	// DrainTimeout bounds the wait for the pass to clear the backlog.
	DrainTimeout time.Duration
	// PageClients is how many clients load the console's system page at
	// once across the window, each PageLoads times; 0 skips the load.
	// PageSystem names the system they open, empty the fleet view's first.
	PageClients int
	PageLoads   int
	PageSystem  string
	// Every and Samples are a sampling run's interval and its count of
	// samples, 0 sampling until stopped; SampleScale reads them.
	Every   time.Duration
	Samples int
	// SettleTimeout bounds the delivery read's wait for the window's batches
	// to land (#1641).
	SettleTimeout time.Duration
	Out, LabLog   io.Writer
}

// scaleLab is the running scale lab's containers, found by their compose
// labels.
type scaleLab struct {
	project string
	server  string
	db      string
	// nodes maps a node's service name (edge-hq) to its container.
	nodes map[string]string
	// image is the server's image, for its build label.
	image string
	// labels are the server container's labels, which name the fleet the
	// lab seeded (lab.FleetOf).
	labels map[string]string
}

// containers is every container the window reads, by the name the table
// lists it under.
func (l scaleLab) containers() map[string]string {
	out := map[string]string{"server": l.server, "db": l.db}
	for name, id := range l.nodes {
		out[name] = id
	}
	return out
}

// MeasureScale measures a scale lab: the one running (Binary empty), or one
// it brings up and tears down (Binary set). The lab's own bring-up timeout
// bounds a hang; the run itself is Warmup + Window plus the probes.
func MeasureScale(ctx context.Context, cfg ScaleConfig) (rep ScaleReport, err error) {
	dc, err := client.New(client.WithHost(cfg.DockerHost))
	if err != nil {
		return ScaleReport{}, fmt.Errorf("docker: %w", err)
	}
	defer dc.Close()
	h := &harness{
		cfg:  Config{Project: lab.ScaleProject, Out: cfg.Out, LabLog: cfg.LabLog},
		dc:   dc,
		http: &http.Client{Timeout: 2 * time.Minute},
	}
	if cfg.Binary != "" {
		stop, err := h.bringUpScale(ctx, cfg)
		if err != nil {
			return ScaleReport{}, err
		}
		defer func() {
			if derr := stop(); derr != nil && err == nil {
				err = derr
			}
		}()
	}
	return h.measureScale(ctx, cfg)
}

// scaleRunArgs is the `lab run` a scale bring-up execs: the scenario at its
// size on a free port, saying how to reach it in ready, held for the process
// pid, on the config's image when it names one.
func scaleRunArgs(cfg ScaleConfig, ready string, pid int) []string {
	args := []string{"lab", "run", "--scenario", cfg.Scenario, "--systems", strconv.Itoa(cfg.Systems),
		"--port", "0", "--ready-file", ready, "--while-pid", strconv.Itoa(pid),
		"--ready-timeout", cfg.ReadyTimeout.String()}
	if cfg.Image != "" {
		args = append(args, "--image", cfg.Image)
	}
	if cfg.InduceUnregistered {
		args = append(args, "--induce-unregistered")
	}
	return args
}

// bringUpScale starts `lab run` for the scale lab, waits for its ready file,
// and returns the function that stops it and checks nothing is left.
func (h *harness) bringUpScale(ctx context.Context, cfg ScaleConfig) (func() error, error) {
	if cfg.Systems <= 0 {
		return nil, errors.New("bringing up a scale lab needs --systems")
	}
	left, err := h.leftovers(ctx)
	if err != nil {
		return nil, err
	}
	if len(left) > 0 {
		return nil, fmt.Errorf("a scale lab is already up (%s): measure it without --systems, or stop it first", strings.Join(left, ", "))
	}
	dir, err := os.MkdirTemp("", "labscale-")
	if err != nil {
		return nil, err
	}
	ready := filepath.Join(dir, "lab.env")
	h.say("bringing up the scale lab: %s at %d systems", cfg.Scenario, cfg.Systems)
	cmd := exec.Command(cfg.Binary, scaleRunArgs(cfg, ready, os.Getpid())...)
	cmd.Stdout, cmd.Stderr = cfg.LabLog, cfg.LabLog
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("start the lab: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stop := func() error {
		defer os.RemoveAll(dir)
		return h.down(cmd, exited)
	}
	// The lab's own bring-up bound ends a hang: it removes what it started
	// and exits, which ends this wait.
	if _, err := waitReady(ctx, ready, exited); err != nil {
		if serr := stop(); serr != nil {
			err = errors.Join(err, serr)
		}
		return nil, err
	}
	return stop, nil
}

// discoverScale finds the running scale lab's containers by label.
func (h *harness) discoverScale(ctx context.Context) (scaleLab, error) {
	f := client.Filters{}
	f.Add("label", lab.LabelComposeProject+"="+h.cfg.Project)
	cs, err := h.dc.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return scaleLab{}, err
	}
	l := scaleLab{project: h.cfg.Project, nodes: map[string]string{}}
	for _, c := range cs.Items {
		if c.Labels[lab.LabelComposeOneOff] == "True" {
			continue
		}
		svc := c.Labels[lab.LabelComposeService]
		if string(c.State) != "running" {
			return scaleLab{}, fmt.Errorf("the %s container is %s, not running", svc, c.State)
		}
		switch svc {
		case "server":
			l.server, l.image, l.labels = c.ID, c.Image, c.Labels
		case "db":
			l.db = c.ID
		default:
			l.nodes[svc] = c.ID
		}
	}
	switch {
	case l.server == "" || l.db == "":
		return scaleLab{}, fmt.Errorf("no scale lab is running (project %s has no server and database): start one with `omniglass lab run --scenario corporate --systems N`", h.cfg.Project)
	case len(l.nodes) == 0:
		return scaleLab{}, fmt.Errorf("project %s has no node containers: it is not a scale lab", h.cfg.Project)
	}
	return l, nil
}

// hostURL is where the server's console answers on this host.
func (h *harness) hostURL(ctx context.Context, id string) (string, error) {
	r, err := h.dc.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	if r.Container.NetworkSettings != nil {
		for _, binds := range r.Container.NetworkSettings.Ports {
			for _, b := range binds {
				if b.HostPort == "" {
					continue
				}
				host := "127.0.0.1"
				if b.HostIP.IsValid() && !b.HostIP.IsUnspecified() {
					host = b.HostIP.String()
				}
				return "http://" + net.JoinHostPort(host, b.HostPort), nil
			}
		}
	}
	return "", errors.New("the server publishes no port on this host")
}

// signIn signs in with the lab's login and mints a token, so the timed
// reads go through the API as a script would.
func (h *harness) signIn(ctx context.Context, base string) (string, error) {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	post := func(path, body string, want int) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode != want {
			return nil, fmt.Errorf("%s: %s %s", path, resp.Status, strings.TrimSpace(string(b)))
		}
		return b, nil
	}
	if _, err := post("/api/v1/auth/login", fmt.Sprintf(`{"username":%q,"password":%q}`, lab.LoginUser, lab.LoginPassword), http.StatusNoContent); err != nil {
		return "", fmt.Errorf("sign in: %w", err)
	}
	b, err := post("/api/v1/auth/me/tokens", `{"description":"the scale lab measurement","ttl_days":1}`, http.StatusCreated)
	if err != nil {
		return "", fmt.Errorf("mint a token: %w", err)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Token == "" {
		return "", errors.New("mint a token: the answer carried none")
	}
	return out.Token, nil
}

// scaleSession is a scale lab found and signed in to: what every window
// reads from, read once however many windows a run measures.
type scaleSession struct {
	l scaleLab
	// fleet is the run's conditions, copied into each window's run.
	fleet ScaleRun
	// pageSystem is the id of the system the page load opens, when it runs.
	pageSystem string
}

func (h *harness) measureScale(ctx context.Context, cfg ScaleConfig) (ScaleReport, error) {
	s, err := h.prepareScale(ctx, cfg)
	if err != nil {
		return ScaleReport{}, err
	}
	if err := sleep(ctx, cfg.Warmup); err != nil {
		return ScaleReport{}, err
	}
	run := s.fleet
	run.Started = time.Now()
	if _, err := h.measureWindow(ctx, cfg, s, &run); err != nil {
		return ScaleReport{}, err
	}
	if cfg.Backlog > 0 {
		if run.Drain, err = h.drainProbe(ctx, s.l.server, cfg); err != nil {
			return ScaleReport{}, err
		}
	}
	return SummarizeScale(run), nil
}

// prepareScale finds the running lab, signs in, reads the fleet's size, and
// makes one fleet view read before anything is timed.
func (h *harness) prepareScale(ctx context.Context, cfg ScaleConfig) (scaleSession, error) {
	l, err := h.discoverScale(ctx)
	if err != nil {
		return scaleSession{}, err
	}
	run, err := scaleRunOf(l.labels, cfg, len(l.nodes))
	if err != nil {
		return scaleSession{}, err
	}
	h.env = map[string]string{"LAB_DB": l.db, "LAB_SERVER": l.server}
	base, err := h.hostURL(ctx, l.server)
	if err != nil {
		return scaleSession{}, err
	}
	h.env["LAB_HOST_URL"] = base
	tok, err := h.signIn(ctx, base)
	if err != nil {
		return scaleSession{}, err
	}
	h.env["LAB_TOKEN"] = tok
	// A sampling run outlives its token's day: a read the server refuses as
	// unauthenticated mints a fresh one and is made again.
	h.tokens = newTokenSource(tok, func(ctx context.Context) (string, error) { return h.signIn(ctx, base) })
	if on, err := h.psql(ctx, `show track_commit_timestamp`); err != nil || strings.TrimSpace(on) != "on" {
		return scaleSession{}, errors.New("the lab's Postgres does not record commit timestamps, which the ingest lag reads: stop this lab and bring it up again with this build's `lab run`")
	}

	if img, err := h.dc.ImageInspect(ctx, l.image); err == nil && img.Config != nil {
		run.Build = img.Config.Labels[lab.VersionLabel]
	}
	fleet, err := h.psql(ctx, fleetSizeSQL)
	if err != nil {
		return scaleSession{}, fmt.Errorf("read the fleet's size: %w", err)
	}
	if f := strings.Split(strings.TrimSpace(fleet), "|"); len(f) == 3 {
		// The size is the one the lab was resolved at; a database holding
		// another count was changed after the seed, which the run names.
		if seeded, _ := strconv.Atoi(f[0]); seeded != run.Systems {
			h.say("note: the lab was brought up at %d systems and its database now holds %d", run.Systems, seeded)
		}
		run.Components, _ = strconv.Atoi(f[1])
		run.Tasks, _ = strconv.Atoi(f[2])
	}
	// The perf gate (#1640) names the host a run measured on and the device
	// set its scenario places. A lab on a remote daemon ran elsewhere, so
	// its runs carry no host rather than this one's.
	if cfg.DockerHost == "" || lab.IsLocal(cfg.DockerHost) {
		host := ReadFingerprint()
		run.Host = &host
	}
	// The device set is the one the lab's labelled scenario and size place
	// (#1719), which a lab this run brought up shares with cfg.
	if digest, err := lab.PerfPlanDigest(run.Scenario, run.Systems); err == nil {
		run.PlanDigest = digest
	}
	// One read before anything is timed, so an API that refuses the fleet
	// view fails the run in seconds, not after its window. Its answer names
	// the system the page load opens.
	view, err := h.fleetView(ctx)
	if err != nil {
		return scaleSession{}, err
	}
	s := scaleSession{l: l, fleet: run}
	if cfg.PageClients > 0 {
		if s.pageSystem, err = PickPageSystem(view, cfg.PageSystem); err != nil {
			return scaleSession{}, err
		}
	}
	h.say("ready: %d systems, %d components, %d enabled tasks, %d node containers; warming up for %s",
		run.Systems, run.Components, run.Tasks, len(l.nodes), cfg.Warmup)
	return s, nil
}

// scaleRunOf is the run's conditions: the fleet as the lab's server
// container is labelled with it (#1719), never as cfg's --scenario and
// --systems, which only size a lab the run brings up; the windows as cfg
// gives them; and the sites as the lab's node containers count them.
func scaleRunOf(serverLabels map[string]string, cfg ScaleConfig, sites int) (ScaleRun, error) {
	fleet, err := lab.FleetOf(serverLabels)
	if err != nil {
		return ScaleRun{}, err
	}
	if fleet.Systems == 0 {
		return ScaleRun{}, fmt.Errorf("the lab runs the %s scenario, which has no size: measure a scale lab (`omniglass lab run --scenario corporate --systems N`)", fleet.Scenario)
	}
	return ScaleRun{Scenario: fleet.Scenario, Systems: fleet.Systems, Warmup: cfg.Warmup, Window: cfg.Window, Sites: sites}, nil
}

// measureWindow measures one window into run: the readings at its ends, the
// fleet view and system page loads across it, the database's size at its
// end, what the server logged in it, each node's endpoint observation around
// it and the delivery receipts it reconciles. It returns when the window
// closed.
func (h *harness) measureWindow(ctx context.Context, cfg ScaleConfig, s scaleSession, run *ScaleRun) (time.Time, error) {
	l := s.l
	// The closed loop's marks sit outside the window's own reads. A lab
	// whose nodes do not answer for their endpoints still measures the rest.
	start, endpointErr := h.markStart(ctx, l)
	db0, use0, err := h.readScale(ctx, l)
	if err != nil {
		return time.Time{}, err
	}
	h.say("measuring for %s, with %d fleet-view reads", cfg.Window, cfg.FleetReads)
	end := time.Now().Add(cfg.Window)
	finishUsage := sampleUsage(ctx, cfg.Window/workingSetReads, func(ctx context.Context) (map[string]Usage, error) {
		return h.readUsage(ctx, l)
	})
	defer finishUsage()
	if err := h.loadWindow(ctx, cfg, s.pageSystem, run); err != nil {
		return time.Time{}, err
	}
	if err := sleep(ctx, time.Until(end)); err != nil {
		return time.Time{}, err
	}
	reads, err := finishUsage()
	if err != nil {
		return time.Time{}, err
	}
	db1, use1, err := h.readScale(ctx, l)
	if err != nil {
		return time.Time{}, err
	}
	var stop endpointMark
	if endpointErr == nil {
		stop, endpointErr = h.markEnd(ctx, l)
	}
	if run.Measured, err = WindowOf(db0, db1, use0, use1); err != nil {
		return time.Time{}, err
	}
	// The ends are reads across the window too.
	for _, ends := range []map[string]Usage{use0, use1} {
		for name, u := range ends {
			reads[name] = append(reads[name], u)
		}
	}
	if run.Measured, err = MedianWorkingSets(run.Measured, reads); err != nil {
		return time.Time{}, err
	}
	h.say("window: %s metric rows a minute, %.0f millicores in all", count(run.Measured.MetricRowsPerMinute), totalMillicores(run.Measured))
	out, err := h.psql(ctx, AbsolutesQuery)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the database's size: %w", err)
	}
	if run.Absolutes, err = ParseAbsolutes(out); err != nil {
		return time.Time{}, err
	}

	if run.Lag, run.LagGap, err = h.ingestLag(ctx, db0.At, db1.At); err != nil {
		return time.Time{}, err
	}
	if run.Redeliveries, err = h.redeliveries(ctx, l.server, db0.At); err != nil {
		return time.Time{}, err
	}
	if run.Lane, err = h.consumerLane(ctx, l.server, db0.At, db1.At); err != nil {
		return time.Time{}, err
	}
	if endpointErr == nil {
		run.Endpoints, endpointErr = h.endpointBrackets(ctx, l, start, stop)
	}
	if endpointErr != nil {
		run.EndpointsGap = endpointErr.Error()
		h.say("endpoints not measured: %v", endpointErr)
	}
	run.Delivery = h.readDelivery(ctx, delivery.Window{From: db0.At, To: db1.At}, cfg.SettleTimeout)
	if run.Delivery.Read {
		h.say("delivery: %d receipts read", len(run.Delivery.Receipts))
	}
	return db1.At, nil
}

// loadWindow reads the fleet view cfg.FleetReads times across the window
// while the page clients load the system page, into run. A read that fails
// stops the page clients before it returns, so none loads into the next
// window.
func (h *harness) loadWindow(ctx context.Context, cfg ScaleConfig, system string, run *ScaleRun) error {
	pageCtx, stopPages := context.WithCancel(ctx)
	pages := h.loadPages(pageCtx, cfg, system)
	defer func() {
		stopPages()
		pages()
	}()
	gap := cfg.Window / time.Duration(max(cfg.FleetReads, 1))
	for i := 0; i < cfg.FleetReads; i++ {
		if err := sleep(ctx, gap); err != nil {
			return err
		}
		start := time.Now()
		if err := h.readFleetView(ctx); err != nil {
			return err
		}
		run.FleetView = append(run.FleetView, time.Since(start))
	}
	run.Page = pages()
	return nil
}

// loadPages starts cfg.PageClients clients loading the system page across
// the window, each cfg.PageLoads times at an even spacing, the clients
// staggered across one spacing so their loads overlap as operators' would.
// The function it returns waits for them and gives their loads.
func (h *harness) loadPages(ctx context.Context, cfg ScaleConfig, system string) func() PageRun {
	run := PageRun{Clients: cfg.PageClients}
	if cfg.PageClients <= 0 {
		return func() PageRun { return run }
	}
	h.say("loading the system page with %d clients, %d loads each", cfg.PageClients, cfg.PageLoads)
	gap := cfg.Window / time.Duration(max(cfg.PageLoads, 1))
	base := h.env["LAB_HOST_URL"]
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for c := range cfg.PageClients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sleep(ctx, gap*time.Duration(c)/time.Duration(cfg.PageClients)) != nil {
				return
			}
			for range cfg.PageLoads {
				due := time.Now().Add(gap)
				load := h.loadSystemPage(ctx, base, system)
				mu.Lock()
				run.Loads = append(run.Loads, load)
				mu.Unlock()
				if sleep(ctx, time.Until(due)) != nil {
					return
				}
			}
		}()
	}
	return func() PageRun {
		wg.Wait()
		return run
	}
}

// uptime is how long the server container had been running at t, so a
// sample shows a restart as its uptime falling.
func (h *harness) uptime(ctx context.Context, server string, t time.Time) (float64, error) {
	r, err := h.dc.ContainerInspect(ctx, server, client.ContainerInspectOptions{})
	if err != nil {
		return 0, fmt.Errorf("read the server's start: %w", err)
	}
	if r.Container.State == nil {
		return 0, errors.New("read the server's start: the container reports no state")
	}
	started, err := time.Parse(time.RFC3339Nano, r.Container.State.StartedAt)
	if err != nil {
		return 0, fmt.Errorf("read the server's start: %w", err)
	}
	return t.Sub(started).Seconds(), nil
}

// SampleScale samples the scale lab already running every cfg.Every, one
// line a sample to out, until cfg.Samples are written or stop is cancelled
// (#1632). The lab is found and warmed up once; each sample is one window,
// with no retention probe, since that restarts the server mid-series.
func SampleScale(stop context.Context, cfg ScaleConfig, out io.Writer) error {
	dc, err := client.New(client.WithHost(cfg.DockerHost))
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer dc.Close()
	h := &harness{
		cfg:  Config{Project: lab.ScaleProject, Out: cfg.Out, LabLog: cfg.LabLog},
		dc:   dc,
		http: &http.Client{Timeout: 2 * time.Minute},
	}
	s, err := h.prepareScale(stop, cfg)
	if err != nil {
		return err
	}
	if sleep(stop, cfg.Warmup) != nil {
		return nil
	}
	return RunSamples(stop, SampleLoop{Every: cfg.Every, Samples: cfg.Samples, Out: out,
		Measure: func(ctx context.Context, seq int) (SampleResult, error) {
			h.say("sample %d", seq)
			run := s.fleet
			run.Started = time.Now()
			end, err := h.measureWindow(ctx, cfg, s, &run)
			if err != nil {
				return SampleResult{}, err
			}
			up, err := h.uptime(ctx, s.l.server, end)
			if err != nil {
				return SampleResult{}, err
			}
			return SampleResult{Report: SummarizeScale(run), End: end, UptimeS: up}, nil
		}})
}

func totalMillicores(w Window) float64 {
	var t float64
	for _, c := range w.Containers {
		t += c.Millicores
	}
	return t
}

// readScale takes one reading of the database and of every container.
func (h *harness) readScale(ctx context.Context, l scaleLab) (DBCounters, map[string]Usage, error) {
	out, err := h.psql(ctx, DBCountersQuery)
	if err != nil {
		return DBCounters{}, nil, fmt.Errorf("read the database counters: %w", err)
	}
	db, err := ParseDBCounters(out)
	if err != nil {
		return DBCounters{}, nil, err
	}
	use, err := h.readUsage(ctx, l)
	if err != nil {
		return DBCounters{}, nil, err
	}
	return db, use, nil
}

// readUsage reads every container's cgroup, the containers read together so
// a large fleet's reading is seconds, not a minute, apart.
func (h *harness) readUsage(ctx context.Context, l scaleLab) (map[string]Usage, error) {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		use  = map[string]Usage{}
		errs []error
	)
	for name, id := range l.containers() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := h.usage(ctx, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("read the %s container: %w", name, err))
				return
			}
			use[name] = u
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
		return nil, errors.Join(errs...)
	}
	return use, nil
}

// workingSetReads is how many times a window reads every container's usage
// across it besides its ends: a read every 10 s over the gate's 5 minute
// window, so the working set it reports is the median of about 30 reads, not
// wherever in the Go heap's collection cycle the window's end landed (#1751).
const workingSetReads = 30

// sampleUsage reads every container's usage on a steady tick of gap, from now
// until it is stopped, so the window's working set has reads spread across it
// to take the median of (MedianWorkingSets). The function it returns stops
// the reads and waits for them, then gives them by container, or the first
// read that failed, which ends them; called again, it gives the same. A read
// the stop itself cuts short is no error: the window it served has closed.
func sampleUsage(ctx context.Context, gap time.Duration, read func(context.Context) (map[string]Usage, error)) func() (map[string][]Usage, error) {
	ctx, stop := context.WithCancel(ctx)
	var (
		reads  = map[string][]Usage{}
		failed error
		done   = make(chan struct{})
	)
	go func() {
		defer close(done)
		tick := time.NewTicker(max(gap, time.Millisecond))
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			use, err := read(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				failed = err
				return
			}
			for name, u := range use {
				reads[name] = append(reads[name], u)
			}
		}
	}()
	return sync.OnceValues(func() (map[string][]Usage, error) {
		stop()
		<-done
		return reads, failed
	})
}

// readFleetView reads the console's whole-fleet view, as its Explore page
// does on load.
func (h *harness) readFleetView(ctx context.Context) error {
	_, err := h.fleetView(ctx)
	return err
}

// fleetView is the console's whole-fleet view's answer.
func (h *harness) fleetView(ctx context.Context) ([]byte, error) {
	var raw json.RawMessage
	if err := h.get(ctx, h.env["LAB_HOST_URL"]+"/api/v1/views/fleet", &raw); err != nil {
		return nil, fmt.Errorf("read the fleet view: %w", err)
	}
	return raw, nil
}

// lagSampleRows bounds the lag query: the newest observed samples of the
// window, whose commit times it reads.
const lagSampleRows = 20000

// ingestLagQuery is the seconds from a sample's probe (its ts, stamped at
// the node) to its commit, for the observed samples the window landed.
func ingestLagQuery(from, to time.Time) string {
	return fmt.Sprintf(`select extract(epoch from pg_xact_commit_timestamp(xmin) - ts) from metric
		where ts >= to_timestamp(%.3f) and ts < to_timestamp(%.3f) and source_rule is null
		order by ts desc limit %d`,
		float64(from.UnixMilli())/1000, float64(to.UnixMilli())/1000, lagSampleRows)
}

// lagAgesQuery is how stale the newest landed observed samples are, among
// the newest by probe time: the age of the newest commit, of the newest
// probe, and of that probe before the window's end.
func lagAgesQuery(to time.Time) string {
	return fmt.Sprintf(`select extract(epoch from now() - max(pg_xact_commit_timestamp(xmin))),
			extract(epoch from now() - max(ts)),
			extract(epoch from to_timestamp(%.3f) - max(ts))
		from (select ts, xmin from metric where source_rule is null order by ts desc limit %d) newest`,
		float64(to.UnixMilli())/1000, lagSampleRows)
}

// settlePoll is how often the delivery read re-reads while it settles.
const settlePoll = 5 * time.Second

// readDelivery settles, then reads, the receipts the delivery table
// reconciles: it re-reads until every boot with a batch minted in the window
// has landed one minted after it, whose resolved reaches past the window, and
// the server has gauged its telemetry consumer's ack floor past the stream's
// last sequence as read after every such batch was published (#1758), or
// until the bound. What has not settled by then is read as unsettled, not
// guessed at. A read that fails leaves the table not measured, with why; the
// run's other figures still print.
func (h *harness) readDelivery(ctx context.Context, w delivery.Window, bound time.Duration) DeliveryRead {
	if w.To.Sub(w.From) > MaxDeliveryWindow {
		return DeliveryRead{Reason: "the window is over 23 h, and receipts are pruned at a day"}
	}
	deadline := time.Now().Add(bound)
	for {
		out, err := h.psql(ctx, ReceiptsQuery(w.From, w.To))
		if err != nil {
			return DeliveryRead{Reason: fmt.Sprintf("the receipts did not read (a lab from before this build's ledger has none: bring it up again): %v", err)}
		}
		rs, err := ParseReceipts(out)
		if err != nil {
			return DeliveryRead{Reason: err.Error()}
		}
		var backlog Backlog
		if out, err := h.psql(ctx, BacklogQuery(w.To)); err == nil {
			backlog = ParseBacklog(out)
		}
		d := DeliveryReadOf(w, rs, backlog)
		if d.StreamClear && len(delivery.Unsettled(w, rs)) == 0 || !time.Now().Before(deadline) {
			return h.readSchedule(ctx, d)
		}
		if err := sleep(ctx, settlePoll); err != nil {
			return DeliveryRead{Reason: err.Error()}
		}
	}
}

// readSchedule adds the schedule hop (#1693) to a settled read: the nodes
// report it in the batches the settle waited on.
func (h *harness) readSchedule(ctx context.Context, d DeliveryRead) DeliveryRead {
	out, err := h.psql(ctx, ScheduleQuery(d.Window.From, d.Window.To))
	if err != nil {
		return DeliveryRead{Reason: fmt.Sprintf("the schedule hop did not read: %v", err)}
	}
	if d.Schedule, err = ParseSchedule(out); err != nil {
		return DeliveryRead{Reason: err.Error()}
	}
	return d
}

// ingestLag reads the lag of the samples the window landed. When none had,
// it returns no lag and the gap that says how stale the newest landed sample
// was: the run still has every other figure to report.
func (h *harness) ingestLag(ctx context.Context, from, to time.Time) ([]float64, *LagGap, error) {
	out, err := h.psql(ctx, ingestLagQuery(from, to))
	if err != nil {
		return nil, nil, fmt.Errorf("read the ingest lag: %w", err)
	}
	lags, err := ParseLagSeconds(out)
	if err != nil {
		return nil, nil, err
	}
	if len(lags) > 0 {
		return lags, nil, nil
	}
	out, err = h.psql(ctx, lagAgesQuery(to))
	if err != nil {
		return nil, nil, fmt.Errorf("read the ingest lag gap: %w", err)
	}
	ages, err := ParseLagAges(out)
	if err != nil {
		return nil, nil, err
	}
	gap := NewLagGap(ages, to.Sub(from))
	return nil, &gap, nil
}

// serverLog is what the server container logged since a time.
func (h *harness) serverLog(ctx context.Context, server string, since time.Time) (string, error) {
	r, err := h.dc.ContainerLogs(ctx, server, client.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Since: strconv.FormatInt(since.Unix(), 10)})
	if err != nil {
		return "", fmt.Errorf("read the server's log: %w", err)
	}
	defer r.Close()
	var log bytes.Buffer
	if _, err := stdcopy.StdCopy(&log, &log, r); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read the server's log: %w", err)
	}
	return log.String(), nil
}

// redeliveries counts the redelivery lines the server logged since the
// window began.
func (h *harness) redeliveries(ctx context.Context, server string, since time.Time) (Redeliveries, error) {
	log, err := h.serverLog(ctx, server, since)
	if err != nil {
		return Redeliveries{}, err
	}
	return CountRedeliveries(strings.NewReader(log))
}

// consumerLane reads the lane tallies the server logged since the window
// began and measures the lane between the first and last of them inside it.
func (h *harness) consumerLane(ctx context.Context, server string, from, to time.Time) (Lane, error) {
	log, err := h.serverLog(ctx, server, from)
	if err != nil {
		return Lane{}, err
	}
	samples, err := ParseLaneLog(strings.NewReader(log))
	if err != nil {
		return Lane{}, err
	}
	return LaneOf(samples, from, to), nil
}
