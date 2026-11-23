//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// impairedComponents is the ids of the components the lab's simulator holds
// an impairment for: the dev fleet's seeded faults (#2434), broken on purpose.
func impairedComponents(ctx context.Context, c *http.Client, base string) (map[string]bool, error) {
	var sim struct {
		Devices []struct {
			ComponentID string          `json:"component_id"`
			Impairment  json.RawMessage `json:"impairment"`
		} `json:"devices"`
	}
	if err := getJSON(ctx, c, base+"/api/v1/outposts/"+OutpostName+"/simulation", &sim); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, d := range sim.Devices {
		if len(d.Impairment) > 0 && string(d.Impairment) != "null" {
			out[d.ComponentID] = true
		}
	}
	return out, nil
}

// labAlarm is an open alarm as the fleet's alarm queue reads it.
type labAlarm struct {
	ID          string `json:"id"`
	Component   string `json:"component"`
	ComponentID string `json:"component_id"`
	Rule        string `json:"raised_by_rule"`
	Message     string `json:"message"`
}

func openLabAlarms(ctx context.Context, c *http.Client, base string) ([]labAlarm, error) {
	var out struct {
		Alarms []labAlarm `json:"alarms"`
	}
	err := getJSON(ctx, c, base+"/api/v1/alarms", &out)
	return out.Alarms, err
}

// untrueAlarms is the lab's account of #2434: every open alarm was raised by
// a rule, carries the event that raised it, is one its rule still holds
// firing on the component, and, for a reachability alarm, sits on a component
// whose reachability reads down. It also names any seeded fault whose device
// holds no open alarm. It returns what is not true, empty once all is.
func untrueAlarms(ctx context.Context, c *http.Client, base string) ([]string, error) {
	alarms, err := openLabAlarms(ctx, c, base)
	if err != nil {
		return nil, err
	}
	impaired, err := impairedComponents(ctx, c, base)
	if err != nil {
		return nil, err
	}
	var bad []string
	alarmed := map[string]bool{}
	for _, a := range alarms {
		alarmed[a.ComponentID] = true
		if a.Rule == "" {
			bad = append(bad, fmt.Sprintf("%s: %q has no rule behind it", a.Component, a.Message))
			continue
		}
		var evs struct {
			Events []struct {
				AlarmID string `json:"alarm_id"`
			} `json:"events"`
		}
		if err := getJSON(ctx, c, base+"/api/v1/components/"+a.ComponentID+"/events?limit=200", &evs); err != nil {
			return nil, err
		}
		fired := false
		for _, e := range evs.Events {
			fired = fired || e.AlarmID == a.ID
		}
		if !fired {
			bad = append(bad, fmt.Sprintf("%s: %q has no triggering event", a.Component, a.Message))
		}
		var state struct {
			Firings []struct {
				ComponentID string `json:"component_id"`
			} `json:"firings"`
		}
		if err := getJSON(ctx, c, base+"/api/v1/event-rules/"+url.PathEscape(a.Rule)+"/state", &state); err != nil {
			return nil, err
		}
		firing := false
		for _, f := range state.Firings {
			firing = firing || f.ComponentID == a.ComponentID
		}
		if !firing {
			bad = append(bad, fmt.Sprintf("%s: %q is open while %s no longer fires on it", a.Component, a.Message, a.Rule))
		}
		if a.Rule == "reachability-lost" {
			var reach struct {
				Endpoints []struct {
					Verdict *struct {
						Value string `json:"value"`
					} `json:"reachability"`
				} `json:"endpoints"`
			}
			if err := getJSON(ctx, c, base+"/api/v1/components/"+a.ComponentID+"/reachability", &reach); err != nil {
				return nil, err
			}
			down := false
			for _, e := range reach.Endpoints {
				down = down || (e.Verdict != nil && e.Verdict.Value == "down")
			}
			if !down {
				bad = append(bad, fmt.Sprintf("%s: reachability lost while every endpoint reads up", a.Component))
			}
		}
	}
	for id := range impaired {
		if !alarmed[id] {
			bad = append(bad, fmt.Sprintf("component %s is impaired and holds no open alarm", id))
		}
	}
	return bad, nil
}
