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

// ScaleConfig is one scale measurement (#1556).
type ScaleConfig struct {
	// Binary, when set, is the omniglass binary whose `lab run` brings up a
	// throwaway scale lab of Systems systems for the run and tears it down
	// after. Empty attaches to the scale lab already running.
	Binary   string
	Scenario string
	Systems  int
	// DockerHost is the daemon the lab runs on.
	DockerHost string
	// ReadyTimeout bounds a bring-up (lab run's own --ready-timeout).
	ReadyTimeout time.Duration
	Warmup       time.Duration
	Window       time.Duration
	// FleetReads is how many console fleet-view reads are timed, spread
	// evenly over the window.
	FleetReads int
	// Backlog is how many aged rows the retention drain probe plants; 0
	// skips the probe, which restarts the server.
	Backlog int64
	// DrainTimeout bounds the wait for the pass to clear the backlog.
	DrainTimeout time.Duration
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
	cmd := exec.Command(cfg.Binary, "lab", "run", "--scenario", cfg.Scenario, "--systems", strconv.Itoa(cfg.Systems),
		"--port", "0", "--ready-file", ready, "--while-pid", strconv.Itoa(os.Getpid()),
		"--ready-timeout", cfg.ReadyTimeout.String())
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
			l.server, l.image = c.ID, c.Image
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

func (h *harness) measureScale(ctx context.Context, cfg ScaleConfig) (ScaleReport, error) {
	l, err := h.discoverScale(ctx)
	if err != nil {
		return ScaleReport{}, err
	}
	h.env = map[string]string{"LAB_DB": l.db, "LAB_SERVER": l.server}
	base, err := h.hostURL(ctx, l.server)
	if err != nil {
		return ScaleReport{}, err
	}
	h.env["LAB_HOST_URL"] = base
	if h.env["LAB_TOKEN"], err = h.signIn(ctx, base); err != nil {
		return ScaleReport{}, err
	}
	if on, err := h.psql(ctx, `show track_commit_timestamp`); err != nil || strings.TrimSpace(on) != "on" {
		return ScaleReport{}, errors.New("the lab's Postgres does not record commit timestamps, which the ingest lag reads: stop this lab and bring it up again with this build's `lab run`")
	}

	run := ScaleRun{Scenario: cfg.Scenario, Started: time.Now(), Warmup: cfg.Warmup, Window: cfg.Window, Sites: len(l.nodes)}
	if run.Scenario == "" {
		run.Scenario = lab.DefaultScenario
	}
	if img, err := h.dc.ImageInspect(ctx, l.image); err == nil && img.Config != nil {
		run.Build = img.Config.Labels[lab.VersionLabel]
	}
	fleet, err := h.psql(ctx, `select (select count(*) from system), (select count(*) from component), (select count(*) from task where enabled)`)
	if err != nil {
		return ScaleReport{}, fmt.Errorf("read the fleet's size: %w", err)
	}
	if f := strings.Split(strings.TrimSpace(fleet), "|"); len(f) == 3 {
		run.Systems, _ = strconv.Atoi(f[0])
		run.Components, _ = strconv.Atoi(f[1])
		run.Tasks, _ = strconv.Atoi(f[2])
	}
	// One read before anything is timed, so an API that refuses the fleet
	// view fails the run in seconds, not after its window.
	if err := h.readFleetView(ctx); err != nil {
		return ScaleReport{}, err
	}
	h.say("ready: %d systems, %d components, %d enabled tasks, %d node containers; warming up for %s",
		run.Systems, run.Components, run.Tasks, len(l.nodes), cfg.Warmup)
	if err := sleep(ctx, cfg.Warmup); err != nil {
		return ScaleReport{}, err
	}

	// The closed loop's marks sit outside the window's own reads. A lab
	// whose nodes do not answer for their endpoints still measures the rest.
	start, endpointErr := h.markStart(ctx, l)
	db0, use0, err := h.readScale(ctx, l)
	if err != nil {
		return ScaleReport{}, err
	}
	h.say("measuring for %s, with %d fleet-view reads", cfg.Window, cfg.FleetReads)
	end := time.Now().Add(cfg.Window)
	gap := cfg.Window / time.Duration(max(cfg.FleetReads, 1))
	for i := 0; i < cfg.FleetReads; i++ {
		if err := sleep(ctx, gap); err != nil {
			return ScaleReport{}, err
		}
		start := time.Now()
		if err := h.readFleetView(ctx); err != nil {
			return ScaleReport{}, err
		}
		run.FleetView = append(run.FleetView, time.Since(start))
	}
	if err := sleep(ctx, time.Until(end)); err != nil {
		return ScaleReport{}, err
	}
	db1, use1, err := h.readScale(ctx, l)
	if err != nil {
		return ScaleReport{}, err
	}
	var stop endpointMark
	if endpointErr == nil {
		stop, endpointErr = h.markEnd(ctx, l)
	}
	if run.Measured, err = WindowOf(db0, db1, use0, use1); err != nil {
		return ScaleReport{}, err
	}
	h.say("window: %s metric rows a minute, %.0f millicores in all", count(run.Measured.MetricRowsPerMinute), totalMillicores(run.Measured))

	if run.Lag, run.LagGap, err = h.ingestLag(ctx, db0.At, db1.At); err != nil {
		return ScaleReport{}, err
	}
	if run.Redeliveries, err = h.redeliveries(ctx, l.server, db0.At); err != nil {
		return ScaleReport{}, err
	}
	if run.Lane, err = h.consumerLane(ctx, l.server, db0.At, db1.At); err != nil {
		return ScaleReport{}, err
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
	if cfg.Backlog > 0 {
		if run.Drain, err = h.drainProbe(ctx, l.server, cfg); err != nil {
			return ScaleReport{}, err
		}
	}
	return SummarizeScale(run), nil
}

func totalMillicores(w Window) float64 {
	var t float64
	for _, c := range w.Containers {
		t += c.Millicores
	}
	return t
}

// readScale takes one reading of the database and of every container, the
// containers read together so a large fleet's reading is seconds, not a
// minute, apart.
func (h *harness) readScale(ctx context.Context, l scaleLab) (DBCounters, map[string]Usage, error) {
	out, err := h.psql(ctx, DBCountersQuery)
	if err != nil {
		return DBCounters{}, nil, fmt.Errorf("read the database counters: %w", err)
	}
	db, err := ParseDBCounters(out)
	if err != nil {
		return DBCounters{}, nil, err
	}
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
		return DBCounters{}, nil, errors.Join(errs...)
	}
	return db, use, nil
}

// readFleetView reads the console's whole-fleet view, as its Explore page
// does on load.
func (h *harness) readFleetView(ctx context.Context) error {
	if err := h.get(ctx, h.env["LAB_HOST_URL"]+"/api/v1/views/fleet", nil); err != nil {
		return fmt.Errorf("read the fleet view: %w", err)
	}
	return nil
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
// the server has gauged its telemetry consumer empty since the window closed,
// or until the bound. What has not settled by then is read as unsettled, not
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
		d := DeliveryRead{Read: true, Window: w, Receipts: rs, StreamClear: backlog.Clear(), Backlog: backlog}
		if d.StreamClear && len(delivery.Unsettled(w, rs)) == 0 || !time.Now().Before(deadline) {
			return d
		}
		if err := sleep(ctx, settlePoll); err != nil {
			return DeliveryRead{Reason: err.Error()}
		}
	}
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
