package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The driver spec and attach over HTTP (#813): driver routes validate specs
// (a broken one is a 422 naming the fault), and creating an endpoint with the
// attach shape derives its transport and tasks from the SEEDED snmp-generic
// spec, proving the ship-with driver is attachable end to end. Skipped
// under -short.
func TestDriverAttachAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "amp-9"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "lab-community", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "public"},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The seeded snmp-generic driver serves its spec.
	out := c.do(ownerTok, http.MethodGet, "/drivers/snmp-generic", nil, http.StatusOK)
	if !strings.Contains(string(out), `"1.3.6.1.2.1.1.3.0"`) {
		t.Fatalf("seeded driver body carries no spec: %s", out)
	}

	// A custom driver with a broken spec is a 422 naming the fault, on create
	// and on update alike.
	status, body := c.send(ownerTok, http.MethodPost, "/drivers", map[string]any{
		"name": "half-baked", "label": "Half Baked",
		"spec": map[string]any{"version": 1, "transport": "tcp", "polls": []map[string]any{{
			"name": "p", "schedule": map[string]any{"every": "10s"},
			"request":    map[string]any{"line": "X"},
			"datapoints": []map[string]any{{"name": "warp-factor", "extract": map[string]any{"key": "w"}}},
		}}},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "warp-factor") {
		t.Fatalf("broken spec create: status %d body %s, want a 422 naming warp-factor", status, body)
	}

	// Attach the seeded driver: inputs only, no transport, no task authoring.
	out = c.do(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver":    "snmp-generic",
		"component": "amp-9",
		"inputs":    map[string]string{"host": "10.20.4.61", "community": "lab-community"},
	}, http.StatusCreated)
	var ep struct {
		ID        string          `json:"id"`
		Transport string          `json:"transport"`
		Driver    *string         `json:"driver"`
		Inputs    json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal(out, &ep); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}
	if ep.Transport != "snmp" || ep.Driver == nil || *ep.Driver != "snmp-generic" {
		t.Fatalf("attach body = %s, want transport snmp and driver snmp-generic", out)
	}
	if !strings.Contains(string(ep.Inputs), "lab-community") {
		t.Fatalf("attach inputs = %s, want the secret reference by name", ep.Inputs)
	}

	// The derived work is visible on the read-only task surface: the
	// reachability probe plus the scalars poll.
	out = c.do(ownerTok, http.MethodGet, "/tasks", nil, http.StatusOK)
	var taskList struct {
		Tasks []struct {
			EndpointID string          `json:"endpoint_id"`
			Mode       string          `json:"mode"`
			Spec       json.RawMessage `json:"spec"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(out, &taskList); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	var derived int
	var sawScalars bool
	for _, task := range taskList.Tasks {
		if task.EndpointID != ep.ID {
			continue
		}
		derived++
		if strings.Contains(string(task.Spec), `"scalars"`) {
			sawScalars = true
		}
	}
	if derived != 2 || !sawScalars {
		t.Fatalf("derived tasks = %d (scalars %v), want 2 with the scalars function among them", derived, sawScalars)
	}

	// The attach refusals surface as 422s naming the fault.
	status, body = c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "snmp-generic", "component": "amp-9",
		"inputs": map[string]string{"community": "lab-community"},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "host") {
		t.Fatalf("missing input: status %d body %s, want 422 naming host", status, body)
	}
	status, body = c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "no-such-driver", "component": "amp-9",
		"inputs": map[string]string{"host": "h"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown driver: status %d body %s, want 422", status, body)
	}
	// Neither transport nor driver is a 422 before any storage work.
	status, body = c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{"component": "amp-9"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("neither transport nor driver: status %d body %s, want 422", status, body)
	}
	// A stub driver (no spec yet) cannot be attached.
	status, body = c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "kestrel-api", "component": "amp-9",
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "stub") {
		t.Fatalf("stub driver: status %d body %s, want 422 naming the stub", status, body)
	}
}
