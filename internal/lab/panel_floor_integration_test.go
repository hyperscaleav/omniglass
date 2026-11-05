//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/http"
	"sort"
)

// classFloor is the lab's own account of the class-signal floor (#1266, ruled
// 2026-10-28) for the Newtron touch panels, until #1371 holds every class to
// it: after the warm-up, each placed Panel 7 carries a collected value of its
// class beyond video-input, identity and uptime (its room's scene state),
// landed from an endpoint of its own. It returns the panels missing one.
func classFloor(ctx context.Context, c *http.Client, base string) ([]string, error) {
	get := func(path string, into any) error { return getJSON(ctx, c, base+"/api/v1"+path, into) }
	var comps struct {
		Components []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Product string `json:"product"`
		} `json:"components"`
	}
	if err := get("/components?limit=1000", &comps); err != nil {
		return nil, err
	}
	var eps struct {
		Endpoints []struct {
			Name        string `json:"name"`
			ComponentID string `json:"component_id"`
		} `json:"endpoints"`
	}
	if err := get("/endpoints?limit=1000", &eps); err != nil {
		return nil, err
	}
	var missing []string
	placed := 0
	for _, comp := range comps.Components {
		if comp.Product != "newtron-panel-7" {
			continue
		}
		placed++
		got := false
		for _, e := range eps.Endpoints {
			if e.ComponentID != comp.ID {
				continue
			}
			ok, err := landed(get, comp.ID, comp.Name, e.Name, panelClassSignals)
			if err != nil {
				return nil, err
			}
			if ok {
				got = true
				break
			}
		}
		if !got {
			missing = append(missing, comp.Name)
		}
	}
	if placed == 0 {
		return nil, fmt.Errorf("the lab places no newtron-panel-7")
	}
	sort.Strings(missing)
	return missing, nil
}

// panelClassSignals are the properties a Newtron touch panel's class reports
// beyond video-input.
var panelClassSignals = []string{"scene-state"}
