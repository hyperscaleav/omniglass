package node

import (
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// A metric sample's ts is whole milliseconds from the node on (#1526): the
// node truncates it where every batch leaves it, the spool, so no probe,
// driver or session has to remember to.

// capturePublisher keeps the bytes the spool ships, decoded.
type capturePublisher struct {
	mu   sync.Mutex
	sent []*ogv1.TelemetryBatch
}

func (c *capturePublisher) publish(_ string, data []byte, _ string) (ackWait, error) {
	var ev ogv1.TelemetryBatch
	if err := proto.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.sent = append(c.sent, &ev)
	c.mu.Unlock()
	return answered(nil), nil
}

// TestSpoolShipsMetricTimestampsOnTheMillisecond: a probe's sample stamped in
// microseconds, one with no ts of its own under a microsecond group ts, and
// the property, event and log lanes of the same group. The metric ts reach
// the stream on their millisecond; the others are sent as they were.
// (Adapted for #1582: the samples ride a task group added to the spool and
// flushed, where they rode a whole batch sent; the group ts is the fallback.)
func TestSpoolShipsMetricTimestampsOnTheMillisecond(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 5, 12, 0, 0, 123456789, time.UTC)
	groupTS := at.Add(time.Second)
	dps := []collection.Sample{
		{Name: "tcp-open", Value: 1, TS: at},
		{Name: "tcp-connect-time", Value: 4},
		{Name: "power-state", Text: "on", IsText: true, TS: at},
	}
	g := groupFor("task-1", dps)
	g.Ts = timestamppb.New(groupTS)
	g.Events = []*ogv1.EventSample{{Name: "input-changed", Message: "hdmi-2", Ts: timestamppb.New(at)}}
	g.TaskLogs = []*ogv1.LogLine{{Message: "line", Ts: timestamppb.New(at)}}

	pub := &capturePublisher{}
	s := newTelemetrySpool("og.v1.node.telemetry", "edge", pub.publish)
	if err := s.add(g); err != nil {
		t.Fatal(err)
	}
	s.flush()
	waitIdle(t, s)

	if len(pub.sent) != 1 || len(pub.sent[0].GetGroups()) != 1 {
		t.Fatalf("%d batches shipped, want 1 of one group", len(pub.sent))
	}
	got := pub.sent[0].GetGroups()[0]
	wantMS := map[string]time.Time{
		"tcp-open":         at.Truncate(time.Millisecond),
		"tcp-connect-time": groupTS.Truncate(time.Millisecond),
	}
	for _, m := range got.GetMetrics() {
		want, ok := wantMS[m.GetName()]
		if !ok {
			t.Fatalf("unexpected metric %s", m.GetName())
		}
		if gotTS := m.GetTs().AsTime(); !gotTS.Equal(want) {
			t.Errorf("%s shipped at %s, want %s", m.GetName(), gotTS.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
		}
	}
	if len(got.GetMetrics()) != len(wantMS) {
		t.Errorf("%d metrics shipped, want %d", len(got.GetMetrics()), len(wantMS))
	}
	if gotTS := got.GetProperties()[0].GetTs().AsTime(); !gotTS.Equal(at) {
		t.Errorf("property ts %s, want %s unchanged", gotTS.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	}
	if gotTS := got.GetEvents()[0].GetTs().AsTime(); !gotTS.Equal(at) {
		t.Errorf("event ts %s, want %s unchanged", gotTS.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	}
	if gotTS := got.GetTaskLogs()[0].GetTs().AsTime(); !gotTS.Equal(at) {
		t.Errorf("log ts %s, want %s unchanged", gotTS.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano))
	}
	if gotTS := got.GetTs().AsTime(); !gotTS.Equal(groupTS) {
		t.Errorf("group ts %s, want %s unchanged", gotTS.Format(time.RFC3339Nano), groupTS.Format(time.RFC3339Nano))
	}
}
