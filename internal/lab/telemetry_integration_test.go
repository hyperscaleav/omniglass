//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/transport"
)

// identityKeys are what a component says about itself rather than what it is
// for: its identity and how long it has run. The collected tier's floor (#1371,
// #1266) is a collected value beyond these.
var identityKeys = []string{"model-number", "serial-number", "firmware-version", "mac-address", "uptime"}

// telemetryGaps are the owners a telemetry check expects to fall short, each
// group with the issue that closes it. An entry is stale when every owner it
// covers meets its tier.
type telemetryGaps struct {
	// Collected is keyed by product: its components lack collected data, or
	// a value beyond identity and uptime.
	Collected map[string]string
	// Catalog is keyed by product like Collected, for the products the
	// simulator catalog lists (unsimulated, passive): the catalog may list
	// one the fleet places nowhere, so covering no owner is not stale.
	Catalog map[string]string
	// Systems is keyed by a system's place and standard ("place/standard"):
	// systems with no value of their own.
	Systems map[string]string
}

// telemetryEverywhere is the lab's account of #1371, written against a base URL
// and a client so the scale lab (#1357) runs it as well. It reads every
// component and system and holds each to its tiers:
//   - built-in: each endpoint that is no planted fault carries every key its
//     transport and attach declare (the registry's set, read from the API);
//   - collected: a component carries a value of writer class collected that
//     landed since `since`, and at least one beyond identity and uptime (a
//     conduit's count where its bound owners hold them, by source);
//   - systems: a system carries a value of its own (not the platform's
//     health verdict) that landed since `since`.
//
// It returns what falls short outside the gaps, and the gap entries now met.
func telemetryEverywhere(ctx context.Context, c *http.Client, base string, since time.Time, planted []string, gaps telemetryGaps) (missing, stale []string, err error) {
	get := func(path string, into any) error { return getJSON(ctx, c, base+"/api/v1"+path, into) }
	isPlanted := map[string]bool{}
	for _, p := range planted {
		isPlanted[p] = true
	}

	var trs struct {
		Transports []transport.Transport `json:"transports"`
	}
	if err := get("/transports", &trs); err != nil {
		return nil, nil, err
	}
	transports := map[string]transport.Transport{}
	for _, t := range trs.Transports {
		transports[t.Name] = t
	}

	type shape struct {
		polls, listeners bool
		secrets          []string
	}
	var drivers struct {
		Drivers []struct {
			Name string `json:"name"`
		} `json:"drivers"`
	}
	if err := get("/drivers", &drivers); err != nil {
		return nil, nil, err
	}
	shapes := map[string]shape{}
	for _, d := range drivers.Drivers {
		var one struct {
			Spec *struct {
				Inputs []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
				} `json:"inputs"`
				Polls     []struct{ Emits []any } `json:"polls"`
				Listeners []struct{ Emits []any } `json:"listeners"`
			} `json:"spec"`
		}
		if err := get("/drivers/"+url.PathEscape(d.Name), &one); err != nil {
			return nil, nil, err
		}
		if one.Spec == nil {
			continue
		}
		s := shape{polls: len(one.Spec.Polls) > 0, listeners: len(one.Spec.Listeners) > 0}
		for _, in := range one.Spec.Inputs {
			if in.Kind == "secret" {
				s.secrets = append(s.secrets, in.Name)
			}
		}
		shapes[d.Name] = s
	}

	// The catalogs: each key's writer class and lane.
	class := map[string]string{}
	metricLane := map[string]bool{}
	var mts struct {
		MetricTypes []struct {
			Name        string `json:"name"`
			WriterClass string `json:"writer_class"`
		} `json:"metric_types"`
	}
	if err := get("/metric-types?limit=1000", &mts); err != nil {
		return nil, nil, err
	}
	for _, m := range mts.MetricTypes {
		class[m.Name], metricLane[m.Name] = m.WriterClass, true
	}
	var pts struct {
		Properties []struct {
			Name        string `json:"name"`
			WriterClass string `json:"writer_class"`
		} `json:"properties"`
	}
	if err := get("/property-types?limit=1000", &pts); err != nil {
		return nil, nil, err
	}
	for _, p := range pts.Properties {
		class[p.Name] = p.WriterClass
	}

	type sample struct {
		TS       time.Time `json:"ts"`
		Instance string    `json:"instance"`
		Source   string    `json:"source"`
	}
	// samples reads an owner's series of a key (owner is "/components/<id>"
	// or "/systems/<id>"); a key the owner holds none of reads empty.
	samples := func(owner, key string) ([]sample, error) {
		lane := "properties"
		if metricLane[key] {
			lane = "metrics"
		}
		var s struct {
			Samples []sample `json:"samples"`
		}
		if err := get(owner+"/"+lane+"/"+url.PathEscape(key)+"/samples?limit=500", &s); err != nil {
			return nil, nil
		}
		return s.Samples, nil
	}
	// keysOf lists the keys an owner holds a value of.
	keysOf := func(owner string) ([]string, error) {
		var m struct {
			Metrics []struct {
				Name      string `json:"metric_type_name"`
				IsSampled bool   `json:"is_sampled"`
			} `json:"metrics"`
		}
		if err := get(owner+"/metrics", &m); err != nil {
			return nil, err
		}
		var p struct {
			Properties []struct {
				Name           string `json:"property_type_name"`
				Reconciliation *struct {
					Is any `json:"is"`
				} `json:"reconciliation"`
			} `json:"properties"`
		}
		if err := get(owner+"/properties", &p); err != nil {
			return nil, err
		}
		var out []string
		for _, x := range m.Metrics {
			if x.IsSampled {
				out = append(out, x.Name)
			}
		}
		for _, x := range p.Properties {
			if x.Reconciliation != nil && x.Reconciliation.Is != nil {
				out = append(out, x.Name)
			}
		}
		return out, nil
	}
	// live reports the keys of an owner's whose class passes keep and that
	// landed since the check's start; from, when set, keeps only samples a
	// source of that prefix wrote (a conduit's, on its bound owner).
	live := func(owner string, keep func(cls string) bool, from string) ([]string, error) {
		keys, err := keysOf(owner)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, k := range keys {
			if !keep(class[k]) {
				continue
			}
			ss, err := samples(owner, k)
			if err != nil {
				return nil, err
			}
			for _, s := range ss {
				if !s.TS.Before(since) && (from == "" || strings.HasPrefix(s.Source, from)) {
					out = append(out, k)
					break
				}
			}
		}
		return out, nil
	}
	collected := func(cls string) bool { return cls == "collected" }
	beyondIdentity := func(keys []string) bool {
		for _, k := range keys {
			if !slices.Contains(identityKeys, k) {
				return true
			}
		}
		return false
	}

	var comps struct {
		Components []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Path    string `json:"path"`
			Product string `json:"product"`
		} `json:"components"`
	}
	if err := get("/components?limit=10000", &comps); err != nil {
		return nil, nil, err
	}
	var eps struct {
		Endpoints []struct {
			Name        string            `json:"name"`
			Label       string            `json:"label"`
			Transport   string            `json:"transport"`
			ComponentID string            `json:"component_id"`
			Driver      string            `json:"driver"`
			Inputs      map[string]string `json:"inputs"`
		} `json:"endpoints"`
	}
	if err := get("/endpoints?limit=10000", &eps); err != nil {
		return nil, nil, err
	}
	byComp := map[string][]int{}
	for i, e := range eps.Endpoints {
		if e.ComponentID != "" {
			byComp[e.ComponentID] = append(byComp[e.ComponentID], i)
		}
	}

	// A gap entry is met when every owner it covers meets its tier.
	gapMet := map[string]bool{}
	gapSeen := map[string]bool{}
	note := func(entry string, met bool) {
		if !gapSeen[entry] {
			gapSeen[entry], gapMet[entry] = true, met
			return
		}
		gapMet[entry] = gapMet[entry] && met
	}

	for _, comp := range comps.Components {
		owner := "/components/" + comp.ID

		// Built-in: every endpoint carries what its transport and attach
		// declare.
		instances := map[string]map[string]bool{}
		have := func(key, instance string) (bool, error) {
			if instances[key] == nil {
				ss, err := samples(owner, key)
				if err != nil {
					return false, err
				}
				instances[key] = map[string]bool{}
				for _, s := range ss {
					instances[key][s.Instance] = true
				}
			}
			return instances[key][instance], nil
		}
		for _, i := range byComp[comp.ID] {
			e := eps.Endpoints[i]
			if isPlanted[e.Label] {
				continue // broken on purpose: a lesson, not a gap
			}
			tr, ok := transports[e.Transport]
			if !ok {
				return nil, nil, fmt.Errorf("endpoint %s on %s: unknown transport %q", e.Name, comp.Path, e.Transport)
			}
			a := transport.Attach{Driver: e.Driver != ""}
			if s, ok := shapes[e.Driver]; ok {
				a.Polls, a.Listeners = s.polls, s.listeners
				for _, in := range s.secrets {
					if e.Inputs[in] != "" {
						a.Credential = true
					}
				}
			}
			var lacks []string
			for _, k := range tr.Expected(a) {
				ok, err := have(k, e.Name)
				if err != nil {
					return nil, nil, err
				}
				if !ok {
					lacks = append(lacks, k)
				}
			}
			if len(lacks) > 0 {
				missing = append(missing, fmt.Sprintf("%s: built-in: endpoint %s (%s) lacks %s", comp.Path, e.Name, e.Transport, strings.Join(lacks, ", ")))
			}
		}

		// Collected: a live collected value, and one beyond identity.
		keys, err := live(owner, collected, "")
		if err != nil {
			return nil, nil, err
		}
		if !beyondIdentity(keys) {
			// A conduit's values land on the owners it binds, by source.
			var b struct {
				Bindings []struct {
					OwnerKind string `json:"owner_kind"`
					OwnerID   string `json:"owner_id"`
				} `json:"bindings"`
			}
			if err := get(owner+"/identity-bindings", &b); err == nil {
				for _, x := range b.Bindings {
					more, err := live("/"+x.OwnerKind+"s/"+x.OwnerID, collected, comp.Name+"/")
					if err != nil {
						return nil, nil, err
					}
					keys = append(keys, more...)
				}
			}
		}
		met := beyondIdentity(keys)
		gap := ""
		if issue, ok := gaps.Collected[comp.Product]; ok {
			gap = "product " + comp.Product + " (" + issue + ")"
		} else if why, ok := gaps.Catalog[comp.Product]; ok {
			gap = "product " + comp.Product + " (" + why + ")"
		}
		if gap != "" {
			note("collected: "+gap, met)
			continue
		}
		switch {
		case len(keys) == 0:
			missing = append(missing, fmt.Sprintf("%s: collected: no live value from the device (%s)", comp.Path, orNone(comp.Product)))
		case !met:
			sort.Strings(keys)
			missing = append(missing, fmt.Sprintf("%s: collected: only identity and uptime (%s; %s)", comp.Path, strings.Join(keys, ", "), orNone(comp.Product)))
		}
	}

	var syss struct {
		Systems []struct {
			ID       string `json:"id"`
			Path     string `json:"path"`
			Location string `json:"location"`
			Standard string `json:"standard"`
		} `json:"systems"`
	}
	if err := get("/systems?limit=10000", &syss); err != nil {
		return nil, nil, err
	}
	notPlatform := func(cls string) bool { return cls != "platform" }
	for _, s := range syss.Systems {
		keys, err := live("/systems/"+s.ID, notPlatform, "")
		if err != nil {
			return nil, nil, err
		}
		met := len(keys) > 0
		if issue, ok := gaps.Systems[s.Location+"/"+s.Standard]; ok {
			note("systems: "+s.Location+"/"+s.Standard+" ("+issue+")", met)
			continue
		}
		if !met {
			missing = append(missing, fmt.Sprintf("%s: system: no live value of its own", s.Path))
		}
	}

	for entry, met := range gapMet {
		if met {
			stale = append(stale, entry)
		}
	}
	// An entry that covers no owner at all is stale too.
	covered := func(prefix, key, issue string) {
		if !gapSeen[prefix+key+" ("+issue+")"] {
			stale = append(stale, prefix+key+" ("+issue+"): covers no owner")
		}
	}
	for k, issue := range gaps.Collected {
		covered("collected: product ", k, issue)
	}
	for k, issue := range gaps.Systems {
		covered("systems: ", k, issue)
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return missing, stale, nil
}

