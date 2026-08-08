package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/hyperscaleav/omniglass/internal/storage"
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
	Name     string    `json:"name" minLength:"1" doc:"A registered metric_type name"`
	Instance string    `json:"instance,omitempty" doc:"Discriminates many values of one name on one owner (three fan speeds, per-port counters)"`
	Value    float64   `json:"value" doc:"The numeric observation"`
	TS       time.Time `json:"ts,omitempty" doc:"When this was observed; defaults to the batch timestamp, then to ingest time"`
}

type pushProperty struct {
	Name     string    `json:"name" minLength:"1" doc:"A registered property_type name"`
	Instance string    `json:"instance,omitempty" doc:"Discriminates many values of one name on one owner"`
	Value    any       `json:"value" doc:"The observed value, shaped by the type's data_type and validated against its validation schema"`
	TS       time.Time `json:"ts,omitempty" doc:"When this was observed; defaults to the batch timestamp, then to ingest time"`
}

type pushEvent struct {
	Name     string    `json:"name" minLength:"1" doc:"A registered event_type name"`
	Instance string    `json:"instance,omitempty" doc:"Discriminates many occurrences of one name on one owner"`
	Message  string    `json:"message,omitempty" doc:"The occurrence's human-readable line"`
	Payload  any       `json:"payload,omitempty" doc:"The occurrence's structured payload, validated against the event_type's payload schema"`
	TS       time.Time `json:"ts,omitempty" doc:"When this occurred; defaults to the batch timestamp, then to ingest time"`
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
			Kind string `json:"kind" enum:"component" doc:"The owner arc. Only component today; system and location arrive with #422"`
			Ref  string `json:"ref" minLength:"1" doc:"The owning entity, by name or id"`
		} `json:"owner" doc:"The entity every row in the batch lands under"`
		Source     string         `json:"source,omitempty" doc:"Who observed this batch (recorded as the provenance source on every row)"`
		TS         time.Time      `json:"ts,omitempty" doc:"Batch timestamp; a per-item timestamp overrides it"`
		Metrics    []pushMetric   `json:"metrics,omitempty" doc:"Numeric observations, validated against metric_type"`
		Properties []pushProperty `json:"properties,omitempty" doc:"Categorical observations, validated against property_type and each type's validation schema"`
		Events     []pushEvent    `json:"events,omitempty" doc:"Natively caught occurrences, validated against event_type"`
		Logs       []pushLog      `json:"logs,omitempty" doc:"Raw untyped log lines. No registry gate"`
	}
}

type pushRejection struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
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
			"violating its type's schema refuses the batch with a 422. Gated by telemetry:push, and the " +
			"caller's scope must cover the declared owner; an out-of-scope owner is a non-disclosing 404.",
	}, "telemetry", "push"), func(ctx context.Context, in *pushInput) (*pushOutput, error) {
		if pub == nil {
			return nil, huma.Error503ServiceUnavailable("telemetry ingest is unavailable (no bus)")
		}
		if in.Body.Owner.Kind != "component" {
			return nil, huma.Error422UnprocessableEntity("owner.kind must be component")
		}
		items := len(in.Body.Metrics) + len(in.Body.Properties) + len(in.Body.Events) + len(in.Body.Logs)
		if items == 0 {
			return nil, huma.Error422UnprocessableEntity("a batch must carry at least one observation or log line")
		}
		if items > pushBatchLimit {
			return nil, huma.Error422UnprocessableEntity("batch exceeds the per-request item limit")
		}

		// Scope is the fence that makes a caller-declared owner trustworthy, and it must
		// be the scope that actually confers telemetry:push, NOT the component:read
		// scope. A principal routinely holds a wide read and a narrow write (viewer over
		// the estate plus operator on one component), so fencing this with the read scope
		// would let it push telemetry to anything it can see. Resolving the push scope and
		// looking the owner up through it means an owner outside the caller's push
		// authority is a non-disclosing 404, the same shape as everywhere else.
		//
		// This is the ONLY fence on the write: the route publishes to the bus, and the
		// consumer that finally writes runs as a trusted server process with no scope of
		// its own. Nothing downstream will catch a mistake made here.
		pushScope := a.scopeFor(ctx, "telemetry", "push")
		comp, err := gw.GetComponent(ctx, in.Body.Owner.Ref, pushScope)
		if err != nil {
			return nil, mapComponentErr(err)
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
			// comp.ID, not comp.Name: this is the one and only authorization
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
			Owner:  &ogv1.Owner{Kind: "component", Ref: comp.ID},
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
		for _, m := range in.Body.Metrics {
			if _, ok := reg.Metric(m.Name); !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: m.Name, Reason: "unregistered metric type name",
				})
				continue
			}
			batch.Metrics = append(batch.Metrics, &ogv1.MetricSample{
				Name: m.Name, Instance: m.Instance, Value: m.Value, Ts: itemTS(m.TS),
			})
		}

		for _, p := range in.Body.Properties {
			pt, ok := reg.Property(p.Name)
			if !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: p.Name, Reason: "unregistered property type name",
				})
				continue
			}
			if p.Value == nil {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: p.Name, Reason: "no value",
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
				Name: p.Name, Instance: p.Instance, ValueJson: string(raw), Ts: itemTS(p.TS),
			})
		}

		for _, e := range in.Body.Events {
			et, ok := reg.Event(e.Name)
			if !ok {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: e.Name, Reason: "unregistered event type name",
				})
				continue
			}
			if e.Message == "" && e.Payload == nil {
				out.Body.Rejected = append(out.Body.Rejected, pushRejection{
					Name: e.Name, Reason: "no content: set message or payload",
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
				Name: e.Name, Instance: e.Instance, Message: e.Message, Payload: payload, Ts: itemTS(e.TS),
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

// itemTS maps an optional per-item timestamp to the wire: absent stays absent
// (nil), so the batch ts, then ingest time, can fill it downstream. A supplied
// ts survives verbatim (normalized to UTC, the storage timezone).
func itemTS(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t.UTC())
}
