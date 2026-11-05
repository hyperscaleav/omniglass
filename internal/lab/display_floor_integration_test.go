//go:build labintegration

package lab

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// displayFloorKeys are the class signals a placed Boreal Edge display carries
// from its driver beyond identity and uptime (#1372): its power and the input
// it shows.
var displayFloorKeys = []string{"power-state", "video-input"}

// displayFloor states the component-level floor for the lab's displays until
// the live check of #1371 holds it for every component: after the warm-up,
// every placed Boreal Edge display holds a collected value of each floor key.
// It returns what is missing, as "<component>: <key>", empty once the floor
// holds.
func displayFloor(ctx context.Context, c *http.Client, base string) ([]string, error) {
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
	var missing []string
	displays := 0
	for _, comp := range comps.Components {
		if !strings.HasPrefix(comp.Product, "boreal-edge-") {
			continue
		}
		displays++
		for _, key := range displayFloorKeys {
			var s struct {
				Samples []struct{} `json:"samples"`
			}
			if err := get("/components/"+comp.ID+"/properties/"+url.PathEscape(key)+"/samples", &s); err != nil || len(s.Samples) == 0 {
				missing = append(missing, comp.Name+" ("+comp.ID+"): "+key)
			}
		}
	}
	if displays == 0 {
		missing = append(missing, "no placed Boreal Edge display, so the floor holds nothing")
	}
	sort.Strings(missing)
	return missing, nil
}
