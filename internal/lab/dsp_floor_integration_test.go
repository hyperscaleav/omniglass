//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// dspProduct is the lab's audio processor (#1493).
const dspProduct = "polaris-dsp-16"

// dspFloor states the class-signal floor for the lab's DSPs (#1266) until the
// lab-wide check holds it (#1371): after the warm-up, every placed Polaris
// DSP 16 carries a collected program `mute` and `audio-level`, values from its
// own API beyond the identity and `uptime` snmp-generic read. It returns what
// is missing, empty once every DSP carries both. A lab with no DSP placed is
// a failure, not a pass: the floor would hold of nothing.
func dspFloor(ctx context.Context, c *http.Client, base string) ([]string, error) {
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
	var metrics struct {
		Metrics []struct {
			Name      string `json:"metric_type_name"`
			IsSampled bool   `json:"is_sampled"`
		} `json:"metrics"`
	}
	var samples struct {
		Samples []struct {
			Source string `json:"source"`
		} `json:"samples"`
	}
	var missing []string
	placed := 0
	for _, comp := range comps.Components {
		if comp.Product != dspProduct {
			continue
		}
		placed++
		metrics.Metrics = nil
		if err := get("/components/"+comp.ID+"/metrics", &metrics); err != nil {
			return nil, err
		}
		level := false
		for _, m := range metrics.Metrics {
			level = level || (m.Name == "audio-level" && m.IsSampled)
		}
		if !level {
			missing = append(missing, comp.Name+": audio-level")
		}
		// A property no sample of which has landed reads as an error or an
		// empty list, both a miss.
		samples.Samples = nil
		if err := get("/components/"+comp.ID+"/properties/"+url.PathEscape("mute")+"/samples", &samples); err != nil || len(samples.Samples) == 0 {
			missing = append(missing, comp.Name+": mute")
		}
	}
	if placed == 0 {
		return []string{fmt.Sprintf("no %s placed in the lab", dspProduct)}, nil
	}
	return missing, nil
}
