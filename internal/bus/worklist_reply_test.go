package bus

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/nats-io/nats.go"
)

// worklistOf is a store whose worklist holds n tasks, or fails with err.
type worklistOf struct {
	fakeStore
	n   int
	err error
}

func (s worklistOf) NodeWorklist(context.Context, string) (storage.Worklist, error) {
	if s.err != nil {
		return storage.Worklist{}, s.err
	}
	wl := storage.Worklist{}
	for range s.n {
		wl.Tasks = append(wl.Tasks, storage.WorklistTask{ID: "task", Mode: "poll", EndpointName: "tcp"})
	}
	return wl, nil
}

// capture routes the default logger to a buffer for the test's length.
func capture(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(prior) })
	return &logged
}

// sent is what one answerWorklist call sent: its body and header.
type sent struct {
	calls int
	body  []byte
	hdr   nats.Header
}

func (r *sent) respond(data []byte, hdr nats.Header) error {
	r.calls++
	r.body, r.hdr = data, hdr
	return nil
}

// refused fails t unless r is one refusal: no body, and a reason naming want.
func (r *sent) refused(t *testing.T, want string) {
	t.Helper()
	if r.calls != 1 || len(r.body) != 0 {
		t.Fatalf("sent %d replies, the last %d bytes, want one refusal with no body", r.calls, len(r.body))
	}
	if _, err := collection.DecodeWorklistReply(r.hdr, r.body); !errors.Is(err, collection.ErrWorklistRefused) || !strings.Contains(err.Error(), want) {
		t.Fatalf("the node reads %v, want a refusal naming %q", err, want)
	}
}

// A reply over max payload cannot be sent; the server says so at ERROR, with
// the node, the size and the limit, and tells the node, which keeps its
// worklist.
func TestAnOversizedWorklistReplyIsLoggedAsAnError(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: worklistOf{n: 50}}
	var r sent
	s.answerWorklist(context.Background(), "edge-1", "og.worklist.edge-1", 1000, r.respond, time.Now())

	r.refused(t, "over max payload 1000")
	if got := s.WorklistCounts().Oversize; got != 1 {
		t.Errorf("oversize count %d, want 1", got)
	}
	line := logged.String()
	for _, want := range []string{"level=ERROR", "node=edge-1", "subject=og.worklist.edge-1", "max_payload=1000", "reply_bytes=", "keeps its last worklist"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %q: %s", want, line)
		}
	}
	if got := s.WorklistReplyBytes(); len(got) != 0 {
		t.Errorf("a reply that was not sent was recorded: %v", got)
	}
}

// The connection counts the header's wire form against max payload, so a
// body under the limit on its own can still be refused with ErrMaxPayload.
// That is an oversize reply, told to the node, not a send failure that
// leaves it waiting out its budget.
func TestAReplyTheConnectionRefusesAsOverMaxPayloadIsOversize(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: worklistOf{n: 2}}
	var r sent
	s.answerWorklist(context.Background(), "edge-1", "og.worklist.edge-1", 1<<20, func(data []byte, hdr nats.Header) error {
		if len(data) > 0 {
			return nats.ErrMaxPayload
		}
		return r.respond(data, hdr)
	}, time.Now())

	r.refused(t, "over max payload 1048576")
	if got := s.WorklistCounts(); got.Oversize != 1 || got.RespondFailed != 0 || got.Served != 0 {
		t.Errorf("counts %+v, want one oversize", got)
	}
	if line := logged.String(); !strings.Contains(line, "level=ERROR") || !strings.Contains(line, "with its header") {
		t.Errorf("no oversize error: %s", line)
	}
	if got := s.WorklistReplyBytes(); len(got) != 0 {
		t.Errorf("a reply that was not sent was recorded: %v", got)
	}
}

// A Respond that fails is warned about, naming the node, the size and the
// limit, rather than dropped.
func TestAWorklistReplyThatFailsToSendIsWarned(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: worklistOf{n: 2}}
	s.answerWorklist(context.Background(), "edge-1", "og.worklist.edge-1", 1<<20, func([]byte, nats.Header) error { return errors.New("connection closed") }, time.Now())

	line := logged.String()
	for _, want := range []string{"level=WARN", "node=edge-1", "reply_bytes=", "max_payload=1048576", "connection closed"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %q: %s", want, line)
		}
	}
	if strings.Count(line, "level=") != 1 {
		t.Errorf("want one log line: %s", line)
	}
	if got := s.WorklistCounts().RespondFailed; got != 1 {
		t.Errorf("respond-failed count %d, want 1", got)
	}
}

