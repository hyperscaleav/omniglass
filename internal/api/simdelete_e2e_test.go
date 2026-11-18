package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestDeletedOutpostLeavesTheSimulatorRegistry holds that decommissioning an outpost
// drops its simulator from the server's registry in the same process: the
// fleet no longer lists it and its own simulation read is a 404, not a stale
// status.
func TestDeletedOutpostLeavesTheSimulatorRegistry(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	panel, _ := lineProtocolProduct(t, ctx, gw)
	ids := map[string]string{}
	for i, n := range []string{"site-a", "site-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
		comp, outpost := "panel-"+n, n
		created, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp, ProductName: &panel}, all, all, all, all)
		if err != nil {
			t.Fatal(err)
		}
		ids[n] = created.ID
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Outpost: &outpost,
			Params: []byte(`{"target":"127.60.0.` + string(rune('1'+i)) + `:51325"}`)}, all); err != nil {
			t.Fatal(err)
		}
	}
	hub := &fakeHub{Registry: simhub.New()}
	hub.Pulled("site-a", collection.SimCapabilities{}, []string{ids["site-a"]})
	hub.Pulled("site-b", collection.SimCapabilities{}, []string{ids["site-b"]})

	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(hub)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	fleetOutposts := func() []string {
		t.Helper()
		var fleet fleetBody
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/simulation", nil, http.StatusOK), &fleet); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, n := range fleet.Outposts {
			out = append(out, n.Outpost)
		}
		return out
	}

	if got := fleetOutposts(); len(got) != 2 {
		t.Fatalf("before the delete the fleet lists %v, want both outposts", got)
	}
	c.do(ownerTok, http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusOK)

	c.do(ownerTok, http.MethodDelete, "/outposts/site-a", nil, http.StatusNoContent)

	if got := fleetOutposts(); len(got) != 1 || got[0] != "site-b" {
		t.Fatalf("after the delete the fleet lists %v, want only site-b", got)
	}
	if _, ok := hub.Status("site-a"); ok {
		t.Fatal("the registry still holds the deleted outpost")
	}
	c.do(ownerTok, http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusNotFound)
}
