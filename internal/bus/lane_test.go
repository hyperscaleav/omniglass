package bus_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// The ingest lane tallies every batch the consumer handles and the time it
// held the lane (#1517), and logs the running tally every so often in the
// line the scale lab's harness reads, so a lab's consumer lane busy row is
// the server's own count and not a probe's guess.
func TestIngestLaneTallyAndLogLine(t *testing.T) {
	f := newDeliveryFixture(t)
	sink := &syncBuffer{}
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, nil)))
	defer slog.SetDefault(prior)
	defer bus.SetLaneLogEvery(20 * time.Millisecond)()

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	if got := srv.LaneTally(); got.Batches != 0 || got.Busy != 0 {
		t.Fatalf("a fresh lane tallied %+v", got)
	}

	if err := srv.PublishTelemetry(f.ctx, pushBatch("lane-1")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.waitLanded(t, "lane-1")
	f.waitDrained(t, srv)
	if err := srv.PublishTelemetry(f.ctx, pushBatch("lane-2")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.waitLanded(t, "lane-2")
	f.waitDrained(t, srv)

	tally := srv.LaneTally()
	if tally.Batches != 2 || tally.Busy <= 0 {
		t.Fatalf("after two batches the lane tallied %+v, want 2 batches and some busy time", tally)
	}

	// The log line carries the cumulative tally; the harness parses it and
	// takes the difference between two.
	deadline := time.Now().Add(5 * time.Second)
	var samples []labfootprint.LaneSample
	for {
		sink.mu.Lock()
		text := sink.buf.String()
		sink.mu.Unlock()
		samples, err = labfootprint.ParseLaneLog(strings.NewReader(text))
		if err != nil {
			t.Fatalf("parse the server's log: %v\n%s", err, text)
		}
		if n := len(samples); n > 0 && samples[n-1].Batches == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no ingest lane line reached 2 batches:\n%s", text)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The logged busy time is the in-process total truncated to milliseconds,
	// so it never exceeds the tally read after the drain.
	last := samples[len(samples)-1]
	if last.Partition != "other" {
		t.Errorf("the push lane's tally logged as partition %q, want other: it is outside the partitions", last.Partition)
	}
	if last.Busy > tally.Busy {
		t.Errorf("the logged busy time %s exceeds the lane's tally %s", last.Busy, tally.Busy)
	}
	// Quiet lanes do not log: the tally line appears only when the count moved.
	sink.mu.Lock()
	before := strings.Count(sink.buf.String(), `msg="ingest lane"`)
	sink.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	sink.mu.Lock()
	after := strings.Count(sink.buf.String(), `msg="ingest lane"`)
	sink.mu.Unlock()
	if after != before {
		t.Errorf("an idle lane kept logging its tally: %d lines, then %d", before, after)
	}
}
