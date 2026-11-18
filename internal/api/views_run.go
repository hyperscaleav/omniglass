package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/logline"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/tail"
)

// The views binding (#1830, ADR-1830): one scoped read that a dashboard
// widget, an agent, or the CLI makes of the sample store. A binding names a
// primitive (metric, property, event, log), an entity selector (literal refs,
// or a kind and a filter), the keys, and a range. /views:run answers every
// binding's history from one snapshot with the tail cursor it was taken at;
// /views:watch follows the tail from that cursor, every binding of a page
// over one SSE stream, so what the history ends on is exactly where the live
// rows begin.

// viewMaxRange bounds a binding's range: two years, the longest trend
// retention a platform setting admits.
const (
	viewMaxRange     = 2 * 365 * 24 * time.Hour
	viewMaxBindings  = 32
	viewTailBatch    = 256
	viewWatchMaxBody = 32 << 10
)

// The watch's bounds. A watch runs at most viewWatchLifetime, then the
// server ends it with its position and the client reconnects, so no stream
// lives on the scope it resolved long ago. Every viewWatchHeartbeat an idle
// one sends its position (keeping the connection alive through proxies and
// the resume cursor inside the tail's ring) and rechecks its caller. One user
// holds at most viewWatchesPerUser open, a server viewWatchesTotal.
const (
	viewWatchLifetime  = 15 * time.Minute
	viewWatchHeartbeat = 15 * time.Second
	viewWatchesPerUser = 16
	viewWatchesTotal   = 1024
)

// viewWatchLimits are the watch's bounds a handler runs with; a zero field
// takes its default.
type viewWatchLimits struct {
	perUser             int
	lifetime, heartbeat time.Duration
}

func (l viewWatchLimits) withDefaults() viewWatchLimits {
	if l.perUser <= 0 {
		l.perUser = viewWatchesPerUser
	}
	if l.lifetime <= 0 {
		l.lifetime = viewWatchLifetime
	}
	if l.heartbeat <= 0 {
		l.heartbeat = viewWatchHeartbeat
	}
	return l
}

type viewFilterBody struct {
	System     string `json:"system,omitempty" maxLength:"200" doc:"Components that are members of this system (name or uuid). Component bindings only"`
	Product    string `json:"product,omitempty" maxLength:"200" doc:"Components of this product (name or uuid). Component bindings only"`
	SystemType string `json:"system_type,omitempty" maxLength:"200" doc:"Systems of this system type (name or uuid). System bindings only"`
}

type viewBindingBody struct {
	Primitive     string          `json:"primitive" enum:"metric,property,event,log" doc:"The primitive read: metric and property answer one series per entity, key and instance; event and log answer rows, newest first, a page at a time"`
	Kind          string          `json:"kind" enum:"component,system" doc:"The entity kind the selector names. The binding needs <kind>:read on top of view:read, and resolves only inside that read's scope. Log lines belong to components"`
	Refs          []string        `json:"refs,omitempty" maxItems:"200" doc:"Literal entities, by name, uuid or dotted address. One outside the caller's scope is the same 404 as one that does not exist. Unset, the binding selects every visible entity of the kind that filter keeps"`
	Filter        *viewFilterBody `json:"filter,omitempty" doc:"Narrows a kind selector; refused beside refs"`
	Keys          []string        `json:"keys,omitempty" maxItems:"50" doc:"The metric, property or event type names read. Required for metric and property; unset on an event binding reads every type; refused on a log binding"`
	From          *time.Time      `json:"from,omitempty" doc:"An absolute range's start. Give from (and optionally to), or window_seconds"`
	To            *time.Time      `json:"to,omitempty" doc:"An absolute range's end, exclusive; unset is now. Needs from"`
	WindowSeconds int             `json:"window_seconds,omitempty" minimum:"1" maximum:"63072000" doc:"A relative range: this many seconds back from now"`
	StepSeconds   int             `json:"step_seconds,omitempty" minimum:"1" maximum:"2592000" doc:"Buckets a metric or property read on an epoch-aligned grid of this width (at most 5000 buckets). An hour-multiple step reaching past the raw retention answers the older hours from the hourly trends. Unset, the raw points"`
	Limit         int             `json:"limit,omitempty" minimum:"1" maximum:"1000" doc:"An event or log page's size; 100 when unset"`
	Page          string          `json:"page,omitempty" maxLength:"64" doc:"An event or log read's next_page, to read further back"`
	Severity      string          `json:"severity,omitempty" enum:"emergency,alert,critical,error,warning,notice,info,debug" doc:"Log bindings: only lines at this severity or more severe"`
	Source        string          `json:"source,omitempty" maxLength:"100" doc:"Log bindings: only lines that arrived on this channel"`
}

