package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// goHandlers is what a Go device family answers in Go (#2829), registered on
// its device beside whatever its interfaces declare. The protocol servers call
// these where a YAML profile's routes and commands would render a template.
type goHandlers struct {
	model   spec.Model
	animate spec.Animator
	// stop ends what animate started.
	stop  func()
	ticks []spec.Tick
	// tickStop ends the tick goroutines and tickDone waits for them.
	tickStop chan struct{}
	tickDone sync.WaitGroup
	src      *behave.DeviceSource
}

// addHandlers registers a family's routes and commands on the device and
// holds its animator for StartBehaviors. model is the row of the product the
// device simulates, the zero Model for a device of the family itself.
func (d *Device) addHandlers(h spec.Handlers, model spec.Model) error {
	d.fam = &goHandlers{model: model, animate: h.Animate, ticks: h.Ticks, src: behave.NewSource(d.DeviceID)}
	for _, r := range h.HTTP {
		method, path, err := parseMatch(r.Match)
		if err != nil {
			return err
		}
		d.routes = append(d.routes, routeEntry{Method: method, Path: path, Go: r.Handle})
		d.registerSets(r.Sets)
	}
	for _, c := range h.Commands {
		d.commands = append(d.commands, commandEntry{Match: c.Match, Go: c.Handle})
	}
	return nil
}

// startAnimator runs the family's animator, once.
func (d *Device) startAnimator() {
	d.behaviorMu.Lock()
	if d.fam == nil || d.fam.stop != nil {
		d.behaviorMu.Unlock()
		return
	}
	stop := func() {}
	if d.fam.animate != nil {
		if s := d.fam.animate(goDevice{d}); s != nil {
			stop = s
		}
	}
	d.fam.stop = stop
	var first []spec.Tick
	d.fam.tickStop = make(chan struct{})
	for _, t := range d.fam.ticks {
		if t.OnStart {
			first = append(first, t)
		}
		d.fam.tickDone.Add(1)
		go d.runTick(t, d.fam.tickStop, &d.fam.tickDone)
	}
	d.behaviorMu.Unlock()
	for _, t := range first {
		d.applyTick(t)
	}
}

// runTick is a tick's goroutine: a deterministic phase offset, then its
// interval, as a YAML behavior's.
func (d *Device) runTick(t spec.Tick, stop <-chan struct{}, done *sync.WaitGroup) {
	defer done.Done()
	timer := time.NewTimer(time.Duration(behaviorPhaseOffset(d.DeviceID, t.Name, int64(t.Interval))))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stop:
		return
	}
	tk := time.NewTicker(t.Interval)
	defer tk.Stop()
	for {
		d.applyTick(t)
		select {
		case <-tk.C:
		case <-stop:
			return
		}
	}
}

// applyTick runs a tick and writes what it returns: only the paths whose value
// changed, never one an operator override holds.
func (d *Device) applyTick(t spec.Tick) {
	now := d.clock()
	span, known := behave.BuildingSpan(d.world(), d.calendarSrc(), d.room)
	c := spec.TickContext{Room: d.roomState(d.room), Now: now, Rand: d.fam.src, Hours: behave.ReadBuildingHours(span, known, now), Spans: d.roomSpans, Bookings: d.bookingsRead}
	changes := t.Run(goDevice{d}, c)
	if len(changes) == 0 {
		return
	}
	held := d.OverridePaths()
	d.mu.Lock()
	defer d.mu.Unlock()
	diffs := map[string]any{}
	for _, ch := range changes {
		if _, over := held[ch.Path]; over {
			continue
		}
		if reflect.DeepEqual(lookupStatePath(d.state, ch.Path), ch.Value) {
			continue
		}
		if err := SetPath(d.state, ch.Path, ch.Value); err != nil {
			slog.Warn("tick write failed", "device_id", d.DeviceID, "tick", t.Name, "path", ch.Path, "error", err)
			continue
		}
		diffs[ch.Path] = ch.Value
	}
	if len(diffs) == 0 {
		return
	}
	d.Version++
	if d.DiffPublisher != nil {
		d.DiffPublisher(StateChange{DeviceID: d.DeviceID, Version: d.Version, Diffs: diffs, Cause: "behavior:" + t.Name})
	}
	d.fanoutToSessions(diffs)
}

// roomSpans is the meetings of the device's room overlapping [from, to): none
// for a device with no calendar.
func (d *Device) roomSpans(from, to time.Time) []behave.Span {
	c := d.calendarSrc()
	if c == nil {
		return nil
	}
	return behave.SpansBetween(c, d.room, from, to)
}

