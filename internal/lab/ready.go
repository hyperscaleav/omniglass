package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Progress is how far the node has come: the simulator's components, and
// the simulated endpoints the node collects.
type Progress struct {
	Connected  bool
	Components int
	Running    int
	// Endpoints and Up count the simulated endpoints the node dials, but the
	// planted faults; Planted counts those. A pushed endpoint (a webhook)
	// carries no reachability verdict by design and is not counted.
	Endpoints int
	Up        int
	Planted   int
}

// readProgress reads the simulator's report and each running component's
// reachability through the API, as the console does.
func readProgress(ctx context.Context, c *http.Client, base string, planted map[string]bool) (Progress, error) {
	var sim struct {
		Connected bool `json:"connected"`
		Devices   []struct {
			ComponentID string `json:"component_id"`
			Status      string `json:"status"`
		} `json:"devices"`
	}
	if err := getJSON(ctx, c, base+"/api/v1/nodes/"+NodeName+"/simulation", &sim); err != nil {
		return Progress{}, err
	}
	p := Progress{Connected: sim.Connected, Components: len(sim.Devices)}
	for _, d := range sim.Devices {
		if d.Status != "running" {
			continue
		}
		p.Running++
		var reach struct {
			Endpoints []struct {
				Label   string          `json:"label"`
				Webhook json.RawMessage `json:"webhook"`
				Verdict *struct {
					Value string `json:"value"`
				} `json:"verdict"`
			} `json:"endpoints"`
		}
		if err := getJSON(ctx, c, base+"/api/v1/components/"+d.ComponentID+"/reachability", &reach); err != nil {
			return Progress{}, err
		}
		for _, e := range reach.Endpoints {
			if len(e.Webhook) > 0 && string(e.Webhook) != "null" {
				continue
			}
			if planted[e.Label] {
				p.Planted++
				continue
			}
			p.Endpoints++
			if e.Verdict != nil && e.Verdict.Value == "up" {
				p.Up++
			}
		}
	}
	return p, nil
}

func getJSON(ctx context.Context, c *http.Client, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// SeedResult is what the lab's seed one-off reports on its last line.
type SeedResult struct {
	// Node is whether the seeded fleet places the lab's node.
	Node bool
	// Summary is the line the status shows.
	Summary string
}

// SeedMarker prefixes the seed one-off's report line.
const SeedMarker = "omniglass-lab-seed:"

// FormatSeedResult is the report line the seed prints, read back by
// ParseSeedResult.
func FormatSeedResult(r SeedResult) string {
	node := "absent"
	if r.Node {
		node = "present"
	}
	return fmt.Sprintf("%s node=%s %s", SeedMarker, node, r.Summary)
}

// ParseSeedResult reads the seed's report from its log.
func ParseSeedResult(log string) SeedResult {
	lines := strings.Split(log, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), SeedMarker)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		node, summary, _ := strings.Cut(rest, " ")
		return SeedResult{Node: node == "node=present", Summary: summary}
	}
	return SeedResult{Node: true, Summary: "seeded"}
}
