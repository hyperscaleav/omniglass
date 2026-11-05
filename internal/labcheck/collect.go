package labcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// listQuery is the page the list reads ask for; the dev lab and its siblings
// fit well inside it.
const listQuery = "?limit=1000"

// Check reads the lab at base with c and judges it against gaps: the live
// check, a function of a base URL and a client so any harness can call it.
// The returned findings are empty once the lab is whole; an error is a failed
// read, not a failed check.
func Check(ctx context.Context, c *http.Client, base string, gaps Gaps) ([]Finding, error) {
	snap, err := Collect(ctx, c, base)
	if err != nil {
		return nil, err
	}
	return Evaluate(snap, gaps), nil
}

type reader struct {
	ctx  context.Context
	c    *http.Client
	base string
}

func (r reader) get(path string, into any) error {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.base+"/api/v1"+path, nil)
	if err != nil {
		return err
	}
	resp, err := r.c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// catalog is the writer class of every key, and which keys are properties
// (read through their samples) rather than metrics (read through the owner's
// effective metrics).
type catalog struct {
	class    map[string]writerclass.Class
	property map[string]bool
}

// classOf is a key's class; a key the catalogs lack is collected, the default
// of ADR-0161.
func (k catalog) classOf(key string) writerclass.Class {
	if c, ok := k.class[key]; ok {
		return c
	}
	return writerclass.Collected
}

func (r reader) catalog() (catalog, error) {
	k := catalog{class: map[string]writerclass.Class{}, property: map[string]bool{}}
	var props struct {
		Properties []struct {
			Name        string `json:"name"`
			WriterClass string `json:"writer_class"`
		} `json:"properties"`
	}
	if err := r.get("/property-types", &props); err != nil {
		return k, err
	}
	for _, p := range props.Properties {
		k.class[p.Name] = writerclass.Class(p.WriterClass)
		k.property[p.Name] = true
	}
	var metrics struct {
		MetricTypes []struct {
			Name        string `json:"name"`
			WriterClass string `json:"writer_class"`
		} `json:"metric_types"`
	}
	if err := r.get("/metric-types", &metrics); err != nil {
		return k, err
	}
	for _, m := range metrics.MetricTypes {
		k.class[m.Name] = writerclass.Class(m.WriterClass)
	}
	return k, nil
}

// ownerValues is every key an owner of kind ("component" or "system") carries
// a value for: a sampled metric, an observed property, or one a calc rule
// calculated.
func (r reader) ownerValues(kind, id string, k catalog) ([]Value, error) {
	var metrics struct {
		Metrics []struct {
			Name      string `json:"metric_type_name"`
			IsSampled bool   `json:"is_sampled"`
		} `json:"metrics"`
	}
	if err := r.get("/"+kind+"s/"+id+"/metrics", &metrics); err != nil {
		return nil, err
	}
	var props struct {
		Properties []struct {
			Name           string `json:"property_type_name"`
			SourceRule     string `json:"source_rule"`
			Reconciliation *struct {
				Is json.RawMessage `json:"is"`
			} `json:"reconciliation"`
		} `json:"properties"`
	}
	if err := r.get("/"+kind+"s/"+id+"/properties", &props); err != nil {
		return nil, err
	}
	var out []Value
	for _, m := range metrics.Metrics {
		if m.IsSampled {
			out = append(out, Value{Key: m.Name, Class: k.classOf(m.Name)})
		}
	}
	for _, p := range props.Properties {
		observed := p.Reconciliation != nil && len(p.Reconciliation.Is) > 0 && string(p.Reconciliation.Is) != "null"
		if observed || p.SourceRule != "" {
			out = append(out, Value{Key: p.Name, Class: k.classOf(p.Name)})
		}
	}
	return out, nil
}

// shapeOf reads what a driver declares that decides its telemetry; a driver
// with no spec (a stub) declares none.
func (r reader) shapeOf(name string, cache map[string]transport.DriverShape) (transport.DriverShape, error) {
	if s, ok := cache[name]; ok || name == "" {
		return s, nil
	}
	var d struct {
		Spec *struct {
			Polls     []json.RawMessage `json:"polls"`
			Listeners []json.RawMessage `json:"listeners"`
			Session   json.RawMessage   `json:"session"`
		} `json:"spec"`
	}
	if err := r.get("/drivers/"+url.PathEscape(name), &d); err != nil {
		return transport.DriverShape{}, err
	}
	var s transport.DriverShape
	if d.Spec != nil {
		s = transport.DriverShape{Polls: len(d.Spec.Polls) > 0, Listeners: len(d.Spec.Listeners) > 0,
			Session: len(d.Spec.Session) > 0 && string(d.Spec.Session) != "null"}
	}
	cache[name] = s
	return s, nil
}

