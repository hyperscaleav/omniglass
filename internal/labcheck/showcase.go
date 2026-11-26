package labcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Showcase is what one device says, counted lane by lane (#2754): the
// distinct metrics it samples, the distinct properties it has observed, the
// events it caught (sent itself; a rule's derived events are not the device
// talking) and its log lines in the read's window.
type Showcase struct {
	Component    string
	Metrics      int
	Properties   int
	CaughtEvents int
	LogLines     int
}

// Floor is the least a showcase device says on each lane after the warm-up.
type Floor struct {
	Metrics      int
	Properties   int
	CaughtEvents int
	LogLines     int
}

// VideoBarFloor is the conference room's video bar (#2754): it calls, drifts
// and logs on its own over two protocols, so after the warm-up it carries
// 20 metrics, 8 properties, an event it caught and a log line.
var VideoBarFloor = Floor{Metrics: 20, Properties: 8, CaughtEvents: 1, LogLines: 1}

// Short names each lane s falls short of the floor on, with the count it
// reached; empty once s is at the floor.
func (f Floor) Short(s Showcase) []string {
	var out []string
	for _, l := range []struct {
		noun      string
		got, want int
	}{
		{"metrics", s.Metrics, f.Metrics},
		{"properties", s.Properties, f.Properties},
		{"caught events", s.CaughtEvents, f.CaughtEvents},
		{"log lines", s.LogLines, f.LogLines},
	} {
		if l.got < l.want {
			out = append(out, fmt.Sprintf("%s: %d %s, want %d", s.Component, l.got, l.noun, l.want))
		}
	}
	return out
}

// ReadShowcase reads what the member of system on product says, off the API
// at base: the one place the showcase floor reads the lab.
func ReadShowcase(ctx context.Context, c *http.Client, base, system, product string) (Showcase, error) {
	r := reader{ctx: ctx, c: c, base: base}
	var members struct {
		Members []struct {
			Component   string `json:"component"`
			ComponentID string `json:"component_id"`
		} `json:"members"`
	}
	if err := r.get("/systems/"+url.PathEscape(system)+"/members", &members); err != nil {
		return Showcase{}, err
	}
	for _, m := range members.Members {
		var comp struct {
			Product string `json:"product"`
		}
		if err := r.get("/components/"+url.PathEscape(m.ComponentID), &comp); err != nil {
			return Showcase{}, err
		}
		if comp.Product == product {
			return r.showcase(m.Component, m.ComponentID)
		}
	}
	return Showcase{}, fmt.Errorf("system %s has no %s member", system, product)
}

func (r reader) showcase(name, id string) (Showcase, error) {
	s := Showcase{Component: name}
	path := "/components/" + url.PathEscape(id)
	var metrics struct {
		Metrics []struct {
			Name      string `json:"metric_type_name"`
			IsSampled bool   `json:"is_sampled"`
		} `json:"metrics"`
	}
	if err := r.get(path+"/metrics", &metrics); err != nil {
		return s, err
	}
	seen := map[string]bool{}
	for _, m := range metrics.Metrics {
		if m.IsSampled && !seen[m.Name] {
			seen[m.Name] = true
			s.Metrics++
		}
	}
	var props struct {
		Properties []struct {
			Name           string `json:"property_type_name"`
			Reconciliation *struct {
				Is json.RawMessage `json:"is"`
			} `json:"reconciliation"`
		} `json:"properties"`
	}
	if err := r.get(path+"/properties", &props); err != nil {
		return s, err
	}
	seen = map[string]bool{}
	for _, p := range props.Properties {
		observed := p.Reconciliation != nil && len(p.Reconciliation.Is) > 0 && string(p.Reconciliation.Is) != "null"
		if observed && !seen[p.Name] {
			seen[p.Name] = true
			s.Properties++
		}
	}
	var events struct {
		Events []struct {
			Origin string `json:"origin"`
		} `json:"events"`
	}
	if err := r.get(path+"/events", &events); err != nil {
		return s, err
	}
	for _, e := range events.Events {
		if e.Origin == "caught" {
			s.CaughtEvents++
		}
	}
	var logs struct {
		Logs []json.RawMessage `json:"logs"`
	}
	if err := r.get(path+"/logs", &logs); err != nil {
		return s, err
	}
	s.LogLines = len(logs.Logs)
	return s, nil
}
