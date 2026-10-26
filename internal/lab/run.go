package lab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Runner realizes a Spec through the Engine: preflight, bring-up, readiness,
// hold, and teardown.
type Runner struct {
	Engine Engine
	Daemon Daemon
	Out    io.Writer
	// Host is this machine's name, recorded as the owner of what the lab
	// creates; Alive reports whether an owner pid on this host still runs.
	Host  string
	Alive func(pid int) bool
	// PortFree checks the console's address before anything is created.
	PortFree func(listen string, port int) error
	// HTTP reaches the lab's console from this machine.
	HTTP *http.Client
	// Poll is the readiness poll interval; ReadyTimeout bounds each wait.
	Poll         time.Duration
	ReadyTimeout time.Duration
}

// Config is one run's settings.
type Config struct {
	Options
	Image ImageChoice
	// Fresh removes the lab's volumes first, so it starts from the seed.
	Fresh bool
	// PlantedFaults are the labels of the simulated endpoints the seed
	// breaks on purpose (things for a learner to find and fix), read from
	// the seed: readiness does not wait for them.
	PlantedFaults []string
	// Hold keeps the lab up until ctx ends, then tears it down. Without
	// it, Run returns once the lab is ready and leaves it running (the
	// integration test's way in); TearDown removes it.
	Hold bool
}

// Lab is a lab that is up.
type Lab struct {
	Spec Spec
	// ConsoleURL is where a person opens the console (the published
	// address); BaseURL is where this machine reaches the API.
	ConsoleURL string
	BaseURL    string
	// NodeRunning is false for a scenario with no node to run.
	NodeRunning bool
	// Pending, when set, says what had not finished coming up when the
	// readiness wait ran out (some endpoints not up yet).
	Pending string
	// Took is how long the lab took to come up.
	Took time.Duration
	ids         map[string]string
}

// ID is the container id of a service.
func (l *Lab) ID(service string) string { return l.ids[service] }

func (r *Runner) say(format string, a ...any) { fmt.Fprintf(r.Out, format+"\n", a...) }

// step prints one stage's line of the bring-up.
func (r *Runner) step(stage, detail string) { r.say("  %-10s %s", stage, detail) }

// Run brings the lab up and, with Hold, keeps it up until ctx ends, then
// tears it down. Every failure before anything is created is a
// PreflightError; a failure after tears down what was created.
func (r *Runner) Run(ctx context.Context, cfg Config) (*Lab, error) {
	start := time.Now()
	spec, err := Plan(cfg.Options)
	if err != nil {
		return nil, err
	}
	if err := r.preflight(ctx, spec, cfg); err != nil {
		return nil, err
	}
	lab, err := r.up(ctx, spec, cfg)
	if err != nil {
		r.say("")
		r.say("The lab did not come up, so it is being removed:")
		r.TearDown(spec)
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return nil, errors.New("interrupted before the lab was ready")
		}
		return nil, err
	}
	lab.Took = time.Since(start).Round(time.Second)
	if !cfg.Hold {
		return lab, nil
	}
	r.readyBanner(lab, cfg)
	err = r.hold(ctx, lab)
	r.say("")
	if err != nil {
		r.say("%v", err)
	}
	r.say("Stopping your lab (press Ctrl-C again to leave it for the next run to clean up).")
	r.TearDown(spec)
	r.say("Stopped. Its data is kept in the volumes %s and %s, so `omniglass lab run` picks up where you left off; --fresh starts over from the seed.",
		spec.Volumes[0].Name, spec.Volumes[1].Name)
	return lab, err
}

// preflight runs every check that comes before anything is created: a live
// lab already holding the project, the console's port, and the image.
func (r *Runner) preflight(ctx context.Context, spec Spec, cfg Config) error {
	if err := r.Reap(ctx, spec.Project); err != nil {
		return err
	}
	if err := r.PortFree(cfg.Listen, cfg.Port); err != nil {
		return err
	}
	labels, found, err := r.Engine.ImageLabels(ctx, cfg.Image.Ref)
	if err != nil {
		return fmt.Errorf("look up the image %s: %w", cfg.Image.Ref, err)
	}
	if err := CheckDevImage(cfg.Image, found, labels); err != nil {
		return &PreflightError{Cause: err.Error(), Fix: "run `make` in your omniglass checkout, then run the lab again.", Err: err}
	}
	return nil
}

