package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestPushCostsOneScopeCheckPerBatch pins the push route's cost on the ingest
// path (ADR-0752): its scope check and its instance check are made once per
// batch, never per sample. A batch of forty-five observations, every one naming
// the owner's endpoint as its instance, issues exactly the statements a batch
// of one does, counted at the gateway's pool.
func TestPushCostsOneScopeCheckPerBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, counter := storagetest.NewCountingDB(t, storage.WithSecretProvider(secret.NewStaticProvider(make([]byte, 32))))
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := bootstrapOwnerTok(t, ctx, gw)
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	comp := "bar-1"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "webhook", Component: &comp}, all); err != nil {
		t.Fatalf("create the webhook endpoint: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(discardPublisher{})))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	batch := func(n int) map[string]any {
		var metrics, properties, events []map[string]any
		for i := 0; i < n; i++ {
			metrics = append(metrics, map[string]any{"name": "room-temperature", "instance": "webhook", "value": 20 + i})
			properties = append(properties, map[string]any{"name": "firmware-version", "instance": "webhook", "value": fmt.Sprintf("1.%d", i)})
			events = append(events, map[string]any{"name": "input-changed", "instance": "webhook", "message": fmt.Sprintf("hdmi%d", i)})
		}
		return map[string]any{"owner": map[string]any{"kind": "component", "ref": comp},
			"metrics": metrics, "properties": properties, "events": events}
	}
	push := func(n int) (int, []string) {
		counter.Reset()
		c.do(tok, http.MethodPost, "/telemetry:push", batch(n), http.StatusAccepted)
		return counter.N(), counter.Summary()
	}
	push(1) // the first request builds the role index and warms the token path
	one, oneSQL := push(1)
	many, manySQL := push(15)
	if many != one {
		t.Fatalf("a batch of 45 observations issued %d statements, a batch of 3 issued %d: the push route does work per sample\n--- 3:\n%s\n--- 45:\n%s",
			many, one, strings.Join(oneSQL, "\n"), strings.Join(manySQL, "\n"))
	}
}
