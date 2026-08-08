package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
)

func pushDupStrptr(s string) *string { return &s }

// TestTelemetryPushSurvivesDuplicateComponentNames is the push lane's
// regression test for the #627 review's finding 2: the push route resolved
// and scope-checked the owner by uuid (the one and only authorization fence
// on the write, per telemetry.go's own comment), then published the batch
// carrying the component's NAME instead of the id it had just resolved. Under
// the OLD code, once a second component shared that name, the consumer's
// re-resolve (ownerArcValue -> scopedByName) would refuse ambiguously, the
// batch would nak-and-Term after five redeliveries, and the caller would have
// already been told 202 Accepted with no way to learn its data never landed.
//
// This drives the real HTTP route with owner.ref set to the UUID of one of
// two same-named components (the scenario the review calls out as the
// specific silent case: a caller naming an ambiguous ref by NAME already
// fails loudly at GetComponent, so only the uuid-addressed caller is at
// risk), through a real bus, asserting the sample lands on the addressed
// component and not its same-named sibling.
//
// The two components below share a name legitimately, by placement (#627:
// db/migrations/20260808090000_names_scope_to_placement.sql), one per room;
// this used to need component_name_key dropped by raw DDL first, because
// that migration was a later task and this test only needed to prove the
// PUSH ROUTE survives that world, not implement it. The DDL now runs for
// real and the constraint it drops no longer exists to drop a second time
// here, so the hack is gone.
func TestTelemetryPushSurvivesDuplicateComponentNames(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

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

	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "push-room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "push-room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-b: %v", err)
	}
	compA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "lobby-display", LocationName: pushDupStrptr(roomA.Name)}, all)
	if err != nil {
		t.Fatalf("component in room-a: %v", err)
	}
	compB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "lobby-display", LocationName: pushDupStrptr(roomB.Name)}, all)
	if err != nil {
		t.Fatalf("component in room-b: %v", err)
	}
	if compA.ID == compB.ID {
		t.Fatalf("the two components did not actually land on different rows")
	}

	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer busSrv.Shutdown()

	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// owner.ref is compA's UUID: GetComponent resolves it cleanly (the id
	// branch is never ambiguous), so the route's authorization fence passes
	// and the caller gets its 202.
	body := map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": compA.ID},
		"source":  "test",
		"metrics": []map[string]any{{"name": "icmp-rtt-avg", "value": 7.5}},
	}
	c.do(ownerTok, http.MethodPost, "/telemetry:push", body, http.StatusAccepted)

	waitFor(t, "the metric row on compA", func() bool {
		m, err := gw.LatestMetric(ctx, compA.ID, "icmp-rtt-avg")
		return err == nil && m != nil && m.Value == 7.5
	})
	// compB must never see it: if the route had published the name instead
	// of the id, the consumer's re-resolve would have refused the whole
	// batch (ambiguous name), landing on NEITHER component, not compB's.
	// Checking compB is empty here is the cross-contamination half of the
	// assertion, matching the pattern the storage-layer duplicate-name tests
	// already use.
	mB, err := gw.LatestMetric(ctx, compB.ID, "icmp-rtt-avg")
	if err != nil {
		t.Fatalf("latest metric compB: %v", err)
	}
	if mB != nil {
		t.Fatalf("compB icmp-rtt-avg = %+v, want nil (cross-contaminated by the shared name)", mB)
	}
}
