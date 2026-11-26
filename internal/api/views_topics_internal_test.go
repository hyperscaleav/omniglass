package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/tail"
)

func TestParseTopics(t *testing.T) {
	t.Parallel()
	got, err := parseTopics("system:hq-huddle, component:room-1.codec")
	if err != nil || len(got) != 2 || got[0] != [2]string{"system", "hq-huddle"} || got[1] != [2]string{"component", "room-1.codec"} {
		t.Fatalf("parseTopics = %v, %v", got, err)
	}
	if got, err := parseTopics(" "); err != nil || got != nil {
		t.Fatalf("no topics = %v, %v", got, err)
	}
	for _, bad := range []string{"system", "system:", "location:hq", "alarm:1", strings.Repeat("system:a,", viewMaxTopics) + "system:b"} {
		if _, err := parseTopics(bad); err == nil {
			t.Errorf("parseTopics(%q) admitted it", bad)
		}
	}
}

// A topic's notices come from the change lane alone, after the stream's
// position, once per spelling the client named the entity by.
func TestTopicMatchChanges(t *testing.T) {
	t.Parallel()
	m := topicMatch{after: 1, byOwner: map[string][]string{"system:s1": {"system:hq", "system:s1"}}}
	commits := []tail.Commit{
		{Rows: []tail.Row{{Lane: tail.LaneChange, OwnerKind: "system", OwnerID: "s1", Key: tail.ChangeUpdate}}},
		{Rows: []tail.Row{
			{Lane: tail.LaneChange, OwnerKind: "system", OwnerID: "s2", Key: tail.ChangeUpdate},
			{Lane: tail.LaneMetric, OwnerKind: "system", OwnerID: "s1", Key: "x"},
			{Lane: tail.LaneChange, OwnerKind: "component", OwnerID: "s1", Key: tail.ChangeUpdate},
			{Lane: tail.LaneChange, OwnerKind: "system", OwnerID: "s1", Key: tail.ChangeUpdate},
		}},
	}
	got := m.changes(commits, []uint64{1, 2})
	if len(got) != 2 || got[0].Topic != "system:hq" || got[1].Topic != "system:s1" || got[0].V != 2 || got[0].ID != "s1" || got[0].Op != "update" {
		t.Fatalf("changes = %+v, want s1's second commit once per spelling", got)
	}
}

// The coalescer sends a topic's first notice at once and holds the rest of
// a burst to one per window, the newest; force hands out everything held.
func TestChangeCoalescerHoldsATopicToItsWindow(t *testing.T) {
	t.Parallel()
	c := newChangeCoalescer(250 * time.Millisecond)
	t0 := time.Unix(1000, 0)
	c.add(viewChangedEvent{Topic: "a", V: 1})
	if got := c.due(t0, false); len(got) != 1 || got[0].V != 1 || c.holding() {
		t.Fatalf("a first notice: %+v", got)
	}
	for v := uint64(2); v <= 20; v++ {
		c.add(viewChangedEvent{Topic: "a", V: v})
	}
	c.add(viewChangedEvent{Topic: "b", V: 21})
	got := c.due(t0.Add(100*time.Millisecond), false)
	if len(got) != 1 || got[0].Topic != "b" || !c.holding() {
		t.Fatalf("inside a's window: %+v, want b alone and a held", got)
	}
	if at := c.next(); !at.Equal(t0.Add(250 * time.Millisecond)) {
		t.Fatalf("next = %v, want a's window end", at)
	}
	got = c.due(t0.Add(250*time.Millisecond), false)
	if len(got) != 1 || got[0].Topic != "a" || got[0].V != 20 || c.holding() {
		t.Fatalf("at a's window end: %+v, want a's newest notice", got)
	}
	c.add(viewChangedEvent{Topic: "a", V: 30})
	if got := c.due(t0.Add(260*time.Millisecond), true); len(got) != 1 || got[0].V != 30 {
		t.Fatalf("forced: %+v", got)
	}
	if !c.next().IsZero() {
		t.Fatal("next with nothing held is not zero")
	}
}

func TestMergeTailRows(t *testing.T) {
	t.Parallel()
	held := []viewTailBindingBody{{Binding: 1, Metric: []viewTailMetricBody{{Value: 1}}}}
	got := mergeTailRows(held, []viewTailBindingBody{{Binding: 0, Metric: []viewTailMetricBody{{Value: 9}}}, {Binding: 1, Metric: []viewTailMetricBody{{Value: 2}}}})
	if len(got) != 2 || got[0].Binding != 0 || len(got[1].Metric) != 2 || got[1].Metric[1].Value != 2 {
		t.Fatalf("merged = %+v", got)
	}
}

// deadlineRecorder is a response writer that records each write deadline
// the stream sets, as http.ResponseController reaches it.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	mu        sync.Mutex
	deadlines []time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadlines = append(d.deadlines, t)
	return nil
}

// Every send sets a fresh write deadline before it writes, so a client that
// stops reading ends the stream instead of parking its goroutine.
func TestSSEWriterSetsADeadlinePerSend(t *testing.T) {
	t.Parallel()
	rec := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	w := newSSEWriter(rec)
	before := time.Now()
	if err := w.retry(sseRetry); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := w.send("b.1", "mark", viewMarkEvent{Cursor: "b.1"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.deadlines) != 3 {
		t.Fatalf("%d deadlines for 3 writes", len(rec.deadlines))
	}
	for _, d := range rec.deadlines {
		if d.Before(before.Add(sseWriteDeadline)) || d.After(time.Now().Add(sseWriteDeadline)) {
			t.Fatalf("deadline %v is not one write deadline out", d)
		}
	}
	if body := rec.Body.String(); !strings.HasPrefix(body, "retry: 3000\n\n") {
		t.Fatalf("the stream does not open with its retry: %q", body)
	}
}

// syncBuffer is a stream body two goroutines share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

type frame struct{ id, event, data string }

func (s *syncBuffer) frames() []frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []frame
	var f frame
	sc := bufio.NewScanner(bytes.NewReader(s.b.Bytes()))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if f.event != "" {
				out = append(out, f)
			}
			f = frame{}
		case strings.HasPrefix(line, "id: "):
			f.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			f.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			f.data = strings.TrimPrefix(line, "data: ")
		}
	}
	return out
}