// viewBindingErr is a binding the contract refuses, a 422 at its index.
type viewBindingErr struct{ msg string }

func (e *viewBindingErr) Error() string { return e.msg }

func bindingErr(format string, args ...any) error {
	return &viewBindingErr{msg: fmt.Sprintf(format, args...)}
}

// toViewBinding checks a binding against the contract and shapes it for the
// Gateway; the caller resolves its scope. It is the one check both routes
// run, so the watch (whose bindings arrive in a query string Huma does not
// validate) refuses exactly what the run does.
func toViewBinding(in viewBindingBody) (storage.ViewBinding, error) {
	var b storage.ViewBinding
	switch tail.Lane(in.Primitive) {
	case tail.LaneMetric, tail.LaneProperty, tail.LaneEvent, tail.LaneLog:
		b.Primitive = tail.Lane(in.Primitive)
	default:
		return b, bindingErr("primitive %q is not one of metric, property, event, log", in.Primitive)
	}
	if in.Kind != "component" && in.Kind != "system" {
		return b, bindingErr("kind %q is not one of component, system", in.Kind)
	}
	b.Kind = in.Kind
	if len(in.Refs) > storage.ViewMaxEntities {
		return b, bindingErr("%d refs, at most %d", len(in.Refs), storage.ViewMaxEntities)
	}
	for _, r := range in.Refs {
		if strings.TrimSpace(r) == "" {
			return b, bindingErr("a ref is empty")
		}
	}
	b.Refs = in.Refs
	if in.Filter != nil {
		if len(in.Refs) > 0 {
			return b, bindingErr("a binding selects by refs or by filter, not both")
		}
		f := *in.Filter
		if b.Kind == "component" && f.SystemType != "" {
			return b, bindingErr("filter.system_type selects systems, not components")
		}
		if b.Kind == "system" && (f.System != "" || f.Product != "") {
			return b, bindingErr("filter.system and filter.product select components, not systems")
		}
		b.Filter = storage.ViewFilter{System: f.System, Product: f.Product, SystemType: f.SystemType}
	}

	switch b.Primitive {
	case tail.LaneMetric, tail.LaneProperty:
		if len(in.Keys) == 0 {
			return b, bindingErr("a %s binding names its keys", b.Primitive)
		}
		if in.Limit != 0 || in.Page != "" {
			return b, bindingErr("limit and page page an event or log read, not a %s series", b.Primitive)
		}
	case tail.LaneEvent, tail.LaneLog:
		if in.StepSeconds != 0 {
			return b, bindingErr("step_seconds buckets a metric or property read, not %s rows", b.Primitive)
		}
	}
	if b.Primitive == tail.LaneLog {
		if b.Kind != "component" {
			return b, bindingErr("log lines belong to components")
		}
		if len(in.Keys) > 0 {
			return b, bindingErr("a log binding has no keys; narrow it with severity and source")
		}
		if in.Severity != "" {
			if _, ok := logline.AtLeast(in.Severity); !ok {
				return b, bindingErr("severity %q is not one of %s", in.Severity, strings.Join(logline.Levels(), ", "))
			}
		}
		b.Logs = storage.LogFilter{Severity: in.Severity, Source: in.Source}
	} else if in.Severity != "" || in.Source != "" {
		return b, bindingErr("severity and source narrow a log binding")
	}
	for _, k := range in.Keys {
		if strings.TrimSpace(k) == "" {
			return b, bindingErr("a key is empty")
		}
	}
	b.Keys = in.Keys

	switch {
	case in.From != nil && in.WindowSeconds != 0:
		return b, bindingErr("a range is from (and to) or window_seconds, not both")
	case in.From != nil:
		b.From = in.From.UTC()
		if in.To != nil {
			b.To = in.To.UTC()
			if !b.To.After(b.From) {
				return b, bindingErr("to is not after from")
			}
			if b.To.Sub(b.From) > viewMaxRange {
				return b, bindingErr("a range of at most two years")
			}
		}
	case in.To != nil:
		return b, bindingErr("to needs from")
	case in.WindowSeconds > 0:
		b.Window = time.Duration(in.WindowSeconds) * time.Second
		if b.Window > viewMaxRange {
			return b, bindingErr("a window of at most two years")
		}
	default:
		return b, bindingErr("a binding has a range: from, or window_seconds")
	}
	if in.StepSeconds < 0 || in.Limit < 0 || in.WindowSeconds < 0 {
		return b, bindingErr("step_seconds, limit and window_seconds are positive")
	}
	b.Step = time.Duration(in.StepSeconds) * time.Second
	if in.Limit > storage.ViewMaxPage {
		return b, bindingErr("limit is at most %d", storage.ViewMaxPage)
	}
	b.Limit, b.Page = in.Limit, in.Page
	return b, nil
}

