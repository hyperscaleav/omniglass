package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The push-ingest write BFF: a first-party route that accepts telemetry in OUR
// schema (a webhook, by contrast, is the case where we do not control the payload
// and something has to normalize it, which is driver work). It is the second ingest
// lane beside the node's, and the two differ in exactly one respect: where the owner
// comes from. A node cannot be trusted to name its own owner, so the server derives
// it from the task's interface; a scoped principal can be, because the scope check
// is the fence.
//
// The route authorizes, resolves and validates, then publishes onto the data lane
// (og.v1.api.telemetry) rather than writing to Postgres directly. That is deliberate:
// the rule engine is designed as a stream consumer, so anything skipping the stream
// would be invisible to every rule, alarm, and derivation. One lane for history,
// regardless of how a record arrived.

// pushBatchLimit bounds how many items one batch may carry, so a single request
// cannot pin the registry snapshot or the publish path for an unbounded time.
//
// The encoded SIZE needs no guard of its own: Huma already caps the request body at
// 1 MiB (a larger body is a 413 before this handler runs), and protobuf encodes more
// compactly than the JSON it came from, so a batch that passed that cap cannot
// exceed NATS's 1 MiB default max payload.
const pushBatchLimit = 1000

// TelemetryPublisher publishes an authorized batch onto the API telemetry lane. The
// bus server implements it; the API holds only this seam, so the route is testable
// with a fake and the handler never owns a NATS connection.
type TelemetryPublisher interface {
	PublishTelemetry(ctx context.Context, b *ogv1.TelemetryBatch) error
}

type pushMetric struct {
	Namespace string    `json:"namespace,omitempty" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The catalog namespace the name is read in: og for the platform's type, local for the operator's, or another the operator named. Omit to walk the search path, the operator's type first, then the platform's. Set, the name is read in that namespace only, reaching an og type the operator's type of the same name shadows; a namespace holding no such type rejects the item"`
	Name      string    `json:"name" minLength:"1" doc:"A registered metric_type name"`
	Instance  string    `json:"instance,omitempty" doc:"The endpoint of the owner this came through, by name (an endpoint is named by its transport); omit for the owner as a whole. An instance naming no endpoint on the owner refuses the batch with a 422"`
	Value     float64   `json:"value" doc:"The numeric observation"`
	TS        time.Time `json:"ts,omitempty" doc:"When this was observed; defaults to the batch timestamp, then to ingest time"`
}

type pushProperty struct {
	Namespace string    `json:"namespace,omitempty" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The catalog namespace the name is read in: og for the platform's type, local for the operator's, or another the operator named. Omit to walk the search path, the operator's type first, then the platform's. Set, the name is read in that namespace only, reaching an og type the operator's type of the same name shadows; a namespace holding no such type rejects the item"`
	Name      string    `json:"name" minLength:"1" doc:"A registered property_type name"`
	Instance  string    `json:"instance,omitempty" doc:"The endpoint of the owner this came through, by name (an endpoint is named by its transport); omit for the owner as a whole. An instance naming no endpoint on the owner refuses the batch with a 422"`
	Value     any       `json:"value" doc:"The observed value, shaped by the type's data_type and validated against its validation schema"`
	TS        time.Time `json:"ts,omitempty" doc:"When this was observed; defaults to the batch timestamp, then to ingest time"`
}

type pushEvent struct {
	Namespace string    `json:"namespace,omitempty" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The catalog namespace the name is read in: og for the platform's type, local for the operator's, or another the operator named. Omit to walk the search path, the operator's type first, then the platform's. Set, the name is read in that namespace only, reaching an og type the operator's type of the same name shadows; a namespace holding no such type rejects the item"`
	Name      string    `json:"name" minLength:"1" doc:"A registered event_type name"`
	Instance  string    `json:"instance,omitempty" doc:"The endpoint of the owner this came through, by name (an endpoint is named by its transport); omit for the owner as a whole. An instance naming no endpoint on the owner refuses the batch with a 422"`
	Message   string    `json:"message,omitempty" doc:"The occurrence's human-readable line"`
	Payload   any       `json:"payload,omitempty" doc:"The occurrence's structured payload, validated against the event_type's payload schema"`
	TS        time.Time `json:"ts,omitempty" doc:"When this occurred; defaults to the batch timestamp, then to ingest time"`
}

