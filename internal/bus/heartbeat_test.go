package bus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// beatMsg is a message that counts the InProgress it is sent, and the
// instant it was acked.
type beatMsg struct {
	fakeMsg
	subject string
	mu      sync.Mutex
	beats   []time.Time
	acked   time.Time
}

func (m *beatMsg) Subject() string { return m.subject }

func (m *beatMsg) InProgress() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beats = append(m.beats, time.Now())
	return nil
}

func (m *beatMsg) DoubleAck(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = time.Now()
	return nil
}

func (m *beatMsg) beatCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.beats)
}

// landWaitStore lands a batch after a wait, as a batch does behind a fold.
type landWaitStore struct {
	outcomeStore
	wait time.Duration
}

func (s *landWaitStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	time.Sleep(s.wait)
	return s.outcomeStore.LandBatch(ctx, b)
}

func TestHeartbeatEveryIsInsideTheAckWait(t *testing.T) {
	for _, ackWait := range []time.Duration{telemetryAckWait, 3 * time.Second, 600 * time.Millisecond} {
		if got := heartbeatEvery(ackWait); got <= 0 || got >= ackWait {
			t.Errorf("heartbeatEvery(%s) = %s, want it positive and shorter than the ack wait", ackWait, got)
		}
	}
	if got := heartbeatEvery(30 * time.Second); got < 10*time.Second || got > 15*time.Second {
		t.Errorf("heartbeatEvery(30s) = %s, want 10 to 15 s (ADR-1529 decision 9)", got)
	}
}

// TestLandingBeatsWhileItRunsAndStopsWhenItReturns: a landing that runs
// across several beats sends InProgress for each, and none after it returns.
func TestLandingBeatsWhileItRunsAndStopsWhenItReturns(t *testing.T) {
	prior := telemetryAckWait
	telemetryAckWait = 60 * time.Millisecond // a beat every 20 ms
	defer func() { telemetryAckWait = prior }()

	s := &Server{}
	m := &beatMsg{subject: "og.v1.telemetry.node-a"}
	want := errTest("the landing's own error")
	err := s.landing(m, func() error {
		time.Sleep(210 * time.Millisecond)
		return want
	})
	if err != want {
		t.Fatalf("landing returned %v, want the landing's own error %v", err, want)
	}
	n := m.beatCount()
	if n < 5 {
		t.Errorf("%d beats in a landing of 210 ms at one every 20 ms, want at least 5", n)
	}
	time.Sleep(100 * time.Millisecond)
	if after := m.beatCount(); after != n {
		t.Errorf("%d beats after the landing returned, want none (it had %d)", after-n, n)
	}
}

// TestLandingThatReturnsAtOnceBeatsNever: an on-time batch lands well inside
// one beat and sends none.
func TestLandingThatReturnsAtOnceBeatsNever(t *testing.T) {
	s := &Server{}
	m := &beatMsg{subject: "og.v1.telemetry.node-a"}
	if err := s.landing(m, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := m.beatCount(); n != 0 {
		t.Errorf("%d beats for a landing that returned at once, want none", n)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// TestHandleTelemetryBeatsWhileItLandsOnEachLane: both lanes' landings send
// InProgress, at an interval shorter than telemetryAckWait, for as long as
// the store takes, and the ack follows the last beat.
func TestHandleTelemetryBeatsWhileItLandsOnEachLane(t *testing.T) {
	prior := telemetryAckWait
	telemetryAckWait = 90 * time.Millisecond // a beat every 30 ms
	defer func() { telemetryAckWait = prior }()

	logs := []*ogv1.LogLine{{Message: "a line"}}
	lanes := []struct {
		name    string
		subject string
		batch   *ogv1.TelemetryBatch
	}{
		{"node", "og.v1.telemetry.node-a", &ogv1.TelemetryBatch{Logs: logs}},
		{"api", collection.APITelemetrySubject, &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Kind: "component", Ref: "bar-1"}, Logs: logs}},
	}
	for _, l := range lanes {
		t.Run(l.name, func(t *testing.T) {
			data, err := proto.Marshal(l.batch)
			if err != nil {
				t.Fatal(err)
			}
			store := &landWaitStore{wait: 400 * time.Millisecond}
			s := &Server{store: store, orphans: newOrphanTally(time.Minute)}
			m := &beatMsg{subject: l.subject}
			m.data = data
			m.header = nats.Header{jetstream.MsgIDHeader: []string{"beat-1"}}
			m.meta = &jetstream.MsgMetadata{NumDelivered: 1}
			s.handleTelemetry(m)

			if m.acked.IsZero() {
				t.Fatalf("the batch was never acked (naked=%v termed=%v)", m.naked, m.termed)
			}
			if n := m.beatCount(); n < 8 {
				t.Fatalf("%d beats across a landing of 400 ms at one every 30 ms, want at least 8", n)
			}
			for i := 1; i < len(m.beats); i++ {
				if gap := m.beats[i].Sub(m.beats[i-1]); gap >= telemetryAckWait {
					t.Errorf("beat %d came %s after the one before, want under the ack wait %s", i, gap, telemetryAckWait)
				}
			}
			if last := m.beats[len(m.beats)-1]; last.After(m.acked) {
				t.Errorf("a beat at %s followed the ack at %s", last, m.acked)
			}
		})
	}
}