// viewBindings checks every binding, refuses one whose kind the caller may
// not read (the primitive's read permission, beside the view:read the route
// is gated by), and scopes the rest to that read.
func viewBindings(ctx context.Context, a *authenticator, in []viewBindingBody, where string) ([]storage.ViewBinding, error) {
	if len(in) == 0 || len(in) > viewMaxBindings {
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("between 1 and %d bindings", viewMaxBindings))
	}
	out := make([]storage.ViewBinding, 0, len(in))
	for i, body := range in {
		b, err := toViewBinding(body)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("binding "+fmt.Sprint(i)+": "+err.Error(),
				&huma.ErrorDetail{Message: err.Error(), Location: fmt.Sprintf("%s[%d]", where, i)})
		}
		if !a.allows(ctx, b.Kind, "read") {
			return nil, huma.Error403Forbidden(fmt.Sprintf("binding %d reads %ss, which needs %s:read", i, b.Kind, b.Kind))
		}
		b.Read = a.scopeFor(ctx, b.Kind, "read")
		out = append(out, b)
	}
	return out, nil
}

// viewReadErr maps a views read's failure: a literal ref out of reach is the
// non-disclosing 404, a bound passed or a malformed page a 422, anything else
// logged and opaque.
func viewReadErr(ctx context.Context, log *slog.Logger, err error) error {
	switch {
	case errors.Is(err, storage.ErrViewEntityNotFound):
		return huma.Error404NotFound(strings.TrimPrefix(err.Error(), "storage: "))
	case errors.Is(err, storage.ErrViewTooLarge), errors.Is(err, storage.ErrViewPage), errors.Is(err, storage.ErrUnknownSeverity):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: "))
	}
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	log.ErrorContext(ctx, "read views", "error", err)
	return huma.Error500InternalServerError("read views")
}

type viewEntityBody struct {
	ID   string `json:"id" doc:"The entity's uuid, the id its series and rows carry"`
	Name string `json:"name"`
	Kind string `json:"kind" doc:"component or system"`
}

type viewPointBody struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

type viewMetricSeriesBody struct {
	Entity   string             `json:"entity" doc:"The entity's uuid"`
	Key      string             `json:"key" doc:"The metric type's name"`
	Instance string             `json:"instance" doc:"The series discriminator; empty for the owner-level series"`
	Points   []viewPointBody    `json:"points,omitempty" doc:"The raw samples, oldest first; on a read without a step"`
	Buckets  []metricBucketBody `json:"buckets,omitempty" doc:"Every bucket of the grid, oldest first; on a read with a step"`
}

type viewPropertyPointBody struct {
	TS    time.Time `json:"ts"`
	Value string    `json:"value"`
}

type viewPropertySeriesBody struct {
	Entity    string                  `json:"entity" doc:"The entity's uuid"`
	Key       string                  `json:"key" doc:"The property type's name"`
	Instance  string                  `json:"instance" doc:"The series discriminator; empty for the owner-level series"`
	CarriedIn *string                 `json:"carried_in,omitempty" doc:"The value in force when the range opens; absent when none"`
	Points    []viewPropertyPointBody `json:"points,omitempty" doc:"The changes, oldest first; on a read without a step"`
	Buckets   []propertyBucketBody    `json:"buckets,omitempty" doc:"Every bucket of the grid, oldest first; on a read with a step"`
}

// viewEventBody is one event row, in history and on the tail alike; the
// lineage fields are history's alone. Spelled out rather than embedding
// eventBody, which Huma's schema would not flatten.
type viewEventBody struct {
	Entity        string          `json:"entity" doc:"The owning entity's uuid"`
	ID            int64           `json:"id" doc:"The row's id, the page order's tiebreak"`
	TS            time.Time       `json:"ts"`
	Key           string          `json:"key" doc:"The event type's name"`
	Instance      string          `json:"instance,omitempty"`
	Origin        string          `json:"origin" doc:"caught, caused, derived or scheduled"`
	Message       string          `json:"message"`
	Attributes    json.RawMessage `json:"attributes,omitempty"`
	Provenance    string          `json:"provenance,omitempty" doc:"The occurrence's lineage; history rows only"`
	Source        string          `json:"source,omitempty" doc:"The transport that produced it; history rows only"`
	DerivedByRule string          `json:"derived_by_rule,omitempty" doc:"The deriving event rule's name; history rows only"`
	AlarmID       string          `json:"alarm_id,omitempty" doc:"The alarm a rule's event opened or resolved; history rows only"`
}

