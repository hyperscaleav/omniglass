package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// An endpoint's audit rows read as JSON (#1088): GET /audit-log carries the
// params and inputs of an attach, an edit and a retry as the objects they
// are, where the write had marshaled the jsonb columns' raw bytes and every
// image read as base64. A driver's spec, the same kind of column, reads as
// JSON too. Skipped under -short.
func TestAnEndpointsAuditRowReadsAsJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)

	ep := attachAuditedEndpoint(t, c, ownerTok)
	c.do(ownerTok, http.MethodPatch, "/endpoints/"+ep, map[string]any{"inputs": map[string]string{"host": "10.20.4.62"}}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ep+":retry", nil, http.StatusOK)

	rows := auditImageRows(t, c, ownerTok, "endpoint")
	create, update, retry := rows["create"], rows["update"], rows["retry"]
	if create == nil || update == nil || retry == nil {
		t.Fatalf("endpoint audit rows = %v, want a create, an update and a retry", rows)
	}
	wantParams := func(host string) map[string]any { return map[string]any{"target": host + ":161"} }
	wantInputs := func(host string) map[string]any {
		return map[string]any{"host": host, "community": "lab-community", "port": "161"}
	}
	for _, c := range []struct {
		row, side string
		image     map[string]any
		host      string
	}{
		{"create", "new", create.New, "10.20.4.61"},
		{"update", "old", update.Old, "10.20.4.61"},
		{"update", "new", update.New, "10.20.4.62"},
		{"retry", "old", retry.Old, "10.20.4.62"},
		{"retry", "new", retry.New, "10.20.4.62"},
	} {
		if got := c.image["Params"]; !reflect.DeepEqual(got, wantParams(c.host)) {
			t.Errorf("%s %s Params = %#v, want the object %v", c.row, c.side, got, wantParams(c.host))
		}
		if got := c.image["Inputs"]; !hasEntries(got, wantInputs(c.host)) {
			t.Errorf("%s %s Inputs = %#v, want an object carrying %v", c.row, c.side, got, wantInputs(c.host))
		}
	}

	c.do(ownerTok, http.MethodPost, "/drivers", map[string]any{
		"name": "audited-poll", "label": "Audited Poll",
		"spec": map[string]any{"version": 1, "transport": "tcp", "polls": []map[string]any{{
			"name": "p", "schedule": map[string]any{"every": "10s"},
			"request": map[string]any{"line": "GET INPUT"},
			"emits":   []map[string]any{{"name": "video-input", "extract": map[string]any{"regex": `^INPUT (\S+)$`}}},
		}}},
	}, http.StatusCreated)
	driver := auditImageRows(t, c, ownerTok, "driver")["create"]
	if driver == nil {
		t.Fatal("no driver create row")
	}
	spec, ok := driver.New["Spec"].(map[string]any)
	if !ok || spec["transport"] != "tcp" {
		t.Fatalf("driver create Spec = %#v, want the spec object", driver.New["Spec"])
	}
}

// An endpoint's secret input is audited by reference, never by value: no
// row of the trail carries the secret's value, readable or inside a base64
// string, while the endpoint's rows name the secret it uses. Skipped under
// -short.
func TestAnEndpointsAuditRowNeverCarriesASecretValue(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	c, ownerTok := auditImagesStack(t)
	ep := attachAuditedEndpoint(t, c, ownerTok)
	c.do(ownerTok, http.MethodPatch, "/endpoints/"+ep, map[string]any{"inputs": map[string]string{"host": "10.20.4.62"}}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ep+":retry", nil, http.StatusOK)

	raw := c.do(ownerTok, http.MethodGet, "/audit-log?limit=500", nil, http.StatusOK)
	if strings.Contains(string(raw), auditedSecretValue) {
		t.Fatal("the audit trail carries the secret value")
	}
	// Encoded is no better than readable: every string in the trail that is
	// base64 is decoded and searched too, wherever the value sits in it.
	var trail any
	if err := json.Unmarshal(raw, &trail); err != nil {
		t.Fatalf("decode audit-log: %v", err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for _, e := range v {
				walk(e)
			}
		case []any:
			for _, e := range v {
				walk(e)
			}
		case string:
			if b, err := base64.StdEncoding.DecodeString(v); err == nil && strings.Contains(string(b), auditedSecretValue) {
				t.Fatalf("the audit trail carries the secret value base64-encoded in %q", v)
			}
		}
	}
	walk(trail)
	if update := auditImageRows(t, c, ownerTok, "endpoint")["update"]; update == nil || !hasEntries(update.New["Inputs"], map[string]any{"community": "lab-community"}) {
		t.Fatalf("the endpoint's update row does not name its secret by reference: %+v", update)
	}
}

// auditedSecretValue is the community the test's secret seals: distinct
// enough that finding it anywhere in the trail is a leak.
const auditedSecretValue = "zebra-violet-7731"

type auditImageRow struct {
	Verb     string         `json:"verb"`
	Resource string         `json:"resource"`
	Old      map[string]any `json:"old"`
	New      map[string]any `json:"new"`
}

// auditImagesStack is a seeded gateway with a secret provider, an owner, a
// component and the snmp community secret an attach references.
func auditImagesStack(t *testing.T) (*apiClient, string) {
	t.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
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
		Fields: map[string]string{"community": auditedSecretValue},
	}, all, true); err != nil {
		t.Fatalf("create secret: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	return &apiClient{t: t, ctx: ctx, base: srv.URL}, ownerTok
}

// attachAuditedEndpoint attaches the seeded snmp-generic driver to amp-9,
// its community input a reference to the secret, and returns the id.
func attachAuditedEndpoint(t *testing.T, c *apiClient, tok string) string {
	t.Helper()
	var ep struct {
		ID string `json:"id"`
	}
	out := c.do(tok, http.MethodPost, "/endpoints", map[string]any{
		"driver": "snmp-generic", "component": "amp-9",
		"inputs": map[string]string{"host": "10.20.4.61", "community": "lab-community"},
	}, http.StatusCreated)
	if err := json.Unmarshal(out, &ep); err != nil || ep.ID == "" {
		t.Fatalf("attach: %v (%s)", err, out)
	}
	return ep.ID
}

// auditImageRows reads one resource's audit rows, the newest per verb.
func auditImageRows(t *testing.T, c *apiClient, tok, resource string) map[string]*auditImageRow {
	t.Helper()
	var out struct {
		Events []auditImageRow `json:"events"`
	}
	raw := c.do(tok, http.MethodGet, "/audit-log?resource="+resource, nil, http.StatusOK)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode audit-log: %v (%s)", err, raw)
	}
	rows := map[string]*auditImageRow{}
	for i := range out.Events {
		if _, seen := rows[out.Events[i].Verb]; !seen {
			rows[out.Events[i].Verb] = &out.Events[i]
		}
	}
	return rows
}

// hasEntries reports whether v is an object holding every entry of want.
func hasEntries(v any, want map[string]any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for k, w := range want {
		if !reflect.DeepEqual(m[k], w) {
			return false
		}
	}
	return true
}
