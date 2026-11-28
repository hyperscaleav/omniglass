package spectest

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// Fake is a spec.Device with no engine behind it, for a family's unit tests:
// the state of one product of a family (its state tree with the model's
// overlay), a clock the test sets, and the writes a handler made. A handler
// or a tick under test reads and writes it as it would the engine's device.
type Fake struct {
	Spec     spec.Spec
	Row      spec.Model
	ID, Host string
	// Clock is what Now reads.
	Clock time.Time
	// Writes lists every WriteMany, Write counting as one of one.
	Writes [][]surface.Change
	// Posts lists every webhook the device was asked to send, Reports every
	// event it was asked to name on its stream (name, data).
	Posts   []spec.Post
	Reports [][2]string
	// RoomsRead is what the device reads of its rooms, none when nil.
	RoomsRead spec.Rooms
	// Events lists every event published, in order.
	Events []spec.Event

	state, defaults map[string]any
}

// NewFake is a device of the family's product (the family's own name for its
// base state), its state and defaults as the engine would build them bar the
// per-device identity.
func NewFake(s spec.Spec, product string) *Fake {
	f := &Fake{Spec: s, ID: "fake-" + product, Host: "fake-" + product, Clock: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	base := clone(s.State)
	for _, m := range s.Models {
		if m.Product == product {
			f.Row = m
			merge(base, clone(m.State))
		}
	}
	f.defaults, f.state = base, clone(base)
	return f
}

func (f *Fake) DeviceID() string      { return f.ID }
func (f *Fake) Hostname() string      { return f.Host }
func (f *Fake) IP() string            { return "127.0.0.1" }
func (f *Fake) Model() spec.Model     { return f.Row }
func (f *Fake) Now() time.Time        { return f.Clock }
func (f *Fake) Read(p string) any     { return look(f.state, p) }
func (f *Fake) Default(p string) any  { return look(f.defaults, p) }
func (f *Fake) State() map[string]any { return clone(f.state) }

// Post records the webhook, as the engine's device would send it.
func (f *Fake) Post(p spec.Post) {
	if p.URL != "" {
		f.Posts = append(f.Posts, p)
	}
}

// Report records the event.
func (f *Fake) Report(name, data string) { f.Reports = append(f.Reports, [2]string{name, data}) }

// Rooms is what the fake reads of its rooms: none unless the test gives it some.
func (f *Fake) Rooms() spec.Rooms {
	if f.RoomsRead != nil {
		return f.RoomsRead
	}
	return spec.RoomsOf(struct{ spec.Device }{f})
}
func (f *Fake) Publish(e spec.Event) { f.Events = append(f.Events, e) }

func (f *Fake) Write(path string, v any) error {
	return f.WriteMany(surface.Change{Path: path, Value: v})
}

func (f *Fake) WriteMany(changes ...surface.Change) error {
	f.Writes = append(f.Writes, changes)
	for _, c := range changes {
		set(f.state, c.Path, c.Value)
	}
	return nil
}

func look(root map[string]any, path string) any {
	var cur any = root
	for _, seg := range strings.Split(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return nil
			}
			cur = c[i]
		default:
			return nil
		}
	}
	return cur
}

// set writes a path, a list element by its index, making the maps it lacks.
func set(root map[string]any, path string, v any) {
	segs := strings.Split(path, ".")
	var cur any = root
	for _, seg := range segs[:len(segs)-1] {
		switch c := cur.(type) {
		case map[string]any:
			next, ok := c[seg]
			if !ok {
				next = map[string]any{}
				c[seg] = next
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return
			}
			cur = c[i]
		}
	}
	last := segs[len(segs)-1]
	switch c := cur.(type) {
	case map[string]any:
		c[last] = v
	case []any:
		if i, err := strconv.Atoi(last); err == nil && i >= 0 && i < len(c) {
			c[i] = v
		}
	}
}

func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return clone(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if d, ok := dst[k].(map[string]any); ok {
				merge(d, sub)
				continue
			}
		}
		dst[k] = v
	}
}

// Ticks runs every tick of the family once against the fake, in order, as the
// engine would apply them (every change written), and returns the paths a tick
// wrote that it did not declare in its Writes.
func (f *Fake) Ticks(c spec.TickContext) (undeclared []string) {
	for _, t := range f.Spec.Ticks {
		declared := map[string]bool{}
		for _, p := range t.Writes {
			declared[p] = true
		}
		for _, ch := range t.Run(f, c) {
			if !declared[ch.Path] {
				undeclared = append(undeclared, fmt.Sprintf("%s writes %s", t.Name, ch.Path))
			}
			set(f.state, ch.Path, ch.Value)
		}
		if t.Emit != nil {
			for _, e := range t.Emit(f, c) {
				f.Publish(e)
			}
		}
	}
	return undeclared
}
