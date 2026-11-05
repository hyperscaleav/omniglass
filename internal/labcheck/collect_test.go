package labcheck_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
)

// fakeLab serves the slice of the API the check reads, for one display on an
// ssh endpoint polled by a driver, in a system holding a pushed booking-count.
// Each field is a lever a test pulls to take something away, the way an
// operator removing a profile or an endpoint does.
type fakeLab struct {
	endpoint     bool // the component has its ssh endpoint
	profile      bool // a simulator profile serves it: the device answers and lands class signals
	dropBuiltIn  string
	systemPushed bool
}

func newFakeLab() *fakeLab { return &fakeLab{endpoint: true, profile: true, systemPushed: true} }

func (l *fakeLab) routes() map[string]any {
	instance := func(name string) map[string]any {
		return map[string]any{"metric_type_name": name, "is_sampled": true, "instance": "ssh"}
	}
	var metrics []any
	for _, k := range []string{"tcp-open", "tcp-connect-time", "ssh-handshake-time", "poll-duration"} {
		if k != l.dropBuiltIn && l.endpoint {
			metrics = append(metrics, instance(k))
		}
	}
	if l.profile {
		metrics = append(metrics, map[string]any{"metric_type_name": "uptime", "is_sampled": true, "instance": "ssh"})
	}
	var props []any
	if l.profile {
		props = append(props, map[string]any{"property_type_name": "power-state", "reconciliation": map[string]any{"is": "on"}},
			map[string]any{"property_type_name": "model-number", "reconciliation": map[string]any{"is": "E55"}})
	}
	var endpoints, reach []any
	if l.endpoint {
		endpoints = []any{map[string]any{"id": "ep1", "name": "ssh", "label": "Display SSH", "transport": "ssh", "component_id": "c1", "driver": "boreal-edge"}}
		verdict := "down"
		if l.profile {
			verdict = "up"
		}
		reach = []any{map[string]any{"id": "ep1", "verdict": map[string]any{"value": verdict}}}
	}
	sample := func(key string) map[string]any {
		if l.dropBuiltIn == key {
			return map[string]any{"samples": []any{}}
		}
		return map[string]any{"samples": []any{map[string]any{"instance": "ssh", "value": "up"}}}
	}
	sysMetrics := []any{}
	if l.systemPushed {
		sysMetrics = append(sysMetrics, map[string]any{"metric_type_name": "booking-count", "is_sampled": true})
	}
	return map[string]any{
		"/property-types": map[string]any{"properties": []any{
			map[string]any{"name": "endpoint-reachable", "writer_class": "node-probe"},
			map[string]any{"name": "endpoint-responsive", "writer_class": "node-probe"},
			map[string]any{"name": "session-state", "writer_class": "node-probe"},
			map[string]any{"name": "power-state", "writer_class": "collected"},
			map[string]any{"name": "model-number", "writer_class": "collected"},
		}},
		"/metric-types": map[string]any{"metric_types": []any{
			map[string]any{"name": "tcp-open", "writer_class": "node-probe"},
			map[string]any{"name": "tcp-connect-time", "writer_class": "node-probe"},
			map[string]any{"name": "ssh-handshake-time", "writer_class": "node-probe"},
			map[string]any{"name": "poll-duration", "writer_class": "node-probe"},
			map[string]any{"name": "uptime", "writer_class": "collected"},
			map[string]any{"name": "booking-count", "writer_class": "collected"},
		}},
		"/components":                 map[string]any{"components": []any{map[string]any{"id": "c1", "name": "Display 1", "product": "test-panel-1"}}},
		"/endpoints":                  map[string]any{"endpoints": endpoints},
		"/drivers/boreal-edge":        map[string]any{"spec": map[string]any{"polls": []any{map[string]any{}}, "session": map[string]any{}}},
		"/components/c1/reachability": map[string]any{"endpoints": reach},
		"/components/c1/metrics":      map[string]any{"metrics": metrics},
		"/components/c1/properties":   map[string]any{"properties": props},
		"/components/c1/properties/endpoint-reachable/samples":  sample("endpoint-reachable"),
		"/components/c1/properties/endpoint-responsive/samples": sample("endpoint-responsive"),
		"/components/c1/properties/session-state/samples":       sample("session-state"),
		"/systems":               map[string]any{"systems": []any{map[string]any{"id": "s1", "name": "Boardroom"}}},
		"/systems/s1/metrics":    map[string]any{"metrics": sysMetrics},
		"/systems/s1/properties": map[string]any{"properties": []any{}},
	}
}

