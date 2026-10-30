package bus

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// The fan-out (#1282, ADR-1282): one owner-resolution step the node lane and
// the webhook lane both pass. A sample naming an identity (a match key and
// its value) is the conduit's report about another owner: it lands on the
// one owner the conduit's identity binding for it names, or on no owner at
// all, kept in the conduit's inbox. Nothing in the payload names an owner:
// the identity is resolved only against the bindings of the conduit the
// batch is already confined to (the task's component on the node lane, the
// webhook token's component on the push lane).

// splitIdentified takes a batch's identified samples out for the fan-out:
// own is the batch with only the conduit's own samples (its log lines kept),
// and fanned holds every identified metric, property and event, nil when
// the batch names no identity, so a batch that names none costs nothing
// more than before.
func splitIdentified(ev *ogv1.TelemetryBatch) (own, fanned *ogv1.TelemetryBatch) {
	n := 0
	for _, m := range ev.GetMetrics() {
		if m.GetIdentity() != nil {
			n++
		}
	}
	for _, p := range ev.GetProperties() {
		if p.GetIdentity() != nil {
			n++
		}
	}
	for _, e := range ev.GetEvents() {
		if e.GetIdentity() != nil {
			n++
		}
	}
	if n == 0 {
		return ev, nil
	}
	own = &ogv1.TelemetryBatch{TaskId: ev.GetTaskId(), NodeId: ev.GetNodeId(), Ts: ev.GetTs(), Labels: ev.GetLabels(),
		Owner: ev.GetOwner(), Source: ev.GetSource(), Logs: ev.GetLogs(), TaskLogs: ev.GetTaskLogs()}
	fanned = &ogv1.TelemetryBatch{Ts: ev.GetTs()}
	for _, m := range ev.GetMetrics() {
		if m.GetIdentity() != nil {
			fanned.Metrics = append(fanned.Metrics, m)
		} else {
			own.Metrics = append(own.Metrics, m)
		}
	}
	for _, p := range ev.GetProperties() {
		if p.GetIdentity() != nil {
			fanned.Properties = append(fanned.Properties, p)
		} else {
			own.Properties = append(own.Properties, p)
		}
	}
	for _, e := range ev.GetEvents() {
		if e.GetIdentity() != nil {
			fanned.Events = append(fanned.Events, e)
		} else {
			own.Events = append(own.Events, e)
		}
	}
	return own, fanned
}

