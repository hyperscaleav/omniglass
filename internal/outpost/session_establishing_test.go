package outpost

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// #2755: a poll on a held session still making its first connection is not a
// collection failure. It waits its timeout on the first dial and then is held
// (counted held, never failed, no backoff), and lands no collection-failed; a
// poll on a session that is rejected still lands its failure.

// stalledDialer holds every dial until the session ends: a first connection
// that outlasts every poll's timeout.
type stalledDialer struct{}

func (stalledDialer) Dial(ctx context.Context, _ string, _ time.Duration, _ collection.Inbound) (collection.HeldConn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// refusingDialer refuses every dial terminally, so the session reads rejected.
type refusingDialer struct{}

func (refusingDialer) Dial(context.Context, string, time.Duration, collection.Inbound) (collection.HeldConn, error) {
	return nil, &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"}
}

// heldPollRig is a collector over one held tcp endpoint, its poll's timeout
// short so a stalled first dial outlasts it, publishing to a stub stream.
type heldPollRig struct {
	coll     *collector
	sessions *sessionManager
	sched    *pollSchedule
	report   *collectionReport
	spool    *telemetrySpool
	pub      *stubPublisher
	poll     collection.TaskSpec
}

func newHeldPollRig(t *testing.T, dialer collection.SessionDialer) *heldPollRig {
	t.Helper()
	log := &transitionLog{}
	sessions := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	t.Cleanup(sessions.closeAll)
	poll := collection.TaskSpec{
		ID: "p1", Mode: "poll", Transport: "tcp", EndpointID: "ep-1", EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:51325","timeout":"50ms"}`),
		Spec: json.RawMessage(`{"driver": "newtron-panel-nvp", "function": "status",
			"request": {"line": "GET INPUT"},
			"emits": [{"name": "video-input", "lane": "property", "extract": {"regex": "^INPUT (\\S+)$"}}]}`),
	}
	listen := listenTask("l1", "ep-1", "10.0.0.5:51325")
	sessions.reconcile([]collection.TaskSpec{poll, listen})
	log.waitFor(t, "l1", collection.SessionEstablishing)
	pub := &stubPublisher{}
	spool := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	sched := newPollSchedule()
	report := newCollectionReport(time.Minute, 0, gateT0)
	coll := newCollector(spool, "edge", &collection.Runner{Line: &lineRecorder{}}, sessions, sched, report)
	coll.every = func(collection.TaskSpec) time.Duration { return time.Minute }
	return &heldPollRig{coll: coll, sessions: sessions, sched: sched, report: report, spool: spool, pub: pub, poll: poll}
}

// collectionFailures is the collection-failed events the stream stored.
func (r *heldPollRig) collectionFailures(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := r.spool.drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	var out []string
	for _, b := range r.pub.shippedBatches(t) {
		for _, g := range b.GetGroups() {
			for _, e := range g.GetEvents() {
				if e.GetName() == collection.EventCollectionFailed {
					out = append(out, e.GetMessage())
				}
			}
		}
	}
	return out
}

// metrics is the report window's rows for the endpoint, by key.
func (r *heldPollRig) metrics() map[string]float64 {
	out := map[string]float64{}
	for _, rep := range r.report.take(gateT0.Add(time.Hour), map[string]string{"ep-1": r.poll.ID}) {
		for _, s := range rep.samples {
			out[s.Name] = s.Value
		}
	}
	return out
}

func TestAPollOnASessionStillEstablishingRecordsNoCollectionFailed(t *testing.T) {
	t.Parallel()
	r := newHeldPollRig(t, stalledDialer{})
	for range 2 {
		if err := r.coll.runPoll(t.Context(), r.poll); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if st, _ := r.sessions.held["ep-1"].sess.State(); st != collection.SessionEstablishing {
		t.Fatalf("session reads %s, want it still establishing past the poll's timeout", st)
	}
	if got := r.collectionFailures(t); len(got) != 0 {
		t.Fatalf("a poll on an establishing session landed collection-failed: %q", got)
	}
	if _, backoff := r.sched.backoff[r.poll.ID]; backoff {
		t.Fatal("a poll on an establishing session booked a backoff")
	}
	got := r.metrics()
	if got[probekeys.PollHeldCount] != 2 {
		t.Fatalf("poll-held-count = %v, want 2 (each run held): %v", got[probekeys.PollHeldCount], got)
	}
	if _, failed := got[probekeys.PollFailureCount]; failed {
		t.Fatalf("a poll on an establishing session counted as a failure: %v", got)
	}
}

// A run now on a session still establishing answers pending, as one beside a
// stream on its first attempt does, and lands no collection-failed.
func TestARunNowOnASessionStillEstablishingAnswersPending(t *testing.T) {
	t.Parallel()
	r := newHeldPollRig(t, stalledDialer{})
	reply := runNowTask(t.Context(), r.spool, "edge", r.coll.runner, r.sessions, r.sched, r.report, r.poll, time.Now().Add(time.Minute))
	if !reply.Pending || reply.Error == "" || len(reply.Results) != 0 {
		t.Fatalf("run now = %+v, want pending naming the first connection", reply)
	}
	if got := r.collectionFailures(t); len(got) != 0 {
		t.Fatalf("a run now on an establishing session landed collection-failed: %q", got)
	}
}

func TestAPollOnARejectedSessionStillRecordsCollectionFailed(t *testing.T) {
	t.Parallel()
	r := newHeldPollRig(t, refusingDialer{})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if st, _ := r.sessions.held["ep-1"].sess.State(); st == collection.SessionRejected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the refusing session never read rejected")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := r.coll.runPoll(t.Context(), r.poll); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := r.collectionFailures(t)
	if len(got) != 1 {
		t.Fatalf("collection-failed = %q, want the rejected session's one failure", got)
	}
	if m := r.metrics(); m[probekeys.PollFailureCount] != 1 {
		t.Fatalf("poll-failure-count = %v, want 1: %v", m[probekeys.PollFailureCount], m)
	}
}
