package collection

import (
	"bufio"
	"errors"
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
}

// The bounds on what a stream can make a node hold: a line, and one event's
// data. Past either, the connection ends with a reason and the session
// reconnects.
const (
	maxSSELine = 1 << 20
	maxSSEData = 1 << 20
)

// errSSELine is a line past maxSSELine.
var errSSELine = errors.New("a stream line exceeds the line cap")

// line feeds one line, its terminator stripped; ok is true when it
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
		if p.data.Len()+len(value)+1 > max {
			return sseEvent{}, false, fmt.Errorf("an event's data exceeds %d bytes", max)
		}
		p.data.WriteString(value)
		p.data.WriteByte('\n')
	case "id":
		if !strings.ContainsRune(value, 0) {
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
		p.event, p.id, p.hasID = "", "", false
	}()
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
// stripped; a line past max is an error.
func readSSELine(br *bufio.Reader, max int) (string, error) {
	var b []byte
	for {
		c, err := br.ReadByte()
		if err != nil {
			return "", err
		}
		switch c {
		case '\n':
			return string(b), nil
		case '\r':
			if next, err := br.Peek(1); err == nil && next[0] == '\n' {
				_, _ = br.ReadByte()
			}
			return string(b), nil
		}
		if len(b) >= max {
			return "", errSSELine
		}
		b = append(b, c)
	}
}

// StreamResume is what a held stream carries across its reconnects: the
// last event ID a reconnect resumes from, the ids it already landed (so a
// server's replay lands nothing twice), and the reconnection time the
// server asked for. It lives in the node's memory with the endpoint, never
// persisted. Safe for concurrent use.
type StreamResume struct {
	mu     sync.Mutex
	lastID string
	seen   map[string]struct{}
	order  []string
	retry  time.Duration
}

// maxResumeIDs bounds the ids a resume remembers: a replay reaches back a
// few events, never thousands.
const maxResumeIDs = 1024

// NewStreamResume is a resume with nothing landed.
func NewStreamResume() *StreamResume { return &StreamResume{seen: map[string]struct{}{}} }

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

// land records an event's id as landed; false when it already was.
func (r *StreamResume) land(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[id]; ok {
		return false
	}
	r.seen[id] = struct{}{}
	r.order = append(r.order, id)
	if len(r.order) > maxResumeIDs {
		delete(r.seen, r.order[0])
		r.order = r.order[1:]
	}
	return true
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
