//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// methodsExercised is the lab's own account of #1370: after the warm-up, every
// seeded driver and every registry transport (less the listed gaps) is healthy
// on a lab component that is not a planted fault, on both tiers. The rule
// (ruled 2026-10-29):
//   - a driver counts when its endpoint reads up (a push endpoint: it has
//     heard a post) AND a value it emits has landed from it, on its own
//     component under its endpoint, or on an owner its conduit binds;
//   - a driverless transport counts on its connection tier: up;
//   - the push lane (POST /telemetry:push) counts on its collected tier alone,
//     having no endpoint: a system holds the booking service's pushed values.
//
// It returns what is missing, empty once every method is exercised.
func methodsExercised(ctx context.Context, c *http.Client, base string, planted []string) ([]string, error) {
	get := func(path string, into any) error { return getJSON(ctx, c, base+"/api/v1"+path, into) }
	isPlanted := map[string]bool{}
	for _, p := range planted {
		isPlanted[p] = true
	}

	var drivers struct {
		Drivers []struct {
			Name string `json:"name"`
		} `json:"drivers"`
	}
	if err := get("/drivers", &drivers); err != nil {
		return nil, err
	}
	emits := map[string][]string{}
	for _, d := range drivers.Drivers {
		var one struct {
			Spec *struct {
				Polls     []struct{ Emits []struct{ Name string } } `json:"polls"`
				Listeners []struct{ Emits []struct{ Name string } } `json:"listeners"`
			} `json:"spec"`
		}
		if err := get("/drivers/"+url.PathEscape(d.Name), &one); err != nil {
			return nil, err
		}
		if one.Spec == nil {
			continue // a stub cannot be attached
		}
		seen := map[string]bool{}
		for _, p := range one.Spec.Polls {
			for _, e := range p.Emits {
				seen[e.Name] = true
			}
		}
		for _, l := range one.Spec.Listeners {
			for _, e := range l.Emits {
				seen[e.Name] = true
			}
		}
		for k := range seen {
			emits[d.Name] = append(emits[d.Name], k)
		}
	}
	var transports struct {
		Transports []struct {
			Name string `json:"name"`
		} `json:"transports"`
	}
	if err := get("/transports", &transports); err != nil {
		return nil, err
	}
	var eps struct {
		Endpoints []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Label       string `json:"label"`
			Transport   string `json:"transport"`
			Component   string `json:"component"`
			ComponentID string `json:"component_id"`
			Driver      string `json:"driver"`
		} `json:"endpoints"`
	}
	if err := get("/endpoints?limit=1000", &eps); err != nil {
		return nil, err
	}

	type reach struct {
		Endpoints []struct {
			ID       string  `json:"id"`
			LastSeen *string `json:"last_seen"`
			Verdict  *struct {
				Value string `json:"value"`
			} `json:"verdict"`
		} `json:"endpoints"`
	}
	type metrics struct {
		Metrics []struct {
			Name      string `json:"metric_type_name"`
			Instance  string `json:"instance"`
			Instances []struct {
				Instance string `json:"instance"`
			} `json:"instances"`
		} `json:"metrics"`
	}
	type samples struct {
		Samples []struct {
			Instance string `json:"instance"`
			Source   string `json:"source"`
		} `json:"samples"`
	}
	reaches := map[string]reach{}
	up := func(compID, epID, transport string) (bool, error) {
		r, ok := reaches[compID]
		if !ok {
			if err := get("/components/"+compID+"/reachability", &r); err != nil {
				return false, err
			}
			reaches[compID] = r
		}
		for _, e := range r.Endpoints {
			if e.ID != epID {
				continue
			}
			if transport == "webhook" {
				return e.LastSeen != nil, nil
			}
			return e.Verdict != nil && e.Verdict.Value == "up", nil
		}
		return false, nil
	}
	// landed reports a value the endpoint's driver emits, landed from it: a
	// metric or property sample on its component under its endpoint, or a
	// property sample on an owner its component (a conduit) binds, whose
	// source names the endpoint.
	landed := func(compID, comp, ep string, keys []string) (bool, error) {
		var m metrics
		if err := get("/components/"+compID+"/metrics", &m); err != nil {
			return false, err
		}
		want := map[string]bool{}
		for _, k := range keys {
			want[k] = true
		}
		for _, x := range m.Metrics {
			if !want[x.Name] {
				continue
			}
			if x.Instance == ep {
				return true, nil
			}
			for _, i := range x.Instances {
				if i.Instance == ep {
					return true, nil
				}
			}
		}
		owners := []string{"/components/" + compID}
		var b struct {
			Bindings []struct {
				OwnerKind string `json:"owner_kind"`
				OwnerID   string `json:"owner_id"`
			} `json:"bindings"`
		}
		if err := get("/components/"+compID+"/identity-bindings", &b); err == nil {
			for _, x := range b.Bindings {
				owners = append(owners, "/"+x.OwnerKind+"s/"+x.OwnerID)
			}
		}
		for _, owner := range owners {
			for _, k := range keys {
				var s samples
				if err := get(owner+"/properties/"+url.PathEscape(k)+"/samples", &s); err != nil {
					continue // a metric key, or nothing of it here
				}
				for _, x := range s.Samples {
					if (owner == owners[0] && x.Instance == ep) || x.Source == comp+"/"+ep {
						return true, nil
					}
				}
			}
		}
		return false, nil
	}

	exercised := map[string]bool{}
	for _, e := range eps.Endpoints {
		if e.ComponentID == "" || isPlanted[e.Label] {
			continue
		}
		if exercised[e.Transport] && (e.Driver == "" || exercised[e.Driver]) {
			continue
		}
		ok, err := up(e.ComponentID, e.ID, e.Transport)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if e.Driver != "" {
			got, err := landed(e.ComponentID, e.Component, e.Name, emits[e.Driver])
			if err != nil {
				return nil, err
			}
			if !got {
				continue // a driver that only connects does not count
			}
			exercised[e.Driver] = true
		}
		exercised[e.Transport] = true
	}

	var missing []string
	for d := range emits {
		if _, gap := devseed.MethodGaps[d]; !gap && !exercised[d] {
			missing = append(missing, "driver "+d)
		}
	}
	for _, tr := range transports.Transports {
		if _, gap := devseed.MethodGaps[tr.Name]; !gap && !exercised[tr.Name] {
			missing = append(missing, "transport "+tr.Name)
		}
	}
	pushed, err := systemPushed(ctx, get)
	if err != nil {
		return nil, err
	}
	if !pushed {
		missing = append(missing, "the push lane (no system holds the booking service's pushed booking-count)")
	}
	sort.Strings(missing)
	return missing, nil
}

// systemPushed reports a system holding a booking-count the booking service
// pushed.
func systemPushed(ctx context.Context, get func(string, any) error) (bool, error) {
	var list struct {
		Systems []struct {
			ID string `json:"id"`
		} `json:"systems"`
	}
	if err := get("/systems?limit=1000", &list); err != nil {
		return false, err
	}
	for _, s := range list.Systems {
		var m struct {
			Metrics []struct {
				Name      string `json:"metric_type_name"`
				IsSampled bool   `json:"is_sampled"`
			} `json:"metrics"`
		}
		if err := get("/systems/"+s.ID+"/metrics", &m); err != nil {
			return false, fmt.Errorf("system %s metrics: %w", s.ID, err)
		}
		for _, x := range m.Metrics {
			if x.Name == "booking-count" && x.IsSampled {
				return true, nil
			}
		}
	}
	return false, nil
}
