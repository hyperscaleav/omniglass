package labcheck_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/labcheck"
)

// The showcase floor (#2754): a device that talks reaches the floor on every
// lane, and one that falls short is named lane by lane, with the count it
// reached.
func TestAShowcaseShortOfItsFloorIsNamedLaneByLane(t *testing.T) {
	floor := labcheck.VideoBarFloor
	if floor.Metrics != 20 || floor.Properties != 8 || floor.CaughtEvents != 1 || floor.LogLines != 1 {
		t.Fatalf("the video bar's floor = %+v, want 20 metrics, 8 properties, a caught event and a log line", floor)
	}
	whole := labcheck.Showcase{Component: "videobar-1", Metrics: 21, Properties: 14, CaughtEvents: 3, LogLines: 5}
	if short := floor.Short(whole); len(short) != 0 {
		t.Fatalf("a bar at its floor falls short: %v", short)
	}
	quiet := labcheck.Showcase{Component: "videobar-1", Metrics: 11, Properties: 6}
	got := strings.Join(floor.Short(quiet), "; ")
	for _, want := range []string{"videobar-1: 11 metrics, want 20", "6 properties, want 8", "0 caught events, want 1", "0 log lines, want 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("short = %q, want it to name %q", got, want)
		}
	}
}

// ReadShowcase reads the system's member of the product off the API (#2754):
// the distinct metrics it samples, the distinct properties it has observed,
// the events it caught (a rule's derived ones are not the device talking)
// and its log lines.
func TestReadShowcaseCountsWhatTheDeviceSays(t *testing.T) {
	routes := map[string]any{
		"/api/v1/systems/hq-divisible-conference-1/members": map[string]any{"members": []any{
			map[string]any{"component": "display-1", "component_id": "c-display"},
			map[string]any{"component": "videobar-1", "component_id": "c-bar"},
		}},
		"/api/v1/components/c-display": map[string]any{"name": "display-1", "product": "display-product"},
		"/api/v1/components/c-bar":     map[string]any{"name": "videobar-1", "product": "bar-product"},
		"/api/v1/components/c-bar/metrics": map[string]any{"metrics": []any{
			map[string]any{"metric_type_name": "jitter", "is_sampled": true},
			map[string]any{"metric_type_name": "tls-handshake-time", "is_sampled": true, "instance": "https"},
			map[string]any{"metric_type_name": "tls-handshake-time", "is_sampled": true, "instance": "wss"},
			map[string]any{"metric_type_name": "replacement-cost", "is_sampled": false},
		}},
		"/api/v1/components/c-bar/properties": map[string]any{"properties": []any{
			map[string]any{"property_type_name": "app-mode", "reconciliation": map[string]any{"is": "ucc"}},
			map[string]any{"property_type_name": "warranty-end", "reconciliation": map[string]any{"is": nil}},
		}},
		"/api/v1/components/c-bar/events": map[string]any{"events": []any{
			map[string]any{"key": "call-started", "origin": "caught"},
			map[string]any{"key": "rule-fired", "origin": "derived"},
		}},
		"/api/v1/components/c-bar/logs": map[string]any{"logs": []any{map[string]any{"message": "Call started"}, map[string]any{"message": "Call ended"}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	got, err := labcheck.ReadShowcase(context.Background(), srv.Client(), srv.URL, "hq-divisible-conference-1", "bar-product")
	if err != nil {
		t.Fatal(err)
	}
	want := labcheck.Showcase{Component: "videobar-1", Metrics: 2, Properties: 1, CaughtEvents: 1, LogLines: 2}
	if got != want {
		t.Fatalf("read %+v, want %+v", got, want)
	}
	if _, err := labcheck.ReadShowcase(context.Background(), srv.Client(), srv.URL, "hq-divisible-conference-1", "dsp-product"); err == nil {
		t.Fatal("a system with no member of the product read as a showcase, want an error")
	}
}
