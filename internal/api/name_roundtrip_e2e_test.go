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

// A name is the address, and the test of that is a ROUND TRIP: what a response
// says about an entity's placement is exactly what a request would say to put it
// there. Before this, a component was created with {"parent": "rack"} and read
// back as {"parent_id": "0198f..."}, so the body could not be fed back to the
// write that produced it and every client had to fetch a second collection and
// join by uuid to render one label.
//
// The guard in name_address_test.go checks the shape of the contract; this checks
// the behaviour, over HTTP, on the three estate entities that had the defect.
func TestPlacementRoundTripsByName(t *testing.T) {
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

	field := func(raw []byte, key string) string {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if v, ok := m[key].(string); ok {
			return v
		}
		return ""
	}
	// A reference carries both forms; these are the id halves.
	mustHave := func(raw []byte, keys ...string) {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, k := range keys {
			if v, present := m[k]; !present || v == "" {
				t.Errorf("response is missing %q: the id is the canonical handle", k)
			}
		}
	}

	// Locations: a site root, then a building under it by name.
	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	b := c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "hq-b1", "location_type": "building", "parent": "hq"}, http.StatusCreated)
	if got := field(b, "parent"); got != "hq" {
		t.Errorf("location parent = %q, want hq (the name the request used)", got)
	}
	mustHave(b, "parent_id")

	// Systems: placed at a location, parented to another system, both by name.
	c.do(tok, http.MethodPost, "/systems", map[string]any{"name": "av", "location": "hq-b1"}, http.StatusCreated)
	s := c.do(tok, http.MethodPost, "/systems",
		map[string]any{"name": "av-sub", "parent": "av", "location": "hq-b1"}, http.StatusCreated)
	if got, want := field(s, "parent"), "av"; got != want {
		t.Errorf("system parent = %q, want %q", got, want)
	}
	if got, want := field(s, "location"), "hq-b1"; got != want {
		t.Errorf("system location = %q, want %q", got, want)
	}
	mustHave(s, "parent_id", "location_id")

	// Components: the body that started this, parent and location both by name.
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "rack", "location": "hq-b1"}, http.StatusCreated)
	comp := c.do(tok, http.MethodPost, "/components",
		map[string]any{"name": "codec", "parent": "rack", "location": "hq-b1"}, http.StatusCreated)
	if got, want := field(comp, "parent"), "rack"; got != want {
		t.Errorf("component parent = %q, want %q", got, want)
	}
	if got, want := field(comp, "location"), "hq-b1"; got != want {
		t.Errorf("component location = %q, want %q", got, want)
	}
	// Both forms, since a client needs the stable handle and a human label.
	mustHave(comp, "parent_id", "location_id")

	// And the read path agrees with the create echo, which is the actual round
	// trip: a GET body could be replayed as the POST that produced it.
	got := c.do(tok, http.MethodGet, "/components/codec", nil, http.StatusOK)
	if field(got, "parent") != "rack" || field(got, "location") != "hq-b1" {
		t.Errorf("GET disagrees with create: parent=%q location=%q",
			field(got, "parent"), field(got, "location"))
	}
}

// A path segment takes either form, so a script holding an id and a human typing
// a name reach the same entity.
func TestPathsAcceptEitherForm(t *testing.T) {
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

	made := c.do(tok, http.MethodPost, "/components", map[string]any{"name": "codec"}, http.StatusCreated)
	var m map[string]any
	if err := json.Unmarshal(made, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatal("create response carried no id")
	}

	byName := c.do(tok, http.MethodGet, "/components/codec", nil, http.StatusOK)
	byID := c.do(tok, http.MethodGet, "/components/"+id, nil, http.StatusOK)
	var a, b map[string]any
	_ = json.Unmarshal(byName, &a)
	_ = json.Unmarshal(byID, &b)
	if a["id"] != b["id"] {
		t.Errorf("the two forms reached different rows: %v vs %v", a["id"], b["id"])
	}
	// A well-formed uuid that is nobody is a clean 404, not a 500.
	c.do(tok, http.MethodGet, "/components/00000000-0000-0000-0000-000000000000", nil, http.StatusNotFound)
}
