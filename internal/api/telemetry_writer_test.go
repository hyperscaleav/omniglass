package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestTelemetryPushRefusesKeysItDoesNotWrite holds the push lane to the
// writer-class invariant (ADR-0161): a push writes only a collected key.
// collection-failed is the node's account of a task it could not run (#1086),
// health is the platform's rollup, and a key a calc rule outputs is that
// rule's, so each is refused by name with the writer that owns it, and none
// lands; a collected key in the same batch still does.
func TestTelemetryPushRefusesKeysItDoesNotWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, c, tok := pushHarness(t, ctx)

	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "avg-uptime", DataType: "float"}); err != nil {
		t.Fatalf("create the calc output type: %v", err)
	}
	if _, err := gw.CreateCalcRule(ctx, "", storage.CalcRuleSpec{Name: "uptime-avg", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "avg-uptime"},
		scope.Set{All: true}); err != nil {
		t.Fatalf("create the calc rule: %v", err)
	}

	var r pushResp
	body := map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "bar-1"},
		"events":     []map[string]any{{"name": "collection-failed", "message": "task t-x: forged"}},
		"properties": []map[string]any{{"name": "health", "value": "healthy"}},
		"metrics":    []map[string]any{{"name": "avg-uptime", "value": 9}, {"name": "uptime", "value": 4242}},
	}
	if err := json.Unmarshal(c.do(tok, http.MethodPost, "/telemetry:push", body, http.StatusAccepted), &r); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	if r.Accepted.Metrics != 1 || r.Accepted.Properties != 0 || r.Accepted.Events != 0 {
		t.Errorf("accepted = %+v, want only uptime", r.Accepted)
	}
	reasons := map[string]string{}
	for _, rej := range r.Rejected {
		reasons[rej.Name] = rej.Reason
	}
	for key, owner := range map[string]string{"collection-failed": "only the node", "health": "health rollup", "avg-uptime": "uptime-avg"} {
		if !strings.Contains(reasons[key], key) || !strings.Contains(reasons[key], owner) {
			t.Errorf("push of %s: reason %q, want it refused by name naming its writer (%s)", key, reasons[key], owner)
		}
	}

	waitFor(t, "the pushed uptime", func() bool {
		m, err := gw.LatestMetricInstance(ctx, "bar-1", "uptime", "")
		return err == nil && m != nil && m.Value == 4242
	})
	evs, err := gw.ListComponentEvents(ctx, "bar-1", 0, 10)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, e := range evs {
		if e.Key == "collection-failed" {
			t.Errorf("a pushed collection-failed landed: %+v", e)
		}
	}
	if m, err := gw.LatestMetric(ctx, "bar-1", "avg-uptime"); err != nil || m != nil {
		t.Errorf("a pushed calc output landed: %+v (%v)", m, err)
	}
}

// TestWriterClassOnTheWire: every catalog type reads its writer class back
// over the API (ADR-0161), and the declared-value route refuses a reserved key
// with a 422 naming it and who writes it, writing nothing.
func TestWriterClassOnTheWire(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	_, c, tok := pushHarness(t, ctx)
	for path, want := range map[string]string{
		"/metric-types/tcp-open":             "node-probe",
		"/property-types/session-state":      "node-probe",
		"/event-types/collection-failed":     "node-probe",
		"/property-types/health":             "platform",
		"/property-types/video-input":        "collected",
		"/metric-types/uptime":               "collected",
		"/event-types/call-started":          "collected",
		"/property-types/endpoint-rejection": "node-probe",
	} {
		var body struct {
			WriterClass string `json:"writer_class"`
		}
		if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if body.WriterClass != want {
			t.Errorf("%s writer_class = %q, want %s", path, body.WriterClass, want)
		}
	}
	for key, value := range map[string]any{"endpoint-reachable": "up", "ssh-host-key": "SHA256:" + strings.Repeat("a", 43), "health": "healthy"} {
		out := c.do(tok, http.MethodPut, "/components/bar-1/properties/"+key, map[string]any{"value": value}, http.StatusUnprocessableEntity)
		if !strings.Contains(string(out), key) || !strings.Contains(string(out), "only") {
			t.Errorf("a declared %s = %s, want a 422 naming the key and its writer", key, out)
		}
	}
}
