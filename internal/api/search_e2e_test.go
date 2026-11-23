package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type searchWire struct {
	Locations  []searchEntityWire `json:"locations"`
	Systems    []searchEntityWire `json:"systems"`
	Components []searchEntityWire `json:"components"`
	Catalog    []struct {
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		Label string `json:"label"`
	} `json:"catalog"`
}

type searchEntityWire struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	LocationPath string `json:"location_path"`
	System       string `json:"system"`
}

func names(es []searchEntityWire) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

// searchFixture is two campuses, each holding a system and a component named
// panel-1, so a name search has same-named hits to tell apart and a scoped
// caller has a location to be kept out of.
type searchFixture struct {
	c      *apiClient
	owner  string
	viewer string // viewer role at the east campus only
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "east", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "east-auditorium", "location_type": "building", "parent": "east"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "west", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "west-auditorium", "location_type": "building", "parent": "west"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "av-east", "system_type_id": "board", "location": "east-auditorium"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "av-west", "system_type_id": "board", "location": "west-auditorium"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "panel-1", "product": "generic-device", "location": "east-auditorium"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "panel-1", "product": "generic-device", "location": "west-auditorium"}, http.StatusCreated)

	eastID := entityID(t, c, owner, "/locations", "east")
	viewer := userWithGrants(t, ctx, dsn, "viewer-east", []grant{{role: "viewer", scopeKind: "location", scopeID: eastID}})
	return &searchFixture{c: c, owner: owner, viewer: viewer}
}

func (f *searchFixture) search(tok, q string, limit int, want int) searchWire {
	f.c.t.Helper()
	path := "/search?q=" + url.QueryEscape(q)
	if limit > 0 {
		path += "&limit=" + strconv.Itoa(limit)
	}
	var w searchWire
	if err := json.Unmarshal(f.c.do(tok, http.MethodGet, path, nil, want), &w); err != nil {
		f.c.t.Fatalf("decode search: %v", err)
	}
	return w
}

// A name search finds each kind, and a component carries its location_path: two
// panel-1 come back and the location_path is what tells them apart.
func TestSearchFindsEachKindWithItsPlace(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	got := f.search(f.owner, "auditorium", 0, http.StatusOK)
	if len(got.Locations) != 2 {
		t.Errorf("locations for auditorium = %v, want both auditoriums", names(got.Locations))
	}
	if got.Systems == nil || got.Components == nil || got.Catalog == nil {
		t.Errorf("every group is an array, never null: %+v", got)
	}

	got = f.search(f.owner, "panel", 0, http.StatusOK)
	if len(got.Components) != 2 {
		t.Fatalf("components for panel = %v, want both panel-1", names(got.Components))
	}
	paths := map[string]bool{}
	for _, c := range got.Components {
		paths[c.LocationPath] = true
	}
	if !paths["east / east-auditorium"] || !paths["west / west-auditorium"] {
		t.Errorf("paths = %v, want each panel-1 told apart by the chain it sits under", paths)
	}

	got = f.search(f.owner, "AV-EAST", 0, http.StatusOK)
	if len(got.Systems) != 1 || got.Systems[0].Name != "av-east" || got.Systems[0].LocationPath != "east / east-auditorium" {
		t.Errorf("systems = %+v, want av-east under its location (case-insensitive)", got.Systems)
	}
}

// A viewer scoped to the east campus finds only what is under it: the west
// auditorium, its system and its panel-1 never appear.
func TestSearchStaysInTheCallersScope(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	got := f.search(f.viewer, "auditorium", 0, http.StatusOK)
	if n := names(got.Locations); len(n) != 1 || n[0] != "east-auditorium" {
		t.Errorf("a viewer scoped to east found locations %v, want only east-auditorium", n)
	}
	got = f.search(f.viewer, "av-", 0, http.StatusOK)
	if n := names(got.Systems); len(n) != 1 || n[0] != "av-east" {
		t.Errorf("a viewer scoped to east found systems %v, want only av-east", n)
	}
	got = f.search(f.viewer, "panel", 0, http.StatusOK)
	if len(got.Components) != 1 || got.Components[0].LocationPath != "east / east-auditorium" {
		t.Errorf("a viewer scoped to east found components %+v, want only the east panel-1", got.Components)
	}
	if n := names(f.search(f.viewer, "west", 0, http.StatusOK).Locations); len(n) != 0 {
		t.Errorf("the viewer found %v by naming the location outside their scope", n)
	}
}

// The limit is per kind and is applied in the database; wildcard characters in
// the text are literal; blank text is refused.
func TestSearchLimitsEachKindAndTakesWildcardsLiterally(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)

	if got := f.search(f.owner, "auditorium", 1, http.StatusOK); len(got.Locations) != 1 {
		t.Errorf("limit=1 returned %d locations, want 1", len(got.Locations))
	}
	for _, q := range []string{"%", "_", `\`} {
		got := f.search(f.owner, q, 0, http.StatusOK)
		if len(got.Locations)+len(got.Systems)+len(got.Components) != 0 {
			t.Errorf("q=%q matched fleet rows %+v: a wildcard must be read literally", q, got)
		}
	}
	f.c.do(f.owner, http.MethodGet, "/search?q=", nil, http.StatusUnprocessableEntity)
	f.c.do(f.owner, http.MethodGet, "/search?q=x&limit=500", nil, http.StatusUnprocessableEntity)
}

// Catalog hits come with their kind, only for a caller who may read that
// catalog; the seeded catalog has metrics and drivers by well-known names.
func TestSearchFindsCatalogEntries(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)
	got := f.search(f.owner, "generic", 0, http.StatusOK)
	kinds := map[string]bool{}
	for _, h := range got.Catalog {
		kinds[h.Kind] = true
	}
	if !kinds["product"] {
		t.Errorf("catalog hits for generic = %+v, want the generic-device product", got.Catalog)
	}
	// The east viewer has the floor's catalog reads, and still sees the same
	// platform-wide catalog (it is not scoped to a location).
	if v := f.search(f.viewer, "generic", 0, http.StatusOK); len(v.Catalog) == 0 {
		t.Errorf("a viewer's catalog hits are empty; the catalog is platform-wide")
	}
}

// An exact name outranks a row that merely contains the text, even when the
// other row has no label of its own to compare.
func TestSearchRanksTheExactNameFirst(t *testing.T) {
	t.Parallel()
	f := newSearchFixture(t)
	f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{"name": "annex-lobby", "location_type": "building", "parent": "east"}, http.StatusCreated)
	f.c.do(f.owner, http.MethodPost, "/locations", map[string]any{"name": "lobby", "location_type": "building", "parent": "east"}, http.StatusCreated)
	got := f.search(f.owner, "lobby", 1, http.StatusOK)
	if n := names(got.Locations); len(n) != 1 || n[0] != "lobby" {
		t.Errorf("limit 1 for lobby = %v, want the exact name, not annex-lobby", n)
	}
}
