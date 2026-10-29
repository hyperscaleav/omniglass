package simnode

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The booking service keeps one meeting day, the same every day (#1370): a
// place reads booked while a meeting runs and free between, and its count is
// the meetings left in the day, the one in progress included.
func TestTheBookingServiceKeepsAMeetingDay(t *testing.T) {
	day := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at        string
		state     string
		remaining int
	}{
		{"08:59", "free", 4},
		{"09:00", "booked", 4},
		{"09:59", "booked", 4},
		{"10:00", "free", 3},
		{"10:45", "booked", 3},
		{"12:00", "free", 2},
		{"13:30", "booked", 2},
		{"15:10", "booked", 1},
		{"16:30", "free", 0},
		{"23:59", "free", 0},
	} {
		at, err := time.Parse("15:04", c.at)
		if err != nil {
			t.Fatal(err)
		}
		now := day.Add(time.Duration(at.Hour())*time.Hour + time.Duration(at.Minute())*time.Minute)
		if state, n := bookingsAt(now); state != c.state || n != c.remaining {
			t.Errorf("at %s: %s with %d left, want %s with %d", c.at, state, n, c.state, c.remaining)
		}
	}
}

// The simulator's booking service pushes through POST /telemetry:push with
// the lab's push token (#1370): it reads the system it may push to off its
// own grant, and the system's booking-count and booking-state land on the
// system, as observed telemetry from the booking service.
func TestThePusherPushesItsSystemsBookings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
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

	p := newPusher(apiSrv.URL, tok)
	p.now = func() time.Time { return time.Date(2026, 10, 29, 13, 30, 0, 0, time.UTC) }
	owner, err := p.owner(ctx)
	if err != nil || owner != sys.ID {
		t.Fatalf("the pusher's owner = %q, %v; want the system it is granted, %s", owner, err, sys.ID)
	}
	if err := p.push(ctx, owner); err != nil {
		t.Fatalf("push: %v", err)
	}
	eventually(t, "the system's bookings", func() bool {
		ms, err := gw.EffectiveMetrics(ctx, "system", sys.ID, all)
		if err != nil {
			return false
		}
		counted := false
		for _, m := range ms {
			if m.MetricTypeName == "booking-count" && m.IsSampled && string(m.SampleValue) == "2" {
				counted = true
			}
		}
		st, err := gw.LatestValue(ctx, "system", sys.ID, "booking-state", "", "observed", all)
		return counted && err == nil && st != nil && string(st.Value) == `"booked"`
	})
}
