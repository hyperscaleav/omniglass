package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestUnboundIdentitiesAPI drives the conduit's inbox read (#1282): an owner
// reads what the conduit heard that no binding places (key, value, count, last
// heard, the keys carried); a viewer out of scope on the conduit gets the
// non-disclosing 404, and an in-scope one its own conduit's empty inbox.
func TestUnboundIdentitiesAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
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
	all := scope.Set{All: true}
	for _, name := range []string{"meetings-cloud", "other-1"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "test-room-id", DataType: "string"}); err != nil {
		t.Fatalf("create the match key: %v", err)
	}
	conduit, err := gw.GetComponent(ctx, "meetings-cloud", all)
	if err != nil {
		t.Fatal(err)
	}
	heard := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if _, err := gw.LandBatch(ctx, storage.IngestBatch{BatchID: "t1", Writer: storage.PushWriter(), Unbound: []storage.UnboundIdentityWrite{
		{ConduitID: conduit.ID, Key: "test-room-id", Value: "r-404", Count: 3, FirstAt: heard, LastAt: heard, Keys: []string{"call-state", "call-started"}},
	}}); err != nil {
		t.Fatalf("land: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	var r struct {
		Component  string `json:"component"`
		Identities []struct {
			Key         string    `json:"key"`
			Value       string    `json:"value"`
			Count       int64     `json:"count"`
			FirstHeard  time.Time `json:"first_heard_at"`
			LastHeard   time.Time `json:"last_heard_at"`
			KeysCarried []string  `json:"keys_carried"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/meetings-cloud/unbound-identities", nil, http.StatusOK), &r); err != nil {
		t.Fatal(err)
	}
	if r.Component != "meetings-cloud" || len(r.Identities) != 1 {
		t.Fatalf("inbox = %+v, want meetings-cloud with one identity", r)
	}
	if u := r.Identities[0]; u.Key != "test-room-id" || u.Value != "r-404" || u.Count != 3 || !u.LastHeard.Equal(heard) || len(u.KeysCarried) != 2 {
		t.Errorf("identity = %+v", u)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var otherID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'other-1'`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodGet, "/components/meetings-cloud/unbound-identities", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/components/other-1/unbound-identities", nil, http.StatusOK)
}