func orNone(product string) string {
	if product == "" {
		return "no product"
	}
	return product
}

// labTelemetryGaps is the dev lab's gap list: the dev seed's (devseed
// CollectedGaps and SystemGaps, a system's fixture key read as its place and
// standard) and the simulator catalog's unsimulated and passive products,
// each with the issue its coverage.yaml line names.
func labTelemetryGaps(scenario string) (telemetryGaps, error) {
	g := telemetryGaps{Collected: map[string]string{}, Catalog: map[string]string{}, Systems: map[string]string{}}
	for p, issue := range devseed.CollectedGaps {
		g.Collected[p] = issue
	}
	cov, err := profiles.LoadCoverage()
	if err != nil {
		return g, err
	}
	for _, m := range []map[string]string{cov.Unsimulated, cov.Passive} {
		for p, why := range m {
			g.Catalog[p] = why
		}
	}
	sc, err := devseed.LookupScenario(scenario)
	if err != nil {
		return g, err
	}
	doc, err := sc.Doc()
	if err != nil {
		return g, err
	}
	place := map[string]string{}
	for _, l := range doc.Locations {
		place[l.Key] = l.Key
		if l.Name != "" {
			place[l.Key] = l.Name
		}
	}
	for _, s := range doc.Systems {
		if issue, ok := devseed.SystemGaps[s.Key]; ok {
			g.Systems[place[s.Location]+"/"+s.Standard] = issue
		}
	}
	return g, nil
}
