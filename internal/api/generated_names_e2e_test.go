package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestLocationGeneratedNameAPI drives #687 over HTTP as an operator meets it: a
// floor created with no name comes back named, the second takes the next
// ordinal, :rename claims the pen and :resetName returns it, and the type's own
// rule round-trips on the registry surface so a console can show which kinds of
// place the platform names.
//
// It is the wire that is under test rather than the gateway. name_generated has
// to reach the console for it to show who owns the name, and a create body that
// still required a name would leave the whole feature unreachable from the
// outside whatever the storage layer can do.
func TestLocationGeneratedNameAPI(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "boi", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "17c", "location_type": "building", "parent": "boi"}, http.StatusCreated)

	var first nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"location_type": "floor", "parent": "17c"}, http.StatusCreated), &first); err != nil {
		t.Fatalf("decode the nameless create: %v", err)
	}
	if first.Name != "1" || !first.NameGenerated {
		t.Fatalf("nameless create = %+v, want name=1 name_generated=true", first)
	}

	var second nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/locations",
		map[string]any{"location_type": "floor", "parent": "17c"}, http.StatusCreated), &second); err != nil {
		t.Fatalf("decode the second create: %v", err)
	}
	if second.Name != "2" {
		t.Fatalf("the second nameless create = %+v, want name=2", second)
	}

	// A nameless create under a type with no rule is a 422 naming the reason,
	// not a 500 and not a row with a blank name.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"location_type": "room", "parent": "17c"}, http.StatusUnprocessableEntity)

	// The pen round trip, addressed by the dotted path: a positional name is
	// only unique within its parent, so "1" on its own is an ambiguous address
	// the moment a second building has a first floor.
	var renamed nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/locations/boi.17c.1:rename",
		map[string]any{"name": "ground"}, http.StatusOK), &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "ground" || renamed.NameGenerated {
		t.Fatalf("rename = %+v, want name=ground name_generated=false", renamed)
	}
	var reset nameBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/locations/boi.17c.ground:resetName", nil, http.StatusOK), &reset); err != nil {
		t.Fatalf("decode resetName: %v", err)
	}
	if reset.Name != "1" || !reset.NameGenerated {
		t.Fatalf("resetName = %+v, want name=1 name_generated=true", reset)
	}

	// The registry surface carries the rule, so a console can tell which types
	// name themselves, and a rule that cannot mint a legal name is refused
	// where it is written rather than at some later create.
	var types struct {
		LocationTypes []struct {
			Name     string `json:"name"`
			NameRule *struct {
				Stem      string `json:"stem"`
				BareFirst bool   `json:"bare_first"`
			} `json:"name_rule"`
		} `json:"location_types"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/location-types", nil, http.StatusOK), &types); err != nil {
		t.Fatalf("decode location types: %v", err)
	}
	seen := map[string]bool{}
	for _, lt := range types.LocationTypes {
		seen[lt.Name] = lt.NameRule != nil
		if lt.Name == "floor" && (lt.NameRule == nil || lt.NameRule.Stem != "") {
			t.Fatalf("the floor type's name_rule = %+v, want a positional rule", lt.NameRule)
		}
	}
	if seen["room"] {
		t.Error("the room type carries a name_rule on the wire, want none: a room's name is the operator's")
	}

	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "wing", "display_name": "Wing", "name_rule": map[string]any{"stem": "Wing"}}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "wing", "display_name": "Wing", "name_rule": map[string]any{"stem": "wing", "bare_first": true}}, http.StatusCreated)
}

// A platform-named location cannot be reclassified to a type with no name rule,
// which is intended (the platform would be left owning a name it can no longer
// mint) and is where most operators will actually meet the refusal: `floor` is
// the only shipped type carrying a rule, so reclassifying a generated floor as
// a room, a building or a campus is the routine misclassification fix and it is
// refused. What the wire owes them is the WAY OUT, and ":rename it first" is
// not something "name it yourself" says: there is no name field on the patch to
// name it in.
func TestReclassifyingAPlatformNamedLocationNamesTheEscape(t *testing.T) {
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

	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "boi", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "17c", "location_type": "building", "parent": "boi"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"location_type": "floor", "parent": "17c"}, http.StatusCreated)

	body := c.do(ownerTok, http.MethodPatch, "/locations/boi.17c.1",
		map[string]any{"location_type": "room"}, http.StatusUnprocessableEntity)
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("decode the refusal: %v", err)
	}
	for _, part := range []string{":rename", "reclassif"} {
		if !strings.Contains(problem.Detail, part) {
			t.Errorf("the 422 detail = %q, want it to mention %q", problem.Detail, part)
		}
	}

	// And the escape it names actually works: claim the name, then reclassify.
	c.do(ownerTok, http.MethodPost, "/locations/boi.17c.1:rename", map[string]any{"name": "lobby"}, http.StatusOK)
	c.do(ownerTok, http.MethodPatch, "/locations/boi.17c.lobby",
		map[string]any{"location_type": "room"}, http.StatusOK)
}
