package lab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SiteState is how far one site of a scale lab has come: whether its node's
// simulator has connected, how many of its components the simulator lists of
// those the plan gives the site, and how many of them are up (running, with
// every endpoint the node dials reading up but the planted faults).
type SiteState struct {
	Node      string
	Connected bool
	Want      int
	Listed    int
	Up        int
	// NotUp names the components listed but not up yet, each with its
	// loopback address (a name is unique only within its system).
	NotUp []string
}

// Ready reports whether the site is all the way up.
func (s SiteState) Ready() bool {
	return s.Connected && s.Listed == s.Want && s.Up == s.Want
}

// reachConcurrency bounds the reachability reads one poll makes at once.
const reachConcurrency = 16

// readSites reads every site's node simulator, and the reachability of each
// running component not already up; up latches the components seen up, so a
// poll reads only what is still coming up.
func readSites(ctx context.Context, c *http.Client, base string, nodes []SiteNode, planted map[string]bool, up map[string]bool) ([]SiteState, error) {
	type device struct {
		ComponentID string `json:"component_id"`
		Component   string `json:"component"`
		Address     string `json:"address"`
		Status      string `json:"status"`
	}
	sims := make([][]device, len(nodes))
	states := make([]SiteState, len(nodes))
	var check []string
	for i, n := range nodes {
		var sim struct {
			Connected bool     `json:"connected"`
			Devices   []device `json:"devices"`
		}
		if err := getJSON(ctx, c, base+"/api/v1/nodes/"+n.Name+"/simulation", &sim); err != nil {
			return nil, err
		}
		sims[i] = sim.Devices
		states[i] = SiteState{Node: n.Name, Connected: sim.Connected, Want: n.Components, Listed: len(sim.Devices)}
		for _, d := range sim.Devices {
			if !up[d.ComponentID] && d.Status == "running" && sim.Connected {
				check = append(check, d.ComponentID)
			}
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errs []error
	sem := make(chan struct{}, reachConcurrency)
	for _, id := range check {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			n, err := readReach(ctx, c, base, id, planted)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if n.Up == n.Endpoints {
				up[id] = true
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	for i := range states {
		for _, d := range sims[i] {
			if up[d.ComponentID] {
				states[i].Up++
			} else {
				states[i].NotUp = append(states[i].NotUp, d.Component+" at "+d.Address)
			}
		}
	}
	return states, nil
}

// SitesSummary is one progress line over every site.
func SitesSummary(states []SiteState) string {
	ready, up, want := 0, 0, 0
	for _, s := range states {
		if s.Ready() {
			ready++
		}
		up += s.Up
		want += s.Want
	}
	return fmt.Sprintf("%d of %d sites up; %d of %d simulated components up", ready, len(states), up, want)
}

// notUpShown is how many of a site's components a timeout names before it
// counts the rest.
const notUpShown = 20

// FormatSitesNotUp names every site not up and why: a simulator never
// connected, components its simulator does not list yet, or the components
// that are not up, the first notUpShown by name.
func FormatSitesNotUp(states []SiteState) string {
	var b strings.Builder
	for _, s := range states {
		if s.Ready() {
			continue
		}
		fmt.Fprintf(&b, "\n  %s: ", s.Node)
		switch {
		case !s.Connected:
			fmt.Fprintf(&b, "its simulator has not connected (0 of %d components up)", s.Want)
			continue
		case s.Listed < s.Want:
			fmt.Fprintf(&b, "its simulator lists %d of its %d components; ", s.Listed, s.Want)
		}
		fmt.Fprintf(&b, "%d of %d components not up", s.Want-s.Up, s.Want)
		if len(s.NotUp) == 0 {
			continue
		}
		names := s.NotUp
		more := 0
		if len(names) > notUpShown {
			names, more = names[:notUpShown], len(names)-notUpShown
		}
		fmt.Fprintf(&b, ": %s", strings.Join(names, ", "))
		if more > 0 {
			fmt.Fprintf(&b, ", and %d more", more)
		}
	}
	return b.String()
}

// waitSites waits until every site's node simulates all of the site's
// components and each of them is up, printing progress as it moves. Past
// the wait it names what is not up.
func (r *Runner) waitSites(ctx context.Context, lab *Lab, c *http.Client, cfg Config) error {
	planted := map[string]bool{}
	for _, l := range cfg.PlantedFaults {
		planted[l] = true
	}
	up := map[string]bool{}
	last := make([]SiteState, len(cfg.SiteNodes))
	for i, n := range cfg.SiteNodes {
		last[i] = SiteState{Node: n.Name, Want: n.Components}
	}
	var lastLine string
	err := r.waitFor(ctx, "every site's node to collect from its components", func() (bool, error) {
		for _, n := range cfg.SiteNodes {
			id := lab.ID(n.Name)
			if st, err := r.Engine.Inspect(ctx, id); err == nil && !st.Running && !st.Restarting {
				return false, r.stopped(ctx, n.Name, id, st)
			}
		}
		states, err := readSites(ctx, c, lab.BaseURL, cfg.SiteNodes, planted, up)
		if err != nil {
			return false, nil
		}
		last = states
		if line := SitesSummary(states); line != lastLine {
			r.step("sites", line)
			lastLine = line
		}
		for _, s := range states {
			if !s.Ready() {
				return false, nil
			}
		}
		return true, nil
	})
	if errors.Is(err, errTimedOut) || errors.Is(err, context.DeadlineExceeded) {
		return &SitesNotUpError{Timeout: cfg.ScaleRun.Timeout, States: last}
	}
	return err
}

// SitesNotUpError is a scale lab that did not come up in time: what it
// waited for last, and how far every site had come.
type SitesNotUpError struct {
	Timeout time.Duration
	// Stage, when set, is the bring-up's stage the time ran out in, before
	// the sites were waited on.
	Stage  string
	States []SiteState
}

func (e *SitesNotUpError) Error() string {
	head := fmt.Sprintf("The scale lab did not come up within %s", e.Timeout)
	if e.Stage != "" {
		head += ": the time ran out " + e.Stage
	}
	return head + ". Not up:" + FormatSitesNotUp(e.States)
}

func (e *SitesNotUpError) Unwrap() error { return errTimedOut }