type viewLogBody struct {
	Entity        string          `json:"entity" doc:"The writing component's uuid"`
	ID            int64           `json:"id"`
	TS            time.Time       `json:"ts" doc:"The time the line carried, else its arrival"`
	ReceivedAt    *time.Time      `json:"received_at,omitempty" doc:"When the server received the line, the order the read pages in"`
	Source        string          `json:"source,omitempty" doc:"The channel it arrived on"`
	Severity      string          `json:"severity,omitempty"`
	Facility      string          `json:"facility,omitempty"`
	Instance      string          `json:"instance,omitempty"`
	Message       string          `json:"message"`
	Attributes    json.RawMessage `json:"attributes,omitempty"`
	Labels        json.RawMessage `json:"labels,omitempty" doc:"History rows only"`
	CorrelationID string          `json:"correlation_id,omitempty" doc:"History rows only"`
}

type viewResultBody struct {
	Cursor      string                   `json:"cursor" doc:"The tail cursor this history was read at: /views:watch from it carries exactly what the history does not. A cursor is <boot>.<sequence>; two from one server boot order by the sequence"`
	Entities    []viewEntityBody         `json:"entities" doc:"The entities the selector resolved to, in scope"`
	From        time.Time                `json:"from" doc:"The range read"`
	To          time.Time                `json:"to"`
	CoveredFrom time.Time                `json:"covered_from" doc:"The earliest instant the read could hold data for: the raw retention's floor, or the trend floor when hourly trends answered the older hours"`
	Metric      []viewMetricSeriesBody   `json:"metric,omitempty" doc:"A metric binding's series"`
	Property    []viewPropertySeriesBody `json:"property,omitempty" doc:"A property binding's series"`
	Events      []viewEventBody          `json:"events,omitempty" doc:"An event binding's page, newest first"`
	Logs        []viewLogBody            `json:"logs,omitempty" doc:"A log binding's page, newest arrival first"`
	NextPage    string                   `json:"next_page,omitempty" doc:"Pages an event or log read further back; absent at the end"`
}

type runViewsInput struct {
	Body struct {
		Bindings []viewBindingBody `json:"bindings" minItems:"1" maxItems:"32" doc:"The bindings read, all from one snapshot"`
	}
}

type runViewsOutput struct {
	Body struct {
		Cursor  string           `json:"cursor" doc:"The tail cursor every result was read at"`
		Now     time.Time        `json:"now" doc:"The snapshot's clock, the end of every relative range"`
		Results []viewResultBody `json:"results" doc:"One result per binding, in order"`
	}
}

func toViewResultBody(cursor string, r storage.ViewResult) viewResultBody {
	out := viewResultBody{Cursor: cursor, From: r.From, To: r.To, CoveredFrom: r.CoveredFrom, NextPage: r.NextPage,
		Entities: make([]viewEntityBody, 0, len(r.Entities))}
	for _, e := range r.Entities {
		out.Entities = append(out.Entities, viewEntityBody{ID: e.ID, Name: e.Name, Kind: e.Kind})
	}
	for _, s := range r.Metric {
		body := viewMetricSeriesBody{Entity: s.EntityID, Key: s.Key, Instance: s.Instance}
		for _, p := range s.Points {
			body.Points = append(body.Points, viewPointBody{TS: p.TS, Value: p.Value})
		}
		for _, b := range s.Buckets {
			body.Buckets = append(body.Buckets, metricBucketBody{TS: b.TS, Count: b.Count, Min: b.Min, Max: b.Max, Avg: b.Avg, Last: b.Last})
		}
		out.Metric = append(out.Metric, body)
	}
	for _, s := range r.Property {
		body := viewPropertySeriesBody{Entity: s.EntityID, Key: s.Key, Instance: s.Instance, CarriedIn: s.CarriedIn}
		for _, p := range s.Points {
			body.Points = append(body.Points, viewPropertyPointBody{TS: p.TS, Value: p.Value})
		}
		for _, b := range s.Buckets {
			body.Buckets = append(body.Buckets, propertyBucketBody{TS: b.TS, Count: b.Count, Last: b.Last, Held: b.Held})
		}
		out.Property = append(out.Property, body)
	}
	for _, e := range r.Events {
		out.Events = append(out.Events, viewEventBody{Entity: e.OwnerID, ID: e.ID, TS: e.TS, Key: e.Key, Instance: e.Instance,
			Origin: e.Origin, Message: e.Message, Attributes: json.RawMessage(e.Attributes), Provenance: e.Provenance,
			Source: e.Source, DerivedByRule: e.DerivedByRule, AlarmID: derefOr(e.AlarmID)})
	}
	for _, l := range r.Logs {
		out.Logs = append(out.Logs, viewLogBody{Entity: l.Component, ID: l.ID, TS: l.TS, ReceivedAt: l.ReceivedAt, Source: l.Source,
			Severity: l.Severity, Facility: l.Facility, Instance: l.Instance, Message: l.Message,
			Attributes: json.RawMessage(l.Attributes), Labels: json.RawMessage(l.Labels), CorrelationID: l.CorrelationID})
	}
	return out
}

