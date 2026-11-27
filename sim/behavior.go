package sim

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/sim/profile"
)

// behaviorEntry holds a compiled behavior ready to run on its own
// goroutine. The Mutations slice was parsed against the device's
// behaviorFuncs FuncMap, which closes over the device's *deviceRand,
// so randStep / drift behaviors are deterministic per device. The
// stop channel is closed by StopBehaviors to wind the runner down.
type behaviorEntry struct {
	Name     string
	Interval time.Duration
	// OnStart ticks the behavior once as it starts, before its phase offset.
	OnStart   bool
	Mutations []mutation
	// Emit, when non-nil, makes the behavior POST an outbound webhook
	// after its mutations apply each tick (best-effort, off the tick
	// goroutine). nil means no emit.
	Emit *emitAction
	// Events are the named events the behavior reports after its mutations
	// (#2754), each data template compiled once against the behavior's
	// FuncMap; one that renders empty is not reported that tick.
	Events []routeEvent
	stop   chan struct{}
	done   chan struct{}
}

// AddBehaviorFromProfile compiles a profile-declared behavior and
// stores it on the device. Compilation here, at LoadDevices time,
// keeps the per-tick path cheap: the goroutine spawned later only
// has to call applyMutations.
//
// Validation:
//
//   - Name must be non-empty (used for logging and phase offsetting).
//   - Schedule.Interval must parse as a Go duration and be positive.
//   - At least one mutation, emit or event must be declared (an empty
//     behavior just burns a goroutine, so we reject at load time rather
//     than ship the foot-gun).
//
// Behaviors that fail validation cause LoadDevices to return an error
// so a malformed profile fails loudly at startup rather than silently
// not animating.
func (d *Device) AddBehaviorFromProfile(b *profile.Behavior) error {
	if b == nil {
		return errors.New("behavior is nil")
	}
	if b.Name == "" {
		return errors.New("behavior missing required field 'name'")
	}
	if b.Schedule.Interval == "" {
		return fmt.Errorf("behavior %q missing schedule.interval", b.Name)
	}
	interval, err := time.ParseDuration(b.Schedule.Interval)
	if err != nil {
		return fmt.Errorf("behavior %q: parsing schedule.interval %q: %w", b.Name, b.Schedule.Interval, err)
	}
	if interval <= 0 {
		return fmt.Errorf("behavior %q: schedule.interval must be positive (got %s)", b.Name, interval)
	}
	if len(b.Mutations) == 0 && b.Emit == nil && len(b.Events) == 0 {
		return fmt.Errorf("behavior %q has no mutations, emit or events", b.Name)
	}

	d.ensureRand()
	funcs := d.behaviorFuncs()
	muts, err := compileBehaviorMutationList(b.Mutations, funcs)
	if err != nil {
		return fmt.Errorf("behavior %q: %w", b.Name, err)
	}
	emit, err := compileEmit(b.Emit, funcs)
	if err != nil {
		return fmt.Errorf("behavior %q: %w", b.Name, err)
	}
	evs, err := compileNamedEvents("behavior "+strconv.Quote(b.Name), b.Events, funcs)
	if err != nil {
		return err
	}

	d.behaviors = append(d.behaviors, behaviorEntry{
		Name:      b.Name,
		Interval:  interval,
		OnStart:   b.Schedule.OnStart,
		Mutations: muts,
		Emit:      emit,
		Events:    evs,
	})
	return nil
}

// StartBehaviors launches one goroutine per registered behavior. Each
// goroutine sleeps for a deterministic phase offset, then ticks at the
// behavior's interval applying its mutations. Safe to call multiple
// times; behaviors already started are skipped via the per-entry stop
// channel sentinel.
//
// The phase offset (hash(device_id, behavior_name) mod interval)
// staggers behaviors across devices so a 1000-device fleet with the
// same uptime-tick behavior won't all fire at the same wall clock
// instant, useful for keeping the event log readable.
//
// A behavior that declares schedule.on_start ticks once, here and before
// StartBehaviors returns, so what it derives from the lab calendar is the
// room's from the device's first request (#2757).
func (d *Device) StartBehaviors() {
	var first []*behaviorEntry
	d.behaviorMu.Lock()
	for i := range d.behaviors {
		b := &d.behaviors[i]
		if b.stop != nil {
			continue
		}
		b.stop = make(chan struct{})
		b.done = make(chan struct{})
		if b.OnStart {
			first = append(first, b)
		}
		offset := time.Duration(behaviorPhaseOffset(d.DeviceID, b.Name, int64(b.Interval)))
		go d.runBehavior(b, offset)
	}
	d.behaviorMu.Unlock()
	for _, b := range first {
		d.tickBehavior(b)
	}
}

// StopBehaviors cancels every running behavior and waits for the
// goroutines to exit. Idempotent: calling on a device with no started
// behaviors returns immediately.
func (d *Device) StopBehaviors() {
	d.behaviorMu.Lock()
	stops := make([]*behaviorEntry, 0, len(d.behaviors))
	for i := range d.behaviors {
		b := &d.behaviors[i]
		if b.stop == nil {
			continue
		}
		close(b.stop)
		stops = append(stops, b)
	}
	d.behaviorMu.Unlock()
	for _, b := range stops {
		<-b.done
		b.stop = nil
		b.done = nil
	}
}

// runBehavior is the per-behavior goroutine body. Sleeps for the phase
// offset, then loops on the ticker applying the behavior's mutations
// each tick. Exits when stop is closed.
func (d *Device) runBehavior(b *behaviorEntry, offset time.Duration) {
	defer close(b.done)

	if offset > 0 {
		t := time.NewTimer(offset)
		select {
		case <-t.C:
		case <-b.stop:
			t.Stop()
			return
		}
	}

	ticker := time.NewTicker(b.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.tickBehavior(b)
		case <-b.stop:
			return
		}
	}
}

