package collection

import (
	"bufio"
	"strconv"
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
		line, err := readSSELine(br, 1<<10)
		if err != nil {
			return out, p
		}
		ev, ok, err := p.line(line)
		if err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if ok {
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
	p := &sseParser{maxData: 16}
	if _, _, err := p.line("data: " + strings.Repeat("x", 32)); err == nil {
		t.Fatal("an event past the data cap was kept")
	}
	br := bufio.NewReader(strings.NewReader(strings.Repeat("y", 64) + "\n"))
	if _, err := readSSELine(br, 16); err == nil {
		t.Fatal("a line past the line cap was read")
	}
}

func TestStreamResumeLandsAnIdOnce(t *testing.T) {
	r := NewStreamResume()
	if !r.land("1") || r.land("1") || !r.land("2") {
		t.Fatal("an id landed twice, or a new one was refused")
	}
	for i := range maxResumeIDs + 10 {
		r.land("id-" + strconv.Itoa(i))
	}
	if len(r.seen) > maxResumeIDs || len(r.order) > maxResumeIDs {
		t.Fatalf("the resume holds %d ids, want at most %d", len(r.seen), maxResumeIDs)
	}
	r.setRetry(5 * time.Millisecond)
	if r.Retry() != 5*time.Millisecond {
		t.Fatalf("retry = %v", r.Retry())
	}
}