// The watch's events. rows carries every binding's matching rows of one or
// more tail commits; its id is the cursor after them, the resume point. mark
// advances the resume point when the commits it passed matched nothing. reset
// names bindings whose cursor the tail no longer holds: the client re-runs
// their history and reconnects with the fresh cursor.
type viewTailBindingBody struct {
	Binding  int                    `json:"binding" doc:"The binding's index in the watch's bindings"`
	Metric   []viewTailMetricBody   `json:"metric,omitempty"`
	Property []viewTailPropertyBody `json:"property,omitempty"`
	Events   []viewEventBody        `json:"events,omitempty"`
	Logs     []viewLogBody          `json:"logs,omitempty"`
}

type viewTailMetricBody struct {
	Entity   string    `json:"entity"`
	Key      string    `json:"key"`
	Instance string    `json:"instance"`
	TS       time.Time `json:"ts"`
	Value    float64   `json:"value"`
}

type viewTailPropertyBody struct {
	Entity   string    `json:"entity"`
	Key      string    `json:"key"`
	Instance string    `json:"instance"`
	TS       time.Time `json:"ts"`
	Value    string    `json:"value"`
}

type viewRowsEvent struct {
	Cursor   string                `json:"cursor" doc:"The cursor after these rows; also the event's id"`
	Bindings []viewTailBindingBody `json:"bindings" doc:"Each binding with rows in these commits"`
}

type viewMarkEvent struct {
	Cursor string `json:"cursor" doc:"The stream's position: every binding has every row up to it"`
}

type viewResetEvent struct {
	Bindings []int `json:"bindings" doc:"The bindings whose cursor the tail no longer holds; re-run their history"`
}

type watchViewsInput struct {
	Bindings        string `query:"bindings" required:"true" maxLength:"32768" doc:"The bindings followed, as a JSON array: each the body of a /views:run binding plus its cursor (the result's cursor it resumes after; unset follows from now)"`
	DurationSeconds int    `query:"duration_seconds" minimum:"1" maximum:"900" doc:"Ends the stream after this many seconds, for a script or the CLI reading a bounded tail. Unset, or past it, the stream ends at its 900 second lifetime, and the client reconnects from its position"`
	LastEventID     string `header:"Last-Event-ID" maxLength:"64" doc:"The last event id the client received, sent by an EventSource on reconnect: every binding resumes from it or its own cursor, whichever is later"`
}

type viewWatchBinding struct {
	viewBindingBody
	Cursor string `json:"cursor,omitempty"`
}

// viewMatch is one binding's live filter: the rows of its primitive whose
// owner it resolved to, whose key it names, inside its range and log filter,
// from commits after its cursor.
type viewMatch struct {
	b      storage.ViewBinding
	after  uint64
	ids    map[string]bool
	keys   map[string]bool
	levels map[string]bool
}

func newViewMatch(b storage.ViewBinding, ents []storage.ViewEntity, after uint64) viewMatch {
	m := viewMatch{b: b, after: after, ids: map[string]bool{}}
	for _, e := range ents {
		m.ids[e.ID] = true
	}
	if len(b.Keys) > 0 {
		m.keys = map[string]bool{}
		for _, k := range b.Keys {
			m.keys[k] = true
		}
	}
	if b.Logs.Severity != "" {
		levels, _ := logline.AtLeast(b.Logs.Severity)
		m.levels = map[string]bool{}
		for _, l := range levels {
			m.levels[l] = true
		}
	}
	return m
}

