package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A push naming its namespace lands there (#1433): og/push-ns reaches the
// platform's row through the real route and bus while the operator's
// push-ns shadows it, a bare push-ns lands on the operator's, and a
// namespace holding no push-ns is rejected by name and namespace.
func TestAPushNamingItsNamespaceLandsThere(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "push-ns", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertMetricType(ctx, storage.MetricType{Name: "push-ns", DataType: "float", Official: true}); err != nil {
		t.Fatal(err)
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer busSrv.Shutdown()
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	raw := c.do(tok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "codec-1"},
		"metrics": []map[string]any{
			{"namespace": "og", "name": "push-ns", "value": 1},
			{"name": "push-ns", "value": 2},
			{"namespace": "acme", "name": "push-ns", "value": 3},
		},
	}, http.StatusAccepted)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rejected, _ := got["rejected"].([]any)
	if len(rejected) != 1 {
		t.Fatalf("rejected = %v, want acme/push-ns only", got["rejected"])
	}
	if r, _ := rejected[0].(map[string]any); r["namespace"] != "acme" || r["name"] != "push-ns" {
		t.Errorf("rejection = %v, want it to name acme and push-ns", r)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	landed := func() map[float64]string {
		rows, err := conn.Query(ctx, `select m.value, t.namespace from metric m join metric_series s on s.id = m.series_id
			join metric_type t on t.id = s.metric_type_id where t.name = 'push-ns'`)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		out := map[float64]string{}
		for rows.Next() {
			var v float64
			var ns string
			if err := rows.Scan(&v, &ns); err != nil {
				t.Fatal(err)
			}
			out[v] = ns
		}
		return out
	}
	waitFor(t, "both samples landed", func() bool { return len(landed()) == 2 })
	if l := landed(); l[1] != "og" || l[2] != "local" {
		t.Errorf("landed %v, want 1 on og and 2 on local", l)
	}
}
