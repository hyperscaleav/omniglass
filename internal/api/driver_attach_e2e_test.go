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
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The driver spec and attach over HTTP (#813): driver routes validate specs
// (a broken one is a 422 naming the fault), and creating an endpoint with the
// attach shape derives its transport and tasks from the SEEDED snmp-generic
// spec, proving the ship-with driver is attachable end to end. Skipped
// under -short.
func TestDriverAttachAPI(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
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
	for _, name := range []string{"amp-10", "amp-11", "amp-12"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", name, err)
		}
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
			"request": map[string]any{"line": "X"},
			"emits":   []map[string]any{{"name": "warp-factor", "extract": map[string]any{"key": "w"}}},
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
	c.do(ownerTok, http.MethodPost, "/drivers", map[string]any{"name": "a-stub", "label": "A stub"}, http.StatusCreated)
	status, body = c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "a-stub", "component": "amp-9",
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "stub") {
		t.Fatalf("stub driver: status %d body %s, want 422 naming the stub", status, body)
	}

	// A number input takes a JSON number (#901): port is declared kind number,
	// and 10161 lands as the stored string "10161". A string still works, and an
	// edit accepts both.
	storedPort := func(body []byte) string {
		t.Helper()
		var got struct {
			Inputs map[string]string `json:"inputs"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode endpoint %s: %v", body, err)
		}
		return got.Inputs["port"]
	}
	out = c.do(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "snmp-generic", "component": "amp-10",
		"inputs": map[string]any{"host": "10.20.4.62", "port": 10161, "community": "lab-community"},
	}, http.StatusCreated)
	if got := storedPort(out); got != "10161" {
		t.Fatalf("numeric port stored as %q, want \"10161\": %s", got, out)
	}
	out = c.do(ownerTok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "snmp-generic", "component": "amp-11",
		"inputs": map[string]any{"host": "10.20.4.63", "port": "10161", "community": "lab-community"},
	}, http.StatusCreated)
	if got := storedPort(out); got != "10161" {
		t.Fatalf("string port stored as %q, want \"10161\": %s", got, out)
	}
	var made struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &made); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}
	out = c.do(ownerTok, http.MethodPatch, "/endpoints/"+made.ID, map[string]any{"inputs": map[string]any{"port": 10162}}, http.StatusOK)
	if got := storedPort(out); got != "10162" {
		t.Fatalf("numeric port edit stored as %q, want \"10162\": %s", got, out)
	}
	out = c.do(ownerTok, http.MethodPatch, "/endpoints/"+made.ID, map[string]any{"inputs": map[string]any{"port": "10163"}}, http.StatusOK)
	if got := storedPort(out); got != "10163" {
		t.Fatalf("string port edit stored as %q, want \"10163\": %s", got, out)
	}

	// A boolean, an object or an array is a 422 naming the key, on create and edit.
	for name, bad := range map[string]any{"boolean": true, "object": map[string]any{"n": 1}, "array": []int{1}} {
		status, body = c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{
			"driver": "snmp-generic", "component": "amp-12",
			"inputs": map[string]any{"host": "10.20.4.64", "port": bad, "community": "lab-community"},
		})
		if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "inputs.port") {
			t.Fatalf("%s port on create: status %d body %s, want 422 naming inputs.port", name, status, body)
		}
		status, body = c.send(ownerTok, http.MethodPatch, "/endpoints/"+made.ID, map[string]any{"inputs": map[string]any{"port": bad}})
		if status != http.StatusUnprocessableEntity || !strings.Contains(string(body), "inputs.port") {
			t.Fatalf("%s port on edit: status %d body %s, want 422 naming inputs.port", name, status, body)
		}
	}
}
