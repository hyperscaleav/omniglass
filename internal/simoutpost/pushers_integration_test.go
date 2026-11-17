package simnode

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The emulator's booking service pushes through POST /telemetry:push with
// the token the lab minted for its principal (#1370, #1743): it reads the
// system it may push to off its own grant, and the system's booking-count
// and booking-state land on the system as observed telemetry.
func TestTheEmulatorsBookingServiceLandsItsBookings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "boardroom"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if err := devseed.GrantPusher(ctx, gw, "", "sim-booking-service", sys.ID); err != nil {
		t.Fatal(err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.SetServiceToken(ctx, "", "sim-booking-service", hash, prefix); err != nil {
		t.Fatal(err)
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer busSrv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer apiSrv.Close()

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e, err := startPushers(store, apiSrv.URL, []Pusher{{Name: "sim-booking-service", Profile: "booking-service", Token: tok}})
	if err != nil {
		t.Fatalf("startPushers: %v", err)
	}
	defer e.StopAll()
	eventually(t, "the system's bookings", func() bool {
		ms, err := gw.EffectiveMetrics(ctx, "system", sys.ID, all)
		if err != nil {
			return false
		}
		counted := false
		for _, m := range ms {
			if m.MetricTypeName == "booking-count" && m.IsSampled {
				counted = true
			}
		}
		st, err := gw.LatestValue(ctx, "system", sys.ID, "booking-state", "", "observed", all)
		return counted && err == nil && st != nil && (string(st.Value) == `"booked"` || string(st.Value) == `"free"`)
	})
}
