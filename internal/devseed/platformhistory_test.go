package devseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// seededSamples counts the server's seeded samples of every key the history
// writes, per key and instance, over the last two hours before now.
func seededSamples(t *testing.T, ctx context.Context, gw storage.Gateway, serverID string) map[[2]string]int {
	t.Helper()
	got := map[[2]string]int{}
	for _, key := range devseed.PlatformHistoryKeys() {
		samples, err := gw.ListMetricSeries(ctx, "component", serverID, key, 2*time.Hour, 10000)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		for _, s := range samples {
			if s.Source == "devseed" {
				got[[2]string{key, s.Instance}]++
			}
		}
	}
	return got
}

// Seeded platform history (#1790) lands an hour of the server's own figures on
// its platform component, a sample a minute for each key and instance the
// panels read, so a capture of platform health draws full lines. A second run
// writes nothing: the history is its own sentinel.
func TestSeedPlatformHistoryLandsAnHourOncePerSeries(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, devseed.BootSeededDSN(t), devseed.SecretProvider())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	now := time.Now()
	if err := devseed.SeedPlatformHistory(ctx, gw, "server", now); err != nil {
		t.Fatalf("seed platform history: %v", err)
	}
	serverID, err := gw.EnsureServerComponent(ctx, "server")
	if err != nil {
		t.Fatal(err)
	}
	first := seededSamples(t, ctx, gw, serverID)
	want := devseed.PlatformHistorySeries()
	if len(want) == 0 {
		t.Fatal("the history names no series")
	}
	for _, s := range want {
		if n := first[s]; n != devseed.PlatformHistoryMinutes {
			t.Errorf("%s %q: %d seeded samples, want %d", s[0], s[1], n, devseed.PlatformHistoryMinutes)
		}
	}
	if err := devseed.SeedPlatformHistory(ctx, gw, "server", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("seed platform history again: %v", err)
	}
	second := seededSamples(t, ctx, gw, serverID)
	for s, n := range second {
		if n != first[s] {
			t.Errorf("%s %q: a second run moved its seeded samples from %d to %d, want none added", s[0], s[1], first[s], n)
		}
	}
}

// A seed that is not asked for platform history writes none: the dev fleet
// lands no sample on any platform component, so a learner's lab reads only
// what its own server measured.
func TestFleetSeedWritesNoPlatformHistory(t *testing.T) {
	ctx, conn, _ := seededFleet(t)
	var n int
	if err := conn.QueryRow(ctx, `
		select count(*) from metric m
		join metric_series s on s.id = m.series_id
		where s.component_id in (select platform_component_id from platform_server)
		   or s.component_id in (select platform_component_id from node)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the dev fleet seed wrote %d samples on platform components, want none", n)
	}
}