// up creates the project and waits until it is ready.
func (r *Runner) up(ctx context.Context, spec Spec, cfg Config) (*Lab, error) {
	lab := &Lab{Spec: spec, ids: map[string]string{}}
	for _, ref := range []struct{ ref string }{{cfg.Image.Ref}, {PostgresImage}} {
		_, found, err := r.Engine.ImageLabels(ctx, ref.ref)
		if err != nil {
			return nil, err
		}
		if found {
			continue
		}
		if ref.ref == cfg.Image.Ref && !cfg.Image.Pull {
			return nil, fmt.Errorf("the image %s is not built; run `make`", ref.ref)
		}
		r.step("image", "pulling "+ref.ref+" (a one-time download)")
		if err := r.Engine.PullImage(ctx, ref.ref); err != nil {
			return nil, fmt.Errorf("pull %s: %w", ref.ref, err)
		}
	}
	r.step("image", fmt.Sprintf("%s (%s)", cfg.Image.Ref, cfg.Image.Why))

	if cfg.Fresh {
		vols, err := r.Engine.Volumes(ctx, ProjectLabels(spec.Project))
		if err != nil {
			return nil, err
		}
		for _, v := range vols {
			if err := r.Engine.RemoveVolume(ctx, v.Name); err != nil {
				return nil, fmt.Errorf("--fresh: remove the volume %s: %w", v.Name, err)
			}
		}
		r.step("data", fmt.Sprintf("--fresh: removed %d volume(s), starting over from the seed", len(vols)))
	}
	for _, v := range spec.Volumes {
		if err := r.Engine.CreateVolume(ctx, v.Name, v.Labels); err != nil {
			return nil, fmt.Errorf("create the volume %s: %w", v.Name, err)
		}
	}
	if err := r.Engine.CreateNetwork(ctx, spec.Network, spec.NetworkLabels); err != nil {
		return nil, fmt.Errorf("create the network %s: %w", spec.Network, err)
	}
	r.step("network", spec.Network+" (a private bridge; only the console is published)")

	id, err := r.Engine.RunContainer(ctx, spec.DB, spec.Network)
	lab.ids["db"] = id
	if err != nil {
		return nil, fmt.Errorf("start the database: %w", err)
	}
	if err := r.waitFor(ctx, "the database to accept connections", func() (bool, error) {
		st, err := r.Engine.Inspect(ctx, id)
		if err != nil {
			return false, err
		}
		if !st.Running {
			return false, r.stopped(ctx, "db", id, st)
		}
		return st.Health == "healthy", nil
	}); err != nil {
		return nil, err
	}
	r.step("database", "Postgres is ready")

	seedID, err := r.Engine.RunContainer(ctx, spec.Seed, spec.Network)
	lab.ids["seed"] = seedID
	if err != nil {
		return nil, fmt.Errorf("start the seed: %w", err)
	}
	code, err := r.Engine.Wait(ctx, seedID)
	if err != nil {
		return nil, fmt.Errorf("the seed: %w", err)
	}
	seedLog, _ := r.Engine.Logs(ctx, seedID, 40)
	if code != 0 {
		return nil, fmt.Errorf("the seed failed (exit %d):\n%s", code, indent(seedLog))
	}
	res := ParseSeedResult(seedLog)
	r.step("seed", res.Summary)
	_ = r.Engine.StopAndRemove(ctx, seedID)
	delete(lab.ids, "seed")

	id, err = r.Engine.RunContainer(ctx, spec.Server, spec.Network)
	lab.ids["server"] = id
	if err != nil {
		return nil, fmt.Errorf("start the server: %w", err)
	}
	var st ContainerState
	if err := r.waitFor(ctx, "the server's published port", func() (bool, error) {
		st, err = r.Engine.Inspect(ctx, id)
		if err != nil {
			return false, err
		}
		return st.Published[consolePort] != "", nil
	}); err != nil {
		return nil, err
	}
	pub := st.Published[consolePort]
	lab.ConsoleURL, lab.BaseURL = consoleURLs(pub)
	if err := r.waitFor(ctx, "the server to answer", func() (bool, error) {
		st, err := r.Engine.Inspect(ctx, id)
		if err != nil {
			return false, err
		}
		if !st.Running && !st.Restarting {
			return false, r.stopped(ctx, "server", id, st)
		}
		resp, err := r.HTTP.Get(lab.BaseURL + "/api/v1/healthz")
		if err != nil {
			return false, nil
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK, nil
	}); err != nil {
		return nil, r.withLogs(ctx, err, "server", id)
	}
	r.step("server", "answering at "+lab.ConsoleURL)

	client, err := r.signIn(ctx, lab.BaseURL)
	if err != nil {
		return nil, err
	}
	r.step("sign in", fmt.Sprintf("the login %s / %s works", LoginUser, LoginPassword))

	if !res.Node {
		r.step("node", "this scenario places no node, so none runs")
		return lab, nil
	}
	id, err = r.Engine.RunContainer(ctx, spec.Node, spec.Network)
	lab.ids["node"] = id
	if err != nil {
		return nil, fmt.Errorf("start the node: %w", err)
	}
	lab.NodeRunning = true
	last, err := r.waitCollecting(ctx, lab, client, cfg.PlantedFaults)
	switch {
	case errors.Is(err, errTimedOut) && last.Running == last.Components && last.Components > 0:
		// Everything runs and the node collects; some endpoints have not read
		// up in time. Worth saying, not worth taking the lab down for.
		lab.Pending = fmt.Sprintf("%d of %d simulated endpoints are not up yet; the console's fleet view shows which, and why.",
			last.Endpoints-last.Up, last.Endpoints)
	case err != nil:
		return nil, r.withLogs(ctx, err, "node", id)
	}
	return lab, nil
}

// errTimedOut is a readiness wait that ran out of time.
var errTimedOut = errors.New("timed out")

// consoleURLs turns the published address into the URL a person opens and
// the base this machine reaches the API at: a wildcard bind is reached on
// loopback.
func consoleURLs(published string) (console, base string) {
	host, port := published, ""
	if i := strings.LastIndex(published, ":"); i >= 0 {
		host, port = published[:i], published[i+1:]
	}
	reach := host
	if host == "0.0.0.0" || host == "[::]" || host == "::" {
		reach = "127.0.0.1"
	}
	return "http://" + reach + ":" + port + "/web", "http://" + reach + ":" + port
}

// signIn signs in with the lab's login, proving it works, and returns a
// client carrying the session for the readiness reads.
func (r *Runner) signIn(ctx context.Context, base string) (*http.Client, error) {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second, Transport: r.HTTP.Transport}
	var last string
	err := r.waitFor(ctx, "the login to sign in", func() (bool, error) {
		body := strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, LoginUser, LoginPassword))
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			last = err.Error()
			return false, nil
		}
		resp.Body.Close()
		last = resp.Status
		return resp.StatusCode == http.StatusNoContent, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w (last answer: %s)", err, last)
	}
	return c, nil
}

