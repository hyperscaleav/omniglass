package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestRowActionsAPI proves the read side computes per-row `actions` from the same
// per-action scope the gateway enforces: a viewer sees rows but no write actions,
// an owner gets every action on every row, and an exclude_root deploy grant gets
// create on the root (placement) but only create+update on descendants, never on
// the root itself. This is what lets the console hide the buttons the server would
// reject. Skipped under -short.
func TestRowActionsAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})
	body := func(name, parent string) map[string]any {
		b := map[string]any{"name": name, "location_type": "campus"}
		if parent != "" {
			b["parent"] = parent
		}
		return b
	}
	c.do(ownerTok, "POST", "/locations", body("root", ""), http.StatusCreated)
	c.do(ownerTok, "POST", "/locations", body("child", "root"), http.StatusCreated)
	c.do(ownerTok, "POST", "/locations", body("other", ""), http.StatusCreated)
	rootID := entityID(t, c, ownerTok, "/locations", "root")

	listActions := func(tok string) map[string][]string {
		_, b := c.send(tok, "GET", "/locations", nil)
		var env struct {
			Locations []struct {
				Name    string   `json:"name"`
				Actions []string `json:"actions"`
			} `json:"locations"`
		}
		if err := json.Unmarshal(b, &env); err != nil {
			t.Fatalf("decode locations: %v (%s)", err, b)
		}
		m := make(map[string][]string, len(env.Locations))
		for _, l := range env.Locations {
			m[l.Name] = l.Actions
		}
		return m
	}

	// Owner: every row, every action.
	oa := listActions(ownerTok)
	if len(oa) != 3 {
		t.Fatalf("owner should see 3 rows, got %v", oa)
	}
	for name, acts := range oa {
		if !reflect.DeepEqual(acts, []string{"create", "update", "delete"}) {
			t.Fatalf("owner %s actions = %v, want all three", name, acts)
		}
	}

	// Viewer@all: sees every row, but no write actions (read-only).
	viewerTok := principalWithGrants(t, ctx, dsn, "viewer-all", []grant{{role: "viewer", scopeKind: "all"}})
	va := listActions(viewerTok)
	if len(va) != 3 {
		t.Fatalf("viewer should see 3 rows, got %v", va)
	}
	for name, acts := range va {
		if len(acts) != 0 {
			t.Fatalf("viewer %s should have no actions, got %v", name, acts)
		}
	}

	// Deploy @ root with subtree_excl_root: root gets create only (placement keeps
	// the root, but update/delete exclude it, and deploy has no delete); the child
	// gets create+update; "other" is out of scope entirely.
	deployTok := principalWithGrants(t, ctx, dsn, "integrator",
		[]grant{{role: "deploy", scopeKind: "location", scopeID: rootID, scopeOp: "subtree_excl_root"}})
	da := listActions(deployTok)
	if _, ok := da["other"]; ok {
		t.Fatalf("deploy should not see 'other', got %v", da)
	}
	if !reflect.DeepEqual(da["root"], []string{"create"}) {
		t.Fatalf("deploy root actions = %v, want [create] (placement only, root not modifiable)", da["root"])
	}
	if !reflect.DeepEqual(da["child"], []string{"create", "update"}) {
		t.Fatalf("deploy child actions = %v, want [create update] (no delete in the role)", da["child"])
	}
}