// matches reports whether a row landed at seq belongs to the binding at now.
func (m viewMatch) matches(seq uint64, r tail.Row, now time.Time) bool {
	if seq <= m.after || r.Lane != m.b.Primitive || r.OwnerKind != m.b.Kind || !m.ids[r.OwnerID] {
		return false
	}
	if m.keys != nil && !m.keys[r.Key] {
		return false
	}
	at := r.TS
	if r.Lane == tail.LaneLog {
		if m.levels != nil && !m.levels[r.Severity] {
			return false
		}
		if m.b.Logs.Source != "" && r.Channel != m.b.Logs.Source {
			return false
		}
		if !r.ReceivedAt.IsZero() {
			at = r.ReceivedAt
		}
	}
	from, to := storage.ViewRange(m.b, now)
	if at.Before(from) {
		return false
	}
	// A relative range ends now and moves with it; an absolute one ends at to.
	return m.b.To.IsZero() || at.Before(to)
}

// viewTailRows folds a run of commits into the rows event: each binding's
// matching rows, in commit order. Empty when nothing matched.
func viewTailRows(matches []viewMatch, commits []tail.Commit, seqs []uint64, now time.Time) []viewTailBindingBody {
	var out []viewTailBindingBody
	for i, m := range matches {
		body := viewTailBindingBody{Binding: i}
		n := 0
		for c, commit := range commits {
			for _, r := range commit.Rows {
				if !m.matches(seqs[c], r, now) {
					continue
				}
				n++
				switch r.Lane {
				case tail.LaneMetric:
					body.Metric = append(body.Metric, viewTailMetricBody{Entity: r.OwnerID, Key: r.Key, Instance: r.Instance, TS: r.TS, Value: r.Number})
				case tail.LaneProperty:
					body.Property = append(body.Property, viewTailPropertyBody{Entity: r.OwnerID, Key: r.Key, Instance: r.Instance, TS: r.TS, Value: r.Text})
				case tail.LaneEvent:
					body.Events = append(body.Events, viewEventBody{Entity: r.OwnerID, ID: r.ID, TS: r.TS, Key: r.Key, Instance: r.Instance,
						Origin: r.Origin, Message: r.Message, Attributes: json.RawMessage(r.Attributes)})
				case tail.LaneLog:
					var received *time.Time
					if !r.ReceivedAt.IsZero() {
						at := r.ReceivedAt
						received = &at
					}
					body.Logs = append(body.Logs, viewLogBody{Entity: r.OwnerID, ID: r.ID, TS: r.TS, ReceivedAt: received, Source: r.Channel,
						Severity: r.Severity, Facility: r.Facility, Instance: r.Instance, Message: r.Message, Attributes: json.RawMessage(r.Attributes)})
				}
			}
		}
		if n > 0 {
			out = append(out, body)
		}
	}
	return out
}

