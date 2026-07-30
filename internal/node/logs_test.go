package node

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

// TestLogSinkCapturesAndDrains proves the sink buffers every record (with its
// bound and inline attrs) and that drain clears the buffer.
func TestLogSinkCapturesAndDrains(t *testing.T) {
	sink := newLogSink(slog.NewTextHandler(io.Discard, nil))
	logger := slog.New(sink).With("node", "site-a")

	logger.Info("worklist pulled", "facility", "collection", "tasks", 3)
	logger.Warn("task skipped", "facility", "collection", "task", "t-icmp")

	recs := sink.drain()
	if len(recs) != 2 {
		t.Fatalf("drain returned %d records, want 2", len(recs))
	}
	if recs[0].message != "worklist pulled" || recs[0].severity != "info" {
		t.Fatalf("record 0 = %+v, want info/worklist pulled", recs[0])
	}
	if recs[0].attrs["node"] != "site-a" || recs[0].attrs["facility"] != "collection" {
		t.Fatalf("record 0 attrs = %v, want bound node + inline facility", recs[0].attrs)
	}
	if recs[1].severity != "warning" {
		t.Fatalf("record 1 severity = %q, want warning", recs[1].severity)
	}

	// A second drain is empty: the buffer cleared.
	if again := sink.drain(); len(again) != 0 {
		t.Fatalf("second drain returned %d records, want 0", len(again))
	}
}

// TestSelfLogEventMapping proves the pure mapping: reserved source/facility lift
// into the LogLine columns, the rest ride the attributes JSON, and severity
// carries through.
func TestSelfLogEventMapping(t *testing.T) {
	recs := []capturedLog{{
		severity: "warning",
		message:  "task skipped",
		attrs:    map[string]any{"source": "collection", "facility": "node", "task": "t-icmp", "node": "site-a"},
	}}
	ev := selfLogBatch("site-a", recs)
	if ev == nil || len(ev.GetLogs()) != 1 {
		t.Fatalf("selfLogBatch = %+v, want one log line", ev)
	}
	l := ev.GetLogs()[0]
	if ev.GetNodeId() != "site-a" {
		t.Fatalf("node id = %q, want site-a", ev.GetNodeId())
	}
	if l.GetMessage() != "task skipped" || l.GetSeverity() != "warning" {
		t.Fatalf("line = %q/%q, want task skipped/warning", l.GetMessage(), l.GetSeverity())
	}
	if l.GetSource() != "collection" || l.GetFacility() != "node" {
		t.Fatalf("source/facility = %q/%q, want collection/node (lifted from attrs)", l.GetSource(), l.GetFacility())
	}
	var rest map[string]any
	if err := json.Unmarshal(l.GetAttributes(), &rest); err != nil {
		t.Fatalf("attributes not JSON: %v", err)
	}
	if _, ok := rest["source"]; ok {
		t.Fatalf("source leaked into attributes: %v", rest)
	}
	if rest["task"] != "t-icmp" || rest["node"] != "site-a" {
		t.Fatalf("attributes = %v, want task + node retained", rest)
	}
}

// TestSelfLogEventDefaultsAndEmpty proves source defaults to "node" when unset and
// that an empty record set ships nothing.
func TestSelfLogEventDefaultsAndEmpty(t *testing.T) {
	if ev := selfLogBatch("site-a", nil); ev != nil {
		t.Fatalf("selfLogBatch(nil) = %+v, want nil (nothing to ship)", ev)
	}
	ev := selfLogBatch("site-a", []capturedLog{{severity: "info", message: "connected to bus", attrs: map[string]any{"url": "nats://x"}}})
	l := ev.GetLogs()[0]
	if l.GetSource() != "node" {
		t.Fatalf("source = %q, want default node", l.GetSource())
	}
	if l.GetFacility() != "" {
		t.Fatalf("facility = %q, want empty", l.GetFacility())
	}
}

// deepNodeWork stands in for arbitrary node code far from the run loop: it holds
// no logger and takes none, it just calls the package-level slog. The self-log
// lane must still carry what it emits.
func deepNodeWork() {
	slog.Warn("probe capability missing", "facility", "collection", "probe", "icmp")
}

// TestSelfLogsAreNodeWide is the emitter contract: once the run loop installs the
// sink as the process default, node code anywhere emits self-logs through the
// package-level slog with no logger threaded to it.
func TestSelfLogsAreNodeWide(t *testing.T) {
	sink := newLogSink(slog.NewTextHandler(io.Discard, nil))
	restore := slog.Default()
	slog.SetDefault(slog.New(sink).With("node", "site-a"))
	defer slog.SetDefault(restore)

	deepNodeWork()

	recs := sink.drain()
	if len(recs) != 1 {
		t.Fatalf("drain returned %d records, want 1 from package-level slog", len(recs))
	}
	if recs[0].message != "probe capability missing" || recs[0].severity != "warning" {
		t.Fatalf("record = %+v, want warning/probe capability missing", recs[0])
	}
	// The run loop's bound attrs still ride along, so the line is attributable.
	if recs[0].attrs["node"] != "site-a" || recs[0].attrs["probe"] != "icmp" {
		t.Fatalf("attrs = %v, want bound node + inline probe", recs[0].attrs)
	}
}

// TestLogSinkBoundsBuffer proves the buffer cannot grow without bound: once every
// node slog call buffers, a chatty node between ticks (or a publish that never
// drains) would otherwise leak. The newest records win and the drain leads with an
// honest overflow line rather than silently losing the count.
func TestLogSinkBoundsBuffer(t *testing.T) {
	sink := newLogSink(slog.NewTextHandler(io.Discard, nil))
	logger := slog.New(sink)
	for i := 0; i < maxBufferedLogs+10; i++ {
		logger.Info("chatty", "i", i)
	}

	recs := sink.drain()
	if len(recs) != maxBufferedLogs+1 {
		t.Fatalf("drain returned %d records, want the cap plus one overflow line", len(recs))
	}
	notice := recs[0]
	if notice.severity != "warning" || notice.attrs["dropped"] != 10 {
		t.Fatalf("overflow notice = %+v, want warning with dropped=10", notice)
	}
	// The oldest were dropped, so the retained window ends at the newest record.
	// slog carries an int attr as int64, which is what rides the attributes JSON.
	if recs[len(recs)-1].attrs["i"] != int64(maxBufferedLogs+9) {
		t.Fatalf("newest retained = %v, want the last emitted record", recs[len(recs)-1].attrs["i"])
	}
}

func TestSeverityOf(t *testing.T) {
	cases := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, "debug"},
		{slog.LevelInfo, "info"},
		{slog.LevelWarn, "warning"},
		{slog.LevelError, "error"},
	}
	for _, c := range cases {
		if got := severityOf(c.level); got != c.want {
			t.Fatalf("severityOf(%v) = %q, want %q", c.level, got, c.want)
		}
	}
}