// Collect reads the lab at base into a Snapshot: each component's endpoints
// (answered or not, and the built-in keys landed under each), each owner's
// values with their writer class, and each driver's shape.
func Collect(ctx context.Context, c *http.Client, base string) (Snapshot, error) {
	r := reader{ctx: ctx, c: c, base: base}
	k, err := r.catalog()
	if err != nil {
		return Snapshot{}, err
	}
	var comps struct {
		Components []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Product string `json:"product"`
		} `json:"components"`
	}
	if err := r.get("/components"+listQuery, &comps); err != nil {
		return Snapshot{}, err
	}
	var eps struct {
		Endpoints []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Transport   string `json:"transport"`
			ComponentID string `json:"component_id"`
			Driver      string `json:"driver"`
		} `json:"endpoints"`
	}
	if err := r.get("/endpoints"+listQuery, &eps); err != nil {
		return Snapshot{}, err
	}
	shapes := map[string]transport.DriverShape{}
	var snap Snapshot
	for _, comp := range comps.Components {
		cc := Component{Name: comp.Name, Product: comp.Product}
		var reach struct {
			Endpoints []struct {
				ID       string  `json:"id"`
				LastSeen *string `json:"last_seen"`
				Verdict  *struct {
					Value string `json:"value"`
				} `json:"verdict"`
			} `json:"endpoints"`
		}
		var mine []int
		for i, e := range eps.Endpoints {
			if e.ComponentID == comp.ID {
				mine = append(mine, i)
			}
		}
		if len(mine) > 0 {
			if err := r.get("/components/"+comp.ID+"/reachability", &reach); err != nil {
				return Snapshot{}, err
			}
		}
		var metrics struct {
			Metrics []struct {
				Name      string `json:"metric_type_name"`
				Instance  string `json:"instance"`
				Instances []struct {
					Instance string `json:"instance"`
				} `json:"instances"`
			} `json:"metrics"`
		}
		if len(mine) > 0 {
			if err := r.get("/components/"+comp.ID+"/metrics", &metrics); err != nil {
				return Snapshot{}, err
			}
		}
		metricInstances := map[string]map[string]bool{} // metric -> endpoint names it landed under
		for _, m := range metrics.Metrics {
			set := map[string]bool{m.Instance: true}
			for _, i := range m.Instances {
				set[i.Instance] = true
			}
			metricInstances[m.Name] = set
		}
		propInstances := map[string]map[string]bool{} // property -> endpoint names, read once per key
		for _, i := range mine {
			e := eps.Endpoints[i]
			shape, err := r.shapeOf(e.Driver, shapes)
			if err != nil {
				return Snapshot{}, err
			}
			ep := Endpoint{Name: e.Name, Transport: e.Transport, Shape: shape, Present: map[string]bool{}}
			for _, v := range reach.Endpoints {
				if v.ID != e.ID {
					continue
				}
				if transport.IsPush(e.Transport) {
					ep.Answered = v.LastSeen != nil
				} else {
					ep.Answered = v.Verdict != nil && v.Verdict.Value == "up"
				}
			}
			for _, key := range transport.ExpectedBuiltIn(e.Transport, shape, ep.Answered) {
				if !k.property[key] {
					ep.Present[key] = metricInstances[key][e.Name]
					continue
				}
				if _, ok := propInstances[key]; !ok {
					var s struct {
						Samples []struct {
							Instance string `json:"instance"`
						} `json:"samples"`
					}
					if err := r.get("/components/"+comp.ID+"/properties/"+url.PathEscape(key)+"/samples?limit=2000", &s); err != nil {
						return Snapshot{}, err
					}
					set := map[string]bool{}
					for _, x := range s.Samples {
						set[x.Instance] = true
					}
					propInstances[key] = set
				}
				ep.Present[key] = propInstances[key][e.Name]
			}
			cc.Endpoints = append(cc.Endpoints, ep)
		}
		if cc.Values, err = r.ownerValues("component", comp.ID, k); err != nil {
			return Snapshot{}, err
		}
		snap.Components = append(snap.Components, cc)
	}

	var systems struct {
		Systems []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"systems"`
	}
	if err := r.get("/systems"+listQuery, &systems); err != nil {
		return Snapshot{}, err
	}
	for _, s := range systems.Systems {
		vs, err := r.ownerValues("system", s.ID, k)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Systems = append(snap.Systems, System{Name: s.Name, Values: slices.Clone(vs)})
	}
	return snap, nil
}