// waitCollecting waits until the simulator serves every simulated component
// and every simulated endpoint reads up but those the seed breaks on
// purpose, printing progress as the counts move.
func (r *Runner) waitCollecting(ctx context.Context, lab *Lab, c *http.Client, plantedLabels []string) (Progress, error) {
	planted := map[string]bool{}
	for _, l := range plantedLabels {
		planted[l] = true
	}
	var lastSim, lastCol string
	var last Progress
	err := r.waitFor(ctx, "the node to collect from the simulated fleet", func() (bool, error) {
		if st, err := r.Engine.Inspect(ctx, lab.ID("node")); err == nil && !st.Running && !st.Restarting {
			return false, r.stopped(ctx, "node", lab.ID("node"), st)
		}
		p, err := readProgress(ctx, c, lab.BaseURL, planted)
		if err != nil {
			return false, nil
		}
		last = p
		if s := fmt.Sprintf("%s with its simulator: %d of %d simulated components running", NodeName, p.Running, p.Components); p.Connected && s != lastSim {
			r.step("node", s)
			lastSim = s
		}
		if p.Running < p.Components || p.Components == 0 {
			return false, nil
		}
		s := fmt.Sprintf("%d of %d simulated endpoints up", p.Up, p.Endpoints)
		if p.Planted > 0 {
			s += fmt.Sprintf(", and %d broken on purpose for you to find", p.Planted)
		}
		if s != lastCol {
			r.step("collecting", s)
			lastCol = s
		}
		return p.Endpoints > 0 && p.Up == p.Endpoints, nil
	})
	return last, err
}

// stopped reports a container that stopped while the lab waited on it,
// with its last log lines.
func (r *Runner) stopped(ctx context.Context, service, id string, st ContainerState) error {
	return r.withLogs(ctx, fmt.Errorf("the %s container stopped (%s, exit %d)", service, st.Status, st.ExitCode), service, id)
}

func (r *Runner) withLogs(ctx context.Context, err error, service, id string) error {
	logs, lerr := r.Engine.Logs(context.WithoutCancel(ctx), id, 20)
	if lerr != nil || strings.TrimSpace(logs) == "" {
		return err
	}
	return fmt.Errorf("%w\nThe %s's last log lines:\n%s", err, service, indent(logs))
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

// waitFor polls cond until it is true, it errs, ctx ends, or ReadyTimeout
// passes.
func (r *Runner) waitFor(ctx context.Context, what string, cond func() (bool, error)) error {
	deadline := time.Now().Add(r.ReadyTimeout)
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w after %s waiting for %s", errTimedOut, r.ReadyTimeout, what)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.Poll):
		}
	}
}