// stopAnimator stops what the animator started.
func (d *Device) stopAnimator() {
	d.behaviorMu.Lock()
	var stop func()
	var ticks chan struct{}
	if d.fam != nil {
		stop, d.fam.stop = d.fam.stop, nil
		ticks, d.fam.tickStop = d.fam.tickStop, nil
	}
	d.behaviorMu.Unlock()
	if ticks != nil {
		close(ticks)
		d.fam.tickDone.Wait()
	}
	if stop != nil {
		stop()
	}
}

// goDevice is the device as a family's handler sees it (spec.Device).
type goDevice struct{ d *Device }

func (g goDevice) DeviceID() string { return g.d.DeviceID }
func (g goDevice) Hostname() string { return g.d.Hostname }
func (g goDevice) IP() string       { return g.d.LoopbackIP.String() }
func (g goDevice) Model() spec.Model {
	if g.d.fam == nil {
		return spec.Model{}
	}
	return g.d.fam.model
}
func (g goDevice) Read(path string) any { return g.d.ReadPath(path) }
func (g goDevice) Default(path string) any {
	g.d.mu.RLock()
	defer g.d.mu.RUnlock()
	return lookupStatePath(g.d.defaults, path)
}
func (g goDevice) State() map[string]any { return g.d.State() }
func (g goDevice) Now() time.Time        { return g.d.clock() }
func (g goDevice) Write(path string, v any) error {
	return g.WriteMany(surface.Change{Path: path, Value: v})
}

// WriteMany applies the changes as one: all of them or, when one path cannot
// be set, none.
func (g goDevice) WriteMany(changes ...surface.Change) error {
	d := g.d
	d.mu.Lock()
	defer d.mu.Unlock()
	snapshot := deepCopy(d.state)
	diffs := make(map[string]any, len(changes))
	for _, ch := range changes {
		if err := SetPath(d.state, ch.Path, ch.Value); err != nil {
			// Restored in place: the state map's identity is held by reference.
			for k := range d.state {
				delete(d.state, k)
			}
			for k, v := range snapshot {
				d.state[k] = v
			}
			return fmt.Errorf("setting %q: %w", ch.Path, err)
		}
		diffs[ch.Path] = ch.Value
	}
	if len(diffs) == 0 {
		return nil
	}
	d.Version++
	// Published under the lock, as a route's mutations are, so diffs reach the
	// publisher in version order.
	if d.DiffPublisher != nil {
		d.DiffPublisher(StateChange{DeviceID: d.DeviceID, Version: d.Version, Diffs: diffs, Cause: "handler"})
	}
	d.fanoutToSessions(diffs)
	return nil
}

// serveFamilyRoute answers a matched route with its Go handler. The handler runs
// without the device lock, reading and writing state through its handle.
func (d *Device) serveFamilyRoute(w http.ResponseWriter, r *http.Request, h spec.HTTPHandler, rc requestContext) {
	req := spec.HTTPRequest{Method: rc.Method, Path: rc.Path, Query: rc.Query, Body: rc.Body}
	if r.Body != nil {
		req.Raw, _ = io.ReadAll(io.LimitReader(r.Body, requestBodyLimit))
	}
	res := h(goDevice{d}, req)
	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}
	var body []byte
	ct := res.ContentType
	switch b := res.Body.(type) {
	case nil:
	case string:
		body = []byte(b)
		if ct == "" {
			ct = "text/plain; charset=utf-8"
		}
	case []byte:
		body = b
		if ct == "" {
			ct = "application/octet-stream"
		}
	default:
		enc, err := json.Marshal(b)
		if err != nil {
			slog.Error("handler response not encoded", "device", d.DeviceID, "route", r.Method+" "+r.URL.Path, "error", err)
			http.Error(w, "response error", http.StatusInternalServerError)
			return
		}
		body = enc
		if ct == "" {
			ct = "application/json"
		}
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// runFamilyCommand answers a matched command with its Go handler.
func (d *Device) runFamilyCommand(ce commandEntry) CommandResult {
	return CommandResult{Response: ce.Go(goDevice{d})}
}

// withSurfaceSubjects is an interface's declared subjects with a family
// surface's appended, each as the profile subject the session compiles (its
// token is the publish token, its Push the line).
func withSurfaceSubjects(declared []profile.Subject, from []surface.Subject) []profile.Subject {
	if len(from) == 0 {
		return declared
	}
	out := append([]profile.Subject(nil), declared...)
	for _, s := range from {
		out = append(out, profile.Subject{Match: s.Match, Path: s.Path, PublishToken: s.Token, Push: s.Push})
	}
	return out
}
