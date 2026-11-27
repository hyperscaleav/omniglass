package lab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// A device someone impaired on purpose (the dev fleet's seeded faults, #2434,
// or a presenter's unplug) is broken on purpose: readiness counts its
// endpoints beside the planted faults and never waits for them to read up,
// while every other device's endpoints still count.
func TestReadProgressCountsAnImpairedDeviceBrokenOnPurpose(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/outposts/"+OutpostName+"/simulation", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"connected": true, "devices": [
			{"component_id": "ok", "status": "running"},
			{"component_id": "dark", "status": "running", "impairment": {"unplugged": true}},
			{"component_id": "slow", "status": "running", "impairment": {"latency_ms": 400}}]}`))
	})
	mux.HandleFunc("/api/v1/components/ok/reachability", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"endpoints": [
			{"label": "", "reachability": {"value": "up"}},
			{"label": "Panel SNMP", "reachability": {"value": "down"}}]}`))
	})
	mux.HandleFunc("/api/v1/components/dark/reachability", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"endpoints": [{"label": "", "reachability": {"value": "down"}}, {"label": ""}]}`))
	})
	mux.HandleFunc("/api/v1/components/slow/reachability", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"endpoints": [{"label": "", "reachability": {"value": "up"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p, err := readProgress(context.Background(), srv.Client(), srv.URL, map[string]bool{"Panel SNMP": true})
	if err != nil {
		t.Fatal(err)
	}
	want := Progress{Connected: true, Components: 3, Running: 3, Endpoints: 1, Up: 1, Planted: 4}
	if p != want {
		t.Errorf("progress = %+v, want %+v", p, want)
	}
}

// The seed's notes ride back with its report, so the bring-up shows them: a
// pusher the kept fleet lacks was a note in a log nobody read (#2194).
func TestParseSeedResultCarriesTheSeedsNotes(t *testing.T) {
	t.Parallel()
	log := "note: this lab's fleet lacks the pusher sim-booking-service\n" +
		FormatSeedResult(SeedResult{Outpost: true, Summary: "kept this lab's fleet from its last run"}) + "\n"
	got := ParseSeedResult(log)
	if !got.Outpost {
		t.Fatalf("outpost not present in %+v", got)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "this lab's fleet lacks the pusher sim-booking-service" {
		t.Fatalf("notes = %q, want the one the seed printed", got.Notes)
	}
	if n := ParseSeedResult(FormatSeedResult(SeedResult{Outpost: true, Summary: "x"})).Notes; len(n) != 0 {
		t.Fatalf("a seed with nothing to say carries notes %q", n)
	}
}

// A pusher the server refuses is named, and one it accepts or one with no
// token is not: the check reads the answer to the pusher's own first call.
func TestUnauthenticatedPushersNamesThoseTheServerRefuses(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer good":
			w.WriteHeader(http.StatusOK)
		case "Bearer busy":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	got := unauthenticatedPushers(context.Background(), srv.Client(), srv.URL, []Pusher{
		{Name: "zeta", Token: "stale"}, {Name: "ok", Token: "good"}, {Name: "webhook"}, {Name: "slow", Token: "busy"}, {Name: "alpha", Token: "other"},
	})
	if want := []string{"alpha", "zeta"}; !slices.Equal(got, want) {
		t.Fatalf("unauthenticated pushers = %v, want %v", got, want)
	}
}
