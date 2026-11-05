// Package labcheck is the lab-level proof of #1266's outcome (#1371): after the
// warm-up, every component carries its full built-in data and at least one
// collected class signal, and every system carries a value of its own. A gap
// fails, naming the owner and the tier.
//
// It is written as a function of a base URL and a client (Check), so the dev
// lab's `make test-lab` and the scale lab's harness (#1357) call the same
// judgment at any size. The judgment itself (Evaluate) is pure over a
// Snapshot; Collect is the one place that reads the API. The expected built-in
// set is not listed here: it is generated from the transport registry and
// probekeys (transport.ExpectedBuiltIn), and a key's class from the catalogs'
// writer classes (ADR-0161).
package labcheck

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Value is one key an owner carries a value for, with the writer class the
// catalogs rule for it.
type Value struct {
	Key   string
	Class writerclass.Class
}

// Endpoint is one endpoint of a component as the lab holds it. Answered is
// whether the device answered (a verdict up, or for a push endpoint a post
// heard), which decides whether the timings and the driver's telemetry are
// owed; Present is the built-in keys that landed under this endpoint.
type Endpoint struct {
	Name      string
	Transport string
	Answered  bool
	// SessionUp is whether the endpoint's held session reads up: a transport
	// whose probe yields to it takes no timing meanwhile.
	SessionUp bool
	Shape     transport.DriverShape
	Present   map[string]bool
	// Planted is an endpoint the seed breaks on purpose for a learner to find
	// (a rotated password, a changed host key). Its ladder stops where the
	// fault does, so it owes no built-in key.
	Planted bool
}

// Component is one component and everything the check reads of it: its
// endpoints and every key it carries a value for.
type Component struct {
	Name      string
	Product   string
	Location  string
	Endpoints []Endpoint
	Values    []Value
}

// System is one system and the keys it carries a value for.
type System struct {
	Name   string
	Values []Value
}

// Snapshot is what a lab holds, read once. Evaluate is a pure function of it.
type Snapshot struct {
	Components []Component
	Systems    []System
}

// Finding is one gap, naming the owner and the tier it fails.
type Finding struct {
	Owner  string
	Tier   devseed.Tier
	Detail string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s tier: %s", f.Owner, f.Tier, f.Detail)
}

// Gaps is the known-gap ledger the check honours, as devseed.TierGaps.
type Gaps = map[devseed.Tier]map[string]devseed.TierGap

// beyondClassSignal are the collected keys that do not count as a class signal
// (ADR-0131): the identity a device reports about itself (the declared
// attribute set of the property catalog, and the key an identity binding ties
// by) and uptime, which every generic SNMP device reads.
var beyondClassSignal = []string{
	"model-number", "serial-number", "firmware-version", "software-version", "mac-address", "ucc-room-id", "uptime",
}

// classSignals are the values of vs that are collected class signals: written
// by the collected class, and neither identity nor uptime.
func classSignals(vs []Value) []string {
	var out []string
	for _, v := range vs {
		if v.Class == writerclass.Collected && !slices.Contains(beyondClassSignal, v.Key) {
			out = append(out, v.Key)
		}
	}
	return out
}

// ownValue is a value a system holds of its own: a pushed or collected one, or
// one a calc rule rolls up. The platform's health verdict and the node's
// probes are about the system's members, not a value it carries itself.
func ownValue(vs []Value) bool {
	return slices.ContainsFunc(vs, func(v Value) bool {
		return v.Class == writerclass.Collected || v.Class == writerclass.Calc
	})
}

// owner names a component for a finding. A component's name is unique within
// its room, not across the fleet, so the product and location say which one.
func (c Component) owner() string {
	switch {
	case c.Product != "" && c.Location != "":
		return fmt.Sprintf("component %s (%s, at %s)", c.Name, c.Product, c.Location)
	case c.Product != "":
		return fmt.Sprintf("component %s (%s)", c.Name, c.Product)
	}
	return "component " + c.Name
}

// builtInGaps are the keys an endpoint owes and has not landed.
func builtInGaps(e Endpoint) []string {
	if e.Planted {
		return nil
	}
	var out []string
	for _, k := range transport.ExpectedBuiltIn(e.Transport, e.Shape, e.Answered, e.SessionUp) {
		if !e.Present[k] {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Evaluate judges a snapshot against the three tiers and returns what fails,
// ordered by owner, then tier. A listed gap suppresses its owners' failures on
// its tier; a listed gap none of whose owners fails is stale and fails in its
// place, so the ledger cannot outlive the issue that closes it.
func Evaluate(s Snapshot, gaps Gaps) []Finding {
	type failure struct {
		owner string
		key   string // the gap key that covers it: the product, or AllSystems
		f     Finding
	}
	var failures []failure
	covered := map[devseed.Tier]map[string]int{} // owners a gap key covers, per tier
	cover := func(tier devseed.Tier, key string) {
		if covered[tier] == nil {
			covered[tier] = map[string]int{}
		}
		covered[tier][key]++
	}
	fail := func(tier devseed.Tier, key, owner, detail string) {
		failures = append(failures, failure{owner: owner, key: key, f: Finding{Owner: owner, Tier: tier, Detail: detail}})
	}

	for _, c := range s.Components {
		owner := c.owner()
		cover(devseed.TierBuiltIn, c.Product)
		cover(devseed.TierCollected, c.Product)
		if len(c.Endpoints) == 0 {
			fail(devseed.TierBuiltIn, c.Product, owner, "has no endpoint, so nothing probes it")
		}
		for _, e := range c.Endpoints {
			if missing := builtInGaps(e); len(missing) > 0 {
				fail(devseed.TierBuiltIn, c.Product, owner, fmt.Sprintf("endpoint %s (%s) lacks %s", e.Name, e.Transport, strings.Join(missing, ", ")))
			}
		}
		if len(classSignals(c.Values)) == 0 {
			fail(devseed.TierCollected, c.Product, owner, "carries no collected class signal beyond identity and uptime")
		}
	}
	for _, sys := range s.Systems {
		cover(devseed.TierSystem, devseed.AllSystems)
		if !ownValue(sys.Values) {
			fail(devseed.TierSystem, devseed.AllSystems, "system "+sys.Name, "carries no value of its own")
		}
	}

	var out []Finding
	failed := map[devseed.Tier]map[string]int{}
	for _, x := range failures {
		if _, listed := gaps[x.f.Tier][x.key]; listed {
			if failed[x.f.Tier] == nil {
				failed[x.f.Tier] = map[string]int{}
			}
			failed[x.f.Tier][x.key]++
			continue
		}
		out = append(out, x.f)
	}
	for tier, owners := range gaps {
		for key, gap := range owners {
			// A gap is stale once nothing it covers fails: at least one
			// owner is placed and none of them lacks the tier.
			if covered[tier][key] > 0 && failed[tier][key] == 0 {
				out = append(out, Finding{Owner: "gap " + key, Tier: tier,
					Detail: fmt.Sprintf("stale: listed against %s, but every owner it covers carries the tier; remove the entry", gap.Issue)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Tier < out[j].Tier
	})
	return out
}
