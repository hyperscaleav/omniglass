package devseed

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Every key on a server panel, and every key one is read against, is seeded
// or exempt with a reason, so a panel key added later cannot draw an empty
// line in a capture unnoticed (#1790).
func TestPlatformHistoryCoversEveryServerPanelKey(t *testing.T) {
	for _, g := range storage.PlatformServerGroups {
		for _, k := range g.Keys {
			for _, name := range []string{k.Name, k.Of} {
				if name == "" {
					continue
				}
				_, seeded := platformHistory[name]
				_, exempt := platformHistoryExempt[name]
				if !seeded && !exempt {
					t.Errorf("panel %s key %s has no seeded history and no exemption", g.Name, name)
				}
				if seeded && exempt {
					t.Errorf("panel %s key %s is both seeded and exempt", g.Name, name)
				}
			}
		}
	}
}

// historyMax is the largest value any minute of key's series takes.
func historyMax(key string) float64 {
	top := 0.0
	for _, s := range platformHistory[key] {
		for i := range PlatformHistoryMinutes {
			top = max(top, historyValue(key, s, i, PlatformHistoryMinutes))
		}
	}
	return top
}

// The seeded hour is a healthy lab's: every failure and queue reads 0, the
// pool sits well under its size, and ingest lags under the 30 seconds that
// reads as falling behind.
func TestPlatformHistoryReadsHealthy(t *testing.T) {
	for _, key := range []string{
		writerclass.DBSlowQueryCount, writerclass.DBPoolWaitCount, writerclass.PushLaneRefusalCount,
		writerclass.BusConsumerPending, writerclass.BusConsumerAckPending, writerclass.CalcPendingDepth,
		writerclass.OutboxQueuedDepth, writerclass.OutboxDueAge,
		writerclass.RetentionBehind, writerclass.RetentionDeletedCount,
	} {
		if top := historyMax(key); top != 0 {
			t.Errorf("%s reaches %v, want 0 the whole hour", key, top)
		}
	}
	if acquired, size := historyMax(writerclass.DBPoolAcquired), historyMax(writerclass.DBPoolMax); acquired > size/4 {
		t.Errorf("the pool reaches %v of %v acquired, want under a quarter", acquired, size)
	}
	if lag := historyMax(writerclass.IngestLagMax); lag >= 30 {
		t.Errorf("ingest lag max reaches %v s, want under 30", lag)
	}
}

// A minute's value depends only on its series and position, so two captures
// draw the same line, and a size that climbs ends the hour at its base.
func TestPlatformHistoryIsDeterministic(t *testing.T) {
	s := platformHistory[writerclass.DatabaseBytes][0]
	if a, b := historyValue(writerclass.DatabaseBytes, s, 7, 60), historyValue(writerclass.DatabaseBytes, s, 7, 60); a != b {
		t.Fatalf("minute 7 read %v then %v", a, b)
	}
	if end := historyValue(writerclass.DatabaseBytes, s, 59, 60); end != s.base {
		t.Fatalf("the newest minute reads %v, want the base %v", end, s.base)
	}
	if start := historyValue(writerclass.DatabaseBytes, s, 0, 60); start >= s.base {
		t.Fatalf("the oldest minute reads %v, want under the base %v as the size climbs", start, s.base)
	}
}