type pushLog struct {
	Message       string    `json:"message" minLength:"1" doc:"The raw log text"`
	Source        string    `json:"source,omitempty" doc:"The channel the line arrived on; falls back to the batch source"`
	Severity      string    `json:"severity,omitempty" doc:"The line's severity, when classified"`
	Facility      string    `json:"facility,omitempty" doc:"The line's facility, when classified"`
	CorrelationID string    `json:"correlation_id,omitempty" doc:"Threads related lines and their derived events"`
	TS            time.Time `json:"ts,omitempty" doc:"When the line arrived; defaults to the batch timestamp"`
}

type pushInput struct {
	Body struct {
		Owner struct {
			Kind string `json:"kind" enum:"component,system" doc:"The owner arc: a component, or a system (telemetry about the system as a whole, such as a cloud service's account of the room it serves). A system batch carries no log lines, since a log line is component-owned"`
			Ref  string `json:"ref" minLength:"1" doc:"The owning entity, by name or id"`
		} `json:"owner" doc:"The entity every row in the batch lands under"`
		Source     string         `json:"source,omitempty" doc:"Who observed this batch (recorded as the provenance source on every row)"`
		TS         time.Time      `json:"ts,omitempty" doc:"Batch timestamp; a per-item timestamp overrides it"`
		Metrics    []pushMetric   `json:"metrics,omitempty" doc:"Numeric observations, validated against metric_type"`
		Properties []pushProperty `json:"properties,omitempty" doc:"Categorical observations, validated against property_type and each type's validation schema"`
		Events     []pushEvent    `json:"events,omitempty" doc:"Natively caught occurrences, validated against event_type"`
		Logs       []pushLog      `json:"logs,omitempty" doc:"Raw untyped log lines. No registry gate. Component owners only: a system batch carrying one is refused with a 422"`
	}
}

type pushRejection struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}

// inNamespace is rej naming the namespace the item was read in.
func inNamespace(rej pushRejection, namespace string) pushRejection {
	rej.Namespace = namespace
	return rej
}

type pushOutput struct {
	Status int
	Body   struct {
		Accepted struct {
			Metrics    int `json:"metrics"`
			Properties int `json:"properties"`
			Events     int `json:"events"`
			Logs       int `json:"logs"`
		} `json:"accepted"`
		Rejected []pushRejection `json:"rejected,omitempty" doc:"Names dropped by reject-not-project. Reported synchronously so a caller learns about a typo"`
	}
}

