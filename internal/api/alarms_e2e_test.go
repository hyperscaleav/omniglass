package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A component's verdict on the alarms read (#1454): "no alarms" is two
// different facts, and the read must not blur them. A component something has
// observed, with nothing wrong, is healthy; a component nothing has ever
// observed (no sample, no alarm, ever) is unknown, because healthy there would
// be a false green.
func TestComponentAlarmsVerdictIsUnknownUntilSomethingObservesIt(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	verdictOf := func(name string) string {
		t.Helper()
		var env struct {
			Verdict string `json:"verdict"`
		}
		raw := c.do(owner, http.MethodGet, "/components/"+name+"/alarms", nil, http.StatusOK)
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode alarms: %v", err)
		}
		return env.Verdict
	}

	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "cam-1", "product": "generic-device"}, http.StatusCreated)
	if got := verdictOf("cam-1"); got != "unknown" {
		t.Fatalf("a never-observed component's verdict = %q, want unknown (healthy would be a false green)", got)
	}

	// An alarm is an observation; clearing it leaves the component observed
	// and now clean, which is healthy.
	id := raiseFor(t, c, owner, "cam-1", "high", "no-signal", "no signal")
	if got := verdictOf("cam-1"); got != "outage" {
		t.Fatalf("verdict with a high alarm = %q, want outage", got)
	}
	c.do(owner, http.MethodDelete, "/components/cam-1/alarms/"+id, nil, http.StatusNoContent)
	if got := verdictOf("cam-1"); got != "healthy" {
		t.Fatalf("verdict after the alarm cleared = %q, want healthy (observed, nothing wrong)", got)
	}

	// An endpoint that has delivered anything is an observation too.
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "cam-2", "product": "generic-device"}, http.StatusCreated)
	var ep struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/endpoints", map[string]any{"transport": "icmp", "component": "cam-2"}, http.StatusCreated), &ep); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}
	if got := verdictOf("cam-2"); got != "unknown" {
		t.Fatalf("a component whose endpoint nothing has collected from = %q, want unknown", got)
	}
	if err := gw.StampEndpointsSeen(ctx, []string{ep.ID}); err != nil {
		t.Fatalf("stamp endpoint seen: %v", err)
	}
	if got := verdictOf("cam-2"); got != "healthy" {
		t.Fatalf("a component whose endpoint has delivered = %q, want healthy", got)
	}
}