// tickBehavior applies one fire of a behavior's mutations against the
// device's current state. Errors log via slog.Warn; behaviors don't
// have an HTTP response surface to gate on, and the priority is keeping
// the goroutine alive across transient failures (a render error one
// tick shouldn't kill the runner).
//
// Diff publishing and session fanout both happen under the same lock
// that bumped the version, matching the route handler's pattern. Two
// concurrent ticks on the same device can't deliver diffs to the
// upstream publisher (or to subscribed shell sessions) in the wrong
// version order, so a `subscribe` client on a persistent SSH/Telnet/
// TCP listener sees behavior-driven changes pushed in the same shape
// it sees route- and command-driven changes.
//
// Path-overlap skip: any mutation whose static path text matches an
// active behavior override on the device is dropped from this tick
// (omniglass-lab#102). The skip applies at the per-mutation level so a multi-path
// behavior keeps writing the paths the override does not cover. Only
// literal paths participate in the skip, templated paths fall through
// to applyMutations as before, which is the conservative default until
// an operator pattern emerges for overriding templated keys.
func (d *Device) tickBehavior(b *behaviorEntry) {
	overridePaths := d.OverridePaths()
	muts := filterOverriddenMutations(b.Mutations, overridePaths)

	d.mu.Lock()
	data := templateData{
		State:    d.state,
		Defaults: d.defaults,
		Hostname: d.Hostname,
		DeviceID: d.DeviceID,
		IP:       d.LoopbackIP.String(),
	}
	// Only what the tick changed fans out (#2754): a timer that rewrites a
	// watched path with the value it already holds (a call cycle keeping an
	// idle call idle) streams nothing and bumps no version.
	diffs, errs := applyChangedMutations(d.state, muts, data)
	if len(diffs) > 0 {
		d.Version++
		if d.DiffPublisher != nil {
			d.DiffPublisher(StateChange{
				DeviceID: d.DeviceID,
				Version:  d.Version,
				Diffs:    diffs,
				Cause:    "behavior:" + b.Name,
			})
		}
		d.fanoutToSessions(diffs)
	}
	// The behavior's named events (#2754), after its changes, rendered
	// against the state they left, in the same numbered stream; one whose
	// data renders empty is not this tick's.
	if d.events != nil {
		for _, ev := range b.Events {
			var buf bytes.Buffer
			if err := ev.data.Execute(&buf, data); err != nil {
				slog.Warn("behavior event not rendered", "device_id", d.DeviceID, "behavior", b.Name, "event", ev.name, "error", err)
				continue
			}
			if strings.TrimSpace(buf.String()) == "" {
				continue
			}
			d.events.publish(ev.name, strings.TrimSpace(buf.String()))
		}
	}
	// Emit the outbound webhook after mutations apply, so the body
	// renders against post-mutation state (a "meeting started" emit sees
	// the meeting flag it just set this tick). render() reads d.state, so
	// it runs under the lock; the actual POST is dispatched on its own
	// goroutine inside runEmit. No-op when the behavior has no emit or
	// the target URL is unconfigured.
	d.runEmit(b.Emit, b.Name, data)
	d.mu.Unlock()

	for _, err := range errs {
		var me *MutationError
		if errors.As(err, &me) {
			slog.Warn("behavior mutation failed",
				"device_id", d.DeviceID,
				"behavior", b.Name,
				"path", me.Path,
				"error", me.Err,
				"optional", me.Optional,
			)
			continue
		}
		slog.Warn("behavior mutation failed",
			"device_id", d.DeviceID,
			"behavior", b.Name,
			"error", err,
		)
	}
}

// TickBehavior fires one tick of the named behavior now, as its timer would,
// and reports whether the device has it: a test stepping a device through
// its cycle without waiting on the clock.
func (d *Device) TickBehavior(name string) bool {
	d.behaviorMu.Lock()
	var b *behaviorEntry
	for i := range d.behaviors {
		if d.behaviors[i].Name == name {
			b = &d.behaviors[i]
		}
	}
	d.behaviorMu.Unlock()
	if b == nil {
		return false
	}
	d.tickBehavior(b)
	return true
}

// ensureRand lazily initializes the per-device random source on first
// use. Behaviors are the only consumer today; deferring construction
// to AddBehaviorFromProfile keeps unrelated tests / device fixtures
// from paying for a rand.New they never touch.
func (d *Device) ensureRand() {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	if d.rand == nil {
		d.rand = newDeviceRand(d.DeviceID)
	}
}

// compileBehaviorMutationList mirrors compileMutationList in mutate.go
// but takes an explicit FuncMap so the device's rand-bound helpers are
// in scope at parse time. The output mutations carry compiled templates
// that already close over those helpers, so applyMutations stays
// FuncMap-agnostic at execution time.
func compileBehaviorMutationList(items []profile.Mutation, funcs template.FuncMap) ([]mutation, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]mutation, 0, len(items))
	for _, item := range items {
		if item.Path == "" {
			return nil, fmt.Errorf("mutation missing required field 'path'")
		}
		m, err := compileOneFuncs(item.Path, item.Value, item.Enum, item.Pattern, item.Optional, funcs)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Behaviors returns the names of all registered behaviors on the
// device, in declaration order. Used by tests and any consumer that needs
// to enumerate what's running.
func (d *Device) Behaviors() []string {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	out := make([]string, 0, len(d.behaviors))
	for _, b := range d.behaviors {
		out = append(out, b.Name)
	}
	return out
}
