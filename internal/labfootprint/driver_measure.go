package labfootprint

import (
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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/client"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The fit bounds a driver's steps must meet to be written down as a cost:
// memory is the steadier read, CPU the noisier (a poll's work lands in
// bursts at its period).
const (
	driverMemResidual = 0.1
	driverCPUResidual = 0.25
)

// simAttachWait is how long a sweep waits for the simulator beside a fresh
// node to connect before its first component is refused for good.
const simAttachWait = 90 * time.Second

// DriverConfig is one driver cost sweep.
type DriverConfig struct {
	// Binary is the omniglass binary whose `lab run` brings the lab up.
	Binary string
	// DockerHost is the daemon the lab runs on.
	DockerHost string
	// Project is the throwaway lab's compose project, its own.
	Project string
	// Image is the build measured; empty is the lab's default.
	Image string
	// StepTasks is about how many tasks each step adds.
	StepTasks int
	// Warmup is how long a step runs before it is measured; Window is the
	// least a step is measured for (a driver's slowest poll lengthens it).
	Warmup time.Duration
	Window time.Duration
	// Only, when set, sweeps just the drivers it names.
	Only []string
	// Out is where progress goes; the lab's own output goes to LabLog.
	Out    io.Writer
	LabLog io.Writer
}

// DriverCostTable is a sweep's result: each measured driver's cost, and
// each driver it did not measure with the reason.
type DriverCostTable struct {
	Host        *Fingerprint `json:"host,omitempty"`
	Date        string       `json:"date"`
	Build       string       `json:"build"`
	StepTasks   int          `json:"step_tasks"`
	Rows        []DriverCost `json:"rows"`
	NotMeasured []DriverSkip `json:"not_measured"`
}

// driverRunArgs is the `lab run` a sweep brings its lab up with: ephemeral,
// on its own project and a free port, bound to this process, with no fleet
// and no node of its own, since the sweep starts a node per driver.
func driverRunArgs(cfg DriverConfig, ready string, pid int) []string {
	args := []string{"lab", "run", "--ephemeral", "--project", cfg.Project, "--port", "0",
		"--ready-file", ready, "--while-pid", strconv.Itoa(pid),
		"--scenario", "empty", "--no-node", "--cadence", lab.CadenceLab}
	if cfg.Image != "" {
		args = append(args, "--image", cfg.Image)
	}
	return args
}

// pickDrivers narrows a plan to the drivers only names, or keeps it whole
// when only is empty. A name the plan does not hold is refused, so a typo is
// not a sweep that measures nothing.
func pickDrivers(targets []DriverTarget, skips []DriverSkip, only []string) ([]DriverTarget, []DriverSkip, error) {
	if len(only) == 0 {
		return targets, skips, nil
	}
	var t []DriverTarget
	var s []DriverSkip
	var unknown []string
	for _, name := range only {
		if i := slices.IndexFunc(targets, func(x DriverTarget) bool { return x.Driver == name }); i >= 0 {
			t = append(t, targets[i])
		} else if i := slices.IndexFunc(skips, func(x DriverSkip) bool { return x.Driver == name }); i >= 0 {
			s = append(s, skips[i])
		} else {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return nil, nil, fmt.Errorf("no shipped driver is named %s", strings.Join(unknown, ", "))
	}
	return t, s, nil
}

// stepComponents are the component counts a driver is measured at: none,
// then n, then 2n, so the fit has an idle node and two loads.
func stepComponents(n int) []int { return []int{0, n, 2 * n} }

func sweepComponentName(node string, i int) string { return fmt.Sprintf("%s-%04d", node, i) }

// secretBody is a sim secret as the create route takes it.
func secretBody(s storage.SecretSpec) map[string]any {
	b := map[string]any{"name": s.Name, "secret_type": s.SecretType, "owner_kind": s.OwnerKind, "fields": s.Fields}
	if s.Label != "" {
		b["label"] = s.Label
	}
	if s.OwnerName != nil {
		b["owner"] = *s.OwnerName
	}
	if s.AdminSensitive != nil {
		b["admin_sensitive"] = *s.AdminSensitive
	}
	return b
}

// reportSettle is how long after a window its poll CPU has landed: two of
// the lab's collection report windows, so the window that held the last run
// has closed and its batch is written.
func reportSettle(report time.Duration) time.Duration { return 2 * report }

// reportWindowOf reads the collection report window from the settings API's
// answer: the lab's live value, which an operator or a cadence may have
// moved off the shipped default.
func reportWindowOf(body []byte) (time.Duration, error) {
	var v struct {
		Values struct {
			Collection struct {
				ReportSeconds int `json:"report_seconds"`
			} `json:"collection"`
		} `json:"values"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, fmt.Errorf("read the settings: %w", err)
	}
	if v.Values.Collection.ReportSeconds <= 0 {
		return 0, errors.New("the settings have no positive collection report_seconds")
	}
	return time.Duration(v.Values.Collection.ReportSeconds) * time.Second, nil
}

// driverTasksWait is how long a step waits for its tasks to reach the count
// its components should give, since the attach compiles them a moment after
// the add answers.
const driverTasksWait = 30 * time.Second

// MeasureDrivers brings a throwaway lab up with no fleet, measures each
// shipped driver on a node of its own, tears the lab down, and checks
// nothing it created is left. A driver whose steps do not fit a line is
// listed as not measured, with why, rather than written down as a cost.
func MeasureDrivers(ctx context.Context, cfg DriverConfig) (table DriverCostTable, err error) {
	if cfg.Project == "" || cfg.Project == lab.DefaultProject || cfg.Project == lab.ScaleProject {
		return DriverCostTable{}, fmt.Errorf("a driver sweep needs a throwaway project of its own, never %s or %s", lab.DefaultProject, lab.ScaleProject)
	}
	if cfg.StepTasks <= 0 {
		return DriverCostTable{}, errors.New("a driver sweep needs a positive step size")
	}
	targets, skips := ShippedDriverSweep()
	targets, skips, err = pickDrivers(targets, skips, cfg.Only)
	if err != nil {
		return DriverCostTable{}, err
	}
	dc, err := client.New(client.WithHost(cfg.DockerHost))
	if err != nil {
		return DriverCostTable{}, fmt.Errorf("docker: %w", err)
	}
	defer dc.Close()
	eng, err := lab.NewDockerEngine(cfg.DockerHost)
	if err != nil {
		return DriverCostTable{}, fmt.Errorf("docker: %w", err)
	}
	h := &harness{cfg: Config{Project: cfg.Project, Out: cfg.Out, LabLog: cfg.LabLog},
		dc: dc, http: &http.Client{Timeout: 2 * time.Minute}}

	left, err := h.leftovers(ctx)
	if err != nil {
		return DriverCostTable{}, err
	}
	if len(left) > 0 {
		return DriverCostTable{}, fmt.Errorf("project %s is already up (%s): stop it first", cfg.Project, strings.Join(left, ", "))
	}
	dir, err := os.MkdirTemp("", "labdrivers-")
	if err != nil {
		return DriverCostTable{}, err
	}
	defer os.RemoveAll(dir)
	ready := filepath.Join(dir, "lab.env")
	h.say("bringing up the throwaway lab %s with no fleet, to sweep %d drivers", cfg.Project, len(targets))
	cmd := exec.Command(cfg.Binary, driverRunArgs(cfg, ready, os.Getpid())...)
	cmd.Stdout, cmd.Stderr = cfg.LabLog, cfg.LabLog
	if err := cmd.Start(); err != nil {
		return DriverCostTable{}, fmt.Errorf("start the lab: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		if derr := h.down(cmd, exited); derr != nil && err == nil {
			err = derr
		}
	}()
	if h.env, err = waitReady(ctx, ready, exited); err != nil {
		return DriverCostTable{}, err
	}

	base := h.env["LAB_HOST_URL"]
	tok, err := h.signIn(ctx, base)
	if err != nil {
		return DriverCostTable{}, fmt.Errorf("sign in: %w", err)
	}
	h.tokens = newTokenSource(tok, func(ctx context.Context) (string, error) { return h.signIn(ctx, base) })
	api := base + "/api/v1"
	// The sim secrets first: a component added before the secret its attach
	// references falls back to a probe, which is not the driver.
	for _, s := range devseed.SimSecretSpecs() {
		if code, err := h.send(ctx, http.MethodPost, api+"/secrets", secretBody(s), nil); err != nil && code != http.StatusConflict {
			return DriverCostTable{}, fmt.Errorf("create the sim secret %s: %w", s.Name, err)
		}
	}

	var settingsBody json.RawMessage
	if _, err := h.send(ctx, http.MethodGet, api+"/settings", nil, &settingsBody); err != nil {
		return DriverCostTable{}, fmt.Errorf("read the settings: %w", err)
	}
	report, err := reportWindowOf(settingsBody)
	if err != nil {
		return DriverCostTable{}, err
	}

	table = DriverCostTable{Date: time.Now().UTC().Format(time.DateOnly), StepTasks: cfg.StepTasks, NotMeasured: skips}
	if img, err := h.dc.ImageInspect(ctx, h.env["LAB_IMAGE"]); err == nil && img.Config != nil {
		table.Build = img.Config.Labels[lab.VersionLabel]
	}
	if cfg.DockerHost == "" || lab.IsLocal(cfg.DockerHost) {
		fp := ReadFingerprint()
		table.Host = &fp
	}
	for i, t := range targets {
		h.say("[%d/%d] %s on %s, polls %v", i+1, len(targets), t.Driver, t.Product, t.Polls)
		row, err := h.measureDriver(ctx, eng, api, cfg, t, report)
		if ctx.Err() != nil {
			return table, ctx.Err()
		}
		if err != nil {
			h.say("  not measured: %v", err)
			table.NotMeasured = append(table.NotMeasured, DriverSkip{Driver: t.Driver, Reason: err.Error()})
			continue
		}
		h.say("  %.0f B and %.2f millicores per task, %.3f ms per run (the thread clock read %.3f ms)",
			row.MemoryPerTaskBytes, row.MillicoresPerTask, row.CPUPerRunMillis, row.ThreadCPUPerRunMillis)
		table.Rows = append(table.Rows, row)
	}
	slices.SortFunc(table.NotMeasured, func(a, b DriverSkip) int { return strings.Compare(a.Driver, b.Driver) })
	return table, nil
}

// stepWindow is when one step was measured.
type stepWindow struct{ from, to time.Time }

// measureDriver measures one driver on a node of its own, its simulator
// beside it, and removes both whatever the outcome.
func (h *harness) measureDriver(ctx context.Context, eng lab.Engine, api string, cfg DriverConfig, t DriverTarget, report time.Duration) (DriverCost, error) {
	node := sweepNodeName(t.Driver)
	if _, err := h.send(ctx, http.MethodPost, api+"/nodes", map[string]string{"name": node}, nil); err != nil {
		return DriverCost{}, fmt.Errorf("create the node: %w", err)
	}
	var enrolled struct {
		Token string `json:"token"`
	}
	if _, err := h.send(ctx, http.MethodPost, api+"/nodes/"+url.PathEscape(node)+":enroll", nil, &enrolled); err != nil {
		return DriverCost{}, fmt.Errorf("enroll the node: %w", err)
	}
	nodeSvc, err := sweepNodeService(h.env, node, enrolled.Token)
	if err != nil {
		return DriverCost{}, err
	}
	// The removal is set before the start's error is checked: a container
	// created but not started comes back with its id and the error, and is
	// still removed.
	var nodeID, simID string
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if simID != "" {
			_ = eng.StopAndRemove(cctx, simID)
		}
		if nodeID != "" {
			_ = eng.StopAndRemove(cctx, nodeID)
		}
	}()
	if nodeID, err = eng.RunContainer(ctx, nodeSvc, h.env["LAB_NETWORK"]); err != nil {
		return DriverCost{}, fmt.Errorf("start the node: %w", err)
	}
	simSvc, err := sweepSimService(h.env, node, enrolled.Token, nodeID)
	if err != nil {
		return DriverCost{}, err
	}
	if simID, err = eng.RunContainer(ctx, simSvc, h.env["LAB_NETWORK"]); err != nil {
		return DriverCost{}, fmt.Errorf("start the simulator: %w", err)
	}

	per := componentsPerStep(cfg.StepTasks, t.Polls)
	window := sweepWindow(cfg.Window, t.Polls)
	warmup := sweepWindow(cfg.Warmup, t.Polls)
	var steps []DriverStep
	var windows []stepWindow
	added := 0
	for _, want := range stepComponents(per) {
		for ; added < want; added++ {
			if err := h.addSimComponent(ctx, api, node, sweepComponentName(node, added), t.Product, added == 0); err != nil {
				return DriverCost{}, err
			}
		}
		tasks, err := h.waitDriverTasks(ctx, node, want*len(t.Polls))
		if err != nil {
			return DriverCost{}, fmt.Errorf("%d components: %w", want, err)
		}
		h.say("  step: %d components, %d tasks; warming up %s, then measuring %s", want, tasks, warmup, window)
		if err := sleep(ctx, warmup); err != nil {
			return DriverCost{}, err
		}
		s, w, err := h.measureStep(ctx, nodeID, window)
		if err != nil {
			return DriverCost{}, err
		}
		s.Components, s.Tasks = want, tasks
		h.say("    working set %.1f MiB, %.1f millicores", s.WorkingSetBytes/(1<<20), s.Millicores)
		steps = append(steps, s)
		windows = append(windows, w)
	}
	// The thread clock's figure lands with each report window, after it
	// closes: wait for the last to land, then read every step's.
	if err := sleep(ctx, reportSettle(report)); err != nil {
		return DriverCost{}, err
	}
	for i, w := range windows {
		out, err := h.psql(ctx, pollCPUQuery(node, w.from, w.to, report))
		if err != nil {
			return DriverCost{}, fmt.Errorf("read the poll CPU: %w", err)
		}
		if steps[i].PollCPUMillis, err = strconv.ParseFloat(strings.TrimSpace(out), 64); err != nil {
			return DriverCost{}, fmt.Errorf("read the poll CPU: %q: %w", out, err)
		}
	}
	return AssembleDriverCost(t, steps, driverMemResidual, driverCPUResidual)
}

// waitDriverTasks waits for node's driver tasks to reach want, and fails if
// they settle anywhere else: a count short of it means an attach fell back to
// a probe, which is not the driver.
func (h *harness) waitDriverTasks(ctx context.Context, node string, want int) (int, error) {
	deadline := time.Now().Add(driverTasksWait)
	for {
		out, err := h.psql(ctx, driverTasksQuery(node))
		if err != nil {
			return 0, fmt.Errorf("count the node's tasks: %w", err)
		}
		tasks, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return 0, fmt.Errorf("count the node's tasks: %q: %w", out, err)
		}
		if tasks == want {
			return tasks, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("%d driver tasks, want %d: the attach fell back", tasks, want)
		}
		if err := sleep(ctx, 3*time.Second); err != nil {
			return 0, err
		}
	}
}

// addSimComponent adds one simulated component to node. The first waits out
// a 409 while the fresh simulator connects; later ones do not, since a 409
// then means the block is full or the profile serves nothing to reach.
func (h *harness) addSimComponent(ctx context.Context, api, node, name, product string, first bool) error {
	u := api + "/nodes/" + url.PathEscape(node) + "/simulation/components"
	body := map[string]string{"name": name, "product": product}
	deadline := time.Now().Add(simAttachWait)
	for {
		code, err := h.send(ctx, http.MethodPost, u, body, nil)
		if err == nil {
			return nil
		}
		if !first || code != http.StatusConflict || time.Now().After(deadline) {
			return fmt.Errorf("add the component %s: %w", name, err)
		}
		if err := sleep(ctx, 3*time.Second); err != nil {
			return err
		}
	}
}

// measureStep reads the node's cgroup across one window: CPU between its
// ends, and the working set as the median of reads spread across it.
func (h *harness) measureStep(ctx context.Context, nodeID string, window time.Duration) (DriverStep, stepWindow, error) {
	first, err := h.usage(ctx, nodeID)
	if err != nil {
		return DriverStep{}, stepWindow{}, fmt.Errorf("read the node: %w", err)
	}
	stop := sampleUsage(ctx, window/workingSetReads, func(ctx context.Context) (map[string]Usage, error) {
		u, err := h.usage(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		return map[string]Usage{"node": u}, nil
	})
	if err := sleep(ctx, window); err != nil {
		_, _ = stop()
		return DriverStep{}, stepWindow{}, err
	}
	reads, err := stop()
	if err != nil {
		return DriverStep{}, stepWindow{}, fmt.Errorf("read the node: %w", err)
	}
	last, err := h.usage(ctx, nodeID)
	if err != nil {
		return DriverStep{}, stepWindow{}, fmt.Errorf("read the node: %w", err)
	}
	mc, err := Millicores(first, last)
	if err != nil {
		return DriverStep{}, stepWindow{}, fmt.Errorf("the node %w", err)
	}
	return DriverStep{WorkingSetBytes: MedianWorkingSetMiB(reads["node"]) * (1 << 20), Millicores: mc},
		stepWindow{from: first.At, to: last.At}, nil
}