// registerTelemetryRoutes wires the push-ingest write surface.
func registerTelemetryRoutes(api huma.API, a *authenticator, gw storage.Gateway, pub TelemetryPublisher) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "push-telemetry",
		Method:        http.MethodPost,
		Path:          "/telemetry:push",
		DefaultStatus: http.StatusAccepted,
		Summary:       "Push telemetry for an owner",
		Description: "Accepts per-lane observations (metrics, properties, events) and raw log lines for one owner and " +
			"publishes them onto the ingest lane. Each lane validates against its own catalog: an unregistered name is " +
			"rejected and reported in the response rather than silently dropped, and a property or event payload " +
			"violating its type's schema refuses the batch with a 422. Every key has one writer class, and a push " +
			"writes only a collected key: a built-in key a node writes (a reachability verdict or rung, a probe metric " +
			"or timing, a held session's state, collection-failed, an endpoint's collection telemetry) is a node's own " +
			"observation and a platform key (health, the webhook receiver's counts) is the platform's, each rejected " +
			"by name on every lane; a key a calc rule outputs is rejected " +
			"naming the rule. The owner is a component or a system; a system batch carries no log lines (a log line is " +
			"component-owned), refused with a 422. An observation's instance is the endpoint it came through: one naming " +
			"no endpoint on the owner refuses the batch with a 422 naming it and listing the owner's endpoints, and a " +
			"system has none, so a system's observations name no instance. Gated by telemetry:push, whose scope must " +
			"cover the declared owner on its own tier (one check per batch): the push scope alone is enough to see the " +
			"owner (no component:read or system:read is needed), an owner outside both the push scope and the caller's " +
			"read of that kind is a non-disclosing 404, and one the caller can read but may not push to is a 403.",
	}, "telemetry", "push"), func(ctx context.Context, in *pushInput) (*pushOutput, error) {
		if pub == nil {
			return nil, huma.Error503ServiceUnavailable("telemetry ingest is unavailable (no bus)")
		}
		kind := in.Body.Owner.Kind
		if kind != "component" && kind != "system" {
			return nil, huma.Error422UnprocessableEntity("owner.kind must be component or system")
		}
		// A log line comes off a component (#589, log_line is component-owned),
		// so a system has none to land: refused whole, before anything is
		// resolved, rather than half-landing the batch around it.
		if kind == "system" && len(in.Body.Logs) > 0 {
			return nil, huma.Error422UnprocessableEntity("a system batch carries no log lines: a log line is component-owned, so push it under the component it came off")
		}
		items := len(in.Body.Metrics) + len(in.Body.Properties) + len(in.Body.Events) + len(in.Body.Logs)
		if items == 0 {
			return nil, huma.Error422UnprocessableEntity("a batch must carry at least one observation or log line")
		}
		if items > pushBatchLimit {
			return nil, huma.Error422UnprocessableEntity("batch exceeds the per-request item limit")
		}

		// Scope is the fence that makes a caller-declared owner trustworthy
		// (ADR-0752, superseding the 2026-08-27 ruling on #752, widened to a
		// system owner by ADR-0422): the owner is a component or a system
		// target, resolved the way every route that acts on one is (#736,
		// ADR-0116), each on its own tier. The ACTION scope is the one that
		// confers telemetry:push, never a read: a principal routinely holds a
		// wide read and a narrow write (viewer over the fleet plus operator on
		// one component), so fencing with the read scope would let it push to
		// anything it can see. Push is the stronger grant, so its own scope is
		// also enough to see the owner: a push-only integration (no read)
		// pushes where it is granted and learns nothing else. The caller's
		// read of the owner's kind decides only which refusal a miss is owed:
		// an owner outside both scopes is the non-disclosing 404, and one it
		// can read but not push the truthful 403, safe because only a caller
		// who could read that row anyway can meet it.
		//
		// This is the ONLY fence on the write: the route publishes to the bus, and the
		// consumer that finally writes runs as a trusted server process with no scope of
		// its own. Nothing downstream will catch a mistake made here. It runs once per
		// batch (a batch has one owner), never per sample; the second resolve runs
		// only on a refusal, to choose its status.
		ownerID, err := resolvePushOwner(ctx, a, gw, kind, in.Body.Owner.Ref)
		if err != nil {
			return nil, err
		}

		// Reject-not-project, applied HERE so rejection is synchronous. Publishing is
		// asynchronous, so a route that validated nothing would have to answer "202,
		// and good luck", which is useless to a human who mistyped a property name.
		// The registry is an in-memory snapshot, so this costs a map lookup per name.
		metricTypes, err := gw.ListMetricTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("read registry")
		}
		properties, err := gw.ListPropertyTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("read registry")
		}
		eventTypes, err := gw.ListEventTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("read registry")
		}
		reg := collection.NewRegistry(metricTypes, properties, eventTypes)

		out := &pushOutput{Status: http.StatusAccepted}
		batch := &ogv1.TelemetryBatch{
			// The resolved id, not a name: this is the one and only authorization
			// fence on the write (see the comment above), and the consumer
			// that lands this batch runs untrusted-input-shaped re-resolves on
			// every lane (ownerArcValue -> scopedByName). Publishing the name
			// back out means an owner authorized cleanly by its stable uuid
			// gets silently re-resolved by a name that #627 no longer
			// guarantees is unique, discarding the batch after Term()
			// with no operator-visible error even though the caller already
			// got its 202. The wire's Owner.Ref stays dual-accept
			// (ADR-0062, proto/og/v1/telemetry.proto) for any other producer;
			// this route always has an id in hand, so it sends it.
			Owner:  &ogv1.Owner{Kind: kind, Ref: ownerID},
			Source: in.Body.Source,
		}
		if !in.Body.TS.IsZero() {
			batch.Ts = timestamppb.New(in.Body.TS.UTC())
		}

		// Reject-not-project is per lane: a name unknown to the lane's OWN catalog
		// is reported in the rejected list, even when another catalog knows it. A
		// value that fails its type's schema refuses the whole request with a 422
		// instead (the #594 contract): the caller declared the right name, so the
		// mistake is in the data, and half-landing a batch around it would leave
		// the caller guessing which rows exist.
		//
		// A reserved key is refused first, by name and on every lane (#1080,
		// ADR-0161): a native probe key is a node's own observation and a
		// platform key the platform's, so its reason names that, not a lane the
		// caller could switch to. A registered key is then held to its writer
		// class, which refuses a calc rule's output naming the rule. The
		// Storage Gateway holds the consumer's write to the same ruling; this
		// is where the caller hears it.
		for _, m := range in.Body.Metrics {
			if rej, ok := reservedRejection(m.Name); ok {
				out.Body.Rejected = append(out.Body.Rejected, inNamespace(rej, m.Namespace))
				continue
			}
			mt, ok := reg.MetricIn(m.Namespace, m.Name)
			if !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Namespace: m.Namespace, Name: m.Name, Reason: unregisteredReason(reg, m.Namespace, m.Name, "metric"),
				})
				continue
			}
			if rej, ok := classRejection(m.Name, mt.WriterClass, mt.WrittenBy); ok {
				out.Body.Rejected = append(out.Body.Rejected, inNamespace(rej, m.Namespace))
				continue
			}
			batch.Metrics = append(batch.Metrics, &ogv1.MetricSample{
				Namespace: m.Namespace, Name: m.Name, Instance: m.Instance, Value: m.Value, Ts: itemTS(m.TS),
			})
		}

		for _, p := range in.Body.Properties {
			if rej, ok := reservedRejection(p.Name); ok {
				out.Body.Rejected = append(out.Body.Rejected, inNamespace(rej, p.Namespace))
				continue
			}
			pt, ok := reg.PropertyIn(p.Namespace, p.Name)
			if !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Namespace: p.Namespace, Name: p.Name, Reason: unregisteredReason(reg, p.Namespace, p.Name, "property"),
				})
				continue
			}
			if rej, ok := classRejection(p.Name, pt.WriterClass, pt.WrittenBy); ok {
				out.Body.Rejected = append(out.Body.Rejected, inNamespace(rej, p.Namespace))
				continue
			}
			if p.Value == nil {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Namespace: p.Namespace, Name: p.Name, Reason: "no value",
				})
				continue
			}
			raw, err := json.Marshal(p.Value)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("property \"" + p.Name + "\": value is not encodable")
			}
			// The same validator the property catalog itself obeys (ADR-0043):
			// data_type shapes the value, the validation schema constrains it.
			if err := key.ValidateValue(pt.DataType, raw, pt.Validation); err != nil {
				return nil, huma.Error422UnprocessableEntity("property \"" + p.Name + "\": " + err.Error())
			}
			batch.Properties = append(batch.Properties, &ogv1.PropertySample{
				Namespace: p.Namespace, Name: p.Name, Instance: p.Instance, ValueJson: string(raw), Ts: itemTS(p.TS),
			})
		}

		for _, e := range in.Body.Events {
			if rej, ok := reservedRejection(e.Name); ok {
				out.Body.Rejected = append(out.Body.Rejected, inNamespace(rej, e.Namespace))
				continue
			}
			et, ok := reg.EventIn(e.Namespace, e.Name)
			if !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Namespace: e.Namespace, Name: e.Name, Reason: unregisteredReason(reg, e.Namespace, e.Name, "event"),
				})
				continue
			}
			if rej, ok := classRejection(e.Name, et.WriterClass, nil); ok {
				out.Body.Rejected = append(out.Body.Rejected, inNamespace(rej, e.Namespace))
				continue
			}
			if e.Message == "" && e.Payload == nil {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Namespace: e.Namespace, Name: e.Name, Reason: "no content: set message or payload",
				})
				continue
			}
			var payload []byte
			if e.Payload != nil {
				raw, err := json.Marshal(e.Payload)
				if err != nil {
					return nil, huma.Error422UnprocessableEntity("event \"" + e.Name + "\": payload is not encodable")
				}
				if err := key.ValidateValue("json", raw, et.PayloadSchema); err != nil {
					return nil, huma.Error422UnprocessableEntity("event \"" + e.Name + "\": " + err.Error())
				}
				payload = raw
			}
			batch.Events = append(batch.Events, &ogv1.EventSample{
				Namespace: e.Namespace, Name: e.Name, Instance: e.Instance, Message: e.Message, Payload: payload, Ts: itemTS(e.TS),
			})
		}

		for _, l := range in.Body.Logs {
			line := &ogv1.LogLine{
				Message: l.Message, Source: l.Source, Severity: l.Severity,
				Facility: l.Facility, CorrelationId: l.CorrelationID,
			}
			if !l.TS.IsZero() {
				line.Ts = timestamppb.New(l.TS.UTC())
			}
			batch.Logs = append(batch.Logs, line)
		}

		// Everything rejected means nothing to publish: report it and stop rather than
		// putting an empty batch on the lane.
		accepted := len(batch.Metrics) + len(batch.Properties) + len(batch.Events) + len(batch.Logs)
		if accepted == 0 {
			return out, nil
		}
		// An observation's instance is its observer (ADR-0456): on this lane, an
		// endpoint of the owner, named, or none (the owner-level series). One
		// naming no endpoint refuses the batch, checked once per batch over
		// what would be published.
		if err := checkInstances(ctx, gw, kind, ownerID, batch); err != nil {
			return nil, err
		}
		if err := pub.PublishTelemetry(ctx, batch); err != nil {
			return nil, huma.Error503ServiceUnavailable("publish telemetry")
		}
		out.Body.Accepted.Metrics = len(batch.Metrics)
		out.Body.Accepted.Properties = len(batch.Properties)
		out.Body.Accepted.Events = len(batch.Events)
		out.Body.Accepted.Logs = len(batch.Logs)
		return out, nil
	})
}

