package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestSoakVerdictReadsThePlatformThroughItsAPI drives the soak verdict's read
// against the real routes: the platform health view and the platform system's
// alarm history, as an operator's token reaches them. A clean platform passes;
// an alarm raised on the server and left open fails; once cleared inside the
// tolerance it passes again; a delivery window that lost something fails.
func TestSoakVerdictReadsThePlatformThroughItsAPI(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	r := newPlatformViewRig(t)
	gw, err := storage.NewPG(r.ctx, r.dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)

	// getAs reads as a token does through the CLI: the API's own path, and
	// anything but a 200 is the read's error.
	getAs := func(tok string) lab.SoakGetter {
		return func(_ context.Context, path string, into any) error {
			status, body := r.c.send(tok, http.MethodGet, strings.TrimPrefix(path, "/api/v1"), nil)
			if status != http.StatusOK {
				return fmt.Errorf("GET %s = %d: %s", path, status, body)
			}
			return json.Unmarshal(body, into)
		}
	}
	get := getAs(setupAllViewer(t, r.ctx, r.dsn, "soak-judge"))

	record := func(loss bool) {
		t.Helper()
		at := time.Now().UTC().Add(-time.Minute)
		in, out := 20.0, 20.0
		if loss {
			out = 19
		}
		var ms []storage.PlatformMetric
		for _, id := range []string{r.node, r.server} {
			ms = append(ms,
				storage.PlatformMetric{ComponentID: id, Key: writerclass.DeliveryInCount, Instance: "metric", Value: in, TS: at},
				storage.PlatformMetric{ComponentID: id, Key: writerclass.DeliveryOutCount, Instance: "metric", Value: out, TS: at},
				storage.PlatformMetric{ComponentID: id, Key: writerclass.DeliveryLossRatio, Instance: "metric", Value: (in - out) / in, TS: at})
		}
		if err := gw.RecordPlatformMetrics(r.ctx, ms); err != nil {
			t.Fatalf("record delivery windows: %v", err)
		}
	}
	judge := func() (lab.SoakReading, lab.SoakVerdict) {
		t.Helper()
		until := time.Now().UTC()
		reading, err := lab.ReadSoak(r.ctx, get, until.Add(-lab.SoakMinWindow-time.Hour), until)
		if err != nil {
			t.Fatalf("ReadSoak: %v", err)
		}
		return reading, lab.JudgeSoak(reading, lab.SoakAlarmTolerance)
	}

	record(false)
	reading, v := judge()
	if reading.Build != "v9.9.9" {
		t.Errorf("build = %q, want the build the server's boot wrote", reading.Build)
	}
	if len(reading.Losses) == 0 {
		t.Fatal("no loss ratio read from the page")
	}
	if !v.Pass {
		t.Fatalf("a clean platform failed: %v", v.Reasons)
	}

	var view struct {
		Servers []struct {
			Name string `json:"name"`
		} `json:"servers"`
	}
	if err := get(r.ctx, "/api/v1/views/platform", &view); err != nil || len(view.Servers) != 1 {
		t.Fatalf("servers = %+v, %v", view.Servers, err)
	}
	server := view.Servers[0].Name
	var alarm struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(r.c.do(r.owner, http.MethodPost, "/components/"+server+"/alarms",
		map[string]any{"severity": "high", "message": "ingest is behind"}, http.StatusCreated), &alarm); err != nil {
		t.Fatal(err)
	}
	if _, v = judge(); v.Pass || !strings.Contains(strings.Join(v.Reasons, "\n"), "still open") {
		t.Errorf("an alarm left open: verdict %+v, want a fail naming it still open", v)
	}

	r.c.do(r.owner, http.MethodDelete, "/components/"+server+"/alarms/"+alarm.ID, nil, http.StatusNoContent)
	if reading, v = judge(); !v.Pass || len(reading.Alarms) != 1 || reading.Alarms[0].ClearedAt == nil {
		t.Errorf("an alarm cleared inside the tolerance: alarms %+v, verdict %+v, want it read as cleared and a pass", reading.Alarms, v)
	}

	record(true)
	if _, v = judge(); v.Pass || !strings.Contains(strings.Join(v.Reasons, "\n"), "loss") {
		t.Errorf("a lossy hop: verdict %+v, want a fail naming the loss", v)
	}

	// A token that cannot read the whole page cannot judge it: a verdict
	// from part of the page would pass what it could not see.
	narrow := getAs(principalWithGrants(t, r.ctx, r.dsn, "soak-node-reader", []grant{{role: "node-reader", scopeKind: "all"}}))
	until := time.Now().UTC()
	if _, err := lab.ReadSoak(r.ctx, narrow, until.Add(-lab.SoakMinWindow), until); err == nil || !strings.Contains(err.Error(), "cannot read the whole platform health page") {
		t.Errorf("a node-read-only token: err = %v, want a refusal naming the page", err)
	}
}
