package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/simhub"
	"github.com/jackc/pgx/v5"
)

// A simulated component reports the product it is, not the family that serves
// it (#2832): a 98-inch display's simulation says it is the 98-inch, and still
// lists the line's other sizes as the variants it could change among.
func TestComponentSimulationReportsItsProductsOwnIdentity(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "site-a"}, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	hub := &fakeHub{Registry: simhub.New()}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(hub)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var ids []string
	for i, product := range []string{"boreal-edge-98", "boreal-edge-55"} {
		name := "display-" + product[len(product)-2:]
		p := product
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &p}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		comp, outpost := name, "site-a"
		target := []string{`{"target":"127.60.0.1:80"}`, `{"target":"127.60.0.2:80"}`}[i]
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Outpost: &outpost, Params: []byte(target)}, all); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := conn.QueryRow(ctx, `select id from component where name = $1`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	hub.Pulled("site-a", collection.SimCapabilities{PortOffset: 10000}, ids)

	for _, tc := range []struct{ name, product, size string }{{"display-98", "boreal-edge-98", "98-inch"}, {"display-55", "boreal-edge-55", "55-inch"}} {
		var sim struct {
			Simulated bool     `json:"simulated"`
			Product   string   `json:"product"`
			Profile   string   `json:"profile"`
			Summary   string   `json:"summary"`
			Variants  []string `json:"variants"`
		}
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/components/"+tc.name+"/simulation", nil, http.StatusOK), &sim); err != nil {
			t.Fatal(err)
		}
		if !sim.Simulated || sim.Product != tc.product || sim.Profile != "boreal-edge" {
			t.Fatalf("%s simulation = %+v, want the %s served by the boreal-edge family", tc.name, sim, tc.product)
		}
		if !strings.Contains(sim.Summary, tc.size) {
			t.Errorf("%s summary %q does not say it is the %s", tc.name, sim.Summary, tc.size)
		}
		// The variants are the line's products and nothing else: a family's own
		// name is the wire API it serves, not a product a component could be
		// changed to (#2846 review).
		if want := []string{"boreal-edge-55", "boreal-edge-65", "boreal-edge-75", "boreal-edge-86", "boreal-edge-98"}; !slices.Equal(sim.Variants, want) {
			t.Errorf("%s variants = %v, want exactly %v", tc.name, sim.Variants, want)
		}
	}
}