// identityKeys is every distinct valid identity a fanned batch names, the one
// read's input.
func identityKeys(fanned *ogv1.TelemetryBatch) []storage.IdentityKey {
	seen := map[storage.IdentityKey]bool{}
	var out []storage.IdentityKey
	add := func(id *ogv1.Identity) {
		k, ok := identityKey(id)
		if ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, m := range fanned.GetMetrics() {
		add(m.GetIdentity())
	}
	for _, p := range fanned.GetProperties() {
		add(p.GetIdentity())
	}
	for _, e := range fanned.GetEvents() {
		add(e.GetIdentity())
	}
	return out
}

// identityKey reads a received identity, or refuses it: an identity is a
// claim from a node or a sender, so the server holds it again to the
// engine's own bounds (driver.ValidIdentity: a key and a value each
// non-empty, at most 256 bytes, valid UTF-8, free of control characters, a
// NUL among them, which would fail the batch's one transaction) before it
// reaches a read or the inbox.
func identityKey(id *ogv1.Identity) (storage.IdentityKey, bool) {
	k := storage.IdentityKey{Key: id.GetKey(), Value: id.GetValue()}
	if driver.ValidIdentity(driver.Identity{Key: k.Key, Value: k.Value}) != nil {
		return storage.IdentityKey{}, false
	}
	return k, true
}

// maxKeysCarried bounds the keys one inbox row remembers.
const maxKeysCarried = 32

// fanPlan is what the fan-out derives from one batch's identified samples:
// the rows that land on their bound owners, what the conduit heard that no
// binding places, and how many samples named no valid identity.
type fanPlan struct {
	rows    storage.FannedRows
	unbound []storage.UnboundIdentityWrite
	invalid int
}

// planFanOut derives the fanned rows (pure). Each sample whose identity is
// bound lands on its owner's arc through the lane's own derivation, with the
// fan-out writer (collected keys only), at the empty instance (the
// conduit's endpoint is no endpoint of the owner, ADR-1282 point 6) and
// naming its observer in source as <conduit>/<endpoint>. Each unbound
// identity is aggregated for the inbox: how many samples named it, first
// and last, and the keys they carried. now dates a sample that carries no
// time.
func planFanOut(fanned *ogv1.TelemetryBatch, conduitID, conduitName, endpoint string,
	owners map[storage.IdentityKey]storage.IdentityOwner, reg collection.Registry, now time.Time) fanPlan {
	w := storage.FanoutWriter(conduitID)
	p := fanPlan{rows: storage.FannedRows{Writer: w}}
	source := conduitName + "/" + endpoint
	type heard struct {
		count       int64
		first, last time.Time
		keys        []string
	}
	unbound := map[storage.IdentityKey]*heard{}
	var order []storage.IdentityKey
	// hear records one unbound sample, dated by the server's clock (a
	// device's or payload's time would let an old record age out the moment
	// it lands, or a future one outlive the inbox's window), and remembers
	// its key only when the lane's catalog holds it, at most
	// maxKeysCarried, so the row names only keys a binding would land.
	hear := func(k storage.IdentityKey, key string, known bool) {
		h, ok := unbound[k]
		if !ok {
			h = &heard{first: now, last: now}
			unbound[k] = h
			order = append(order, k)
		}
		h.count++
		if known && len(h.keys) < maxKeysCarried && !slices.Contains(h.keys, key) {
			h.keys = append(h.keys, key)
		}
	}
	// route sorts one sample: its owner's sub-batch when bound, the inbox
	// when not, nothing when its identity is no valid identity.
	byOwner := map[storage.IdentityOwner]*ogv1.TelemetryBatch{}
	var owned []storage.IdentityOwner
	sub := func(o storage.IdentityOwner) *ogv1.TelemetryBatch {
		b, ok := byOwner[o]
		if !ok {
			b = &ogv1.TelemetryBatch{Ts: fanned.GetTs(), Source: source}
			byOwner[o] = b
			owned = append(owned, o)
		}
		return b
	}
	route := func(id *ogv1.Identity, name string, known bool) (storage.IdentityOwner, bool) {
		k, ok := identityKey(id)
		if !ok {
			p.invalid++
			return storage.IdentityOwner{}, false
		}
		o, ok := owners[k]
		if !ok {
			hear(k, name, known)
			return storage.IdentityOwner{}, false
		}
		return o, true
	}
	for _, m := range fanned.GetMetrics() {
		_, known := reg.MetricIn(m.GetNamespace(), m.GetName())
		if o, ok := route(m.GetIdentity(), m.GetName(), known); ok {
			b := sub(o)
			b.Metrics = append(b.Metrics, &ogv1.MetricSample{Namespace: m.GetNamespace(), Name: m.GetName(), Value: m.GetValue(), Ts: m.GetTs()})
		}
	}
	for _, s := range fanned.GetProperties() {
		_, known := reg.PropertyIn(s.GetNamespace(), s.GetName())
		if o, ok := route(s.GetIdentity(), s.GetName(), known); ok {
			b := sub(o)
			b.Properties = append(b.Properties, &ogv1.PropertySample{Namespace: s.GetNamespace(), Name: s.GetName(), ValueJson: s.GetValueJson(), Ts: s.GetTs()})
		}
	}
	for _, e := range fanned.GetEvents() {
		_, known := reg.EventIn(e.GetNamespace(), e.GetName())
		if o, ok := route(e.GetIdentity(), e.GetName(), known); ok {
			b := sub(o)
			b.Events = append(b.Events, &ogv1.EventSample{Namespace: e.GetNamespace(), Name: e.GetName(), Message: e.GetMessage(), Payload: e.GetPayload(), Ts: e.GetTs()})
		}
	}
	// The empty instance: a sub-batch's samples carry none, and the owner
	// passed to the lanes names no endpoint, so the lane stamps none.
	none := storage.TaskOwner{}
	for _, o := range owned {
		b, arc := byOwner[o], sampleArc{Kind: o.Kind, ID: o.ID}
		p.rows.Metrics = append(p.rows.Metrics, deriveMetrics(b, arc, none, reg, w)...)
		p.rows.Properties = append(p.rows.Properties, deriveProperties(b, arc, none, reg, w)...)
		p.rows.Events = append(p.rows.Events, deriveEvents(b, arc, none, reg, w)...)
	}
	for _, k := range order {
		h := unbound[k]
		keys := slices.Clone(h.keys)
		slices.Sort(keys)
		p.unbound = append(p.unbound, storage.UnboundIdentityWrite{ConduitID: conduitID, Key: k.Key, Value: k.Value,
			Count: h.count, FirstAt: h.first, LastAt: h.last, Keys: keys})
	}
	return p
}

// unboundWarnWindow is how often the ingest warns about one conduit's
// unbound identities: once per conduit per window, never per sample (#1200's
// discipline); the inbox holds the detail.
const unboundWarnWindow = 5 * time.Minute

// unboundWarner remembers, per conduit, when it last warned and how many
// unbound samples it has heard since.
type unboundWarner struct {
	mu    sync.Mutex
	last  map[string]time.Time
	quiet map[string]int
}

// note records n unbound samples on a conduit and warns when the conduit's
// window has closed, naming what the window held. Entries whose window
// closed long ago are forgotten, so the map is bounded by the conduits heard
// recently.
func (u *unboundWarner) note(conduit, name string, n, identities int, now time.Time) {
	u.mu.Lock()
	if u.last == nil {
		u.last, u.quiet = map[string]time.Time{}, map[string]int{}
	}
	at, seen := u.last[conduit]
	if seen && now.Sub(at) < unboundWarnWindow {
		u.quiet[conduit] += n
		u.mu.Unlock()
		return
	}
	suppressed := u.quiet[conduit]
	u.last[conduit], u.quiet[conduit] = now, 0
	for c, t := range u.last {
		if now.Sub(t) > 2*unboundWarnWindow {
			delete(u.last, c)
			delete(u.quiet, c)
		}
	}
	u.mu.Unlock()
	slog.Warn("conduit reported identities no binding places, kept in its unbound inbox",
		"facility", "ingest", "conduit", name, "samples", n, "identities", identities,
		"suppressed_since_last_warning", suppressed, "window", unboundWarnWindow.String())
}
