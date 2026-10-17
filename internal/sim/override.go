package sim

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// BehaviorOverrideSpec is the shape of a single externally applied,
// scenario-driven behavior override (a future control path, e.g.
// impairments, issue #889). The simulator compiles the expression and runs it on its declared interval, just like a
// profile-declared behavior, but with one extra rule: any profile
// behavior whose mutation targets the same path skips that path while
// the override is active. The skip prevents value-flapping between two
// writers on the same key (#102).
type BehaviorOverrideSpec struct {
	OverrideID string
	Path       string
	Expression string
	// IntervalMs is the tick period in milliseconds; intervals < 0
	// are rejected, 0 is rejected so callers can't accidentally
	// register a behavior that fires immediately on every tick.
	IntervalMs int32
}

// overrideEntry is the running representation of a single override
// installed on the device. Each spec compiles into one mutation and
// gets its own goroutine on its own ticker, same lifecycle as a
// profile-declared behaviorEntry but tracked separately so
// SetBehaviorOverrides can replace the whole set atomically without
// touching profile baselines.
type overrideEntry struct {
	OverrideID string
	Path       string
	Interval   time.Duration
	Mutation   mutation
	stop       chan struct{}
	done       chan struct{}
}

// SetBehaviorOverrides replaces the device's currently-running override
// set with the supplied specs. The caller always supplies the full
// active set, so this is always a complete replacement, partial deltas
// would race with overrides the simulator might already have started.
//
// Validation errors return a non-nil error and leave the device's
// active set unchanged so a malformed override doesn't blow away the
// good ones the operator already applied.
func (d *Device) SetBehaviorOverrides(specs []BehaviorOverrideSpec) error {
	// Compile every spec first so a single bad expression rejects the
	// whole batch before we tear down the existing goroutines.
	d.ensureRand()
	compiled := make([]overrideEntry, 0, len(specs))
	for _, spec := range specs {
		if spec.Path == "" {
			return fmt.Errorf("override %q missing path", spec.OverrideID)
		}
		if spec.Expression == "" {
			return fmt.Errorf("override %q on %q missing expression", spec.OverrideID, spec.Path)
		}
		if spec.IntervalMs <= 0 {
			return fmt.Errorf("override %q interval_ms must be positive (got %d)", spec.OverrideID, spec.IntervalMs)
		}
		// Override mutations are always treated as optional from the
		// simulator's perspective. A stanza targets a heterogeneous
		// fleet and not every device profile declares every key;
		// profile.Mutation.Optional=true makes a missing path log a
		// warning rather than fail (#102 silent-skip semantics).
		m, err := compileBehaviorMutationList([]profile.Mutation{{
			Path:     spec.Path,
			Value:    spec.Expression,
			Optional: true,
		}}, behaviorFuncs(d.rand))
		if err != nil {
			return fmt.Errorf("override %q on %q: %w", spec.OverrideID, spec.Path, err)
		}
		if len(m) != 1 {
			return fmt.Errorf("override %q: compileBehaviorMutationList produced %d mutations, want 1", spec.OverrideID, len(m))
		}
		compiled = append(compiled, overrideEntry{
			OverrideID: spec.OverrideID,
			Path:       spec.Path,
			Interval:   time.Duration(spec.IntervalMs) * time.Millisecond,
			Mutation:   m[0],
		})
	}

	// Stop the previous set under the behavior lock, then swap in the
	// new one. Doing the stop + replace under the same lock keeps the
	// per-tick "is this override path covered" check consistent: the
	// profile-behavior tick sees the new override set (so it skips the
	// right paths) only after the old runners are gone.
	d.behaviorMu.Lock()
	stops := make([]*overrideEntry, 0, len(d.overrides))
	for i := range d.overrides {
		o := &d.overrides[i]
		if o.stop == nil {
			continue
		}
		close(o.stop)
		stops = append(stops, o)
	}
	d.overrides = nil
	d.behaviorMu.Unlock()

	for _, o := range stops {
		<-o.done
	}

	d.behaviorMu.Lock()
	d.overrides = compiled
	for i := range d.overrides {
		o := &d.overrides[i]
		o.stop = make(chan struct{})
		o.done = make(chan struct{})
		go d.runOverride(o)
	}
	d.behaviorMu.Unlock()
	return nil
}

// overridePathsLocked returns the set of paths currently covered by an
// active override. The profile-behavior tick consults this so writes
// that would overlap an override skip silently. Caller MUST hold
// behaviorMu.
func (d *Device) overridePathsLocked() map[string]struct{} {
	if len(d.overrides) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(d.overrides))
	for _, o := range d.overrides {
		out[o.Path] = struct{}{}
	}
	return out
}

