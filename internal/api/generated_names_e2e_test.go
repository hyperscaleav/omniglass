package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// nameBody is the slice of a system or location response these cases read: the
// name the platform picked and the pen that says who owns it.
type nameBody struct {
	Name          string `json:"name"`
	NameGenerated bool   `json:"name_generated"`
}

// TestSystemGeneratedNameAPI drives #686's system half over HTTP, as an
// operator meets it: a create that omits the name comes back named, a second
// one in the same room takes the ordinal, :rename claims the pen, and
// :resetName is the only way back. The wire is what this asserts, not the
// gateway: name_generated has to reach the console for it to show who owns the
// name at all.
func TestSystemGeneratedNameAPI(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "gen-room", "location_type": "campus"}, http.StatusCreated)

	var first nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"system_type_id": "board", "location": "gen-room"}, http.StatusCreated), &first); err != nil {
		t.Fatalf("decode the nameless create: %v", err)
	}
	if first.Name != "boardroom" || !first.NameGenerated {
		t.Fatalf("nameless create = %+v, want name=boardroom name_generated=true", first)
	}

	var second nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"system_type_id": "board", "location": "gen-room"}, http.StatusCreated), &second); err != nil {
		t.Fatalf("decode the second create: %v", err)
	}
	if second.Name != "boardroom-2" {
		t.Fatalf("the second nameless create = %+v, want name=boardroom-2", second)
	}

	// A nameless create with no system_type is a 422 naming the reason, not a
	// 500 and not a row with a blank name.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"location": "gen-room"}, http.StatusUnprocessableEntity)

	var renamed nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/systems/boardroom:rename",
		map[string]any{"name": "exec-suite"}, http.StatusOK), &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "exec-suite" || renamed.NameGenerated {
		t.Fatalf("rename = %+v, want name=exec-suite name_generated=false", renamed)
	}

	var reset nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/systems/exec-suite:resetName", nil, http.StatusOK), &reset); err != nil {
		t.Fatalf("decode resetName: %v", err)
	}
	if reset.Name != "boardroom" || !reset.NameGenerated {
		t.Fatalf("resetName = %+v, want name=boardroom name_generated=true", reset)
	}

	// A missing system is the ordinary non-disclosing 404, same as :rename.
	c.do(ownerTok, http.MethodPost, "/systems/no-such-system:resetName", nil, http.StatusNotFound)
}

// TestLocationResetNameRefusesAPI pins the seam #687 fills: the verb exists on
// the location surface, is gated by location:rename like the others, and
// answers 422 today because a location_type carries no name rule to generate
// from. The pen ships on the wire too, always false, so the console can render
// one control for all three trees.
func TestLocationResetNameRefusesAPI(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var created nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"name": "reset-room", "location_type": "campus"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.NameGenerated {
		t.Fatalf("a created location = %+v, want name_generated=false", created)
	}

	c.do(ownerTok, http.MethodPost, "/locations/reset-room:resetName", nil, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/locations/no-such-room:resetName", nil, http.StatusNotFound)
}
