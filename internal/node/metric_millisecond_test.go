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
// microseconds, one with no ts of its own under a microsecond batch ts, and
// the property, event and log lanes of the same batch. The metric ts reach
// the stream on their millisecond; the others are sent as they were.
func TestSpoolShipsMetricTimestampsOnTheMillisecond(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 123456789, time.UTC)
	batchTS := at.Add(time.Second)
	dps := []collection.Sample{
		{Name: "tcp-open", Value: 1, TS: at},
		{Name: "tcp-connect-time", Value: 4},
		{Name: "power-state", Text: "on", IsText: true, TS: at},
	}
	ev := buildBatch("task-1", "edge", dps)
	ev.Ts = timestamppb.New(batchTS)
	ev.Events = []*ogv1.EventSample{{Name: "input-changed", Message: "hdmi-2", Ts: timestamppb.New(at)}}
	ev.TaskLogs = []*ogv1.LogLine{{Message: "line", Ts: timestamppb.New(at)}}

	pub := &capturePublisher{}
	s := newTelemetrySpool("og.v1.node.telemetry", pub.publish)
	if err := s.send(ev); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, s)

	if len(pub.sent) != 1 {
		t.Fatalf("%d batches shipped, want 1", len(pub.sent))
	}
	got := pub.sent[0]
	wantMS := map[string]time.Time{
		"tcp-open":         at.Truncate(time.Millisecond),
		"tcp-connect-time": batchTS.Truncate(time.Millisecond),
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
	if gotTS := got.GetTs().AsTime(); !gotTS.Equal(batchTS) {
		t.Errorf("batch ts %s, want %s unchanged", gotTS.Format(time.RFC3339Nano), batchTS.Format(time.RFC3339Nano))
	}
}
