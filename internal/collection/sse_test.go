package collection

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

// The server-sent events interpretation (#1038), the WHATWG event-stream
// rules: fields, comments, the data buffer, the last event ID buffer, retry,
// and every line terminator.

func parseAll(t *testing.T, stream string) ([]sseEvent, *sseParser) {
	t.Helper()
	p := &sseParser{}
	br := bufio.NewReader(strings.NewReader(stream))
	var out []sseEvent
	for {
		line, truncated, err := readSSELine(br, 1<<10)
		if err != nil {
			return out, p
		}
		if ev, ok := p.feed(line, truncated); ok {
			out = append(out, ev)
		}
	}
}

func TestSSEParserReadsTheSpecExamples(t *testing.T) {
	evs, p := parseAll(t, ": a comment\n\ndata: first event\nid: 1\n\ndata:second event\nid\n\ndata:  third event\n\n")
	if len(evs) != 3 {
		t.Fatalf("events = %+v, want three", evs)
	}
	if evs[0].data != "first event" || evs[0].id != "1" || !evs[0].hasID || evs[0].event != "message" {
		t.Fatalf("first = %+v", evs[0])
	}
	// "id" with no value resets the last event ID; the event carries its own
	// (empty) id.
	if evs[1].data != "second event" || evs[1].id != "" || !evs[1].hasID {
		t.Fatalf("second = %+v", evs[1])
	}
	// One leading space is the separator; the next is data.
	if evs[2].data != " third event" || evs[2].hasID {
		t.Fatalf("third = %+v", evs[2])
	}
	if p.lastEventID != "" {
		t.Fatalf("last event id = %q, want reset", p.lastEventID)
	}

	evs, _ = parseAll(t, "data\n\ndata\ndata\n\ndata:\n")
	if len(evs) != 2 || evs[0].data != "" || evs[1].data != "\n" {
		t.Fatalf("empty data events = %+v", evs)
	}

	// Data lines join with a newline; the event names it; every terminator.
	evs, p = parseAll(t, "event: change\r\ndata: {\"a\":\r\ndata: 1}\r\nid: 7\r\nretry: 2500\r\n\r\nevent: x\rdata: y\rretry: soon\r\r")
	if len(evs) != 2 || evs[0].event != "change" || evs[0].data != "{\"a\":\n1}" || evs[0].id != "7" {
		t.Fatalf("events = %+v", evs)
	}
	if evs[1].event != "x" || evs[1].data != "y" || evs[1].hasID {
		t.Fatalf("second = %+v", evs[1])
	}
	if !p.retrySet || p.retry != 2500*time.Millisecond {
		t.Fatalf("retry = %v (%v), want 2.5s and a non-digit retry ignored", p.retry, p.retrySet)
	}
	// An id persists: an event without one dispatches under the last.
	if p.lastEventID != "7" {
		t.Fatalf("last event id = %q, want 7", p.lastEventID)
	}

	// An id carrying NUL is ignored; an event with no data dispatches
	// nothing but still moves the last event ID.
	evs, p = parseAll(t, "id: a\x00b\ndata: z\n\nid: 9\n\n")
	if len(evs) != 1 || evs[0].hasID {
		t.Fatalf("events = %+v", evs)
	}
	if p.lastEventID != "9" {
		t.Fatalf("last event id = %q, want 9 from the event with no data", p.lastEventID)
	}
}

func TestSSEParserBoundsWhatAComponentCanMakeItHold(t *testing.T) {
	// An event past the data cap is dropped whole, its id still counted,
	// and the stream reads on: one oversize event never wedges it.
	p := &sseParser{maxData: 16}
	evs := []sseEvent{}
	for _, l := range []string{"id: 3", "data: " + strings.Repeat("x", 32), "data: more", "", "id: 4", "data: small", ""} {
		if ev, ok := p.feed(l, false); ok {
			evs = append(evs, ev)
		}
	}
	if len(evs) != 2 || !evs[0].oversize || evs[0].id != "3" || evs[0].data != "" || evs[1].oversize || evs[1].data != "small" {
		t.Fatalf("events = %+v, want the oversize one dropped and the next whole", evs)
	}
	if p.lastEventID != "4" {
		t.Fatalf("last event id = %q, want 4", p.lastEventID)
	}
	// A line past the line cap is read to its end and cut; the next line
	// reads whole.
	br := bufio.NewReader(strings.NewReader("data: " + strings.Repeat("y", 64) + "\nid: 5\n"))
	line, truncated, err := readSSELine(br, 16)
	if err != nil || !truncated || len(line) != 16 {
		t.Fatalf("an oversize line = %q, %v, %v", line, truncated, err)
	}
	if line, truncated, err := readSSELine(br, 16); err != nil || truncated || line != "id: 5" {
		t.Fatalf("the next line = %q, %v, %v", line, truncated, err)
	}
	// An id past its cap is ignored, never sent back as Last-Event-ID.
	q := &sseParser{}
	q.feed("id: "+strings.Repeat("i", maxSSEID+1), false)
	q.feed("data: x", false)
	if ev, ok := q.feed("", false); !ok || ev.hasID || q.lastEventID != "" {
		t.Fatalf("an oversize id = %+v, last %q", ev, q.lastEventID)
	}
}

func TestStreamResumeCarriesTheLastIDAndRetry(t *testing.T) {
	r := NewStreamResume()
	if r.LastID() != "" || r.Retry() != 0 {
		t.Fatal("a new resume resumes from something")
	}
	r.setLastID("7")
	r.setRetry(5 * time.Millisecond)
	if r.LastID() != "7" || r.Retry() != 5*time.Millisecond {
		t.Fatalf("resume = %q, %v", r.LastID(), r.Retry())
	}
}
