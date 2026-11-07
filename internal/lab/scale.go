package lab

import (
	"context"
	"fmt"
	"time"
)

// The scale lab (#1554) is a resolved scenario plan brought up as a
// multi-site deployment: the database, the server, and one node per site,
// each simulating its site's components. What it needs of the host grows with
// the plan, so it is estimated from measured costs before anything starts,
// and a size the host cannot fit is refused, naming the largest that fits.

// Footprint is what a part of the lab takes of the host at steady state:
// resident memory in MiB, and CPU in cores.
type Footprint struct {
	MemMiB float64
	CPU    float64
}

// plus is f with n of g added.
func (f Footprint) plus(g Footprint, n int) Footprint {
	return Footprint{MemMiB: f.MemMiB + g.MemMiB*float64(n), CPU: f.CPU + g.CPU*float64(n)}
}

// CostModel prices a scale lab: the server and Postgres together (Base), each
// site's node container on its own (Node), and each simulated component
// (Component, its simulator's share and its node's collection from it).
type CostModel struct {
	Base, Node, Component Footprint
}

// MeasuredCost is the cost model as measured with docker stats on a 12-core,
// 32 GB WSL dev host at --systems 100 and --systems 1000 (the PR that
// brought the scale lab up records the readings). The base is the server and
// Postgres together, the node is a node container serving nothing, and the
// component cost is the rest divided over the components.
var MeasuredCost = CostModel{
	Base:      Footprint{MemMiB: 260, CPU: 0.30},
	Node:      Footprint{MemMiB: 32, CPU: 0.02},
	Component: Footprint{MemMiB: 0.9, CPU: 0.004},
}

// Estimate is what a lab of sites sites and components simulated components
// takes of the host.
func (m CostModel) Estimate(sites, components int) Footprint {
	return m.Base.plus(m.Node, sites).plus(m.Component, components)
}

// HostResources is what the Docker host has: its memory and its CPUs.
type HostResources struct {
	MemBytes int64
	CPUs     int
}

// Headroom is the share of the host the scale lab leaves to everything
// else (the desktop, the editor, the daemon itself).
const Headroom = 0.2

// Usable is the share of the host a lab may plan to take.
func (h HostResources) Usable() Footprint {
	return Footprint{
		MemMiB: float64(h.MemBytes) / (1 << 20) * (1 - Headroom),
		CPU:    float64(h.CPUs) * (1 - Headroom),
	}
}

// Fits reports whether a lab taking need fits on the host.
func (h HostResources) Fits(need Footprint) bool {
	u := h.Usable()
	return need.MemMiB <= u.MemMiB && need.CPU <= u.CPU
}

// Shape is a scenario's size at a number of systems: how many sites, and
// how many simulated components.
type Shape func(systems int) (sites, components int, err error)

// LargestFit is the largest size in [lo, hi] whose lab fits on the host, by a
// binary search over the shape (a larger size never takes less); ok is false
// when not even lo fits.
func LargestFit(shape Shape, lo, hi int, m CostModel, h HostResources) (systems int, ok bool, err error) {
	fits := func(n int) (bool, error) {
		sites, comps, err := shape(n)
		if err != nil {
			return false, err
		}
		return h.Fits(m.Estimate(sites, comps)), nil
	}
	if f, err := fits(lo); err != nil || !f {
		return 0, false, err
	}
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		f, err := fits(mid)
		if err != nil {
			return 0, false, err
		}
		if f {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, true, nil
}

// ScaleRun is a scale lab's settings beyond its plan.
type ScaleRun struct {
	// Shape, MinSystems and MaxSystems are the scenario's sizes, for
	// naming the largest that fits when the asked-for one does not.
	Shape                  Shape
	MinSystems, MaxSystems int
	Cost                   CostModel
	// Host, when set, stands in for what the Docker host reports (a forced
	// low limit, to see a refusal without a small machine).
	Host *HostResources
	// Timeout bounds the whole bring-up, the seed included: past it, the lab
	// names the sites and components not up and is removed.
	Timeout time.Duration
}

// scaleTotals is the plan the options carry: its sites and components.
func scaleTotals(o Options) (sites, components int) {
	for _, n := range o.SiteNodes {
		components += n.Components
	}
	return len(o.SiteNodes), components
}

// checkCapacity refuses a scale lab the host cannot fit, before anything is
// created, naming the estimate, the host, and the largest size that fits.
func (r *Runner) checkCapacity(ctx context.Context, cfg Config) error {
	sc := cfg.ScaleRun
	host := sc.Host
	if host == nil {
		h, err := r.Engine.Resources(ctx)
		if err != nil {
			return fmt.Errorf("read the Docker host's memory and CPUs: %w", err)
		}
		host = &h
	}
	sites, comps := scaleTotals(cfg.Options)
	need := sc.Cost.Estimate(sites, comps)
	use := host.Usable()
	r.step("capacity", fmt.Sprintf("%d systems: %d sites, %d components; about %s and %.1f cores of the %s and %.1f cores this host can spare",
		cfg.Systems, sites, comps, mib(need.MemMiB), need.CPU, mib(use.MemMiB), use.CPU))
	if host.Fits(need) {
		return nil
	}
	cause := fmt.Sprintf("A scale lab of %d systems (%d sites, %d simulated components) needs about %s of memory and %.1f cores; this Docker host has %s and %d cores, of which the lab plans to use at most %.0f%%.",
		cfg.Systems, sites, comps, mib(need.MemMiB), need.CPU, mib(float64(host.MemBytes)/(1<<20)), host.CPUs, (1-Headroom)*100)
	largest, ok, err := LargestFit(sc.Shape, sc.MinSystems, sc.MaxSystems, sc.Cost, *host)
	if err != nil {
		return err
	}
	if !ok {
		return &PreflightError{Cause: cause + " Not even the smallest size fits.",
			Fix: fmt.Sprintf("give Docker more memory or CPUs; the smallest scale lab, --systems %d, needs more than this host can spare.", sc.MinSystems)}
	}
	return &PreflightError{Cause: cause,
		Fix: fmt.Sprintf("run --systems %d, the largest that fits, or give Docker more memory or CPUs.", largest)}
}

// mib reads a size in MiB as a person would.
func mib(m float64) string {
	if m >= 1024 {
		return fmt.Sprintf("%.1f GiB", m/1024)
	}
	return fmt.Sprintf("%.0f MiB", m)
}