// resolvePushOwner resolves a push's declared owner through the fence
// (ADR-0752, ADR-0422): first with the telemetry:push scope as both halves, so
// the push grant alone sees its target; on a miss, again with the caller's own
// read of that kind as the read half, only to choose the refusal (outside
// both, the non-disclosing 404; readable but not pushable, the 403). kind is a
// component or a system, each resolved and refused on its own tier.
func resolvePushOwner(ctx context.Context, a *authenticator, gw storage.Gateway, kind, ref string) (string, error) {
	notFound, mapErr := storage.ErrComponentNotFound, mapComponentErr
	if kind == "system" {
		notFound, mapErr = storage.ErrSystemNotFound, mapSystemErr
	}
	pushScope := a.scopeFor(ctx, "telemetry", "push")
	id, err := gw.ResolveActionTarget(ctx, kind, ref, pushScope, pushScope)
	if errors.Is(err, notFound) {
		_, err = gw.ResolveActionTarget(ctx, kind, ref, a.scopeFor(ctx, kind, "read"), pushScope)
		if err == nil {
			err = notFound
		}
	}
	if err != nil {
		return "", mapErr(err)
	}
	return id, nil
}

// reservedRejection refuses a key of the reserved family on every lane, by
// name (#1080, ADR-0161): a native probe key (read from probekeys, the one
// declaration the probes emit from, collection-failed among them) is the
// node's own observation, which the console reads as the node's, and a
// platform key is the platform's (health, the rollup's verdict, and the
// webhook receiver's counts, #1369). The match is
// case-insensitive and ignores surrounding space, failing closed. The reason
// names the key and who writes it.
func reservedRejection(name string) (pushRejection, bool) {
	c, ok := writerclass.Reserved(name)
	if !ok {
		return pushRejection{}, false
	}
	return pushRejection{Name: name, Reason: writerclass.Refusal(c, name, writerclass.Push, "")}, true
}

