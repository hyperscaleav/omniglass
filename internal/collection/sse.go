package collection

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server-sent events (#1038, slice 3 of #1113): the event-stream
// interpretation the WHATWG HTML standard defines, pure, so the reader that
// holds a stream only feeds it lines. A line is a field (event, data, id,
// retry), a comment, or blank, which dispatches the event the fields built;
// the last event ID persists across events and is what a reconnect resumes
// from.

// sseEvent is one dispatched event.
type sseEvent struct {
	data  string
	event string // "message" when the stream named none
	// id is the event's own id; hasID is whether it carried an id field
	// (an event without one dispatches under the last, and is never
	// deduplicated by it).
	id    string
	hasID bool
	// oversize marks an event whose data passed the cap: its data is
	// dropped, its id still counts.
	oversize bool
}

// sseParser builds events from a stream's lines.
type sseParser struct {
	data    strings.Builder
	event   string
	id      string
	hasID   bool
	maxData int // the data a single event may carry; zero is maxSSEData
	// lastEventID is the last event ID string a dispatch set, the id a
	// reconnect sends as Last-Event-ID.
	lastEventID string
	idBuffer    string
	// retry is the reconnection time the stream asked for, retrySet once
	// it has.
	retry    time.Duration
	retrySet bool
	// overflow marks the event being built as past the data cap.
	overflow bool
}

// The bounds on what a stream can make a node hold: a line, one event's
// data, and an id. A line past its cap is read to its end and kept to the
// cap; an event whose data passes its cap is dropped whole, its id still
// counted; an id past its cap is ignored. None ends the connection, so one
// oversize event never wedges a stream that would replay it.
const (
	maxSSELine = 1 << 20
	maxSSEData = 1 << 20
	maxSSEID   = 1 << 10
)

// feed feeds one line (truncated when it passed the line cap); ok is true
// when it dispatched an event.
func (p *sseParser) feed(l string, truncated bool) (sseEvent, bool) {
	if truncated {
		// Only a data line's length matters: the event it belongs to is
		// past the cap. Any other field this long is ignored.
		if field, _, _ := strings.Cut(l, ":"); field == "data" {
			p.overflow = true
		}
		return sseEvent{}, false
	}
	ev, ok, _ := p.line(l)
	return ev, ok
}

// line feeds one whole line, its terminator stripped; ok is true when it
// dispatched an event.
func (p *sseParser) line(l string) (ev sseEvent, ok bool, err error) {
	if l == "" {
		return p.dispatch()
	}
	if strings.HasPrefix(l, ":") {
		return sseEvent{}, false, nil // a comment (a keep-alive)
	}
	field, value, found := strings.Cut(l, ":")
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "event":
		p.event = value
	case "data":
		max := p.maxData
		if max <= 0 {
			max = maxSSEData
		}
		if p.overflow || p.data.Len()+len(value)+1 > max {
			p.overflow = true
			p.data.Reset()
			return sseEvent{}, false, fmt.Errorf("an event's data exceeds %d bytes", max)
		}
		p.data.WriteString(value)
		p.data.WriteByte('\n')
	case "id":
		if !strings.ContainsRune(value, 0) && len(value) <= maxSSEID {
			p.idBuffer, p.id, p.hasID = value, value, true
		}
	case "retry":
		if ms, err := strconv.ParseUint(value, 10, 31); err == nil && value != "" && strings.Trim(value, "0123456789") == "" {
			p.retry, p.retrySet = time.Duration(ms)*time.Millisecond, true
		}
	}
	return sseEvent{}, false, nil
}

// dispatch ends the event the fields built: the last event ID moves to the
// buffer whether or not the event carried data, and an event with no data
// dispatches nothing.
func (p *sseParser) dispatch() (sseEvent, bool, error) {
	p.lastEventID = p.idBuffer
	defer func() {
		p.data.Reset()
		p.event, p.id, p.hasID, p.overflow = "", "", false, false
	}()
	if p.overflow {
		ev := sseEvent{event: p.event, id: p.id, hasID: p.hasID, oversize: true}
		if ev.event == "" {
			ev.event = "message"
		}
		return ev, true, nil
	}
	if p.data.Len() == 0 {
		return sseEvent{}, false, nil
	}
	ev := sseEvent{data: strings.TrimSuffix(p.data.String(), "\n"), event: p.event, id: p.id, hasID: p.hasID}
	if ev.event == "" {
		ev.event = "message"
	}
	return ev, true, nil
}

// readSSELine reads one line terminated by CRLF, LF or CR, the terminator
// stripped; a line past max is read to its end and returned cut to max,
// truncated.
func readSSELine(br *bufio.Reader, max int) (line string, truncated bool, err error) {
	var b []byte
	for {
		c, err := br.ReadByte()
		if err != nil {
			return "", false, err
		}
		switch c {
		case '\n':
			return string(b), truncated, nil
		case '\r':
			if next, err := br.Peek(1); err == nil && next[0] == '\n' {
				_, _ = br.ReadByte()
			}
			return string(b), truncated, nil
		}
		if len(b) >= max {
			truncated = true
			continue
		}
		b = append(b, c)
	}
}

// StreamResume is what a held stream carries across its reconnects: the
// last event ID a reconnect resumes from, and the reconnection time the
// server asked for. It lives in the node's memory with the endpoint, never
// persisted. Safe for concurrent use.
type StreamResume struct {
	mu     sync.Mutex
	lastID string
	retry  time.Duration
}

// NewStreamResume is a resume from nothing.
func NewStreamResume() *StreamResume { return &StreamResume{} }

// LastID is the id a reconnect resumes from, empty before any.
func (r *StreamResume) LastID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastID
}

func (r *StreamResume) setLastID(id string) {
	r.mu.Lock()
	r.lastID = id
	r.mu.Unlock()
}

// Retry is the reconnection time the server last asked for, zero before it
// asked.
func (r *StreamResume) Retry() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.retry
}

func (r *StreamResume) setRetry(d time.Duration) {
	r.mu.Lock()
	r.retry = d
	r.mu.Unlock()
}