// runOverride is the per-override goroutine body. Mirrors runBehavior
// but applies a single mutation per tick and tags the StateChange with
// the override-derived Cause so the console can attribute the diff back
// to the scenario rule. Phase offsets are not staggered here, overrides
// are scenario-driven, short-lived, and operators expect a freshly
// applied scenario to start ticking promptly.
func (d *Device) runOverride(o *overrideEntry) {
	defer close(o.done)
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.tickOverride(o)
		case <-o.stop:
			return
		}
	}
}

// tickOverride applies an override's single mutation. Holds the device
// write lock across mutation + diff publish so any concurrent
// profile-behavior tick sees the updated state and version.
func (d *Device) tickOverride(o *overrideEntry) {
	d.mu.Lock()
	data := templateData{
		State:    d.state,
		Defaults: d.defaults,
		Hostname: d.Hostname,
		DeviceID: d.DeviceID,
		IP:       d.LoopbackIP.String(),
	}
	diffs, errs := applyMutations(d.state, []mutation{o.Mutation}, data)
	if len(diffs) > 0 {
		d.Version++
		if d.DiffPublisher != nil {
			d.DiffPublisher(StateChange{
				DeviceID: d.DeviceID,
				Version:  d.Version,
				Diffs:    diffs,
				Cause:    "override:" + o.OverrideID,
			})
		}
		d.fanoutToSessions(diffs)
	}
	d.mu.Unlock()

	for _, err := range errs {
		var me *MutationError
		if errors.As(err, &me) {
			// Optional mutations (every override mutation is optional)
			// log a warning instead of failing, operators want
			// stanzas that target heterogeneous fleets to skip
			// devices that don't have the path.
			slog.Warn("override mutation failed",
				"device_id", d.DeviceID,
				"override_id", o.OverrideID,
				"path", me.Path,
				"error", me.Err,
				"optional", me.Optional,
			)
			continue
		}
		slog.Warn("override mutation failed",
			"device_id", d.DeviceID,
			"override_id", o.OverrideID,
			"error", err,
		)
	}
}

// StopBehaviorOverrides cancels every running override and waits for
// the goroutines to exit. Idempotent; called by the simulator on
// shutdown so override goroutines don't leak past the device.
func (d *Device) StopBehaviorOverrides() {
	d.behaviorMu.Lock()
	stops := make([]*overrideEntry, 0, len(d.overrides))
	for i := range d.overrides {
		o := &d.overrides[i]
		if o.stop == nil {
			continue
		}
		close(o.stop)
		stops = append(stops, o)
	}
	d.behaviorMu.Unlock()
	for _, o := range stops {
		<-o.done
	}
}

// OverridePaths returns the snapshot set of paths currently covered by
// an active override. Used by tests and (indirectly via tickBehavior)
// by the profile-behavior runner's path-overlap skip.
func (d *Device) OverridePaths() map[string]struct{} {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	if len(d.overrides) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(d.overrides))
	for _, o := range d.overrides {
		out[o.Path] = struct{}{}
	}
	return out
}

// OverrideIDs returns the OverrideIDs of every active override on the
// device, in start order. Used by tests to inspect the result of a
// SetBehaviorOverrides call.
func (d *Device) OverrideIDs() []string {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	out := make([]string, 0, len(d.overrides))
	for _, o := range d.overrides {
		out = append(out, o.OverrideID)
	}
	return out
}

// filterOverriddenMutations returns the subset of muts whose static
// path text is NOT in overridePaths. Used by tickBehavior to drop
// writes to paths an active scenario override would claim.
//
// Only mutations whose path template is a single text node, i.e. a
// literal path with no {{ ... }} interpolation, are subject to the
// skip. Templated paths fall through (rendered + applied as before)
// because the override system's apply surface targets literal state
// keys, so a {{ .Args.id }}-templated mutation can't ever be the
// "same write" as a scenario override on `status.dsp.cpu_load`.
func filterOverriddenMutations(muts []mutation, overridePaths map[string]struct{}) []mutation {
	if len(overridePaths) == 0 || len(muts) == 0 {
		return muts
	}
	out := muts[:0:0]
	for _, m := range muts {
		raw, ok := rawPathSource(m.path)
		if ok {
			if _, hit := overridePaths[raw]; hit {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// overrideRuntimeTickAll is a test helper that runs one synchronous
// tick for every active override on the device. Production code does
// not call this, the per-override goroutine drives ticks on the
// declared interval. Tests need a way to validate behavior without
// sleeping for the real interval.
func (d *Device) overrideRuntimeTickAll() {
	d.behaviorMu.Lock()
	entries := make([]*overrideEntry, 0, len(d.overrides))
	for i := range d.overrides {
		entries = append(entries, &d.overrides[i])
	}
	d.behaviorMu.Unlock()
	for _, o := range entries {
		d.tickOverride(o)
	}
}