func (l *fakeLab) serve(t *testing.T) string {
	t.Helper()
	routes := l.routes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[strings.TrimPrefix(r.URL.Path, "/api/v1")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func check(t *testing.T, l *fakeLab, gaps labcheck.Gaps) []labcheck.Finding {
	t.Helper()
	fs, err := labcheck.Check(context.Background(), http.DefaultClient, l.serve(t), gaps, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestCheckPassesALabThatCarriesEverything(t *testing.T) {
	if fs := check(t, newFakeLab(), nil); len(fs) != 0 {
		t.Fatalf("findings on a full lab:\n%s", render(fs))
	}
}

func TestRemovingAnEndpointFailsBuiltInNamingTheComponent(t *testing.T) {
	l := newFakeLab()
	l.endpoint = false
	fs := check(t, l, nil)
	var builtIn bool
	for _, f := range fs {
		if f.Tier == devseed.TierBuiltIn && strings.Contains(f.String(), "Display 1") {
			builtIn = true
		}
	}
	if !builtIn {
		t.Fatalf("no built-in finding names Display 1:\n%s", render(fs))
	}
}

func TestRemovingAProfileFailsCollectedNamingTheComponent(t *testing.T) {
	l := newFakeLab()
	l.profile = false
	fs := check(t, l, nil)
	var collected bool
	for _, f := range fs {
		if f.Tier == devseed.TierCollected && strings.Contains(f.String(), "Display 1") {
			collected = true
		}
	}
	if !collected {
		t.Fatalf("no collected finding names Display 1:\n%s", render(fs))
	}
}

func TestAMissingMetricAndAMissingPropertyEachFailBuiltIn(t *testing.T) {
	for _, key := range []string{"poll-duration", "ssh-handshake-time", "endpoint-responsive", "session-state"} {
		l := newFakeLab()
		l.dropBuiltIn = key
		fs := check(t, l, nil)
		if len(fs) != 1 || fs[0].Tier != devseed.TierBuiltIn || !strings.Contains(fs[0].String(), key) {
			t.Errorf("dropping %s: findings = %s, want one built-in finding naming it", key, render(fs))
		}
	}
}

func TestASystemWithNoPushedValueFailsTheSystemTier(t *testing.T) {
	l := newFakeLab()
	l.systemPushed = false
	fs := check(t, l, nil)
	if len(fs) != 1 || fs[0].Tier != devseed.TierSystem || !strings.Contains(fs[0].String(), "Boardroom") {
		t.Fatalf("findings = %s, want one system finding naming Boardroom", render(fs))
	}
}

func TestCheckHonoursTheGapsItIsGiven(t *testing.T) {
	l := newFakeLab()
	l.systemPushed = false
	gaps := labcheck.Gaps{devseed.TierSystem: {devseed.AllSystems: {Issue: "#1361"}}}
	if fs := check(t, l, gaps); len(fs) != 0 {
		t.Fatalf("a listed gap still fails:\n%s", render(fs))
	}
	if fs := check(t, newFakeLab(), gaps); len(fs) != 1 || !strings.Contains(fs[0].String(), "stale") {
		t.Fatalf("findings = %s, want the stale entry", render(fs))
	}
}

func TestCheckReturnsAnErrorWhenTheAPIFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) }))
	defer srv.Close()
	if _, err := labcheck.Check(context.Background(), http.DefaultClient, srv.URL, nil, nil); err == nil {
		t.Fatal("Check on a failing API returned no error")
	}
}
