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

// ScaleProject is a scale lab's compose project: its own, so it runs beside
// a learner's lab, and every container and network it creates carries it.
const ScaleProject = "omniglass-scale"

// The measuring ladder (#1719): every lab that measures runs the corporate
// scenario at a stated size, so two figures compare only when the fleet they
// were read from is the same. SmokeSystems is the smoke rung (make test-lab
// and make lab-footprint), the smallest size that seeds every product the
// manifest names; the scale levels are 300, 1,000 and 3,000 systems. The dev
// scenario is the showcase, never a measurement basis.
const (
	MeasureScenario = "corporate"
	SmokeSystems    = 20
)

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
// 32 GiB dev host (scripts/scale-lab.sh, settled 30 seconds after ready) at
// --systems 20 (3 sites, 67 components) and --systems 100 (3 sites, 322
// components). A node container took about 40 MiB at 6 to 24 components, 72
// at 68 and 199 at 230: 36 MiB plus 0.7 MiB a component, rounded up for the
// small sites. The server and Postgres took 180 MiB at 67 components and
// 223 at 322: 170 MiB plus 0.17 MiB a component. CPU is the mean of spiky
// samples: the server and Postgres a tenth of a core at 67 components and
// three tenths at 322, a node a hundredth or less.
var MeasuredCost = CostModel{
	Base:      Footprint{MemMiB: 170, CPU: 0.05},
	Node:      Footprint{MemMiB: 36, CPU: 0.01},
	Component: Footprint{MemMiB: 0.9, CPU: 0.001},
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
	// HostMemory, when set, stands in for the memory the Docker host
	// reports (a forced low limit, to see a refusal without a small
	// machine).
	HostMemory int64
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

// Capacity is a scale lab's estimate against a host, and the decision: it
// fits, or the largest size that does (LargestOK false when none does).
type Capacity struct {
	Systems, Sites, Components int
	Need                       Footprint
	Host                       HostResources
	Fits                       bool
	Largest                    int
	LargestOK                  bool
	MinSystems                 int
}

// AssessCapacity estimates a scale lab of systems systems (sites sites,
// components components) and decides whether the host fits it, searching
// for the largest size that does when it does not.
func AssessCapacity(sc ScaleRun, host HostResources, systems, sites, components int) (Capacity, error) {
	c := Capacity{Systems: systems, Sites: sites, Components: components, Host: host, MinSystems: sc.MinSystems,
		Need: sc.Cost.Estimate(sites, components)}
	c.Fits = host.Fits(c.Need)
	if c.Fits {
		return c, nil
	}
	var err error
	c.Largest, c.LargestOK, err = LargestFit(sc.Shape, sc.MinSystems, sc.MaxSystems, sc.Cost, host)
	return c, err
}

// Summary is the estimate set against what the host can spare.
func (c Capacity) Summary() string {
	use := c.Host.Usable()
	return fmt.Sprintf("%d systems: %d sites, %d components; about %s and %.1f cores of the %s and %.1f cores this host can spare",
		c.Systems, c.Sites, c.Components, FormatMiB(c.Need.MemMiB), c.Need.CPU, FormatMiB(use.MemMiB), use.CPU)
}

// Err is the refusal of a size that does not fit, naming the largest that
// does; nil when it fits.
func (c Capacity) Err() error {
	if c.Fits {
		return nil
	}
	cause := fmt.Sprintf("A scale lab of %d systems (%d sites, %d simulated components) needs about %s of memory and %.1f cores; this Docker host has %s and %d cores, of which the lab plans to use at most %.0f%%.",
		c.Systems, c.Sites, c.Components, FormatMiB(c.Need.MemMiB), c.Need.CPU, FormatMiB(float64(c.Host.MemBytes)/(1<<20)), c.Host.CPUs, (1-Headroom)*100)
	if !c.LargestOK {
		return &PreflightError{Cause: cause + " Not even the smallest size fits.",
			Fix: fmt.Sprintf("give Docker more memory or CPUs; the smallest scale lab, --systems %d, needs more than this host can spare.", c.MinSystems)}
	}
	return &PreflightError{Cause: cause,
		Fix: fmt.Sprintf("run --systems %d, the largest that fits, or give Docker more memory or CPUs.", c.Largest)}
}

// ScaleHost is the host a scale lab plans against: the Docker host's own
// report, with its memory replaced by a forced limit when one is set.
func ScaleHost(ctx context.Context, eng Engine, forcedMem int64) (HostResources, error) {
	host, err := eng.Resources(ctx)
	if err != nil {
		return host, fmt.Errorf("read the Docker host's memory and CPUs: %w", err)
	}
	if forcedMem > 0 {
		host.MemBytes = forcedMem
	}
	return host, nil
}

// checkCapacity refuses a scale lab the host cannot fit, before anything is
// created, naming the estimate, the host, and the largest size that fits.
func (r *Runner) checkCapacity(ctx context.Context, cfg Config) error {
	host, err := ScaleHost(ctx, r.Engine, cfg.ScaleRun.HostMemory)
	if err != nil {
		return err
	}
	sites, comps := scaleTotals(cfg.Options)
	c, err := AssessCapacity(*cfg.ScaleRun, host, cfg.Systems, sites, comps)
	if err != nil {
		return err
	}
	r.step("capacity", c.Summary())
	return c.Err()
}

// FormatMiB reads a size in MiB as a person would.
func FormatMiB(m float64) string {
	if m >= 1024 {
		return fmt.Sprintf("%.1f GiB", m/1024)
	}
	return fmt.Sprintf("%.0f MiB", m)
}