// hold keeps the lab up until ctx ends, watching that its long-lived
// containers stay up: one stopped or removed from outside ends the hold,
// saying which.
func (r *Runner) hold(ctx context.Context, lab *Lab) error {
	services := []string{"db", "server"}
	if lab.NodeRunning {
		services = append(services, "node")
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			for _, s := range services {
				st, err := r.Engine.Inspect(ctx, lab.ID(s))
				if ctx.Err() != nil {
					return nil
				}
				if err != nil {
					return fmt.Errorf("the lab's %s container is gone (removed from outside the lab)", s)
				}
				if !st.Running && !st.Restarting {
					return r.stopped(ctx, s, lab.ID(s), st)
				}
			}
		}
	}
}

// TearDown removes every container and the network of the project, and
// keeps its volumes. It runs on its own clock: the run's context has
// usually ended by now.
func (r *Runner) TearDown(spec Spec) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	n, err := r.removeProject(ctx, ProjectLabels(spec.Project))
	if err != nil {
		r.say("  Could not remove everything: %v", err)
		r.say("  Remove what is left with: docker compose -p %s down", spec.Project)
		return
	}
	r.step("removed", fmt.Sprintf("%d container(s) and the network %s", n, spec.Network))
}

// removeProject stops and removes the containers, then the networks, that
// carry labels. The containers stop in parallel.
func (r *Runner) removeProject(ctx context.Context, labels map[string]string) (int, error) {
	cs, err := r.Engine.Containers(ctx, labels)
	if err != nil {
		return 0, err
	}
	var wg sync.WaitGroup
	errs := make([]error, len(cs))
	for i, c := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.Engine.StopAndRemove(ctx, c.ID)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return 0, err
	}
	nets, err := r.Engine.Networks(ctx, labels)
	if err != nil {
		return len(cs), err
	}
	for _, n := range nets {
		if err := r.Engine.RemoveNetwork(ctx, n.Name); err != nil {
			return len(cs), err
		}
	}
	return len(cs), nil
}

// Reap removes what a lab of this project left behind when it was killed too
// hard to tear down (its owner on this host is gone). A lab whose owner is
// alive, or that another host owns, is refused, never touched.
func (r *Runner) Reap(ctx context.Context, project string) error {
	labels := ProjectLabels(project)
	cs, err := r.Engine.Containers(ctx, labels)
	if err != nil {
		return err
	}
	nets, err := r.Engine.Networks(ctx, labels)
	if err != nil {
		return err
	}
	if len(cs) == 0 && len(nets) == 0 {
		return nil
	}
	owners := map[string]bool{}
	for _, res := range append(cs, nets...) {
		host, pid := res.Labels[LabelLabHost], res.Labels[LabelLabPID]
		owners[host+" "+pid] = true
		if host != r.Host {
			return &PreflightError{
				Cause: fmt.Sprintf("A lab named %s is already running, started from another machine (%s).", project, host),
				Fix:   "stop it there, or remove it with `docker compose -p " + project + " down` if you know it is abandoned.",
			}
		}
		if n, err := strconv.Atoi(pid); err == nil && r.Alive(n) {
			return &PreflightError{
				Cause: fmt.Sprintf("A lab is already running here (process %d).", n),
				Fix:   "use that one, or stop it with Ctrl-C in its terminal and run the lab again.",
			}
		}
	}
	r.say("  %-10s a previous lab was stopped too hard to clean up after itself; removing what it left", "cleanup")
	if _, err := r.removeProject(ctx, labels); err != nil {
		return fmt.Errorf("remove what a previous lab left behind: %w", err)
	}
	return nil
}

// readyBanner is what a person needs once the lab is up: where to go, how to
// sign in, how to stop it, and what to do next.
func (r *Runner) readyBanner(lab *Lab, cfg Config) {
	r.say("")
	r.say("Your omniglass lab is ready (it took %s).", lab.Took)
	if lab.Pending != "" {
		r.say("  Note: %s", lab.Pending)
	}
	r.say("")
	r.say("  Console    %s", lab.ConsoleURL)
	r.say("  Sign in    user %s, password %s", LoginUser, LoginPassword)
	r.say("")
	r.say("  Stop it    press Ctrl-C here. Your lab's data is kept for next time;")
	r.say("             omniglass lab run --fresh starts over from the seed.")
	r.say("")
	r.say("Where to go next: the guided walkthroughs, each recorded on this same lab,")
	r.say("so every screen matches yours:")
	r.say("  %s", WalkthroughsURL)
	r.say("")
	r.say("The lab is one Docker project; see it with: docker compose -p %s ps", lab.Spec.Project)
	if cfg.Listen != DefaultListen {
		r.say("")
		r.say("Note: the console is published on %s, not just this machine's loopback: anyone who can reach it can sign in with the well-known login.", cfg.Listen)
	}
}

// WalkthroughsURL is the lab guide's list of guided walkthroughs.
const WalkthroughsURL = "https://docs.omniglass.hyperscaleav.com/guides/lab/#walk-through-it"
