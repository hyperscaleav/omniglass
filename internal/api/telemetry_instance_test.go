package api_test

import (
	"context"
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

// TestPushRefusesAnInstanceNamingNoEndpoint holds the push lane to ADR-0456's
// meaning of instance: an observed row's instance is the observer, and on the
// push lane that is an endpoint of the owner, named. A push naming an instance
// no endpoint on the owner carries is refused whole with a 422 that names the
// instance and lists the owner's endpoints, rather than landing a series every
// owner-level read would then treat as a retired observer's. A push naming the
// owner's endpoint, or no instance at all, lands.
func TestPushRefusesAnInstanceNamingNoEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(make([]byte, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := bootstrapOwnerTok(t, ctx, gw)
	all := scope.Set{All: true}
	for _, name := range []string{"bar-1", "bare-1"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	comp := "bar-1"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "webhook", Component: &comp}, all); err != nil {
		t.Fatalf("create the webhook endpoint: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(discardPublisher{})))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	push := func(owner, lane string, item map[string]any, want int) string {
		t.Helper()
		return string(c.do(tok, http.MethodPost, "/telemetry:push", map[string]any{
			"owner": map[string]any{"kind": "component", "ref": owner},
			lane:    []map[string]any{item},
		}, want))
	}

	for lane, item := range map[string]map[string]any{
		"metrics":    {"name": "room-temperature", "instance": "rx-2", "value": 21.5},
		"properties": {"name": "video-input", "instance": "rx-2", "value": "hdmi3"},
		"events":     {"name": "input-changed", "instance": "rx-2", "message": "hdmi3"},
	} {
		body := push("bar-1", lane, item, http.StatusUnprocessableEntity)
		if !strings.Contains(body, `rx-2`) || !strings.Contains(body, "webhook") {
			t.Fatalf("%s on an unknown instance: %s, want a 422 naming rx-2 and listing the endpoint webhook", lane, body)
		}
	}
	if body := push("bare-1", "properties", map[string]any{"name": "video-input", "instance": "webhook", "value": "hdmi3"}, http.StatusUnprocessableEntity); !strings.Contains(body, "no endpoints") {
		t.Fatalf("an instance on a component with no endpoints: %s, want a 422 saying it has none", body)
	}
	push("bar-1", "properties", map[string]any{"name": "video-input", "instance": "webhook", "value": "hdmi3"}, http.StatusAccepted)
	push("bar-1", "properties", map[string]any{"name": "video-input", "value": "hdmi1"}, http.StatusAccepted)
}
