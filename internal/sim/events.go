package sim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// The server-sent events face (#1038): a simulated component's web server
// streams every change to the state paths its profile declares, as a
// component's web page updates live, for the collector's held stream to
// meet on a real wire. Each change is one numbered event (`event: change`,
// its data the path and the new value as JSON); the last maxEventReplay are
// kept, and a reconnect's Last-Event-ID replays from that event, the event
// itself included (as some servers resend it), so a client's dedup is
// exercised on every resume. The stream sits behind the face's auth, and
// the console can drop every stream, as a restart would.

// maxEventReplay is how many events a face keeps for a reconnect's replay.
const maxEventReplay = 256

// eventKeepAlive is how often an idle stream sends a comment line.
const eventKeepAlive = 15 * time.Second

// streamEvent is one numbered change.
type streamEvent struct {
	id   uint64
	name string
	data string
}

// eventHub is a face's events: the ring a reconnect replays, and the
// streams connected now.
type eventHub struct {
	cfg  profile.Events
	mu   sync.Mutex
	seq  uint64
	ring []streamEvent
	subs map[*eventSub]struct{}
	// opened counts the streams the face opened, and attempts the requests
	// for one, refused or not.
	opened   int
	attempts atomic.Int64
}

// eventSub is one connected stream: the events it has yet to write, and its
// end (the console's drop, or falling behind).
type eventSub struct {
	ch   chan streamEvent
	stop chan struct{}
	once sync.Once
}

func (s *eventSub) end() { s.once.Do(func() { close(s.stop) }) }

func newEventHub(cfg profile.Events) *eventHub {
	return &eventHub{cfg: cfg, subs: map[*eventSub]struct{}{}}
}

// watches reports a state path the face streams: one of the declared paths
// or beneath one; every path when none is declared.
func (h *eventHub) watches(path string) bool {
	if len(h.cfg.Paths) == 0 {
		return true
	}
	for _, p := range h.cfg.Paths {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// fanout numbers a batch's watched changes, in path order, keeps them for a
// replay, and hands them to every connected stream. Never blocks: a stream
// that falls behind is ended, and its client resumes from its last event.
func (h *eventHub) fanout(diffs map[string]any) {
	paths := make([]string, 0, len(diffs))
	for p := range diffs {
		if h.watches(p) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	sort.Strings(paths)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range paths {
		data, err := json.Marshal(map[string]any{"path": p, "value": diffs[p]})
		if err != nil {
			continue
		}
		h.emitLocked("change", string(data))
	}
}

// publish streams one named event a route reports (#1145), numbered with
// the changes and kept for a replay as they are.
func (h *eventHub) publish(name, data string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.emitLocked(name, data)
}

// emitLocked numbers an event, keeps it for a replay, and hands it to every
// connected stream; a stream that falls behind is ended. Caller holds mu.
func (h *eventHub) emitLocked(name, data string) {
	h.seq++
	ev := streamEvent{id: h.seq, name: name, data: data}
	h.ring = append(h.ring, ev)
	if len(h.ring) > maxEventReplay {
		h.ring = h.ring[len(h.ring)-maxEventReplay:]
	}
	for s := range h.subs {
		select {
		case s.ch <- ev:
		default:
			s.end()
			delete(h.subs, s)
		}
	}
}

// replayFrom is what a reconnect from lastID replays: the kept events from
// that id on (all the ring holds when it reaches back further). An id that
// is not one of this face's replays nothing.
func (h *eventHub) replayFrom(lastID string) []streamEvent {
	n, err := strconv.ParseUint(lastID, 10, 64)
	if err != nil || n == 0 {
		return nil
	}
	var out []streamEvent
	for _, ev := range h.ring {
		if ev.id >= n {
			out = append(out, ev)
		}
	}
	return out
}

// subscribe registers a stream and returns its replay, under one lock, so
// no change falls between the two.
func (h *eventHub) subscribe(lastID string) (*eventSub, []streamEvent) {
	s := &eventSub{ch: make(chan streamEvent, 64), stop: make(chan struct{})}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[s] = struct{}{}
	h.opened++
	return s, h.replayFrom(lastID)
}

func (h *eventHub) unsubscribe(s *eventSub) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
	s.end()
}

// drop ends every connected stream and reports how many.
func (h *eventHub) drop() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := len(h.subs)
	for s := range h.subs {
		s.end()
		delete(h.subs, s)
	}
	return n
}

// SetEvents gives the device's web server an events face. Call before the
// http faces start.
func (d *Device) SetEvents(cfg profile.Events) {
	if cfg.Path == "" {
		d.events = nil
		return
	}
	d.events = newEventHub(cfg)
}

// EventStreams counts the event streams the device opened.
func (d *Device) EventStreams() int {
	if d.events == nil {
		return 0
	}
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	return d.events.opened
}

// EventStreamAttempts counts the requests for the device's event stream,
// the ones its auth gate refused included.
func (d *Device) EventStreamAttempts() int64 {
	if d.events == nil {
		return 0
	}
	return d.events.attempts.Load()
}

// DropEventStreams ends every connected event stream, as a restart would,
// and reports how many.
func (d *Device) DropEventStreams() int {
	if d.events == nil {
		return 0
	}
	return d.events.drop()
}

// isEventStream reports a request for the face's stream.
func (d *Device) isEventStream(r *http.Request) bool {
	return d.events != nil && r.Method == http.MethodGet && r.URL.Path == d.events.cfg.Path
}

// serveEvents holds one stream open: the retry it asks for, the replay a
// Last-Event-ID asks for, then every change until the client goes, the
// console drops it, or it falls behind.
func (d *Device) serveEvents(w http.ResponseWriter, r *http.Request) {
	h := d.events
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sub, replay := h.subscribe(r.Header.Get("Last-Event-ID"))
	defer h.unsubscribe(sub)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if h.cfg.RetryMS > 0 {
		fmt.Fprintf(w, "retry: %d\n\n", h.cfg.RetryMS)
	}
	write := func(ev streamEvent) {
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.id, ev.name, ev.data)
	}
	for _, ev := range replay {
		write(ev)
	}
	fl.Flush()
	tick := time.NewTicker(eventKeepAlive)
	defer tick.Stop()
	for {
		select {
		case ev := <-sub.ch:
			write(ev)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		case <-sub.stop:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// operateEventsDrop answers the console's drop of every stream.
func (d *Device) operateEventsDrop() OperationResult {
	n := d.DropEventStreams()
	d.publishOperation("http", http.MethodPost, d.events.cfg.DropPath, http.StatusOK)
	return OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"dropped":%d}`, n) + "\n"}
}
