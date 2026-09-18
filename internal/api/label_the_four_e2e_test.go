package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The label of the last four tables, over HTTP, as an operator drives it (#613).
//
// The gateway tests prove the column and the ordering. What only this layer can
// prove is the WIRE: that the label survives the Huma bodies in both directions
// on all four, that clearing one is expressible (an empty string, distinct from
// an omitted field), and that a PATCH which says nothing about the label leaves
// it alone. The last is the one a body shape gets wrong silently, since a
// missing field and an empty one look identical in a map.
func TestTheFourCarryALabelOverTheWire(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(make([]byte, 32))))
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A labelled body, decoded far enough to read the pair.
	type labelled struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	decode := func(raw []byte) labelled {
		t.Helper()
		var l labelled
		if err := json.Unmarshal(raw, &l); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return l
	}

	// --- tag: the name-addressed one -----------------------------------------
	tag := decode(c.do(tok, http.MethodPost, "/tags", map[string]any{
		"name": "cost-center", "label": "Cost Center",
	}, http.StatusCreated))
	if tag.Label != "Cost Center" || tag.Name != "cost-center" {
		t.Errorf("created tag = %+v, want cost-center / Cost Center", tag)
	}
	// A governance PATCH that says nothing about the label leaves it, and the
	// row is still addressed by the name it was created with.
	kept := decode(c.do(tok, http.MethodPatch, "/tags/cost-center", map[string]any{
		"applies_to": []string{"component"},
	}, http.StatusOK))
	if kept.Label != "Cost Center" || kept.Name != "cost-center" {
		t.Errorf("after a governance patch = %+v, want the label untouched at the same address", kept)
	}
	// And an empty label CLEARS it, which an omitted field must not.
	cleared := decode(c.do(tok, http.MethodPatch, "/tags/cost-center", map[string]any{
		"label": "",
	}, http.StatusOK))
	if cleared.Label != "" {
		t.Errorf("tag label = %q after an explicit clear, want empty", cleared.Label)
	}

	// --- variable: the simplest, and the one whose value is now optional ------
	v := decode(c.do(tok, http.MethodPost, "/variables", map[string]any{
		"name": "poll-interval", "label": "Poll Interval", "value_type": "int",
		"owner_kind": "platform", "value": 30,
	}, http.StatusCreated))
	if v.Label != "Poll Interval" {
		t.Errorf("created variable label = %q, want Poll Interval", v.Label)
	}
	// A label-only PATCH: no value on the wire at all. This is what the route's
	// optional `value` buys, and without it a relabel would have to resend the
	// value it is not changing.
	relabelled := decode(c.do(tok, http.MethodPatch, "/variables/"+v.ID, map[string]any{
		"label": "Polling interval (seconds)",
	}, http.StatusOK))
	if relabelled.Label != "Polling interval (seconds)" {
		t.Errorf("variable label = %q after a label-only patch", relabelled.Label)
	}
	var withValue struct {
		Value int `json:"value"`
	}
	// And the other direction: a value-only PATCH leaves the label alone.
	raw := c.do(tok, http.MethodPatch, "/variables/"+v.ID, map[string]any{"value": 60}, http.StatusOK)
	if err := json.Unmarshal(raw, &withValue); err != nil {
		t.Fatalf("decode variable: %v", err)
	}
	if got := decode(raw); got.Label != "Polling interval (seconds)" || withValue.Value != 60 {
		t.Errorf("after a value-only patch: label %q value %d, want the label untouched and 60",
			got.Label, withValue.Value)
	}

	// --- secret: the projections ---------------------------------------------
	s := decode(c.do(tok, http.MethodPost, "/secrets", map[string]any{
		"name": "poll-community", "label": "Polling community", "secret_type": "snmp-community",
		"owner_kind": "platform", "fields": map[string]string{"community": "public"},
	}, http.StatusCreated))
	if s.Label != "Polling community" {
		t.Errorf("created secret label = %q", s.Label)
	}
	var secretList struct {
		Secrets []labelled `json:"secrets"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/secrets", nil, http.StatusOK), &secretList); err != nil {
		t.Fatalf("decode secrets: %v", err)
	}
	seen := false
	for _, got := range secretList.Secrets {
		if got.Name == "poll-community" {
			seen = true
			if got.Label != "Polling community" {
				t.Errorf("listed secret label = %q", got.Label)
			}
		}
	}
	if !seen {
		t.Error("the secret is missing from the directory")
	}
	// A field write leaves the label alone: the two are separate instructions
	// even though they arrive in one body.
	afterField := decode(c.do(tok, http.MethodPatch, "/secrets/"+s.ID, map[string]any{
		"fields": map[string]string{"community": "private"},
	}, http.StatusOK))
	if afterField.Label != "Polling community" {
		t.Errorf("secret label = %q after a field-only patch, want it untouched", afterField.Label)
	}

	// --- interface: the label AT CREATE --------------------------------------
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "product": "generic-device"}, http.StatusCreated)
	first := decode(c.do(tok, http.MethodPost, "/endpoints", map[string]any{
		"transport": "ssh", "component": "codec-1", "label": "Control processor",
	}, http.StatusCreated))
	if first.Label != "Control processor" {
		t.Errorf("interface label = %q on the create response: the label is the ONLY operator-typed "+
			"identity an interface has, so it has to arrive with the row", first.Label)
	}
	if first.Name != "ssh" {
		t.Errorf("interface name = %q, want the derived ssh: a label does not name the row", first.Name)
	}
	// The second of the same protocol, unlabelled, reads its derived name.
	second := decode(c.do(tok, http.MethodPost, "/endpoints", map[string]any{
		"transport": "tcp", "component": "codec-1",
	}, http.StatusCreated))
	if second.Label != "" {
		t.Errorf("interface label = %q on a create with none, want absent", second.Label)
	}
	patched := decode(c.do(tok, http.MethodPatch, "/endpoints/"+second.ID, map[string]any{
		"label": "Streaming control",
	}, http.StatusOK))
	if patched.Label != "Streaming control" || patched.Name != "tcp" {
		t.Errorf("patched interface = %+v, want the label set and the derived name kept", patched)
	}
}