func (s *syncBuffer) changed() []viewChangedEvent {
	var out []viewChangedEvent
	for _, f := range s.frames() {
		if f.event == "changed" {
			var ev viewChangedEvent
			_ = json.Unmarshal([]byte(f.data), &ev)
			out = append(out, ev)
		}
	}
	return out
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func change(id string) []tail.Row {
	return []tail.Row{{Lane: tail.LaneChange, OwnerKind: "system", OwnerID: id, Key: tail.ChangeUpdate}}
}

// runTopicStream follows one system topic on hub until the returned stop.
func runTopicStream(t *testing.T, hub *tail.Hub, clock viewWatchClock) (*syncBuffer, func()) {
	t.Helper()
	after, _ := hub.Seq(hub.Cursor())
	buf := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if clock.life == 0 {
		clock.life = time.Minute
	}
	if clock.beat == 0 {
		clock.beat = time.Minute
	}
	if clock.stands == nil {
		clock.stands = func() bool { return true }
	}
	go func() {
		defer close(done)
		streamViews(ctx, hub, newSSEWriter(buf), viewStream{topics: &topicMatch{after: after, byOwner: map[string][]string{"system:s1": {"system:s1"}}}}, clock)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return buf, func() { <-done }
}

// A burst of changes to one topic reaches the stream coalesced: its first
// notice at once, the rest of the burst as one more at the window's end,
// the newest; and every event's id is a position past only notices sent.
func TestStreamViewsCoalescesABurstPerTopic(t *testing.T) {
	t.Parallel()
	hub := tail.New(1024, 0)
	every := 200 * time.Millisecond
	buf, _ := runTopicStream(t, hub, viewWatchClock{every: every})
	waitUntil(t, "the opening mark", func() bool { return len(buf.frames()) > 0 })
	hub.Publish(change("s1"))
	waitUntil(t, "the leading notice", func() bool { return len(buf.changed()) == 1 })
	start := time.Now()
	for range 50 {
		hub.Publish(change("s1"))
		hub.Publish(change("other"))
	}
	waitUntil(t, "the burst's trailing notice", func() bool { return len(buf.changed()) >= 2 })
	time.Sleep(2 * every)
	got := buf.changed()
	if len(got) != 2 {
		t.Fatalf("a 50-change burst inside one window after a notice reached the stream as %d notices in all, want 2 (the leading one, then the burst as one)", len(got))
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the trailing notice took %s, want within a window", elapsed)
	}
	newest, _ := hub.Seq(hub.Cursor())
	if got[1].V+1 != newest {
		t.Fatalf("the trailing notice stands for v=%d, want the burst's newest s1 change (%d)", got[1].V, newest-1)
	}
	frames := buf.frames()
	if last := frames[len(frames)-1]; last.event != "changed" || last.id != hub.Cursor() {
		t.Fatalf("the last frame %+v does not carry the stream's position %s", last, hub.Cursor())
	}
}

// On server shutdown a stream sends what it holds, then a final mark with
// its position, and ends.
func TestStreamViewsEndsWithAFinalMarkOnShutdown(t *testing.T) {
	t.Parallel()
	hub := tail.New(64, 0)
	shutdown := NewStreamShutdown()
	buf, wait := runTopicStream(t, hub, viewWatchClock{every: time.Hour, shutdown: shutdown.Done()})
	waitUntil(t, "the opening mark", func() bool { return len(buf.frames()) > 0 })
	hub.Publish(change("s1"))
	waitUntil(t, "the first notice", func() bool { return len(buf.changed()) == 1 })
	hub.Publish(change("s1"))
	time.Sleep(50 * time.Millisecond)
	shutdown.Close()
	shutdown.Close()
	wait()
	frames := buf.frames()
	if n := len(buf.changed()); n != 2 {
		t.Fatalf("the held notice did not go out before the end: %d notices", n)
	}
	if last := frames[len(frames)-1]; last.event != "mark" || last.id != hub.Cursor() {
		t.Fatalf("the stream ended on %+v, want a final mark at %s", last, hub.Cursor())
	}
}

// A topics position the ring no longer holds is a reset naming the topics.
func TestStreamViewsResetsTopicsPastTheRing(t *testing.T) {
	t.Parallel()
	hub := tail.New(2, 0)
	for range 5 {
		hub.Publish(change("x"))
	}
	buf := &syncBuffer{}
	streamViews(context.Background(), hub, newSSEWriter(buf), viewStream{topics: &topicMatch{after: 1, byOwner: map[string][]string{}}},
		viewWatchClock{life: time.Minute, beat: time.Minute, stands: func() bool { return true }})
	frames := buf.frames()
	if len(frames) != 1 || frames[0].event != "reset" || !strings.Contains(frames[0].data, `"topics":true`) {
		t.Fatalf("frames = %+v, want one reset naming the topics", frames)
	}
}

var _ http.ResponseWriter = (*deadlineRecorder)(nil)
