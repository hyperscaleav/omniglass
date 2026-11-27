package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Progress is how far the outpost has come: the simulator's components, and
// the simulated endpoints the outpost collects.
type Progress struct {
	Connected  bool
	Components int
	Running    int
	// Endpoints and Up count the simulated endpoints the outpost dials, but the
	// planted faults and the impaired devices' endpoints; Planted counts those. A pushed endpoint (a webhook)
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
			ComponentID string          `json:"component_id"`
			Status      string          `json:"status"`
			Impairment  json.RawMessage `json:"impairment"`
		} `json:"devices"`
	}
	if err := getJSON(ctx, c, base+"/api/v1/outposts/"+OutpostName+"/simulation", &sim); err != nil {
		return Progress{}, err
	}
	p := Progress{Connected: sim.Connected, Components: len(sim.Devices)}
	for _, d := range sim.Devices {
		if d.Status != "running" {
			continue
		}
		p.Running++
		// A device someone impaired (a seeded fault, #2434, or a presenter's
		// unplug) is broken on purpose, so none of its endpoints is waited on.
		impaired := len(d.Impairment) > 0 && string(d.Impairment) != "null"
		n, err := readReach(ctx, c, base, d.ComponentID, planted, impaired)
		if err != nil {
			return Progress{}, err
		}
		p.Endpoints += n.Endpoints
		p.Up += n.Up
		p.Planted += n.Planted
	}
	return p, nil
}

// reachCount is a component's simulated endpoints as readiness counts them:
// those the outpost dials but the planted faults, those of them up, and the
// planted ones.
type reachCount struct {
	Endpoints, Up, Planted int
}

// readReach reads one component's reachability and counts its endpoints. A
// pushed endpoint (a webhook) carries no verdict by design and is not counted.
// Every endpoint of an impaired component counts as planted.
func readReach(ctx context.Context, c *http.Client, base, componentID string, planted map[string]bool, impaired bool) (reachCount, error) {
	var reach struct {
		Endpoints []struct {
			Label   string          `json:"label"`
			Webhook json.RawMessage `json:"webhook"`
			Verdict *struct {
				Value string `json:"value"`
			} `json:"reachability"`
		} `json:"endpoints"`
	}
	if err := getJSON(ctx, c, base+"/api/v1/components/"+componentID+"/reachability", &reach); err != nil {
		return reachCount{}, err
	}
	var n reachCount
	for _, e := range reach.Endpoints {
		if len(e.Webhook) > 0 && string(e.Webhook) != "null" {
			continue
		}
		if impaired || planted[e.Label] {
			n.Planted++
			continue
		}
		n.Endpoints++
		if e.Verdict != nil && e.Verdict.Value == "up" {
			n.Up++
		}
	}
	return n, nil
}

func getJSON(ctx context.Context, c *http.Client, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return doJSON(c, req, http.StatusOK, into)
}

// doJSON sends req and decodes its answer, which must carry the status want.
func doJSON(c *http.Client, req *http.Request, want int, into any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return fmt.Errorf("%s %s: %s", req.Method, req.URL, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// SeedResult is what the lab's seed one-off reports on its last line.
type SeedResult struct {
	// Outpost is whether the seeded fleet places the lab's outpost.
	Outpost bool
	// Summary is the line the status shows.
	Summary string
	// Notes are what the seed had to say beside its report (SeedNotePrefix
	// lines), shown with the bring-up so a person reads them: a pusher the
	// kept fleet lacks, say (#2194).
	Notes []string
}

// SeedNotePrefix starts a line the seed prints for the person bringing the lab up.
const SeedNotePrefix = "note: "

// SeedMarker prefixes the seed one-off's report line.
const SeedMarker = "omniglass-lab-seed:"

// FormatSeedResult is the report line the seed prints, read back by
// ParseSeedResult.
func FormatSeedResult(r SeedResult) string {
	outpost := "absent"
	if r.Outpost {
		outpost = "present"
	}
	return fmt.Sprintf("%s outpost=%s %s", SeedMarker, outpost, r.Summary)
}

// ParseSeedResult reads the seed's report from its log: the last report line,
// and every note the seed printed.
func ParseSeedResult(log string) SeedResult {
	lines := strings.Split(log, "\n")
	var notes []string
	for _, l := range lines {
		if note, ok := strings.CutPrefix(strings.TrimSpace(l), SeedNotePrefix); ok {
			notes = append(notes, strings.TrimSpace(note))
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), SeedMarker)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		outpost, summary, _ := strings.Cut(rest, " ")
		return SeedResult{Outpost: outpost == "outpost=present", Summary: summary, Notes: notes}
	}
	return SeedResult{Outpost: true, Summary: "seeded", Notes: notes}
}

// unauthenticatedPushers is the pushers whose token the server refuses, by
// name: each is asked who it is (GET /auth/me, the first call a pusher makes,
// #1743) and a 401 names it. A pusher with no token holds none to try. An
// answer that is neither 200 nor 401 (the server busy, a network error) names
// no one: it says nothing about the token. Reads only. Sorted by pusher.
func unauthenticatedPushers(ctx context.Context, c *http.Client, base string, pushers []Pusher) []string {
	var bad []string
	for _, p := range pushers {
		if p.Token == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/auth/me", nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+p.Token)
		resp, err := c.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			bad = append(bad, p.Name)
		}
	}
	sort.Strings(bad)
	return bad
}