// A failed read is warned about once, naming the node and the error, and the
// node is told, so it keeps its worklist rather than reading an empty one.
func TestAFailedWorklistReadIsWarned(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: worklistOf{err: errors.New("db down")}}
	var r sent
	s.answerWorklist(context.Background(), "edge-1", "og.worklist.edge-1", 1<<20, r.respond, time.Now())

	r.refused(t, "worklist read failed")
	if got := s.WorklistCounts().ReadFailed; got != 1 {
		t.Errorf("read-failed count %d, want 1", got)
	}

	line := logged.String()
	for _, want := range []string{"level=WARN", "node=edge-1", "db down"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %q: %s", want, line)
		}
	}
	if strings.Count(line, "level=") != 1 {
		t.Errorf("want one log line: %s", line)
	}
}

// Each successful reply's size is recorded under its node, and logged at DEBUG
// as the bytes the node was sent.
func TestASuccessfulWorklistReplyRecordsItsSize(t *testing.T) {
	logged := capture(t, slog.LevelDebug)
	s := &Server{store: worklistOf{n: 3}}
	var got []byte
	s.answerWorklist(context.Background(), "edge-1", "og.worklist.edge-1", 1<<20, func(b []byte, _ nats.Header) error { got = b; return nil }, time.Now())
	s.store = worklistOf{n: 6}
	var other []byte
	s.answerWorklist(context.Background(), "edge-2", "og.worklist.edge-2", 1<<20, func(b []byte, _ nats.Header) error { other = b; return nil }, time.Now())

	sizes := s.WorklistReplyBytes()
	if sizes["edge-1"] != len(got) || sizes["edge-2"] != len(other) || len(got) == 0 || len(other) <= len(got) {
		t.Fatalf("recorded %v, sent %d and %d bytes", sizes, len(got), len(other))
	}
	if !strings.Contains(logged.String(), "level=DEBUG") || !strings.Contains(logged.String(), "reply_bytes=") {
		t.Errorf("no DEBUG size line: %s", logged.String())
	}
	if strings.Contains(logged.String(), "level=WARN") {
		t.Errorf("a small reply warned: %s", logged.String())
	}
}

// A reply past 75% of max payload is warned about once per node per hour: the
// second inside the hour is silent, one after it warns again, and another node
// has its own hour.
func TestANearLimitWorklistReplyIsWarnedOncePerNodePerHour(t *testing.T) {
	s := &Server{store: worklistOf{n: 10}}
	ok := func([]byte, nats.Header) error { return nil }
	bg := context.Background()
	s.answerWorklist(bg, "probe", "s", 1<<20, ok, time.Now())
	limit := int64(s.WorklistReplyBytes()["probe"]) * 100 / 80 // the reply is 80% of the limit
	logged := capture(t, slog.LevelWarn)

	t0 := time.Now()
	s.answerWorklist(bg, "edge-1", "s", limit, ok, t0)
	s.answerWorklist(bg, "edge-1", "s", limit, ok, t0.Add(59*time.Minute))
	if n := strings.Count(logged.String(), "near max payload"); n != 1 {
		t.Fatalf("%d warnings inside the hour, want 1: %s", n, logged.String())
	}
	s.answerWorklist(bg, "edge-2", "s", limit, ok, t0.Add(time.Minute))
	if n := strings.Count(logged.String(), "near max payload"); n != 2 {
		t.Fatalf("%d warnings after another node's, want 2", n)
	}
	s.answerWorklist(bg, "edge-1", "s", limit, ok, t0.Add(time.Hour))
	if n := strings.Count(logged.String(), "near max payload"); n != 3 {
		t.Fatalf("%d warnings after the hour, want 3", n)
	}
	for _, want := range []string{"node=edge-1", "reply_bytes=", "max_payload="} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("warning lacks %q: %s", want, logged.String())
		}
	}
}

// A reply at or under 75% of the limit never warns.
func TestAWorklistReplyUnderThreeQuartersOfTheLimitDoesNotWarn(t *testing.T) {
	var w worklistSizes
	now := time.Now()
	if w.observe("edge-1", 750, 1000, now) {
		t.Error("warned at exactly 75%")
	}
	if !w.observe("edge-1", 751, 1000, now) {
		t.Error("did not warn past 75%")
	}
}