// registerViewRunRoutes wires the views binding's two routes.
func registerViewRunRoutes(api huma.API, a *authenticator, gw storage.Gateway, log *slog.Logger, limits viewWatchLimits) {
	limits = limits.withDefaults()
	watches := newWaitLimiter(viewWatchesTotal, limits.perUser)
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "run-views",
		Extensions:  mcpExposed(),
		Method:      http.MethodPost,
		Path:        "/views:run",
		Summary:     "Read the history of one or more view bindings",
		Description: "Reads each binding's history from one snapshot and answers the tail cursor it was taken at (ADR-1830). A binding names a primitive (metric, property, event or log), an entity selector (literal refs, or a kind and a filter), the keys, and a range (from and to, or window_seconds back from now). A metric or property binding answers one series per resolved entity, key and instance: its raw points, or with step_seconds every bucket of an epoch-aligned grid (metric buckets carry count, min, max, avg and last; property buckets count, last and held), the older hours from the hourly trends when an hour-multiple step reaches past the raw retention. An event or log binding answers a page of rows, newest first, and a next_page. /views:watch from the cursor carries exactly the rows the history does not. Gated by view:read; each binding also needs <kind>:read (component:read or system:read) and resolves only inside that read's scope: a filter selector resolves to the visible entities, and a literal ref out of scope is the same 404 as one that does not exist. A selector past 200 entities, a raw read past 20000 points or a grid past 5000 buckets is a 422, never a truncated answer.",
	}, "view", "read"), func(ctx context.Context, in *runViewsInput) (*runViewsOutput, error) {
		bindings, err := viewBindings(ctx, a, in.Body.Bindings, "body.bindings")
		if err != nil {
			return nil, err
		}
		snap, err := gw.ReadViews(ctx, bindings)
		if err != nil {
			return nil, viewReadErr(ctx, log, err)
		}
		out := &runViewsOutput{}
		out.Body.Cursor, out.Body.Now = snap.Cursor, snap.Now
		out.Body.Results = make([]viewResultBody, 0, len(snap.Results))
		for _, r := range snap.Results {
			out.Body.Results = append(out.Body.Results, toViewResultBody(snap.Cursor, r))
		}
		return out, nil
	})

	op := a.gated(huma.Operation{
		OperationID: "watch-views",
		Method:      http.MethodGet,
		Path:        "/views:watch",
		Summary:     "Follow view bindings live over one SSE stream",
		Description: "Streams the rows that land for every binding as Server-Sent Events, one connection for all of them (ADR-1830). Each binding resumes after its own cursor (the /views:run result's), so history and live rows meet with no gap and no duplicate. A rows event carries each binding's matching rows and has the cursor after them as its id; a mark event advances that position when nothing matched; a reconnecting EventSource sends it back as Last-Event-ID and every binding resumes from it or its own cursor, whichever is later. A reset event names bindings whose cursor the tail no longer holds (too old, or from before a server restart), and the stream ends: the client re-runs their history and reconnects. The tail is the ingest landing point of this server, not a poll; rows a calc or rule derives are not on it. Gated by view:read, with each binding's <kind>:read and scope as /views:run: the selector resolves once at connect, and the stream carries only rows of the entities it resolved to. A stream is bounded so that scope never goes stale: it ends at its lifetime (15 minutes, or duration_seconds) with its position, and on each 15 second heartbeat it rechecks its caller, ending once the session no longer resolves or the caller's grants changed; the client reconnects from its cursor, and the reconnect authenticates and resolves again. One user holds at most 16 watches open; past it the open is a 429.",
	}, "view", "read")
	op.Responses = map[string]*huma.Response{"200": {Description: "The event stream", Content: map[string]*huma.MediaType{
		"text/event-stream": {Schema: viewWatchSchema(api)},
	}}}
	huma.Register(api, op, func(ctx context.Context, in *watchViewsInput) (*huma.StreamResponse, error) {
		var raw []viewWatchBinding
		dec := json.NewDecoder(strings.NewReader(in.Bindings))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&raw); err != nil {
			return nil, huma.Error422UnprocessableEntity("bindings is a JSON array of bindings: "+err.Error(),
				&huma.ErrorDetail{Message: err.Error(), Location: "query.bindings"})
		}
		bodies := make([]viewBindingBody, len(raw))
		for i, r := range raw {
			bodies[i] = r.viewBindingBody
		}
		bindings, err := viewBindings(ctx, a, bodies, "query.bindings")
		if err != nil {
			return nil, err
		}
		hub := gw.ViewTail()
		if hub == nil {
			return nil, huma.Error503ServiceUnavailable("this server has no live tail")
		}
		var resume uint64
		resumed := false
		if in.LastEventID != "" {
			if n, err := hub.Seq(in.LastEventID); err == nil {
				resume, resumed = n, true
			}
		}
		matches := make([]viewMatch, len(bindings))
		var reset []int
		now := hub.Cursor()
		for i, b := range bindings {
			ents, err := gw.ResolveViewEntities(ctx, b)
			if err != nil {
				return nil, viewReadErr(ctx, log, err)
			}
			cursor := raw[i].Cursor
			if cursor == "" {
				cursor = now
			}
			// A binding cursor from before a restart resets, unless the stream
			// already carried this boot's rows past it: a Last-Event-ID on this
			// hub is the client saying it has them.
			after, err := hub.Seq(cursor)
			if err != nil && !resumed {
				reset = append(reset, i)
			}
			after = max(after, resume)
			matches[i] = newViewMatch(b, ents, after)
		}
		life := limits.lifetime
		if d := time.Duration(in.DurationSeconds) * time.Second; d > 0 && d < life {
			life = d
		}
		// The place is taken last, so a refused open holds none, and freed
		// when the stream ends, however it ends.
		user := actorID(ctx)
		if !watches.acquire(user) {
			return nil, huma.NewError(http.StatusTooManyRequests, fmt.Sprintf("too many open watches: a user holds at most %d; close one, or bind more on one watch", limits.perUser))
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer watches.release(user)
			hctx.SetHeader("Content-Type", "text/event-stream")
			hctx.SetHeader("Cache-Control", "no-store")
			hctx.SetStatus(http.StatusOK)
			w := &sseWriter{w: hctx.BodyWriter()}
			stands := func() bool { return a.callerStands(ctx) }
			streamViews(hctx.Context(), hub, w, matches, reset, viewWatchClock{life: life, beat: limits.heartbeat, stands: stands})
		}}, nil
	})
}

// viewWatchClock is one stream's time: it ends after life, and every beat
// it sends its position and asks stands whether its caller still holds the
// credential and the grants it opened with.
type viewWatchClock struct {
	life, beat time.Duration
	stands     func() bool
}

