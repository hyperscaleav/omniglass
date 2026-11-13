package storage

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
)

// Dependency kinds (#1648): what a component_dependency edge says the
// dependent needs from its upstream.
const (
	// DependencyPower: the dependent is fed from an outlet of the upstream
	// (a PDU). A power command on that outlet takes the dependent down whole.
	DependencyPower = "power"
	// DependencyHost: the dependent is hosted by the upstream (a chassis and
	// its line card, a codec and the peripheral it reboots), so whatever
	// disrupts the upstream disrupts it.
	DependencyHost = "host"
	// DependencyPath: the dependent is reached through the upstream (a
	// gateway). Stored here, read by the upstream gate (#1652), never by the
	// hold set.
	DependencyPath = "path"
)

// HoldEdge is one dependency edge as the hold-set loader reads it.
type HoldEdge struct {
	ComponentID string // the dependent
	UpstreamID  string // what it depends on
	Kind        string
	Outlet      string // set on a power edge only
}

// HoldEndpoint is one endpoint of a component the walk can reach, with the
// two facts the hold set turns on: where it is placed and whether it is
// out-of-band.
type HoldEndpoint struct {
	ID          string
	ComponentID string
	Node        string
	OutOfBand   bool
}

// HoldInput is everything ResolveHoldSet reads: the command's target and the
// node delivering it, whether the command type is a power command and the
// outlet its params name, and the edges and endpoints reachable from the
// target.
type HoldInput struct {
	Target    string
	Node      string
	Power     bool
	Outlet    string
	Edges     []HoldEdge
	Endpoints []HoldEndpoint
}

// ResolveHoldSet names the endpoints a disruptive command closes the gates
// of (#1648), sorted, each once, only those placed on the delivering node.
//
// A command that is not a power command takes its target down: the target's
// in-band endpoints are held (an out-of-band one, a BMC, answers through a
// reboot), and so are the in-band endpoints of every host dependent,
// transitively. A power command on an outlet takes down what that outlet
// feeds: every endpoint of each component with a power edge to the target on
// the outlet, out-of-band included (it loses power too), plus those
// components' host dependents' in-band endpoints. The PDU itself keeps
// running, so its own endpoints are never held by a power command.
// Containment (parent_id) is not read and neither are path edges. The walk
// keeps a visited set, so a cycle that got in anyway terminates.
func ResolveHoldSet(in HoldInput) []string {
	hostDependents := map[string][]string{}
	for _, e := range in.Edges {
		if e.Kind == DependencyHost {
			hostDependents[e.UpstreamID] = append(hostDependents[e.UpstreamID], e.ComponentID)
		}
	}

	whole := map[string]bool{}  // every endpoint held
	inBand := map[string]bool{} // in-band endpoints held
	visited := map[string]bool{in.Target: true}
	var queue []string
	if in.Power {
		// The PDU keeps running: visited, never held.
		for _, e := range in.Edges {
			if e.Kind == DependencyPower && e.UpstreamID == in.Target && in.Outlet != "" && e.Outlet == in.Outlet && !visited[e.ComponentID] {
				visited[e.ComponentID] = true
				whole[e.ComponentID] = true
				queue = append(queue, e.ComponentID)
			}
		}
	} else {
		inBand[in.Target] = true
		queue = append(queue, in.Target)
	}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, d := range hostDependents[c] {
			if visited[d] {
				continue
			}
			visited[d] = true
			inBand[d] = true
			queue = append(queue, d)
		}
	}

	out := []string{}
	seen := map[string]bool{}
	for _, e := range in.Endpoints {
		if e.Node != in.Node || seen[e.ID] {
			continue
		}
		if whole[e.ComponentID] || (inBand[e.ComponentID] && !e.OutOfBand) {
			seen[e.ID] = true
			out = append(out, e.ID)
		}
	}
	slices.Sort(out)
	return out
}

// OutletParam reads the outlet a power command names: params[name], a string
// or a JSON number written as its digits. A whole number reads as its
// integer digits however the JSON spells it (2.0, 20e-1): an integer schema
// admits those, and the PDU switches the same outlet as for 2, so the hold
// set must name the same outlet too (#1653). Anything else, or no name, is
// no outlet, which feeds nothing.
func OutletParam(params []byte, name string) string {
	if name == "" {
		return ""
	}
	switch v := paramsMap(params)[name].(type) {
	case string:
		return v
	case json.Number:
		return wholeDigits(v)
	}
	return ""
}

// wholeDigits writes a JSON number that is a whole number within float64's
// exact integer range as its integer digits, and any other number as written.
func wholeDigits(n json.Number) string {
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return n.String()
	}
	return strconv.FormatInt(int64(f), 10)
}