// classRejection refuses a registered key whose writer class does not admit a
// push: a calc rule's output, which only that rule writes, named in the reason.
func classRejection(name string, class writerclass.Class, calc *storage.CalcWriter) (pushRejection, bool) {
	if writerclass.Admits(class, writerclass.Push, name) {
		return pushRejection{}, false
	}
	rule := ""
	if calc != nil {
		rule = calc.Rule
	}
	return pushRejection{Name: name, Reason: writerclass.Refusal(class, name, writerclass.Push, rule)}, true
}

// itemTS maps an optional per-item timestamp to the wire: absent stays absent
// (nil), so the batch ts, then ingest time, can fill it downstream. A supplied
// ts survives verbatim (normalized to UTC, the storage timezone).
func itemTS(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t.UTC())
}

// maxListedEndpoints bounds the endpoints an unknown-instance refusal lists, so
// a component with many endpoints costs the refusal a fixed size.
const maxListedEndpoints = 10

// checkInstances refuses a batch any of whose observations names an instance
// that is no endpoint on its owner (ADR-0456: an observed row's instance is
// its observer, and on the push lane that is one of the owner's endpoints).
// It reads the owner's endpoint names once, and only when an instance is
// named at all, so its cost is per batch, never per sample. A system has no
// endpoints (ADR-0422), so on a system any named instance refuses, with no
// read at all. The refusal names the first unknown instance and lists the
// owner's endpoints, bounded: the caller already resolved the owner within
// its own scope.
func checkInstances(ctx context.Context, gw storage.Gateway, kind, ownerID string, b *ogv1.TelemetryBatch) error {
	named := map[string]bool{}
	for _, m := range b.Metrics {
		named[m.GetInstance()] = true
	}
	for _, p := range b.Properties {
		named[p.GetInstance()] = true
	}
	for _, e := range b.Events {
		named[e.GetInstance()] = true
	}
	delete(named, "")
	if len(named) == 0 {
		return nil
	}
	var names []string
	if kind == "component" {
		var err error
		if names, err = gw.ComponentEndpointNames(ctx, ownerID); err != nil {
			return huma.Error500InternalServerError("read the owner's endpoints")
		}
	}
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	for _, inst := range slices.Sorted(maps.Keys(named)) {
		if !known[inst] {
			return huma.Error422UnprocessableEntity(fmt.Sprintf(
				"instance %q names no endpoint on this %s: an observation's instance is the endpoint it came through, or none for the %s as a whole; %s",
				inst, kind, kind, endpointListing(names)))
		}
	}
	return nil
}

// endpointListing is the refusal's list of an owner's endpoint names, sorted
// and bounded at maxListedEndpoints.
func endpointListing(names []string) string {
	if len(names) == 0 {
		return "it has no endpoints"
	}
	sorted := slices.Sorted(slices.Values(names))
	shown := sorted
	if len(shown) > maxListedEndpoints {
		shown = shown[:maxListedEndpoints]
	}
	out := "its endpoints: " + strings.Join(shown, ", ")
	if rest := len(sorted) - len(shown); rest > 0 {
		out += fmt.Sprintf(", and %d more", rest)
	}
	return out
}

// unregisteredReason is why a push's name did not resolve on its lane: it is
// registered nowhere, or the name is held on another lane by an operator's row
// that shadows an official one of the same name on this install (#1307), which
// is not "unregistered": the official catalog does hold it.
func unregisteredReason(reg collection.Registry, namespace, name, kind string) string {
	if namespace != "" {
		return fmt.Sprintf("no %s type %q in namespace %q", kind, name, namespace)
	}
	if reg.IsShadowed(name) {
		return "name is shadowed on this install: an operator's row of the same name in another catalog takes it over the official " + kind + " type; name namespace \"og\" to reach the official one"
	}
	return "unregistered " + kind + " type name"
}