// streamViews follows the tail for matches until the client leaves, the
// stream's life ends, or its caller no longer stands.
func streamViews(ctx context.Context, hub *tail.Hub, w *sseWriter, matches []viewMatch, reset []int, clock viewWatchClock) {
	pos := ^uint64(0)
	for _, m := range matches {
		pos = min(pos, m.after)
	}
	cursor := ""
	if len(reset) == 0 {
		if _, _, err := hub.Read(hubCursor(hub, pos), 1); err != nil {
			// The slowest binding's cursor has left the ring: name every
			// binding the ring no longer reaches.
			for i, m := range matches {
				if _, _, err := hub.Read(hubCursor(hub, m.after), 1); err != nil {
					reset = append(reset, i)
				}
			}
		} else {
			cursor = hubCursor(hub, pos)
		}
	}
	if len(reset) > 0 {
		_ = w.send("", "reset", viewResetEvent{Bindings: reset})
		return
	}
	if err := w.send(cursor, "mark", viewMarkEvent{Cursor: cursor}); err != nil {
		return
	}
	done := time.NewTimer(clock.life)
	defer done.Stop()
	beat := time.NewTicker(clock.beat)
	defer beat.Stop()
	sent := cursor
	for {
		wake := hub.Wait()
		commits, next, err := hub.Read(cursor, viewTailBatch)
		if err != nil {
			// The client fell a whole ring behind: every binding re-reads.
			all := make([]int, len(matches))
			for i := range all {
				all[i] = i
			}
			_ = w.send("", "reset", viewResetEvent{Bindings: all})
			return
		}
		if len(commits) > 0 {
			seqs := make([]uint64, len(commits))
			for i, c := range commits {
				seqs[i], _ = hub.Seq(c.Cursor)
			}
			if rows := viewTailRows(matches, commits, seqs, time.Now()); len(rows) > 0 {
				if err := w.send(next, "rows", viewRowsEvent{Cursor: next, Bindings: rows}); err != nil {
					return
				}
				sent = next
			}
			cursor = next
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-done.C:
			if sent != cursor {
				_ = w.send(cursor, "mark", viewMarkEvent{Cursor: cursor})
			}
			return
		case <-beat.C:
			// A caller whose session ended or whose grants changed gets no
			// more rows: the stream ends, and the reconnect authenticates
			// and resolves its scope again.
			if !clock.stands() {
				return
			}
			if err := w.send(cursor, "mark", viewMarkEvent{Cursor: cursor}); err != nil {
				return
			}
			sent = cursor
		case <-wake:
		}
	}
}

// hubCursor renders a position on hub as its cursor.
func hubCursor(hub *tail.Hub, seq uint64) string {
	cur := hub.Cursor()
	boot, _, _ := strings.Cut(cur, ".")
	return fmt.Sprintf("%s.%d", boot, seq)
}

// sseWriter frames Server-Sent Events and flushes each one.
type sseWriter struct {
	w io.Writer
}

func (s *sseWriter) send(id, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var buf strings.Builder
	if id != "" {
		buf.WriteString("id: " + id + "\n")
	}
	buf.WriteString("event: " + event + "\ndata: ")
	buf.Write(b)
	buf.WriteString("\n\n")
	if _, err := s.w.Write([]byte(buf.String())); err != nil {
		return err
	}
	if rw, ok := s.w.(http.ResponseWriter); ok {
		return http.NewResponseController(rw).Flush()
	}
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// viewWatchSchema publishes the stream's three events in the spec, the shape
// Huma's sse package gives a stream: an array of the possible messages.
func viewWatchSchema(api huma.API) *huma.Schema {
	reg := api.OpenAPI().Components.Schemas
	msg := func(event string, data any, hint string) *huma.Schema {
		return &huma.Schema{
			Title: "Event " + event,
			Type:  huma.TypeObject,
			Properties: map[string]*huma.Schema{
				"id":    {Type: huma.TypeString, Description: "The cursor after the event, the resume point; absent on reset"},
				"event": {Type: huma.TypeString, Description: "The event name", Extensions: map[string]any{"const": event}},
				"data":  reg.Schema(reflect.TypeOf(data), true, hint),
			},
			Required: []string{"event", "data"},
		}
	}
	return &huma.Schema{
		Title:       "Server Sent Events",
		Description: "Each oneOf object is one possible event, serialized as UTF-8 text per the Server-Sent Events specification.",
		Type:        huma.TypeArray,
		Items: &huma.Schema{Extensions: map[string]any{"oneOf": []*huma.Schema{
			msg("mark", viewMarkEvent{}, "mark"),
			msg("reset", viewResetEvent{}, "reset"),
			msg("rows", viewRowsEvent{}, "rows"),
		}}},
	}
}
